package jobs

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"webvirtcloud-go/internal/command"
)

func createdVolumePath(args []string) (string, bool) {
	if len(args) < 3 || args[0] != "vol-create-as" {
		return "", false
	}
	pool, name := args[1], args[2]
	if pool == "" || name == "" || strings.Contains(pool, "/") || strings.Contains(name, "/") || strings.Contains(pool, "..") || strings.Contains(name, "..") {
		return "", false
	}
	return filepath.Join("/var/lib/libvirt/images", name), true
}

func shareCreatedVolume(path string) error {
	clean := filepath.Clean(path)
	if clean != path || !strings.HasPrefix(path, "/var/lib/libvirt/images/") || strings.Contains(path, "..") {
		return fmt.Errorf("新建卷路径无效")
	}
	group, err := user.LookupGroup("libvirt")
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return err
	}
	if err := os.Chown(path, 0, gid); err != nil {
		return err
	}
	return os.Chmod(path, 0o640)
}

func (w *Worker) shareLocalCreatedVolume(target command.Target, args []string) {
	if target.Kind != command.TargetLocal {
		return
	}
	path, ok := createdVolumePath(args)
	if !ok {
		return
	}
	if err := shareCreatedVolume(path); err != nil {
		w.log.Warn("share created volume", "path", path, "error", err)
	}
}
