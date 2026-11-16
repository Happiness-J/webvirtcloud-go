package command

import (
	"fmt"
	"path/filepath"
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
	RoleDesktop  = "desktop"
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
	case "vm-create":
		if tool != "virt-install" {
			return "", fmt.Errorf("创建虚拟机只允许 virt-install")
		}
		if err := validateVMCreateArgs(args); err != nil {
			return "", err
		}
		return RoleOperator, nil
	case "host-test":
		if err := validateHostTestArgs(tool, args); err != nil {
			return "", err
		}
		return RoleOperator, nil
	case "filesystem-inspect":
		if tool != "virt-filesystems" {
			return "", fmt.Errorf("任务类型 filesystem-inspect 仅支持 virt-filesystems")
		}
		if len(args) != 5 || args[0] != "-a" || args[2] != "--all" || args[3] != "--long" || args[4] != "--human-readable" || !strings.HasPrefix(args[1], "/") || strings.Contains(args[1], "..") {
			return "", fmt.Errorf("文件系统检查参数不正确")
		}
		return RoleOperator, nil
	case "vm-nic":
		if tool != "virt-xml" || !validNICAdvancedArgs(args) {
			return "", fmt.Errorf("网卡高级参数无效")
		}
		return RoleOperator, nil
	case "vm-firewall":
		if tool != "virt-xml" || !validFirewallArgs(args) {
			return "", fmt.Errorf("防火墙参数无效")
		}
		return RoleOperator, nil
	case "vm-debug":
		if tool != "virt-xml" || !validGDBArgs(args) {
			return "", fmt.Errorf("GDB 参数无效")
		}
		return RoleAdmin, nil
	case "vm-video":
		if tool != "virt-xml" || !validVideoArgs(args) {
			return "", fmt.Errorf("显卡参数无效")
		}
		return RoleOperator, nil
	case "vm-disk-controller":
		if tool != "virt-xml" || !validDiskControllerArgs(args) {
			return "", fmt.Errorf("磁盘控制器参数无效")
		}
		return RoleOperator, nil
	case "vm-profile":
		if tool != "virt-xml" {
			return "", fmt.Errorf("客户机优化只允许 virt-xml")
		}
		if err := validateGuestProfileArgs(args); err != nil {
			return "", err
		}
		return RoleOperator, nil
	case "vm-device":
		if tool != "virt-xml" {
			return "", fmt.Errorf("虚拟设备配置只允许 virt-xml")
		}
		if err := validateGuestDeviceArgs(args); err != nil {
			return "", err
		}
		return RoleOperator, nil
	case "vm-performance":
		if tool != "virt-xml" && tool != "virsh" {
			return "", fmt.Errorf("性能配置工具无效")
		}
		if err := validatePerformanceArgs(tool, args); err != nil {
			return "", err
		}
		return RoleOperator, nil
	case "vm-boot":
		if tool != "virt-xml" {
			return "", fmt.Errorf("启动配置只允许 virt-xml")
		}
		if err := validateBootEditArgs(args); err != nil {
			return "", err
		}
		return RoleOperator, nil
	case "vm-clone":
		if tool != "virt-clone" {
			return "", fmt.Errorf("任务类型 vm-clone 仅支持 virt-clone")
		}
		if err := validateVMCloneArgs(args); err != nil {
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
		switch args[0] {
		case "undefine":
			return RoleAdmin, nil
		case "start", "shutdown":
			return RoleDesktop, nil
		default:
			return RoleOperator, nil
		}
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
	case "setmaxmem":
		kiB, err := strconv.ParseUint(args[2], 10, 64)
		if err != nil || len(args) != 4 || args[3] != "--config" || kiB < 64*1024 || kiB > 1<<40 {
			return fmt.Errorf("最大内存参数不正确")
		}
		return nil
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
	case "net-define":
		if len(args) != 2 || args[1] != "/dev/stdin" {
			return fmt.Errorf("网络定义参数不正确")
		}
		return nil
	case "net-undefine":
		if len(args) != 2 {
			return fmt.Errorf("删除网络参数不正确")
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
		if args[0] != "blockresize" || (len(args) != 4 && !(len(args) == 5 && args[4] == "--guest-notify")) {
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
		if err := validateImageConvertArgs(args); err == nil {
			return nil
		}
		return fmt.Errorf("离线磁盘操作参数不正确")
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

func validateVMCloneArgs(args []string) error {
	if len(args) < 5 || args[0] != "--original" || args[2] != "--name" || args[4] != "--auto-clone" || (len(args) != 5 && !(len(args) == 6 && args[5] == "--reflink")) {
		return fmt.Errorf("克隆参数不正确")
	}
	if err := validateActionOperand(args[1]); err != nil {
		return fmt.Errorf("源虚拟机名称无效: %w", err)
	}
	if err := validateActionOperand(args[3]); err != nil {
		return fmt.Errorf("克隆名称无效: %w", err)
	}
	if args[1] == args[3] {
		return fmt.Errorf("克隆名称不能与源虚拟机相同")
	}
	return nil
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
	case "shutdown", "reboot", "destroy", "suspend", "resume", "managedsave":
		if len(args) != 2 {
			return fmt.Errorf("虚拟机操作 %s 的参数数量不正确", args[0])
		}
	case "start":
		if !(len(args) == 2 || (len(args) == 3 && (args[2] == "--managed" || args[2] == "--paused"))) {
			return fmt.Errorf("虚拟机启动参数不正确")
		}
	case "undefine":
		if len(args) < 2 {
			return fmt.Errorf("虚拟机删除参数数量不正确")
		}
		allowed := map[string]bool{
			"--snapshots-metadata": true, "--checkpoints-metadata": true,
			"--nvram": true, "--remove-all-storage": true,
		}
		seen := make(map[string]bool, len(args)-2)
		for _, flag := range args[2:] {
			if !allowed[flag] || seen[flag] {
				return fmt.Errorf("虚拟机删除参数不正确")
			}
			seen[flag] = true
		}
	case "snapshot-create-as":
		if len(args) != 4 || args[3] != "--quiesce" {
			return fmt.Errorf("静默快照参数不正确")
		}
	case "snapshot-revert", "snapshot-delete", "checkpoint-delete":
		if len(args) != 3 {
			return fmt.Errorf("恢复点操作 %s 的参数数量不正确", args[0])
		}
		if err := validateActionOperand(args[2]); err != nil {
			return fmt.Errorf("恢复点名称无效: %w", err)
		}
	case "checkpoint-create-as":
		if len(args) != 4 || args[2] != "--name" {
			return fmt.Errorf("增量检查点参数不正确")
		}
		if err := validateActionOperand(args[3]); err != nil {
			return fmt.Errorf("检查点名称无效: %w", err)
		}
	case "attach-device", "detach-device":
		return validateHostdevArgs(args)
	case "attach-disk":
		if !(len(args) == 7 && args[4] == "--targetbus" && args[5] == "usb" && args[6] == "--live") && !(len(args) == 8 && args[4] == "--targetbus" && args[5] == "virtio" && args[6] == "--live" && args[7] == "--config") {
			return fmt.Errorf("USB 挂载参数不正确")
		}
		if err := ValidateDevicePathPublic(args[2]); err != nil {
			return err
		}
		if err := ValidateDiskTargetPublic(args[3]); err != nil {
			return err
		}
	case "detach-disk":
		if !((len(args) == 4 && args[3] == "--live") || (len(args) == 5 && args[3] == "--live" && args[4] == "--config")) {
			return fmt.Errorf("磁盘卸载参数不正确")
		}
		if err := ValidateDiskTargetPublic(args[2]); err != nil {
			return err
		}
	case "attach-interface":
		if len(args) != 9 || args[2] != "network" || args[5] != "--model" || args[6] != "virtio" || args[7] != "--live" || args[8] != "--config" {
			return fmt.Errorf("网卡热添加参数不正确")
		}
		if err := validateActionOperand(args[4]); err != nil {
			return fmt.Errorf("虚拟网络名称无效")
		}
	case "change-media":
		if err := ValidateDiskTargetPublic(args[2]); err != nil {
			return err
		}
		switch {
		case len(args) == 6 && args[3] == "--eject" && args[4] == "--live" && args[5] == "--config":
		case len(args) == 7 && args[3] == "--insert" && strings.HasPrefix(args[4], "/") && strings.HasSuffix(strings.ToLower(args[4]), ".iso") && !strings.Contains(args[4], "..") && args[5] == "--live" && args[6] == "--config":
		default:
			return fmt.Errorf("光盘更换参数不正确")
		}
	case "dumpxml":
		if len(args) != 4 || args[2] != "--destination" || !validTransferDir(args[3]) {
			return fmt.Errorf("导出参数不正确")
		}
	case "define":
		if len(args) != 2 || !strings.HasPrefix(args[1], "/") || strings.Contains(args[1], "..") || !strings.HasSuffix(args[1], ".xml") {
			return fmt.Errorf("导入参数不正确")
		}
	case "domrename":
		if len(args) != 3 || validateActionOperand(args[1]) != nil || validateActionOperand(args[2]) != nil || args[1] == args[2] {
			return fmt.Errorf("重命名参数不正确")
		}
	case "numatune", "vcpupin":
		if !validCPUSetArg(args) {
			return fmt.Errorf("CPU 放置参数无效")
		}
	case "node-memory-tune":
		if len(args) != 3 || args[1] != "shm-merge" || (args[2] != "0" && args[2] != "1") {
			return fmt.Errorf("KSM 参数无效")
		}
	case "migrate":
		if len(args) < 4 || args[2] != "--live" || ValidateMigrationURIPublic(args[3]) != nil {
			return fmt.Errorf("迁移参数不正确")
		}
		index := 4
		if index < len(args) && args[index] == "--bandwidth" {
			if index+1 >= len(args) {
				return fmt.Errorf("迁移带宽无效")
			}
			value, err := strconv.Atoi(args[index+1])
			if err != nil || value < 1 || value > 1048576 {
				return fmt.Errorf("迁移带宽无效")
			}
			index += 2
		}
		if index < len(args) {
			if len(args) != index+1 || args[index] != "--tls" {
				return fmt.Errorf("迁移加密参数无效")
			}
		}
	default:
		return fmt.Errorf("任务类型 vm-action 不支持操作 %q", args[0])
	}
	return nil
}

// ValidateActionOperandPublic checks names supplied by operators for VMs,
// snapshots and checkpoints before they become command arguments.
func ValidateActionOperandPublic(value string) error {
	return validateActionOperand(value)
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
	case RoleDesktop:
		return role == RoleAdmin || role == RoleOperator || role == RoleDesktop
	default:
		return false
	}
}

func knownTool(tool string) bool {
	_, ok := toolNames[tool]
	return ok
}

func validateHostdevArgs(args []string) error {
	if len(args) != 3 || (args[2] != "--config" && args[2] != "--live") {
		return fmt.Errorf("主机设备参数不正确")
	}
	if err := validateActionOperand(args[1]); err != nil {
		return fmt.Errorf("虚拟机名称无效")
	}
	return nil
}

// ValidateHostdevXML accepts only the device XML generated for USB passthrough
// or a raw block LUN. Callers must pass the job stdin through this check.
func ValidateHostdevXML(value string) error {
	if len(value) == 0 || len(value) > 1024 || strings.ContainsAny(value, "\n\r\x00") {
		return fmt.Errorf("主机设备描述无效")
	}
	usb := regexp.MustCompile(`^<hostdev mode='subsystem' type='usb' managed='yes'><source><vendor id='0x[0-9a-f]{4}'/><product id='0x[0-9a-f]{4}'/></source></hostdev>$`)
	block := regexp.MustCompile(`^<disk type='block' device='lun'><driver name='qemu' type='raw' cache='none' io='native'/><source dev='/dev/[A-Za-z0-9_./-]{1,200}'/><target dev='sd[a-z]{1,4}' bus='scsi'/></disk>$`)
	if usb.MatchString(value) {
		return nil
	}
	if block.MatchString(value) && !strings.Contains(value, "..") && !unsafeHostdevBlock(value) {
		return nil
	}
	return fmt.Errorf("主机设备描述无效")
}

func ValidateDevicePathPublic(value string) error {
	if value == "" || strings.Contains(value, "..") || strings.ContainsAny(value, " \t\r\n") {
		return fmt.Errorf("设备路径无效")
	}
	if !strings.HasPrefix(value, "/dev/") {
		return fmt.Errorf("只允许挂载 /dev 下的主机设备")
	}
	return nil
}

// ValidateDiskTargetPublic accepts a guest-visible disk target such as sda,
// vdb or xvdc. It deliberately rejects paths and option-like values because
// virsh detach-disk treats its third argument as a target, not a host path.
func ValidateDiskTargetPublic(value string) error {
	if len(value) < 3 || len(value) > 16 || strings.TrimSpace(value) != value {
		return fmt.Errorf("磁盘目标无效")
	}
	lower := strings.ToLower(value)
	prefixLength := 0
	for _, prefix := range []string{"xvd", "sd", "vd", "hd"} {
		if strings.HasPrefix(lower, prefix) {
			prefixLength = len(prefix)
			break
		}
	}
	if prefixLength == 0 || prefixLength == len(lower) {
		return fmt.Errorf("磁盘目标无效")
	}
	for _, letter := range lower[prefixLength:] {
		if letter < 'a' || letter > 'z' {
			return fmt.Errorf("磁盘目标无效")
		}
	}
	return nil
}

func ValidateMigrationURIPublic(value string) error {
	if value == "" || strings.ContainsAny(value, " \t\r\n") || !(strings.HasPrefix(value, "qemu+ssh://") || strings.HasPrefix(value, "qemu+tls://")) {
		return fmt.Errorf("迁移目标必须使用 qemu+ssh 或 qemu+tls")
	}
	return nil
}

func validateVMCreateArgs(args []string) error {
	if len(args) < 14 || args[0] != "--name" || args[2] != "--vcpus" || args[4] != "--memory" ||
		args[6] != "--disk" || args[8] != "--osinfo" || args[10] != "--graphics" ||
		(args[11] != "vnc,listen=127.0.0.1" && args[11] != "spice,listen=127.0.0.1") || args[12] != "--noautoconsole" {
		return fmt.Errorf("创建虚拟机参数不正确")
	}
	if err := validateActionOperand(args[1]); err != nil {
		return fmt.Errorf("虚拟机名称无效")
	}
	if !validCPUArg(args[3]) {
		return fmt.Errorf("vCPU 数量无效")
	}
	memory, err := strconv.Atoi(args[5])
	if err != nil || memory < 256 || memory > 1048576 {
		return fmt.Errorf("内存参数无效")
	}
	diskValue := strings.TrimPrefix(args[7], "size=")
	diskValue = strings.TrimSuffix(diskValue, ",bus=virtio")
	diskValue = strings.TrimSuffix(diskValue, ",bus=sata")
	diskValue = strings.TrimSuffix(diskValue, ",bus=scsi")
	if strings.Contains(diskValue, ",bus=nvme,queues=") {
		parts := strings.Split(diskValue, ",bus=nvme,queues=")
		if len(parts) != 2 || !validQueueArg(parts[1]) {
			return fmt.Errorf("磁盘控制器参数无效")
		}
		diskValue = parts[0]
	}
	diskSize, diskFormat, ok := strings.Cut(diskValue, ",format=")
	diskGB, diskErr := strconv.Atoi(diskSize)
	if !strings.HasPrefix(args[7], "size=") || !ok || diskFormat != "qcow2" || diskErr != nil || diskGB < 1 || diskGB > 10000 {
		return fmt.Errorf("磁盘参数无效")
	}
	osVariant := args[9]
	if len(osVariant) == 0 || len(osVariant) > 128 || strings.IndexFunc(osVariant, func(r rune) bool {
		return !(r == '.' || r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z')
	}) >= 0 {
		return fmt.Errorf("系统参数无效")
	}
	index := 13
	switch args[index] {
	case "--cdrom":
		if len(args) <= index+1 || !strings.HasPrefix(args[index+1], "/") || strings.Contains(args[index+1], "..") || !strings.HasSuffix(strings.ToLower(args[index+1]), ".iso") {
			return fmt.Errorf("安装 ISO 路径无效")
		}
		index += 2
	case "--import":
		index++
	default:
		return fmt.Errorf("创建方式无效")
	}
	if index < len(args) && args[index] == "--network" {
		if index+1 >= len(args) {
			return fmt.Errorf("网络参数无效")
		}
		network, ok := strings.CutPrefix(args[index+1], "network=")
		if !ok || validateActionOperand(network) != nil {
			return fmt.Errorf("网络参数无效")
		}
		index += 2
	}
	if index < len(args) && args[index] == "--cloud-init" {
		if len(args) < index+2 || !validCloudInitArg(args[index+1]) {
			return fmt.Errorf("cloud-init 参数无效")
		}
		index += 2
	}
	if index < len(args) && args[index] == "--rng" {
		if index+1 >= len(args) || args[index+1] != "/dev/urandom" {
			return fmt.Errorf("随机数设备参数无效")
		}
		index += 2
	}
	if index < len(args) && args[index] == "--watchdog" {
		if index+1 >= len(args) || (args[index+1] != "i6300esb,action=reset" && args[index+1] != "i6300esb,action=shutdown" && args[index+1] != "i6300esb,action=none") {
			return fmt.Errorf("看门狗参数无效")
		}
		index += 2
	}
	if index < len(args) && args[index] == "--boot" {
		if index+1 >= len(args) || !validBootOrder(args[index+1]) {
			return fmt.Errorf("启动顺序参数无效")
		}
		index += 2
	}
	if index < len(args) && args[index] == "--video" {
		if index+1 >= len(args) || !validVideoValue(args[index+1]) {
			return fmt.Errorf("显卡参数无效")
		}
		index += 2
	}
	if index < len(args) && args[index] == "--cpu" {
		if index+1 >= len(args) || !validCPUModelArg(args[index+1]) {
			return fmt.Errorf("CPU 模型参数无效")
		}
		index += 2
	}
	if index < len(args) {
		if len(args) != index+2 || args[index] != "--memorybacking" || (args[index+1] != "hugepages,page.size=2" && args[index+1] != "hugepages,page.size=1024") {
			return fmt.Errorf("大页内存参数无效")
		}
	}
	return nil
}

func validCPUModelArg(value string) bool {
	base, feature, hasFeature := strings.Cut(value, ",")
	if hasFeature && feature != "+vmx" && feature != "+svm" {
		return false
	}
	return base == "host-model" || base == "host-passthrough" || validSimpleName(base)
}

func validSimpleName(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			return false
		}
	}
	return true
}

func validCloudInitArg(value string) bool {
	user, network, ok := strings.Cut(value, ",")
	if !ok || !strings.HasPrefix(user, "user-data=") || !strings.HasPrefix(network, "network-config=") {
		return false
	}
	userPath := strings.TrimPrefix(user, "user-data=")
	networkPath := strings.TrimPrefix(network, "network-config=")
	return userPath == networkPath && validGeneratedCloudInitPath(userPath)
}

func validGeneratedCloudInitPath(value string) bool {
	clean := filepath.Clean(value)
	return clean == value && filepath.IsAbs(clean) && !strings.Contains(clean, "..") && strings.HasPrefix(filepath.Base(clean), "cidata_") && strings.HasSuffix(clean, ".iso")
}

func validTransferDir(value string) bool {
	return strings.HasPrefix(value, "/") && !strings.Contains(value, "..") && !strings.ContainsAny(value, " \t\r\n")
}

func validCPUArg(value string) bool {
	head, _, _ := strings.Cut(value, ",")
	vcpus, err := strconv.Atoi(head)
	return err == nil && vcpus >= 1 && vcpus <= 128 && (value == head || strings.Contains(value, "sockets=") && strings.Contains(value, "cores=") && strings.Contains(value, "threads="))
}

func validCPUSetArg(args []string) bool {
	if len(args) < 4 || args[len(args)-1] != "--config" {
		return false
	}
	value := args[len(args)-2]
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r == '-' || r == ',' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func unsafeHostdevBlock(value string) bool {
	start := strings.Index(value, "dev='")
	end := strings.Index(value, "'/><target")
	if start < 0 || end < start {
		return true
	}
	path := value[start+5 : end]
	base := path[strings.LastIndex(path, "/")+1:]
	if strings.HasPrefix(base, "loop") || strings.HasPrefix(base, "dm-") || strings.HasPrefix(base, "zram") || strings.HasPrefix(base, "nbd") {
		return true
	}
	whole := (strings.HasPrefix(base, "sd") || strings.HasPrefix(base, "vd") || strings.HasPrefix(base, "hd")) && len(base) == 3
	nvme := strings.HasPrefix(base, "nvme") && strings.Contains(base, "n") && !strings.Contains(base, "p")
	mmc := strings.HasPrefix(base, "mmcblk") && !strings.Contains(base, "p")
	return whole || nvme || mmc
}

func validateBootEditArgs(args []string) error {
	if len(args) != 4 || args[1] != "--edit" || args[2] != "--boot" || validateActionOperand(args[0]) != nil || !validBootOrder(args[3]) {
		return fmt.Errorf("启动配置参数无效")
	}
	return nil
}

func validBootOrder(value string) bool {
	order, menu, ok := strings.Cut(value, ",menu=")
	if !ok || (menu != "on" && menu != "off") || order == "" {
		return false
	}
	seen := map[string]bool{}
	for _, part := range strings.Split(order, ",") {
		if (part != "hd" && part != "cdrom" && part != "network") || seen[part] {
			return false
		}
		seen[part] = true
	}
	return true
}

func validateImageConvertArgs(args []string) error {
	if len(args) != 8 || args[0] != "convert" || args[1] != "-p" || args[2] != "-f" || args[4] != "-O" {
		return fmt.Errorf("镜像转换参数不正确")
	}
	if !validConvertFormatArg(args[3]) || !validConvertFormatArg(args[5]) || args[3] == args[5] {
		return fmt.Errorf("镜像格式无效")
	}
	if !validManagedImagePath(args[6]) || !validManagedImagePath(args[7]) || filepath.Dir(args[6]) != filepath.Dir(args[7]) || args[6] == args[7] {
		return fmt.Errorf("镜像路径无效")
	}
	return nil
}

func validConvertFormatArg(value string) bool {
	return value == "qcow2" || value == "raw" || value == "vmdk"
}

func validManagedImagePath(value string) bool {
	clean := filepath.Clean(value)
	return clean == value && filepath.IsAbs(clean) && !strings.Contains(value, "..") && !strings.ContainsAny(value, " \t\r\n")
}

func validatePerformanceArgs(tool string, args []string) error {
	if tool == "virt-xml" && len(args) == 5 && args[1] == "--edit" && strings.HasPrefix(args[2], "mac=") && args[3] == "--network" && strings.HasPrefix(args[4], "driver_queues=") {
		if validateActionOperand(args[0]) != nil || !validMACArg(strings.TrimPrefix(args[2], "mac=")) || !validQueueArg(strings.TrimPrefix(args[4], "driver_queues=")) {
			return fmt.Errorf("网卡队列参数无效")
		}
		return nil
	}
	if tool == "virt-xml" && len(args) == 5 && args[1] == "--edit" && strings.HasPrefix(args[2], "target=") && args[3] == "--disk" && (args[4] == "discard=ignore" || args[4] == "discard=unmap") {
		if validateActionOperand(args[0]) != nil || ValidateDiskTargetPublic(strings.TrimPrefix(args[2], "target=")) != nil {
			return fmt.Errorf("discard 参数无效")
		}
		return nil
	}
	if tool == "virsh" && len(args) == 12 && args[0] == "blkdeviotune" && args[3] == "--read-bytes-sec" && args[5] == "--write-bytes-sec" && args[7] == "--read-iops-sec" && args[9] == "--write-iops-sec" && args[11] == "--config" {
		if validateActionOperand(args[1]) != nil || ValidateDiskTargetPublic(args[2]) != nil || !validTuneArg(args[4]) || !validTuneArg(args[6]) || !validTuneArg(args[8]) || !validTuneArg(args[10]) {
			return fmt.Errorf("磁盘限速参数无效")
		}
		return nil
	}
	return fmt.Errorf("性能配置参数无效")
}

func validQueueArg(value string) bool {
	number, err := strconv.Atoi(value)
	return err == nil && number >= 1 && number <= 16
}

func validTuneArg(value string) bool {
	number, err := strconv.ParseUint(value, 10, 64)
	return err == nil && number <= 1000000000000
}

func validMACArg(value string) bool {
	parts := strings.Split(value, ":")
	if len(parts) != 6 {
		return false
	}
	for _, part := range parts {
		if len(part) != 2 {
			return false
		}
		for _, r := range part {
			if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
				return false
			}
		}
	}
	return true
}

func validateGuestDeviceArgs(args []string) error {
	if len(args) != 4 || validateActionOperand(args[0]) != nil || (args[1] != "--add-device" && args[1] != "--remove-device") {
		return fmt.Errorf("虚拟设备参数无效")
	}
	switch args[2] {
	case "--rng":
		if args[1] == "--add-device" && args[3] == "/dev/urandom" || args[1] == "--remove-device" && args[3] == "1" {
			return nil
		}
	case "--watchdog":
		if args[1] == "--add-device" && (args[3] == "i6300esb,action=reset" || args[3] == "i6300esb,action=shutdown" || args[3] == "i6300esb,action=none") || args[1] == "--remove-device" && args[3] == "1" {
			return nil
		}
	}
	return fmt.Errorf("虚拟设备参数无效")
}

func validateGuestProfileArgs(args []string) error {
	if len(args) != 6 || args[1] != "--edit" || args[2] != "--features" || args[4] != "--clock" || validateActionOperand(args[0]) != nil {
		return fmt.Errorf("客户机优化参数无效")
	}
	windows := args[3] == "hyperv_relaxed=on,hyperv_vapic=on,hyperv_spinlocks=on" && args[5] == "offset=localtime,hypervclock_present=yes"
	linux := args[3] == "acpi=on" && args[5] == "offset=utc,timer_kvm.present=yes"
	if windows || linux {
		return nil
	}
	return fmt.Errorf("客户机优化参数无效")
}

func validDiskControllerArgs(args []string) bool {
	if len(args) != 5 || args[1] != "--edit" || !strings.HasPrefix(args[2], "target=") || args[3] != "--disk" || validateActionOperand(args[0]) != nil || ValidateDiskTargetPublic(strings.TrimPrefix(args[2], "target=")) != nil {
		return false
	}
	switch args[4] {
	case "bus=virtio", "bus=sata", "bus=scsi":
		return true
	default:
		value, ok := strings.CutPrefix(args[4], "bus=nvme,queues=")
		return ok && validQueueArg(value)
	}
}

func validVideoArgs(args []string) bool {
	return len(args) == 4 && args[1] == "--edit" && args[2] == "--video" && validateActionOperand(args[0]) == nil && validVideoValue(args[3])
}

func validVideoValue(value string) bool {
	switch value {
	case "model=none", "model=virtio,vram=16384", "model=virtio,vram=32768", "model=virtio,vram=65536", "model=virtio,vram=131072", "model=virtio,vram=262144", "model=virtio,vram=524288":
		return true
	case "model=qxl,vram=16384", "model=qxl,vram=32768", "model=qxl,vram=65536", "model=qxl,vram=131072", "model=qxl,vram=262144", "model=qxl,vram=524288":
		return true
	case "model=vga,vram=16384", "model=vga,vram=32768", "model=vga,vram=65536", "model=vga,vram=131072", "model=vga,vram=262144", "model=vga,vram=524288":
		return true
	default:
		return false
	}
}

func validGDBArgs(args []string) bool {
	if len(args) != 4 || args[1] != "--edit" || args[2] != "--qemu-commandline" || validateActionOperand(args[0]) != nil {
		return false
	}
	prefix := "-gdb tcp:127.0.0.1:"
	if !strings.HasPrefix(args[3], prefix) {
		return false
	}
	number, err := strconv.Atoi(strings.TrimPrefix(args[3], prefix))
	return err == nil && number >= 1024 && number <= 65535
}

func validFirewallArgs(args []string) bool {
	if len(args) != 5 || args[1] != "--edit" || !strings.HasPrefix(args[2], "mac=") || args[3] != "--network" || !strings.HasPrefix(args[4], "filterref=clean-traffic,") || validateActionOperand(args[0]) != nil || !validMACArg(strings.TrimPrefix(args[2], "mac=")) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(args[4], "filterref=clean-traffic,"), ",")
	if len(parts) < 3 || len(parts) > 4 {
		return false
	}
	if (parts[0] != "in" && parts[0] != "out") || (parts[1] != "tcp" && parts[1] != "udp" && parts[1] != "icmp") {
		return false
	}
	if parts[1] != "icmp" {
		port, err := strconv.Atoi(parts[2])
		if err != nil || port < 1 || port > 65535 {
			return false
		}
	}
	return len(parts) == 3 || parts[3] == "antispoof"
}

func validNICAdvancedArgs(args []string) bool {
	if len(args) != 5 || args[1] != "--edit" || !strings.HasPrefix(args[2], "mac=") || args[3] != "--network" || validateActionOperand(args[0]) != nil || !validMACArg(strings.TrimPrefix(args[2], "mac=")) {
		return false
	}
	for _, part := range strings.Split(args[4], ",") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return false
		}
		switch key {
		case "access", "trunk":
			id, err := strconv.Atoi(value)
			if err != nil || id < 1 || id > 4094 {
				return false
			}
		case "mtu":
			mtu, err := strconv.Atoi(value)
			if err != nil || mtu < 576 || mtu > 9000 {
				return false
			}
		case "mac":
			if !validMACArg(value) {
				return false
			}
		case "link":
			if value != "up" && value != "down" {
				return false
			}
		default:
			return false
		}
	}
	return true
}
