package command

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"webvirtcloud-go/internal/remote"
)

var helpCommandLine = regexp.MustCompile(`^\s{2,}([a-z][a-z0-9-]*)(?:\s{2,}.*)?$`)
var virshGroupNameLine = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
var virshGroupKeywordLine = regexp.MustCompile(`\(help keyword '([a-z][a-z0-9-]*)'\)`)

const discoveryWorkers = 12

// Discover probes the installed tools without connecting to a libvirt host.
// Discovery is advisory: a command absent from the current client's help can
// still be submitted through the advanced argv path for version differences.
func Discover(ctx context.Context, runner *Runner) Discovery {
	result := Discovery{Tools: make(map[string]ToolCapability)}
	if runner == nil {
		return result
	}
	tools := runner.Tools()
	capabilities := make([]ToolCapability, len(tools))
	sem := make(chan struct{}, discoveryWorkers)
	var wg sync.WaitGroup
	for i, tool := range tools {
		i, tool := i, tool
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				capabilities[i] = discoverTool(ctx, runner, tool)
			case <-ctx.Done():
				capabilities[i] = ToolCapability{Name: tool.Name, Available: tool.Available, Error: "探测已取消"}
			}
		}()
	}
	wg.Wait()
	for _, capability := range capabilities {
		result.Tools[capability.Name] = capability
	}
	return result
}

func discoverTool(ctx context.Context, runner *Runner, tool Tool) ToolCapability {
	capability := ToolCapability{Name: tool.Name, Available: tool.Available}
	if !tool.Available {
		capability.Error = "工具未安装"
		return capability
	}
	capability.Version = probeVersion(ctx, runner, tool.Name)
	switch tool.Name {
	case "virsh":
		capability.Commands, capability.Error = probeVirshCommands(ctx, runner)
	case "qemu-img":
		capability.Commands, capability.Error = probeQemuImgCommands(ctx, runner)
	case "qemu-io":
		capability.Commands, capability.Error = probeQemuIOCommands(ctx, runner)
	case "qemu-nbd":
		capability.Commands = map[string]bool{"serve": true, "list": true, "connect": true, "disconnect": true, "help": true, "version": true}
	case "qemu-storage-daemon", "qemu-pr-helper", "qemu-vmsr-helper", "qemu-vnc":
		capability.Commands = map[string]bool{"serve": true, "help": true, "version": true}
	case "qemu-trace-stap":
		capability.Commands = map[string]bool{"list": true, "run": true}
	case "virt-what", "virt-top":
		capability.Commands = catalogCommandSet(tool.Name)
	default:
		if isVirtCatalogTool(tool.Name) {
			capability.Commands = catalogCommandSet(tool.Name)
		}
	}
	return capability
}

func catalogCommandSet(tool string) map[string]bool {
	commands := make(map[string]bool)
	for _, item := range Catalog() {
		if item.Tool == tool {
			commands[item.Command] = true
		}
	}
	return commands
}

func ApplyDiscovery(items []CatalogItem, discovery Discovery) []CatalogItem {
	out := append([]CatalogItem(nil), items...)
	for i := range out {
		tool, ok := discovery.Tools[out[i].Tool]
		if !ok {
			out[i].ToolAvailable = false
			out[i].ProbeState = "unknown"
			continue
		}
		out[i].ToolAvailable = tool.Available
		if len(tool.Commands) == 0 {
			out[i].ProbeState = "unknown"
			continue
		}
		if tool.Commands[out[i].Command] {
			out[i].ProbeState = "advertised"
		} else {
			out[i].ProbeState = "missing"
		}
	}
	return out
}

// DiscoverRemote probes tool presence on a qemu+ssh target with one hardened
// SSH command. Versions are intentionally omitted: opening dozens of SSH
// sessions just to render a menu would amplify load against every KVM host.
// On a transport failure, tools are left selectable and the error is surfaced
// so a temporary network outage does not silently disable the command center.
func DiscoverRemote(ctx context.Context, runner *Runner, target remote.Target, timeout time.Duration) (Discovery, error) {
	result := Discovery{Tools: make(map[string]ToolCapability)}
	if runner == nil {
		return result, fmt.Errorf("command runner is unavailable")
	}
	tools := runner.Tools()
	if runner.sshPath == "" {
		for _, tool := range tools {
			result.Tools[tool.Name] = ToolCapability{Name: tool.Name, Available: false, Error: "OpenSSH 客户端未安装，无法执行远程命令"}
		}
		return result, remote.ErrNoSSHClient
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	sshArgs, err := target.CommandArgs(remote.PresenceScript(runner.remotePATH, names))
	if err != nil {
		return result, err
	}
	if timeout <= 0 || timeout > 10*time.Second {
		timeout = 10 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	probeResult, probeErr := runner.runProcess(runCtx, timeout, runner.sshPath, sshArgs, "", []string{"ssh", target.Destination(), "--", "command -v"})
	present := make(map[string]bool)
	for _, line := range strings.Split(probeResult.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			present[line] = true
		}
	}
	for _, tool := range tools {
		capability := ToolCapability{Name: tool.Name, Available: present[tool.Name]}
		if probeErr != nil {
			capability.Available = true
			capability.Error = "目标主机能力探测失败：" + probeErr.Error()
		} else if !capability.Available {
			capability.Error = "目标主机 PATH 中未安装"
		} else {
			capability.Note = "目标主机已安装（轻量探测，未读取版本和子命令）"
		}
		result.Tools[tool.Name] = capability
	}
	return result, probeErr
}

func probeVersion(parent context.Context, runner *Runner, tool string) string {
	out, err := probe(parent, runner, tool, "--version")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func probeVirshCommands(ctx context.Context, runner *Runner) (map[string]bool, string) {
	out, err := probe(ctx, runner, "virsh", "help")
	if err != nil {
		return nil, "无法读取 virsh help：" + err.Error()
	}
	groups := parseVirshGroups(out)
	if len(groups) == 0 {
		return nil, "virsh help 未返回命令分组"
	}
	commands := make(map[string]bool)
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, group := range groups {
		group := group
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			groupCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			defer cancel()
			help, err := probe(groupCtx, runner, "virsh", "help", group)
			if err != nil {
				return
			}
			found := parseVirshCommandHelp(help, virshKnown)
			mu.Lock()
			for name := range found {
				commands[name] = true
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		if len(commands) > 0 {
			return commands, "命令支持探测未完整完成：" + err.Error()
		}
		return nil, "命令支持探测超时：" + err.Error()
	}
	if len(commands) == 0 {
		return nil, "virsh help 未返回可识别的命令"
	}
	return commands, ""
}

func parseVirshGroups(out string) []string {
	keywordGroups := make([]string, 0, 16)
	keywordSeen := make(map[string]bool)
	for _, line := range strings.Split(out, "\n") {
		match := virshGroupKeywordLine.FindStringSubmatch(line)
		if len(match) != 2 || keywordSeen[match[1]] {
			continue
		}
		keywordSeen[match[1]] = true
		keywordGroups = append(keywordGroups, match[1])
	}
	if len(keywordGroups) > 0 {
		return keywordGroups
	}

	// Older libvirt versions printed a flat "Group of commands" table.
	groups := make([]string, 0, 16)
	seen := make(map[string]bool)
	inGroups := false
	sawEntry := false
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "group of commands") || strings.Contains(lower, "grouped commands") {
			inGroups = true
			continue
		}
		if !inGroups {
			continue
		}
		if trimmed == "" {
			if sawEntry {
				break
			}
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		name := strings.ToLower(fields[0])
		if !virshGroupNameLine.MatchString(name) || seen[name] {
			continue
		}
		if name == "command" || name == "commands" {
			continue
		}
		seen[name] = true
		groups = append(groups, name)
		sawEntry = true
	}
	return groups
}

func parseVirshCommandHelp(out string, known map[string]bool) map[string]bool {
	commands := make(map[string]bool)
	for _, line := range strings.Split(out, "\n") {
		match := helpCommandLine.FindStringSubmatch(line)
		if len(match) < 2 {
			continue
		}
		name := match[1]
		if known == nil || known[name] {
			commands[name] = true
		}
	}
	return commands
}

func probeQemuImgCommands(ctx context.Context, runner *Runner) (map[string]bool, string) {
	out, err := probe(ctx, runner, "qemu-img", "--help")
	if err != nil {
		return nil, "无法读取 qemu-img --help：" + err.Error()
	}
	commands := map[string]bool{"help": true, "version": true}
	inSyntax := false
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "Command syntax:" || strings.HasPrefix(trimmed, "Recognized commands") {
			inSyntax = true
			continue
		}
		if !inSyntax || trimmed == "" {
			continue
		}
		name := strings.Fields(trimmed)[0]
		if qemuImgKnown(name) {
			commands[name] = true
		}
	}
	if len(commands) <= 2 {
		return nil, "qemu-img --help 未返回命令列表"
	}
	return commands, ""
}

func probeQemuIOCommands(ctx context.Context, runner *Runner) (map[string]bool, string) {
	out, err := probe(ctx, runner, "qemu-io", "-c", "help")
	if err != nil {
		return nil, "无法读取 qemu-io -c help：" + err.Error()
	}
	commands := map[string]bool{"tool-help": true, "tool-version": true}
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "Use 'help") {
			continue
		}
		name := strings.Fields(trimmed)[0]
		if knownQemuIOCommand(name) {
			commands[name] = true
		}
	}
	if len(commands) <= 2 {
		return nil, "qemu-io -c help 未返回命令列表"
	}
	return commands, ""
}

func knownQemuIOCommand(name string) bool {
	switch name {
	case "abort", "aio_flush", "aio_read", "aio_write", "alloc", "break", "close",
		"discard", "flush", "help", "info", "length", "map", "open", "quit", "read",
		"readv", "remove_break", "reopen", "resume", "sigraise", "sleep", "truncate",
		"wait_break", "write", "writev":
		return true
	}
	return false
}

func probe(ctx context.Context, runner *Runner, tool string, args ...string) (string, error) {
	path, ok := runner.paths[tool]
	if !ok {
		return "", exec.ErrNotFound
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	// #nosec G204 -- path belongs to the configured command allowlist and the probe argv is fixed.
	cmd := exec.CommandContext(probeCtx, path, args...)
	cmd.Env = runnerEnv()
	out, err := cmd.CombinedOutput()
	if probeCtx.Err() != nil {
		return "", probeCtx.Err()
	}
	return sanitizeCommandText(string(out)), err
}
