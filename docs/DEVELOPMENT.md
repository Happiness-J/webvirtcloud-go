# Development

## Requirements

- Go 1.26.10, as selected by `go.mod`
- GNU Make
- PostgreSQL 16 or newer for integration tests
- Git
- Optional: QEMU, libvirt, virtinst, libguestfs, and OpenSSH for host integration

The application embeds templates, CSS, JavaScript, and noVNC into the Go binary. Node.js is not required.

## Build and unit tests

```bash
make check
make build
./bin/wvc version
```

`make check` verifies formatting, module tidiness, static analysis, and unit tests. Race detection requires a working C compiler:

```bash
make test-race
```

## PostgreSQL integration tests

Create a database and role with permission to create temporary schemas:

```bash
sudo -u postgres psql <<'SQL'
CREATE ROLE wvc LOGIN PASSWORD 'wvc';
CREATE DATABASE wvc_test OWNER wvc;
SQL
```

Run the complete suite:

```bash
export WVC_TEST_DATABASE_URL='postgres://wvc:wvc@127.0.0.1:5432/wvc_test?sslmode=disable'
make test-integration
```

Tests create unique schemas and drop them during cleanup. They must not point at a production database. To use the Compose PostgreSQL service instead:

```bash
docker compose up -d db
export WVC_TEST_DATABASE_URL='postgres://wvc:wvc@127.0.0.1:55433/wvc_test?sslmode=disable'
```

Create the `wvc_test` database first if the Compose volume initialized only the application database.

## Coverage

```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

For integration coverage, set `WVC_TEST_DATABASE_URL` before running the same commands.

## Docker behind an HTTP proxy

The Docker daemon does not inherit the interactive shell proxy environment. If image pulls time out while `curl` works, review `deploy/docker-http-proxy.conf.example`, install an adjusted systemd drop-in, and restart Docker. Never commit proxy credentials.

## Run locally

```bash
export WVC_DATABASE_URL='postgres://wvc:YOUR_PASSWORD@127.0.0.1:5432/wvc?sslmode=disable'
export WVC_MASTER_KEY_FILE="$(pwd)/data/master.key"
export WVC_ADDR='127.0.0.1:8080'
export WVC_ADMIN_USERNAME='admin'
make run
```

Leave `WVC_ADMIN_PASSWORD` unset to generate a one-time password beside the master key. For local shell use only:

```bash
curl -fsS http://127.0.0.1:8080/readyz
./bin/wvc doctor
```

## Code layout

| Path | Responsibility |
|---|---|
| `cmd/wvc` | process lifecycle and CLI |
| `internal/web` | HTTP routes, templates, sessions, CSRF, RBAC, console proxy wiring |
| `internal/store` | PostgreSQL access and embedded migrations |
| `internal/jobs` | asynchronous worker lifecycle |
| `internal/command` | command catalog, argument validation, execution policy |
| `internal/libvirt` | libvirt-oriented VM, storage, network, and console helpers |
| `internal/remote` | hardened SSH execution |
| `internal/console` | noVNC WebSocket-to-VNC proxying |
| `internal/secret` | purpose-bound AES-256-GCM cryptography |
| `internal/audit` | durable audit recording |

## Changing the command catalog

The command catalog is security-sensitive. Preserve argv semantics, required confirmation fields, read-only classification, session restrictions, connection override rejection, and output/time limits. Add table-driven tests for new validation paths and run the integration suite when execution policy changes.

## Dependencies

Keep `go.mod` tidy and avoid adding dependencies for small utilities already provided by the standard library. Dependabot opens weekly grouped updates. Security-sensitive dependency changes require the normal CI, race, integration, and vulnerability checks.
