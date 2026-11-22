package web

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func imageConvertArgs(sourcePath, sourceFormat, targetName, targetFormat string) ([]string, error) {
	if !validImagePath(sourcePath) || !validConvertFormat(sourceFormat) || !validConvertFormat(targetFormat) {
		return nil, fmt.Errorf("镜像转换参数无效")
	}
	if sourceFormat == targetFormat {
		return nil, fmt.Errorf("目标格式必须不同于源格式")
	}
	if err := validConvertName(targetName); err != nil {
		return nil, err
	}
	target := filepath.Join(filepath.Dir(sourcePath), targetName)
	if !validImagePath(target) || filepath.Clean(target) == filepath.Clean(sourcePath) {
		return nil, fmt.Errorf("目标镜像路径无效")
	}
	return []string{"convert", "-p", "-f", sourceFormat, "-O", targetFormat, sourcePath, target}, nil
}

func validConvertFormat(value string) bool {
	switch value {
	case "qcow2", "raw", "vmdk":
		return true
	default:
		return false
	}
}

func validImagePath(value string) bool {
	clean := filepath.Clean(value)
	return clean == value && strings.HasPrefix(value, "/") && !strings.Contains(value, "..") && !strings.ContainsAny(value, " \t\r\n")
}

func validConvertName(value string) error {
	if value == "" || len(value) > 128 || strings.Contains(value, "/") || strings.Contains(value, "..") || strings.HasPrefix(value, ".") {
		return fmt.Errorf("目标文件名无效")
	}
	for _, r := range value {
		if !(r == '.' || r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			return fmt.Errorf("目标文件名无效")
		}
	}
	return nil
}

func detectImageFormat(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".qcow2", ".qcow":
		return "qcow2"
	case ".raw", ".img":
		return "raw"
	case ".vmdk":
		return "vmdk"
	}
	return detectImageHeader(path)
}

func detectImageHeader(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	header := make([]byte, 4)
	if _, err := file.Read(header); err != nil {
		return ""
	}
	switch string(header) {
	case "QFI\xfb":
		return "qcow2"
	case "KDMV":
		return "vmdk"
	default:
		return ""
	}
}
