package web

import "fmt"

type guestProfile struct {
	Name     string
	Features string
	Clock    string
	Hints    []string
}

func guestOptimization(kind string) (guestProfile, error) {
	switch kind {
	case "windows":
		return guestProfile{Name: "Windows", Features: "hyperv_relaxed=on,hyperv_vapic=on,hyperv_spinlocks=on", Clock: "offset=localtime,hypervclock_present=yes", Hints: []string{"请在客户机内安装 virtio 磁盘、网卡和随机数驱动。", "时钟使用本地时间，并启用 Hyper-V 时钟。"}}, nil
	case "linux":
		return guestProfile{Name: "Linux", Features: "acpi=on", Clock: "offset=utc,timer_kvm.present=yes", Hints: []string{"建议客户机使用 kvm-clock。", "建议磁盘调度器使用 none 或 mq-deadline。"}}, nil
	default:
		return guestProfile{}, fmt.Errorf("请选择 Windows 或 Linux 优化档")
	}
}

func guestProfileArgs(vm, kind string) ([]string, error) {
	profile, err := guestOptimization(kind)
	if err != nil {
		return nil, err
	}
	return []string{vm, "--edit", "--features", profile.Features, "--clock", profile.Clock}, nil
}
