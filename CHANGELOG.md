# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and releases use [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- `wvc version`, `wvc doctor`, `wvc reset-password`, and `wvc help` commands.
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

### Security

- Updated `golang.org/x/crypto` to a revision containing the `GO-2026-5932` module fix.

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
