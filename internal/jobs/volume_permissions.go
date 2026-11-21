package jobs

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"webvirtcloud-go/internal/command"
)

func createdVolumeArgs(args []string) (pool, name string, ok bool) {
	if len(args) < 3 || args[0] != "vol-create-as" {
		return "", "", false
	}
	pool, name = args[1], args[2]
	if pool == "" || name == "" || strings.Contains(pool, "/") || strings.Contains(name, "/") || strings.Contains(pool, "..") || strings.Contains(name, "..") {
		return "", "", false
	}
	return pool, name, true
}

func volumePathFromOutput(output string) (string, bool) {
	for _, line := range strings.Split(output, "\n") {
		path := strings.TrimSpace(line)
		if strings.HasPrefix(path, "/") && !strings.Contains(path, "..") && filepath.Clean(path) == path {
			return path, true
		}
	}
	return "", false
}

func shareCreatedVolume(path string) error {
	clean := filepath.Clean(path)
	if !strings.HasPrefix(path, "/") || strings.Contains(path, "..") || clean != path {
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
	if target.Kind != command.TargetLocal || w.runner == nil {
		return
	}
	pool, name, ok := createdVolumeArgs(args)
	if !ok {
		return
	}
	result, err := w.runner.Run(context.Background(), target.Request("virsh", []string{"vol-path", "--pool", pool, name}, "", true, 10*time.Second))
	if err != nil {
		w.log.Warn("locate created volume", "pool", pool, "volume", name, "error", err)
		return
	}
	path, ok := volumePathFromOutput(result.Stdout)
	if !ok {
		w.log.Warn("locate created volume", "pool", pool, "volume", name, "error", "no volume path")
		return
	}
	if err := shareCreatedVolume(path); err != nil {
		w.log.Warn("share created volume", "path", path, "error", err)
	}
}
