package utils

import (
	"os"
	"bytes"
)
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

func CheckFormat(filePath string) string {
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
