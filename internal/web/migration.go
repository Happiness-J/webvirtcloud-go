package web

import (
	"fmt"
	"strconv"

	"webvirtcloud-go/internal/command"
)

func migrationArgs(vm, target, bandwidth string, encrypted bool) ([]string, error) {
	if err := command.ValidateMigrationURIPublic(target); err != nil {
		return nil, err
	}
	args := []string{"migrate", vm, "--live", target}
	if bandwidth != "" {
		value, err := strconv.Atoi(bandwidth)
		if err != nil || value < 1 || value > 1048576 {
			return nil, fmt.Errorf("迁移带宽必须是 1 到 1048576 MiB/s")
		}
		args = append(args, "--bandwidth", strconv.Itoa(value))
	}
	if encrypted {
		args = append(args, "--tls")
	}
	return args, nil
}
