package command

import (
	"fmt"
	"strconv"
	"strings"
)

// ValidateAsyncMode rejects command modes that cannot be owned by a finite
// asynchronous job. In particular, daemonizing tools detach from the job's
// process group, so a timeout or panel restart can leave an unmanaged service
// listening on a host socket. Interactive editors and servers have the same
// problem: they wait for a TTY or peer that the job runner does not provide.
//
// Foreground long-running commands are still supported and are terminated when
// their configured timeout expires. Operators must use a supervised service for
// permanent QEMU helpers instead of asking the job queue to daemonize them.
func ValidateAsyncMode(tool string, args []string) error {
	if informationalInvocation(args) {
		return nil
	}
	if tool == "virsh" && virshConsoleAttachCommand(args) && hasLongOption(args, "--console") {
		return fmt.Errorf("%w: virsh %s --console 会持续附着到串口控制台，不能由有限时长的异步任务托管；请使用 Web VNC 控制台", ErrNotAllowed, virshCommandName(args))
	}
	switch tool {
	case "qemu-storage-daemon":
		if hasLongOption(args, "--daemonize") {
			return fmt.Errorf("%w: qemu-storage-daemon --daemonize 会脱离任务进程组；请以前台方式运行并由 systemd 或容器托管", ErrNotAllowed)
		}
	case "qemu-pr-helper", "qemu-vmsr-helper":
		if hasLongOption(args, "--daemon") || hasShortFlag(args, 'd', map[byte]bool{'q': true, 'v': true}) {
			return fmt.Errorf("%w: %s --daemon 会脱离任务进程组；请以前台方式运行并由 systemd 或容器托管", ErrNotAllowed, tool)
		}
	case "qemu-nbd":
		if hasLongOption(args, "--fork") {
			return fmt.Errorf("%w: qemu-nbd --fork 会脱离任务进程组；请使用前台导出并设置合理超时", ErrNotAllowed)
		}
	case "guestfish":
		if hasLongOption(args, "--listen") {
			return fmt.Errorf("%w: guestfish --listen 是长期服务模式；请在宿主机上单独托管", ErrNotAllowed)
		}
	case "guestmount":
		return fmt.Errorf("%w: guestmount 默认 fork 并持续等待卸载，不能由有限时长的异步任务托管；请改用 guestfish 或 virt-copy-*，guestunmount 仍可用于清理已有挂载", ErrNotAllowed)
	case "virt-rescue", "virt-p2v", "virt-login-shell":
		return fmt.Errorf("%w: %s 需要图形界面或交互式终端，不能由异步任务执行；请在宿主机终端使用，或改用非交互工具", ErrNotAllowed, tool)
	case "virt-top":
		if !virtTopBatchMode(args) {
			return fmt.Errorf("%w: virt-top 必须使用 -b/--batch 或 --script 批处理模式；交互式 curses 界面不能由异步任务执行", ErrNotAllowed)
		}
		if !virtTopPositiveIterations(args) {
			return fmt.Errorf("%w: virt-top 必须使用正数 -n/--iterations 限制采样次数；否则任务会持续运行到超时", ErrNotAllowed)
		}
	case "virt-builder-repository":
		if hasLongOption(args, "--interactive") || hasShortFlag(args, 'i', map[byte]bool{'v': true}) {
			return fmt.Errorf("%w: virt-builder-repository --interactive 需要终端输入", ErrNotAllowed)
		}
	case "virt-edit":
		if !hasLongOption(args, "--expr") && !hasShortOption(args, "-e") {
			return fmt.Errorf("%w: virt-edit 必须提供 --expr/-e；交互式编辑器不能作为异步任务运行", ErrNotAllowed)
		}
	}
	return nil
}

func virtTopBatchMode(args []string) bool {
	return hasShortFlag(args, 'b', map[byte]bool{}) ||
		hasLongOption(args, "--batch") ||
		hasLongOption(args, "--script")
}

func virtTopPositiveIterations(args []string) bool {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			break
		}
		if arg == "-n" {
			return index+1 < len(args) && positiveDecimal(args[index+1])
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
			cluster := strings.TrimPrefix(arg, "-")
			for position := 0; position < len(cluster); position++ {
				switch cluster[position] {
				case 'n':
					value := cluster[position+1:]
					if value == "" {
						return index+1 < len(args) && positiveDecimal(args[index+1])
					}
					return positiveDecimal(value)
				case 'd', 'o':
					// These short options consume the rest of the token as
					// their value, so a later "n" is not an option.
					position = len(cluster)
				}
			}
			continue
		}
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		if name == "" || !strings.HasPrefix("iterations", name) {
			continue
		}
		if !hasValue {
			if index+1 >= len(args) {
				return false
			}
			index++
			value = args[index]
		}
		return positiveDecimal(value)
	}
	return false
}

func positiveDecimal(value string) bool {
	n, err := strconv.Atoi(value)
	return err == nil && n > 0
}

func virshConsoleAttachCommand(args []string) bool {
	command := virshCommandName(args)
	if command == "" {
		return false
	}
	// virsh accepts unique command prefixes. Treat every potential prefix of
	// start/create as console-attaching so an abbreviation cannot bypass the
	// check in an asynchronous job.
	return strings.HasPrefix("start", command) || strings.HasPrefix("create", command)
}

func informationalInvocation(args []string) bool {
	return len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "--version" || args[0] == "-V")
}

func hasLongOption(args []string, name string) bool {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		provided, _, _ := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		if provided == "" {
			continue
		}
		// GNU getopt_long accepts unambiguous prefixes. Match those
		// conservatively so --daemoniz, --for and --inter cannot bypass the
		// full-option check. Full names of unrelated options do not match.
		if strings.HasPrefix(strings.TrimPrefix(name, "--"), provided) {
			return true
		}
	}
	return false
}

// hasShortFlag parses a conservative subset of getopt short-option clusters.
// noArgument lists the no-argument flags that may precede the target in the
// same token. Once an option that takes a value is encountered, the rest of
// that token is treated as its value and is not scanned as flags.
func hasShortFlag(args []string, flag byte, noArgument map[byte]bool) bool {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if len(arg) < 2 || arg[0] != '-' || arg[1] == '-' {
			continue
		}
		for i := 1; i < len(arg); i++ {
			if arg[i] == flag {
				return true
			}
			if !noArgument[arg[i]] {
				break
			}
		}
	}
	return false
}

func hasShortOption(args []string, name string) bool {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == name || (name == "-e" && strings.HasPrefix(arg, "-e") && len(arg) > 2) {
			return true
		}
	}
	return false
}

// hasShortOptionWithValue reports whether a short option that takes a value
// appears either as its own token or with an attached value. The scan is
// deliberately conservative for policy decisions: an option byte embedded in
// a cluster may be a false positive, but must never let a write escape the
// host serialization lock.
func hasShortOptionWithValue(args []string, option byte) bool {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if len(arg) >= 2 && arg[0] == '-' && arg[1] != '-' {
			for index := 1; index < len(arg); index++ {
				if arg[index] == option {
					return true
				}
			}
		}
	}
	return false
}
