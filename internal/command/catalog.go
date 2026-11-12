package command

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

//go:embed catalog_virsh.json
var virshCatalogJSON []byte

type virshDocEntry struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Syntax   string `json:"syntax"`
	Summary  string `json:"summary"`
}

var virshCategoryNames = map[string]string{
	"generic-commands":             "通用与宿主机",
	"domain-commands":              "虚拟机",
	"device-commands":              "设备热插拔",
	"snapshot-commands":            "快照",
	"checkpoint-commands":          "检查点",
	"backup-commands":              "备份",
	"storage-pool-commands":        "存储池",
	"volume-commands":              "存储卷",
	"virtual-network-commands":     "虚拟网络",
	"network-port-commands":        "网络端口",
	"interface-commands":           "主机接口",
	"nwfilter-commands":            "网络过滤器",
	"nwfilter-binding-commands":    "过滤器绑定",
	"secret-commands":              "Secret",
	"nodedev-commands":             "节点设备",
	"hypervisor-specific-commands": "QEMU 专用",
}

// #nosec G101 -- these are command names and explanatory text, not credentials.
var disabledVirsh = map[string]string{
	"help":               "交互式帮助命令；命令目录已直接展示全部帮助信息。",
	"quit":               "交互终端内建命令，异步任务中没有意义。",
	"exit":               "交互终端内建命令，异步任务中没有意义。",
	"cd":                 "交互终端内建命令，异步任务中没有意义。",
	"pwd":                "交互终端内建命令，异步任务中没有意义。",
	"connect":            "连接 URI 由主机配置管理，禁止从命令中心覆盖。",
	"console":            "交互式串口控制台无法作为异步任务运行；请使用内置 VNC 控制台。",
	"event":              "事件流会长期占用异步任务；请使用任务或审计页面。",
	"await":              "事件流会长期占用异步任务。",
	"net-event":          "事件流会长期占用异步任务。",
	"pool-event":         "事件流会长期占用异步任务。",
	"secret-event":       "事件流会长期占用异步任务。",
	"nodedev-event":      "事件流会长期占用异步任务。",
	"qemu-monitor-event": "事件流会长期占用异步任务。",
}

// emptyArgTools are commands whose default invocation is meaningful without
// any argv. Keep this explicit: an empty argv for virsh would open an
// interactive shell, while an empty argv for qemu-img is not a useful job.
var emptyArgTools = map[string]bool{
	"virt-pki-validate":    true,
	"virt-sanlock-cleanup": true,
	"virt-host-validate":   true,
	"virt-what":            true,
	"libguestfs-test-tool": true,
}

// AllowsEmptyArgs reports whether the tool has a documented zero-argument
// invocation that the command center may submit.
func AllowsEmptyArgs(tool string) bool { return emptyArgTools[tool] }

var readOnlyVirshNames = toSet(
	"version", "uri", "hostname", "sysinfo", "nodeinfo", "nodecpumap",
	"nodecpustats", "nodememstats", "nodesevinfo", "capabilities",
	"domcapabilities", "pool-capabilities", "list", "freecell", "freepages",
	"cpu-baseline", "cpu-compare", "cpu-models", "hypervisor-cpu-compare",
	"hypervisor-cpu-baseline", "hypervisor-cpu-models",
	"cpu-stats", "backup-dumpxml",
	"domthrottlegroupinfo", "domthrottlegrouplist", "domblkerror", "domblkinfo",
	"domblklist", "domblkstat", "domcontrol", "domdisplay", "domfsinfo",
	"domhostname", "domid", "domif-getlink", "domifaddr", "domiflist",
	"domifstat", "dominfo", "domjobinfo", "domlaunchsecinfo", "dommemstat",
	"domname", "domstate", "domstats", "domuuid", "domxml-from-native",
	"dumpxml", "domxml-to-native", "guestinfo", "guestvcpus", "iothreadinfo", "managedsave-dumpxml",
	"maxvcpus", "migrate-getmaxdowntime", "migrate-getspeed", "save-image-dumpxml",
	"ttyconsole", "vcpucount", "vcpuinfo", "vncdisplay", "nodefs",
	"nodedev-dumpxml", "nodedev-info", "nodedev-list",
	"net-dumpxml", "net-info", "net-list", "net-name", "net-uuid", "net-dhcp-leases",
	"net-port-list", "net-port-dumpxml",
	"iface-dumpxml", "iface-info", "iface-list", "iface-mac", "iface-name",
	"find-storage-pool-sources", "find-pool-sources-as", "pool-dumpxml", "pool-info", "pool-list", "pool-name", "pool-uuid",
	"vol-dumpxml", "vol-info", "vol-key", "vol-list", "vol-name", "vol-path", "vol-pool",
	"secret-dumpxml", "secret-list",
	"snapshot-current", "snapshot-dumpxml", "snapshot-info", "snapshot-list", "snapshot-parent",
	"checkpoint-dumpxml", "checkpoint-info", "checkpoint-list", "checkpoint-parent",
	"nwfilter-dumpxml", "nwfilter-list",
	"nwfilter-binding-dumpxml", "nwfilter-binding-list",
	"get-user-sshkeys",
)

var readOnlyQemuImg = toSet("info", "compare", "map", "measure")

var highRiskVirsh = toSet(
	"destroy", "reset", "undefine", "domrename", "blockcommit", "blockcopy",
	"blockpull", "blockresize", "change-media", "detach-device", "detach-device-alias",
	"detach-disk", "detach-interface", "dom-fd-associate", "domfsfreeze", "domfsthaw",
	"domfstrim", "inject-nmi", "lxc-enter-namespace", "managedsave", "migrate",
	"migrate-to-uri", "nodefsthaw", "nodefsfreeze", "nodesuspend", "qemu-attach",
	"qemu-agent-command", "qemu-monitor-command", "restore", "save", "send-key",
	"send-process-signal", "set-user-password", "set-user-sshkeys", "shutdown",
	"snapshot-delete", "snapshot-revert", "vol-delete", "vol-wipe", "net-destroy",
	"net-undefine", "pool-delete", "pool-destroy", "pool-undefine", "secret-set-value",
	"secret-undefine", "nodedev-destroy", "nodedev-reset", "iface-destroy",
	"iface-undefine", "nwfilter-undefine", "nwfilter-binding-delete", "domjobabort",
)

var catalogOnce sync.Once
var catalogItems []CatalogItem
var virshKnown = make(map[string]bool)
var virshReadOnly = make(map[string]bool)

// Catalog returns the complete documentation-derived command catalog. It is
// descriptive metadata only; Runner.Validate remains the execution boundary.
func Catalog() []CatalogItem {
	catalogOnce.Do(buildCatalog)
	return append([]CatalogItem(nil), catalogItems...)
}

func buildCatalog() {
	var entries []virshDocEntry
	if err := json.Unmarshal(virshCatalogJSON, &entries); err != nil {
		panic(fmt.Sprintf("decode embedded virsh catalog: %v", err))
	}
	items := make([]CatalogItem, 0, len(entries)+256)
	for _, entry := range entries {
		readOnly := readOnlyVirshNames[entry.Name]
		disabledReason := disabledVirsh[entry.Name]
		disabled := disabledReason != ""
		if strings.HasSuffix(entry.Name, "-edit") || entry.Name == "edit" {
			disabled = true
			disabledReason = "该命令需要交互式编辑器；请先导出 XML，修改后使用对应的 define/update 命令。"
		}
		options, positionals := AnalyzeSyntax(entry.Name, entry.Syntax)
		options, positionals = applyVirshOverrides(entry.Name, options, positionals)
		category := virshCategoryNames[entry.Category]
		if category == "" {
			category = "其他 libvirt 命令"
		}
		anchor := entry.Name
		if entry.Name == "quit" || entry.Name == "exit" {
			anchor = "quit-exit"
		}
		item := CatalogItem{
			Tool: "virsh", Command: entry.Name, Category: category,
			Summary: entry.Summary, Usage: entry.Syntax, ReadOnly: readOnly,
			Risk: riskForVirsh(entry.Name, readOnly), DocsURL: "https://libvirt.org/manpages/virsh.html#" + anchor,
			Sensitive: sensitiveVirsh(entry.Name, readOnly), Disabled: disabled,
			DisabledReason: disabledReason, ArgPrefix: []string{entry.Name},
			Options: options, Positionals: positionals,
		}
		items = append(items, item)
		virshKnown[entry.Name] = true
		if readOnly {
			virshReadOnly[entry.Name] = true
		}
	}
	items = append(items, qemuImgCatalog()...)
	items = append(items, qemuIOCatalog()...)
	items = append(items, qemuNbdCatalog()...)
	items = append(items, qemuToolsCatalog()...)
	items = append(items, virtCatalog()...)
	markUnsafeParameters(items)
	catalogItems = items
}

// markUnsafeParameters keeps browser-generated forms aligned with
// ValidateAsyncMode. These flags detach a child process from the job process
// group or wait for an interactive terminal, so they cannot be represented by
// a finite asynchronous job. The server check remains authoritative for raw
// argv submitted through the advanced field or a direct HTTP request.
func markUnsafeParameters(items []CatalogItem) {
	reasons := map[string]map[string]string{
		"qemu-storage-daemon": {
			"--daemonize": "后台化会脱离任务进程组；请以前台方式运行并由 systemd 或容器托管。",
		},
		"qemu-pr-helper": {
			"--daemon": "后台化会脱离任务进程组；请以前台方式运行并由 systemd 托管。",
		},
		"qemu-vmsr-helper": {
			"--daemon": "后台化会脱离任务进程组；请以前台方式运行并由 systemd 托管。",
		},
		"qemu-nbd": {
			"--fork": "后台化会脱离任务进程组；请使用前台导出并设置合理超时。",
		},
		"guestfish": {
			"--listen": "监听模式是长期服务，不能由异步任务托管；请在宿主机上单独运行。",
		},
		"virt-builder-repository": {
			"--interactive": "交互式模式需要终端输入；请改用非交互参数。",
		},
	}
	for i := range items {
		if items[i].Tool == "virsh" && (items[i].Command == "start" || items[i].Command == "create") {
			for j := range items[i].Options {
				if items[i].Options[j].Flag == "--console" {
					items[i].Options[j].Disabled = true
					items[i].Options[j].DisabledReason = "控制台会持续附着到串口；异步任务会一直等待。请启动后使用 Web VNC 控制台。"
				}
			}
		}
		if items[i].Tool == "guestmount" && items[i].Command == "mount" {
			items[i].Disabled = true
			items[i].DisabledReason = "guestmount 默认 fork 并持续等待卸载，无法由有限时长的异步任务托管；请使用 guestfish/virt-copy-*，guestunmount 仍可用于清理已有挂载。"
		}
		if items[i].Tool == "virt-top" && items[i].Command == "batch" {
			for j := range items[i].Options {
				if items[i].Options[j].Flag == "-b" || items[i].Options[j].Flag == "-n" {
					items[i].Options[j].Required = true
				}
			}
		}
		toolReasons := reasons[items[i].Tool]
		if len(toolReasons) == 0 {
			continue
		}
		for _, specs := range [][]ParameterSpec{items[i].Common, items[i].Options, items[i].Positionals} {
			for j := range specs {
				reason := toolReasons[specs[j].Flag]
				if reason == "" {
					continue
				}
				specs[j].Disabled = true
				specs[j].DisabledReason = reason
			}
		}
	}
}

func qemuImgCatalog() []CatalogItem {
	specs := []struct {
		name, syntax, summary, risk string
		readOnly                    bool
	}{
		{"amend", "amend [--object objectdef] [--image-opts] [-p] [-q] [-f fmt] [-t cache] [--force] -o options filename", "修改镜像格式相关元数据。", "极高", false},
		{"bench", "bench [-c count] [-d depth] [-f fmt] [--flush-interval=flush_interval] [-i aio] [-n] [--no-drain] [-o offset] [--pattern=pattern] [-q] [-s buffer_size] [-S step_size] [-t cache] [-w] [-U] filename", "对镜像执行顺序/随机 I/O 基准测试。", "高", false},
		{"bitmap", "bitmap (--merge SOURCE | --add | --remove | --clear | --enable | --disable)... [-b source_file [-F source_fmt]] [-g granularity] [--object objectdef] [--image-opts | -f fmt] filename bitmap", "管理 qcow2 持久 dirty bitmap。", "极高", false},
		{"check", "check [--object objectdef] [--image-opts] [-q] [-f fmt] [--output=ofmt] [-r [leaks | all]] [-T src_cache] [-U] filename", "检查 qcow2 镜像一致性；-r 可能执行修复。", "高", false},
		{"commit", "commit [--object objectdef] [--image-opts] [-q] [-f fmt] [-t cache] [-b base] [-r rate_limit] [-d] [-p] filename", "把镜像变更合并到后端镜像。", "极高", false},
		{"compare", "compare [--object objectdef] [--image-opts] [-f fmt] [-F fmt] [-T src_cache] [-p] [-q] [-s] [-U] filename1 filename2", "比较两个镜像内容。", "低", true},
		{"convert", "convert [--object objectdef] [--image-opts] [--target-image-opts] [--target-is-zero] [--bitmaps] [-U] [-C] [-c] [-p] [-q] [-n] [-f fmt] [-t cache] [-T src_cache] [-O output_fmt] [-B backing_file [-F backing_fmt]] [-o options] [-l snapshot_param] [-S sparse_size] [-r rate_limit] [-m num_coroutines] [-W] [--salvage] filename [filename2 [...]] output_filename", "转换或复制镜像。", "高", false},
		{"create", "create [--object objectdef] [-q] [-f fmt] [-b backing_file] [-F backing_fmt] [-u] [-o options] filename [size]", "创建新磁盘镜像。", "中", false},
		{"dd", "dd [--image-opts] [-U] [-f fmt] [-O output_fmt] [bs=block_size] [count=blocks] [skip=blocks] if=input of=output", "在镜像之间执行块级复制。", "极高", false},
		{"info", "info [--object objectdef] [--image-opts] [-f fmt] [--output=ofmt] [--backing-chain] [-U] filename", "显示镜像格式、虚拟大小和后端链。", "低", true},
		{"map", "map [--object objectdef] [--image-opts] [-f fmt] [--start-offset=offset] [--max-length=len] [--output=ofmt] [-U] filename", "显示镜像已分配区域。", "低", true},
		{"measure", "measure [--output=ofmt] [-O output_fmt] [-o options] [--size N | [--object objectdef] [--image-opts] [-f fmt] [-l snapshot_param] filename]", "估算转换后镜像大小。", "低", true},
		{"rebase", "rebase [--object objectdef] [--image-opts] [-U] [-q] [-f fmt] [-t cache] [-T src_cache] [-p] [-u] -b backing_file [-F backing_fmt] filename", "修改 qcow2 后端文件。", "极高", false},
		{"resize", "resize [--object objectdef] [--image-opts] [-f fmt] [--preallocation=prealloc] [-q] [--shrink] filename [+ | -]size", "调整镜像虚拟大小。", "高", false},
		{"snapshot", "snapshot [--object objectdef] [--image-opts] [-U] [-q] [-l | -a snapshot | -c snapshot | -d snapshot] filename", "列出、创建、应用或删除 qcow2 内部快照。", "高", false},
	}
	items := make([]CatalogItem, 0, len(specs))
	for _, spec := range specs {
		options, positionals := AnalyzeSyntax(spec.name, spec.syntax)
		if spec.name == "dd" {
			for i := range positionals {
				switch positionals[i].Name {
				case "bs":
					positionals[i].ValuePrefix = positionals[i].Name + "="
					positionals[i].Label = "bs="
					positionals[i].Placeholder = "块大小（如 4k）"
				case "count":
					positionals[i].ValuePrefix = "count="
					positionals[i].Label = "count="
					positionals[i].Placeholder = "块数"
				case "skip":
					positionals[i].ValuePrefix = "skip="
					positionals[i].Label = "skip="
					positionals[i].Placeholder = "跳过块数"
				case "if":
					positionals[i].ValuePrefix = "if="
					positionals[i].Label = "if="
					positionals[i].Placeholder = "输入镜像路径"
				case "of":
					positionals[i].ValuePrefix = "of="
					positionals[i].Label = "of="
					positionals[i].Placeholder = "输出镜像路径"
				}
			}
		}
		applyQemuImgOverrides(spec.name, &options, &positionals)
		items = append(items, CatalogItem{
			Tool: "qemu-img", Command: spec.name, Category: "QEMU 镜像", Summary: spec.summary,
			Usage: spec.syntax, ReadOnly: spec.readOnly, Risk: spec.risk,
			DocsURL: "https://www.qemu.org/docs/master/tools/qemu-img.html", Sensitive: true,
			ArgPrefix: []string{spec.name}, Common: qemuImgCommon(), Options: options, Positionals: positionals,
		})
	}
	items = append(items,
		CatalogItem{
			Tool: "qemu-img", Command: "help", Category: "QEMU 镜像", Summary: "显示 qemu-img 帮助和格式列表。",
			Usage: "--help", ReadOnly: true, Risk: "低", DocsURL: "https://www.qemu.org/docs/master/tools/qemu-img.html",
			ArgPrefix: []string{"--help"},
		},
		CatalogItem{
			Tool: "qemu-img", Command: "version", Category: "QEMU 镜像", Summary: "显示 qemu-img 版本。",
			Usage: "--version", ReadOnly: true, Risk: "低", DocsURL: "https://www.qemu.org/docs/master/tools/qemu-img.html",
			ArgPrefix: []string{"--version"},
		},
	)
	return items
}

func qemuImgCommon() []ParameterSpec {
	return []ParameterSpec{
		{Name: "trace", Flag: "-T", Label: "Trace 配置（全局选项）", Placeholder: "enable=... 或 events=...", ValueType: "text", Multiple: true},
	}
}

func applyVirshOverrides(name string, options, positionals []ParameterSpec) ([]ParameterSpec, []ParameterSpec) {
	switch name {
	case "allocpages":
		setOption(&options, "--pagesize", "页大小", "4K/2M/1G", "text", false, true)
		setOption(&options, "--pagecount", "页数量", "页数量", "number", false, true)
		setOption(&options, "--cellno", "NUMA 节点编号", "0", "number", false, false)
		positionals = dropPositionals(positionals, "pagesize", "pagecount", "cellno")
	case "freecell":
		setOption(&options, "--cellno", "NUMA 节点编号", "0", "number", false, false)
		positionals = dropPositionals(positionals, "cellno")
	case "freepages":
		setOption(&options, "--cellno", "NUMA 节点编号", "0", "number", false, false)
		setOption(&options, "--pagesize", "页大小", "4K/2M/1G", "text", false, false)
		positionals = dropPositionals(positionals, "cellno", "pagesize")
	case "domdisplay":
		setOption(&options, "--type", "显示类型", "vnc/spice/rdp", "text", false, false)
		positionals = dropPositionals(positionals, "type")
	case "domfsfreeze", "domfsthaw":
		setOption(&options, "--mountpoint", "挂载点（每行一个）", "/mnt/data", "text", true, false)
		positionals = dropPositionals(positionals, "mountpoint")
	case "schedinfo":
		setOption(&options, "--set", "调度参数（每行一个，格式 name=value）", "vcpu_period=1000", "text", true, false)
		positionals = dropPositionals(positionals, "parameter", "schedinfo")
	case "snapshot-create-as", "checkpoint-create-as":
		setOption(&options, "--diskspec", "磁盘规格（每行一个）", "vda,snapshot=external", "text", true, false)
		positionals = dropPositionals(positionals, "diskspec")
	case "snapshot-list":
		setOption(&options, "--from", "从指定快照开始", "snapshot-name", "text", false, false)
		positionals = dropPositionals(positionals, "snapshot")
	case "checkpoint-list":
		setOption(&options, "--from", "从指定检查点开始", "checkpoint-name", "text", false, false)
		positionals = dropPositionals(positionals, "checkpoint")
	case "domstats":
		positionals = dropPositionals(positionals, "y")
	}
	return options, positionals
}

func setOption(options *[]ParameterSpec, flag, label, placeholder, valueType string, multiple, required bool) {
	for i := range *options {
		if (*options)[i].Flag != flag {
			continue
		}
		(*options)[i].Label = label
		(*options)[i].Placeholder = placeholder
		(*options)[i].ValueType = valueType
		(*options)[i].Multiple = multiple
		(*options)[i].Required = required
		(*options)[i].OptionalValue = false
		return
	}
}

func dropPositionals(positionals []ParameterSpec, names ...string) []ParameterSpec {
	drop := make(map[string]bool, len(names))
	for _, name := range names {
		drop[name] = true
	}
	out := positionals[:0]
	for _, spec := range positionals {
		if !drop[spec.Name] {
			out = append(out, spec)
		}
	}
	return out
}

func applyQemuImgOverrides(name string, options *[]ParameterSpec, positionals *[]ParameterSpec) {
	switch name {
	case "bitmap":
		if !hasCatalogPositional(*positionals, "bitmap") {
			*positionals = append(*positionals, ParameterSpec{
				Name: "bitmap", Label: "Bitmap 名称", Placeholder: "bitmap", ValueType: "text", Required: true,
			})
		}
	case "convert":
		for i := range *positionals {
			if (*positionals)[i].Name != "filename2" {
				continue
			}
			(*positionals)[i].Label = "额外输入镜像（每行一个）"
			(*positionals)[i].Placeholder = "额外输入镜像路径（每行一个）"
			(*positionals)[i].Multiple = true
		}
	case "resize":
		filtered := (*positionals)[:0]
		for _, spec := range *positionals {
			if spec.Name != "+" && spec.Name != "-" {
				filtered = append(filtered, spec)
			}
		}
		*positionals = filtered
		for i := range *positionals {
			if (*positionals)[i].Name != "size" {
				continue
			}
			(*positionals)[i].Label = "调整后的大小（如 +10G 或 -5G）"
			(*positionals)[i].Placeholder = "+10G 或 -5G"
			(*positionals)[i].Required = true
		}
	}
	for i := range *options {
		switch (*options)[i].Flag {
		case "--object":
			(*options)[i].Multiple = true
		case "-t", "-T":
			setOptionChoices(&(*options)[i], "cache", cacheModeChoices)
		case "-i":
			setOptionChoices(&(*options)[i], "aio", aioModeChoices)
		case "--output":
			setOptionChoices(&(*options)[i], "human/json", []string{"human", "json"})
		case "--preallocation":
			setOptionChoices(&(*options)[i], "off/metadata/falloc/full", []string{"off", "metadata", "falloc", "full"})
		}
	}
}

var cacheModeChoices = []string{"none", "writeback", "writethrough", "directsync", "unsafe"}
var aioModeChoices = []string{"threads", "native", "io_uring"}

func setOptionChoices(spec *ParameterSpec, placeholder string, choices []string) {
	spec.ValueType = "choice"
	spec.Placeholder = placeholder
	spec.Choices = append([]string(nil), choices...)
}

func cloneChoices(choices []string) []string {
	return append([]string(nil), choices...)
}

func hasCatalogPositional(positionals []ParameterSpec, name string) bool {
	for _, spec := range positionals {
		if spec.Name == name {
			return true
		}
	}
	return false
}

func qemuIOCatalog() []CatalogItem {
	specs := []struct {
		name, syntax, summary, risk string
		needsFile                   bool
	}{
		{"abort", "abort", "模拟进程崩溃，仅用于测试。", "极高", false},
		{"aio_flush", "aio_flush", "等待全部异步请求完成。", "中", true},
		{"aio_read", "aio_read [-Ciqv] [-P pattern] off len [len..]", "发起异步读取。", "中", true},
		{"aio_write", "aio_write [-Cfiquz] [-P pattern] off len [len..]", "发起异步写入。", "高", true},
		{"alloc", "alloc offset [count]", "检查指定偏移是否已分配。", "中", true},
		{"break", "break event tag", "设置事件断点。", "中", false},
		{"close", "close", "关闭当前文件。", "中", false},
		{"discard", "discard [-Cq] off len", "丢弃指定范围。", "高", true},
		{"flush", "flush", "把内存中的文件状态刷到磁盘。", "中", true},
		{"help", "help [command]", "显示 qemu-io 命令帮助。", "低", false},
		{"info", "info", "显示当前文件信息。", "低", true},
		{"length", "length", "显示当前文件长度。", "低", true},
		{"map", "map", "显示文件已分配区域。", "低", true},
		{"open", "open [-rsCnkUi] [-t cache] [-d discard] [-o options] [path]", "打开一个新文件。", "中", false},
		{"quit", "quit", "退出 qemu-io 命令解释器。", "低", false},
		{"read", "read [-abCpqv] [-P pattern [-s off] [-l len]] off len", "读取指定范围。", "中", true},
		{"readv", "readv [-Cqv] [-P pattern] off len [len..]", "读取多个指定范围。", "中", true},
		{"remove_break", "remove_break tag", "删除事件断点。", "中", false},
		{"reopen", "reopen [(-r|-w)] [-c cache] [-o options]", "以新选项重新打开镜像。", "中", true},
		{"resume", "resume tag", "恢复被断点暂停的请求。", "中", false},
		{"sigraise", "sigraise signal", "向当前进程发送信号。", "极高", false},
		{"sleep", "sleep milliseconds", "等待指定毫秒数。", "低", false},
		{"truncate", "truncate [-m prealloc_mode] off", "截断当前文件。", "高", true},
		{"wait_break", "wait_break tag", "等待请求命中断点。", "中", false},
		{"write", "write [-bcCfnpquz] [-P pattern | -s source_file] off len", "写入指定范围。", "高", true},
		{"writev", "writev [-Cfq] [-P pattern] off len [len..]", "写入多个指定范围。", "高", true},
	}
	items := make([]CatalogItem, 0, len(specs))
	for _, spec := range specs {
		options, positionals := AnalyzeSyntax(spec.name, spec.syntax)
		applyQemuIOOverrides(&options)
		common := qemuIOCommon(spec.needsFile)
		if qemuIOStatefulCommands[spec.name] {
			common = append(common, qemuIOSequenceOptions()...)
		}
		items = append(items, CatalogItem{
			Tool: "qemu-io", Command: spec.name, Category: "QEMU I/O", Summary: spec.summary,
			Usage: spec.syntax, ReadOnly: false, Risk: spec.risk,
			DocsURL: "https://manpages.debian.org/trixie/qemu-utils/qemu-io.1.en.html", Sensitive: true,
			BuildMode: "qemu-io", Common: common, Options: options, Positionals: positionals,
		})
	}
	items = append(items,
		CatalogItem{
			Tool: "qemu-io", Command: "tool-help", Category: "QEMU I/O", Summary: "显示 qemu-io 工具级帮助。",
			Usage: "--help", ReadOnly: true, Risk: "低", DocsURL: "https://manpages.debian.org/trixie/qemu-utils/qemu-io.1.en.html",
			ArgPrefix: []string{"--help"},
		},
		CatalogItem{
			Tool: "qemu-io", Command: "tool-version", Category: "QEMU I/O", Summary: "显示 qemu-io 版本。",
			Usage: "--version", ReadOnly: true, Risk: "低", DocsURL: "https://manpages.debian.org/trixie/qemu-utils/qemu-io.1.en.html",
			ArgPrefix: []string{"--version"},
		},
	)
	return items
}

var qemuIOStatefulCommands = map[string]bool{
	"break": true, "remove_break": true, "resume": true, "wait_break": true,
}

func qemuIOSequenceOptions() []ParameterSpec {
	return []ParameterSpec{
		{
			Name: "commands_before", Label: "前置 qemu-io 命令（每行一条，先执行）",
			Placeholder: "break write 0\nwait_break write", ValueType: "textarea", Multiple: true,
		},
		{
			Name: "commands_after", Label: "后续 qemu-io 命令（每行一条，后执行）",
			Placeholder: "aio_write 0 4096\nwait_break write", ValueType: "textarea", Multiple: true,
		},
	}
}

func applyQemuIOOverrides(options *[]ParameterSpec) {
	for i := range *options {
		spec := &(*options)[i]
		if spec.ValueType == "bool" {
			continue
		}
		switch spec.Placeholder {
		case "cache":
			setOptionChoices(spec, "cache", cacheModeChoices)
		case "discard":
			setOptionChoices(spec, "discard", []string{"ignore", "unmap"})
		case "prealloc_mode":
			setOptionChoices(spec, "off/metadata/falloc/full", []string{"off", "metadata", "falloc", "full"})
		}
	}
}

func qemuIOCommon(needsFile bool) []ParameterSpec {
	return []ParameterSpec{
		{Name: "image", Label: "镜像/块设备路径", Placeholder: "/var/lib/libvirt/images/disk.qcow2", ValueType: "text", Required: needsFile},
		{Name: "format", Flag: "-f", Label: "镜像格式", Placeholder: "qcow2", ValueType: "text"},
		{Name: "readonly", Flag: "-r", Label: "只读打开", ValueType: "bool"},
		{Name: "snapshot", Flag: "-s", Label: "使用临时快照", ValueType: "bool"},
		{Name: "nocache", Flag: "-n", Label: "禁用宿主缓存", ValueType: "bool"},
		{Name: "forceShare", Flag: "-U", Label: "允许共享打开", ValueType: "bool"},
		{Name: "imageOpts", Flag: "--image-opts", Label: "FILE 作为完整 image options", ValueType: "bool"},
		{Name: "copyOnRead", Flag: "-C", Label: "copy-on-read", ValueType: "bool"},
		{Name: "misalign", Flag: "-m", Label: "允许非对齐 I/O", ValueType: "bool"},
		{Name: "nativeAIO", Flag: "-k", Label: "使用 native AIO", ValueType: "bool"},
		{Name: "cache", Flag: "-t", Label: "缓存模式", ValueType: "choice", Choices: cloneChoices(cacheModeChoices)},
		{Name: "aio", Flag: "-i", Label: "AIO 模式", ValueType: "choice", Choices: cloneChoices(aioModeChoices)},
		{Name: "discard", Flag: "-d", Label: "Discard 模式", ValueType: "choice", Choices: []string{"ignore", "unmap"}},
		{Name: "trace", Flag: "-T", Label: "Trace 配置", ValueType: "text"},
		{Name: "object", Flag: "--object", Label: "QEMU object 定义", ValueType: "text", Multiple: true},
	}
}

func qemuNbdCatalog() []CatalogItem {
	common := qemuNbdOptions()
	file := ParameterSpec{Name: "file", Label: "镜像/块设备路径", Placeholder: "/var/lib/libvirt/images/disk.qcow2", ValueType: "text", Required: true}
	device := ParameterSpec{Name: "device", Label: "NBD 设备", Placeholder: "/dev/nbd0", ValueType: "text", Required: true}
	connectOptions := append(qemuNbdConnectOptions(), ParameterSpec{Name: "connect", Flag: "--connect", Label: "连接设备", Placeholder: "/dev/nbd0", ValueType: "text", Required: true})
	return []CatalogItem{
		{Tool: "qemu-nbd", Command: "serve", Category: "QEMU NBD", Summary: "通过 TCP 或 Unix socket 导出镜像。", Usage: "[OPTIONS] FILE", Risk: "高", DocsURL: "https://www.qemu.org/docs/master/tools/qemu-nbd.html", Sensitive: true, Options: cloneParameters(common), Positionals: []ParameterSpec{file}},
		{Tool: "qemu-nbd", Command: "list", Category: "QEMU NBD", Summary: "列出另一个 NBD 服务导出的设备。", Usage: "-L [OPTIONS]", ReadOnly: true, Risk: "中", DocsURL: "https://www.qemu.org/docs/master/tools/qemu-nbd.html", Sensitive: true, ArgPrefix: []string{"--list"}, Options: qemuNbdListOptions()},
		{Tool: "qemu-nbd", Command: "connect", Category: "QEMU NBD", Summary: "把镜像连接到本地内核 NBD 设备。", Usage: "-c DEV [OPTIONS] FILE", Risk: "高", DocsURL: "https://www.qemu.org/docs/master/tools/qemu-nbd.html", Sensitive: true, Options: connectOptions, Positionals: []ParameterSpec{file}},
		{Tool: "qemu-nbd", Command: "disconnect", Category: "QEMU NBD", Summary: "断开本地内核 NBD 设备。", Usage: "-d DEV", Risk: "高", DocsURL: "https://www.qemu.org/docs/master/tools/qemu-nbd.html", Sensitive: true, ArgPrefix: []string{"--disconnect"}, Positionals: []ParameterSpec{device}},
		{Tool: "qemu-nbd", Command: "help", Category: "QEMU NBD", Summary: "显示 qemu-nbd 帮助和版本。", Usage: "--help", ReadOnly: true, Risk: "低", DocsURL: "https://www.qemu.org/docs/master/tools/qemu-nbd.html", Sensitive: true, ArgPrefix: []string{"--help"}},
		{Tool: "qemu-nbd", Command: "version", Category: "QEMU NBD", Summary: "显示 qemu-nbd 版本。", Usage: "--version", ReadOnly: true, Risk: "低", DocsURL: "https://www.qemu.org/docs/master/tools/qemu-nbd.html", Sensitive: true, ArgPrefix: []string{"--version"}},
	}
}

func qemuNbdOptions() []ParameterSpec {
	return []ParameterSpec{
		{Name: "port", Flag: "--port", Label: "监听端口", Placeholder: "10809", ValueType: "number"},
		{Name: "bind", Flag: "--bind", Label: "绑定地址", Placeholder: "127.0.0.1", ValueType: "text"},
		{Name: "socket", Flag: "--socket", Label: "Unix socket", Placeholder: "/run/wvc/disk.sock", ValueType: "text"},
		{Name: "shared", Flag: "--shared", Label: "共享客户端数", Placeholder: "1", ValueType: "number"},
		{Name: "persistent", Flag: "--persistent", Label: "最后一个客户端断开后保持运行", ValueType: "bool"},
		{Name: "verbose", Flag: "--verbose", Label: "详细日志", ValueType: "bool"},
		{Name: "exportName", Flag: "--export-name", Label: "导出名称", ValueType: "text"},
		{Name: "description", Flag: "--description", Label: "导出描述", ValueType: "text"},
		{Name: "offset", Flag: "--offset", Label: "导出起始偏移", Placeholder: "0", ValueType: "text"},
		{Name: "allocationDepth", Flag: "--allocation-depth", Label: "暴露 allocation depth", ValueType: "bool"},
		{Name: "bitmap", Flag: "--bitmap", Label: "暴露 dirty bitmap", ValueType: "text"},
		{Name: "object", Flag: "--object", Label: "QEMU object 定义", ValueType: "text", Multiple: true},
		{Name: "tlsCreds", Flag: "--tls-creds", Label: "TLS object ID", ValueType: "text"},
		{Name: "tlsAuthz", Flag: "--tls-authz", Label: "TLS 授权 object ID", ValueType: "text"},
		{Name: "trace", Flag: "--trace", Label: "Trace 配置", ValueType: "text"},
		{Name: "handshakeLimit", Flag: "--handshake-limit", Label: "握手超时秒数（0 表示不限制）", Placeholder: "10", ValueType: "number"},
		{Name: "fork", Flag: "--fork", Label: "后台运行（会产生独立进程）", ValueType: "bool"},
		{Name: "pidFile", Flag: "--pid-file", Label: "PID 文件", ValueType: "text"},
		{Name: "selinuxLabel", Flag: "--selinux-label", Label: "SELinux socket 标签", ValueType: "text"},
		{Name: "format", Flag: "--format", Label: "镜像格式", Placeholder: "qcow2", ValueType: "text"},
		{Name: "readonly", Flag: "--read-only", Label: "只读导出", ValueType: "bool"},
		{Name: "snapshot", Flag: "--snapshot", Label: "临时快照导出", ValueType: "bool"},
		{Name: "loadSnapshot", Flag: "--load-snapshot", Label: "加载内部快照", ValueType: "text"},
		{Name: "nocache", Flag: "--nocache", Label: "禁用宿主缓存", ValueType: "bool"},
		{Name: "cache", Flag: "--cache", Label: "缓存模式", ValueType: "choice", Choices: []string{"none", "writeback", "writethrough", "directsync", "unsafe"}},
		{Name: "aio", Flag: "--aio", Label: "AIO 模式", ValueType: "choice", Choices: []string{"threads", "native", "io_uring"}},
		{Name: "discard", Flag: "--discard", Label: "Discard 模式", ValueType: "choice", Choices: []string{"ignore", "unmap"}},
		{Name: "detectZeroes", Flag: "--detect-zeroes", Label: "零块检测", ValueType: "choice", Choices: []string{"off", "on", "unmap"}},
		{Name: "imageOpts", Flag: "--image-opts", Label: "FILE 作为完整 image options", ValueType: "bool"},
	}
}

func qemuNbdListOptions() []ParameterSpec {
	return []ParameterSpec{
		{Name: "port", Flag: "--port", Label: "NBD 端口", Placeholder: "10809", ValueType: "number"},
		{Name: "bind", Flag: "--bind", Label: "NBD 地址", Placeholder: "127.0.0.1", ValueType: "text"},
		{Name: "socket", Flag: "--socket", Label: "Unix socket", Placeholder: "/run/wvc/disk.sock", ValueType: "text"},
		{Name: "tlsCreds", Flag: "--tls-creds", Label: "TLS object ID", ValueType: "text"},
		{Name: "tlsHostname", Flag: "--tls-hostname", Label: "TLS 证书校验主机名", ValueType: "text"},
		{Name: "trace", Flag: "--trace", Label: "Trace 配置", ValueType: "text"},
		{Name: "object", Flag: "--object", Label: "QEMU object 定义", ValueType: "text", Multiple: true},
	}
}

func qemuNbdConnectOptions() []ParameterSpec {
	return []ParameterSpec{
		{Name: "format", Flag: "--format", Label: "镜像格式", Placeholder: "qcow2", ValueType: "text"},
		{Name: "readonly", Flag: "--read-only", Label: "只读连接", ValueType: "bool"},
		{Name: "snapshot", Flag: "--snapshot", Label: "使用临时快照", ValueType: "bool"},
		{Name: "loadSnapshot", Flag: "--load-snapshot", Label: "加载内部快照", ValueType: "text"},
		{Name: "nocache", Flag: "--nocache", Label: "禁用宿主缓存", ValueType: "bool"},
		{Name: "cache", Flag: "--cache", Label: "缓存模式", ValueType: "choice", Choices: []string{"none", "writeback", "writethrough", "directsync", "unsafe"}},
		{Name: "aio", Flag: "--aio", Label: "AIO 模式", ValueType: "choice", Choices: []string{"threads", "native", "io_uring"}},
		{Name: "discard", Flag: "--discard", Label: "Discard 模式", ValueType: "choice", Choices: []string{"ignore", "unmap"}},
		{Name: "detectZeroes", Flag: "--detect-zeroes", Label: "零块检测", ValueType: "choice", Choices: []string{"off", "on", "unmap"}},
		{Name: "imageOpts", Flag: "--image-opts", Label: "FILE 作为完整 image options", ValueType: "bool"},
		{Name: "object", Flag: "--object", Label: "QEMU object 定义", ValueType: "text", Multiple: true},
		{Name: "trace", Flag: "--trace", Label: "Trace 配置", ValueType: "text"},
		{Name: "persistent", Flag: "--persistent", Label: "保持服务运行", ValueType: "bool"},
		{Name: "shared", Flag: "--shared", Label: "共享客户端数", ValueType: "number"},
	}
}

func cloneParameters(in []ParameterSpec) []ParameterSpec {
	return append([]ParameterSpec(nil), in...)
}

func riskForVirsh(name string, readOnly bool) string {
	if readOnly {
		return "低"
	}
	if highRiskVirsh[name] {
		return "高"
	}
	for _, marker := range []string{"destroy", "delete", "undefine", "remove", "wipe", "reset", "abort"} {
		if strings.Contains(name, marker) {
			return "高"
		}
	}
	return "中"
}

func sensitiveVirsh(name string, readOnly bool) bool {
	if !readOnly {
		return true
	}
	switch name {
	case "secret-get-value", "get-user-sshkeys", "guestinfo", "guestvcpus", "domfsinfo",
		"domusers", "domifaddr", "domlaunchsecinfo", "dumpxml", "domxml-to-native",
		"snapshot-dumpxml", "pool-dumpxml", "vol-dumpxml", "net-dumpxml",
		"secret-dumpxml", "iface-dumpxml", "nwfilter-dumpxml", "nodedev-dumpxml",
		"checkpoint-dumpxml", "managedsave-dumpxml", "save-image-dumpxml":
		return true
	}
	return false
}

// Sensitive reports commands that expose secrets, execute arbitrary guest
// code, perform raw disk I/O, or access arbitrary local files.
func Sensitive(tool string, args []string) (bool, string) {
	catalogOnce.Do(buildCatalog)
	if len(args) == 0 {
		if isVirtCatalogTool(tool) {
			_, _, sensitive := virtCatalogPolicy(tool, args)
			if sensitive {
				return true, "virt/libguestfs 命令可读取或修改虚拟机磁盘、宿主配置及 guest 数据"
			}
		}
		return false, ""
	}
	if tool == "virsh" {
		readOnly, known := Classify(tool, args)
		if !known || !readOnly {
			return true, "修改 libvirt/QEMU 状态、磁盘、网络或宿主机资源"
		}
		switch virshCommandName(args) {
		case "secret-get-value":
			return true, "导出 libvirt secret 明文"
		case "dumpxml", "domxml-to-native", "snapshot-dumpxml", "pool-dumpxml",
			"vol-dumpxml", "net-dumpxml", "secret-dumpxml", "iface-dumpxml",
			"nwfilter-dumpxml", "nodedev-dumpxml", "checkpoint-dumpxml",
			"managedsave-dumpxml", "save-image-dumpxml":
			return true, "XML 可能包含密钥、路径和宿主配置"
		case "guestinfo", "guestvcpus", "domfsinfo", "domusers", "domifaddr", "get-user-sshkeys", "domlaunchsecinfo":
			return true, "guest agent/用户信息可能包含敏感数据"
		}
		return false, ""
	}
	if isVirtCatalogTool(tool) {
		_, _, sensitive := virtCatalogPolicy(tool, args)
		if sensitive {
			return true, "virt/libguestfs 命令可读取或修改虚拟机磁盘、宿主配置及 guest 数据"
		}
		return false, ""
	}
	switch tool {
	case "qemu-img":
		commandName, informational, _ := qemuImgCommand(args)
		if informational || commandName == "help" || commandName == "version" {
			return false, ""
		}
		return true, "可读取或修改宿主上的任意镜像文件"
	case "qemu-io":
		if args[0] == "--help" || args[0] == "-h" || args[0] == "--version" || args[0] == "-V" {
			return false, ""
		}
		return true, "允许原始块设备 I/O 或网络导出"
	case "qemu-nbd":
		mode, _ := qemuNbdMode(args)
		if mode == "help" || mode == "version" {
			return false, ""
		}
		return true, "允许原始块设备 I/O 或网络导出"
	case "qemu-trace-stap":
		commandName, informational, _ := qemuTraceSTAPCommand(args)
		if informational || commandName == "list" {
			return false, ""
		}
		return true, "SystemTap 跟踪会话可观察宿主上的 QEMU 进程"
	case "qemu-storage-daemon", "qemu-pr-helper", "qemu-vmsr-helper", "qemu-vnc":
		if informationalInvocation(args) {
			return false, ""
		}
		return true, "启动宿主级 QEMU 服务、socket 或导出"
	}
	return true, "高级命令"
}

func Classify(tool string, args []string) (readOnly bool, known bool) {
	catalogOnce.Do(buildCatalog)
	if len(args) == 0 {
		if isVirtCatalogTool(tool) {
			readOnly, known, _ := virtCatalogPolicy(tool, args)
			return readOnly, known
		}
		return false, false
	}
	switch tool {
	case "virsh":
		name := virshCommandName(args)
		_, ok := virshReadOnly[name]
		if ok {
			return true, true
		}
		return false, virshKnown[name]
	case "qemu-img":
		commandName, informational, known := qemuImgCommand(args)
		if informational || commandName == "help" || commandName == "version" {
			return true, true
		}
		_, ok := readOnlyQemuImg[commandName]
		if ok {
			return true, true
		}
		return false, known
	case "qemu-nbd":
		mode, known := qemuNbdMode(args)
		if mode == "list" || mode == "help" || mode == "version" {
			return true, true
		}
		return false, known
	case "qemu-io":
		if args[0] == "--help" || args[0] == "-h" || args[0] == "--version" || args[0] == "-V" {
			return true, true
		}
	case "qemu-storage-daemon", "qemu-pr-helper", "qemu-vmsr-helper", "qemu-vnc":
		if args[0] == "--help" || args[0] == "-h" || args[0] == "--version" || args[0] == "-V" {
			return true, true
		}
		return false, true
	case "qemu-trace-stap":
		commandName, informational, known := qemuTraceSTAPCommand(args)
		if informational || commandName == "list" {
			return true, true
		}
		return false, known
	}
	if isVirtCatalogTool(tool) {
		readOnly, known, _ := virtCatalogPolicy(tool, args)
		return readOnly, known
	}
	return false, false
}

// Blocked reports catalogued commands that cannot run as detached jobs, such
// as interactive consoles, editors and one-shot shells. The browser disables
// their forms, but Validate enforces the same rule for direct HTTP requests and
// queued jobs.

func boundedEventQuery(args []string) bool {
	for index, arg := range args {
		if arg == "--timeout" && index+1 < len(args) {
			timeout, err := strconv.Atoi(args[index+1])
			return err == nil && timeout > 0 && timeout <= 5
		}
		if strings.HasPrefix(arg, "--timeout=") {
			timeout, err := strconv.Atoi(strings.TrimPrefix(arg, "--timeout="))
			return err == nil && timeout > 0 && timeout <= 5
		}
	}
	return false
}

func Blocked(tool string, args []string) (bool, string) {
	if len(args) == 0 {
		return false, ""
	}
	if len(args) == 1 {
		switch args[0] {
		case "--help", "-h", "--version", "-V":
			return false, ""
		}
	}
	if tool == "virsh" {
		if name := virshCommandName(args); name != "" {
			if boundedEventQuery(args) && (name == "event" || strings.HasPrefix("event", name)) {
				return false, ""
			}
			if reason, ok := disabledVirsh[name]; ok {
				return true, reason
			}
			// libvirt resolves unambiguous command-name prefixes (for example
			// "cons" selects "console"). Reject every prefix of an interactive
			// command so abbreviations cannot bypass the asynchronous-job
			// boundary.
			for disabledName, reason := range disabledVirsh {
				if strings.HasPrefix(disabledName, name) {
					return true, reason
				}
			}
			for _, item := range Catalog() {
				if item.Tool != "virsh" || !item.Disabled || len(item.ArgPrefix) != 1 {
					continue
				}
				if strings.HasPrefix(item.ArgPrefix[0], name) {
					return true, item.DisabledReason
				}
			}
		}
	}
	for _, item := range Catalog() {
		if item.Tool != tool || !item.Disabled || !prefixMatches(item.ArgPrefix, args) {
			continue
		}
		return true, item.DisabledReason
	}
	return false, ""
}

func qemuImgKnown(name string) bool {
	switch name {
	case "amend", "bench", "bitmap", "check", "commit", "compare", "convert", "create", "dd", "info", "map", "measure", "rebase", "resize", "snapshot":
		return true
	}
	return false
}

func toSet(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}
