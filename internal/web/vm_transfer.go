package web

import (
	"fmt"
	"strings"

	"webvirtcloud-go/internal/libvirt"
)

func exportArgs(vm, destination string) ([]string, error) {
	if err := libvirt.ValidateName(vm); err != nil {
		return nil, fmt.Errorf("虚拟机名称无效")
	}
	if !validTransferDir(destination) {
		return nil, fmt.Errorf("导出目录无效")
	}
	return []string{"dumpxml", vm, "--destination", destination}, nil
}

func importArgs(source, name string) ([]string, error) {
	if !validTransferDir(source) || libvirt.ValidateName(name) != nil {
		return nil, fmt.Errorf("导入参数无效")
	}
	return []string{"define", source + "/" + name + ".xml"}, nil
}

func validTransferDir(value string) bool {
	return strings.HasPrefix(value, "/") && !strings.Contains(value, "..") && !strings.ContainsAny(value, " \t\r\n")
}
