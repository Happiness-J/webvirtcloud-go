package command

// qemuToolsCatalog covers the standalone tools documented by QEMU in addition
// to qemu-img, qemu-io and qemu-nbd. qemu-system-* is deliberately managed via
// libvirt instead of being launched as an untracked daemon from the panel.
func qemuToolsCatalog() []CatalogItem {
	items := make([]CatalogItem, 0, 16)
	items = append(items, qemuStorageDaemonCatalog()...)
	items = append(items, qemuPRHelperCatalog()...)
	items = append(items, qemuTraceSTAPCatalog()...)
	items = append(items, qemuVMSRHelperCatalog()...)
	items = append(items, qemuVNCCatalog()...)
	return items
}

func qemuStorageDaemonCatalog() []CatalogItem {
	const docs = "https://www.qemu.org/docs/master/tools/qemu-storage-daemon.html"
	return []CatalogItem{
		{
			Tool: "qemu-storage-daemon", Command: "serve", Category: "QEMU 存储守护进程",
			Summary: "启动由 QMP 控制、不运行虚拟机的长期存储守护进程。", Usage: "[OPTIONS]",
			Risk: "高", DocsURL: docs, Sensitive: true,
			Options: []ParameterSpec{
				{Name: "blockdev", Flag: "--blockdev", Label: "块节点定义（可重复）", Placeholder: "driver=qcow2,node-name=disk,file.driver=file,file.filename=/path/disk.qcow2", ValueType: "text", Multiple: true},
				{Name: "chardev", Flag: "--chardev", Label: "字符设备定义（可重复）", Placeholder: "socket,id=char1,path=/run/qsd-qmp.sock,server=on,wait=off", ValueType: "text", Multiple: true},
				{Name: "export", Flag: "--export", Label: "块导出定义（可重复）", Placeholder: "nbd,id=export0,node-name=disk,writable=off", ValueType: "text", Multiple: true},
				{Name: "monitor", Flag: "--monitor", Label: "QMP monitor 定义（可重复）", Placeholder: "chardev=char1", ValueType: "text", Multiple: true},
				{Name: "nbdServer", Flag: "--nbd-server", Label: "NBD 服务定义（可重复）", Placeholder: "addr.type=inet,addr.host=127.0.0.1,addr.port=10809", ValueType: "text", Multiple: true},
				{Name: "object", Flag: "--object", Label: "QEMU 对象定义（可重复）", Placeholder: "secret,id=sec0,file=/run/secrets/disk.key", ValueType: "text", Multiple: true},
				{Name: "pidfile", Flag: "--pidfile", Label: "PID 文件", Placeholder: "/run/qemu-storage-daemon.pid", ValueType: "text"},
				{Name: "daemonize", Flag: "--daemonize", Label: "后台运行", ValueType: "bool"},
				{Name: "trace", Flag: "-T", Label: "Trace 配置（可重复）", Placeholder: "enable=... 或 events=...", ValueType: "text", Multiple: true},
			},
		},
		qemuToolHelpItem("qemu-storage-daemon", "QEMU 存储守护进程", "--help", "显示 qemu-storage-daemon 帮助。", docs),
		qemuToolVersionItem("qemu-storage-daemon", "QEMU 存储守护进程", "显示 qemu-storage-daemon 版本。", docs),
	}
}

func qemuPRHelperCatalog() []CatalogItem {
	const docs = "https://www.qemu.org/docs/master/tools/qemu-pr-helper.html"
	return []CatalogItem{
		{
			Tool: "qemu-pr-helper", Command: "serve", Category: "QEMU PR Helper",
			Summary: "启动 SCSI 持久保留 helper socket，供 QEMU 特权 SCSI 透传使用。", Usage: "[OPTION]",
			Risk: "高", DocsURL: docs, Sensitive: true,
			Options: []ParameterSpec{
				{Name: "daemon", Flag: "--daemon", Label: "后台运行并创建 PID 文件", ValueType: "bool"},
				{Name: "quiet", Flag: "--quiet", Label: "降低日志级别", ValueType: "bool"},
				{Name: "verbose", Flag: "--verbose", Label: "提高日志级别", ValueType: "bool"},
				{Name: "pidfile", Flag: "--pidfile", Label: "PID 文件", Placeholder: "/run/qemu-pr-helper.pid", ValueType: "text"},
				{Name: "socket", Flag: "--socket", Label: "Unix socket 路径", Placeholder: "/run/qemu-pr-helper.sock", ValueType: "text"},
				{Name: "trace", Flag: "-T", Label: "Trace 配置（可重复）", ValueType: "text", Multiple: true},
				{Name: "user", Flag: "--user", Label: "降权用户", Placeholder: "qemu", ValueType: "text"},
				{Name: "group", Flag: "--group", Label: "降权用户组", Placeholder: "qemu", ValueType: "text"},
			},
		},
		qemuToolHelpItem("qemu-pr-helper", "QEMU PR Helper", "--help", "显示 qemu-pr-helper 帮助。", docs),
		qemuToolVersionItem("qemu-pr-helper", "QEMU PR Helper", "显示 qemu-pr-helper 版本。", docs),
	}
}

func qemuTraceSTAPCatalog() []CatalogItem {
	const docs = "https://www.qemu.org/docs/master/tools/qemu-trace-stap.html"
	verbose := ParameterSpec{Name: "verbose", Flag: "-v", Label: "显示 SystemTap 执行详情", ValueType: "bool"}
	return []CatalogItem{
		{
			Tool: "qemu-trace-stap", Command: "list", Category: "QEMU Trace",
			Summary: "列出 QEMU 二进制中匹配模式的所有 SystemTap probe。", Usage: "[-v] list BINARY [PATTERN...]",
			Risk: "低", DocsURL: docs, ReadOnly: true,
			Common: []ParameterSpec{verbose}, ArgPrefix: []string{"list"},
			Positionals: []ParameterSpec{
				{Name: "binary", Label: "QEMU 二进制路径", Placeholder: "qemu-system-x86_64", ValueType: "text", Required: true},
				{Name: "pattern", Label: "Probe 匹配模式（每行一个，可留空）", Placeholder: "qcrypto*", ValueType: "text", Multiple: true},
			},
		},
		{
			Tool: "qemu-trace-stap", Command: "run", Category: "QEMU Trace",
			Summary: "运行 SystemTap 会话并输出匹配的 QEMU probe。", Usage: "[-v] run [--pid PID] BINARY PATTERN...",
			Risk: "中", DocsURL: docs, Sensitive: true,
			Common: []ParameterSpec{verbose}, ArgPrefix: []string{"run"},
			Options: []ParameterSpec{
				{Name: "pid", Flag: "--pid", Label: "只跟踪指定进程 PID", Placeholder: "1732", ValueType: "number"},
			},
			Positionals: []ParameterSpec{
				{Name: "binary", Label: "QEMU 二进制路径", Placeholder: "qemu-system-x86_64", ValueType: "text", Required: true},
				{Name: "pattern", Label: "Probe 匹配模式（每行一个）", Placeholder: "qio*", ValueType: "text", Required: true, Multiple: true},
			},
		},
	}
}

func qemuVMSRHelperCatalog() []CatalogItem {
	const docs = "https://www.qemu.org/docs/master/tools/qemu-vmsr-helper.html"
	return []CatalogItem{
		{
			Tool: "qemu-vmsr-helper", Command: "serve", Category: "QEMU RAPL Helper",
			Summary: "启动虚拟 RAPL MSR helper socket，为 QEMU 提供受保护的能耗寄存器访问。", Usage: "[OPTION]",
			Risk: "高", DocsURL: docs, Sensitive: true,
			Options: []ParameterSpec{
				{Name: "daemon", Flag: "--daemon", Label: "后台运行并创建 PID 文件", ValueType: "bool"},
				{Name: "quiet", Flag: "--quiet", Label: "降低日志级别", ValueType: "bool"},
				{Name: "verbose", Flag: "--verbose", Label: "提高日志级别", ValueType: "bool"},
				{Name: "pidfile", Flag: "--pidfile", Label: "PID 文件", Placeholder: "/run/qemu-vmsr-helper.pid", ValueType: "text"},
				{Name: "socket", Flag: "--socket", Label: "Unix socket 路径", Placeholder: "/run/qemu-vmsr-helper.sock", ValueType: "text"},
				{Name: "trace", Flag: "-T", Label: "Trace 配置（可重复）", ValueType: "text", Multiple: true},
				{Name: "user", Flag: "--user", Label: "降权用户", Placeholder: "qemu", ValueType: "text"},
				{Name: "group", Flag: "--group", Label: "降权用户组", Placeholder: "qemu", ValueType: "text"},
			},
		},
		qemuToolHelpItem("qemu-vmsr-helper", "QEMU RAPL Helper", "--help", "显示 qemu-vmsr-helper 帮助。", docs),
		qemuToolVersionItem("qemu-vmsr-helper", "QEMU RAPL Helper", "显示 qemu-vmsr-helper 版本。", docs),
	}
}

func qemuVNCCatalog() []CatalogItem {
	const docs = "https://www.qemu.org/docs/master/tools/qemu-vnc.html"
	return []CatalogItem{
		{
			Tool: "qemu-vnc", Command: "serve", Category: "QEMU VNC",
			Summary: "连接 QEMU D-Bus 显示后端并启动独立 VNC 服务。", Usage: "[OPTIONS]",
			Risk: "中", DocsURL: docs, Sensitive: true,
			Options: []ParameterSpec{
				{Name: "dbusAddress", Flag: "--dbus-address", Label: "D-Bus 地址", Placeholder: "unix:path=/run/qemu/display.sock", ValueType: "text"},
				{Name: "dbusP2PFd", Flag: "--dbus-p2p-fd", Label: "D-Bus P2P 文件描述符", ValueType: "number"},
				{Name: "busName", Flag: "--bus-name", Label: "D-Bus bus name", Placeholder: "org.qemu", ValueType: "text"},
				{Name: "wait", Flag: "--wait", Label: "等待 D-Bus bus name 出现", ValueType: "bool"},
				{Name: "password", Flag: "--password", Label: "要求运行时设置 VNC 密码", ValueType: "bool"},
				{Name: "vncAddr", Flag: "--vnc-addr", Label: "VNC 监听地址", Placeholder: "localhost:0", ValueType: "text"},
				{Name: "websocket", Flag: "--websocket", Label: "WebSocket 监听地址", Placeholder: "localhost:5700", ValueType: "text"},
				{Name: "object", Flag: "--object", Label: "QEMU 对象定义（可重复）", ValueType: "text", Multiple: true},
				{Name: "tlsCreds", Flag: "--tls-creds", Label: "TLS 证书目录", Placeholder: "/etc/pki/qemu-vnc", ValueType: "text"},
				{Name: "tlsAuthz", Flag: "--tls-authz", Label: "TLS 授权对象 ID", ValueType: "text"},
				{Name: "sasl", Flag: "--sasl", Label: "启用 SASL 认证", ValueType: "bool"},
				{Name: "saslAuthz", Flag: "--sasl-authz", Label: "SASL 授权对象 ID", ValueType: "text"},
				{Name: "share", Flag: "--share", Label: "显示共享策略", ValueType: "choice", Choices: []string{"allow-exclusive", "force-shared", "ignore"}},
				{Name: "vtChardev", Flag: "--vt-chardev", Label: "文本控制台 chardev（可重复）", ValueType: "text", Multiple: true},
				{Name: "noVT", Flag: "--no-vt", Label: "禁用文本控制台", ValueType: "bool"},
				{Name: "keyboardLayout", Flag: "--keyboard-layout", Label: "键盘布局", Placeholder: "en-us", ValueType: "text"},
				{Name: "lossy", Flag: "--lossy", Label: "允许有损压缩", ValueType: "bool"},
				{Name: "nonAdaptive", Flag: "--non-adaptive", Label: "禁用自适应编码", ValueType: "bool"},
				{Name: "trace", Flag: "-T", Label: "Trace 配置（可重复）", ValueType: "text", Multiple: true},
			},
		},
		qemuToolHelpItem("qemu-vnc", "QEMU VNC", "--help", "显示 qemu-vnc 帮助。", docs),
		qemuToolVersionItem("qemu-vnc", "QEMU VNC", "显示 qemu-vnc 版本。", docs),
	}
}

func qemuToolHelpItem(tool, category, flag, summary, docs string) CatalogItem {
	return CatalogItem{
		Tool: tool, Command: "help", Category: category, Summary: summary, Usage: flag,
		ReadOnly: true, Risk: "低", DocsURL: docs, ArgPrefix: []string{flag},
	}
}

func qemuToolVersionItem(tool, category, summary, docs string) CatalogItem {
	return CatalogItem{
		Tool: tool, Command: "version", Category: category, Summary: summary, Usage: "--version",
		ReadOnly: true, Risk: "低", DocsURL: docs, ArgPrefix: []string{"--version"},
	}
}
