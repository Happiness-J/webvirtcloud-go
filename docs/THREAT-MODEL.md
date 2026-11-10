# Threat model

## Assets

- libvirt connection URIs and SSH credentials
- the master key and derived console-token keys
- administrator/operator sessions and CSRF tokens
- queued command arguments, stdin, output, and errors
- VM, storage, and network control operations
- audit history and its integrity chain
- VNC passwords and live console traffic

## Actors

- unauthenticated Internet or LAN client
- authenticated viewer
- authenticated operator
- administrator, including the command center
- local unprivileged process on the panel host
- compromised guest VM
- database attacker with read access
- malicious or compromised remote KVM host

## Trust boundaries

1. Browser to reverse proxy or application HTTP listener
2. application to PostgreSQL
3. application to local libvirt/QEMU process and Unix socket
4. application to remote hosts through OpenSSH, TCP, or TLS
5. application to VNC listeners
6. build pipeline and released container/binary to operator

## Key controls

- Argon2id password hashing and server-side sessions
- role checks independent of UI visibility
- CSRF checks on state-changing requests
- login throttling by source/account pair, source IP, and global observation
- auto-escaped server-side templates and restrictive security headers
- purpose-bound AES-256-GCM for host URIs and sensitive task data
- argv-based local execution with catalog/policy validation
- hardened OpenSSH options and strict known-host verification for remote execution
- short-lived, single-use, purpose-scoped console tokens
- host-side VNC exposure diagnostics for wildcard/non-loopback listeners and missing passwords
- PostgreSQL-backed per-host write serialization
- durable audit outbox and HMAC hash-chain verification
- native systemd sandboxing with no ambient capabilities and a read-only OS hierarchy
- non-root container process, dropped capabilities, and no-new-privileges

## Assumptions

- administrators protect their browsers and credentials
- TLS is correctly terminated outside the application when exposed beyond a trusted network
- the master key is protected and backed up
- PostgreSQL and host operating systems are patched
- SSH host keys and remote accounts are correctly configured
- container deployments are not granting additional Linux capabilities or host mounts

## Non-goals

- protecting a guest from a malicious guest administrator
- making plain VNC confidential
- providing tenant isolation inside one libvirt host
- defending an already compromised `admin` session from approved privileged work
- replacing host firewalls, SELinux/AppArmor, EDR, or infrastructure monitoring
- protecting secrets from a fully compromised panel host with root access

## High-risk changes

Review authentication, role mapping, template rendering, proxy headers, URI parsing, argument construction, command catalog policy, cryptography, migrations, audit chaining, and container privileges as security-sensitive changes.
