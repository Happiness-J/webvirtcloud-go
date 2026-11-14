package web

import "webvirtcloud-go/internal/libvirt"

func renameArgs(oldName, newName string) ([]string, error) {
	if err := libvirt.ValidateName(oldName); err != nil || libvirt.ValidateName(newName) != nil || oldName == newName {
		return nil, errorString("虚拟机名称无效")
	}
	return []string{"domrename", oldName, newName}, nil
}
