# Contributing to WebVirtCloud Go

Thank you for contributing. This project manages privileged QEMU/KVM control planes, so changes touching authentication, authorization, command construction, process execution, cryptography, proxying, database migrations, or deployment defaults require extra care.

## Before you start

- Search existing issues and pull requests.
- Open an issue before large features or behavior changes.
- Report vulnerabilities privately as described in [SECURITY.md](SECURITY.md).
- Never include passwords, session cookies, console tokens, master keys, SSH private keys, database dumps, VM disks, or production logs with secrets.

## Development requirements

- Go 1.26.10 or newer, matching `go.mod`
- GNU Make
- PostgreSQL 16 or newer for integration tests
- Git
- Linux QEMU/libvirt packages only when testing host integration

See [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) for setup commands.

## Required checks

Run these before opening a pull request:

```bash
make check
make test-race
```

When persistence, authentication, job execution, or HTTP routing changes, also run the PostgreSQL integration suite:

```bash
export WVC_TEST_DATABASE_URL='postgres://wvc:wvc@127.0.0.1:55432/wvc_test?sslmode=disable'
make test-integration
```

Security-sensitive changes should also run:

```bash
make vuln
```

## Engineering rules

- Keep command execution argv-based. Do not introduce shell interpolation for locally executed tools.
- Treat host URIs and task payloads as untrusted. Preserve purpose-bound authenticated encryption.
- Preserve the distinction between `viewer`, `operator`, and `admin`. The command center grants administrator-equivalent host access.
- Add or update tests for behavior changes. Integration tests must create isolated schemas and clean up after themselves.
- Update migrations in a forward-only sequence. Never rewrite a migration that may already have run.
- Keep HTML templates auto-escaped. Do not add template.HTML or client-side innerHTML for user-controlled data.
- Keep new dependencies minimal and justify their maintenance and security cost.
- Update README or the files under `docs/` when deployment, configuration, security, or operations change.

## Commit and pull request guidance

Use focused commits with imperative messages such as `fix: reject invalid listen addresses`. A pull request should explain:

- the user-visible problem
- the chosen implementation
- security and compatibility impact
- tests performed
- migration or rollback notes when applicable

Maintainers may ask for a smaller change or a threat-model review before merging privileged behavior.

## License

By submitting a contribution, you agree that it may be distributed under the Apache License 2.0, as described in [LICENSE](LICENSE).
