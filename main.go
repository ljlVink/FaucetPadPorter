package main

import (
	"bufio"
	"encoding/xml"
	"faucetpadporter/smaliengine"
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
		wg.Add(2)
		go func(p string) {
			defer wg.Done()
			imgpath := filepath.Join(Tmppath, "base_payload", p+".img")
			outputpath := filepath.Join(Tmppath, "base_images")
			extractimg(imgpath, outputpath)
		}(part)
		go func(p string) {
			defer wg.Done()
			imgpath := filepath.Join(Tmppath, "port_payload", p+".img")
			outputpath := filepath.Join(Tmppath, "port_images")
			extractimg(imgpath, outputpath)
		}(part)
	}
	wg.Wait()
}
func package_img(parts []string) {
	var wg sync.WaitGroup
	for _, imgname := range parts {
		wg.Add(1)
		go func(imgname string) {
			defer wg.Done()
			imgpath := filepath.Join(Tmppath, "port_images", imgname)
			fsconfig_path := filepath.Join(Tmppath, "port_images", "config", imgname+"_fs_config")
			context_config_path := filepath.Join(Tmppath, "port_images", "config", imgname+"_file_contexts")
			fmt.Println(imgpath)
			fmt.Println(fsconfig_path)
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

func stage1_unzip() {
	fmt.Println("stage 1: unzipping base pkg and port pkg")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err = UnzipPayloadbin(basePkg, "tmp", "payload.bin", "base.payload.bin")
		checkerr(err)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		err = UnzipPayloadbin(portPkg, "tmp", "payload.bin", "port.payload.bin")
		checkerr(err)
	}()
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
	wg.Add(1)
	go func() {
		defer wg.Done()
		err = payloaddump("port.payload.bin", "port_payload")
		checkerr(err)
	}()
	wg.Wait()
}
func stage3_unparse() {
	fmt.Println("stage 3: unparse images of super (base port) (system system_ext product mi_ext)")
	utils.CreateDirectoryIfNotExists(filepath.Join(Execpath, "tmp", "port_images", "config"))
	utils.CreateDirectoryIfNotExists(filepath.Join(Execpath, "tmp", "base_images", "config"))
	parts := []string{"system", "system_ext", "product", "mi_ext"}
	extract_all_images(parts)
}
func stage4_modify_prop_config() {
	defer Wg.Done()
	fmt.Println("stage 4: read configs and modify")
	base_product_prop := filepath.Join(Tmppath, "base_images", "product", "etc", "build.prop")
	base_device_id, err = getAndroidPropValue(base_product_prop, "ro.product.product.name")
	checkerr(err)
	port_product_prop := filepath.Join(Tmppath, "port_images", "product", "etc", "build.prop")
	port_miext_prop := filepath.Join(Tmppath, "port_images", "mi_ext", "etc", "build.prop")
	port_device_id, err = getAndroidPropValue(port_product_prop, "ro.product.product.name")
	checkerr(err)
	fmt.Println("base_device id:", base_device_id)
	fmt.Println("base_device id:", port_device_id)

	fmt.Println("mod:", port_miext_prop)
	err = updateAndroidPropValue(port_miext_prop, "ro.product.mod_device", base_device_id)
	checkerr(err)
	err = updateAndroidPropValue(port_product_prop, "ro.product.product.name", base_device_id)
	checkerr(err)
	base_density_v2_prop, err = getAndroidPropValue(base_product_prop, "persist.miui.density_v2")
	err = updateAndroidPropValue(port_product_prop, "persist.miui.density_v2", base_density_v2_prop)
	checkerr(err)
	err = updateAndroidPropValue(port_product_prop, "ro.sf.lcd_density", base_density_v2_prop)
	ignore_err(err)
	err = updateAndroidPropValue(port_product_prop, "persist.miui.auto_ui_enable", "true")
	checkerr(err)
	err = updateAndroidPropValue(port_product_prop, "debug.game.video.speed", "true")
	checkerr(err)
	err = updateAndroidPropValue(port_product_prop, "debug.game.video.support", "true")
	checkerr(err)
	err = updateAndroidPropValue(port_product_prop, "persist.sys.background_blur_supported", "true")
	checkerr(err)
	err = updateAndroidPropValue(port_product_prop, "persist.sys.background_blur_version", "2")
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
		fmt.Println("warn:")
	}
}
func stage10_downgrade_mslgrdp() {
	defer Wg.Done()
	fmt.Println("stage 10: Downgrade MSLG app")
	port_mslgrdp_folder := filepath.Join(Tmppath, "port_images", "product", "app", "MSLgRdp")
	base_mslgrdp_folder := filepath.Join(Tmppath, "base_images", "product", "app", "MSLgRdp")
	base_mslgrdp_app := filepath.Join(base_mslgrdp_folder, "MSLgRdp.apk")
	if utils.DirectoryExists(port_mslgrdp_folder) && utils.FileExists(base_mslgrdp_app) {
		fmt.Println("found MSLg app folder and base app mslgrdp exists ->> downgrade !")
		utils.ReplaceFolder(base_mslgrdp_folder, port_mslgrdp_folder)
	}
}
func stage11_unlock_freeform_settings() {
	defer Wg.Done()
	fmt.Println("stage 11: Unlock freeform settings")
	var apk smaliengine.Apkfile
	apk.Apkpath = filepath.Join(Tmppath, "port_images", "system_ext", "framework", "miui-services.jar")
	apk.Pkgname = "miui_services"
	apk.Execpath = Execpath
	apk.Need_api_29 = true
	smaliengine.PatchApk_Return_number(apk, "com.android.server.wm.MiuiFreeFormStackDisplayStrategy", "getMaxMiuiFreeFormStackCount", 256)
	smaliengine.RepackApk(apk)
}
func stage12_settings_unlock_content_extension() {
	defer Wg.Done()
	fmt.Println("stage 12: Unlock Content Extension(Settings)")
	var apk smaliengine.Apkfile
	apk.Apkpath = filepath.Join(Tmppath, "port_images", "system_ext", "priv-app", "Settings", "Settings.apk")
	apk.Pkgname = "Settings"
	apk.Execpath = Execpath
	smaliengine.PatchApk_Return_Boolean(apk, "com.android.settings.utils.SettingsFeatures", "isNeedRemoveContentExtension", false)
	smaliengine.PatchApk_Return_Boolean(apk, "com.android.settings.utils.SettingsFeatures", "shouldShowAutoUIModeSetting", true)
	smaliengine.PatchApk_Return_Boolean(apk, "com.android.settings.utils.SettingsFeatures", "showHighRefreshPreference", true)
	lines,err:=utils.ReadLinesFromFile(filepath.Join(Execpath,"Settings_patch1.txt"))
	checkerr(err)
	//设置statusbar图标数量
	smaliengine.PatchApk_Return_and_patch_line(apk,"com.android.settings.NotificationStatusBarSettings","setupShowNotificationIconCount",lines)
	smaliengine.RepackApk(apk)
}
func stage13_patch_systemUI() {
	defer Wg.Done()
	fmt.Println("stage 13: Patch systemUI")
	var apk smaliengine.Apkfile
	apk.Apkpath = filepath.Join(Tmppath, "port_images", "system_ext", "priv-app", "MiuiSystemUI", "MiuiSystemUI.apk")
	apk.Pkgname = "MiuiSystemUI"
	apk.Execpath = Execpath
	smaliengine.Add_method_after(apk, "com.android.wm.shell.miuimultiwinswitch.miuiwindowdecor.MiuiDotView", filepath.Join(Execpath, "systemUI_patch1.txt"))
	smaliengine.Patch_before_funcstart(apk, "com.android.wm.shell.miuimultiwinswitch.miuiwindowdecor.MiuiDotView", "onDraw", filepath.Join(Execpath, "systemUI_patch2.txt"), false)
	smaliengine.Add_method_after(apk, "com.android.wm.shell.miuifreeform.MiuiInfinityModeTaskRepository", filepath.Join(Execpath, "systemUI_patch4.txt"))
	smaliengine.Patch_before_funcstart(apk, "com.android.wm.shell.miuifreeform.MiuiInfinityModeTaskRepository", "findTopDraggableFullscreenTaskInfo", filepath.Join(Execpath, "systemUI_patch3.txt"), true)
	smaliengine.Add_method_after(apk, "com.android.systemui.navigationbar.gestural.NavigationHandle", filepath.Join(Execpath, "systemUI_patch5.txt"))
	smaliengine.Patch_before_funcstart(apk, "com.android.systemui.navigationbar.gestural.NavigationHandle", "onDraw", filepath.Join(Execpath, "systemUI_patch6.txt"), false)
	smaliengine.RepackApk(apk)
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
	utils.CopyFile(filepath.Join(Execpath, "MIUIContentExtension.apk"), filepath.Join(Tmppath, "port_images", "product", "priv-app", "MIUIContentExtension", "MIUIContentExtension.apk"))
}
func stage15_downgrade_privapp_verification() {
	defer Wg.Done()
	fmt.Println("stage 15:downgrade priv-app verification")
	var apk smaliengine.Apkfile
	apk.Apkpath = filepath.Join(Tmppath, "port_images", "system", "system", "framework", "services.jar")
	apk.Pkgname = "services"
	apk.Execpath = Execpath
	outputpath, err := smaliengine.DecompileApk(apk)
	checkerr(err)
	err = utils.CopyFile(filepath.Join(Execpath, "signkill.smali"), filepath.Join(Tmppath, "apkdec", "services", "smali", "com", "android", "signkill.smali"))
	checkerr(err)
	ParsingPackageUtils, err := smaliengine.Findfile_with_classname("com.android.server.pm.pkg.parsing.ParsingPackageUtils", outputpath)
	checkerr(err)
	fmt.Println("ParsingPackageUtils", ParsingPackageUtils)
	ScanPackageUtils, err := smaliengine.Findfile_with_classname("com.android.server.pm.ScanPackageUtils", outputpath)
	checkerr(err)
	fmt.Println("ScanPackageUtils", ScanPackageUtils)
	PackageSessionVerifier, err := smaliengine.Findfile_with_classname("com.android.server.pm.PackageSessionVerifier", outputpath)
	checkerr(err)
	fmt.Println("PackageSessionVerifier", PackageSessionVerifier)
	ApexManager, err := smaliengine.Findfile_with_classname("com.android.server.pm.ApexManager$ApexManagerImpl", outputpath)
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
	smaliengine.RepackApk(apk)
}
func stage16_patch_desktop() {
	defer Wg.Done()
	fmt.Println("stage 16:fix desktop")
	var apk smaliengine.Apkfile
	apk.Apkpath = filepath.Join(Tmppath, "port_images", "product", "priv-app", "MiuiHome", "MiuiHome.apk")
	apk.Pkgname = "MiuiHome"
	apk.Execpath = Execpath
	smaliengine.PatchApk_Return_number(apk, "com.miui.home.launcher.DeviceConfig", "calculateHotseatMaxCount", 100)
	smaliengine.PatchApk_Return_Boolean(apk, "com.miui.home.launcher.DeviceConfig", "checkHomeIsSupportHotSeatsBlur", true)
	smaliengine.PatchApk_Return_Boolean(apk, "com.miui.home.launcher.DeviceConfig", "checkIsRecentsTaskSupportBlurV2", true)
	smaliengine.PatchApk_Return_Boolean(apk, "com.miui.home.launcher.common.BlurUtils", "isUseCompleteBlurOnDev", true)
	smaliengine.PatchApk_Return_Boolean(apk, "com.miui.home.recents.DimLayer", "isSupportDim", true)
	smaliengine.Add_method_after(apk, "com.miui.home.recents.GestureInputHelper", filepath.Join(Execpath, "MiuiHome_patch1.txt"))
	smaliengine.Patch_before_funcstart(apk, "com.miui.home.recents.GestureInputHelper", "onTriggerGestureSuccess", filepath.Join(Execpath, "MiuiHome_patch2.txt"), true)
	smaliengine.RepackApk(apk)
}
func stage17_copy_custsettings() {
	defer Wg.Done()
	fmt.Println("stage 17: move Cust Settings to product")
	utils.CopyFile(filepath.Join(Execpath, "CustSettings.apk"), filepath.Join(Tmppath, "port_images", "product", "priv-app", "CustSettings", "CustSettings.apk"))
}
func stage18_powerkeeper_maxfps() {
	defer Wg.Done()
	fmt.Println("stage 18: disable screen code receive")
	var apk smaliengine.Apkfile
	apk.Apkpath = filepath.Join(Tmppath, "port_images", "system","system", "app", "PowerKeeper", "PowerKeeper.apk")
	apk.Pkgname = "PowerKeeper"
	apk.Execpath = Execpath
	smaliengine.PatchApk_Return_number(apk,"com.miui.powerkeeper.feedbackcontrol.ThermalManager","getDisplayCtrlCode",0)
	smaliengine.RepackApk(apk)
}
func stage19_remove_useless_apps(){
	defer Wg.Done()
	err=utils.DeleteDirectory(filepath.Join(Tmppath,"port_images","product","data-app","MIUIDuokanReaderPad"))
	checkerr(err)
	err=utils.DeleteDirectory(filepath.Join(Tmppath,"port_images","product","data-app","MIpayPad_NO_NFC"))	
	checkerr(err)
	err=utils.DeleteDirectory(filepath.Join(Tmppath,"port_images","product","data-app","MIUIYoupin"))
	checkerr(err)
	err=utils.DeleteDirectory(filepath.Join(Tmppath,"port_images","product","data-app","MiShop"))
	checkerr(err)
	err=utils.DeleteDirectory(filepath.Join(Tmppath,"port_images","product","data-app","MIUIGameCenterPad"))
	checkerr(err)
	err=utils.DeleteDirectory(filepath.Join(Tmppath,"port_images","product","data-app","MIUIEmail"))
	checkerr(err)
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
	flag.Parse()
	executable, _ := os.Executable()
	Execpath = filepath.Dir(executable)
	Binpath = filepath.Join(Execpath, "bin", sysType)
	Tmppath = filepath.Join(Execpath, "tmp")
	Outpath = filepath.Join(Execpath, "out")
	Imgextractorpath = filepath.Join(Execpath, "bin", "imgextractor")
	utils.CreateDirectoryIfNotExists(Outpath)
	if basePkg == "" || portPkg == "" {
		ErrorAndExit("Base package or port package is null")
	}
	if basePkg == portPkg {
		ErrorAndExit("Base package or port package is same.")
	}
	if !utils.FileExists(basePkg) || !utils.FileExists(portPkg) {
		ErrorAndExit("Base package or port package not found")
	}
	if thread <= 8 {
		ErrorAndExit("Too few CPU threads (<=8) , the program may cause problems")
	}
	fmt.Println("===========Welcome Faucet Pad OS Porter============")
	fmt.Println("OS=" + sysType)
	fmt.Printf("Thread=%d\n", thread)
	fmt.Println("Binpath=" + Binpath)
	fmt.Println("basepkg=" + basePkg)
	fmt.Println("portpkg=" + portPkg)
	fmt.Println("Execpath=" + Execpath)
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
			utils.DeleteDirectory(Outpath)
			utils.CreateDirectoryIfNotExists(Outpath)
		}
		current_stage++
	}
	startTime := time.Now()
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
	if current_stage == 4 {
		Wg.Add(16)
		go stage4_modify_prop_config()
		go stage5_modify_overlay_config()
		go stage6_modify_displayconfig()
		go stage7_change_device_features()
		go stage8_modify_camera()
		go stage9_add_autoui_adaption()
		go stage10_downgrade_mslgrdp()
		go stage11_unlock_freeform_settings()
		go stage12_settings_unlock_content_extension()
		go stage13_patch_systemUI()
		go stage14_fix_content_extension()
		go stage15_downgrade_privapp_verification()
		go stage16_patch_desktop()
		go stage17_copy_custsettings()
		go stage18_powerkeeper_maxfps()
		go stage19_remove_useless_apps()
		Wg.Wait()
		current_stage=99
	}

	if current_stage == 99 {
		fmt.Println("stage 99:update FS config and Context and package (EROFS).")
		parts := []string{"system","system_ext","product","mi_ext"}
		package_img(parts)
	}
	elapsedTime := time.Since(startTime)
	elapsedMinutes := elapsedTime.Minutes()
	fmt.Printf("Success !! Elapsed Time %.2f mins\n", elapsedMinutes)
}
