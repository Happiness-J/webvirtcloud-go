# Third-party licenses

WebVirtCloud Go is distributed under Apache-2.0. The project also contains or links third-party code and documentation assets under their respective licenses.

## Go dependencies

The full license texts are stored under `third_party/` and are included in release artifacts and the container image.

| Module | Version | License |
|---|---|---|
| github.com/gorilla/websocket | v1.5.3 | BSD-2-Clause |
| github.com/jackc/pgpassfile | v1.0.0 | MIT |
| github.com/jackc/pgservicefile | 5a60cdf6a761 | MIT |
| github.com/jackc/pgx/v5 | v5.12.0 | MIT |
| github.com/jackc/puddle/v2 | v2.2.3 | MIT |
| golang.org/x/crypto | 2f1f834330b2 | BSD-3-Clause |
| golang.org/x/sync | v0.25.0 | BSD-3-Clause |
| golang.org/x/sys | v0.50.0 | BSD-3-Clause |
| golang.org/x/text | v0.43.0 | BSD-3-Clause |

Versions are defined authoritatively by `go.mod` and `go.sum`. Regenerate the license tree with `make licenses` after approved dependency changes.

## noVNC 1.7.0

The bundled noVNC core is licensed under MPL-2.0. The noVNC distribution also includes MPL-2.0, BSD-2-Clause, BSD-3-Clause, SIL Open Font License 1.1, and pako's MIT license texts.

See:

- `internal/web/static/novnc/LICENSE.txt`
- `internal/web/static/novnc/AUTHORS`
- `internal/web/static/novnc/docs/`
- `internal/web/static/novnc/vendor/pako/LICENSE`

## Command metadata and runtime tools

The structured command catalog is derived from public libvirt, QEMU, virtinst, and libguestfs documentation. Links and upstream names remain subject to their respective projects and licenses. Runtime packages in the container image retain Debian package copyright metadata under `/usr/share/doc/`.
