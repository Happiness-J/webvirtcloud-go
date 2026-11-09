# 命令覆盖与执行边界

## 入口

1. **VM 详情页**：启动、关机、重启、强制断电、挂起/恢复、自启动、快照、删除定义；破坏性操作要求服务端确认字段。
2. **任务页**：异步执行、查看输出、取消、审计。
3. **命令中心**（管理员）：按工具/分类搜索官方目录，点选命令后使用自动生成的位置参数、开关、数值和枚举字段构建 argv；破坏性命令仍要求确认。版本特有参数可切换到“高级原始参数”。

命令中心从 libvirt、virtinst、libguestfs 与 QEMU 官方文档生成结构目录，并在运行时按执行目标探测工具可用性。本机目标读取版本和命令支持；`qemu+ssh` 目标用一次 SSH 检查目标机 PATH。当前包含 284 个 virsh 入口、65 个 QEMU 工具入口和 181 个 virt/libguestfs 入口，共 530 个结构化入口。探测仅用于提示，不替代实际工具校验；目录保留跨版本命令，避免升级面板后旧主机不可用。

浏览器只提交结构化 argv JSON（或高级模式下的原始参数文本）。服务端会重新检查工具、参数数量/长度、stdin/输出上限、只读属性、连接覆盖、敏感等级和当前用户角色，然后写入异步任务；不会把浏览器输入交还给 shell 执行。

## 执行与权限边界

- 命令中心仅管理员可进入；`viewer` 只读，`operator` 仅能执行允许的生命周期和查询动作，`admin` 可执行任意目录命令。破坏性操作必须显式确认。
- 原始 argv 入口仍受同一套工具白名单、连接 URI 注入和敏感命令策略约束；不能借原始模式绕过 `qemu-system-*` 禁用或交互命令限制。
- Worker 在真正执行前会重新读取用户，检查账号未禁用、任务类型与工具组合合法，并按具体 `virsh` 动作复核最低角色；敏感 `command` 还要求当前角色仍是 `admin`。排队期间被降权的用户不会继续获得旧权限，例如管理员提交的 `undefine` 在降为 `operator` 后会被拒绝。

## virsh

连接 URI 从加密的主机配置注入。用户参数中的 `--connect`、`--connect=...`、GNU 长选项缩写以及 `connect` 内建子命令都会被拒绝；对于确实以 `-c` 表示连接的 virt/libguestfs 工具，`-c` 也会被拒绝。`virt-install` 的 `-c` 是 `--cdrom`，因此仍可正常使用。只读 `virsh` 命令由服务端分类并附加 `--readonly`，客户端不能自行伪造。VM、存储和网络页面的清单读取同样从主机配置解析执行目标；`qemu+ssh` 主机通过目标机 SSH 执行 `virsh`，不会在面板本机直连。

常见覆盖类别：

- 查询：`list`、`dominfo`、`domstate`、`domstats`、`domblklist`、`domiflist`、`dommemstat`、`cpu-stats`、`vcpucount`、`domcapabilities`、`capabilities`、`nodeinfo`、`version`、`sysinfo`。
- 生命周期：`start`、`shutdown`、`reboot`、`reset`、`destroy`、`suspend`、`resume`、`autostart`、`managedsave`、`restore`、`save`、`undefine`。
- XML：`dumpxml`、`define`、`edit`、`metadata`。
- 快照：`snapshot-create`、`snapshot-create-as`、`snapshot-list`、`snapshot-info`、`snapshot-revert`、`snapshot-delete`、`snapshot-dumpxml`。
- 磁盘/网络：`attach-disk`、`detach-disk`、`attach-interface`、`detach-interface`、`domblkstat`、`domifstat`、`blockresize`、`blockcopy`、`blockcommit`、`blockpull`。
- 资源调优：`setvcpus`、`setmem`、`setmaxmem`、`memtune`、`schedinfo`、`numatune`、`blkiotune`、`vcpu-pin`。
- 迁移：`migrate`、`migrate-to-uri`、`domjobinfo`、`domjobabort`。
- 存储：`pool-*`、`vol-*`，包括创建、构建、启动、刷新、扩容、克隆、上传/下载、删除。
- 网络：`net-*`、`iface-*`、`nwfilter-*`、`net-dhcp-leases`。
- Secret：`secret-*`，其中明文导出仅管理员。
- 高级：`qemu-monitor-command`、`qemu-agent-command`。

不同 libvirt 版本支持的参数不同：目录来自官方文档，执行能力由目标主机上的实际 `virsh` 决定；页面会显示“当前版本已列出/未列出”，但不会把探测结果当成新的权限边界。`console`、`edit`、事件流、`connect` 等交互式入口在页面中明确禁用或引导到 VNC/原始入口；`virsh start/create --console` 会持续附着控制台，因此也禁止作为异步任务执行，请改用 Web VNC 控制台。`qemu-monitor-command`、`qemu-agent-command` 等仍以管理员敏感命令执行。

连接 URI 只允许 `qemu`、`qemu+ssh`、`qemu+unix`、`qemu+tcp`、`qemu+tls`，且不能为空。URL 密码、fragment、未知 query 参数和用户提供的 `--connect`/`-c`、其长选项缩写以及 `connect` 子命令都会被拒绝；连接信息始终来自加密的主机配置。

## qemu-img

页面提供 `amend`、`bench`、`bitmap`、`check`、`commit`、`compare`、`convert`、`create`、`dd`、`info`、`map`、`measure`、`rebase`、`resize`、`snapshot` 全部子命令，以及 `--help`/`--version` 和全局 `-T` trace。等价命令行示例：

```text
info /var/lib/libvirt/images/vm.qcow2
check -f qcow2 /path/disk.qcow2
create -f qcow2 /path/new.qcow2 20G
convert -p -f qcow2 -O raw input.qcow2 output.raw
resize /path/disk.qcow2 +10G
snapshot -l /path/disk.qcow2
bitmap --help
```

`convert` 的 `filename2` 字段每行填写一个额外输入镜像；`resize` 的大小字段直接填写 `+10G` 或 `-5G`，不会把符号拆成独立参数；`bitmap` 子命令保留镜像后的 bitmap 名称。

## qemu-io

页面提供 26 个 qemu-io 命令入口以及工具级 `--help`/`--version`，并生成文件选项和 `-c` 命令串。`-f/-t/-r/-s/-n/-C/-m/-k/-i/-d/-T/-U/--object/--image-opts` 等文件级选项位于 `-c` 之前，命令自身的开关位于 `-c` 字符串内部：

```text
-f qcow2 -r -c "read -v 0 4096" /path/disk.qcow2
-t writeback -c "write -P 0x00 0 4096" /path/disk.raw
-f raw -c "readv 0 512 512" /path/disk.raw
-c "sleep 1000"
```

`sleep` 的毫秒参数在 qemu-io 帮助正文中要求、但部分版本的 synopsis 未写出，页面已按必填数值字段处理。`break`、`remove_break`、`resume`、`wait_break` 等状态相关命令依赖同一 qemu-io 进程内的连续命令；页面为这些入口提供“前置命令”和“后续命令”字段，每一行会生成一个独立的 `-c`，按“前置命令 → 当前命令 → 后续命令 → 镜像路径”的顺序提交，不需要切换到高级模式。例如：

```text
qemu-io -c "aio_write -P 0x41 0 4096" \
  -c "break 4096" \
  -c "wait_break 4096" \
  /path/disk.raw
```

`qemu-io -c` 的命令串不是 shell：它按空白切分参数，不解析单引号、双引号或反斜杠转义。页面因此不会给命令串添加 shell 引号；命令级字段如果包含空白会被表单拒绝。镜像路径本身可以是带空格的独立 argv，但 `-s source_file` 这类位于 `-c` 内部的路径不能包含空白。

## qemu-nbd

页面提供导出、列举、连接、断开、帮助和版本入口，全部帮助页选项均可点选：

```text
--list
--help
--read-only --format=qcow2 --port=10809 /path/disk.qcow2
-c /dev/nbd0 --format=qcow2 /path/disk.qcow2
-d /dev/nbd0
```

`connect` 可点选 format、read-only、snapshot、cache、AIO、discard、detect-zeroes、image-opts 等块设备选项；`disconnect` 按官方 `qemu-nbd -d dev` 语法只填写设备路径。`--tls-hostname`、`--handshake-limit` 等新版本选项会保留在目录中，旧版本执行时会由实际 QEMU 报“未知选项”。

## 其他 QEMU 独立工具

QEMU 官方 Tools 手册中的其余独立程序也已接入命令中心，全部选项按官方帮助页生成可点选字段：

- `qemu-storage-daemon`：`--blockdev`、`--chardev`、`--export`、`--monitor`、`--nbd-server`、`--object`、`--pidfile`、`--daemonize` 和 trace；可由 QMP 控制块作业与导出，不启动虚拟机。
- `qemu-pr-helper`：SCSI 持久保留 helper 的 daemon、socket、PID、降权用户/组和 trace 选项；通常应作为宿主机 systemd 服务运行。
- `qemu-vmsr-helper`：虚拟 RAPL MSR helper 的 daemon、socket、PID、降权用户/组和 trace 选项；依赖 Intel RAPL/powercap 驱动和相应权限，缺少驱动时会启动失败。
- `qemu-vnc`：连接 QEMU D-Bus 显示后端的独立 VNC 服务，支持 VNC/WebSocket 地址、TLS、SASL、授权对象、共享策略、键盘布局和 trace。
- `qemu-trace-stap`：`list` 与 `run` 两个 SystemTap 跟踪入口，支持 Probe 模式、QEMU 二进制路径和 PID 过滤。

这些工具通常是长期运行的服务。`--daemonize`/`--daemon`/`--fork` 会让进程脱离任务进程组，`--listen` 会等待远程交互，因此表单会禁用这些字段并显示原因；服务端也会拒绝原始 argv 中的对应模式。需要常驻服务时，请用 systemd 或容器管理固定 socket、PID 文件、降权用户和重启策略。

## virtinst 与 libvirt 辅助工具

命令中心提供以下可点选入口；支持 libvirt 连接的工具会从当前主机配置注入 `--connect`：

- `virt-install`：名称、内存、vCPU、CPU/机型、os-variant、安装 ISO/安装树/PXE/import、cloud-init、磁盘、文件系统、网络、VNC/SPICE、设备直通、TPM/RNG/VSOCK/IOMMU、调优、autostart、dry-run 和 XML 输出。
- `virt-clone`：源虚拟机、自动克隆、新名称/UUID、磁盘路径、MAC、reflink、替换和仅打印 XML。
- `virt-xml`：`--update/--add-device/--remove-device/--build-xml` 动作，define/start/print-diff/print-xml，以及磁盘、网卡、图形、控制器、主机设备、内存/CPU/NUMA/IO 调优等 XML 修改字段。
- `virt-admin`：`server-list`、日志过滤器/输出、daemon 超时/关闭、线程池、客户端限额、TLS 重载、client 列表/详情/断连等 13 个子命令逐项成表。
- `virt-host-validate`、`virt-qemu-run`、`virt-xml-validate`：宿主机前置条件检查、独立 guest XML 启动和 libvirt XML RNG 校验。
- `virt-pki-validate`、`virt-sanlock-cleanup`：libvirt TLS 证书目录校验和 sanlock 锁文件清理。
- `virt-convert`：保留旧版 virtinst 工具入口；新版发行版若未打包该命令，会显示“缺失”，实际迁移优先使用 `virt-v2v`。

`virt-login-shell`、`virt-rescue` 需要交互式 TTY，`virt-p2v` 是物理机图形客户端；目录会明确标记为不可异步执行。交互式控制台请使用内置 noVNC，物理机迁移请先生成 p2v 介质后在目标物理机启动。

## libguestfs 工具

页面覆盖官方 libguestfs 工具族，并为每个工具生成独立字段：

- 检查与读取：`virt-cat`、`virt-df`、`virt-diff`、`virt-filesystems`、`virt-inspector`、`virt-list-filesystems`、`virt-list-partitions`、`virt-log`、`virt-ls`、`virt-alignment-scan`、`virt-win-reg`。
- 构建与修改：`virt-builder`、`virt-builder-repository`、`virt-customize`、`virt-sysprep`、`virt-format`、`virt-resize`、`virt-sparsify`、`virt-make-fs`、`virt-dib`。
- 文件与归档：`virt-copy-in`、`virt-copy-out`、`virt-edit`、`virt-tail`、`virt-tar`、`virt-tar-in`、`virt-tar-out`、`virt-get-kernel`。
- 交互 API 与挂载：`guestfish` 通过表单中的命令列表执行非交互 libguestfs API；`guestunmount` 用于清理面板主机上已有的 FUSE 挂载。`guestmount` 默认 fork 并持续等待卸载，无法由有限异步任务托管，页面保留说明但禁止提交；请改用 `guestfish`、`virt-copy-*` 或 `virt-cat`。`libguestfs-test-tool` 用于自检 appliance，`libguestfs-make-fixed-appliance` 用于离线 appliance。
- 迁移：`virt-v2v` 支持输入/输出模式、VDDK 参数、网络/bridge/MAC 映射及 RHV/OpenStack/oVirt 输出选项；`virt-v2v-in-place`、`virt-v2v-inspector`、`virt-v2v-open` 分别提供原地迁移、迁移前检查和输入打开。`virt-p2v` 是物理机 ISO 图形客户端，只保留目录说明并禁止异步执行；`virt-p2v-make-disk` 与 `virt-p2v-make-kickstart` 用于生成迁移介质。

磁盘路径的解释跟随执行目标：本机目标使用面板进程或 app 容器可见的路径；`qemu+ssh` 目标使用目标物理机路径，因为命令实际在该物理机执行；`qemu+tcp/tls` 目标只允许 libvirt 感知工具。`virt-format`、`virt-resize`、`virt-sparsify` 不接受 `--connect`，但可以在 `qemu+ssh` 主机上通过 SSH 执行。libguestfs 需要目标环境中的 guestfs appliance、临时目录和足够资源；生产机若需要 KVM 加速，应提供 `/dev/kvm`，否则会退回较慢的软件模拟。

命令目录覆盖上游工具语法，但 Docker 镜像只安装 Debian 仓库可提供的子集。Debian 未打包或移除的工具（例如 `qemu-vnc`、`virt-convert`、`virt-list-filesystems`、`virt-list-partitions`、`virt-tar`）会显示“缺失”；需要时请自行构建，并通过对应的 `WVC_*_BIN` 指向二进制。目录存在不代表镜像里已经有该可执行文件。

`guestmount` 即使手工开启 `/dev/fuse` 和 `CAP_SYS_ADMIN`，其默认 fork/长期挂载语义仍不适合异步任务队列；服务端会拒绝执行。请使用 `guestfish`/`virt-copy-*`/`virt-cat` 等非挂载方式。

## 宿主机检测与监控

- `virt-what` 识别物理机、KVM、VMware、云平台或容器环境。
- `virt-top` 必须同时启用 `-b`（或 `--script`）和正数 `-n`/`--iterations`；可点选采样次数、间隔、排序、CSV 文件、流式输出和统计开关。服务端拒绝无次数上限的调用，避免任务持续运行到超时。

## 执行位置

面板可以同时纳管多台 KVM 物理机，且任务真正在所选物理机执行。`qemu+ssh://` 主机上的 `virsh`、所有 QEMU 独立工具和 virt/libguestfs 工具通过加固 OpenSSH 执行；libvirt 工具在远端使用 `qemu:///system` 或 `qemu:///session`，不会嵌套 `qemu+ssh`。`qemu:///`/`qemu+unix://` 主机在面板本机执行。`qemu+tcp://`/`qemu+tls://` 没有 SSH 端点，只允许接受 `--connect` 的 libvirt 工具，独立 QEMU 工具会收到明确拒绝。

每台物理机的写任务使用 `host:<hostID>` 串行域，不同物理机可并行；只读任务可并行。带宿主文件输出的任务按写任务处理，例如 `virt-top --csv/--debug` 和 `virt-v2v-inspector -O`，不会绕过同主机串行锁。页面不会在远程工具不可用时回退到面板本机。

## 为什么不直接启动 qemu-system-*

由 libvirt 管理的 VM 直接启动 `qemu-system-*` 会导致：

- libvirt 不知道 VM 正在运行，`virsh` 状态错误；
- 磁盘锁、迁移、快照和 VNC 端口冲突；
- 进程生命周期脱离任务队列，Web 重启后成为孤儿进程；
- 安全组、AppArmor/SELinux 和 cgroup 限制被绕过。

`virt-qemu-run` 也是独立 guest 入口：它通过 libvirt 库启动一个随该前台进程结束的临时 guest，不纳入常驻 libvirtd 的生命周期管理。命令中心保留它用于管理员排障/测试，任务超时会终止进程组；生产虚拟机请使用 `virt-install` 创建并通过 `virsh` 管理。

因此生产 VM 生命周期通过 `virsh`/libvirt 执行；`virt-qemu-run` 仅作为管理员排障/测试的独立临时 guest 例外。QEMU 的镜像、I/O、导出、存储守护、跟踪、RAPL、PR 和独立 VNC 能力通过 QEMU 官方独立工具覆盖；运行中虚拟机的设备、CPU、内存、快照、迁移等能力通过 virsh、QMP 和 Guest Agent 覆盖。`qemu-system-*` 的启动参数不直接作为任意命令暴露。

## Web 控制台网络边界

- 默认只允许本机 libvirt、`qemu+unix` 和 `qemu+ssh` 连接 VNC。
- `qemu+tcp` / `qemu+tls` 可正常用于 `virsh`，但 VNC 协议本身没有传输加密；Web 控制台默认拒绝这类远程明文 VNC。
- 只有显式设置 `WVC_ALLOW_INSECURE_VNC=true` 时才会直连远程 VNC。启用前必须确认面板到宿主机的网络已由 VPN、隔离网络或其他加密隧道保护。
- WebSocket 与 VNC 密码使用不同用途的短时令牌；每个令牌绑定当前登录用户、主机和 VM，并且只能消费一次。断线后浏览器不会复用旧令牌，请刷新控制台页面重新签发。
- 令牌位于 URL query 中。部署示例的 Nginx 使用 `$uri` 记录访问日志以避免泄漏；仍应避免把控制台 URL 复制到工单、聊天或浏览器书签。
- 当前 Web 控制台只支持 VNC；虚拟机只有 SPICE 时页面会提示不支持。
