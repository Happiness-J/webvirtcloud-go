package command

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var scaledAmountPattern = regexp.MustCompile(`^[1-9][0-9]{0,15}[KMGTPE]?$`)
var signedScaledAmountPattern = regexp.MustCompile(`^[+-][1-9][0-9]{0,15}[KMGTPE]?$`)

const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
)

// JobMinimumRole returns the least-privileged role allowed to execute a
// queued job. The HTTP layer and worker share this action-aware policy so a
// role change while a job is queued cannot preserve permissions that the user
// no longer has.
func JobMinimumRole(kind, tool string, args []string) (string, error) {
	switch kind {
	case "command":
		if !knownTool(tool) {
			return "", fmt.Errorf("任务类型 command 不支持工具 %q", tool)
		}
		return RoleAdmin, nil
	case "host-test":
		if err := validateHostTestArgs(tool, args); err != nil {
			return "", err
		}
		return RoleOperator, nil
	case "vm-action":
		if len(args) == 0 {
			return "", fmt.Errorf("任务缺少命令参数")
		}
		if tool != "virsh" {
			return "", fmt.Errorf("任务类型 vm-action 仅支持 virsh")
		}
		if err := validateVMActionArgs(args); err != nil {
			return "", err
		}
		if args[0] == "undefine" {
			return RoleAdmin, nil
		}
		return RoleOperator, nil
	case "storage-action":
		if tool != "virsh" {
			return "", fmt.Errorf("任务类型 storage-action 仅支持 virsh")
		}
		if err := validateStorageActionArgs(args); err != nil {
			return "", err
		}
		return RoleOperator, nil
	case "network-action":
		if tool != "virsh" {
			return "", fmt.Errorf("任务类型 network-action 仅支持 virsh")
		}
		if err := validateNetworkActionArgs(args); err != nil {
			return "", err
		}
		return RoleOperator, nil
	case "disk-action":
		if err := validateDiskActionArgs(tool, args); err != nil {
			return "", err
		}
		return RoleOperator, nil
	case "vm-resource":
		if err := validateVMResourceArgs(tool, args); err != nil {
			return "", err
		}
		return RoleOperator, nil
	default:
		return "", fmt.Errorf("未知任务类型 %q", kind)
	}
}

func validateVMResourceArgs(tool string, args []string) error {
	if tool != "virsh" || len(args) < 3 {
		return fmt.Errorf("虚拟机资源调优参数不正确")
	}
	if err := validateActionOperand(args[1]); err != nil {
		return fmt.Errorf("虚拟机名称无效: %w", err)
	}
	switch args[0] {
	case "setvcpus":
		count, err := strconv.Atoi(args[2])
		if err != nil || count < 1 || count > 1024 {
			return fmt.Errorf("vCPU 数量必须在 1-1024 之间")
		}
		switch {
		case len(args) == 4 && args[3] == "--config":
			return nil
		case len(args) == 5 && args[3] == "--live" && args[4] == "--config":
			return nil
		case len(args) == 5 && args[3] == "--maximum" && args[4] == "--config":
			return nil
		default:
			return fmt.Errorf("vCPU 调优参数不正确")
		}
	case "setmem":
		kiB, err := strconv.ParseUint(args[2], 10, 64)
		if err != nil || kiB < 64*1024 || kiB > 1<<40 {
			return fmt.Errorf("内存容量必须在 64 MiB 到 1 TiB 之间")
		}
		switch {
		case len(args) == 4 && args[3] == "--config":
			return nil
		case len(args) == 5 && args[3] == "--live" && args[4] == "--config":
			return nil
		default:
			return fmt.Errorf("内存调优参数不正确")
		}
	default:
		return fmt.Errorf("不支持的虚拟机资源操作 %q", args[0])
	}
}

func validateStorageActionArgs(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("存储操作缺少参数")
	}
	switch args[0] {
	case "pool-info":
		if len(args) != 2 {
			return fmt.Errorf("存储池诊断参数数量不正确")
		}
		return validateActionOperand(args[1])
	case "pool-refresh", "pool-start":
		if len(args) != 2 {
			return fmt.Errorf("存储池操作参数数量不正确")
		}
		return validateActionOperand(args[1])
	case "pool-autostart":
		if len(args) == 2 {
			return validateActionOperand(args[1])
		}
		if len(args) == 3 && args[1] == "--disable" {
			return validateActionOperand(args[2])
		}
		return fmt.Errorf("存储池自启动参数不正确")
	case "vol-create-as":
		if len(args) != 6 || args[4] != "--format" || !validVolumeFormatArg(args[5]) {
			return fmt.Errorf("创建卷参数不正确")
		}
		if err := validateActionOperand(args[1]); err != nil {
			return err
		}
		if err := validateActionOperand(args[2]); err != nil {
			return err
		}
		return validateScaledAmount(args[3], false)
	case "vol-clone":
		if len(args) != 5 || args[1] != "--pool" {
			return fmt.Errorf("克隆卷参数不正确")
		}
		for _, value := range args[2:] {
			if err := validateActionOperand(value); err != nil {
				return err
			}
		}
		return nil
	case "vol-resize":
		if len(args) < 6 || args[1] != "--pool" || args[3] != "--delta" {
			return fmt.Errorf("卷扩容参数不正确")
		}
		if err := validateActionOperand(args[2]); err != nil {
			return err
		}
		if err := validateActionOperand(args[len(args)-2]); err != nil {
			return err
		}
		if err := validateScaledAmount(args[len(args)-1], false); err != nil {
			return err
		}
		if len(args) == 6 {
			return nil
		}
		if len(args) == 7 && args[4] == "--shrink" {
			return nil
		}
		return fmt.Errorf("卷缩容参数不正确")
	case "vol-delete":
		if len(args) != 4 || args[1] != "--pool" {
			return fmt.Errorf("删除卷参数不正确")
		}
		if err := validateActionOperand(args[2]); err != nil {
			return err
		}
		return validateActionOperand(args[3])
	default:
		return fmt.Errorf("不支持的存储操作 %q", args[0])
	}
}

func validateNetworkActionArgs(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("网络操作缺少参数")
	}
	switch args[0] {
	case "net-dhcp-leases", "net-info":
		if len(args) != 2 {
			return fmt.Errorf("网络诊断参数数量不正确")
		}
		return validateActionOperand(args[1])
	case "net-start", "net-destroy":
		if len(args) != 2 {
			return fmt.Errorf("网络操作参数数量不正确")
		}
		return validateActionOperand(args[1])
	case "net-autostart":
		if len(args) == 2 {
			return validateActionOperand(args[1])
		}
		if len(args) == 3 && args[1] == "--disable" {
			return validateActionOperand(args[2])
		}
		return fmt.Errorf("网络自启动参数不正确")
	default:
		return fmt.Errorf("不支持的网络操作 %q", args[0])
	}
}

func validateDiskActionArgs(tool string, args []string) error {
	switch tool {
	case "virsh":
		if len(args) != 4 || args[0] != "blockresize" {
			return fmt.Errorf("在线磁盘扩容参数不正确")
		}
		if err := validateActionOperand(args[1]); err != nil {
			return err
		}
		if err := validateActionOperand(args[2]); err != nil {
			return err
		}
		size, err := strconv.ParseUint(args[3], 10, 64)
		if err != nil || size == 0 {
			return fmt.Errorf("在线磁盘容量无效")
		}
		return nil
	case "qemu-img":
		if len(args) == 3 && args[0] == "resize" {
			return validateDiskResizePathAndAmount(args[1], args[2], false)
		}
		if len(args) == 4 && args[0] == "resize" && args[1] == "--shrink" {
			return validateDiskResizePathAndAmount(args[2], args[3], true)
		}
		return fmt.Errorf("离线磁盘扩缩容参数不正确")
	default:
		return fmt.Errorf("磁盘操作不支持工具 %q", tool)
	}
}

func validateDiskResizePathAndAmount(path, amount string, shrink bool) error {
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\x00\r\n") ||
		len(path) > 4096 || strings.TrimSpace(path) != path {
		return fmt.Errorf("磁盘路径无效")
	}
	if !signedScaledAmountPattern.MatchString(amount) {
		return fmt.Errorf("磁盘调整容量无效")
	}
	if shrink != strings.HasPrefix(amount, "-") {
		return fmt.Errorf("磁盘缩容标志与容量方向不一致")
	}
	return nil
}

func validateScaledAmount(value string, signed bool) error {
	pattern := scaledAmountPattern
	if signed {
		pattern = signedScaledAmountPattern
	}
	if !pattern.MatchString(value) {
		return fmt.Errorf("容量格式无效")
	}
	return nil
}

func validVolumeFormatArg(format string) bool {
	switch format {
	case "raw", "qcow", "qcow2", "qed", "vmdk", "iso":
		return true
	default:
		return false
	}
}

func validateHostTestArgs(tool string, args []string) error {
	switch tool {
	case "virsh":
		if len(args) == 1 && args[0] == "version" {
			return nil
		}
		return fmt.Errorf("任务类型 host-test 仅允许执行 virsh version")
	case "virt-host-validate":
		switch {
		case len(args) == 0:
			return nil
		case len(args) == 1 && validHypervisorType(args[0]):
			return nil
		case len(args) == 2 && args[0] == "--quiet" && validHypervisorType(args[1]):
			return nil
		default:
			return fmt.Errorf("宿主机健康检查参数不正确")
		}
	default:
		return fmt.Errorf("任务类型 host-test 不支持工具 %q", tool)
	}
}

func validHypervisorType(value string) bool {
	switch value {
	case "qemu", "lxc", "xen", "bhyve", "vmx", "test":
		return true
	default:
		return false
	}
}

func validateVMActionArgs(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("虚拟机操作缺少参数")
	}
	if args[0] == "autostart" {
		if len(args) == 2 {
			if err := validateActionOperand(args[1]); err != nil {
				return fmt.Errorf("虚拟机名称无效: %w", err)
			}
			return nil
		}
		if len(args) == 3 && args[1] == "--disable" {
			if err := validateActionOperand(args[2]); err != nil {
				return fmt.Errorf("虚拟机名称无效: %w", err)
			}
			return nil
		}
		return fmt.Errorf("虚拟机自启动操作参数不正确")
	}
	if err := validateActionOperand(args[1]); err != nil {
		return fmt.Errorf("虚拟机名称无效: %w", err)
	}
	switch args[0] {
	case "start", "shutdown", "reboot", "destroy", "suspend", "resume", "undefine":
		if len(args) != 2 {
			return fmt.Errorf("虚拟机操作 %s 的参数数量不正确", args[0])
		}
	case "snapshot-create-as", "snapshot-revert", "snapshot-delete", "backup-create-as", "backup-restore", "backup-delete":
		if len(args) != 3 {
			return fmt.Errorf("恢复点操作 %s 的参数数量不正确", args[0])
		}
		if err := validateActionOperand(args[2]); err != nil {
			return fmt.Errorf("恢复点名称无效: %w", err)
		}
	default:
		return fmt.Errorf("任务类型 vm-action 不支持操作 %q", args[0])
	}
	return nil
}

func validateActionOperand(value string) error {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 256 {
		return fmt.Errorf("长度无效")
	}
	if value == "." || value == ".." || strings.TrimSpace(value) != value || strings.HasPrefix(value, "-") || strings.ContainsAny(value, `/\\`) {
		return fmt.Errorf("包含非法字符")
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return fmt.Errorf("包含不可见控制字符")
		}
	}
	return nil
}

// RoleAllows reports whether role satisfies the minimum role returned by
// JobMinimumRole.
func RoleAllows(role, minimum string) bool {
	switch minimum {
	case RoleAdmin:
		return role == RoleAdmin
	case RoleOperator:
		return role == RoleAdmin || role == RoleOperator
	default:
		return false
	}
}

func knownTool(tool string) bool {
	_, ok := toolNames[tool]
	return ok
}
