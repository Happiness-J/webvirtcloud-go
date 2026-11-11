# WebVirtCloud Go

一个面向单组织、多用户的 QEMU/KVM / libvirt 生产级控制面。它不是旧 Python WebVirtCloud 的逐行移植，而是保留核心能力后重新实现的 Go 版本：

> 开源协作入口：[English overview](README.en.md) · [开发指南](docs/DEVELOPMENT.md) · [运维与备份](docs/OPERATIONS.md) · [威胁模型](docs/THREAT-MODEL.md) · [GitHub 发布指南](docs/PUBLISHING.md) · [安全政策](SECURITY.md)

- 服务端渲染 Web UI，模板和静态资源全部嵌入单个二进制；运行时不依赖 Node。
- PostgreSQL + `pgx`，任务队列使用 `FOR UPDATE SKIP LOCKED`，支持多 worker、取消、心跳和重启回收；同一主机的写任务串行执行，只读任务仍可并行。
- Argon2id 密码、服务端会话、CSRF、登录限流、角色权限和审计留痕；主审计链暂时失败时事件进入 PostgreSQL durable outbox，由后台任务重试写回，业务操作不会因此回滚。
- 主机 URI 与任务载荷使用 AES-256-GCM 加密，密文通过 AAD 绑定到主机 ID、任务 ID 和字段。
- 本机命令不经过 shell，以 argv 直接执行；`qemu+ssh` 主机的命令经服务端严格 POSIX 转义后通过加固 OpenSSH 执行。连接 URI 只能来自加密的主机配置。
- 内置 noVNC 1.7.0 和 WebSocket-to-TCP 控制台代理；`qemu+ssh` 主机通过 OpenSSH `-W` 隧道连接 VNC。
- 三层操作入口：常用 VM 快捷操作、完整异步任务、管理员高级命令中心。
- 主机运维工作台：节点 CPU/内存状态、虚拟机 balloon/vCPU 资源视图、最多 50 台虚拟机的批量启停/重启/挂起/自启动策略，以及 vCPU 和内存的持久化/在线调优。

## 架构

一句话数据流：浏览器 → Go Web/API → PostgreSQL 任务队列 → Worker → `virsh` / QEMU 独立工具 → libvirt / QEMU KVM；控制台流量由 Go 内置的 noVNC WebSocket 代理转发到 VM VNC。

```mermaid
flowchart TB
    User["管理员 / 运维用户<br/>浏览器"]
    Proxy["Nginx / TLS 反向代理<br/>可选"]

    subgraph App["WebVirtCloud Go（单二进制）"]
        Web["Web UI / REST API<br/>会话、CSRF、RBAC、限流"]
        Console["noVNC 控制台代理<br/>WebSocket → TCP / SSH"]
        Worker["异步任务 Worker<br/>领取、取消、心跳、超时"]
        Runner["命令执行器<br/>结构化 argv，不经过 shell"]
        Assets["内嵌模板 / 静态资源 / noVNC"]
    end

    PG[("PostgreSQL 16<br/>用户、主机、任务、会话、审计")]
    Key["master.key<br/>AES-256-GCM 主密钥"]

    subgraph Tools["面板主机命令执行层<br/>本机 / qemu+tcp / qemu+tls"]
        Virsh["virsh<br/>连接本机或远程 libvirt"]
        QEMU["qemu-img / qemu-io / qemu-nbd<br/>qemu-storage-daemon / qemu-pr-helper 等"]
        Virt["virt-install / virt-xml<br/>libguestfs / virt-v2v"]
    end

    SSHExec["加固 OpenSSH 执行器<br/>BatchMode + 严格 known_hosts<br/>argv 逐项 POSIX 转义"]

    subgraph Targets["多台 KVM 物理机（每台独立 Host 配置）"]
        Local["本机 libvirt<br/>qemu:///system"]
        RemoteA["KVM 物理机 A<br/>qemu+ssh / qemu+tcp / qemu+tls"]
        RemoteB["KVM 物理机 B<br/>qemu+ssh / qemu+tcp / qemu+tls"]
        VNC["各 VM 的 VNC 控制台"]
    end

    User -->|HTTP / HTTPS| Proxy
    Proxy -->|127.0.0.1:8080| Web
    Proxy <-->|WebSocket /console/...| Console
    User -.->|无 Nginx：IP:8080| Web
    User -.->|无 Nginx：WebSocket| Console

    Web -->|读取会话、审计、资源| PG
    Web -->|提交异步任务| PG
    Worker -->|FOR UPDATE SKIP LOCKED| PG
    Worker --> Runner
    Runner --> Virsh
    Runner --> QEMU
    Runner --> Virt
    Runner -->|qemu+ssh 目标| SSHExec

    Virsh --> Local
    Virsh -->|qemu+tcp/tls：仅 libvirt 感知工具| RemoteA
    Virsh -->|qemu+tcp/tls：仅 libvirt 感知工具| RemoteB
    SSHExec -->|远程 virsh / qemu-img / virt-*| RemoteA
    SSHExec -->|远程 virsh / qemu-img / virt-*| RemoteB
    Console -->|本机 TCP / SSH -W 隧道| VNC
    Local --- VNC
    RemoteA --- VNC
    RemoteB --- VNC

    Key -.->|加解密主机 URI / 任务载荷| Web
    Key -.->|加解密任务载荷| Worker
    Assets -.-> Web
    Assets -.-> Console
```

主要路径：

- **管理请求**：浏览器访问 Web/API，Go 进程直接读写 PostgreSQL 中的用户、主机、会话和审计数据。
- **命令执行**：Web 只负责校验并写入任务；Worker 通过 `FOR UPDATE SKIP LOCKED` 领取任务。本机/本地 libvirt 目标直接以 argv 启动；`qemu+ssh` 目标通过加固 OpenSSH 在对应物理机执行 `virsh`、QEMU 独立工具或 virt/libguestfs 工具。VM、存储和网络页面读取也使用同一执行链路，不会把 `qemu+ssh://...` 交给面板本机 `virsh`。
- **控制台**：浏览器与 Go 内置的 noVNC 代理建立同源 WebSocket；代理使用本机 TCP 或 `qemu+ssh -W` 隧道连接目标 VM 的 VNC 端口。
- **信任边界**：`master.key` 不存数据库；主机 URI 和任务载荷以 AES-256-GCM 加密并绑定用途。`qemu-system-*` 不由 Web 直接裸启动；生产 VM 生命周期经过 libvirt，`virt-qemu-run` 仅作为管理员排障/测试的独立临时 guest 例外。

## 部署

支持两种部署方式：

1. **本机直接部署**：Go 二进制由 systemd 管理，PostgreSQL 可以是本机服务或外部数据库。
2. **Docker Compose 部署**：应用和 PostgreSQL 都由 Compose 管理，适合快速上线和隔离运行环境。

两种方式都使用同一套环境变量。最小生产配置必须包含 `WVC_DATABASE_URL`、`WVC_MASTER_KEY_FILE`、`WVC_ADDR`、管理员初始化配置，以及是否经过反向代理的 `WVC_SECURE_COOKIES` / `WVC_TRUST_PROXY`。完整变量表见[配置](#配置)。

> 程序本身只监听 HTTP，不终止 TLS。需要 HTTPS 时必须使用 Nginx、Caddy、Traefik 或云负载均衡器。没有反向代理时可以直接使用 `http://面板IP:8080`，但密码、会话和 VNC 流量不加密，只适合可信内网或 VPN。

> noVNC 需要浏览器安全上下文。面板和浏览器在同一台机器时可使用 `http://localhost:8080`。可信内网明确接受明文传输时，可设置 `WVC_ALLOW_INSECURE_CONSOLE=true` 允许普通 HTTP 地址初始化 noVNC；登录凭据、会话和 VNC 流量不会被加密。

### 部署前准备

- 一台或多台运行 QEMU/KVM/libvirt 的 Linux 物理机。每台机器在“主机”页面单独配置；`qemu+ssh` 主机的 VM/存储/网络读取、独立 QEMU 工具和 virt/libguestfs 工具都会在该物理机执行，不会回退到面板本机。
- PostgreSQL 16。原生部署需要自行安装；Compose 方式由 `db` 服务提供。
- 面板机的依赖按实际启用的主机类型决定：管理本机 `qemu://`、`qemu+unix://` 或远程 `qemu+tcp/tls` 主机时需要本机 `virsh`；管理 `qemu+ssh` 主机时需要 `openssh-client`。`qemu-img`、`qemu-io`、`qemu-nbd`、virtinst、guestfs-tools 和 virt-v2v 属于可选命令能力，缺失只会让对应命令不可用，不会让整个 `/readyz` 失败。纯远程 `qemu+ssh` 部署的面板机不需要安装本机 `virsh` 或 QEMU 工具。若要让 `qemu+ssh` 目标执行完整命令中心，还需在各台 KVM 物理机的 `WVC_REMOTE_PATH` 中安装对应工具；页面会按目标主机 PATH 显示可用性。控制台和远程命令执行都使用 `WVC_SSH_BIN` 指定的 OpenSSH 客户端。
- 面板主机时间同步正常，且能访问目标 libvirt 主机。使用 `qemu+ssh://` 时，需要提前配置免密 SSH、`known_hosts` 和面板专用密钥。

可用 `openssl rand -hex 24` 生成 URL-safe 的数据库密码，用 `openssl rand -hex 16` 生成管理员初始密码。

Debian 13（trixie）或 Ubuntu 可参考下面的命令。Debian 12（bookworm）仓库没有 `virt-top`，请先升级到 trixie，或省略该包并接受命令中心将 `virt-top` 标记为“缺失”。项目 Docker 镜像固定使用 `debian:trixie-slim`，安装 Debian 能提供的常用 QEMU/libvirt/libguestfs 工具；镜像并不等于完整上游工具集，Debian 未打包的工具（例如 `qemu-vnc`、`virt-convert`、`virt-list-filesystems` 等）不会伪装成已安装。请以页面中的“缺失”状态和实际 `command -v` 结果为准，必要时用 `WVC_*_BIN` 指向自建二进制。

```bash
sudo apt update
sudo apt install -y ca-certificates postgresql postgresql-client \
  libvirt-daemon-system libvirt-clients \
  qemu-system-x86 qemu-utils openssh-client
# 可选：扩展 virt/libguestfs 命令能力
sudo apt install -y virtinst guestfs-tools libguestfs-tools virt-v2v virt-what virt-top
```

RHEL/Fedora 系使用对应的 `postgresql-server`、`libvirt-daemon-kvm`、`libvirt-client`、`qemu-kvm`、`qemu-img`、`openssh-clients`、`virt-install`、`guestfs-tools` 等包。以发行版实际包名为准。

### 方式一：本机直接部署

#### 自动化安装（Ubuntu）

仓库提供可重复执行的 Ubuntu 本机安装脚本。先构建二进制，再以 root 运行：

```bash
make build
sudo ./deploy/install-ubuntu-local.sh
```

脚本会安装 PostgreSQL、创建 `wvc` 运行用户、生成环境文件、注册 systemd 服务并执行就绪检查。可通过 `WVC_PANEL_ADDR` 修改监听地址；UFW 不会自动修改，除非显式设置 `WVC_UFW_ALLOW_CIDRS`。已有 `/etc/webvirtcloud-go/env` 会被保留。设置 `WVC_INSTALL_EXTENDED_TOOLS=1` 可同时安装发行版提供的 libguestfs、virt-v2v、virt-what、virt-top、virt-p2v 等完整工具集。

Debian/Ubuntu 默认的 `/var/lib/libvirt/images` 通常不允许 `wvc` 写入，因此离线 `qemu-img` 扩缩容会失败。确认要让面板直接管理本机镜像后，可运行：

```bash
sudo env WVC_GRANT_IMAGE_ACCESS=1 ./deploy/install-ubuntu-local.sh
```

安装器会通过 POSIX ACL 仅授予 `wvc` 对 `WVC_IMAGE_DIR` 的读写和执行权限，并把同一路径写入 `WVC_LOCAL_IMAGE_DIR` 供 `wvc doctor` 检查。若只管理远程 `qemu+ssh` 主机，不需要开启该选项。

#### 手工安装步骤

#### 1. 启动 libvirt 并验证连接

```bash
sudo systemctl enable --now libvirtd
sudo virsh -c qemu:///system list --all
```

如果发行版使用模块化 daemon，也可以用 `virtqemud.socket`；无论服务名是什么，最终必须保证 `/var/run/libvirt/libvirt-sock` 存在，并且面板运行用户可以访问。

#### 2. 创建运行用户、目录和权限

```bash
sudo useradd --system --create-home --home-dir /var/lib/webvirtcloud-go --shell /usr/sbin/nologin wvc
sudo usermod -aG libvirt wvc
sudo install -d -o wvc -g wvc -m 0750 /var/lib/webvirtcloud-go
sudo install -d -o wvc -g wvc -m 0700 /var/lib/webvirtcloud-go/data
sudo install -d -o wvc -g wvc -m 0700 /var/lib/webvirtcloud-go/.ssh
sudo install -d -o wvc -g wvc -m 0700 /var/lib/webvirtcloud-go/tmp
sudo install -d -o root -g wvc -m 0750 /etc/webvirtcloud-go
sudo -u wvc virsh -c qemu:///system list --all
```

`master.key` 会自动创建在 `WVC_MASTER_KEY_FILE` 所在目录。该目录必须由运行用户拥有，且不能对 group/other 可写。面板专用 SSH 私钥、`config` 和 `known_hosts` 放到 `/var/lib/webvirtcloud-go/.ssh`，权限保持 `0600`/`0700`。

#### 3. 创建 PostgreSQL 用户和数据库

原生 PostgreSQL 默认端口是 `5432`，不是 Compose 示例中映射到宿主机的 `55433`：

```bash
sudo systemctl enable --now postgresql
sudo -u postgres psql <<'SQL'
CREATE ROLE wvc LOGIN PASSWORD 'CHANGE_ME_URL_SAFE_PASSWORD';
CREATE DATABASE wvc OWNER wvc;
SQL
```

数据库密码放在 `WVC_DATABASE_URL` 时必须做 URL 编码；建议直接使用只含 `A-Z a-z 0-9 . _ ~ -` 的随机密码。示例：

```text
postgres://wvc:CHANGE_ME_URL_SAFE_PASSWORD@127.0.0.1:5432/wvc?sslmode=disable
```

如果 PostgreSQL 已配置 TLS，把 `sslmode` 改为 `verify-full`，并配置 CA。程序启动时会自动执行 schema migration，不需要手工建表。

#### 4. 构建并安装二进制

需要 Go 1.26.10+。也可以在其他机器交叉/静态构建后只拷贝二进制：

```bash
make build
sudo install -o root -g root -m 0755 bin/wvc /usr/local/bin/wvc
sudo install -o root -g root -m 0644 deploy/wvc.service /etc/systemd/system/webvirtcloud-go.service
sudo install -o root -g root -m 0644 deploy/60-webvirtcloud-go.rules /etc/polkit-1/rules.d/60-webvirtcloud-go.rules
sudo systemctl daemon-reload
sudo systemctl restart polkit
```

`deploy/wvc.service` 使用 `/etc/webvirtcloud-go/env`、运行用户 `wvc` 和 `libvirt` 组；如目录不同，先修改 unit 中的 `WorkingDirectory`、`EnvironmentFile` 和 `ReadWritePaths`。`deploy/60-webvirtcloud-go.rules` 只允许 `wvc` 读取 libvirt secure domain XML，这是带密码 VNC 控制台正常工作所必需的。

#### 5. 写环境变量文件

```bash
sudo install -o root -g wvc -m 0640 /dev/null /etc/webvirtcloud-go/env
sudo editor /etc/webvirtcloud-go/env
```

最小内容示例（直接通过 `IP:8080` 访问，不经过 Nginx）：

```bash
WVC_ADDR=0.0.0.0:8080
WVC_DATABASE_URL=postgres://wvc:CHANGE_ME_URL_SAFE_PASSWORD@127.0.0.1:5432/wvc?sslmode=disable
WVC_MASTER_KEY_FILE=/var/lib/webvirtcloud-go/data/master.key
WVC_ADMIN_USERNAME=admin
WVC_ADMIN_PASSWORD=CHANGE_ME_AT_LEAST_10_CHARS
WVC_WORKERS=2
WVC_SECURE_COOKIES=false
WVC_TRUST_PROXY=false
WVC_LOG_LEVEL=info
WVC_LOG_FORMAT=text
```

说明：

- `WVC_ADDR=0.0.0.0:8080` 监听所有 IPv4 地址；`127.0.0.1:8080` 只允许本机访问（适合前面有 Nginx）。
- `WVC_ADMIN_PASSWORD` 只在空数据库首次启动时使用，长度至少 10 个字符。留空则由程序生成一次性密码。首次管理员创建后应从服务环境删除该变量，后续改密请使用页面或 `reset-password`。
- 修改数据库密码后必须同步修改 `WVC_DATABASE_URL`。密码含 `@ : / ? # [ ] %` 等字符时必须百分号编码。
- 如果 `virsh`、`virt-*` 或 `qemu-*` 不在 systemd 默认 PATH，使用绝对路径设置 `WVC_VIRSH_BIN`、`WVC_QEMU_IMG_BIN` 等变量；完整清单见 `.env.example`。
- systemd 单元已将 `TMPDIR` 固定为 `/var/lib/webvirtcloud-go/tmp`；确认该目录有足够空间且属于 `wvc`。若在 `/etc/webvirtcloud-go/env` 中覆盖 `TMPDIR`，也要保证目录可写。仓库自带的单元使用 `ProtectSystem=strict`、`ProtectHome=true`、空 capability 集合、`RestrictAddressFamilies`、`RestrictNamespaces=~user`、`LimitCORE=0` 和 `TasksMax=1024`；可用 `systemd-analyze security webvirtcloud-go.service` 检查实际生效情况。
- 直接通过 HTTP 访问时不要设置 `WVC_TRUST_PROXY=true`；经过反向代理时必须设置，并把 `WVC_TRUSTED_PROXY_CIDRS` 收紧到实际代理地址。

#### 6. 启动并获取初始密码

```bash
sudo systemctl enable --now webvirtcloud-go
sudo systemctl status webvirtcloud-go
sudo journalctl -u webvirtcloud-go -f
# 另一个终端确认 HTTP 已监听：
curl -I http://127.0.0.1:8080/login
```

如果未设置 `WVC_ADMIN_PASSWORD`：

```bash
sudo ls -l /var/lib/webvirtcloud-go/data/initial-admin-password-*.txt
sudo cat /var/lib/webvirtcloud-go/data/initial-admin-password-*.txt
```

CLI 排障：

```bash
wvc version
wvc doctor
wvc healthcheck
```

管理员密码遗失时，可以生成新的随机密码并撤销全部现有会话：

```bash
sudo -u wvc bash -c 'set -a; . /etc/webvirtcloud-go/env; /usr/local/bin/wvc reset-password'
sudo -u wvc cat /var/lib/webvirtcloud-go/data/reset-password-*.txt
```

命令会以 `0600` 权限写入一次性密码文件。登录后立即修改密码并删除该文件。

登录后点击右上角用户名进入“修改密码”，立即更换管理员密码并删除一次性密码文件；修改后所有会话都会失效。然后在“主机”页面添加 libvirt 连接，本机通常使用 `qemu:///system`，远程使用 `qemu+ssh://用户@宿主机/system`。主机连接 URI 为必填项，空 URI 不会回退到面板进程的默认 libvirt 连接。

#### 7. 无 Nginx 时直接访问

确认 `WVC_ADDR=0.0.0.0:8080`，放行防火墙：

```bash
sudo ufw allow 8080/tcp
# firewalld:
sudo firewall-cmd --permanent --add-port=8080/tcp && sudo firewall-cmd --reload
```

浏览器访问 `http://面板服务器IP:8080`。浏览器只需要访问 Web 端口，控制台通过同一个 Web 端口和 WebSocket 转发；如果面板使用 `qemu+tcp`/`qemu+tls` 连接远程 VNC，面板到 KVM 宿主机之间仍需允许对应 VNC 端口，或改用 `qemu+ssh`。

这种方式没有 TLS：不要把 8080 直接暴露到公网；如必须公网访问，请使用 VPN 或增加 Nginx/负载均衡器终止 TLS。增加反向代理后必须同时设置 `WVC_SECURE_COOKIES=true`、`WVC_TRUST_PROXY=true`，并把 `WVC_TRUSTED_PROXY_CIDRS` 收紧到代理的实际来源地址。

#### 8. 有 Nginx 时通过域名/HTTPS 访问

应用改为只监听本机：

```bash
WVC_ADDR=127.0.0.1:8080
WVC_SECURE_COOKIES=true
WVC_TRUST_PROXY=true
WVC_TRUSTED_PROXY_CIDRS=127.0.0.1/32,::1/128
```

修改环境变量后执行 `sudo systemctl restart webvirtcloud-go`。

安装并修改示例配置中的域名、证书路径：

```bash
sudo cp deploy/nginx.conf.example /etc/nginx/sites-available/webvirtcloud-go
sudo ln -s /etc/nginx/sites-available/webvirtcloud-go /etc/nginx/sites-enabled/webvirtcloud-go
sudo editor /etc/nginx/sites-available/webvirtcloud-go
sudo nginx -t
sudo systemctl reload nginx
```

示例包含 HTTPS 跳转、安全响应头、登录限流、`/metrics` 限制和 VNC WebSocket 升级。访问 `https://wvc.example.com`。Nginx 日志格式使用 `$uri` 而不是 `$request`，避免控制台短时 token 进入 access log。

示例直接引用 Let's Encrypt 证书路径；执行 `nginx -t` 前必须先签发证书，或把 `ssl_certificate`/`ssl_certificate_key` 改成已有证书。证书可以由 `certbot`、企业 CA 或云负载均衡器签发。

### 方式二：Docker Compose 部署

#### 1. 安装 Docker 和准备 libvirt

需要 Linux 主机上的 Docker Engine 24+ 和 Compose v2；Docker Desktop 无法直接使用宿主机 libvirt socket，不适用于本部署方式。

```bash
docker version
docker compose version
sudo systemctl enable --now libvirtd
if ! sudo test -S /var/run/libvirt/libvirt-sock; then
  echo "错误：/var/run/libvirt/libvirt-sock 不存在，拒绝启动挂载该 socket 的 Compose" >&2
  exit 1
fi
echo libvirt-socket-ok
```

Compose 把宿主机 libvirt socket **以读写方式**挂载进容器；libvirt 客户端需要通过该 socket 双向通信，不能加 `:ro`。容器仍受宿主机 socket 权限和组 ID 约束。`LIBVIRT_GID` 必须等于宿主机 libvirt socket 的组 ID：

> 不要先启动 Compose 再补 socket。Compose 对该 bind mount 使用 `create_host_path: false`；宿主机路径不存在时会直接拒绝启动，不会悄悄创建同名目录。若面板只管理远程 `qemu+ssh://` 主机，可以删除 Compose 中的 libvirt socket bind mount 和 `group_add`，但仍需保留 SSH 私钥及 `known_hosts` 配置。

```bash
getent group libvirt
stat -c '%g' /var/run/libvirt/libvirt-sock
```

把结果写入 `.env` 的 `LIBVIRT_GID`。

#### 2. 创建 .env

```bash
install -d -m 0700 deploy/ssh
cp .env.example .env
chmod 600 .env
editor .env
```

最小必改项：

| 变量 | 说明 |
|---|---|
| `POSTGRES_PASSWORD` | PostgreSQL 密码；Compose 会用它生成 `WVC_DATABASE_URL`。建议使用 URL-safe 强密码 |
| `WVC_ADMIN_PASSWORD` | 首次启动管理员密码，至少 10 位；留空则生成一次性密码文件 |
| `LIBVIRT_GID` | 宿主机 `/var/run/libvirt/libvirt-sock` 的组 ID |
| `WVC_DATABASE_URL` | 通常留空，由 `POSTGRES_*` 自动生成；只有密码包含 URL 保留字符时才显式设置并编码 |
| `WVC_MASTER_KEY_FILE` | Compose 已固定为 `/app/data/master.key`，持久化在 `wvc-data` 卷，必须与数据库一起备份 |
| `WVC_SECURE_COOKIES` | 直接 HTTP 访问时为 `false`；HTTPS 反向代理后为 `true` |
| `WVC_TRUST_PROXY` | 无反向代理时为 `false`；经过 Nginx 时为 `true` |
| `WVC_SSH_DIR` | 面板专用 SSH 材料目录，默认 `./deploy/ssh` |
| `GOPROXY` | 构建镜像时访问 Go 模块代理；国内网络可改为 `https://goproxy.cn,direct` |
| `APT_MIRROR` / `APT_SECURITY_MIRROR` | 可选 Debian 软件包镜像；Debian 官方源不可达时可设置，例如清华镜像 |

可选配置：

- `WVC_WORKERS=2`：任务并发数，按 CPU 和磁盘 I/O 调整。
- `WVC_LOG_LEVEL`、`WVC_LOG_FORMAT`：日志级别和格式。
- `WVC_METRICS_TOKEN`：启用受 Bearer token 保护的 `/metrics`。
- `WVC_IMAGE_DIR`：需要让容器内 QEMU 工具访问宿主机镜像时设置，并取消 Compose 中对应 volume 注释；容器以 uid `10001` 运行，挂载目录必须可写。
- `WVC_APP_MEM_LIMIT`、`WVC_APP_CPUS`：应用容器资源上限，默认 `4g` 和 `2.0`。运行大磁盘 `virt-v2v`/libguestfs 转换前应按数据量调高内存。
- `WVC_TMP_DIR`：默认不需要设置，Compose 已把命名卷 `wvc-tmp` 挂到 `/var/tmp`。若改用独立宿主机目录，删除 Compose 中的 `wvc-tmp:/var/tmp`，取消 `${WVC_TMP_DIR:-./data/tmp}:/var/tmp` 注释，并确保目录可由 uid `10001` 写入。
- `WVC_HEALTHCHECK_URL`：容器内健康检查地址，默认 `http://127.0.0.1:8080/readyz`；只有修改 `WVC_ADDR` 或监听端口时才需调整。
- `WVC_PUBLISH_ADDR`：Compose 映射到宿主机的地址，默认 `127.0.0.1`。有 Nginx 时保持默认；无反向代理直接访问时才改为 `0.0.0.0` 或具体网卡 IP。
- KVM 加速：取消 `/dev/kvm:/dev/kvm` 注释，并把宿主机 `kvm` 组 GID 加入 `group_add`。具体步骤见下节。

不要同时保留旧的 `WVC_DATABASE_URL`：Compose 会优先使用显式值；只有密码含 URL 保留字符时才建议手工设置并做百分号编码。

SSH 目录只放面板专用私钥、`config` 和 `known_hosts`，不要挂载整个宿主机的 `~/.ssh`。`qemu+ssh` 强制 `BatchMode=yes` 和 `StrictHostKeyChecking=yes`，主机密钥必须预先写入 `known_hosts`。

容器固定以 uid/gid `10001:10001` 运行，而绑定挂载不会自动改变宿主文件所有权。若 `deploy/ssh` 由宿主普通用户创建，容器内的 `wvc` 用户会因目录或私钥不可读而无法建立 `qemu+ssh` 连接。按实际文件名修正：

```bash
sudo chown -R 10001:10001 deploy/ssh
sudo chmod 700 deploy/ssh
sudo find deploy/ssh -maxdepth 1 -type f \( -name 'id_*' -o -name config \) -exec chmod 600 {} +
sudo find deploy/ssh -maxdepth 1 -type f -name known_hosts -exec chmod 644 {} +
```

不要把私钥设置为 `644`，也不要把整个宿主 `~/.ssh` 目录挂进容器。验证时可在容器内检查：

```bash
docker compose exec app sh -c 'id && ls -la /home/wvc/.ssh'
```

若使用 SSH `config`，可再用 `ssh -G <Host别名>` 检查实际生效的用户、私钥和主机密钥策略。启用 `/dev/kvm` 时，除取消 `devices` 中的注释外，还必须把宿主机 `kvm` 组 GID 加入 `group_add`：

```bash
getent group kvm
stat -c '%g' /dev/kvm
```

在 `docker-compose.yml` 中追加实际 GID，例如：

```yaml
    group_add:
      - "${LIBVIRT_GID:-992}"
      - "108"
```

不要为了省事加入 `0` 或宿主机 `root` 组。修改后执行 `docker compose up -d --force-recreate app`，并用 `docker compose exec app sh -c 'id; test -r /dev/kvm && test -w /dev/kvm'` 验证。`guestmount` 会 fork 并持续等待卸载，不符合有限异步任务模型，命令中心会禁用它；不要为了它放开 `/dev/fuse` 和 `CAP_SYS_ADMIN`，请改用 `guestfish`、`virt-copy-*` 等非挂载方式。

#### 3. 启动 Compose

```bash
docker compose config
docker compose up -d --build
docker compose ps
docker compose logs -f app
# 等待 app 显示 healthy，并验证就绪端点：
curl -fsS http://127.0.0.1:8080/readyz
```

Compose 会启动 PostgreSQL 和应用；数据库 schema 在应用启动时自动迁移。数据卷为 `wvc-db`（数据库）、`wvc-data`（`master.key`、初始化密码等）和 `wvc-tmp`（libguestfs/virt-v2v 临时数据）。镜像自带 `/readyz` 健康检查，`docker compose ps` 中 app 应从 `starting` 变为 `healthy`；首次启动执行迁移时可能需要几分钟。首次启动后获取管理员密码：

```bash
docker compose exec app sh -c 'cat /app/data/initial-admin-password-*.txt'
```

如果已设置 `WVC_ADMIN_PASSWORD`，直接使用该密码登录。

Compose 通过 `WVC_PUBLISH_ADDR` 控制 Web 的宿主机绑定地址，默认 `127.0.0.1:8080`；数据库固定映射到 `127.0.0.1:55433`。因此默认只能从宿主机本机访问；远程访问要么加 Nginx，要么按下节把 `WVC_PUBLISH_ADDR` 改为 `0.0.0.0`。

#### 4. 无 Nginx 时直接访问

在 `.env` 中把宿主机发布地址改为所有网卡（或替换成具体面板 IP）：

```bash
WVC_PUBLISH_ADDR=0.0.0.0
WVC_ADDR=:8080
WVC_SECURE_COOKIES=false
WVC_TRUST_PROXY=false
```

`docker-compose.yml` 使用 `"${WVC_PUBLISH_ADDR:-127.0.0.1}:8080:8080"`，不需要手工修改 YAML。改完 `.env` 后执行 `docker compose up -d --force-recreate app` 让端口映射生效。

然后重建并放行防火墙：

```bash
docker compose up -d
sudo ufw allow 8080/tcp
# firewalld:
sudo firewall-cmd --permanent --add-port=8080/tcp && sudo firewall-cmd --reload
```

访问 `http://面板服务器IP:8080`。数据库的 `55433` 端口默认只绑定 `127.0.0.1`，不要为了面板访问而把它暴露到公网。直接 HTTP 模式同样只适合可信内网/VPN；如果使用 HTTPS，必须让 Nginx 终止 TLS 并设置 `WVC_SECURE_COOKIES=true`、`WVC_TRUST_PROXY=true`。

#### 5. 有 Nginx 时

保持 `.env` 中 Compose 默认的 `WVC_PUBLISH_ADDR=127.0.0.1`，在宿主机安装 Nginx，复制 `deploy/nginx.conf.example`，修改 `server_name` 和证书路径：

```bash
sudo cp deploy/nginx.conf.example /etc/nginx/sites-available/webvirtcloud-go
sudo ln -s /etc/nginx/sites-available/webvirtcloud-go /etc/nginx/sites-enabled/webvirtcloud-go
sudo editor /etc/nginx/sites-available/webvirtcloud-go
sudo nginx -t && sudo systemctl reload nginx
```

`.env` 设置：

```bash
WVC_SECURE_COOKIES=true
WVC_TRUST_PROXY=true
# Nginx 在宿主机、应用在容器内时，容器看到的对端通常是 Docker 网关，
# 不是 Compose 服务子网。查看实际网关：
docker inspect -f '{{range .NetworkSettings.Networks}}{{.Gateway}}{{end}}' "$(docker compose ps -q app)"
# 假设输出 172.18.0.1，则只信任该地址：
WVC_TRUSTED_PROXY_CIDRS=172.18.0.1/32
```

如果 Nginx 也运行在该 Compose 网络中，则改为信任 Compose 的实际子网，可通过 `docker network inspect` 查看。不要照搬示例网段。先确保证书已签发，再执行 `docker compose up -d --force-recreate app` 使环境变量生效，并刷新 Nginx。访问 `https://wvc.example.com`。Nginx 示例已经处理 `/console/.../ws` 的 WebSocket 升级、长连接超时和 `$uri` 安全日志格式。

Compose 默认设置 `WVC_CONSOLE_LOOPBACK_ADDRESS=host.docker.internal` 并添加 `host.docker.internal:host-gateway`。当 libvirt XML 中的 VNC 监听 `0.0.0.0` 时，容器可通过宿主机网关访问；但仅绑定在宿主机 `127.0.0.1`：端口上的 VNC **不能**可靠地通过 Docker 网关访问，单靠这个变量无法穿透宿主回环监听。遇到这种情况，应把 VNC 改为监听宿主机受防火墙保护的私网地址，或使用 `qemu+ssh` 控制台通道；远程 `qemu+ssh` 和非回环的私有 VNC 地址不会被改写。原生部署必须把该变量留空。

### 本地开发（可选）

本地直接运行需要 Go 1.26.10+、PostgreSQL 16 和上述 libvirt/QEMU 软件：

```bash
export PATH=/path/to/go/bin:$PATH
export WVC_DATABASE_URL='postgres://wvc:CHANGE_ME_URL_SAFE_PASSWORD@127.0.0.1:55433/wvc?sslmode=disable'
export WVC_ADMIN_PASSWORD='change-this-password'
make build
./bin/wvc
```

集成测试需要独立测试库（测试会生成唯一 schema、用户和主机，不会清空已有数据）。下面默认使用 Compose 映射到宿主机的 `55433`；如果使用本机 PostgreSQL，把端口改为 `5432`：

```bash
createdb -h 127.0.0.1 -p 55433 -U wvc wvc_test
make test-integration
```

## 功能范围

### Web 页面

- 概览、主机、虚拟机列表与详情、存储池/卷、虚拟网络、VNC 控制台。
- 宿主机 KVM/cgroup/设备健康检查，以及虚拟机块设备错误、guest 内存统计和近期事件诊断。
- 主机运维工作台、节点和虚拟机资源视图、批量生命周期/自启动操作，以及 vCPU/内存持久化与在线调优。
- 虚拟机磁盘在线扩容和关机态扩容/缩容，包含分区与文件系统前置风险提示。
- 存储卷创建、克隆、扩容、缩容和删除，以及存储池刷新、启动和自启动管理。
- 虚拟网络启动、停止和自启动管理。
- 异步任务列表、详情、自动刷新输出、取消任务。
- 用户创建、角色调整、禁用、密码重置。
- 审计日志（登录、主机、用户、命令入队、任务结果）。
- 管理员命令中心与完整命令目录。

### 多台 KVM 物理机纳管

本项目不是单机面板。每台 KVM 物理机在“主机”页面独立登记为一条 Host 记录，拥有自己的加密 libvirt URI、名称、描述和启用状态；同一个面板可以同时纳管多台物理机。典型配置：

```text
qemu+ssh://root@10.0.0.11/system
qemu+ssh://root@10.0.0.12/system
qemu+ssh://root@10.0.0.13/system
```

多主机行为由服务端和 PostgreSQL 共同保证：

- 任务保存明确的 `host_id`；Worker 只解密并连接该任务所属物理机的 URI。URI 密文以 `host:<hostID>` 为 AAD，不能把 A 主机的密文调换给 B 主机使用。
- 同一物理机上的写任务通过 `resource_key=host:<hostID>` 和数据库 partial unique index 串行执行；不同物理机的写任务可以并行，只读任务也可并行。该约束跨多个面板副本和多个 Worker 生效。
- 虚拟机列表按物理机分组展示；控制台令牌绑定“物理机 + 虚拟机 + 当前用户”，`qemu+ssh` 控制台通过对应物理机的 SSH 隧道连接。
- 主机存在排队或运行中任务时，禁止更换其连接 URI；主机被删除前也必须等待活动任务结束，避免任务在不知情的情况下切到另一台宿主机。

使用 `qemu+ssh` 时，面板主机或 app 容器需要准备专用 SSH 私钥、`config` 和 `known_hosts`，并确保对每台物理机都能以非交互方式登录。Docker 部署的目录和权限见 `deploy/ssh/README.md`。

选择 `qemu+ssh://` 主机时，`virsh`、`qemu-img`、`qemu-io`、`qemu-nbd`、virt/libguestfs 等命令统一通过该 URI 对应的 OpenSSH 连接在目标物理机执行；远程工具按 `WVC_REMOTE_PATH` 查找，磁盘路径也是目标物理机路径。libvirt 工具在目标机使用 `qemu:///system` 或 `qemu:///session`，不会嵌套 `qemu+ssh`。`qemu+tcp://`/`qemu+tls://` 没有 SSH 端点，只允许接受 `--connect` 的 libvirt 工具；独立 QEMU 工具会被明确拒绝，绝不会静默回退到面板本机。完整边界见 `docs/COMMANDS.md`。

### QEMU/KVM 命令覆盖与边界

高级命令中心提供结构化命令目录和参数构建器，同时保留高级原始 argv 入口。目录按 libvirt、virtinst、libguestfs 与 QEMU 官方文档生成，并在运行时按执行目标探测工具可用性：本机目标读取版本和命令支持，`qemu+ssh` 目标通过一次 SSH 检查目标 PATH，避免面板目录与目标主机工具版本脱节。当前共 530 个结构化入口：

- `virsh`：官方 manpage 的 284 个命令入口，覆盖生命周期、XML define/edit、快照、检查点、备份、迁移、CPU/内存/NUMA/IO 调优、磁盘/网卡热插拔、存储池/卷、虚拟网络/端口、接口、nwfilter、secret、QMP、guest agent、宿主机能力和节点设备等。
- `qemu-img`：15 个官方子命令以及 `--help`/`--version`，共 17 个入口；包含全局 `-T` trace、多输入 `convert`、`dd` 的 `bs=/count=/skip=/if=/of=` 和 `resize` 的 `+size/-size` 形式。
- `qemu-io`：26 个 I/O 命令以及工具级 `--help`/`--version`，共 28 个入口；多长度 `readv/aio_read/writev` 和 `sleep <milliseconds>` 均有专用字段，文件级选项会正确放在 `-c` 之前。
- `qemu-nbd`：导出、列举、连接/断开、帮助/版本入口，覆盖共享、bitmap、TLS、offset、cache/AIO/discard、handshake 等帮助页选项；`connect` 使用块设备选项，`disconnect` 严格只接收设备路径。
- `qemu-storage-daemon`、`qemu-pr-helper`、`qemu-vmsr-helper`、`qemu-vnc`：官方帮助页上的守护进程/服务选项，包括 blockdev、chardev、export、NBD server、QMP monitor、PID、TLS、SASL、VNC 和降权参数。`qemu-vmsr-helper` 依赖宿主机的 Intel RAPL/powercap 驱动，没有 `/sys/class/powercap/intel-rapl/enabled` 时会直接启动失败。`qemu-vnc` 不由 Debian/Ubuntu 的常用 QEMU 软件包默认提供；它属于可选的独立上游工具，缺失时目录仍会显示但标记为“缺失”且不能执行。需要时请从与主机 QEMU 版本匹配的上游源码构建，或用 `WVC_QEMU_VNC_BIN` 指向可用二进制。
- `qemu-trace-stap`：`list` 和 `run` 两个 SystemTap 跟踪入口，支持探测模式、PID 和 QEMU 二进制路径。
- `virt-install`、`virt-clone`、`virt-convert`、`virt-xml`：创建、克隆、转换和离线编辑 libvirt domain；安装介质、磁盘、网络、图形、设备直通、cloud-init、调优、XML 动作等均有可点选字段。
- `virt-admin` 的 13 个子命令、`virt-host-validate`、`virt-qemu-run`、`virt-xml-validate`：服务端线程池/客户端/TLS/日志管理，宿主机 KVM 前置条件检查，guest XML 独立启动与 RNG 校验。
- `virt-pki-validate`、`virt-sanlock-cleanup`、`guestfish`、`guestunmount`、`libguestfs-test-tool`、`libguestfs-make-fixed-appliance`：TLS 证书校验、sanlock 锁清理、非交互 guestfish、FUSE 挂载清理，以及 libguestfs 自检和固定 appliance 构建。`guestmount` 默认 fork 并持续等待卸载，页面会保留说明但禁止异步提交；请改用 `guestfish`/`virt-copy-*`。
- `virt-builder`、`virt-customize`、`virt-sysprep`、`virt-format`、`virt-resize`、`virt-sparsify`、`virt-diff`、`virt-cat`、`virt-ls`、`virt-copy-in/out`、`virt-filesystems`、`virt-inspector` 等 libguestfs 工具：模板构建、离线定制、身份清理、磁盘格式化和扩容、差异比较、文件访问、检查与导出。
- `virt-v2v`、`virt-v2v-in-place`、`virt-v2v-inspector`、`virt-v2v-open`、`virt-p2v`、`virt-p2v-make-disk`、`virt-p2v-make-kickstart`：复制迁移、原地迁移、迁移前检查、在 virt-v2v 输入上运行工具，以及物理机迁移客户端/启动介质生成；`virt-p2v` 是物理机 ISO 图形客户端，页面中明确禁用异步提交。
- `virt-what` 与批处理模式的 `virt-top`：宿主机虚拟化识别和 CSV/流式资源采样。`virt-top` 必须同时提供 `-b`（或 `--script`）和正数 `-n`/`--iterations`，防止任务无限运行。

页面会为每条命令生成全局选项、位置参数、布尔开关、数值/文本选项、枚举选项和命令预览；点击即可提交，无需记忆命令语法。互斥分组和版本差异仍可通过“高级原始参数”补充。qemu-io 的 `break/remove_break/resume/wait_break` 等状态相关命令在页面中提供“前置命令”和“后续命令”字段，每一行生成一个独立的 `-c`，并按“前置命令 → 当前命令 → 后续命令 → 镜像路径”的顺序在同一 qemu-io 进程内执行，不需要切到高级模式。

覆盖范围是 libvirt CLI、QEMU 独立工具（`qemu-img`、`qemu-io`、`qemu-nbd`、`qemu-storage-daemon`、`qemu-pr-helper`、`qemu-vmsr-helper`、`qemu-vnc`、`qemu-trace-stap`）以及 virt/libguestfs。`qemu-system-*` 不由面板作为异步任务裸启动，生产 VM 生命周期应统一交给 libvirt，以免绕过其 cgroup、安全标签、锁和生命周期管理。`virt-qemu-run` 是唯一例外：它通过 libvirt 库启动绑定到该进程的独立临时 guest，不会进入常驻 libvirtd 的生命周期管理；因此它仅保留给管理员排障/测试，任务超时会终止整个进程组，日常创建和运行 VM 请使用 `virt-install` 与 `virsh`。交互式命令（`console`、`edit`、事件流、`connect`）、需要 TTY 的命令以及会脱离任务进程组的 `--daemonize`/`--daemon`/`--fork`/`--listen` 模式不会展示为可提交选项；对应参数字段会直接禁用并显示原因，服务端仍会再次拒绝原始 argv 中的同类模式。

`qemu-io -c` 的命令串按空白切分且不解析 shell 引号；页面不会给命令串添加 shell 引号，并会拒绝包含空白的命令级字段。镜像文件本身作为独立 argv，仍可包含空格。

参数通过独立的 shell 风格解析器转成 argv；本机目标**不会启动 shell**，`qemu+ssh` 目标由服务端逐项 POSIX 转义后交给远端 `sh -c` 执行，`|`、`$(...)`、重定向等仍是普通参数。`virsh`、virtinst 与 libguestfs 的连接 URI 强制从所选主机配置注入；`--connect`、对应工具的 `-c`、长选项缩写以及 `connect` 内建子命令都会被拒绝。`virt-install` 的 `-c` 是 `--cdrom`，不会被误判为连接覆盖。服务端仍会执行连接覆盖检查、只读分类、敏感命令判定、参数/输入/输出大小限制和进程组超时。

执行位置完全由当前主机配置决定：`qemu:///`/`qemu+unix://` 目标是面板本机，`qemu+ssh://` 目标是 SSH 登录的 KVM 物理机，`qemu+tcp://`/`qemu+tls://` 目标只支持 libvirt 感知工具。Docker Compose 部署时，面板容器仍需要基础 QEMU/virsh 工具用于本机管理；对远程 `qemu+ssh` 主机执行时，命令、磁盘路径和 `/dev/kvm` 等资源都属于目标物理机，不会落到 app 容器。`virt-format`、`virt-resize`、`virt-sparsify` 等不接受 `--connect` 的工具在 `qemu+ssh` 主机上通过 SSH 远程执行；在 `qemu+tcp/tls` 主机上则被拒绝。详细说明见 `docs/COMMANDS.md`。

直连 `qemu-system-*` 启动虚拟机不在支持范围内。由 libvirt 管理的 VM 应始终通过 `virsh` 操作，否则会绕过 libvirt 状态、锁、迁移和控制台管理。`virt-qemu-run` 只用于独立临时 guest 的排障/测试，不能替代 libvirtd 下的生产生命周期管理。

### libvirt URI

支持 `qemu:///system`、`qemu+ssh://`、`qemu+unix://`、`qemu+tcp://`、`qemu+tls://`。不支持的 scheme、URL 密码、片段和未知查询参数会被拒绝。

Web 控制台默认只允许本机 libvirt、`qemu+ssh`，或显式开启有风险的 `WVC_ALLOW_INSECURE_VNC=true`。`qemu+tcp` / `qemu+tls` 的 libvirt 管理连接可以加密，但 VNC 数据流本身没有传输加密，因此默认拒绝远程明文控制台。

## 安全模型

- `viewer`：只读页面；不能执行命令或打开控制台。
- `operator`：VM 常用生命周期、快照、连接测试；破坏性操作需要显式确认；只能看到自己的任务。
- `admin`：主机和用户管理、任意命令、XML/QMP/secret、本机 QEMU 工具。
- 所有状态变更 POST 都校验会话 CSRF；登录失败按 IP+用户名和独立 IP 阻断，用户名维度也会计数；全局失败计数仅用于观测，不硬阻断全站登录，避免密码喷洒把正常管理员一起锁在门外。
- 主机 URI 以 `host:<hostID>` 作为 AAD；任务参数、stdin、输出和错误以 `job:<jobID>:<field>` 作为 AAD。启动时会校验并把旧 `v1:` 密文迁移到绑定用途的 `v2:` 格式；普通读取路径拒绝旧格式。
- 任务执行前会重新读取发起人的当前角色和启用状态，并校验任务类型/工具组合及具体 `virsh` 动作的最低角色；排队期间被降权或禁用的用户不会继续执行敏感任务或管理员专属动作。
- 主机存在排队或运行中的任务时禁止修改其连接 URI，避免旧任务在执行时被静默重定向到另一台宿主机；名称、说明和启用状态仍可正常修改。
- 审计日志使用 HMAC-SHA256 哈希链；保留期清理会写 checkpoint，启动时验证链完整性，并拒绝在 HMAC 链后追加旧 SHA-256 行。主链写入暂时失败时，事件先进入 durable outbox，由各副本用租约式原子领取并退避重试；`source_id` 保证重复领取不会产生重复审计记录。
- 数据库迁移记录 checksum；二进制拒绝未知的更高版本 schema 和已应用迁移内容变化。
- `/metrics` 默认关闭；设置 `WVC_METRICS_TOKEN` 后只接受 `Authorization: Bearer <token>`。
- 控制台使用短时 HMAC 令牌，并同时绑定登录用户、主机和 VM；每个 WebSocket/密码令牌只能消费一次，使用 PostgreSQL 原子去重，多副本同样生效。WebSocket 强制同源，只有来自可信代理 CIDR 的对端才可影响 `X-Forwarded-Proto`。
- `qemu+ssh` 使用 `BatchMode=yes`、`StrictHostKeyChecking=yes`、`ClearAllForwardings=yes`、RFC/OpenSSH 连接超时与 keepalive；远程 argv 逐项 POSIX 转义，远端 shell 退出时通过 trap 终止子进程。需要预先配置 `known_hosts` 和免密密钥。
- 高级命令中心等价于宿主机管理员权限。只授予可信管理员，不要暴露到公网无防护环境。

## 配置

| 环境变量 | 默认值 | 说明 |
|---|---|---|
| `WVC_ADDR` | `:8080` | 监听地址 |
| `WVC_DATABASE_URL` | 必填 | PostgreSQL DSN；原生部署通常写 `5432`，Compose 未显式设置时由 `POSTGRES_*` 自动生成。程序不会使用内置弱口令连接数据库 |
| `WVC_MASTER_KEY_FILE` | `data/master.key` | AES-256-GCM 主密钥文件；必须独立备份且仅进程用户可读 |
| `WVC_LOCAL_IMAGE_DIR` | `/var/lib/libvirt/images` | `wvc doctor` 检查的本机镜像目录；必须与原生安装的 `WVC_IMAGE_DIR` 指向同一位置 |
| `WVC_ADMIN_USERNAME` | `admin` | 空库首次启动创建的管理员名 |
| `WVC_ADMIN_PASSWORD` | 空 | 留空则生成一次性密码并写入 `initial-admin-password-*.txt`，权限 `0600` |
| `WVC_WORKERS` | `2` | worker 数，1–32 |
| `WVC_MAX_ACTIVE_JOBS_PER_USER` | `20` | 每个用户允许的排队+运行任务上限，1–1000 |
| `WVC_MAX_ACTIVE_JOBS` | `200` | 全系统允许的排队+运行任务上限，必须不小于单用户上限且不超过 10000 |
| `WVC_SESSION_TTL` | `12h` | 会话有效期 |
| `WVC_MAX_SESSIONS_PER_USER` | `20` | 每个用户保留的有效会话上限，1–1000；新登录会淘汰最久未使用的会话 |
| `WVC_COMMAND_TIMEOUT` | `15m` | 默认命令超时 |
| `WVC_MAX_COMMAND_TIMEOUT` | `1h` | 单任务最大超时 |
| `WVC_MAX_OUTPUT_BYTES` | `1048576` | stdout/stderr 各自上限 |
| `WVC_MAX_ARGS` | `128` | 单任务最大参数数 |
| `WVC_MAX_STDIN_BYTES` | `1048576` | stdin 上限 |
| `WVC_CONSOLE_TOKEN_TTL` | `1m` | 控制台令牌有效期 |
| `WVC_MAX_CONSOLE_SESSIONS` | `64` | 单进程控制台 WebSocket 会话上限，1–1024 |
| `WVC_SECURE_COOKIES` | `false` | HTTPS 环境必须设为 `true` |
| `WVC_TRUST_PROXY` | `false` | 信任反向代理的 `X-Forwarded-*` |
| `WVC_TRUSTED_PROXY_CIDRS` | `127.0.0.0/8,::1/128` | 允许提供转发头的代理地址/CIDR |
| `WVC_LOGIN_WINDOW` | `15m` | 登录失败统计窗口 |
| `WVC_LOGIN_MAX_FAILURES` | `5` | IP+用户名窗口失败上限 |
| `WVC_LOGIN_IP_MAX_FAILURES` | `25` | 单一来源 IP 跨用户名窗口失败上限 |
| `WVC_LOGIN_GLOBAL_MAX_FAILURES` | `200` | 全局登录失败观测阈值，应高于 IP 上限；不硬阻断全站登录 |
| `WVC_ARGON2_CONCURRENCY` | `2` | 并发 Argon2 计算上限 |
| `WVC_LOG_LEVEL` | `info` | `debug/info/warn/error` |
| `WVC_LOG_FORMAT` | `text` | `text/json` |
| `WVC_JOB_RETENTION` | `720h` | 已完成任务保留期；`0` 禁用清理 |
| `WVC_AUDIT_RETENTION` | `4320h` | 审计保留期；`0` 禁用清理 |
| `WVC_AUDIT_VERIFY_TIMEOUT` | `30m` | 启动时完整校验审计哈希链的超时，1m–24h；历史量大时可调高 |
| `WVC_METRICS_TOKEN` | 空 | 空表示禁用 `/metrics`；设置后为 Bearer token，至少 32 字节 |
| `WVC_CONSOLE_LOOPBACK_ADDRESS` | 空 | 容器部署时改写 VNC 目标；Compose 默认 `host.docker.internal`，仅对可经宿主机网关访问的监听有效，不能穿透宿主 `127.0.0.1` 监听 |
| `WVC_VIRSH_BIN` | `virsh` | virsh 路径 |
| `WVC_QEMU_IMG_BIN` | `qemu-img` | qemu-img 路径 |
| `WVC_QEMU_IO_BIN` | `qemu-io` | qemu-io 路径 |
| `WVC_QEMU_NBD_BIN` | `qemu-nbd` | qemu-nbd 路径 |
| `WVC_QEMU_STORAGE_DAEMON_BIN` | `qemu-storage-daemon` | QEMU 存储守护进程路径 |
| `WVC_QEMU_PR_HELPER_BIN` | `qemu-pr-helper` | SCSI 持久保留 helper 路径 |
| `WVC_QEMU_TRACE_STAP_BIN` | `qemu-trace-stap` | SystemTap 跟踪工具路径 |
| `WVC_QEMU_VMSR_HELPER_BIN` | `qemu-vmsr-helper` | 虚拟 RAPL MSR helper 路径 |
| `WVC_QEMU_VNC_BIN` | `qemu-vnc` | 可选独立 QEMU VNC 服务路径；多数发行版不提供该二进制，缺失时命令不可执行 |
| `WVC_SSH_BIN` | `ssh` | OpenSSH 客户端路径；`qemu+ssh` 控制台和远程命令执行共用 |
| `WVC_REMOTE_PATH` | `/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin` | `qemu+ssh` 目标执行工具时使用的固定 PATH；远程工具按规范命令名查找 |
| `WVC_VIRT_*_BIN` / `WVC_GUEST*_BIN` / `WVC_LIBGUESTFS_*_BIN` | 工具同名 | 54 个 virt/libguestfs 可执行文件路径；完整变量见 `.env.example` |
| `WVC_ALLOW_INSECURE_VNC` | `false` | 允许远程 `qemu+tcp/tls` 的明文 VNC；仅用于受保护网络 |
| `WVC_ALLOW_INSECURE_CONSOLE` | `false` | 允许浏览器通过普通 HTTP 页面初始化 noVNC；仅用于可信内网或 VPN |

## 生产加固与运维

### 网络与访问控制

- 公网部署优先使用 TLS 反向代理，并设置 `WVC_ADDR=127.0.0.1:8080`、`WVC_PUBLISH_ADDR=127.0.0.1`、`WVC_SECURE_COOKIES=true`、`WVC_TRUST_PROXY=true`；`WVC_TRUSTED_PROXY_CIDRS` 只包含实际代理地址，不能使用 `0.0.0.0/0`。
- 无反向代理时，`WVC_ADDR=0.0.0.0:8080` 或 Compose 的 `WVC_PUBLISH_ADDR=0.0.0.0` 只能用于可信内网、VPN 或跳板机环境。HTTP 模式下密码、会话和控制台数据都没有传输加密。
- 只放行实际需要的端口。PostgreSQL `5432`（Compose 宿主映射为 `55433`）、libvirt socket 和 QEMU VNC 端口不应暴露到公网。
- 把 libvirt VNC `listen` 绑定到 `127.0.0.1` 并使用面板内的 noVNC 代理。虚拟机详情页和控制台页会提示监听所有接口、非回环地址或未设置 VNC 密码的配置。
- 高级命令中心等价于宿主机管理员权限，只授予可信管理员；泄露管理员账号等同于泄露宿主机控制权。

### 备份与恢复

生产环境必须同时备份 PostgreSQL、`master.key` 和面板 SSH 材料。`master.key` 缺失后，数据库中已保存的主机 URI 和任务载荷无法解密；只备份数据库不够。

原生部署示例：

```bash
umask 077
pg_dump -h 127.0.0.1 -p 5432 -U wvc -Fc -f "wvc-$(date +%F).dump" wvc
sudo install -m 0600 /var/lib/webvirtcloud-go/data/master.key /安全备份目录/master.key
sudo tar -C /var/lib/webvirtcloud-go -czf /安全备份目录/wvc-ssh.tar.gz .ssh
```

Compose 示例：

```bash
umask 077
docker compose exec -T db sh -c 'pg_dump -U "$POSTGRES_USER" -Fc "$POSTGRES_DB"' > "wvc-$(date +%F).dump"
docker compose exec -T app cat /app/data/master.key > master.key
chmod 600 master.key
tar -C deploy -czf wvc-ssh.tar.gz ssh
```

恢复时先停止应用，恢复数据库到同一时间点，再把匹配的主密钥和 SSH 材料放回原路径并修正所有权/权限，最后启动应用。原生 PostgreSQL 可使用 `pg_restore -h 127.0.0.1 -p 5432 -U wvc -d wvc wvc-YYYY-MM-DD.dump`；远程数据库请替换 host/port。恢复前先在测试库演练。

### 升级

升级前先备份数据库和主密钥。应用启动时会自动执行数据库 migration，不需要手工建表；不要跳过多个大版本后直接覆盖，先阅读对应版本的发布说明。

从引入任务串行化的旧版本升级时，迁移 `009_job_serialization_backfill.sql` 会为历史排队/运行中任务补齐资源锁。如果旧数据库里同一主机存在多个历史 `running` 任务，迁移会保留最早的一条，并把其余记录标记为失败，避免升级后绕过串行约束。升级前请先确认宿主机上的真实任务状态；升级窗口中应停止新的写操作。

原生部署：

```bash
make build
sudo install -o root -g root -m 0755 bin/wvc /usr/local/bin/wvc
sudo systemctl restart webvirtcloud-go
sudo journalctl -u webvirtcloud-go -n 100 --no-pager
```

Compose：

```bash
docker compose build --pull app
docker compose up -d
docker compose logs --tail=100 app
```

多副本部署应共享同一个 PostgreSQL、同一个 `master.key` 和一致的 SSH 材料，并尽量在同一维护窗口内完成所有实例升级，避免长期混跑不兼容版本。本机目标命令只作用于接收任务的实例；如果要使用本机 QEMU/virt 工具，建议固定到单一执行节点或共享相同的镜像/工具环境。远程 `qemu+ssh` 命令作用于主机配置指向的物理机，不受接收任务的副本影响。

### 运行维护

- 任务和审计按 `WVC_JOB_RETENTION`、`WVC_AUDIT_RETENTION` 自动清理；审计 checkpoint 会保留被清理区间的链锚点。
- `/metrics` 暴露 `wvc_audit_outbox_total`、`wvc_audit_outbox_due`、`wvc_audit_outbox_failed`；持续增长表示主审计链写入异常，应检查 PostgreSQL 和进程日志。
- 控制台令牌的防重放记录存储在 PostgreSQL 中，多副本部署不需要粘性会话；过期记录由周期维护自动清理。
- 确保 `/var/lib/libvirt/images` 或自定义 `WVC_IMAGE_DIR` 有足够空间。Compose 默认把命名卷 `wvc-tmp` 挂到 `TMPDIR=/var/tmp`，避免临时镜像写满 Docker 可写层；该卷仍会占用 Docker 数据盘，需用 `docker system df -v` 监控其容量并定期清理残留临时文件。大磁盘转换也可以按上文改用独立宿主机目录。
- libguestfs 有 `/dev/kvm` 时才使用 KVM 加速；没有时退回软件模拟。Compose 默认 `cap_drop: ALL`；`guestmount` 因默认 fork 且需要长期挂载，不在异步任务中开放。
- 首次登录后修改管理员密码并删除 `initial-admin-password-*.txt`。定期检查 `WVC_LOG_LEVEL=warn/error` 日志、登录限流触发情况和审计页面。

## 已知边界

- 当前是单组织角色模型，没有租户、项目或按 VM 的细粒度 ACL；同角色用户看到同一套主机资源。
- 主机级写任务锁按 Host ID 生效。同一台物理机不要重复登记成多个 Host，否则这些记录不会自动互斥；`virsh migrate` 等跨主机操作只锁定发起任务的主机，目标主机上的并发写任务需由运维避让或在维护窗口执行。
- 支持可选 OpenLDAP/Active Directory 登录和任务 Webhook。没有内建 SMTP、OAuth、备份调度器或计费。
- `qemu+ssh` 远程执行依赖目标物理机已安装对应工具、SSH 用户权限和 `WVC_REMOTE_PATH`；`qemu+tcp/tls` 主机没有 SSH 端点，无法执行独立 QEMU 工具。远程目标有 `setsid` 时，命令以独立进程组运行，超时、取消或 SSH 通道断开会清理整个进程组；目标机没有 `setsid` 时退化为清理直接子进程，自行创建新会话并逃逸的孙进程仍可能残留。
- Docker 镜像只包含 Debian 可提供的工具子集，命令目录比镜像实际安装的二进制更完整。Debian 未打包的 `qemu-vnc`、`virt-convert`、`virt-list-filesystems` 等工具会显示“缺失”；没有安装或未设置可用的 `WVC_*_BIN` 时不会伪装成可执行。
- `qemu-vmsr-helper` 依赖 Intel RAPL/powercap；宿主机没有相应驱动时，即使二进制存在也无法启动。
- SPICE 控制台未接入；Web 控制台目前支持 VNC（含 `qemu+ssh` 隧道）。
- libvirt 仍是 VM 事实来源，平台不维护独立 VM 清单。
- 历史 legacy SHA-256 审计链无法自动升级为 HMAC；升级后只能保证新链和迁移后链的完整性。
- 库内 HMAC 链能发现中间记录被修改或删除，但拥有数据库写权限的攻击者若删除最新尾部记录，必须依靠外部 WORM 存储、时间戳服务或定期导出的链尾锚点才能发现。
- 控制台令牌位于 WebSocket/密码请求的 URL query 中；应用和示例 Nginx 不记录 query，但浏览器历史、调试工具或错误采集系统仍可能捕获短时令牌。令牌已绑定会话并一次性消费，仍应避免复制或长期保留控制台 URL。
- 取消 running 任务会请求终止整个进程组；如果命令在取消信号生效前已经正常退出，任务仍可能记录为成功。最终结果以任务详情、退出码和审计记录为准。
- `virsh start/create --console` 会持续附着到串口控制台，因此命令中心禁止以异步任务执行；启动虚拟机后请使用 Web VNC 控制台。
- `virt-rescue`、`virt-login-shell` 需要 TTY，`virt-p2v` 是物理机图形客户端，`guestmount` 会 fork 并等待卸载；这些入口保留目录说明但禁止异步提交。`virt-tail` 会持续跟随文件，只能依赖任务超时终止，并通常以超时状态结束。
- 直接运行 `qemu-system-x86_64` 会绕过 libvirt 的 VM 生命周期和资源管理，因此不提供任意命令行入口；虚拟机应通过 libvirt、`virt-install` 和命令中心的 QEMU 工具管理。

## 验证

```bash
make check
make test-race
make test-integration
make staticcheck
make gosec
make vuln
```

测试包含命令注入/连接覆盖校验、加密用途绑定与旧密文迁移、审计降级检测、控制台 WebSocket 代理/同源边界、登录限流和输入校验、CSRF、多物理机并行与同物理机串行、主机创建、VM 分组页面、异步 `virsh` 任务及角色越权。

noVNC 资源来自 noVNC 1.7.0，核心库按 MPL 2.0 授权；完整许可证文本见 `internal/web/static/novnc/docs/`。

## License

本项目使用 [Apache License 2.0](LICENSE)。第三方组件、Go 依赖许可证和上游文档来源见 [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md)、[NOTICE](NOTICE) 和 `third_party/`；noVNC 仍按其 MPL-2.0 许可证分发。
