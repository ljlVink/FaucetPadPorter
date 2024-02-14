package utils

import "os"

func WriteTofile(fileName, content string) {
    file, _ := os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    defer file.Close()
    _, _ = file.WriteString(content+"\n")
}
