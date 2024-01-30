package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"path/filepath"
	"runtime"
	"bytes"
	"github.com/otiai10/copy"
	"time"
	"bufio"
	"strings"
)

var sysType string
var basePkg string
var portPkg string
var execpath string
var err error
var binpath string
var imgextractorpath string 
var toolpath string
var tmppath string
var thread int
var current_stage int

var base_device_id string 
var port_device_id string
var base_density_v2_prop string
//chatgpt
var formats = [][]interface{}{
	{[]byte{'P', 'K'}, "zip"},
	{[]byte{'O', 'P', 'P', 'O', 'E', 'N', 'C', 'R', 'Y', 'P', 'T', '!'}, "ozip"},
	{[]byte{'7', 'z'}, "7z"},
	{[]byte{0x53, 0xef}, "ext", 1080},
	{[]byte{0x3a, 0xff, 0x26, 0xed}, "sparse"},
	{[]byte{0xe2, 0xe1, 0xf5, 0xe0}, "erofs", 1024},
	{[]byte{'C', 'r', 'A', 'U'}, "payload"},
	{[]byte{'A', 'V', 'B', '0'}, "vbmeta"},
	{[]byte{0xd7, 0xb7, 0xab, 0x1e}, "dtbo"},
	{[]byte{0xd0, 0x0d, 0xfe, 0xed}, "dtb"},
	{[]byte{'M', 'Z'}, "exe"},
	{[]byte{'.', 'E', 'L', 'F'}, "elf"},
	{[]byte{'A', 'N', 'D', 'R', 'O', 'I', 'D', '!'}, "boot"},
	{[]byte{'V', 'N', 'D', 'R', 'B', 'O', 'O', 'T'}, "vendor_boot"},
	{[]byte{'A', 'V', 'B', 'f'}, "avb_foot"},
	{[]byte{'B', 'Z', 'h'}, "bzip2"},
	{[]byte{'C', 'H', 'R', 'O', 'M', 'E', 'O', 'S'}, "chrome"},
	{[]byte{0x1f, 0x8b}, "gzip"},
	{[]byte{0x1f, 0x9e}, "gzip"},
	{[]byte{0x02, 0x21, 0x4c, 0x18}, "lz4_legacy"},
	{[]byte{0x03, 0x21, 0x4c, 0x18}, "lz4"},
	{[]byte{0x04, 0x22, 0x4d, 0x18}, "lz4"},
	{[]byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0x03}, "zopfli"},
	{[]byte{0xfd, '7', 'z', 'X', 'Z'}, "xz"},
	{[]byte{']', 0x00, 0x00, 0x00, 0x04, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, "lzma"},
	{[]byte{0x02, '!', 'L', 0x18}, "lz4_lg"},
	{[]byte{0x89, 'P', 'N', 'G'}, "png"},
	{[]byte{'L', 'O', 'G', 'O', '!', '!', '!', '!'}, "logo"},
}

func ErrorAndExit(msg string) {
	fmt.Println("Error:" + msg)
	if current_stage!=0{
		fmt.Printf("You can add argument -stage %d\n",current_stage)
	}
	os.Exit(0)
}
func fileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return !os.IsNotExist(err)
}
func checkerr(err error) {
	if err != nil {
		ErrorAndExit(err.Error())
	}
}
func ignore_err(err error){
	if err != nil {
		fmt.Println("warning:",err)
	}
}
//chatgpt
func createDirectoryIfNotExists(directoryPath string) error {
	_, err := os.Stat(directoryPath)
	if os.IsNotExist(err) {
		err := os.MkdirAll(directoryPath, os.ModePerm)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return nil
}
//chatgpt
func unzip(source, target string, filesToExtract []string,rename string) error {
	reader, err := zip.OpenReader(source)
	if err != nil {
		return err
	}
	defer reader.Close()

	if err := os.MkdirAll(target, os.ModePerm); err != nil {
		return err
	}
	for _, file := range reader.File {
		if shouldExtract(file.Name, filesToExtract) {
			zippedFile, err := file.Open()
			if err != nil {
				return err
			}
			defer zippedFile.Close()
			targetPath := filepath.Join(target, rename)
			if file.FileInfo().IsDir() {
				if err := os.MkdirAll(targetPath, os.ModePerm); err != nil {
					return err
				}
			} else {
				extractedFile, err := os.Create(targetPath)
				if err != nil {
					return err
				}
				defer extractedFile.Close()
				if _, err := io.Copy(extractedFile, zippedFile); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
//chatgpt
func shouldExtract(fileName string, filesToExtract []string) bool {
	for _, file := range filesToExtract {
		if fileName == file {
			return true
		}
	}
	return false
}
func payloaddump(filename string, dumppath string) error {
	extractPath := filepath.Join(execpath, "tmp", dumppath)
	payloadPath := filepath.Join(execpath, "tmp", filename)
	thread_:=fmt.Sprintf("%d",thread/2)
	fmt.Println("payload->thread",thread_)
	err = runCommand(binpath,"./payload-dumper-go","-c", thread_,"-o", extractPath, payloadPath)
	if err != nil {
		return err
	}
	return nil
}
//注意：dest是目录名
func UnzipPayloadbin(pkg string, dest string, filename string, rename string) error {
	targetDir := filepath.Join(execpath, dest)
	filesToExtract := []string{filename}
	err = unzip(pkg, targetDir, filesToExtract,rename)
	if err != nil {
		return err
	}
	return nil
}
func runCommand(dir string,command string, args ...string) error {
	cmd := exec.Command(command, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = dir

	err := cmd.Run()
	if err != nil {
		return err
	}

	return nil
}
func compare(header []byte, filePath string, number int) bool {
    file, err := os.Open(filePath)
    if err != nil {
        return false
    }
    defer file.Close()

    _, err = file.Seek(int64(number), 0)
    if err != nil {
        return false
    }

    data := make([]byte, len(header))
    _, err = file.Read(data)
    if err != nil {
        return false
    }

    return bytes.Equal(data, header)
}

func checkFormat(filePath string) string {
    for _, f := range formats {
        if len(f) == 2 {
            if compare(f[0].([]byte), filePath, 0) {
                return f[1].(string)
            }
        } else if len(f) == 3 {
            if compare(f[0].([]byte), filePath, f[2].(int)) {
                return f[1].(string)
            }
        }
    }

    return "unknown"
}

func extractimg(imgpath,outputpath string){
	format:=checkFormat(imgpath)
	fmt.Println(imgpath,"format",format)
	if format=="erofs"{
		err=runCommand(binpath,"./extract.erofs","-x","-i",imgpath,"-o",outputpath)
		checkerr(err)
	}else if format=="ext"{
		err=runCommand(imgextractorpath,"python","./imgextractor.py",imgpath,outputpath)
		checkerr(err)
	}else{
		ErrorAndExit("unknown image?"+imgpath)
	}
}
func deleteDirectory(directoryPath string) error {
	err := os.RemoveAll(directoryPath)
	if err != nil {
		return err
	}
	return nil
}

func extract_all_images(parts []string){
	var wg sync.WaitGroup
	for _,part := range parts{
		wg.Add(2)
		go func(p string){
			defer wg.Done()
			imgpath:=filepath.Join(tmppath,"base_payload",p+".img")
			outputpath:=filepath.Join(tmppath,"base_images")	
			extractimg(imgpath,outputpath)
		}(part)	
		go func(p string){
			defer wg.Done()
			imgpath:=filepath.Join(tmppath,"port_payload",p+".img")
			outputpath:=filepath.Join(tmppath,"port_images")	
			extractimg(imgpath,outputpath)
		}(part)	
	}
	wg.Wait()
}
func package_img(parts []string){
	var wg sync.WaitGroup
	for _,imgname := range parts{
		wg.Add(1)
		go func (imgname string){
			defer wg.Done()
			imgpath:=filepath.Join(tmppath,"port_images",imgname)
			fsconfig_path:=filepath.Join(tmppath,"port_images","config",imgname+"_fs_config")	
			context_config_path:=filepath.Join(tmppath,"port_images","config",imgname+"_file_contexts")	
			fmt.Println(imgpath)
			fmt.Println(fsconfig_path)
			err=runCommand(imgextractorpath,"python","./fspatch.py",imgpath,fsconfig_path)
			checkerr(err)
			err=runCommand(imgextractorpath,"python","./contextpatch.py",imgpath,context_config_path)
			checkerr(err)
			currentTime := time.Now()
			unixTimestamp := fmt.Sprintf("%d",currentTime.Unix())
			output_img_path:=filepath.Join(execpath,"out",imgname+".img")
			err=runCommand(binpath,"./mkfs.erofs","-z","lz4hc,8","-T",unixTimestamp,"--mount-point=/"+imgname,"--fs-config-file="+fsconfig_path,"--file-contexts="+context_config_path,output_img_path,imgpath)
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
//chatgpt
func updateAndroidPropValue(propFile, propName, newValue string) error {
	file, err := os.OpenFile(propFile, os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	var lines []string
	var found bool  // 标志是否找到匹配的属性
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
	// 如果没有找到匹配的属性，追加新的属性
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
//chatgpt
func directoryExists(directoryPath string) bool {
	_, err := os.Stat(directoryPath)
	return !os.IsNotExist(err) 
}
func replaceFolder(srcPath, destPath string) error {
	err := os.RemoveAll(destPath)
	if err != nil {
		return err
	}
	err = copy.Copy(srcPath, destPath)
	if err != nil {
		return err
	}
	fmt.Printf("Folder %s replaced with %s\n", srcPath, destPath)
	return nil
}
func replaceFile(srcPath, destPath string) error {
	// 移动或替换文件
	err := os.Rename(srcPath, destPath)
	if err != nil {
		return err
	}
	return nil
}
func copyFile(srcPath, destPath string) error {
	err := copy.Copy(srcPath, destPath)
	if err != nil {
		return err
	}
	fmt.Printf("File %s copied to %s\n", srcPath, destPath)
	return nil
}
func stage1_unzip() {
	fmt.Println("stage 1: unzipping base pkg and port pkg")
	var wg sync.WaitGroup
	wg.Add(1)
	go func(){
		defer wg.Done()
		err = UnzipPayloadbin(basePkg, "tmp", "payload.bin", "base.payload.bin")
		checkerr(err)	
	}()	
	wg.Add(1)
	go func(){
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
	go func(){
		defer wg.Done()
		err = payloaddump("base.payload.bin", "base_payload")
		checkerr(err)
	}()
	wg.Add(1)
	go func(){
		defer wg.Done()
		err = payloaddump("port.payload.bin", "port_payload")
		checkerr(err)
	}()
	wg.Wait()
}
func stage3_unparse(){
	fmt.Println("stage 3: unparse images of super (base port) (system system_ext product mi_ext)")
	parts := []string{"system", "system_ext", "product","mi_ext" }
	extract_all_images(parts)
}
func stage4_modify_prop_config(){
	fmt.Println("stage 4: read configs and modify")
	base_product_prop:=filepath.Join(tmppath,"base_images","product","etc","build.prop")
	base_device_id,err =getAndroidPropValue(base_product_prop,"ro.product.product.name")
	checkerr(err)
	port_product_prop:=filepath.Join(tmppath,"port_images","product","etc","build.prop")
	port_device_id,err =getAndroidPropValue(port_product_prop,"ro.product.product.name")
	checkerr(err)
	fmt.Println("base_device id:",base_device_id)
	fmt.Println("base_device id:",port_device_id)
	port_miext_prop:=filepath.Join(tmppath,"port_images","mi_ext","etc","build.prop")
	fmt.Println("mod:",port_miext_prop)
	err=updateAndroidPropValue(port_miext_prop,"ro.product.mod_device",base_device_id)
	checkerr(err)
	err=updateAndroidPropValue(port_product_prop,"ro.product.product.name",base_device_id)
	checkerr(err)
	base_density_v2_prop,err=getAndroidPropValue(base_product_prop,"persist.miui.density_v2")
	err=updateAndroidPropValue(port_product_prop,"persist.miui.density_v2",base_density_v2_prop)
	checkerr(err)
	err=updateAndroidPropValue(port_product_prop,"ro.sf.lcd_density",base_density_v2_prop)
	ignore_err(err)
	err=updateAndroidPropValue(port_product_prop,"persist.miui.auto_ui_enable","true")
	checkerr(err)
	err=updateAndroidPropValue(port_product_prop,"debug.game.video.speed","true")
	checkerr(err)
	err=updateAndroidPropValue(port_product_prop,"debug.game.video.support","true")
	checkerr(err)
	err=updateAndroidPropValue(port_product_prop,"persist.sys.background_blur_supported","true")
	checkerr(err)
	err=updateAndroidPropValue(port_product_prop,"persist.sys.background_blur_version","2")
	checkerr(err)
}
func stage5_modify_overlay_config(){
	fmt.Println("stage 5: modify AospFrameworkResOverlay MiuiFrameworkResOverlay DevicesAndroidOverlay")
	base_aosp_overlay := filepath.Join(tmppath,"base_images","product","overlay","AospFrameworkResOverlay.apk")
	port_aosp_overlay := filepath.Join(tmppath,"port_images","product","overlay","AospFrameworkResOverlay.apk")
	err=replaceFile(base_aosp_overlay,port_aosp_overlay)
	checkerr(err)
	base_miui_overlay := filepath.Join(tmppath,"base_images","product","overlay","MiuiFrameworkResOverlay.apk")
	port_miui_overlay := filepath.Join(tmppath,"port_images","product","overlay","MiuiFrameworkResOverlay.apk")
	err=replaceFile(base_miui_overlay,port_miui_overlay)
	checkerr(err)
	base_device_overlay := filepath.Join(tmppath,"base_images","product","overlay","DevicesAndroidOverlay.apk")
	port_device_overlay := filepath.Join(tmppath,"port_images","product","overlay","DevicesAndroidOverlay.apk")
	err=replaceFile(base_device_overlay,port_device_overlay)
	checkerr(err)
	base_device1_overlay := filepath.Join(tmppath,"base_images","product","overlay","DevicesOverlay.apk")
	port_device1_overlay := filepath.Join(tmppath,"port_images","product","overlay","DevicesOverlay.apk")
	err=replaceFile(base_device1_overlay,port_device1_overlay)
	checkerr(err)
}
func stage6_modify_displayconfig(){
	fmt.Println("stage 6: replace media and displayid folder")
	base_media := filepath.Join(tmppath,"base_images","product","media")
	port_media := filepath.Join(tmppath,"port_images","product","media")
	err=replaceFolder(base_media,port_media)
	checkerr(err)
	base_display := filepath.Join(tmppath,"base_images","product","etc","displayconfig")
	port_display := filepath.Join(tmppath,"port_images","product","etc","displayconfig")
	err=replaceFolder(base_display,port_display)
	checkerr(err)
}
func stage7_change_device_features(){
	fmt.Println("stage 7:change device_features")
	base_feature := filepath.Join(tmppath,"base_images","product","etc","device_features")
	port_feature := filepath.Join(tmppath,"port_images","product","etc","device_features")
	err=replaceFolder(base_feature,port_feature)
	checkerr(err)
}
func stage8_modify_camera(){
	fmt.Println("stage 8:modify Camera")
	base_cam := filepath.Join(tmppath,"base_images","product","priv-app","MiuiCamera")
	err=deleteDirectory(filepath.Join(tmppath,"port_images","product","MiuiCamera"))
	ignore_err(err)
	port_cam := filepath.Join(tmppath,"port_images","product","app","MiuiCamera")
	err=replaceFolder(base_cam,port_cam)
	checkerr(err)
}
func stage9_add_autoui_adaption(){
	fmt.Println("stage 9: add autoui adaption")
	base_autoui := filepath.Join(tmppath,"base_images","product","etc","autoui_list.xml")
	port_autoui := filepath.Join(tmppath,"port_images","product","etc","autoui_list.xml")
	//port_autoui_folder := filepath.Join(tmppath,"port_images","product","etc")
	if fileExists(base_autoui)||!fileExists(port_autoui){
		fmt.Println("found autoui_list.xml and port file don't have")
		err=copyFile(base_autoui,port_autoui)
		checkerr(err)
	}
}
func stage10_downgrade_mslgrdp(){
	fmt.Println("stage 10:(Temp) Downgrade MSLG app")
	port_mslgrdp_folder := filepath.Join(tmppath,"port_images","product","app","MSLgRdp")
	base_mslgrdp_folder := filepath.Join(tmppath,"base_images","product","app","MSLgRdp")
	base_mslgrdp_app :=filepath.Join(base_mslgrdp_folder,"MSLgRdp.apk")
	if directoryExists(port_mslgrdp_folder)&&fileExists(base_mslgrdp_app){
		fmt.Println("found MSLg app folder and base app mslgrdp exists ->> downgrade !")
		replaceFolder(base_mslgrdp_folder,port_mslgrdp_folder)
	}
}
//2023-01-27
func main() {
	sysType = runtime.GOOS
	thread =runtime.NumCPU()
	startTime := time.Now()

	if sysType != "linux" && sysType != "windows" {
		ErrorAndExit("You are running on an unsupported system.")
	}

	flag.StringVar(&basePkg, "base", "", "Original package (zip full ota package)")
	flag.StringVar(&portPkg, "port", "", "Port package (zip full ota package)")
	flag.StringVar(&execpath, "exec", "", "(Development Options)Point where is workpath")
	flag.IntVar(&current_stage,"stage",0,"In which stage to start(If the program exited unexpectedly,-stage xxx)")
	flag.Parse()
	executable, _ := os.Executable()
	execpath=filepath.Dir(executable)
	binpath = filepath.Join(execpath, "bin", sysType)
	toolpath = filepath.Join(execpath,"bin")
	tmppath = filepath.Join(execpath,"tmp")
	imgextractorpath = filepath.Join(toolpath,"imgextractor")
	createDirectoryIfNotExists(filepath.Join(execpath,"out"))
	if basePkg == "" || portPkg == "" {
		ErrorAndExit("Base package or port package is null")
	}
	if basePkg == portPkg {
		ErrorAndExit("Base package or port package is same?")
	}
	if !fileExists(basePkg) || !fileExists(portPkg) {
		ErrorAndExit("Base package or port package not found")
	}
	if thread<=8{
		ErrorAndExit("Too few CPU threads (<=8) , the program may cause problems")
	}
	fmt.Println("===========Welcome Faucet Pad OS Porter============")
	fmt.Println("OS="+ sysType)
	fmt.Printf("Thread=%d\n",thread)
	fmt.Println("binpath="+ binpath)
	fmt.Println("basepkg="+ basePkg)
	fmt.Println("portpkg="+ portPkg)
	fmt.Println("execpath="+execpath)
	if current_stage!=0{
		fmt.Printf("program will resume from stage:%d\n",current_stage)
	}
	fmt.Println("========The program will start in 5 seconds========")
	time.Sleep(5 * time.Second)
	if current_stage==0{
		fmt.Println("(Not yet implemented!) clearing workspace!!")
		current_stage++
	}
	if current_stage==1{
		stage1_unzip()
		current_stage++
	}
	if current_stage==2{
		stage2_unpayload()
		current_stage++
	}
	if current_stage==3{
		stage3_unparse()
		current_stage++
	}
	if current_stage==4{
		stage4_modify_prop_config()
		current_stage++
	}
	if current_stage==5{
		stage5_modify_overlay_config()
		current_stage++
	}
	if current_stage==6{
		stage6_modify_displayconfig()
		current_stage++
	}
	if current_stage==7{
		stage7_change_device_features()
		current_stage++
	}
	if current_stage==8{
		stage8_modify_camera()
		current_stage++
	}
	if current_stage==9{
		stage9_add_autoui_adaption()
		current_stage++
	}
	if current_stage==10{
		stage10_downgrade_mslgrdp()
		current_stage++
	}
	if current_stage==11{
		fmt.Println("stage ?:update FS config and Context and package (EROFS).")
		parts := []string{"system","system_ext","product","mi_ext"}
		package_img(parts)
	}
	elapsedTime := time.Since(startTime)
	elapsedMinutes := elapsedTime.Minutes()
	fmt.Printf("Success !! Elapsed Time %.2f mins", elapsedMinutes)
}