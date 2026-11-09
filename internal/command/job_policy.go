package command

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

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
		if len(args) == 0 {
			return "", fmt.Errorf("任务缺少命令参数")
		}
		if tool != "virsh" {
			return "", fmt.Errorf("任务类型 host-test 仅支持 virsh")
		}
		if len(args) != 1 || args[0] != "version" {
			return "", fmt.Errorf("任务类型 host-test 仅允许执行 virsh version")
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
	default:
		return "", fmt.Errorf("未知任务类型 %q", kind)
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
	case "snapshot-create-as", "snapshot-revert", "snapshot-delete":
		if len(args) != 3 {
			return fmt.Errorf("快照操作 %s 的参数数量不正确", args[0])
		}
		if err := validateActionOperand(args[2]); err != nil {
			return fmt.Errorf("快照名称无效: %w", err)
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
