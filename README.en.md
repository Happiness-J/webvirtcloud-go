# WebVirtCloud Go

A single-binary QEMU/KVM control plane for Linux, backed by libvirt and PostgreSQL.

WebVirtCloud Go is designed for small infrastructure teams that need a practical owner-operated virtualization panel with explicit privilege boundaries. It is not a drop-in line-for-line port of the legacy Python WebVirtCloud.

## Features

- server-rendered Web UI embedded in one Go binary
- local libvirt and remote `qemu+ssh` hosts
- VM lifecycle, snapshots, storage pools, volumes, and virtual networks
- per-host operations workbench with node/VM resource visibility, bounded bulk lifecycle actions, autostart policy, and vCPU/memory tuning
- host virtualization health checks and VM diagnostics for disk errors, guest memory, recent events, and snapshot recovery
- asynchronous tasks with quotas, cancellation, heartbeat, and stale-job recovery
- Argon2id passwords, server-side sessions, CSRF, RBAC, and login throttling
- purpose-bound AES-256-GCM encryption for host URIs and sensitive task fields
- audit hash chain with durable outbox recovery
- built-in noVNC 1.7.0 and WebSocket-to-VNC proxy
- host-side VNC exposure diagnostics for wildcard listeners and missing VNC passwords
- hardened native systemd sandbox and local QEMU image permission diagnostics
- structured command center for libvirt, QEMU tools, virtinst, and libguestfs
- systemd, Docker Compose, and Nginx deployment examples

## Quick start

Native build:

```bash
go version
make check
make build
./bin/wvc version
```

Run with PostgreSQL:

```bash
export WVC_DATABASE_URL='postgres://wvc:CHANGE_ME@127.0.0.1:5432/wvc?sslmode=disable'
export WVC_MASTER_KEY_FILE="$PWD/data/master.key"
export WVC_ADDR='127.0.0.1:8080'
make run
```

Docker Compose:

```bash
cp .env.example .env
chmod 600 .env
# Set POSTGRES_PASSWORD and review every deployment-specific option.
docker compose up -d --build
docker compose logs -f app
```

The default Compose publish address is loopback-only. Direct HTTP should stay on a trusted LAN or encrypted VPN. Put the application behind HTTPS before exposing it to an untrusted network.

## Documentation

- [Chinese deployment and reference guide](README.md)
- [Development](docs/DEVELOPMENT.md)
- [Operations and backup](docs/OPERATIONS.md)
- [Threat model](docs/THREAT-MODEL.md)
- [Publishing to GitHub](docs/PUBLISHING.md)
- [Contributing](CONTRIBUTING.md)
- [Security policy](SECURITY.md)
- [Command coverage](docs/COMMANDS.md)

## Security warning

The administrator command center is effectively administrator access to managed KVM hosts. VNC has no native transport encryption. Do not expose plain HTTP or VNC to the public Internet. See [SECURITY.md](SECURITY.md).

## License

Apache License 2.0. Bundled third-party notices are listed in [NOTICE](NOTICE) and under `internal/web/static/novnc/`.
