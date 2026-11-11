# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and releases use [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Per-host operations workbench with host CPU/memory measurements, per-domain balloon/vCPU statistics, state-aware bulk lifecycle controls, autostart policy changes, and persisted/online vCPU and memory tuning.
- Host virtualization health checks using `virt-host-validate`, plus VM diagnostics for block-device errors, guest memory statistics, and recent libvirt events.
- VM backup creation, restore, and deletion with explicit confirmation.
- Storage-pool capacity checks and virtual-network diagnostics, including DHCP lease inspection.
- Direct restore and delete controls for existing VM snapshots.
- `wvc version`, `wvc doctor`, `wvc reset-password`, and `wvc help` commands.
- Trusted-intranet HTTP mode for noVNC via `WVC_ALLOW_INSECURE_CONSOLE`.
- Console security diagnostics for wildcard/non-loopback VNC listeners and unpassworded VNC.
- Console password retrieval now requests libvirt security-sensitive XML, which is required when VNC passwords are configured.
- Console dialing now accepts the RFC 6598 shared address range used by Tailscale and private VPNs.
- Optional installer ACL setup and `wvc doctor` diagnostics for local QEMU image directory access.
- VM disk growth and offline grow/shrink controls with explicit destructive confirmation.
- Storage pool refresh/start/autostart controls and volume create/clone/resize/delete operations.
- Virtual-network start/stop/autostart controls.
- GitHub CI, CodeQL, Dependabot, tagged release automation, checksums, and build provenance.
- Contributor, security, support, operational, publishing, and threat-model documentation.
- Complete noVNC and Go dependency license texts for source, release, and container distribution.
- Container image version labels, SBOM/provenance publishing, and a read-only Compose root filesystem.
- Make targets for formatting, dependency tidiness, race tests, vulnerability scans, snapshots, and Docker builds.

### Changed

- Listen addresses are validated before database startup.
- Updated Go dependencies to current compatible patch/minor releases.
- The native Ubuntu installer is now repository-relative and no longer contains machine-specific paths.
- VM lifecycle actions now follow the current power state, and console links are only shown for running consoles.
- The command center limits its initial rendered list and correctly discovers current virsh and qemu-img help formats.
- Audit details, stopped-VM console errors, job timestamps, favicon handling, and dashboard tool-list density were corrected.
- noVNC now refuses insecure HTTP contexts with a clear message instead of reporting a false connected state.
- Native systemd deployment now uses a strict filesystem view, an empty capability set, private home directories, process limits, and additional kernel/namespace protections.
- The Ubuntu installer no longer prints the generated initial administrator password into installer logs.

### Security

- Updated `golang.org/x/crypto` to a revision containing the `GO-2026-5932` module fix.
- The service logs explicit startup warnings for plain-HTTP consoles and for a bootstrap password left in the service environment.
- VM and console pages now warn when libvirt exposes VNC on all interfaces, a non-loopback address, or without a password.
- Native installation includes a scoped polkit rule that permits only the `wvc` service account to read libvirt secure domain XML for authenticated VNC access.

## [0.1.0] - Initial development

- Single-binary Go control plane with PostgreSQL-backed users, hosts, sessions, jobs, and audit history.
- QEMU/KVM lifecycle management through libvirt.
- Async task queue with deduplication, quotas, cancellation, heartbeat, and stale-job recovery.
- Purpose-bound AES-256-GCM storage for host URIs and sensitive job data.
- Server-rendered Web UI, RBAC, CSRF protection, login throttling, and audit hash chain.
- Built-in noVNC WebSocket proxy.
- Local and `qemu+ssh` host execution modes.
- Structured command center covering libvirt, QEMU tools, virtinst, and libguestfs.
- Native systemd, Ubuntu installer, Docker Compose, and Nginx deployment examples.
