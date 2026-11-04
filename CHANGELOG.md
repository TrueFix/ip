# Changelog

## v0.1.3 — 2026-11-04

- Expand raw API releases to 20 OS/CPU targets across Linux, macOS and Windows.
- Generate release uploads from the checksum manifest so every built target is included.

## v0.1.2 — 2026-11-04

- Publish standalone API binaries instead of release archives and bundled helpers.

## v0.1.1 — 2026-11-04

- Update the MaxMind database reader to v1.13.1 and flock to v0.13.1.
- Update the runtime image to Alpine 3.24.
- Upgrade the pinned checkout, QEMU, Buildx, Docker login and build/push Actions.
- Keep `latest` as the default Compose image tag for both services.

## v0.1.0 — 2026-11-04

Initial release.

- Local IPv4/IPv6 lookups with MaxMind, DB-IP fallback and optional IPinfo.
- SQLite update audits, checksummed snapshots and conditional downloads.
- Trusted-proxy handling, RPM/RPS limits and private Prometheus metrics.
- One Docker Hub image for the API and updater.
- Static Linux releases for amd64, arm64, ARMv7, 386 and RISC-V 64.
- Automatic Docker Hub and GitHub Release publishing on version tags.
