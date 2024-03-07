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
type BoolArray struct {
	XMLName xml.Name `xml:"bool"`
	Name    string   `xml:"name,attr"`
	Flags  string 	`xml:",chardata"`
}

type Resources_str struct {
	XMLName xml.Name      `xml:"resources"`
	Arrays  []StringArray `xml:"string-array"`
}
type Resources_bool struct {
	XMLName xml.Name    `xml:"resources"`
	Arrays  []BoolArray `xml:"bool"`
}

// 修改xml string文件，目前默认在res目录下 WIP
func ModifyRes_stringArray(apk Apkfile, filename, targetNodeName string, newData []string) {
	DecompileApk(apk)
	realpath := filepath.Join(apk.Execpath, "tmp", "apkdec", apk.Pkgname, "res", filename)
	fmt.Println("realpath->>>", realpath)
	modifyXMLFile_str(realpath, targetNodeName, newData)
}

// 修改xml bool文件，目前默认在res目录下 WIP
func ModifyRes_bool(apk Apkfile, filename, targetNodeName string, newData string) {
	DecompileApk(apk)
	realpath := filepath.Join(apk.Execpath, "tmp", "apkdec", apk.Pkgname, "res", filename)
	fmt.Println("realpath->>>", realpath)
	err := modifyXMLFile_bool(realpath, targetNodeName, newData)
	checkerr(err)
}

func modifyXMLFile_bool(filename, targetNodeName string, newData string) error {
	xmlFile, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("open file fail: %w", err)
	}
	defer xmlFile.Close()
	byteValue, err := io.ReadAll(xmlFile)
	if err != nil {
		return fmt.Errorf("read file error: %w", err)
	}
	var resources Resources_bool
	err = xml.Unmarshal(byteValue, &resources)
	if err != nil {
		return fmt.Errorf("error occur when unmarshal xml: %w", err)
	}
	found := false
	for i, array := range resources.Arrays {
		if array.Name == targetNodeName {
			resources.Arrays[i].Flags = newData
			found = true
			break
		}
	}
	if !found {
		var newBoolarray BoolArray
		newBoolarray.Flags=newData
		newBoolarray.Name=targetNodeName
		resources.Arrays = append(resources.Arrays, newBoolarray)
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

func modifyXMLFile_str(filename, targetNodeName string, newData []string) error {
	xmlFile, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("open file fail: %w", err)
	}
	defer xmlFile.Close()
	byteValue, err := io.ReadAll(xmlFile)
	if err != nil {
		return fmt.Errorf("read file error: %w", err)
	}
	var resources Resources_str
	err = xml.Unmarshal(byteValue, &resources)
	if err != nil {
		return fmt.Errorf("error occur when unmarshal xml: %w", err)
	}
	found := false
	for i, array := range resources.Arrays {
		fmt.Println(array.XMLName)
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
