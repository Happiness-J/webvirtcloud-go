# Security Policy

## Supported versions

Security fixes are provided for the latest tagged release and the `main` branch. Users should run a supported release and keep PostgreSQL, libvirt, QEMU, OpenSSH, the Go toolchain, and container base images current.

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability.

Use the repository's **Security → Advisories → Report a vulnerability** flow to create a private GitHub security advisory. Include:

- affected version or commit
- deployment model and relevant configuration, with secrets removed
- reproduction steps or a minimal proof of concept
- security impact
- suggested mitigation, if known

If private advisories are not yet enabled, contact the repository maintainers through a private channel listed in the repository profile. Do not publish exploit details before a fix and coordinated disclosure date are agreed.

Maintainers aim to acknowledge a report within three business days, validate it, prepare a fix, and publish an advisory with affected and fixed versions. Timelines depend on severity and reproducibility.

## Security boundary

WebVirtCloud Go is a privileged management plane, not a low-privilege dashboard.

- The `admin` command center is equivalent to administrator access on managed KVM hosts.
- A `viewer` must not be able to execute commands, mutate hosts, or open a VNC console.
- Host connection URIs and sensitive task fields are encrypted with a key that is not stored in PostgreSQL.
- VNC traffic has no native transport encryption. Direct HTTP or plain VNC is only suitable for a trusted LAN or encrypted VPN.
- The application does not terminate TLS. Public exposure requires a correctly configured HTTPS reverse proxy or load balancer.

See [docs/THREAT-MODEL.md](docs/THREAT-MODEL.md) for assets, trust boundaries, and non-goals.

## In scope

Examples of security-sensitive issues include:

- authentication or session bypass
- privilege escalation between roles
- CSRF or cross-origin console weaknesses
- command injection, argument injection, or connection URI override
- exposure of credentials, tokens, master keys, task payloads, or audit data
- WebSocket proxy confusion or resource exhaustion with security impact
- database migration or audit-chain integrity bypass
- container escape caused by the project's supplied configuration

## Out of scope

- vulnerabilities in a managed guest operating system or application
- attacks that require an already compromised `admin` session, unless the issue enables persistence or escapes documented boundaries
- unsupported reverse-proxy, firewall, or TLS configurations that expose plain HTTP publicly
- denial-of-service reports without a practical, project-specific mitigation
- reports produced solely by a scanner without a demonstrated affected path

Please act in good faith, avoid privacy violations or destructive testing, and give maintainers a reasonable opportunity to remediate before disclosure.
