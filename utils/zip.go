package utils

import (
	"archive/zip"
	"io"
	"path/filepath"
	"os"
)

//chatgpt
func Unzip(source, target string, filesToExtract []string,rename string) error {
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
