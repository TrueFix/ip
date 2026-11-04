# Changelog

## Unreleased

- Use only the matching `CHANGELOG.md` version section for GitHub release notes, including reruns.

## v1.0.0 — 2026-11-04

- Fix loaded-snapshot integrity checks during failed replacement publications and reopen readers after atomic repair.
- Normalize IPv4-mapped trusted-proxy CIDRs and consistently hide private status routes on the public listener.
- Restrict download redirects to HTTPS and approved hosts, suppress credential-bearing referrers, and reclaim definitely unpublished snapshots after late failures.
- Precompute country/flag metadata and cache timezone locations with bounded storage while preserving current DST calculations.
- Gate publishing on tests, dependency audits and production-container smoke checks; verify main/manual runs without publishing.
- Prevent older stable releases from downgrading Docker `latest`; add command lifecycle and failure-path tests.
- Consolidate operational documentation, remove historical session notes, and add one root `llms.txt` guide.
- Publish measured lookup/enrichment benchmarks with reproduction details, and validate documentation links in tests.

## v0.1.4 — 2026-11-04

- Add `RATELIMIT_ENABLE` (default `true`) to disable all built-in limiting with `false`.
- Present configuration in source-specific tables with explicit authentication requirements and local ISP examples.
- Remove Docker-only environment overrides; configure image tags, ports and mounts directly in Compose.
- Interpret `RATE_LIMIT_RPM=0` as zero allowed requests; only `RATELIMIT_ENABLE=false` disables limiting.
- Separate source-specific environment examples and document every runtime/Compose setting.
- Reduce the root environment example to credential-free DB-IP with optional overrides commented out.
- Add a short deployment guide and standalone DB-IP/MaxMind Compose templates.
- Require both MaxMind credentials at Compose validation time for the hybrid template.
- Remove the redundant minimal Compose file.
- Allow switching database providers without comparing their independent build dates.

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
