package apkengine

import (
	"bufio"
	"faucetpadporter/utils"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)
type Apkfile struct{
	Apkpath string
	Pkgname string
	Execpath string
	Need_api_29 bool 
	Use_apkeditor bool  //使用这个选项时一般需要处理resource的内容
}
func checkerr(err error) {
	if err != nil {
		fmt.Println(err)
		utils.WriteTofile("APKPATCH_ERROR_LOG",err.Error())
	}
}
func removeLines(filePath string, startLine, endLine int) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	lines := make([]string, 0)
	lineNumber := 1
	for scanner.Scan() {
		if lineNumber < startLine || lineNumber > endLine {
			lines = append(lines, scanner.Text())
		}
		lineNumber++
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	outputFile, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer outputFile.Close()
	writer := bufio.NewWriter(outputFile)
	for _, line := range lines {
		_, err := fmt.Fprintln(writer, line)
		if err != nil {
			return err
		}
	}
	return writer.Flush()
}
func insertLinesAfter(filePath string, targetLine int, linesToInsert []string,renamelocals bool) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	lines := make([]string, 0)
	lineNumber := 1
	if renamelocals{
		for scanner.Scan() {
			if lineNumber == targetLine {
				lines = append(lines, linesToInsert...)
			}else{
				lines = append(lines, scanner.Text())
			}
			lineNumber++
		}	
	}else{
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
			if lineNumber == targetLine {
				lines = append(lines, linesToInsert...)
			}
			lineNumber++
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	outputFile, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer outputFile.Close()
	writer := bufio.NewWriter(outputFile)
	for _, line := range lines {
		_, err := fmt.Fprintln(writer, line)
		if err != nil {
			return err
		}
	}
	return writer.Flush()
}
func PatchApk_Return_number(apk Apkfile,classname string,funcname string,changeTo int){
	linesToInsert := []string{".locals 5", "const/16 v0, "+intToHexadecimal(changeTo), "return v0"}
	PatchApk_Return_and_patch_line(apk,classname,funcname,linesToInsert)
}
func PatchApk_Return_Boolean(apk Apkfile,classname,funcname string,changeTo bool){
	if changeTo{
		linesToInsert := []string{".locals 1", "const/4 v0, 0x1", "return v0"}
		PatchApk_Return_and_patch_line(apk,classname,funcname,linesToInsert)
	}else{
		linesToInsert := []string{".locals 1", "const/4 v0, 0x0", "return v0"}
		PatchApk_Return_and_patch_line(apk,classname,funcname,linesToInsert)
	}
}

func replaceConst4(newHexValue string, smaliCode string) (string, error) {
	re := regexp.MustCompile(`const/4 (\w+), 0x[0-9a-fA-F]+`)
	newSmaliCode := re.ReplaceAllString(smaliCode, fmt.Sprintf("const/16 $1, %s", newHexValue))
	if newSmaliCode == smaliCode {
		return "", fmt.Errorf("not found const/4")
	}
	return newSmaliCode, nil
}
func Add_method_after(apk Apkfile,classname,patch_file_path string){
	output_path,err:=DecompileApk(apk)
	checkerr(err)
	class_name_path,err1:=Findfile_with_classname(classname,output_path)
	checkerr(err1)
	fileA, err := os.Open(patch_file_path)
	checkerr(err)
	defer fileA.Close()
	fileB, err := os.OpenFile(class_name_path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
	checkerr(err)
	defer fileB.Close()
	_, err = io.Copy(fileB, fileA)
	if err != nil {
		panic(err)
	}
}
func Patch_before_funcstart(apk Apkfile,classname,funcname string,patch_file_path string,isNeedtoModifylocals bool){
	output_path,err:=DecompileApk(apk)
	checkerr(err)
	class_name_path,err1:=Findfile_with_classname(classname,output_path)
	checkerr(err1)
	f, err := os.Open(class_name_path)
	checkerr(err)
	defer f.Close()
	pattern := `\.method.*`+funcname
	re := regexp.MustCompile(pattern)
	scanner := bufio.NewScanner(f)
	lineNumber := 0
	for scanner.Scan() {
		line := scanner.Text()
		lineNumber++
		if re.MatchString(line){
			break
		}
	}

	fmt.Println("get func line",lineNumber)
	fmt.Println("should insert from",lineNumber+1)//跳过寄存器
	if lineNumber<=10{
		checkerr(fmt.Errorf("not found?"))
	}
	lines,err:=utils.ReadLinesFromFile(patch_file_path)
	checkerr(err)
	insertLinesAfter(class_name_path,lineNumber+1,lines,isNeedtoModifylocals)
}

func PatchApk_Fix_init_vars(apk Apkfile,classname,pattern string,changeTo int){
	output_path,err:=DecompileApk(apk)
	checkerr(err)
	class_name_path,err1:=Findfile_with_classname(classname,output_path)
	checkerr(err1)
	f, err := os.Open(class_name_path)
	checkerr(err)
	defer f.Close()
	re := regexp.MustCompile(pattern)
	re1 := regexp.MustCompile(`const/4`)
	scanner := bufio.NewScanner(f)
	last_const_v4_line:=0
	lineNumber := 0
	const_4_line:=""
	for scanner.Scan() {
		line := scanner.Text()
		lineNumber++
		if re1.MatchString(line){
			last_const_v4_line=lineNumber
			const_4_line=line
		}
		if re.MatchString(line){
			break
		}
	}
	fmt.Println("class_name_path",class_name_path)
	fmt.Println("const_4_line",const_4_line)
	rep,err:=replaceConst4(intToHexadecimal(changeTo),const_4_line)
	checkerr(err)
	fmt.Println("fixed const_4_line",rep)
	fmt.Println("last_const_v4_line:",last_const_v4_line)
	fmt.Println("linenumber:",lineNumber)
	err=removeLines(class_name_path,last_const_v4_line,last_const_v4_line)
	checkerr(err)
	err=insertLinesAfter(class_name_path,last_const_v4_line,[]string{rep},false)
	checkerr(err)
}
func PatchApk_Return_and_patch_line(apk Apkfile,classname,funcname string,lines []string){
	output_path,err:=DecompileApk(apk)
	checkerr(err)
	class_name_path,err1:=Findfile_with_classname(classname,output_path)
	checkerr(err1)
	f, err := os.Open(class_name_path)
	checkerr(err)
	defer f.Close()
	pattern := `\.method.*`+funcname
	re := regexp.MustCompile(pattern)
	pattern1 := `\.end method\s*$`
	re1 := regexp.MustCompile(pattern1)
	scanner := bufio.NewScanner(f)
	lineNumber := 0
	flag :=false
	func_start:=0
	func_end:=0
	for scanner.Scan() {
		line := scanner.Text()
		lineNumber++
		if !flag && re.MatchString(line){
			flag=true
			func_start=lineNumber
		}
		if flag && re1.MatchString(line){
			func_end=lineNumber
			break
		}
	}
	should_remove_from:=func_start+1
	should_remove_to:=func_end-1
	fmt.Println("shouldremove from:",should_remove_from)
	fmt.Println("shouldremove to:",should_remove_to)
	err=removeLines(class_name_path,should_remove_from,should_remove_to)
	checkerr(err)
	err=insertLinesAfter(class_name_path,func_start,lines,false)
	checkerr(err)
}
func intToHexadecimal(n int) string {
	hexStr := strconv.FormatInt(int64(n), 16)
	result := "0x" + hexStr
	return result
}
//java -jar ./apktool.jar b filename -o apkpath -c
func RepackApk(apk Apkfile){
	execpath:=apk.Execpath
	apktoolpath:=filepath.Join(execpath,"bin","apktool")
	apk_smali_path:=filepath.Join(execpath,"tmp","apkdec",apk.Pkgname)
	if apk.Use_apkeditor{
		err:=utils.RunCommand(apktoolpath,"java","-jar","./APKEditor-1.3.5.jar","b","-i",apk_smali_path,"-o",apk.Apkpath+".1") //写回 (apkeditor)
		checkerr(err)
		//对齐，否则apk会报错
		err=utils.RunCommand(execpath,"zipalign","-p","-f","-v","4",apk.Apkpath+".1",apk.Apkpath)
		checkerr(err)
		utils.DeleteFile(apk.Apkpath+".1")
		return
	}
	if !apk.Need_api_29{
		//输出到xxx.apk.1,对齐之后再输出到源文件并删除,对于apk标签有need_api29的时候那玩意大概率是个系统框架的jar，不需要对齐
		err:=utils.RunCommand(apktoolpath,"java","-jar","./apktool.jar","b",apk_smali_path,"-o",apk.Apkpath+".1","-c") //写回
		if err!=nil{
			panic(err)
		}
		//对齐，否则apk会报错
		err=utils.RunCommand(execpath,"zipalign","-p","-f","-v","4",apk.Apkpath+".1",apk.Apkpath)
		checkerr(err)
		utils.DeleteFile(apk.Apkpath+".1")
	}else{
		err:=utils.RunCommand(apktoolpath,"java","-jar","./apktool.jar","b",apk_smali_path,"-o",apk.Apkpath,"-api","34") //写回
		checkerr(err)
	}

}
//类名，apk位置
func Findfile_with_classname(classname,output_path string)(class_path_file string,err error){
	if utils.FileExists(filepath.Join(output_path,"path-map.json")){
		//使用了apkeditor解包
		class_path:=filepath.Join(output_path,"smali","classes")
		resultArray := strings.Split(classname, ".")
		for _, str := range resultArray {
			class_path=filepath.Join(class_path,str)
		}
		class_path+=".smali"
		if utils.FileExists(class_path) {
			return class_path,nil
		}else{
			//可能存在classes2345
			for i := 2; ; i++ {
				dexPath := fmt.Sprintf("classes%d", i)
				currentClassPath := filepath.Join(output_path,"smali", dexPath)
				for _, str := range resultArray {
					currentClassPath = filepath.Join(currentClassPath, str)
				}
				currentClassPath += ".smali"
				fmt.Println(currentClassPath)
				if utils.FileExists(currentClassPath) {
					return currentClassPath, nil
				}
				nextDexPath := fmt.Sprintf("classes%d", i+1)
				nextDexPath = filepath.Join(output_path, nextDexPath)
				if !utils.DirectoryExists(nextDexPath) {
					break
				}
			}
			return "", fmt.Errorf("(apkeditor)not found file for class %s", classname)
		}
	}else{
		class_path:=filepath.Join(output_path,"smali")
		resultArray := strings.Split(classname, ".")
		for _, str := range resultArray {
			class_path=filepath.Join(class_path,str)
		}
		class_path+=".smali"
		if utils.FileExists(class_path) {
			return class_path,nil
		}else{
			//可能存在smali_classes12345
			for i := 1; ; i++ {
				dexPath := fmt.Sprintf("smali_classes%d", i)
				currentClassPath := filepath.Join(output_path, dexPath)
				for _, str := range resultArray {
					currentClassPath = filepath.Join(currentClassPath, str)
				}
		
				currentClassPath += ".smali"
				if utils.FileExists(currentClassPath) {
					return currentClassPath, nil
				}
				nextDexPath := fmt.Sprintf("smali_classes%d", i+1)
				nextDexPath = filepath.Join(output_path, nextDexPath)
				if !utils.DirectoryExists(nextDexPath) {
					break
				}
			}
			return "", fmt.Errorf("not found file for class %s", classname)
		
		}
	
	}
}
func DecompileApk(apk Apkfile)(outputpath string,error error){
	execpath:=apk.Execpath
	if !apk.Use_apkeditor{
		apktoolpath:=filepath.Join(execpath,"bin","apktool")
		pkgname:=apk.Pkgname
		output_path:=filepath.Join(execpath,"tmp","apkdec",pkgname)
		if utils.DirectoryExists(output_path){
			return output_path,nil //already exists!
		}
		fmt.Println(apk.Apkpath)
		err:=utils.RunCommand(apktoolpath,"java","-jar","./apktool.jar","-r","-f","d",apk.Apkpath,"-o",output_path)
		return output_path,err
	}else{
		apktoolpath:=filepath.Join(execpath,"bin","apktool")
		pkgname:=apk.Pkgname
		output_path:=filepath.Join(execpath,"tmp","apkdec",pkgname)
		if utils.DirectoryExists(output_path){
			return output_path,nil //already exists!
		}
		fmt.Println(apk.Apkpath)
		err:=utils.RunCommand(apktoolpath,"java","-jar","./APKEditor-1.3.5.jar","d","-i",apk.Apkpath,"-o",output_path)
		return output_path,err
	}
}