package apkengine

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type StringArray struct {
	XMLName xml.Name `xml:"string-array"`
	Name    string   `xml:"name,attr"`
	Items   []string `xml:"item"`
}

type Resources struct {
	XMLName  xml.Name     `xml:"resources"`
	Arrays   []StringArray `xml:"string-array"`
}

//修改xml文件，目前默认在res目录下
func ModifyRes_stringArray(apk Apkfile,filename,targetNodeName string ,newData[]string){
	if !apk.Use_apkeditor{
		checkerr(fmt.Errorf("error!! this apk is not decomplied by apkeditor! (check apk.Use_apkeditor=true?)"))
	}
	DecompileApk(apk)
	realpath:=filepath.Join(apk.Apkpath,"resources","package_1","res",filename)
	fmt.Println("realpath->>>",realpath)
	modifyXMLFile(realpath,targetNodeName,newData)
}

func modifyXMLFile(filename, targetNodeName string, newData []string) error {
	xmlFile, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("open file fail: %w", err)
	}
	defer xmlFile.Close()
	byteValue, err := io.ReadAll(xmlFile)
	if err != nil {
		return fmt.Errorf("read file error: %w", err)
	}
	var resources Resources
	err = xml.Unmarshal(byteValue, &resources)
	if err != nil {
		return fmt.Errorf("error occur when unmarshal xml: %w", err)
	}
	found := false
	for i, array := range resources.Arrays {
		fmt.Println(array.Name)
		if array.Name == targetNodeName {
			resources.Arrays[i].Items = newData
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("node not found ?: %s", targetNodeName)
	}
	outputXML, err := xml.MarshalIndent(resources, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}
	err = os.WriteFile(filename, outputXML, 0644)
	if err != nil {
		return fmt.Errorf("writing file error: %w", err)
	}
	return nil
}
