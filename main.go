package main

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"faucetpadporter/apkengine"
	"faucetpadporter/utils"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var sysType string
var basePkg string
var portPkg string
var buildhost string
var buildtime string
var skip_thread_limit bool //跳过线程数量检查
var primary_port bool      //基本移植
var init_debug bool        //开启包内debug
var dec_mode bool          //整包apk逆向模式
var gitver string
var err error

var Execpath string
var Binpath string
var Imgextractorpath string
var Tmppath string
var Outpath string
var thread int
var current_stage int

var base_device_id string
var port_device_id string
var base_density_v2_prop string
var Wg sync.WaitGroup

type Permissions struct {
	XMLName xml.Name             `xml:"permissions"`
	PrivApp []PrivAppPermissions `xml:"privapp-permissions"`
}

type PrivAppPermissions struct {
	Package    string       `xml:"package,attr"`
	Permission []Permission `xml:"permission"`
}

type Permission struct {
	Name string `xml:"name,attr"`
}
type APKInfo struct {
	Name string
	Path string
}

func ErrorAndExit(msg string) {
	fmt.Println("Error:" + msg)
	if current_stage != 0 {
		fmt.Printf("You can add argument -stage %d\n", current_stage)
	}
	os.Exit(0)
}
func checkerr(err error) {
	if err != nil {
		ErrorAndExit(err.Error())
	}
}
func ignore_err(err error) {
	if err != nil {
		fmt.Println("warning:", err)
	}
}
func payloaddump(filename string, dumppath string) error {
	extractPath := filepath.Join(Execpath, "tmp", dumppath)
	payloadPath := filepath.Join(Execpath, "tmp", filename)
	thread_ := fmt.Sprintf("%d", thread/2)
	fmt.Println("payload->thread", thread_)
	err = utils.RunCommand(Binpath, "./payload-dumper-go", "-c", thread_, "-o", extractPath, payloadPath)
	if err != nil {
		return err
	}
	err = utils.DeleteFile(payloadPath) //删除文件以节省空间
	return err
}

// 注意：dest是目录名
func UnzipPayloadbin(pkg string, dest string, filename string, rename string) error {
	targetDir := filepath.Join(Execpath, dest)
	filesToExtract := []string{filename}
	err = utils.Unzip(pkg, targetDir, filesToExtract, rename)
	if err != nil {
		return err
	}
	return nil
}

func extractimg(imgpath, outputpath string) {
	format := utils.CheckFormat(imgpath)
	fmt.Println(imgpath, "format", format)
	if format == "erofs" {
		err = utils.RunCommand(Binpath, "./extract.erofs", "-x", "-i", imgpath, "-o", outputpath)
		checkerr(err)
	} else if format == "ext" {
		err = utils.RunCommand(Imgextractorpath, "python", "./imgextractor.py", imgpath, outputpath)
		checkerr(err)
	} else {
		ErrorAndExit("unknown image?" + imgpath)
	}
}

func extract_all_images(parts []string) {
	var wg sync.WaitGroup
	for _, part := range parts {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			imgpath := filepath.Join(Tmppath, "base_payload", p+".img")
			outputpath := filepath.Join(Tmppath, "base_images")
			extractimg(imgpath, outputpath)
		}(part)
		if dec_mode {
			continue
		}
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			imgpath := filepath.Join(Tmppath, "port_payload", p+".img")
			outputpath := filepath.Join(Tmppath, "port_images")
			extractimg(imgpath, outputpath)
		}(part)
	}
	wg.Wait()
}

// 考虑到未来重新打包可能存在重打包base的情况，true为打包port，false为打包base
func package_img(parts []string, base_or_port bool) {
	var wg sync.WaitGroup
	for _, imgname := range parts {
		wg.Add(1)
		go func(imgname string) {
			defer wg.Done()
			var imgpath string
			var fsconfig_path string
			var context_config_path string
			if base_or_port {
				imgpath = filepath.Join(Tmppath, "port_images", imgname)
				fsconfig_path = filepath.Join(Tmppath, "port_images", "config", imgname+"_fs_config")
				context_config_path = filepath.Join(Tmppath, "port_images", "config", imgname+"_file_contexts")
				fmt.Println(imgpath)
				fmt.Println(fsconfig_path)
			} else {
				imgpath = filepath.Join(Tmppath, "base_images", imgname)
				fsconfig_path = filepath.Join(Tmppath, "base_images", "config", imgname+"_fs_config")
				context_config_path = filepath.Join(Tmppath, "base_images", "config", imgname+"_file_contexts")
				fmt.Println(imgpath)
				fmt.Println(fsconfig_path)
			}
			err = utils.RunCommand(Imgextractorpath, "python", "./fspatch.py", imgpath, fsconfig_path)
			checkerr(err)
			err = utils.RunCommand(Imgextractorpath, "python", "./contextpatch.py", imgpath, context_config_path)
			checkerr(err)
			currentTime := time.Now()
			unixTimestamp := fmt.Sprintf("%d", currentTime.Unix())
			output_img_path := filepath.Join(Outpath, imgname+".img")
			err = utils.RunCommand(Binpath, "./mkfs.erofs", "-z", "lz4hc,8", "-T", unixTimestamp, "--mount-point=/"+imgname, "--fs-config-file="+fsconfig_path, "--file-contexts="+context_config_path, output_img_path, imgpath)
			checkerr(err)
		}(imgname)
	}
	wg.Wait()
}

func getAndroidPropValue(propFile, propName string) (string, error) {
	file, err := os.Open(propFile)
	if err != nil {
		return "", err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.SplitN(line, "=", 2)
		if len(fields) == 2 && strings.TrimSpace(fields[0]) == propName {
			return strings.TrimSpace(fields[1]), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("property not found: %s", propName)
}

// chatgpt
func updateAndroidPropValue(propFile, propName, newValue string) error {
	file, err := os.OpenFile(propFile, os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	var lines []string
	var found bool
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.SplitN(line, "=", 2)
		if len(fields) == 2 && strings.TrimSpace(fields[0]) == propName {
			line = fmt.Sprintf("%s=%s", propName, newValue)
			found = true
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if !found {
		lines = append(lines, fmt.Sprintf("%s=%s", propName, newValue))
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.Seek(0, 0); err != nil {
		return err
	}
	writer := bufio.NewWriter(file)
	for _, line := range lines {
		fmt.Fprintln(writer, line)
	}
	return writer.Flush()
}
func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return ""
	}
	return hostname
}
func getCurrentTime() string {
	currentTime := time.Now()
	formattedTime := currentTime.Format("2006-01-02_150405")
	return formattedTime
}
func calculateMD5Hash(text string) string {
	hasher := md5.New()
	hasher.Write([]byte(text))
	hashSum := hasher.Sum(nil)
	hexString := hex.EncodeToString(hashSum)
	return hexString
}

func insertStringBeforeTag(filename, searchString, insertString string) error {
	file, err := os.OpenFile(filename, os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	var lines []string
	var lineNumber int
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if scanner.Text() == searchString {
			lineNumber = len(lines)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	lines = append(lines[:lineNumber-1], append([]string{insertString}, lines[lineNumber-1:]...)...)
	if _, err := file.Seek(0, 0); err != nil {
		return err
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	writer := bufio.NewWriter(file)
	defer writer.Flush()
	for _, line := range lines {
		_, err := fmt.Fprintln(writer, line)
		if err != nil {
			return err
		}
	}
	return nil
}
func findAPKs(dir string) []APKInfo {
	var apkInfos []APKInfo
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(info.Name()), ".apk") {
			name := strings.TrimSuffix(info.Name(), ".apk")
			apkInfos = append(apkInfos, APKInfo{Name: name, Path: path})
		}
		return nil
	})
	if err != nil {
		fmt.Println("Error:", err)
	}
	return apkInfos
}

func decompile_apks(apkinfo []APKInfo) {
	//var wg sync.WaitGroup
	jadxpath := filepath.Join(Execpath, "bin", "jadx", "bin")
	for _, apk_info := range apkinfo {
		cmd := []string{"-d", filepath.Join(Tmppath, "apk_analysis", apk_info.Name), apk_info.Path, "--deobf", "--deobf-use-sourcename", "--threads-count", fmt.Sprintf("%d", thread)}
		err = utils.RunCommand(jadxpath, "./jadx", cmd...)

		/*wg.Add(1)
		go func(apk_info APKInfo) {
			defer wg.Done()
			cmd:=[]string{"-d",filepath.Join(Tmppath,"apk_analysis",apk_info.Name),apk_info.Path,"--deobf","--deobf-use-sourcename"}
			err=utils.RunCommand(jadxpath,"./jadx",cmd...)
		}(apk_info)*/
	}
	//wg.Wait()
}

// chatgpt
func extractDate(dateStr, layout string) string {
	parts := strings.Split(dateStr, "-")
	if len(parts) < 2 {
		return ""
	}
	datePart := parts[1]
	t, err := time.Parse(layout, "rootfs-"+datePart)
	if err != nil {
		return ""
	}
	return t.Format(layout)
}
func stage1_unzip() {
	fmt.Println("stage 1: unzipping base pkg and port pkg")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err = UnzipPayloadbin(basePkg, "tmp", "payload.bin", "base.payload.bin")
		checkerr(err)
	}()
	if !dec_mode {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err = UnzipPayloadbin(portPkg, "tmp", "payload.bin", "port.payload.bin")
			checkerr(err)
		}()
	}
	wg.Wait()

}

func stage2_unpayload() {

	fmt.Println("stage 2: unpack payload.bin")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err = payloaddump("base.payload.bin", "base_payload")
		checkerr(err)
	}()

	if !dec_mode {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err = payloaddump("port.payload.bin", "port_payload")
			checkerr(err)
		}()
	}
	wg.Wait()
}
func stage3_unparse() {
	fmt.Println("stage 3: unparse images of super (base port) (system system_ext product mi_ext)")
	if !dec_mode {
		utils.CreateDirectoryIfNotExists(filepath.Join(Execpath, "tmp", "port_images", "config"))
	}
	utils.CreateDirectoryIfNotExists(filepath.Join(Execpath, "tmp", "base_images", "config"))
	parts := []string{"system", "system_ext", "product", "mi_ext", "odm"}
	extract_all_images(parts)
	if dec_mode {
		return
	}
	base_product_prop := filepath.Join(Tmppath, "base_images", "product", "etc", "build.prop")
	checkerr(err)
	port_product_prop := filepath.Join(Tmppath, "port_images", "product", "etc", "build.prop")
	checkerr(err)
	base_device_id, err = getAndroidPropValue(base_product_prop, "ro.product.product.name")
	checkerr(err)
	port_device_id, err = getAndroidPropValue(port_product_prop, "ro.product.product.name")
	checkerr(err)
	fmt.Println("base_device id:", base_device_id)
	fmt.Println("base_device id:", port_device_id)
}
func stage4_modify_prop_config() {
	defer Wg.Done()
	fmt.Println("stage 4: read configs and modify")
	base_product_prop := filepath.Join(Tmppath, "base_images", "product", "etc", "build.prop")
	checkerr(err)
	port_product_prop := filepath.Join(Tmppath, "port_images", "product", "etc", "build.prop")
	checkerr(err)
	port_miext_prop := filepath.Join(Tmppath, "port_images", "mi_ext", "etc", "build.prop")
	fmt.Println("mod:", port_miext_prop)
	err = updateAndroidPropValue(port_miext_prop, "ro.product.mod_device", base_device_id)
	checkerr(err)
	err = updateAndroidPropValue(port_miext_prop, "ro.faucetpadporter.settings", port_device_id+"/"+base_device_id+"/"+buildtime+"/"+buildhost+"/"+calculateMD5Hash(port_device_id+base_device_id+buildtime+buildhost+"faucetpadporter"))
	checkerr(err)
	if init_debug {
		updateAndroidPropValue(port_miext_prop, "ro.secure", "0")
		updateAndroidPropValue(port_miext_prop, "ro.adb.secure", "0")
		updateAndroidPropValue(port_miext_prop, "ro.debuggable", "1")
	}
	err = updateAndroidPropValue(port_product_prop, "ro.product.product.name", base_device_id)
	checkerr(err)
	base_density_v2_prop, err = getAndroidPropValue(base_product_prop, "persist.miui.density_v2")
	err = updateAndroidPropValue(port_product_prop, "persist.miui.density_v2", base_density_v2_prop)
	checkerr(err)
	err = updateAndroidPropValue(port_product_prop, "ro.sf.lcd_density", base_density_v2_prop) //关于dpi相关
	ignore_err(err)
	err = updateAndroidPropValue(port_product_prop, "persist.miui.auto_ui_enable", "true") //关于应用布局优化
	checkerr(err)
	err = updateAndroidPropValue(port_product_prop, "debug.game.video.speed", "true") //游戏视频三倍加速
	checkerr(err)
	err = updateAndroidPropValue(port_product_prop, "debug.game.video.support", "true") //游戏视频三倍加速
	checkerr(err)
	err = updateAndroidPropValue(port_product_prop, "persist.sys.background_blur_supported", "true") //高级材质2
	checkerr(err)
	err = updateAndroidPropValue(port_product_prop, "persist.sys.background_blur_version", "2") //高级材质2
	checkerr(err)

}
func stage5_modify_overlay_config() {
	defer Wg.Done()
	fmt.Println("stage 5: modify AospFrameworkResOverlay MiuiFrameworkResOverlay DevicesAndroidOverlay")
	base_aosp_overlay := filepath.Join(Tmppath, "base_images", "product", "overlay", "AospFrameworkResOverlay.apk")
	port_aosp_overlay := filepath.Join(Tmppath, "port_images", "product", "overlay", "AospFrameworkResOverlay.apk")
	err = utils.ReplaceFile(base_aosp_overlay, port_aosp_overlay)
	checkerr(err)
	base_miui_overlay := filepath.Join(Tmppath, "base_images", "product", "overlay", "MiuiFrameworkResOverlay.apk")
	port_miui_overlay := filepath.Join(Tmppath, "port_images", "product", "overlay", "MiuiFrameworkResOverlay.apk")
	err = utils.ReplaceFile(base_miui_overlay, port_miui_overlay)
	checkerr(err)
	base_device_overlay := filepath.Join(Tmppath, "base_images", "product", "overlay", "DevicesAndroidOverlay.apk")
	port_device_overlay := filepath.Join(Tmppath, "port_images", "product", "overlay", "DevicesAndroidOverlay.apk")
	err = utils.ReplaceFile(base_device_overlay, port_device_overlay)
	checkerr(err)
	base_device1_overlay := filepath.Join(Tmppath, "base_images", "product", "overlay", "DevicesOverlay.apk")
	port_device1_overlay := filepath.Join(Tmppath, "port_images", "product", "overlay", "DevicesOverlay.apk")
	err = utils.ReplaceFile(base_device1_overlay, port_device1_overlay)
	checkerr(err)
	stage22_enable_cellular_share()
}
func stage6_modify_displayconfig() {
	defer Wg.Done()
	fmt.Println("stage 6: replace media and displayid folder")
	base_media := filepath.Join(Tmppath, "base_images", "product", "media")
	port_media := filepath.Join(Tmppath, "port_images", "product", "media")
	err = utils.ReplaceFolder(base_media, port_media)
	checkerr(err)
	base_display := filepath.Join(Tmppath, "base_images", "product", "etc", "displayconfig")
	port_display := filepath.Join(Tmppath, "port_images", "product", "etc", "displayconfig")
	err = utils.ReplaceFolder(base_display, port_display)
	checkerr(err)
}
func stage7_change_device_features() {
	defer Wg.Done()
	fmt.Println("stage 7: change device_features")
	base_feature := filepath.Join(Tmppath, "base_images", "product", "etc", "device_features")
	port_feature := filepath.Join(Tmppath, "port_images", "product", "etc", "device_features")
	err = utils.ReplaceFolder(base_feature, port_feature)
	checkerr(err)
	//wild mode?
	err = insertStringBeforeTag(filepath.Join(port_feature, base_device_id+".xml"), "</features>", `    <bool name="support_wild_boost">true</bool>`)
	checkerr(err)
}
func stage8_modify_camera() {
	defer Wg.Done()
	fmt.Println("stage 8: modify Camera")
	base_cam := filepath.Join(Tmppath, "base_images", "product", "priv-app", "MiuiCamera")
	port_cam_orig := filepath.Join(Tmppath, "port_images", "product", "priv-app", "MiuiCamera")
	port_cam_To := filepath.Join(Tmppath, "port_images", "product", "app", "MiuiCamera")
	err = utils.DeleteDirectory(port_cam_orig)
	ignore_err(err)
	err = utils.ReplaceFolder(base_cam, port_cam_To)
	checkerr(err)
}
func stage9_add_autoui_adaption() {
	defer Wg.Done()
	fmt.Println("stage 9: add autoui adaption")
	base_autoui := filepath.Join(Tmppath, "base_images", "product", "etc", "autoui_list.xml")
	port_autoui := filepath.Join(Tmppath, "port_images", "product", "etc", "autoui_list.xml")
	//port_autoui_folder := filepath.Join(Tmppath,"port_images","product","etc")
	if utils.FileExists(base_autoui) && !utils.FileExists(port_autoui) {
		fmt.Println("found autoui_list.xml and port file don't have")
		err = utils.CopyFile(base_autoui, port_autoui)
		checkerr(err)
	} else {
		fmt.Println("Port rom has autoui rules. don't move.")
	}
}
func stage10_fix_biometric_face() {
	defer Wg.Done()
	fmt.Println("stage 10: Fix biometric face")
	port_Biometric_folder := filepath.Join(Tmppath, "port_images", "product", "app", "Biometric")
	base_Biometric_folder := filepath.Join(Tmppath, "base_images", "product", "app", "Biometric")
	if utils.DirectoryExists(base_Biometric_folder) {
		fmt.Println("found base bio app folder ! Start replace")
		utils.ReplaceFolder(base_Biometric_folder, port_Biometric_folder)
	}
}
func stage11_unlock_freeform_settings() {
	defer Wg.Done()
	fmt.Println("stage 11: Unlock freeform settings")
	var apk apkengine.Apkfile
	apk.Apkpath = filepath.Join(Tmppath, "port_images", "system_ext", "framework", "miui-services.jar")
	apk.Pkgname = "miui_services"
	apk.Execpath = Execpath
	apk.Need_api_29 = true
	apkengine.PatchApk_Return_number(apk, "com.android.server.wm.MiuiFreeFormStackDisplayStrategy", "getMaxMiuiFreeFormStackCount", 256)
	apkengine.RepackApk(apk)
}
func stage12_settings_unlock_content_extension() {
	defer Wg.Done()
	fmt.Println("stage 12: Unlock Content Extension(Settings)")
	var apk apkengine.Apkfile
	apk.Apkpath = filepath.Join(Tmppath, "port_images", "system_ext", "priv-app", "Settings", "Settings.apk")
	apk.Pkgname = "Settings"
	apk.Execpath = Execpath
	apkengine.PatchApk_Return_Boolean(apk, "com.android.settings.utils.SettingsFeatures", "isNeedRemoveContentExtension", false)
	apkengine.PatchApk_Return_Boolean(apk, "com.android.settings.utils.SettingsFeatures", "shouldShowAutoUIModeSetting", true)
	apkengine.PatchApk_Return_Boolean(apk, "com.android.settings.utils.SettingsFeatures", "showHighRefreshPreference", true)
	apkengine.PatchApk_Return_Boolean(apk, "com.android.settings.utils.SettingsFeatures", "isSupportMiuiDesktopMode", true)
	//设置statusbar图标数量
	/*
		填坑:使用apkeditor可能会导致签名炸/??未知错误，待解决
		lines,err:=utils.ReadLinesFromFile(filepath.Join(Execpath,"res","Settings_patch1.txt"))
		checkerr(err)
		apkengine.PatchApk_Return_and_patch_line(apk,"com.android.settings.NotificationStatusBarSettings","setupShowNotificationIconCount",lines)
		apkengine.ModifyRes_stringArray(apk,filepath.Join("values","arrays.xml"),"notification_icon_counts_values",[]string{"0","3","10"})
	*/
	apkengine.RepackApk(apk)
}
func stage13_patch_systemUI() {
	defer Wg.Done()
	fmt.Println("stage 13: Patch systemUI")
	var apk apkengine.Apkfile
	apk.Apkpath = filepath.Join(Tmppath, "port_images", "system_ext", "priv-app", "MiuiSystemUI", "MiuiSystemUI.apk")
	apk.Pkgname = "MiuiSystemUI"
	apk.Execpath = Execpath
	apkengine.Add_method_after(apk, "com.android.wm.shell.miuimultiwinswitch.miuiwindowdecor.MiuiDotView", filepath.Join(Execpath, "res", "systemUI_patch1.txt"))
	apkengine.Patch_before_funcstart(apk, "com.android.wm.shell.miuimultiwinswitch.miuiwindowdecor.MiuiDotView", "onDraw", filepath.Join(Execpath, "res", "systemUI_patch2.txt"), false)
	apkengine.Add_method_after(apk, "com.android.wm.shell.miuifreeform.MiuiInfinityModeTaskRepository", filepath.Join(Execpath, "res", "systemUI_patch4.txt"))
	apkengine.Patch_before_funcstart(apk, "com.android.wm.shell.miuifreeform.MiuiInfinityModeTaskRepository", "findTopDraggableFullscreenTaskInfo", filepath.Join(Execpath, "res", "systemUI_patch3.txt"), true)
	apkengine.Add_method_after(apk, "com.android.systemui.navigationbar.gestural.NavigationHandle", filepath.Join(Execpath, "res", "systemUI_patch5.txt"))
	apkengine.Patch_before_funcstart(apk, "com.android.systemui.navigationbar.gestural.NavigationHandle", "onDraw", filepath.Join(Execpath, "res", "systemUI_patch6.txt"), false)
	apkengine.RepackApk(apk)
}
func stage14_fix_content_extension() {
	defer Wg.Done()
	fmt.Println("stage 14: fix content extentension (file)")
	xmlFilePath := filepath.Join(Tmppath, "port_images", "product", "etc", "permissions", "privapp-permissions-product.xml")
	file, err := os.Open(xmlFilePath)
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer file.Close()
	var permissions Permissions
	decoder := xml.NewDecoder(file)
	err = decoder.Decode(&permissions)
	if err != nil {
		fmt.Println("Error decoding XML:", err)
		return
	}
	newPrivApp := PrivAppPermissions{
		Package: "com.miui.contentextension",
		Permission: []Permission{
			{Name: "android.permission.WRITE_SECURE_SETTINGS"},
		},
	}
	newPrivApp1 := PrivAppPermissions{
		Package: "com.miui.calculator2",
		Permission: []Permission{
			{Name: "android.permission.WRITE_SECURE_SETTINGS"},
			{Name: "com.miui.securitycenter.permission.SYSTEM_PERMISSION_DECLARE"},
			{Name: "android.permission.RECEIVE_BOOT_COMPLETED"},
		},
	}
	permissions.PrivApp = append(permissions.PrivApp, newPrivApp)
	permissions.PrivApp = append(permissions.PrivApp, newPrivApp1)
	xmlBytes, err := xml.MarshalIndent(permissions, "", "    ")
	if err != nil {
		fmt.Println("Error encoding XML:", err)
		return
	}
	err = os.WriteFile(xmlFilePath, xmlBytes, 0644)
	checkerr(err)
	utils.CopyFile(filepath.Join(Execpath, "res", "MIUIContentExtension.apk"), filepath.Join(Tmppath, "port_images", "product", "priv-app", "MIUIContentExtension", "MIUIContentExtension.apk"))
}
func stage15_downgrade_privapp_verification() {
	defer Wg.Done()
	fmt.Println("stage 15:downgrade priv-app verification")
	var apk apkengine.Apkfile
	apk.Apkpath = filepath.Join(Tmppath, "port_images", "system", "system", "framework", "services.jar")
	apk.Pkgname = "services"
	apk.Execpath = Execpath
	outputpath, err := apkengine.DecompileApk(apk)
	checkerr(err)
	err = utils.CopyFile(filepath.Join(Execpath, "res", "signkill.smali"), filepath.Join(Tmppath, "apkdec", "services", "smali", "com", "android", "signkill.smali"))
	checkerr(err)
	ParsingPackageUtils, err := apkengine.Findfile_with_classname("com.android.server.pm.pkg.parsing.ParsingPackageUtils", outputpath)
	checkerr(err)
	fmt.Println("ParsingPackageUtils", ParsingPackageUtils)
	ScanPackageUtils, err := apkengine.Findfile_with_classname("com.android.server.pm.ScanPackageUtils", outputpath)
	checkerr(err)
	fmt.Println("ScanPackageUtils", ScanPackageUtils)
	PackageSessionVerifier, err := apkengine.Findfile_with_classname("com.android.server.pm.PackageSessionVerifier", outputpath)
	checkerr(err)
	fmt.Println("PackageSessionVerifier", PackageSessionVerifier)
	ApexManager, err := apkengine.Findfile_with_classname("com.android.server.pm.ApexManager$ApexManagerImpl", outputpath)
	checkerr(err)
	fmt.Println("ApexManager", ApexManager)
	err = utils.ReplaceStringInFile(ParsingPackageUtils, "Landroid/util/apk/ApkSignatureVerifier;->getMinimumSignatureSchemeVersionForTargetSdk", "Lcom/android/signkill;->getMinimumSignatureSchemeVersionForTargetSdk")
	checkerr(err)
	err = utils.ReplaceStringInFile(ScanPackageUtils, "Landroid/util/apk/ApkSignatureVerifier;->getMinimumSignatureSchemeVersionForTargetSdk", "Lcom/android/signkill;->getMinimumSignatureSchemeVersionForTargetSdk")
	checkerr(err)
	err = utils.ReplaceStringInFile(PackageSessionVerifier, "Landroid/util/apk/ApkSignatureVerifier;->getMinimumSignatureSchemeVersionForTargetSdk", "Lcom/android/signkill;->getMinimumSignatureSchemeVersionForTargetSdk")
	checkerr(err)
	err = utils.ReplaceStringInFile(ApexManager, "Landroid/util/apk/ApkSignatureVerifier;->getMinimumSignatureSchemeVersionForTargetSdk", "Lcom/android/signkill;->getMinimumSignatureSchemeVersionForTargetSdk")
	checkerr(err)
	apkengine.RepackApk(apk)
}
func stage16_patch_desktop() {
	defer Wg.Done()
	fmt.Println("stage 16:patch desktop")
	var apk apkengine.Apkfile
	apk.Apkpath = filepath.Join(Tmppath, "port_images", "product", "priv-app", "MiuiHome", "MiuiHome.apk")
	apk.Pkgname = "MiuiHome"
	apk.Execpath = Execpath
	apkengine.PatchApk_Return_number(apk, "com.miui.home.launcher.DeviceConfig", "calculateHotseatMaxCount", 100)
	apkengine.PatchApk_Return_Boolean(apk, "com.miui.home.launcher.DeviceConfig", "checkHomeIsSupportHotSeatsBlur", true)
	apkengine.PatchApk_Return_Boolean(apk, "com.miui.home.launcher.DeviceConfig", "checkIsRecentsTaskSupportBlurV2", true)
	apkengine.PatchApk_Return_Boolean(apk, "com.miui.home.launcher.common.BlurUtils", "isUseCompleteBlurOnDev", true)
	apkengine.PatchApk_Return_Boolean(apk, "com.miui.home.recents.DimLayer", "isSupportDim", true)
	apkengine.Add_method_after(apk, "com.miui.home.recents.GestureInputHelper", filepath.Join(Execpath, "res", "MiuiHome_patch1.txt"))
	apkengine.Patch_before_funcstart(apk, "com.miui.home.recents.GestureInputHelper", "onTriggerGestureSuccess", filepath.Join(Execpath, "res", "MiuiHome_patch2.txt"), true)
	apkengine.Add_method_after(apk, "com.miui.home.recents.GestureTouchEventTracker", filepath.Join(Execpath, "res", "MiuiHome_patch3.txt"))
	apkengine.Patch_before_funcstart(apk, "com.miui.home.recents.GestureTouchEventTracker", "isUseDockFollowGesture", filepath.Join(Execpath, "res", "MiuiHome_patch4.txt"), true)
	apkengine.RepackApk(apk)
}
func stage17_copy_custsettings() {
	defer Wg.Done()
	fmt.Println("stage 17: move Cust Settings to product")
	utils.CopyFile(filepath.Join(Execpath, "res", "CustSettings.apk"), filepath.Join(Tmppath, "port_images", "product", "priv-app", "CustSettings", "CustSettings.apk"))
}
func stage18_powerkeeper_maxfps() {
	defer Wg.Done()
	fmt.Println("stage 18: disable screen code receive")
	var apk apkengine.Apkfile
	apk.Apkpath = filepath.Join(Tmppath, "port_images", "system", "system", "app", "PowerKeeper", "PowerKeeper.apk")
	apk.Pkgname = "PowerKeeper"
	apk.Execpath = Execpath
	apkengine.PatchApk_Return_number(apk, "com.miui.powerkeeper.feedbackcontrol.ThermalManager", "getDisplayCtrlCode", 0)
	apkengine.RepackApk(apk)
}
func stage19_remove_useless_apps() {
	defer Wg.Done()
	fmt.Println("stage 19: remove useless apps")
	utils.DeleteDirectory(filepath.Join(Tmppath, "port_images", "product", "data-app", "MIUIDuokanReaderPad"))
	utils.DeleteDirectory(filepath.Join(Tmppath, "port_images", "product", "data-app", "MIpayPad_NO_NFC"))
	utils.DeleteDirectory(filepath.Join(Tmppath, "port_images", "product", "data-app", "MIUIYoupin"))
	utils.DeleteDirectory(filepath.Join(Tmppath, "port_images", "product", "data-app", "MiShop"))
	utils.DeleteDirectory(filepath.Join(Tmppath, "port_images", "product", "data-app", "MIUIGameCenterPad"))
	utils.DeleteDirectory(filepath.Join(Tmppath, "port_images", "product", "data-app", "MIUIEmail"))
	utils.DeleteDirectory(filepath.Join(Tmppath, "port_images", "product", "data-app", "BaiduIME"))
	utils.DeleteDirectory(filepath.Join(Tmppath, "port_images", "product", "data-app", "com.iflytek.inputmethod.miui"))
	utils.DeleteDirectory(filepath.Join(Tmppath, "port_images", "product", "data-app", "MIService"))
	utils.DeleteDirectory(filepath.Join(Tmppath, "port_images", "product", "data-app", "Mitukid"))
	utils.DeleteDirectory(filepath.Join(Tmppath, "port_images", "product", "data-app", "MIUIHuanji"))
	utils.DeleteDirectory(filepath.Join(Tmppath, "port_images", "product", "data-app", "MIUIVideoPad"))
	utils.DeleteDirectory(filepath.Join(Tmppath, "port_images", "product", "app", "Updater"))
}

func stage20_upgrade_rootfs_usrimg_prop() {
	defer Wg.Done()
	fmt.Println("stage 20: upgrade rootfs usr prop")
	base_odm_prop := filepath.Join(Tmppath, "base_images", "odm", "etc", "build.prop")
	port_odm_prop := filepath.Join(Tmppath, "port_images", "odm", "etc", "build.prop")
	mslg_version_prop := "ro.vendor.mslg.rootfs.version"
	base_rootfs_version, _ := getAndroidPropValue(base_odm_prop, mslg_version_prop)
	port_rootfs_version, _ := getAndroidPropValue(port_odm_prop, mslg_version_prop) //rootfs-YY.MM.DD.tgz
	layout := "rootfs-06.01.02.tgz"
	if base_rootfs_version == "" {
		fmt.Println("base device does not support mslg V2")
		return
	}
	if port_rootfs_version == "" {
		fmt.Println("base supports mslg v2, but port does not seem to support it.")
		return
	}
	base_rootfs_date, err := time.Parse(layout, extractDate(base_rootfs_version, layout))
	checkerr(err)
	port_rootfs_date, err := time.Parse(layout, extractDate(port_rootfs_version, layout))
	checkerr(err)
	if base_rootfs_date.Before(port_rootfs_date) {
		fmt.Println("base rootfs is outdated,update rootfs version.")
		err = utils.CopyFile(filepath.Join(Tmppath, "port_images", "odm", "etc", "assets", "mslgusrimg"), filepath.Join(Tmppath, "base_images", "odm", "etc", "assets", "mslgusrimg"))
		checkerr(err)
		err = utils.CopyFile(filepath.Join(Tmppath, "port_images", "odm", "etc", "assets", "md5.txt"), filepath.Join(Tmppath, "base_images", "odm", "etc", "assets", "md5.txt"))
		checkerr(err)
		err = utils.CopyFile(filepath.Join(Tmppath, "port_images", "odm", "etc", "assets", port_rootfs_version), filepath.Join(Tmppath, "base_images", "odm", "etc", "assets", port_rootfs_version))
		checkerr(err)
		_ = utils.DeleteFile(filepath.Join(Tmppath, "base_images", "odm", "etc", "assets", base_rootfs_version))
		err = updateAndroidPropValue(base_odm_prop, mslg_version_prop, port_rootfs_version) //注意 打包时候要 使用本机的odm !!!
		checkerr(err)
	} else {
		fmt.Println("base rootfs is newer or equal to port version,no need to update.")
	}
}

func stage21_lowram_device_dkt() {
	defer Wg.Done()
	fmt.Println("stage 21: enable desktop mode for low ram devices")
	var apk apkengine.Apkfile
	apk.Apkpath = filepath.Join(Tmppath, "port_images", "product", "app", "MIUISystemUIPlugin", "MIUISystemUIPlugin.apk")
	apk.Pkgname = "MIUISystemUIPlugin"
	apk.Execpath = Execpath
	apkengine.PatchApk_Return_Boolean(apk, "miui.systemui.quicksettings.MiuiDesktopModeTile", "isAvailable", true)
	apkengine.RepackApk(apk)
}
func stage22_enable_cellular_share() {
	//no need to add defer Wg.Done()
	var apk apkengine.Apkfile
	apk.Apkpath = filepath.Join(Tmppath, "port_images", "product", "overlay", "MiuiFrameworkResOverlay.apk")
	apk.Pkgname = "MiuiFrameworkResOverlay"
	apk.Execpath = Execpath
	apk.Force_unpack_res = true
	apkengine.DecompileApk(apk)
	apkengine.ModifyRes_bool(apk, filepath.Join("values", "bools.xml"), "config_celluar_shared_support", "true")
	apkengine.RepackApk(apk)
}

// 2023-01-27
func main() {
	sysType = runtime.GOOS
	thread = runtime.NumCPU()
	if sysType != "linux" && sysType != "windows" {
		ErrorAndExit("You are running on an unsupported system.")
	}
	flag.StringVar(&basePkg, "base", "", "Original package (zip full ota package)")
	flag.StringVar(&portPkg, "port", "", "Port package (zip full ota package)")
	flag.IntVar(&current_stage, "stage", 0, "In which stage to start(If the program exited unexpectedly,-stage xxx)")
	flag.BoolVar(&skip_thread_limit, "skip_thread_limit", false, "skip program thread check")
	flag.BoolVar(&primary_port, "primary", false, "Only primary port,and no new features will be added")
	flag.BoolVar(&init_debug, "init_debug", false, "open init debug(dangerous!!),not yet implemented")
	flag.BoolVar(&dec_mode, "dec_mode", false, "Start decompile each apk to java code from baserom (use jadx)")
	flag.StringVar(&buildhost, "author", getHostname(), "set author name")
	flag.Parse()
	buildtime = getCurrentTime()
	executable, _ := os.Executable()
	Execpath = filepath.Dir(executable)
	Binpath = filepath.Join(Execpath, "bin", sysType)
	Tmppath = filepath.Join(Execpath, "tmp")
	Outpath = filepath.Join(Execpath, "out", "images")
	Imgextractorpath = filepath.Join(Execpath, "bin", "imgextractor")
	utils.CreateDirectoryIfNotExists(Outpath)
	if thread <= 8 && !skip_thread_limit {
		ErrorAndExit("Too few CPU threads (<=8) , the program may cause problems")
	}
	if dec_mode {
		fmt.Println("===========Welcome Faucet Pad OS Decompiler========")
	} else {
		fmt.Println("===========Welcome Faucet Pad OS Porter============")
	}
	if gitver == "" {
		gitver = "dev_line"
	}
	fmt.Println("gitver=", gitver)
	fmt.Println("BuildHost=", buildhost)
	fmt.Println("BuildTime=", buildtime)
	fmt.Println("OS=" + sysType)
	fmt.Printf("Thread=%d\n", thread)
	fmt.Println("Binpath=" + Binpath)
	fmt.Println("basepkg=" + basePkg)
	if !dec_mode {
		fmt.Println("portpkg=" + portPkg)
	}
	fmt.Println("Execpath=" + Execpath)
	if skip_thread_limit && thread <= 8 {
		fmt.Println("You have enabled the flag of skip_thread_limit and detected that the number of threads is too few.. the program may cause problems")
	}
	if init_debug {
		fmt.Println("init_debug flag is enabled,do not share your package to others.")
	}
	if current_stage != 0 {
		fmt.Printf("program will resume from stage:%d\n", current_stage)
	}
	if current_stage == 0 {
		fmt.Println("========The program will start in 5 seconds========")
		time.Sleep(5 * time.Second)
		fmt.Println("clearing workspace!!")
		if utils.DirectoryExists(Tmppath) {
			fmt.Println("delete tmp dictionary")
			utils.DeleteDirectory(Tmppath)
			utils.DeleteDirectory(filepath.Join(Execpath, "out"))
			utils.CreateDirectoryIfNotExists(Outpath)
		}
		current_stage++
	}
	startTime := time.Now()
	/*
		Download rom :Not yet implemented.
		if strings.HasPrefix(basePkg, "https://") {
			fmt.Println("need to download")
		}
		if strings.HasPrefix(portPkg, "https://") {
			fmt.Println("need to download")
		}*/
	if basePkg == "" || portPkg == "" && !dec_mode {
		ErrorAndExit("Base package or port package is null")
	}
	if basePkg == portPkg {
		ErrorAndExit("Base package or port package is same.")
	}
	if !utils.FileExists(basePkg) || (!utils.FileExists(portPkg) && !dec_mode) {
		ErrorAndExit("Base package or port package not found")
	}
	if current_stage == 1 {
		stage1_unzip()
		current_stage++
	}
	if current_stage == 2 {
		stage2_unpayload()
		current_stage++
	}
	if current_stage == 3 {
		stage3_unparse()
		current_stage++
	}
	if current_stage == 4 && dec_mode {
		fmt.Println("stage 4 :find and decompile apks in base image")
		apks := findAPKs(filepath.Join(Tmppath, "base_images"))
		fmt.Println("Total apks (apk in product system system_ext mi_ext):", len(apks))
		decompile_apks(apks)
		current_stage = 999
	}
	if current_stage == 4 {
		//仅做基础移植
		if primary_port {
			Wg.Add(8)
			go stage4_modify_prop_config()
			go stage5_modify_overlay_config()
			go stage6_modify_displayconfig()
			go stage7_change_device_features()
			go stage8_modify_camera()
			go stage9_add_autoui_adaption()
			go stage10_fix_biometric_face()
			go stage20_upgrade_rootfs_usrimg_prop()
			Wg.Wait()

		} else {
			Wg.Add(18)
			go stage4_modify_prop_config()
			go stage5_modify_overlay_config()
			go stage6_modify_displayconfig()
			go stage7_change_device_features()
			go stage8_modify_camera()
			go stage9_add_autoui_adaption()
			go stage10_fix_biometric_face()
			go stage11_unlock_freeform_settings()
			go stage12_settings_unlock_content_extension()
			go stage13_patch_systemUI()
			go stage14_fix_content_extension()
			go stage15_downgrade_privapp_verification()
			go stage16_patch_desktop()
			go stage17_copy_custsettings()
			go stage18_powerkeeper_maxfps()
			go stage19_remove_useless_apps()
			go stage20_upgrade_rootfs_usrimg_prop()
			go stage21_lowram_device_dkt()
			Wg.Wait()
		}
		current_stage = 99
	}
	if current_stage == 98 {
		fmt.Println("debug....")
	}
	if current_stage == 99 {
		if utils.FileExists(filepath.Join(Execpath, "APKPATCH_ERROR_LOG")) {
			fmt.Println("ERROR occoured when patching apks,pls check APKPATCH_ERROR_LOG")
			current_stage = 999
		} else {
			fmt.Println("stage 99:update FS config and Context and package (EROFS).")
			parts_port := []string{"system", "system_ext", "product", "mi_ext"}
			package_img(parts_port, true)
			parts_base := []string{"odm"}
			package_img(parts_base, false)
			current_stage++
		}
	}
	if current_stage == 100 {
		fmt.Println("stage final: make (uotan) fastbootd flash script and move files")
		utils.WriteTofile(filepath.Join(Execpath, "out", "flashall_fastbootd.txt"), "codename:"+base_device_id)
		parts := []string{"system", "system_ext", "product", "mi_ext", "odm", "vendor", "vendor_dlkm"}
		for _, part := range parts {
			utils.WriteTofile(filepath.Join(Execpath, "out", "flashall_fastbootd.txt"), part)
		}
		fmt.Println("stage final: make (uotan) fastboot flash script")
		utils.WriteTofile(filepath.Join(Execpath, "out", "flashall_fastboot.txt"), "codename:"+base_device_id)
		total, err := utils.FindIMGFiles(filepath.Join(Tmppath, "base_payload"))
		checkerr(err)
		part1 := utils.Finddiff(total, parts)
		checkerr(err)
		for _, part := range part1 {
			err = utils.CopyFile(filepath.Join(Tmppath, "base_payload", part)+".img", filepath.Join(Execpath, "out", "images", part)+".img")
			checkerr(err)
			utils.WriteTofile(filepath.Join(Execpath, "out", "flashall_fastboot.txt"), part)
		}
	}
	elapsedTime := time.Since(startTime)
	elapsedMinutes := elapsedTime.Minutes()
	fmt.Printf("Success !! Elapsed Time %.2f mins\n", elapsedMinutes)
}
