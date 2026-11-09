# Operations

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
