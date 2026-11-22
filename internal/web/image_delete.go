package web

import (
	"fmt"
	"os"
)

func deleteLibraryImage(dir, name string) error {
	if !validLibraryName(name) {
		return fmt.Errorf("镜像名称无效")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("镜像目录不可用")
	}
	defer root.Close()
	info, err := root.Stat(name)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("镜像不存在")
	}
	if err := root.Remove(name); err != nil {
		return fmt.Errorf("无法删除镜像")
	}
	return nil
}
