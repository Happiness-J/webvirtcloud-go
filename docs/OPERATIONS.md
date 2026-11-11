# Operations

## Desktop users

The `desktop` role is intended for people who need a graphical console but must not administer the platform. An administrator grants access to specific VM names. A desktop user can open only those VNC consoles and use start or graceful shutdown for those assigned VMs. Reboot, forced power-off, storage changes, command execution, and user administration remain unavailable.

Windows, Linux, and other graphical guests use the same console path. Install and run the QEMU Guest Agent in the guest when operators need OS, user, and filesystem information from the VM page.

## Backup, capacity, and network checks

The VM page lists guest IP addresses. It first asks the QEMU Guest Agent and falls back to libvirt DHCP leases when the agent is unavailable. This lets operators and desktop users reach SSH or RDP without opening the graphical console.

Authorized operators and desktop users can request a read-only PNG screenshot of a running or paused VM before opening noVNC. Desktop users only receive screenshots for VMs explicitly assigned to them. The image is returned directly to the browser and is not stored by the panel.

The VM page shows when libvirt reports a pending ACPI shutdown reason. A graceful shutdown only requests the guest to stop; it does not confirm that the operating system has finished. If the request remains pending, check the guest agent and ACPI support before using forced power-off.

Operators can create, restore, and delete libvirt backups from a VM page. Restore and delete require the confirmation checkbox. A restore replaces the current VM disk state, so the VM should be shut down and at least one fallback backup retained.

Each storage pool has a capacity check that records `pool-info` as a job. Virtual networks have two diagnostic actions: `net-info` for bridge and activation state, and `net-dhcp-leases` for current DHCP assignments.

## Host and VM diagnostics

Operators can queue `virt-host-validate --quiet qemu` from a host page. The result is stored as a normal job, including any failed KVM, cgroup, device, or security-driver checks.

A VM detail page also shows three bounded diagnostic views:

- `domblkerror`: current block-device errors that may explain stalled I/O.
- `dommemstat`: guest memory statistics when the guest agent responds.
- `event --timestamp --timeout 1`: lifecycle or device events observed during a one-second sampling window.

Existing snapshots can be restored or deleted directly from their rows. Both actions require the explicit confirmation checkbox because they can discard VM state or recovery points.

## Health checks

- `/healthz`: process is serving HTTP.
- `/readyz`: database, configured host execution dependencies, and at least one live job worker are ready.
- `/metrics`: disabled until `WVC_METRICS_TOKEN` is set; use `Authorization: Bearer <token>`.

Use `wvc healthcheck` for container probes and `wvc doctor` for configuration and dependency diagnostics. `wvc doctor` does not start the server or apply migrations.

If the configured administrator password is lost, reset it from the panel host:

```bash
sudo -u wvc bash -c 'set -a; . /etc/webvirtcloud-go/env; /usr/local/bin/wvc reset-password'
sudo -u wvc cat /var/lib/webvirtcloud-go/data/reset-password-*.txt
```

The command generates a fresh random password, atomically writes a mode-0600 one-time file, revokes every existing session for that account, and writes an audit event. Change the password after login and delete the file.

## Operations workbench

Open a host and choose **运维工作台** (Operations workbench) to inspect node information, current CPU/memory measurements, per-domain vCPU and balloon memory, and accumulated CPU time. The page uses read-only `virsh nodeinfo`, `nodecpustats --percent`, `nodememstats`, and `domstats` calls against the host execution target, including `qemu+ssh` targets.

Operators and administrators can submit state-checked bulk actions for up to 50 VMs at a time: start, graceful shutdown, reboot, suspend, resume, and autostart policy. The complete selection is validated before the first job is queued, so a VM in an incompatible power state rejects the batch instead of producing a misleading partial request. Every accepted VM still becomes an individual asynchronous job with its own audit record; per-host writes remain serialized by the worker.

VM detail pages expose vCPU and memory tuning:

- **下次启动生效** changes persistent configuration with `virsh setvcpus --config` or `virsh setmem --config`.
- **在线并持久化** requires a running or paused VM and applies `--live --config`; guest support, maximum vCPU/memory limits, and hot-plug capability still determine whether libvirt accepts the change.
- **最大 vCPU 上限** updates the persistent maximum with `--maximum --config`; raise this before hot-adding vCPUs to an existing VM when the current maximum is too low.

Resource tuning changes virtual hardware only. Extending partitions, filesystems, guest applications, or NUMA placement remains a guest/host administrator task. Review the resulting job output and confirm both live and persistent values before considering the change complete.

## Backups

A restorable deployment requires both PostgreSQL and the master key:

1. Dump PostgreSQL with `pg_dump --format=custom`.
2. Back up `WVC_MASTER_KEY_FILE` from a secure location.
3. Record the deployed binary or container version.
4. Protect backups as sensitive data.

The master key decrypts host connection URIs and sensitive job fields. Losing it can make encrypted records unrecoverable. Never place it in the database or a public object store.

Example:

```bash
sudo -u postgres pg_dump --format=custom --file=/secure-backup/wvc.dump wvc
sudo install -m 0600 /var/lib/webvirtcloud-go/data/master.key /secure-backup/master.key
```

Retention and off-host backup policy depends on the organization and may include encrypted storage and restore drills.

## Local image-tool access

The panel service runs as the unprivileged `wvc` user. `virsh` operations use the libvirt socket, but offline `qemu-img`, libguestfs, and `virt-v2v` commands access disk files directly and therefore need filesystem permission in addition to the systemd `ReadWritePaths` sandbox exception. The path checked by `wvc doctor` is `WVC_LOCAL_IMAGE_DIR` and defaults to `/var/lib/libvirt/images`.

Debian and Ubuntu commonly ship `/var/lib/libvirt/images` as `root:root` mode `0711`, which is not writable by `wvc`. A reviewed native installation can grant a narrow POSIX ACL:

```bash
sudo apt install acl
sudo setfacl -R -m u:wvc:rwX /var/lib/libvirt/images
sudo setfacl -d -m u:wvc:rwx /var/lib/libvirt/images
sudo -u wvc bash -c 'set -a; . /etc/webvirtcloud-go/env; set +a; /usr/local/bin/wvc doctor'
```

The Ubuntu installer performs the same operation only when invoked with `WVC_GRANT_IMAGE_ACCESS=1`. This grants the management plane effective write access to local VM disks; omit it when managing only remote `qemu+ssh` hosts or when using a separate, narrowly scoped image directory.

## Restore

Restore the database and key from the same point in time, then start a matching or newer application version:

```bash
sudo systemctl stop webvirtcloud-go
sudo -u postgres pg_restore --clean --if-exists --create --dbname=postgres /secure-backup/wvc.dump
sudo install -o wvc -g wvc -m 0600 /secure-backup/master.key /var/lib/webvirtcloud-go/data/master.key
sudo systemctl start webvirtcloud-go
```

Validate `/readyz`, log in, open a host, and confirm an encrypted host URI and a recent encrypted job can be read. The application verifies the audit hash chain during startup and will refuse a tampered or incompatible chain.

## Upgrades

1. Back up PostgreSQL and the master key.
2. Read the release notes and configuration changes.
3. Replace the binary or pull the new container image.
4. Restart the service.
5. Verify `/readyz`, logs, host discovery, and a read-only command.

Schema migrations run automatically. Do not run an older binary against a database migrated by a newer binary. Roll back to a known-good binary only when the migration remains compatible; otherwise restore the pre-upgrade backup.

## TLS and network exposure

The Go process serves HTTP. For any untrusted network:

- terminate TLS in Nginx, Caddy, Traefik, or a cloud load balancer
- set `WVC_SECURE_COOKIES=true`
- set `WVC_TRUST_PROXY=true`
- restrict `WVC_TRUSTED_PROXY_CIDRS` to actual proxy addresses
- preserve WebSocket upgrades for `/console/<host>/<vm>/ws`
- avoid logging query strings for console routes

Direct HTTP is appropriate only for a trusted LAN or encrypted VPN. Do not expose port 8080 directly to the public Internet.

### Tailscale HTTPS

When Tailscale MagicDNS and HTTPS certificates are available, Tailscale Serve can provide a trusted HTTPS endpoint without a public DNS record:

```bash
sudo tailscale serve --bg 8080
```

The packaged `deploy/wvc-tailscale.conf` systemd drop-in trusts only Tailscale's loopback proxy so HTTPS WebSocket Origin checks work while direct LAN HTTP remains available:

```bash
sudo install -d -m 0755 /etc/systemd/system/webvirtcloud-go.service.d
sudo install -m 0644 deploy/wvc-tailscale.conf \
  /etc/systemd/system/webvirtcloud-go.service.d/tailscale.conf
sudo systemctl daemon-reload
sudo systemctl restart webvirtcloud-go
tailscale serve status
```

Open the panel through the resulting `https://<machine>.<tailnet>.ts.net/` URL for VNC. noVNC deliberately refuses to initialize on plain HTTP because browsers do not expose the required cryptographic APIs outside a secure context.

## VNC

VNC has no transport encryption. Prefer local libvirt or `qemu+ssh` consoles. `WVC_ALLOW_INSECURE_VNC=true` weakens the default console boundary and should only be used when another encrypted network layer protects VNC.

For a trusted intranet where the panel is intentionally served over plain HTTP, set `WVC_ALLOW_INSECURE_CONSOLE=true`. This allows noVNC to initialize outside a browser secure context. It does not encrypt login credentials, sessions, VNC passwords, or console traffic. The example systemd drop-in is `deploy/wvc-intranet-http.conf`.

Direct VNC targets are restricted to loopback, RFC 1918/4193 private addresses, and the RFC 6598 `100.64.0.0/10` shared range used by Tailscale and some internal VPNs. Public VNC addresses remain rejected.

Bind libvirt VNC graphics to `127.0.0.1` and use the panel's WebSocket proxy:

```xml
<graphics type='vnc' port='-1' autoport='yes' listen='127.0.0.1' passwd='CHANGE_ME'/>
```

The VM and console pages warn when VNC listens on `0.0.0.0`, a non-loopback address, or has no password. A wildcard listener without a password lets any reachable client bypass panel authentication and take over the console. Correct the libvirt definition and restart or update the graphics device; do not rely on the application proxy to protect a separately exposed VNC port.

Password-protected VNC requires libvirt security-sensitive domain XML. The packaged `deploy/60-webvirtcloud-go.rules` grants `org.libvirt.api.domain.read-secure` only to the `wvc` service account. For a manual installation, install that file under `/etc/polkit-1/rules.d/` and restart polkit before using the panel console. Do not grant this action to interactive or unrelated users.

Virtinst can update the live and persistent graphics definition without dropping the guest:

```bash
read -r -s -p 'New VNC password: ' VNC_PASSWORD
printf '\n'
virt-xml DOMAIN --update --define --edit \
  --graphics "vnc,listen=127.0.0.1,password=${VNC_PASSWORD}"
unset VNC_PASSWORD
```

Review the resulting definition with `virsh dumpxml DOMAIN` before relying on it. Some QEMU or libvirt versions may require a guest restart for graphics changes.

## systemd sandbox

The packaged `deploy/wvc.service` uses `ProtectSystem=strict`, `ProtectHome=true`, an empty capability bounding set, `RestrictAddressFamilies`, `RestrictRealtime`, `RestrictNamespaces=~user`, `SystemCallArchitectures=native`, core-dump suppression, and a `TasksMax` limit. `ReadWritePaths` allows only the application state directory, its private `/var/tmp`, and the optional libvirt image directory.

If an organization installs additional QEMU/libguestfs helpers that require a Linux namespace or device, add a reviewed systemd drop-in rather than removing all hardening:

```bash
systemd-analyze security webvirtcloud-go.service
```

After the first administrator is created, remove `WVC_ADMIN_PASSWORD` from the service environment. It is only a bootstrap input; keeping it in the environment adds no benefit and increases the chance that deployment backups or diagnostics capture it.

## Logs and retention

Use structured JSON logs in production when your log platform supports them:

```bash
WVC_LOG_FORMAT=json
WVC_LOG_LEVEL=info
```

Configure job and audit retention with `WVC_JOB_RETENTION` and `WVC_AUDIT_RETENTION`. Audit retention must satisfy organizational and regulatory requirements.

## Scaling

Multiple replicas can share PostgreSQL. Per-host writes are serialized through the database, and console token use is atomic. Session state also lives in PostgreSQL. Ensure all replicas use the same master key, database, and trusted proxy configuration.

The noVNC WebSocket connection is handled by the replica that accepted the browser request. Use sticky routing only if required by the surrounding load balancer; the application does not require it for ordinary HTTP requests.

## Firewall guidance

Expose only the reverse-proxy listener or a restricted application port. Restrict PostgreSQL to the application network. For native deployment, PostgreSQL normally listens on `127.0.0.1:5432`; do not expose it publicly.

The optional Ubuntu installer does not change firewall rules unless `WVC_UFW_ALLOW_CIDRS` is explicitly provided.
