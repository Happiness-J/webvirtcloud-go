package command

import "strings"

const (
	virtCategoryLibvirt  = "libvirt 管理与校验"
	virtCategoryVirtinst = "virtinst：创建与编辑"
	virtCategoryGuestfs  = "libguestfs：磁盘与 Guest"
	virtCategoryMigrate  = "迁移与物理机"
	virtCategoryInspect  = "宿主机检测与监控"
)

var virtCatalogToolSet = func() map[string]bool {
	out := make(map[string]bool, 64)
	for _, name := range []string{
		"virt-admin", "virt-host-validate", "virt-qemu-run", "virt-login-shell",
		"virt-xml-validate", "virt-pki-validate", "virt-sanlock-cleanup",
		"virt-install", "virt-clone", "virt-convert", "virt-xml",
		"guestfish", "guestmount", "guestunmount",
		"libguestfs-test-tool", "libguestfs-make-fixed-appliance",
		"virt-alignment-scan", "virt-builder", "virt-builder-repository",
		"virt-cat", "virt-copy-in", "virt-copy-out", "virt-customize", "virt-df",
		"virt-dib", "virt-diff", "virt-edit", "virt-filesystems", "virt-format",
		"virt-get-kernel", "virt-inspector", "virt-list-filesystems",
		"virt-list-partitions", "virt-log", "virt-ls", "virt-make-fs",
		"virt-p2v", "virt-p2v-make-disk", "virt-p2v-make-kickstart", "virt-rescue",
		"virt-resize", "virt-sparsify", "virt-sysprep", "virt-tail", "virt-tar",
		"virt-tar-in", "virt-tar-out", "virt-v2v", "virt-v2v-in-place",
		"virt-v2v-inspector", "virt-v2v-open", "virt-win-reg",
		"virt-what", "virt-top",
	} {
		out[name] = true
	}
	return out
}()

func isVirtCatalogTool(tool string) bool { return virtCatalogToolSet[tool] }

func virtCatalog() []CatalogItem {
	items := make([]CatalogItem, 0, 160)
	items = append(items, virtAdminCatalog()...)
	items = append(items, virtHostToolsCatalog()...)
	items = append(items, virtinstCatalog()...)
	items = append(items, libguestfsCatalog()...)
	items = append(items, virtMigrationCatalog()...)
	items = append(items, virtMonitorCatalog()...)
	return items
}

func virtCatalogPolicy(tool string, args []string) (readOnly, known, sensitive bool) {
	best := -1
	for _, item := range catalogItems {
		if item.Tool != tool || !prefixMatches(item.ArgPrefix, args) {
			continue
		}
		if len(item.ArgPrefix) > best {
			best = len(item.ArgPrefix)
			readOnly, known, sensitive = item.ReadOnly, true, item.Sensitive
		}
	}
	if !known {
		return false, false, true
	}
	// Some commands are read-only only in their default form. Treat host-file
	// output options as writes so they participate in the host serialization
	// domain instead of running concurrently with VM/disk mutations.
	switch tool {
	case "virt-top":
		if hasLongOption(args, "--csv") || hasLongOption(args, "--debug") {
			readOnly = false
		}
	case "virt-v2v-inspector":
		if hasShortOptionWithValue(args, 'O') || hasLongOption(args, "--output") {
			readOnly = false
		}
	}
	return readOnly, true, sensitive
}

func prefixMatches(prefix, args []string) bool {
	if len(prefix) == 0 {
		return true
	}
	if len(args) < len(prefix) {
		return false
	}
	for i := range prefix {
		if args[i] != prefix[i] {
			return false
		}
	}
	return true
}

func vItem(tool, command, category, summary, usage, docs, risk string, readOnly, sensitive bool, prefix []string, common, options, positionals []ParameterSpec) CatalogItem {
	return CatalogItem{
		Tool: tool, Command: command, Category: category, Summary: summary,
		Usage: usage, ReadOnly: readOnly, Risk: risk, DocsURL: docs,
		Sensitive: sensitive, ArgPrefix: prefix, Common: common,
		Options: options, Positionals: positionals,
	}
}

func vHelpVersion(tool, category, docs string) []CatalogItem {
	return []CatalogItem{
		{Tool: tool, Command: "help", Category: category, Summary: "显示 " + tool + " 官方帮助。", Usage: "--help", ReadOnly: true, Risk: "低", DocsURL: docs, ArgPrefix: []string{"--help"}},
		{Tool: tool, Command: "version", Category: category, Summary: "显示 " + tool + " 版本。", Usage: "--version", ReadOnly: true, Risk: "低", DocsURL: docs, ArgPrefix: []string{"--version"}},
	}
}

func vBool(flag, label string) ParameterSpec {
	return ParameterSpec{Name: strings.TrimLeft(flag, "-"), Flag: flag, Label: label, ValueType: "bool"}
}

func vRequiredBool(flag, label string) ParameterSpec {
	spec := vBool(flag, label)
	spec.Required = true
	return spec
}

func vText(flag, label, placeholder string) ParameterSpec {
	return ParameterSpec{Name: strings.TrimLeft(flag, "-"), Flag: flag, Label: label, Placeholder: placeholder, ValueType: "text"}
}

func vRequiredText(flag, label, placeholder string) ParameterSpec {
	spec := vText(flag, label, placeholder)
	spec.Required = true
	return spec
}

func vNumber(flag, label, placeholder string) ParameterSpec {
	return ParameterSpec{Name: strings.TrimLeft(flag, "-"), Flag: flag, Label: label, Placeholder: placeholder, ValueType: "number"}
}

func vRequiredNumber(flag, label, placeholder string) ParameterSpec {
	spec := vNumber(flag, label, placeholder)
	spec.Required = true
	return spec
}

func vChoice(flag, label string, choices ...string) ParameterSpec {
	return ParameterSpec{Name: strings.TrimLeft(flag, "-"), Flag: flag, Label: label, Placeholder: choices[0], Choices: choices, ValueType: "choice"}
}

func vMulti(flag, label, placeholder string) ParameterSpec {
	spec := vText(flag, label, placeholder)
	spec.Multiple = true
	return spec
}

func vPositional(name, label, placeholder string, required, multiple bool) ParameterSpec {
	return ParameterSpec{Name: name, Label: label, Placeholder: placeholder, ValueType: "text", Required: required, Multiple: multiple}
}

func vGuestSource(includeFormat bool) []ParameterSpec {
	out := vGuestImages(includeFormat)
	return append(out, vText("--domain", "从 libvirt 虚拟机读取磁盘", "vm-name"))
}

func vGuestImages(includeFormat bool) []ParameterSpec {
	out := []ParameterSpec{
		vMulti("--add", "磁盘镜像（可重复）", "/var/lib/libvirt/images/disk.qcow2"),
	}
	if includeFormat {
		out = append(out, vChoice("--format", "镜像格式", "auto", "raw", "qcow2", "vmdk", "vdi", "vhdx"))
	}
	return out
}

func virtAdminCatalog() []CatalogItem {
	const docs = "https://libvirt.org/manpages/virt-admin.html"
	cat := virtCategoryLibvirt
	server := vPositional("server", "libvirt 服务名称", "libvirtd", true, false)
	client := vPositional("client", "客户端 ID", "1", true, false)
	items := []CatalogItem{
		vItem("virt-admin", "server-list", cat, "列出当前 libvirt 管理连接上的服务端。", "server-list", docs, "低", true, true, []string{"server-list"}, nil, nil, nil),
		vItem("virt-admin", "daemon-log-filters", cat, "查看或设置 libvirt 守护进程日志过滤器。", "daemon-log-filters [--filters string]", docs, "中", false, true, []string{"daemon-log-filters"}, nil, []ParameterSpec{vText("--filters", "日志过滤器（留空则查询）", "4:util.object")}, nil),
		vItem("virt-admin", "daemon-log-outputs", cat, "查看或设置 libvirt 守护进程日志输出。", "daemon-log-outputs [--outputs string]", docs, "中", false, true, []string{"daemon-log-outputs"}, nil, []ParameterSpec{vText("--outputs", "日志输出（留空则查询）", "4:stderr 2:syslog:wvc")}, nil),
		vItem("virt-admin", "daemon-timeout", cat, "查看或设置守护进程关机超时。", "daemon-timeout [--timeout] NUM", docs, "中", false, true, []string{"daemon-timeout"}, nil, []ParameterSpec{vNumber("--timeout", "超时秒数（留空则查询）", "120")}, nil),
		vItem("virt-admin", "daemon-shutdown", cat, "关闭 libvirt 守护进程。", "daemon-shutdown [--preserve]", docs, "高", false, true, []string{"daemon-shutdown"}, nil, []ParameterSpec{vBool("--preserve", "保留状态并准备重启")}, nil),
		vItem("virt-admin", "server-threadpool-info", cat, "查看服务端线程池配置。", "server-threadpool-info server", docs, "低", true, true, []string{"server-threadpool-info"}, nil, nil, []ParameterSpec{server}),
		vItem("virt-admin", "server-threadpool-set", cat, "调整服务端线程池工作线程数量。", "server-threadpool-set server [--min-workers count] [--max-workers count] [--priority-workers count]", docs, "高", false, true, []string{"server-threadpool-set"}, nil, []ParameterSpec{
			vNumber("--min-workers", "最小工作线程数", "5"), vNumber("--max-workers", "最大工作线程数", "20"), vNumber("--priority-workers", "优先工作线程数", "5"),
		}, []ParameterSpec{server}),
		vItem("virt-admin", "server-clients-info", cat, "查看服务端客户端连接限额。", "server-clients-info server", docs, "低", true, true, []string{"server-clients-info"}, nil, nil, []ParameterSpec{server}),
		vItem("virt-admin", "server-clients-set", cat, "调整服务端客户端连接限额。", "server-clients-set server [--max-clients count] [--max-unauth-clients count]", docs, "高", false, true, []string{"server-clients-set"}, nil, []ParameterSpec{
			vNumber("--max-clients", "最大客户端数", "100"), vNumber("--max-unauth-clients", "最大未认证客户端数", "20"),
		}, []ParameterSpec{server}),
		vItem("virt-admin", "server-update-tls", cat, "重新加载服务端 TLS 证书与密钥。", "server-update-tls server", docs, "高", false, true, []string{"server-update-tls"}, nil, nil, []ParameterSpec{server}),
		vItem("virt-admin", "client-list", cat, "列出服务端客户端连接。", "client-list server", docs, "低", true, true, []string{"client-list"}, nil, nil, []ParameterSpec{server}),
		vItem("virt-admin", "client-info", cat, "查看客户端连接详情。", "client-info server client", docs, "低", true, true, []string{"client-info"}, nil, nil, []ParameterSpec{server, client}),
		vItem("virt-admin", "client-disconnect", cat, "强制断开指定客户端。", "client-disconnect server client", docs, "高", false, true, []string{"client-disconnect"}, nil, nil, []ParameterSpec{server, client}),
	}
	return append(items, vHelpVersion("virt-admin", cat, docs)...)
}

func virtHostToolsCatalog() []CatalogItem {
	items := []CatalogItem{
		vItem("virt-host-validate", "validate", virtCategoryLibvirt,
			"检查宿主机 KVM、cgroup、设备、安全策略等虚拟化前置条件。", "virt-host-validate [OPTIONS] [HV-TYPE]",
			"https://libvirt.org/manpages/virt-host-validate.html", "低", true, false, nil,
			nil, []ParameterSpec{vBool("--quiet", "只输出失败项")},
			[]ParameterSpec{{Name: "hv-type", Label: "虚拟化类型", Placeholder: "qemu", ValueType: "choice", Choices: []string{"qemu", "lxc", "xen", "bhyve", "vmx", "test"}}}),
		vItem("virt-qemu-run", "run", virtCategoryLibvirt,
			"用 libvirt 的 QEMU 驱动启动一个独立 guest XML；进程退出时虚拟机结束。", "virt-qemu-run [OPTION]... GUEST-XML-FILE",
			"https://libvirt.org/manpages/virt-qemu-run.html", "高", false, true, nil,
			nil, []ParameterSpec{
				vText("--root", "运行时根目录", "/run/wvc/virt-qemu-run"),
				vMulti("--secret", "Secret XML 文件,密钥文件", "/path/secret.xml,/path/secret-value"),
				vBool("--verbose", "详细日志"),
			}, []ParameterSpec{vPositional("guest-xml", "guest XML 文件", "/path/guest.xml", true, false)}),
		vItem("virt-xml-validate", "validate", virtCategoryLibvirt,
			"用 libvirt RNG schema 校验 domain/network/storage 等 XML 文件。", "virt-xml-validate [OPTIONS] XML-FILE [SCHEMA]",
			"https://libvirt.org/manpages/virt-xml-validate.html", "低", true, false, nil,
			nil, nil,
			[]ParameterSpec{
				vPositional("xml-file", "XML 文件", "/path/domain.xml", true, false),
				{Name: "schema", Label: "XML schema", Placeholder: "domain", ValueType: "choice", Choices: []string{"domain", "network", "networkport", "nwfilter", "storagepool", "storagevol", "secret", "snapshot", "checkpoint", "interface", "nodedev", "capability", "domainsnapshot", "domaincheckpoint", "domainbackup"}},
			}),
		vItem("virt-login-shell", "interactive", virtCategoryLibvirt,
			"将受限登录会话放入 LXC guest；需要交互式 shell，不适合异步任务。", "virt-login-shell", "https://libvirt.org/manpages/virt-login-shell.html", "高", false, true, nil, nil, nil, nil),
	}
	items[len(items)-1].Disabled = true
	items[len(items)-1].DisabledReason = "该命令会启动交互式登录 shell，无法在异步任务中完成；请通过主机 SSH 或用 virsh 管理 LXC 容器。"
	items = append(items,
		vItem("virt-pki-validate", "validate", virtCategoryLibvirt,
			"检查 libvirt TLS 证书目录和权限，可指定 trust、server 或 client 作用域。", "virt-pki-validate [trust|server|client]",
			"https://libvirt.org/manpages/virt-pki-validate.html", "低", true, false, nil, nil, nil,
			[]ParameterSpec{{Name: "scope", Label: "校验作用域（留空校验全部）", Placeholder: "trust", ValueType: "choice", Choices: []string{"trust", "server", "client"}}}),
		vItem("virt-sanlock-cleanup", "cleanup", virtCategoryLibvirt,
			"清理不再对应任何运行中 libvirt 虚拟机的 sanlock 锁文件。", "virt-sanlock-cleanup",
			"https://libvirt.org/manpages/virt-sanlock-cleanup.html", "高", false, true, nil, nil, nil, nil),
	)
	for _, tool := range []string{"virt-host-validate", "virt-qemu-run", "virt-xml-validate", "virt-login-shell", "virt-pki-validate", "virt-sanlock-cleanup"} {
		docs := "https://libvirt.org/manpages/" + tool + ".html"
		items = append(items, vHelpVersion(tool, virtCategoryLibvirt, docs)...)
	}
	return items
}

func virtinstCatalog() []CatalogItem {
	const installDocs = "https://manpages.debian.org/bookworm/virtinst/virt-install.1.en.html"
	const cloneDocs = "https://manpages.debian.org/bookworm/virtinst/virt-clone.1.en.html"
	const xmlDocs = "https://manpages.debian.org/bookworm/virtinst/virt-xml.1.en.html"
	const convertDocs = "https://manpages.debian.org/testing/virtinst/virt-convert.1.en.html"
	cat := virtCategoryVirtinst
	installOptions := []ParameterSpec{
		vRequiredText("--name", "虚拟机名称", "vm01"),
		vText("--memory", "内存大小（MiB）", "4096"),
		vText("--ram", "内存大小（MiB，旧别名）", "4096"),
		vText("--vcpus", "vCPU 数量/拓扑", "4"),
		vText("--cpu", "CPU 模型/特性", "host-passthrough"),
		vText("--machine", "机器类型", "q35"),
		vText("--arch", "CPU 架构", "x86_64"),
		vText("--os-variant", "操作系统版本", "ubuntu22.04"),
		vText("--osinfo", "libosinfo 操作系统版本（新别名）", "ubuntu22.04"),
		vText("--virt-type", "虚拟化类型", "kvm"),
		vBool("--accelerate", "启用 KVM 硬件加速"),
		vText("--description", "虚拟机描述", "Web application VM"),
		vText("--uuid", "新虚拟机 UUID（留空自动生成）", "自动生成时留空"),
		vText("--keymap", "键盘映射", "en-us"),
		vText("--boot", "启动配置", "uefi"),
		vText("--metadata", "元数据", "title=Web VM"),
		vText("--events", "事件配置", "on_poweroff=destroy"),
		vText("--resource", "资源分区配置", "partition=/virtual-machines"),
		vText("--sysinfo", "SMBIOS 配置", "type=host"),
		vText("--xml", "附加 XML 片段", "xpath=/domain/devices,file=/path/fragment.xml"),
		vMulti("--qemu-commandline", "QEMU 额外参数（可重复）", "-cpu host"),
		vText("--numatune", "NUMA 调优", "nodeset=0"),
		vText("--memtune", "内存调优", "hard_limit=8388608"),
		vText("--blkiotune", "块 I/O 调优", "weight=500"),
		vText("--cputune", "CPU 调优", "shares=1024"),
		vText("--security", "安全标签", "type=static,label=system_u:system_r:svirt_t:s0:c1,c2"),
		vText("--keywrap", "密钥封装", "cipher_name=aes"),
		vText("--iothreads", "I/O 线程", "2"),
		vText("--features", "Hyper-V/KVM 特性", "hyperv.relaxed=on"),
		vText("--clock", "时钟配置", "offset=utc"),
		vText("--pm", "电源管理", "suspend_to=mem"),
		vText("--launchSecurity", "启动安全配置", "type=sev"),
		vText("--idmap", "用户命名空间映射", "uid.start=0,uid.target=1000,uid.count=65536"),
	}
	installOptions = append(installOptions,
		vText("--cdrom", "安装 ISO 或设备", "/var/lib/libvirt/images/install.iso"),
		vText("--location", "安装树 URL/路径", "https://example.invalid/os"),
		vBool("--pxe", "从 PXE 启动安装"),
		vBool("--import", "导入现有磁盘，不执行安装"),
		vText("--extra-args", "内核额外参数", "console=ttyS0"),
		vText("--initrd-inject", "注入到 initrd 的文件", "/path/answerfile"),
		vText("--install", "安装方式", "no_install=yes"),
		vText("--unattended", "无人值守安装配置", "admin-password-file=/path/pw"),
		vText("--cloud-init", "cloud-init 配置", "user-data=/path/user-data"),
		vMulti("--disk", "磁盘配置（可重复）", "size=20,format=qcow2,bus=virtio"),
		vMulti("--filesystem", "共享文件系统（可重复）", "dir=/srv/share,target=share"),
		vMulti("--network", "网络配置（可重复）", "network=default,model=virtio"),
		vText("--bridge", "旧式桥接网络", "br0"),
		vText("--mac", "固定 MAC 地址", "52:54:00:12:34:56"),
		vMulti("--graphics", "图形设备（可重复）", "vnc,listen=127.0.0.1"),
		vChoice("--autoconsole", "自动控制台", "default", "text", "graphical", "none"),
		vBool("--noautoconsole", "不自动打开控制台"),
		vBool("--vnc", "创建 VNC 图形设备"),
		vText("--vnclisten", "VNC 监听地址", "127.0.0.1"),
		vNumber("--vncport", "VNC 端口", "5900"),
		vBool("--sdl", "创建 SDL 图形设备"),
		vBool("--nographics", "不创建图形设备"),
		vBool("--hvm", "全虚拟化 guest"),
		vBool("--paravirt", "半虚拟化 guest"),
		vBool("--container", "创建 LXC 容器"),
		vMulti("--controller", "控制器设备（可重复）", "type=scsi,model=virtio-scsi"),
		vMulti("--input", "输入设备（可重复）", "type=tablet,bus=usb"),
		vText("--host-device", "主机设备直通（旧别名）", "pci_0000_01_00_0"),
		vMulti("--hostdev", "主机设备直通（可重复）", "pci_0000_01_00_0"),
		vMulti("--sound", "声音设备（可重复）", "model=ich9"),
		vText("--soundhw", "声音硬件（旧别名）", "ich9"),
		vMulti("--audio", "音频后端（可重复）", "type=spice"),
		vMulti("--watchdog", "看门狗（可重复）", "model=i6300esb,action=reset"),
		vMulti("--serial", "串口（可重复）", "pty"),
		vMulti("--parallel", "并口（可重复）", "pty"),
		vMulti("--channel", "通道设备（可重复）", "spicevmc,name=vdagent"),
		vMulti("--console", "控制台设备（可重复）", "pty,target_type=serial"),
		vText("--video", "视频设备", "model=qxl"),
		vText("--smartcard", "智能卡", "mode=passthrough"),
		vText("--redirdev", "USB 重定向", "bus=usb,type=spicevmc"),
		vText("--memballoon", "内存气球", "model=virtio"),
		vText("--tpm", "TPM 设备", "backend.type=emulator"),
		vText("--rng", "随机数设备", "backend.model=virtio"),
		vText("--panic", "panic 设备", "model=isa"),
		vText("--shmem", "共享内存", "name=shmem0,size=64"),
		vText("--memdev", "内存设备", "model=dax"),
		vText("--vsock", "VSOCK 设备", "cid=3"),
		vText("--iommu", "IOMMU 设备", "model=intel"),
		vBool("--autostart", "宿主机启动时自动启动"),
		vBool("--transient", "创建临时 guest"),
		vBool("--live", "启用 live 安装/迁移模式"),
		vBool("--reinstall", "重装已存在的 guest"),
		vBool("--nodisks", "不创建磁盘"),
		vBool("--nonetworks", "不创建网络接口"),
		vBool("--nonsparse", "新磁盘完整分配空间"),
		vText("--file", "新磁盘镜像路径", "/var/lib/libvirt/images/vm01.qcow2"),
		vText("--file-size", "新磁盘大小", "20G"),
		vBool("--destroy-on-exit", "virt-install 退出时销毁 guest"),
		vBool("--print-xml", "只打印 XML 不创建 guest"),
		vBool("--noreboot", "安装完成后不重启"),
		ParameterSpec{Name: "wait", Flag: "--wait", Label: "等待安装完成（可选秒数，留空即无限等待）", Placeholder: "留空无限等待", ValueType: "text", OptionalValue: true},
		vBool("--dry-run", "仅验证并展示操作"),
		vText("--check", "检查配置路径", "/var/lib/libvirt/images"),
		vBool("--quiet", "安静模式"),
		vBool("--debug", "调试输出"),
	)
	items := []CatalogItem{
		vItem("virt-install", "create", cat, "通过 libvirt 创建 KVM/Xen/LXC 虚拟机，表单覆盖安装介质、磁盘、网络、图形、设备和调优选项。", "virt-install [OPTION]...", installDocs, "高", false, true, nil, nil, installOptions, nil),
		vItem("virt-clone", "clone", cat, "克隆已关机虚拟机及其磁盘，并生成新的名称、UUID 和 MAC。", "virt-clone [OPTION]...", cloneDocs, "高", false, true, nil, nil, []ParameterSpec{
			vText("--original", "源虚拟机", "source-vm"),
			vText("--original-xml", "源虚拟机 XML 文件", "/path/source.xml"),
			vBool("--auto-clone", "自动生成名称、磁盘路径和 MAC"),
			vRequiredText("--name", "新虚拟机名称", "clone-vm"),
			vText("--uuid", "新 UUID", "自动生成时留空"),
			vMulti("--file", "新磁盘路径（按源磁盘顺序）", "/var/lib/libvirt/images/clone.qcow2"),
			vText("--nvram", "新 NVRAM 文件", "/var/lib/libvirt/qemu/nvram/clone_VARS.fd"),
			vMulti("--force-copy", "强制复制目标磁盘（可重复）", "vda"),
			vMulti("--skip-copy", "跳过复制目标磁盘（可重复）", "vdb"),
			vBool("--nonsparse", "完整分配磁盘空间"),
			vBool("--preserve-data", "保留原磁盘数据而不复制"),
			vText("--reflink", "reflink 策略", "auto"),
			vMulti("--mac", "新 MAC 地址", "52:54:00:12:34:56"),
			vBool("--print-xml", "只打印 XML"),
			vBool("--replace", "替换同名 guest"),
			vText("--check", "检查配置路径", "/var/lib/libvirt/images"),
			vBool("--quiet", "安静模式"),
			vBool("--debug", "调试输出"),
		}, nil),
		vItem("virt-convert", "convert", cat, "转换 VMware VMX/OVF 等虚拟机配置并导入 libvirt（旧版 virtinst 可选工具）。", "virt-convert INPUT.vmx|INPUT.ovf|INPUT-DIR|INPUT.zip [OPTIONS]", convertDocs, "高", false, true, nil, nil, []ParameterSpec{
			vChoice("--input-format", "输入格式", "vmx", "ovf"),
			vChoice("--disk-format", "输出磁盘格式", "raw", "qcow2", "vmdk", "vdi", "vhdx", "none"),
			vText("--destination", "转换后磁盘目录", "/var/lib/libvirt/images"),
			vBool("--noautoconsole", "不自动连接 guest 控制台"),
			vBool("--print-xml", "只打印转换后的 XML"),
			vBool("--dry-run", "只检查转换"),
			vBool("--quiet", "安静模式"),
			vBool("--debug", "调试输出"),
		}, []ParameterSpec{vPositional("input", "输入虚拟机配置/磁盘", "/path/vm.vmx", true, false)}),
		vItem("virt-xml", "edit", cat, "通过命令行动作修改 libvirt domain XML，可定义、启动、热插拔设备或输出 XML 差异。", "virt-xml DOMAIN XML-ACTION XML-OPTION [OUTPUT-OPTION] [MISC-OPTIONS] ...", xmlDocs, "高", false, true, nil, nil, virtXMLOptions(), []ParameterSpec{
			vPositional("domain", "虚拟机（名称/ID/UUID）", "vm01", true, false),
		}),
	}
	for _, tool := range []string{"virt-install", "virt-clone", "virt-convert", "virt-xml"} {
		docs := installDocs
		switch tool {
		case "virt-clone":
			docs = cloneDocs
		case "virt-xml":
			docs = xmlDocs
		case "virt-convert":
			docs = convertDocs
		}
		items = append(items, vHelpVersion(tool, cat, docs)...)
	}
	return items
}

func virtXMLOptions() []ParameterSpec {
	return []ParameterSpec{
		vBool("--update", "更新已有设备/配置"),
		vBool("--add-device", "热添加/定义新设备"),
		vBool("--remove-device", "移除设备"),
		{Name: "edit", Flag: "--edit", Label: "编辑 XML 块（可选编号/all/设备过滤）", Placeholder: "留空编辑第一块", ValueType: "text", OptionalValue: true},
		vBool("--build-xml", "只构建并打印 XML"),
		vBool("--convert-to-q35", "把 i440FX 配置转换为 Q35"),
		vBool("--convert-to-vnc", "转换为单一 VNC 图形配置"),
		vBool("--define", "修改持久配置"),
		vBool("--no-define", "只修改运行态"),
		vBool("--start", "创建后启动"),
		vBool("--print", "打印修改后的 XML"),
		vBool("--print-diff", "打印 XML 差异"),
		vBool("--print-xml", "打印最终 XML"),
		vBool("--confirm", "交互确认"),
		vText("--os-variant", "操作系统版本", "ubuntu22.04"),
		vText("--osinfo", "libosinfo 操作系统版本（新别名）", "ubuntu22.04"),
		vMulti("--disk", "磁盘设备（可重复）", "path=/path/disk.qcow2,target=vdb"),
		vMulti("--network", "网络设备（可重复）", "network=default,model=virtio"),
		vMulti("--graphics", "图形设备（可重复）", "vnc,port=-1"),
		vMulti("--controller", "控制器（可重复）", "type=scsi,model=virtio-scsi"),
		vMulti("--input", "输入设备（可重复）", "type=tablet,bus=usb"),
		vText("--host-device", "主机设备直通（旧别名）", "pci_0000_01_00_0"),
		vMulti("--hostdev", "主机设备（可重复）", "pci_0000_01_00_0"),
		vMulti("--channel", "通道设备（可重复）", "spicevmc,name=vdagent"),
		vMulti("--console", "控制台设备（可重复）", "pty"),
		vMulti("--serial", "串口（可重复）", "pty"),
		vMulti("--parallel", "并口（可重复）", "pty"),
		vText("--video", "视频设备", "model=qxl"),
		vText("--sound", "声音设备", "model=ich9"),
		vText("--audio", "音频后端", "type=spice"),
		vText("--watchdog", "看门狗", "model=i6300esb"),
		vText("--memballoon", "内存气球", "model=virtio"),
		vText("--tpm", "TPM 设备", "backend.type=emulator"),
		vText("--rng", "随机数设备", "backend.model=virtio"),
		vText("--panic", "panic 设备", "model=isa"),
		vText("--shmem", "共享内存", "name=shmem0,size=64"),
		vText("--memdev", "内存设备", "model=dax"),
		vText("--smartcard", "智能卡", "mode=passthrough"),
		vText("--redirdev", "USB 重定向", "bus=usb,type=spicevmc"),
		vText("--filesystem", "文件系统", "dir=/srv/share,target=share"),
		vText("--memory", "内存配置", "memory=4096"),
		vText("--vcpus", "vCPU 配置", "vcpu.current=4"),
		vText("--cpu", "CPU 配置", "mode=host-passthrough"),
		vText("--metadata", "元数据", "title=Web VM"),
		vText("--boot", "启动配置", "uefi"),
		vText("--clock", "时钟配置", "offset=utc"),
		vText("--features", "特性", "hyperv.relaxed=on"),
		vText("--events", "事件配置", "on_poweroff=destroy"),
		vText("--pm", "电源管理", "suspend_to=mem"),
		vText("--sysinfo", "SMBIOS 配置", "type=host"),
		vText("--resources", "资源分区配置", "partition=/virtual-machines"),
		vText("--security", "安全标签", "type=static,label=svirt_t"),
		vText("--seclabel", "安全标签模型", "type=dynamic"),
		vText("--cputune", "CPU 调优", "shares=1024"),
		vText("--memtune", "内存调优", "hard_limit=8388608"),
		vText("--blkiotune", "块 I/O 调优", "weight=500"),
		vText("--numatune", "NUMA 调优", "nodeset=0"),
		vText("--memorybacking", "内存后端", "hugepages"),
		vText("--iothreads", "I/O 线程", "2"),
		vText("--idmap", "用户命名空间映射", "uid.start=0,uid.target=1000,uid.count=65536"),
		vText("--keywrap", "密钥封装", "cipher_name=aes"),
		vText("--launchSecurity", "启动安全配置", "type=sev"),
		vMulti("--qemu-commandline", "QEMU 参数（可重复）", "-cpu host"),
		vMulti("--xml", "XML 修改（可重复）", "xpath=/domain/devices,file=/path/fragment.xml"),
		vBool("--quiet", "安静模式"),
		vBool("--debug", "调试输出"),
	}
}

func libguestfsCatalog() []CatalogItem {
	items := make([]CatalogItem, 0, 100)
	cat := virtCategoryGuestfs
	docs := func(tool string) string { return "https://libguestfs.org/" + tool + ".1.html" }

	items = append(items,
		vItem("virt-builder", "build", cat, "从官方模板快速构建 guest 镜像，并可在构建时注入用户、SSH key、软件包和脚本。", "virt-builder os-version [--options]", docs("virt-builder"), "高", false, true, nil,
			nil, append(append(virtBuilderOptions(), vCustomizeOperations()...), vWindowsInjectionOperations()...), []ParameterSpec{vPositional("os-version", "模板版本", "ubuntu-22.04", true, false)}),
		vItem("virt-builder", "list", cat, "列出可用模板。", "virt-builder --list [--long] [--list-format short|long|json]", docs("virt-builder"), "低", true, true, []string{"--list"},
			nil, []ParameterSpec{vBool("--long", "长格式"), vChoice("--list-format", "列表格式", "short", "long", "json")}, []ParameterSpec{{Name: "os-version", Label: "模板版本过滤", Placeholder: "ubuntu-22.04", ValueType: "text"}}),
		vItem("virt-builder", "notes", cat, "显示指定模板的发行说明。", "virt-builder --notes os-version", docs("virt-builder"), "低", true, true, []string{"--notes"}, nil, nil, []ParameterSpec{vPositional("os-version", "模板版本", "ubuntu-22.04", true, false)}),
		vItem("virt-builder", "print-cache", cat, "显示模板缓存目录和内容。", "virt-builder --print-cache", docs("virt-builder"), "低", true, true, []string{"--print-cache"}, nil, nil, nil),
		vItem("virt-builder", "cache-all-templates", cat, "下载并缓存全部模板。", "virt-builder --cache-all-templates", docs("virt-builder"), "中", false, true, []string{"--cache-all-templates"}, nil, nil, nil),
		vItem("virt-builder", "delete-cache", cat, "删除模板缓存。", "virt-builder --delete-cache", docs("virt-builder"), "中", false, true, []string{"--delete-cache"}, nil, []ParameterSpec{vBool("--no-delete-on-failure", "失败时保留缓存")}, nil),
		vItem("virt-builder", "get-kernel", cat, "从已构建镜像提取内核和 initramfs。", "virt-builder --get-kernel DISKIMAGE [--format raw|qcow2] [--output OUTPUTDIR]", docs("virt-builder"), "中", false, true, []string{"--get-kernel"},
			nil, []ParameterSpec{vChoice("--format", "镜像格式", "qcow2", "raw"), vText("--output", "输出目录", "/tmp/kernel-export")}, []ParameterSpec{vPositional("diskimage", "磁盘镜像", "/var/lib/libvirt/images/disk.qcow2", true, false)}),
		vItem("virt-builder-repository", "update", cat, "生成或更新 virt-builder 模板仓库及索引。", "virt-builder-repository /path/to/repository [--interactive] [--gpg-key KEYID]", docs("virt-builder-repository"), "高", false, true, nil,
			nil, []ParameterSpec{
				vBool("--interactive", "交互式填写模板信息"), vText("--gpg-key", "签名 GPG key ID", "ABCDEF0123456789"), vBool("--no-compression", "不压缩仓库索引"), vBool("--machine-readable", "机器可读输出"), vBool("--colors", "彩色输出"), vBool("--quiet", "安静模式"), vBool("--verbose", "详细日志"),
			}, []ParameterSpec{vPositional("repository", "模板仓库目录", "/srv/virt-builder-repo", true, false)}),
		vItem("guestfish", "execute", cat, "以非交互方式调用 libguestfs API：挂载 guest 磁盘并执行一条或多条 guestfish 命令。",
			"guestfish [--options] [commands...]", docs("guestfish"), "高", false, true, nil,
			nil, guestfishOptions(), []ParameterSpec{vPositional("command", "guestfish 命令（每行一条，按顺序执行）", "cat /etc/hostname", false, true)}),
		vItem("guestmount", "mount", cat, "使用 FUSE 把 guest 文件系统挂载到面板主机目录；需要容器具备 FUSE 设备能力。",
			"guestmount [--options] -a disk.img -m device mountpoint | -d domain -i mountpoint", docs("guestmount"), "高", false, true, nil,
			vGuestSource(true), guestmountOptions(), []ParameterSpec{vPositional("mountpoint", "面板主机挂载点", "/mnt/guest", true, false)}),
		vItem("guestunmount", "unmount", cat, "卸载由 guestmount 挂载的目录，并清理残留 FUSE 进程。",
			"guestunmount [--options] mountpoint", docs("guestunmount"), "中", false, false, nil,
			nil, []ParameterSpec{
				vNumber("--fd", "guestmount 文件描述符", "3"), vNumber("--retry", "重试次数", "5"), vBool("--no-retry", "失败时不重试"), vBool("--quiet", "安静模式"),
			}, []ParameterSpec{vPositional("mountpoint", "面板主机挂载点", "/mnt/guest", true, false)}),
		vItem("libguestfs-test-tool", "test", cat, "运行 libguestfs 自检，确认 appliance、QEMU、libvirt 与 SELinux 环境是否可用。",
			"libguestfs-test-tool [--options]", docs("libguestfs-test-tool"), "低", true, false, nil,
			nil, []ParameterSpec{
				vText("--qemu", "QEMU 可执行文件路径", "/usr/bin/qemu-system-x86_64"), vText("--qemudir", "QEMU 源码目录", "/usr/src/qemu"), vNumber("-t", "启动超时秒数", "600"),
			}, nil),
		vItem("libguestfs-make-fixed-appliance", "build", cat, "生成可分发或离线部署的固定 libguestfs appliance。",
			"libguestfs-make-fixed-appliance [--xz] OUTPUTDIR", docs("libguestfs-make-fixed-appliance"), "中", false, true, nil,
			nil, []ParameterSpec{vBool("--xz", "在当前目录生成压缩 appliance 包")}, []ParameterSpec{
				vPositional("outputdir", "输出目录（使用 --xz 时留空）", "/var/lib/libguestfs", false, false)},
		),
		vItem("virt-customize", "customize", cat, "离线定制 guest：安装软件、注入 SSH key、设置密码/主机名、运行脚本、上传文件等。", "virt-customize [--options] -d domname | -a disk.img [-a ...]", docs("virt-customize"), "极高", false, true, nil,
			append([]ParameterSpec{
				vMulti("--add", "磁盘镜像（可重复）", "/var/lib/libvirt/images/disk.qcow2"), vText("--domain", "从 libvirt 虚拟机读取磁盘", "vm01"), vChoice("--format", "镜像格式", "auto", "raw", "qcow2", "vmdk", "vdi", "vhdx"),
				vMulti("--attach", "附加 ISO/磁盘（可重复）", "/path/extra.iso"), vText("--attach-format", "附加介质格式", "raw"), vBool("--dry-run", "只演练不落盘"), vNumber("--memsize", "appliance 内存（MiB）", "1024"), vNumber("--smp", "appliance vCPU 数", "2"), vBool("--echo-keys", "回显密钥输入"), vBool("--keys-from-stdin", "从 stdin 读取密钥"), vBool("--network", "启用 appliance 网络"), vBool("--no-network", "禁用 appliance 网络"), vBool("--quiet", "安静模式"), vBool("--verbose", "详细日志"), vBool("-x", "跟踪 libguestfs API"), vBool("--colors", "彩色输出"),
			}, append(vCustomizeOperations(), vWindowsInjectionOperations()...)...), nil, nil),
		vItem("virt-dib", "build", cat, "调用 diskimage-builder 生成 guest 镜像。", "virt-dib -B DIB-LIB [options] elements...", docs("virt-dib"), "高", false, true, nil,
			nil, []ParameterSpec{
				vText("--dib-lib", "diskimage-builder 路径", "/usr/share/diskimage-builder"), vText("--checksum", "校验和文件", "SHA256SUMS"), vBool("--network", "允许构建时联网"), vBool("--no-network", "禁止构建时联网"), vBool("--no-delete-on-failure", "失败时保留临时文件"), vBool("--skip-base", "跳过 base element"), vBool("--machine-readable", "机器可读输出"), vBool("--colors", "彩色输出"), vBool("--quiet", "安静模式"), vBool("--verbose", "详细日志"),
			}, []ParameterSpec{vPositional("element", "diskimage-builder elements（可重复）", "ubuntu", true, true)}),
	)

	items = append(items,
		vItem("virt-alignment-scan", "scan", cat, "检查虚拟机分区是否按宿主机/存储最佳边界对齐。", "virt-alignment-scan [--options] -d domname | -a disk.img [-a ...]", docs("virt-alignment-scan"), "低", true, true, nil,
			vGuestSource(true), []ParameterSpec{vNumber("--blocksize", "块大小（字节）", "4096"), vBool("--uuid", "同时显示分区 UUID"), vBool("--quiet", "安静模式"), vBool("--verbose", "详细日志")}, nil),
		vItem("virt-cat", "cat", cat, "从关机虚拟机磁盘中读取文件，不需要启动 guest。", "virt-cat [--options] -d domname file [file ...]", docs("virt-cat"), "低", true, true, nil,
			vGuestSource(true), []ParameterSpec{vNumber("--blocksize", "块大小（字节）", "4096"), vBool("--echo-keys", "回显 LUKS 密钥输入"), vBool("--keys-from-stdin", "从 stdin 读取密钥"), vBool("--verbose", "详细日志")},
			[]ParameterSpec{vPositional("file", "guest 内文件路径（可重复）", "/etc/hostname", true, true)}),
		vItem("virt-copy-in", "copy-in", cat, "把宿主机文件或目录递归复制到 guest 磁盘。", "virt-copy-in -a disk.img file|dir [file|dir ...] /destination", docs("virt-copy-in"), "高", false, true, nil,
			vGuestSource(true), nil, []ParameterSpec{
				vPositional("source", "宿主机文件/目录（可重复）", "/srv/config", true, true),
				vPositional("destination", "guest 内目标目录", "/etc", true, false),
			}),
		vItem("virt-copy-out", "copy-out", cat, "把 guest 磁盘中的文件或目录递归复制到宿主机目录。", "virt-copy-out -a disk.img /file|dir [file|dir ...] localdir", docs("virt-copy-out"), "中", false, true, nil,
			vGuestSource(true), nil, []ParameterSpec{
				vPositional("source", "guest 内文件/目录（可重复）", "/var/log", true, true),
				vPositional("destination", "宿主机目标目录", "/tmp/guest-export", true, false),
			}),
		vItem("virt-df", "df", cat, "显示 guest 文件系统使用量，类似 df。", "virt-df [--options] [domain ...]", docs("virt-df"), "低", true, true, nil,
			vGuestSource(true), []ParameterSpec{
				vBool("--csv", "CSV 输出"), vBool("--human-readable", "人类可读单位"), vBool("--inodes", "显示 inode 用量"), vBool("--one-per-guest", "每个 guest 只显示汇总"), vBool("--uuid", "显示 UUID"), vNumber("--blocksize", "块大小（字节）", "4096"), vBool("--verbose", "详细日志"),
			}, []ParameterSpec{{Name: "domain", Label: "虚拟机名称（可重复，留空则全部）", Placeholder: "vm01", ValueType: "text", Multiple: true}}),
		vItem("virt-diff", "diff", cat, "比较两个 guest/磁盘的文件、元数据和扩展属性差异。", "virt-diff [--options] -d domain1 -D domain2 | -a disk1 -A disk2", docs("virt-diff"), "低", true, true, nil,
			[]ParameterSpec{vMulti("--add", "第一组磁盘（可重复）", "disk1.qcow2"), vText("--domain", "第一个虚拟机", "vm-before"), vText("--format", "第一组磁盘格式", "qcow2"), vMulti("-A", "第二组磁盘（可重复）", "disk2.qcow2"), vText("-D", "第二个虚拟机", "vm-after")},
			[]ParameterSpec{
				vBool("--all", "显示所有差异"), vBool("--atime", "比较访问时间"), vNumber("--blocksize", "块大小（字节）", "4096"), vBool("--checksum", "校验文件内容"), vBool("--csv", "CSV 输出"), vBool("--dir-links", "比较硬链接"), vBool("--dir-times", "比较目录时间"), vBool("--extra-stats", "附加统计"), vBool("--human-readable", "人类可读单位"), vBool("--times", "比较时间戳"), vBool("--time-days", "时间按天显示"), vBool("--time-relative", "相对时间"), vBool("--time-t", "固定时间基准"), vBool("--uids", "比较 UID/GID"), vBool("--xattrs", "比较扩展属性"), vBool("--echo-keys", "回显密钥输入"), vBool("--keys-from-stdin", "从 stdin 读取密钥"), vBool("--verbose", "详细日志"),
			}, nil),
		vItem("virt-edit", "edit", cat, "非交互式编辑 guest 文件；用 --expr 提供 Perl 替换表达式。", "virt-edit [--options] -d domname file [file ...] [-e expr]", docs("virt-edit"), "高", false, true, nil,
			vGuestSource(true), []ParameterSpec{vRequiredText("--expr", "非交互 Perl 表达式", "s/^OPT=.*/OPT=ok/"), vNumber("--blocksize", "块大小（字节）", "4096"), vBool("--echo-keys", "回显密钥输入"), vBool("--keys-from-stdin", "从 stdin 读取密钥"), vBool("--verbose", "详细日志")},
			[]ParameterSpec{vPositional("file", "guest 内文件路径（可重复）", "/etc/ssh/sshd_config", true, true)}),
		vItem("virt-filesystems", "list", cat, "列出 guest 中的文件系统、分区、LVM PV/VG/LV 和块设备。", "virt-filesystems [--options] -d domname | -a disk.img [-a ...]", docs("virt-filesystems"), "低", true, true, nil,
			vGuestSource(true), []ParameterSpec{
				vBool("--all", "显示所有对象"), vBool("--blkdevs", "显示块设备"), vBool("--blocksize", "显示块大小"), vBool("--csv", "CSV 输出"), vBool("--extra", "显示额外字段"), vBool("--filesystems", "显示文件系统"), vBool("--human-readable", "人类可读单位"), vBool("--long", "长格式"), vBool("--lvs", "显示逻辑卷"), vBool("--no-title", "不输出标题"), vBool("--parts", "显示分区"), vBool("--pvs", "显示物理卷"), vBool("--uuid", "显示 UUID"), vBool("--vgs", "显示卷组"), vBool("--echo-keys", "回显密钥输入"), vBool("--keys-from-stdin", "从 stdin 读取密钥"), vBool("--verbose", "详细日志"),
			}, nil),
		vItem("virt-format", "format", cat, "重新格式化 guest 磁盘，可创建分区和 LVM。", "virt-format [--options] -a disk.img [-a ...]", docs("virt-format"), "极高", false, true, nil,
			vGuestImages(true), []ParameterSpec{
				vText("--lvm", "LVM 卷组/逻辑卷", "/dev/VG/LV"), vText("--partition", "分区方案", "mbr"), vBool("--wipe", "先擦除旧数据"), vNumber("--blocksize", "块大小（字节）", "4096"), vBool("--verbose", "详细日志"),
			}, nil),
		vItem("virt-get-kernel", "get-kernel", cat, "从 guest 中提取内核和 initramfs。", "virt-get-kernel [--options] -d domname | -a disk.img", docs("virt-get-kernel"), "中", false, true, nil,
			vGuestSource(false), []ParameterSpec{
				vText("--output", "输出目录", "/tmp/kernel-export"), vBool("--unversioned-names", "输出不带版本号的文件名"), vBool("--machine-readable", "机器可读输出"), vBool("--echo-keys", "回显密钥输入"), vBool("--keys-from-stdin", "从 stdin 读取密钥"), vBool("--quiet", "安静模式"), vBool("--verbose", "详细日志"),
			}, nil),
		vItem("virt-inspector", "inspect", cat, "检查 guest 操作系统、应用、内核、挂载点和虚拟化元数据并输出 XML/JSON。", "virt-inspector [--options] -d domname | -a disk.img [-a ...]", docs("virt-inspector"), "中", true, true, nil,
			vGuestSource(true), []ParameterSpec{
				vBool("--no-applications", "不枚举应用"), vBool("--no-icon", "不提取图标"), vNumber("--blocksize", "块大小（字节）", "4096"), vBool("--echo-keys", "回显密钥输入"), vBool("--keys-from-stdin", "从 stdin 读取密钥"), vBool("--verbose", "详细日志"),
			}, nil),
		vItem("virt-list-filesystems", "list", cat, "兼容旧版的 guest 文件系统列表命令。", "virt-list-filesystems [--options] domname | disk.img ...", docs("virt-list-filesystems"), "低", true, true, nil,
			[]ParameterSpec{vChoice("--format", "镜像格式", "auto", "raw", "qcow2", "vmdk", "vdi", "vhdx")},
			[]ParameterSpec{vBool("--all", "包括所有文件系统"), vBool("--long", "长格式")},
			[]ParameterSpec{{Name: "guest-or-disk", Label: "虚拟机或磁盘路径（可重复）", Placeholder: "vm01 或 disk.qcow2", ValueType: "text", Multiple: true}}),
		vItem("virt-list-partitions", "list", cat, "兼容旧版的 guest 分区列表命令。", "virt-list-partitions [--options] domname | disk.img ...", docs("virt-list-partitions"), "低", true, true, nil,
			[]ParameterSpec{vChoice("--format", "镜像格式", "auto", "raw", "qcow2", "vmdk", "vdi", "vhdx")},
			[]ParameterSpec{vBool("--long", "长格式"), vBool("--human-readable", "人类可读单位"), vBool("--total", "显示每个块设备的总大小")},
			[]ParameterSpec{{Name: "guest-or-disk", Label: "虚拟机或磁盘路径（可重复）", Placeholder: "vm01 或 disk.qcow2", ValueType: "text", Multiple: true}}),
		vItem("virt-log", "log", cat, "提取 guest 的 syslog、journal 和 Windows 事件日志。", "virt-log [--options] -d domname | -a disk.img [-a ...]", docs("virt-log"), "中", true, true, nil,
			vGuestSource(true), []ParameterSpec{vNumber("--blocksize", "块大小（字节）", "4096"), vBool("--echo-keys", "回显密钥输入"), vBool("--keys-from-stdin", "从 stdin 读取密钥"), vBool("--verbose", "详细日志")}, nil),
		vItem("virt-ls", "ls", cat, "列出 guest 目录；支持递归、校验和和时间字段。", "virt-ls [--options] -d domname directory [directory ...]", docs("virt-ls"), "低", true, true, nil,
			vGuestSource(true), []ParameterSpec{
				vBool("--checksum", "显示校验和"), vBool("--csv", "CSV 输出"), vBool("--extra-stats", "附加统计"), vBool("--human-readable", "人类可读单位"), vBool("--long", "长格式"), vBool("--recursive", "递归列出"), vBool("--times", "显示时间"), vBool("--time-days", "时间按天显示"), vBool("--time-relative", "相对时间"), vBool("--time-t", "固定时间基准"), vBool("--uids", "显示 UID/GID"), vNumber("--blocksize", "块大小（字节）", "4096"), vBool("--echo-keys", "回显密钥输入"), vBool("--keys-from-stdin", "从 stdin 读取密钥"), vBool("--verbose", "详细日志"),
			}, []ParameterSpec{vPositional("directory", "guest 内目录（可重复）", "/var/log", true, true)}),
		vItem("virt-make-fs", "create", cat, "从 tar 归档、压缩包或目录创建可挂载的 guest 文件系统镜像。", "virt-make-fs [--options] input.tar|directory output.img", docs("virt-make-fs"), "高", false, true, nil,
			nil, []ParameterSpec{
				vText("--size", "镜像大小", "10G"), vText("--type", "文件系统类型", "ext4"), vText("--label", "文件系统标签", "data"), vText("--format", "输出镜像格式", "qcow2"), vChoice("--partition", "分区方案", "mbr", "gpt", "none"), vBool("--floppy", "创建 FAT 软盘镜像"), vBool("--verbose", "详细日志"),
			}, []ParameterSpec{
				vPositional("input", "输入 tar/目录", "/srv/data.tar.gz", true, false),
				vPositional("output", "输出镜像", "/var/lib/libvirt/images/data.img", true, false),
			}),
		vItem("virt-resize", "resize", cat, "扩展或收缩 guest 磁盘及其中分区/文件系统。", "virt-resize [--options] indisk outdisk", docs("virt-resize"), "极高", false, true, nil,
			nil, []ParameterSpec{
				vMulti("--resize", "调整分区大小（可重复）", "/dev/sda2=+10G"), vMulti("--expand", "扩展分区到剩余空间（可重复）", "/dev/sda2"), vMulti("--shrink", "收缩分区（可重复）", "/dev/sda2"), vMulti("--ignore", "忽略分区（可重复）", "/dev/sda3"), vMulti("--delete", "删除分区（可重复）", "/dev/sda4"), vBool("--no-copy-boot-loader", "不复制 boot loader"), vBool("--no-extra-partition", "不创建额外分区"), vBool("--no-expand-content", "不扩展文件系统内容"), vBool("--no-sparse", "不生成稀疏输出"), vBool("--ntfsresize-force", "强制 NTFS 调整"), vBool("--dry-run", "仅检查"), vBool("--machine-readable", "机器可读输出"), vBool("--debug", "调试输出"), vBool("--quiet", "安静模式"), vBool("--verbose", "详细日志"),
			}, []ParameterSpec{
				vPositional("indisk", "输入磁盘", "/var/lib/libvirt/images/old.qcow2", true, false),
				vPositional("outdisk", "输出磁盘", "/var/lib/libvirt/images/new.qcow2", true, false),
			}),
		vItem("virt-sparsify", "sparsify", cat, "回收 guest 磁盘未使用空间并生成稀疏/压缩镜像。", "virt-sparsify [--options] indisk outdisk | --in-place disk", docs("virt-sparsify"), "高", false, true, nil,
			nil, []ParameterSpec{
				vBool("--compress", "压缩输出"), vBool("--in-place", "原地精简"), vText("--convert", "转换输出格式", "qcow2"), vText("--format", "输入格式", "qcow2"), vText("--tmp", "临时目录", "/var/tmp"), vText("--ignore", "忽略文件系统（可重复）", "/var/lib/docker"), vChoice("--check-tmpdir", "临时目录空间检查", "ignore", "continue", "warn", "fail"), vText("--zero", "零块处理策略", "zero"), vText("-o", "qemu-img 选项", "preallocation=metadata"), vText("--key", "LUKS 密钥选择器", "all"), vBool("--machine-readable", "机器可读输出"), vBool("--echo-keys", "回显密钥输入"), vBool("--keys-from-stdin", "从 stdin 读取密钥"), vBool("--wrap", "长日志自动换行"), vBool("-x", "跟踪 libguestfs API"), vBool("--quiet", "安静模式"), vBool("--verbose", "详细日志"),
			}, []ParameterSpec{
				vPositional("indisk", "输入磁盘", "/var/lib/libvirt/images/disk.qcow2", true, false),
				vPositional("outdisk", "输出磁盘（--in-place 时留空）", "/var/lib/libvirt/images/disk-sparse.qcow2", false, false),
			}),
		vItem("virt-sysprep", "run", cat, "重置 guest 身份信息（machine-id、SSH host key、日志、用户痕迹等）。", "virt-sysprep [--options] -d domname | -a disk.img [-a ...]", docs("virt-sysprep"), "极高", false, true, nil,
			vGuestSource(true), []ParameterSpec{
				vText("--enable", "启用指定操作（逗号分隔，可加 + 前缀）", "ssh-hostkeys,udev-persistent-net"), vText("--operations", "覆盖操作列表（用 - 前缀禁用）", "defaults,-tmp-files"), vText("--mount-options", "文件系统挂载选项", "/:noatime;/var:rw,nodiratime"), vBool("--list-operations", "列出可用操作"), vBool("--dry-run", "只演练不落盘"), vBool("--network", "启用 appliance 网络"), vBool("--no-network", "禁用 appliance 网络"), vBool("--echo-keys", "回显密钥输入"), vBool("--keys-from-stdin", "从 stdin 读取密钥"), vBool("--colors", "彩色输出"), vBool("--quiet", "安静模式"), vBool("--verbose", "详细日志"), vBool("-x", "跟踪 libguestfs API"),
			}, append(vWindowsInjectionOperations(), vBool("--no-logfile", "不保留 sysprep 日志"), vBool("--wrap", "长日志自动换行"))),
		vItem("virt-tail", "tail", cat, "跟踪 guest 内文件的新增内容；任务超时后停止。", "virt-tail [--options] -d domname file [file ...]", docs("virt-tail"), "中", false, true, nil,
			vGuestSource(true), []ParameterSpec{vBool("--follow", "持续跟踪文件变化"), vNumber("--blocksize", "块大小（字节）", "4096"), vBool("--echo-keys", "回显密钥输入"), vBool("--keys-from-stdin", "从 stdin 读取密钥"), vBool("--verbose", "详细日志")},
			[]ParameterSpec{vPositional("file", "guest 内文件（可重复）", "/var/log/messages", true, true)}),
		vItem("virt-tar-in", "extract", cat, "把宿主机 tar 归档解压到 guest 目录。", "virt-tar-in -a disk.img data.tar /destination", docs("virt-tar-in"), "高", false, true, nil,
			vGuestSource(true), nil, []ParameterSpec{
				vPositional("tarball", "宿主机 tar 文件（- 表示 stdin）", "/tmp/data.tar", true, false),
				vPositional("destination", "guest 内目标目录", "/opt/data", true, false),
			}),
		vItem("virt-tar-out", "archive", cat, "把 guest 目录打包为宿主机 tar 文件。", "virt-tar-out -a disk.img /dir files.tar", docs("virt-tar-out"), "中", false, true, nil,
			vGuestSource(true), nil, []ParameterSpec{
				vPositional("directory", "guest 内目录", "/var/log", true, false),
				vPositional("tarball", "宿主机 tar 文件（- 表示 stdout）", "/tmp/logs.tar", true, false),
			}),
		vItem("virt-win-reg", "query", cat, "读取、导出或合并 Windows guest 注册表。", "virt-win-reg domname 'HKLM\\Path\\To\\Subkey' [name]", docs("virt-win-reg"), "高", false, true, nil,
			nil, []ParameterSpec{
				vBool("--merge", "合并 .reg 文件到 guest 注册表"), vBool("--unsafe-printable-strings", "以不安全方式输出可打印字符串"), vBool("--debug", "调试输出"),
			}, []ParameterSpec{
				{Name: "guest", Label: "虚拟机或磁盘", Placeholder: "win-vm", ValueType: "text", Required: true},
				{Name: "key", Label: "注册表键", Placeholder: "HKLM\\Software\\Microsoft", ValueType: "text", Required: true},
				{Name: "value", Label: "值名称（留空导出整个键）", Placeholder: "ProductName", ValueType: "text"},
			}),
	)

	// virt-tar predates the split tar-in/tar-out commands. Keep both classic
	// modes clickable instead of exposing an interactive shell.
	items = append(items,
		vItem("virt-tar", "extract", cat, "兼容旧版的 virt-tar 提取模式。", "virt-tar [--options] -x domname directory tarball", docs("virt-tar"), "中", false, true, []string{"-x"},
			vGuestSource(true), nil, []ParameterSpec{
				vPositional("directory", "guest 内目录", "/var/log", true, false),
				vPositional("tarball", "宿主机 tar 文件", "/tmp/logs.tar", true, false),
			}),
		vItem("virt-tar", "upload", cat, "兼容旧版的 virt-tar 上传模式。", "virt-tar [--options] -u domname tarball directory", docs("virt-tar"), "高", false, true, []string{"-u"},
			vGuestSource(true), nil, []ParameterSpec{
				vPositional("tarball", "宿主机 tar 文件", "/tmp/data.tar", true, false),
				vPositional("directory", "guest 内目标目录", "/opt/data", true, false),
			}),
	)

	// virt-rescue starts an interactive rescue shell. The catalog documents it
	// but deliberately prevents a queued job from hanging with no TTY.
	rescue := vItem("virt-rescue", "interactive", cat, "启动交互式救援 shell（需要 TTY，不能作为异步任务）。", "virt-rescue [--options] -d domname | -a disk.img [-a ...]", docs("virt-rescue"), "高", false, true, nil,
		vGuestSource(true), nil, nil)
	rescue.Disabled = true
	rescue.DisabledReason = "virt-rescue 需要交互式终端；请使用 guestfish/virt-customize 的非交互能力，或在宿主机终端执行。"
	items = append(items, rescue)

	tools := []string{
		"virt-builder", "virt-builder-repository", "guestfish", "guestmount", "guestunmount",
		"libguestfs-test-tool", "libguestfs-make-fixed-appliance", "virt-customize", "virt-dib",
		"virt-alignment-scan", "virt-cat", "virt-copy-in", "virt-copy-out", "virt-df",
		"virt-diff", "virt-edit", "virt-filesystems", "virt-format", "virt-get-kernel",
		"virt-inspector", "virt-list-filesystems", "virt-list-partitions", "virt-log",
		"virt-ls", "virt-make-fs", "virt-rescue", "virt-resize", "virt-sparsify",
		"virt-sysprep", "virt-tail", "virt-tar", "virt-tar-in", "virt-tar-out", "virt-win-reg",
	}
	for _, tool := range tools {
		items = append(items, vHelpVersion(tool, cat, docs(tool))...)
	}
	return items
}

func virtBuilderOptions() []ParameterSpec {
	return []ParameterSpec{
		vText("--output", "输出镜像", "/var/lib/libvirt/images/disk.qcow2"), vText("--size", "磁盘大小", "20G"), vChoice("--format", "输出格式", "qcow2", "raw", "vmdk", "vdi", "vhdx"), vText("--arch", "目标架构", "x86_64"),
		vMulti("--attach", "附加 ISO/磁盘（可重复）", "/path/extra.iso"), vText("--attach-format", "附加介质格式", "raw"),
		vText("--source", "模板/仓库 URL", "https://libguestfs.org/download/builder"), vBool("--no-cache", "禁用下载缓存"), vBool("--check-signature", "校验模板签名"), vBool("--no-check-signature", "跳过模板签名校验"), vBool("--no-sync", "不执行 fsync"), vBool("--no-warn-if-partition", "不警告分区表"), vBool("--network", "允许构建时联网"), vBool("--no-network", "禁止构建时联网"), vBool("--no-delete-on-failure", "失败时保留临时文件"), vBool("--machine-readable", "机器可读输出"), vBool("--colors", "彩色输出"), vBool("--quiet", "安静模式"), vBool("--verbose", "详细日志"),
	}
}

func guestfishOptions() []ParameterSpec {
	options := vGuestSource(true)
	return append(options,
		vBool("--ro", "以只读方式打开磁盘"),
		vBool("--rw", "以读写方式打开磁盘"),
		vMulti("--mount", "挂载 guest 文件系统（可重复）", "/dev/sda1:/"),
		vBool("--inspector", "自动检查并挂载 guest 文件系统（-i）"),
		vMulti("--file", "从文件读取 guestfish 命令（可重复）", "/srv/guestfish-script"),
		vBool("--cmd-help", "显示 guestfish API 命令帮助"),
		vText("--new", "创建指定大小的临时磁盘", "1G"),
		vBool("--network", "允许 appliance 联网"),
		vBool("--selinux", "启用 SELinux 支持"),
		vNumber("--blocksize", "块大小（字节）", "512"),
		vBool("--no-sync", "退出时不执行 fsync"),
		vBool("--no-progress-bars", "禁用进度条"),
		vBool("--progress-bars", "启用进度条"),
		vBool("--no-dest-paths", "不补全 guest 内目标路径"),
		vBool("--listen", "监听远程 guestfish 协议"),
		vBool("--remote", "连接远程 guestfish 协议"),
		vBool("--echo-keys", "回显 LUKS 密钥输入"),
		vBool("--keys-from-stdin", "从 stdin 读取密钥"),
		vBool("-x", "跟踪 libguestfs API 调用"),
		vBool("--verbose", "详细日志"),
	)
}

func guestmountOptions() []ParameterSpec {
	return []ParameterSpec{
		vMulti("--mount", "挂载设备（可重复）", "/dev/sda1:/"),
		vBool("--inspector", "自动检查并挂载 guest 文件系统（-i）"),
		vBool("--ro", "只读挂载"),
		vBool("--rw", "读写挂载"),
		vBool("--selinux", "启用 SELinux 支持"),
		vMulti("--option", "FUSE 挂载选项（可重复）", "allow_other"),
		vNumber("--dir-cache-timeout", "目录缓存秒数", "60"),
		vBool("--no-fork", "前台运行，不 fork"),
		vBool("--no-sync", "退出时不执行 fsync"),
		vText("--pid-file", "PID 文件", "/run/guestmount.pid"),
		vNumber("--fd", "使用指定文件描述符", "3"),
		vNumber("--blocksize", "块大小（字节）", "512"),
		vBool("--echo-keys", "回显 LUKS 密钥输入"),
		vBool("--keys-from-stdin", "从 stdin 读取密钥"),
		vBool("--trace", "跟踪 libguestfs API 调用"),
		vBool("--verbose", "详细日志"),
	}
}

func vCustomizeOperations() []ParameterSpec {
	return []ParameterSpec{
		vMulti("--append-line", "向文件追加一行（可重复）", "/etc/hosts:10.0.0.1 db.local"),
		vMulti("--chmod", "修改 guest 文件权限（可重复）", "0600:/etc/shadow"),
		vMulti("--chown", "修改 guest 文件所有者（可重复）", "0:0:/var/log/audit.log"),
		vText("--commands-from-file", "从文件读取定制命令", "/path/customize-commands"),
		vMulti("--copy", "guest 内复制（可重复）", "/etc/a:/etc/b"),
		vMulti("--copy-in", "从宿主机复制到 guest（可重复）", "/srv/config:/etc/config"),
		vMulti("--delete", "删除 guest 文件/目录（可重复）", "/var/log/*.log"),
		vMulti("--edit", "用 Perl 表达式修改文件（可重复）", "/etc/ssh/sshd_config:s/^#PermitRootLogin.*/PermitRootLogin no/"),
		vMulti("--firstboot", "首次启动脚本（可重复）", "/path/firstboot.sh"),
		vMulti("--firstboot-command", "首次启动命令（可重复）", "systemctl enable my.service"),
		vMulti("--firstboot-install", "首次启动时安装的软件包（可重复）", "nginx,curl"),
		vText("--hostname", "guest 主机名", "web01"),
		vMulti("--install", "构建时安装软件包（可重复）", "nginx,curl"),
		vMulti("--link", "创建符号链接（可重复）", "/etc/a:/etc/b"),
		vMulti("--mkdir", "创建目录（可重复）", "/opt/app"),
		vMulti("--move", "移动 guest 文件（可重复）", "/etc/a:/etc/b"),
		vMulti("--password", "设置用户密码（可重复）", "root:password:new-password"),
		vText("--root-password", "设置 root 密码", "password:new-password"),
		vMulti("--run", "运行 guest 内脚本（可重复）", "/path/script.sh"),
		vMulti("--run-command", "运行 guest 内命令（可重复）", "systemctl enable nginx"),
		vMulti("--scrub", "擦除文件内容（可重复）", "/var/log/messages"),
		vMulti("--ssh-inject", "注入 SSH 公钥（可重复）", "root:file:/root/.ssh/id_rsa.pub"),
		vMulti("--tar-in", "解压 tar 到 guest（可重复）", "/tmp/data.tar:/opt/data"),
		vText("--timezone", "时区", "Asia/Shanghai"),
		vMulti("--touch", "创建文件（可重复）", "/etc/motd"),
		vMulti("--truncate", "截断文件（可重复）", "/var/log/messages"),
		vMulti("--truncate-recursive", "递归截断（可重复）", "/var/log"),
		vMulti("--uninstall", "卸载软件包（可重复）", "telnet"),
		vBool("--update", "更新已安装软件包"),
		vMulti("--upload", "上传文件到 guest（可重复）", "/srv/app:/opt/app"),
		vMulti("--write", "写入文件内容（可重复）", "/etc/motd:Welcome"),
		vBool("--no-logfile", "不保留 virt-customize 日志"),
		vChoice("--password-crypto", "密码哈希算法", "md5", "sha256", "sha512"),
		vBool("--no-selinux-relabel", "不执行 SELinux relabel"), vBool("--selinux-relabel", "兼容选项（无实际操作）"),
		vText("--sm-register", "Spacewalk/Satellite 注册参数", "profile=web"), vText("--sm-remove", "移除 Spacewalk 系统 ID", "1"), vText("--sm-unregister", "注销 Spacewalk 系统", "1"),
	}
}

func vWindowsInjectionOperations() []ParameterSpec {
	return []ParameterSpec{
		vText("--inject-qemu-ga", "注入 QEMU Guest Agent", "method=msi"),
		vText("--inject-virtio-win", "注入 VirtIO Win 驱动", "method=iso"),
	}
}

func virtMigrationCatalog() []CatalogItem {
	cat := virtCategoryMigrate
	v2vDocs := "https://libguestfs.org/virt-v2v.1.html"
	v2vInPlaceDocs := "https://libguestfs.org/virt-v2v-in-place.1.html"
	v2vInspectorDocs := "https://libguestfs.org/virt-v2v-inspector.1.html"
	v2vOpenDocs := "https://libguestfs.org/virt-v2v-open.1.html"
	p2vDocs := "https://libguestfs.org/virt-p2v.1.html"
	p2vDiskDocs := "https://libguestfs.org/virt-p2v-make-disk.1.html"
	p2vKickstartDocs := "https://libguestfs.org/virt-p2v-make-kickstart.1.html"
	items := []CatalogItem{
		vItem("virt-v2v", "convert", cat, "把 VMware/Xen/OVA/磁盘中的虚拟机转换为 KVM/libvirt、oVirt/RHV、OpenStack 或本地镜像。", "virt-v2v [-i mode] [other -i* options] [-o mode] [other -o* options] [guest|filename]", v2vDocs, "极高", false, true, nil,
			nil, []ParameterSpec{
				vChoice("-i", "输入模式", "libvirt", "disk", "libvirtxml", "ova", "vmx", "xen", "local"),
				vText("-ic", "输入 libvirt URI", "qemu+ssh://esxi/system"),
				vText("-if", "输入格式", "qcow2"), vMulti("-io", "输入选项 OPTION=VALUE（可重复）", "vddk-thumbprint=AA:BB"),
				vText("-it", "VDDK 传输模式", "vddk"), vText("-ip", "输入密码文件", "/etc/wvc/v2v-password"), vMulti("-b", "输入/输出 bridge 映射（可重复）", "in:out"),
				vMulti("--network", "网络映射（可重复）", "in:out"), vMulti("--bridge", "bridge 映射（可重复）", "in:out"), vMulti("--mac", "MAC 映射（可重复）", "aa:bb:cc:dd:ee:ff:bridge:out"),
				vChoice("-o", "输出模式", "libvirt", "local", "disk", "null", "json", "openstack", "ovirt", "rhv", "rhv-upload", "vdsm"),
				vText("-oc", "输出 libvirt URI", "qemu:///system"), vChoice("-of", "输出格式", "qcow2", "raw"), vMulti("-oo", "输出选项 OPTION=VALUE（可重复）", "rhv-cluster=Default"), vText("-os", "输出存储", "default"), vText("-op", "输出密码文件", "/etc/wvc/output-password"), vText("-on", "输出名称", "converted-vm"), vChoice("-oa", "输出分配模式", "sparse", "preallocated"),
				vText("--bandwidth", "带宽限制（bps）", "104857600"), vText("--bandwidth-file", "带宽计划文件", "/path/bandwidth.conf"), vChoice("--root", "root 设备选择", "ask", "single", "first"), vBool("--in-place", "原地转换"), vBool("--compressed", "压缩输出"), vBool("--no-copy", "不复制磁盘"), vBool("--print-estimate", "只估算迁移大小"), vBool("--print-source", "只打印源信息"), vBool("--qemu-boot", "输出 qemu-boot 配置"), vBool("--debug-overlays", "保留调试 overlay"), vBool("--machine-readable", "机器可读输出"), vBool("--echo-keys", "回显密钥输入"), vBool("--keys-from-stdin", "从 stdin 读取密钥"), vBool("--quiet", "安静模式"), vBool("--verbose", "详细日志"), vBool("-x", "跟踪 libguestfs API"), vBool("--colors", "彩色输出"),
			}, []ParameterSpec{{Name: "guest-or-filename", Label: "源虚拟机或文件名", Placeholder: "vm01 或 /path/source", ValueType: "text"}}),
		vItem("virt-v2v-in-place", "convert", cat, "直接在源磁盘上原地完成虚拟机到 KVM 的转换，不保留原始磁盘副本。",
			"virt-v2v-in-place -i disk [other -i* options] [virt-customize options] filename [-O output.xml]", v2vInPlaceDocs, "极高", false, true, nil,
			nil, append(virtV2VInputOptions(),
				append([]ParameterSpec{vText("-O", "输出 libvirt XML 文件", "/path/converted.xml")}, vCustomizeOperations()...)...),
			[]ParameterSpec{{Name: "guest-or-filename", Label: "源虚拟机或磁盘文件", Placeholder: "vm01 或 /path/disk.qcow2", ValueType: "text", Required: true}}),
		vItem("virt-v2v-inspector", "inspect", cat, "在不转换的情况下检查源虚拟机、估算磁盘需求并输出可转换性 XML。",
			"virt-v2v-inspector [-i* options] [virt-customize options] guest [-O output.xml]", v2vInspectorDocs, "高", true, true, nil,
			nil, append(virtV2VInputOptions(),
				append([]ParameterSpec{vText("-O", "输出检查结果 XML", "/path/inspection.xml")}, vCustomizeOperations()...)...),
			[]ParameterSpec{{Name: "guest-or-filename", Label: "源虚拟机或磁盘文件", Placeholder: "vm01 或 /path/disk.qcow2", ValueType: "text", Required: true}}),
		vItem("virt-v2v-open", "run", cat, "打开 virt-v2v 输入并通过 @@ 占位符在 guest 镜像上运行指定程序。",
			"virt-v2v-open [-i* options] guest --run 'program [--options] @@'", v2vOpenDocs, "极高", false, true, nil,
			nil, append(virtV2VInputOptions(), vRequiredText("--run", "在输入上运行的程序（@@ 表示磁盘）", "virt-inspector --format=raw @@")),
			[]ParameterSpec{{Name: "guest-or-filename", Label: "源虚拟机或磁盘文件", Placeholder: "/path/disk.qcow2", ValueType: "text", Required: true}}),
		vItem("virt-p2v-make-disk", "build", cat, "把 virt-p2v 写入 U 盘/磁盘，用于物理机到 KVM 的迁移。", "virt-p2v-make-disk -o /dev/sdX [os-version]", p2vDiskDocs, "极高", false, true, nil,
			nil, []ParameterSpec{
				vText("--output", "目标设备或文件", "/dev/sdb"), vText("--arch", "目标架构", "x86_64"), vText("--install", "额外软件包（逗号分隔）", "tcpdump,traceroute"), vText("--inject-ssh-identity", "注入 SSH 私钥文件", "/root/.ssh/id_rsa"), vBool("--no-warn-if-partition", "写入分区时不告警"), vBool("--verbose", "详细日志"),
			}, []ParameterSpec{{Name: "os-version", Label: "p2v 操作系统版本", Placeholder: "rhel-8", ValueType: "text"}}),
		vItem("virt-p2v-make-kickstart", "build", cat, "生成 virt-p2v 启动 kickstart，供 PXE/安装服务器使用。", "virt-p2v-make-kickstart [-o p2v.ks] [--proxy=http://...] repo [repo...]", p2vKickstartDocs, "高", false, true, nil,
			nil, []ParameterSpec{
				vText("--output", "输出 kickstart", "/srv/tftp/p2v.ks"), vText("--proxy", "HTTP(S) 代理", "http://proxy:3128"), vText("--install", "额外软件包（逗号分隔）", "tcpdump,traceroute"), vText("--inject-ssh-identity", "注入 SSH 私钥文件", "/root/.ssh/id_rsa"), vBool("--scratch", "使用 koji scratch build"), vBool("--verbose", "详细日志"),
			}, []ParameterSpec{vPositional("repo", "软件仓库 URL（可重复）", "https://repo.example/os", true, true)}),
	}
	p2v := vItem("virt-p2v", "convert", cat, "图形化物理机到 KVM 转换客户端；必须在待迁移物理机启动 virt-p2v ISO，不能在服务器异步任务中运行。",
		"virt-p2v [--options]", p2vDocs, "极高", false, true, nil, nil, []ParameterSpec{
			vText("--cmdline", "调试用内核命令行覆盖", "root=/dev/sda1"), vBool("--iso", "启用 ISO 内危险操作（关机等）"), vText("--nbd", "NBD 服务优先级", "qemu-nbd,nbdkit"), vText("--test-disk", "测试用磁盘文件", "/path/disk.img"), vBool("--colors", "彩色输出"), vBool("--verbose", "详细日志"),
		}, nil)
	p2v.Disabled = true
	p2v.DisabledReason = "virt-p2v 是运行在待迁移物理机上的图形化 ISO 客户端，不是服务器端异步命令；请生成 p2v 介质后在物理机启动。"
	items = append(items, p2v)
	for _, tool := range []string{
		"virt-v2v", "virt-v2v-in-place", "virt-v2v-inspector", "virt-v2v-open",
		"virt-p2v", "virt-p2v-make-disk", "virt-p2v-make-kickstart",
	} {
		docs := v2vDocs
		switch tool {
		case "virt-v2v-in-place":
			docs = v2vInPlaceDocs
		case "virt-v2v-inspector":
			docs = v2vInspectorDocs
		case "virt-v2v-open":
			docs = v2vOpenDocs
		case "virt-p2v":
			docs = p2vDocs
		case "virt-p2v-make-disk":
			docs = p2vDiskDocs
		case "virt-p2v-make-kickstart":
			docs = p2vKickstartDocs
		}
		items = append(items, vHelpVersion(tool, cat, docs)...)
	}
	return items
}

func virtV2VInputOptions() []ParameterSpec {
	return []ParameterSpec{
		vChoice("-i", "输入模式", "libvirt", "disk", "libvirtxml", "ova", "vmx", "xen", "local"),
		vText("-ic", "输入 libvirt URI", "qemu+ssh://esxi/system"),
		vText("-if", "输入格式", "qcow2"),
		vMulti("-io", "输入选项 OPTION=VALUE（可重复）", "vddk-thumbprint=AA:BB"),
		vText("-it", "VDDK 传输模式", "vddk"),
		vText("-ip", "输入密码文件", "/etc/wvc/v2v-password"),
	}
}

func virtMonitorCatalog() []CatalogItem {
	cat := virtCategoryInspect
	whatDocs := "https://manpages.debian.org/trixie/virt-what/virt-what.1.en.html"
	topDocs := "https://manpages.debian.org/trixie/virt-top/virt-top.1.en.html"
	items := []CatalogItem{
		vItem("virt-what", "detect", cat, "检测当前主机是否运行在物理机、KVM、VMware、云平台或容器中。", "virt-what", whatDocs, "低", true, false, nil, nil, nil, nil),
		vItem("virt-top", "batch", cat, "以无界面批处理模式采集 libvirt 虚拟机 CPU、内存、磁盘和网络统计。", "virt-top -b [--csv FILE] -n iterations", topDocs, "低", true, false, nil,
			nil, []ParameterSpec{
				vRequiredBool("-b", "批处理模式（忽略按键）"), vRequiredNumber("-n", "采集次数", "1"), vNumber("-d", "采集间隔秒数", "3"), vChoice("-o", "排序字段", "cpu", "mem", "time", "id", "name", "netrx", "nettx", "blockrdrq", "blockwrrq"), vText("--csv", "CSV 输出文件", "/tmp/virt-top.csv"), vBool("--no-csv-cpu", "CSV 不含 CPU 统计"), vBool("--no-csv-mem", "CSV 不含内存统计"), vBool("--no-csv-block", "CSV 不含块设备统计"), vBool("--no-csv-net", "CSV 不含网络统计"), vBool("--no-init-file", "不读取 ~/.virt-toprc"), vText("--init-file", "初始化配置文件", "/etc/virt-toprc"), vText("--debug", "调试日志文件", "/tmp/virt-top-debug.log"), vBool("--stream", "流式输出"), vBool("--block-in-bytes", "块 I/O 以字节显示"), vNumber("--hist-cpu", "历史 CPU 更新秒数", "60"), vText("--end-time", "退出时间", "+00:05:00"), vBool("-1", "按物理 CPU 显示"), vBool("-2", "按网络接口显示"), vBool("-3", "按块设备显示"),
			}, nil),
	}
	items = append(items, vHelpVersion("virt-what", cat, whatDocs)...)
	items = append(items, vHelpVersion("virt-top", cat, topDocs)...)
	return items
}
