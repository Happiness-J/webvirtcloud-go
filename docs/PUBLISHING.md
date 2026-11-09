# Publishing to GitHub

This repository contains the files needed for GitHub CI, CodeQL, Dependabot, issue forms, pull request checks, tagged releases, checksums, build provenance, and GHCR container publication.

## 1. Initialize the local repository

The current project directory is not a Git repository:

```bash
cd /path/to/webvirtcloud-go
git init -b main
git add .
git status --short
git commit -m "Initial WebVirtCloud Go import"
```

Review `git status` before committing. Never add `.env`, master keys, SSH private keys, generated admin-password files, database dumps, or VM disks.

## 2. Create an empty GitHub repository

Create an empty repository named `webvirtcloud-go`. Do not let GitHub add a generated README, license, or `.gitignore`.

```bash
git remote add origin git@github.com:OWNER/webvirtcloud-go.git
git push -u origin main
```

Replace `OWNER` with the actual account or organization.

## 3. Update repository-specific metadata

- Enable Actions, Dependabot alerts, Dependabot security updates, and CodeQL.
- Enable private vulnerability reporting under **Settings → Security**.
- Enable Discussions if you want usage questions and community support.
- Protect `main`: require pull requests, CI, CodeQL, and conversation resolution before merge.
- Add the repository description and topics: `kvm`, `libvirt`, `qemu`, `virtualization`, `golang`, `postgresql`, `novnc`.
- Optionally add the repository URL to `README.md` and `README.en.md`.

The workflow files use `github.repository`, so they do not need owner-specific edits.

## 4. Verify CI

Open a pull request containing a small documentation change. Confirm these checks pass:

- `Test and build`
- `Vulnerability scan`
- `Build container`
- `Analyze Go`

The test job starts PostgreSQL and runs the store and web integration suites under the race detector.

## 5. Publish a release

Tag a semantic version and push it:

```bash
git tag -a v0.1.0 -m "WebVirtCloud Go v0.1.0"
git push origin v0.1.0
```

The release workflow:

- builds Linux `amd64` and `arm64` binaries
- generates `SHA256SUMS`
- attaches GitHub build provenance
- creates a GitHub release with generated notes
- publishes versioned and SHA-tagged container images to `ghcr.io/OWNER/webvirtcloud-go`
- attaches an SBOM and provenance to the container image

After publication, verify `dist` artifacts, release notes, the container manifest, and one fresh deployment from the published artifact.

## 6. Optional signing

Enable GitHub artifact attestation verification for consumers:

```bash
gh attestation verify wvc-linux-amd64 --repo OWNER/webvirtcloud-go
```

Organizations with a release-signing policy can add Sigstore Cosign or their existing signing system around the produced checksums.

## 7. Before announcing

- Replace template placeholders or repository-specific support contacts where required.
- Add a real screenshot or short demonstration if one is available; do not publish infrastructure details.
- Confirm the default branch is `main`.
- Confirm the Apache-2.0 license and bundled noVNC notices are present in releases.
- Test Docker Compose from a clean checkout.
- Test native installation on a disposable Ubuntu/Debian machine.
- Rotate any credentials used during development.
