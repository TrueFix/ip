# Production and releases

## Release gate

This repository contains production hardening, not proof that an arbitrary
deployment is secure. Before public launch:

- Rotate every provider key previously disclosed in chat and use mounted `_FILE`
  secrets where possible. Keep real `.env`/databases out of Git and images.
- Review MaxMind, DB-IP, IPinfo and bundled-data terms for your public API.
- Run `make check`, `make audit`, a production-image build and the image smoke test.
- Configure TLS, a real certificate, allowed hosts, ingress trust, firewall,
  Cloudflare cache bypass and edge limits for your actual deployment.
- Scrape private metrics and load alert rules. Verify alert delivery, not just
  Prometheus configuration. Check readiness, updater freshness and source builds.
- Exercise a provider update, API restart and graceful shutdown in staging.
- Keep metrics private and configure proxy/Cloudflare logging retention.

Docker builds/startup could not be executed in the coding session because the
Docker daemon denied access. The tag workflow only builds/pushes images and
publishes releases; it does not run automated audits/tests. Run the retained
synthetic-data container smoke test manually before tagging. Local Go tests/vet/
audit do not replace actual Docker/ingress validation.

## Version control

Git is initialized on `main`. No identity, remote, commit, tag or push is created
automatically. The repository-local secret guard is enabled through
`core.hooksPath=.githooks`; other contributors should run `make hooks`.

Review the first commit carefully:

```sh
python3 scripts/check-secrets.py
git status --short
git add .
git diff --cached --stat
python3 scripts/check-secrets.py --staged
git commit -m "Initial myip service"
```

Set your Git author identity and remote explicitly if needed; do not use a
fabricated account. Before pushing, enable protected `main` and review requirements,
private vulnerability reporting, secret scanning and push protection on GitHub.
The local guard blocks common secret/data paths, even when force-added.

## Versioned builds

Use SemVer tags (`v0.1.0`, `v0.1.1`, etc.). Do not change API routes to `/v1`;
release versioning is separate from the agreed HTTP endpoint paths.

```sh
make build VERSION=v0.1.0
./bin/myip --version
./bin/updater --version
./bin/bench --version
```

Version, Git revision, build date and Go version are visible in `/docs`;
`myip_build_info` exposes bounded release labels to Prometheus. Default source
builds report `dev`/`unknown` until stamped. `make build` derives revision and
version from Git when available and stamps a UTC build date.

`make release VERSION=v0.1.0` produces one **API binary per OS/CPU target**, plus
SHA-256 checksums in ignored `dist/`. The checked-in target list is
`scripts/release-targets.txt`; both building and uploading use that manifest's
outputs so new targets cannot be silently omitted from release uploads.

| OS | Architectures |
| --- | --- |
| Linux | 386, amd64, ARMv5/v6/v7, arm64, loong64, mips, mipsle, mips64, mips64le, ppc64, ppc64le, riscv64, s390x |
| macOS (`darwin`) | amd64 (Intel), arm64 (Apple Silicon) |
| Windows | 386 (x86), amd64 (x64), arm64 |

Assets are named `myip-v0.1.0-linux-amd64`, `myip-v0.1.0-darwin-arm64`,
`myip-v0.1.0-windows-amd64.exe`, etc. No tar/zip archives or helper executables
are attached. All are built with CGO disabled. Linux binaries are static, so
separate Ubuntu/Debian/Fedora/Alpine/Arch executables are unnecessary; they still
require compatible CPU hardware and Go 1.27's supported OS/kernel versions.
Cross-compilation is not runtime testing on every distro. macOS/Windows binaries
use their normal OS system facilities and are not claimed to be fully static.
The binaries are not Developer ID-notarized or Authenticode-signed; those OSes
may display security prompts. Verify checksums and the download source.

Download the correct binary, verify its checksum, and `chmod +x` it on Unix.
No Go compiler or distro package manager is required to run it. It needs a
provisioned database snapshot; run the Docker updater or build the source updater
separately for provisioning and scheduled refreshes. Docker continues to publish
Linux amd64/arm64 images with all roles, independently of this raw-binary matrix.

The only workflow is `.github/workflows/publish.yml`. On each version-tag push:

1. Build one combined image for Linux amd64/arm64 and push
   `mayankfawkes/ip:<tag>` to Docker Hub. Stable tags also update `latest`;
   prerelease tags never replace it. Provenance/SBOM attestations are enabled.
2. Build the raw Linux/macOS/Windows API binaries/checksums and create a **GitHub Release**, attaching
   the files. Prerelease tags produce prereleases; reruns can replace the same
   tag's assets.

Configure repository **secrets** `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` with
push access to `mayankfawkes/ip`. GitHub Release upload uses the job-scoped
`GITHUB_TOKEN` with `contents: write`; no extra release token is needed.
Metadata comes automatically from the tag, SHA and commit timestamp, not `.env`.
No provider credentials are needed or passed to publishing builds.

Tag only a reviewed commit after running local checks. There is no separate
PR-test/audit workflow. Third-party Actions are SHA-pinned. To publish after
committing/pushing the reviewed source:

```sh
git tag v0.1.0
git push origin v0.1.0
```

## Docker deployment

Copy `.env.example` to an ignored `.env`, fill in the chosen provider settings,
and set `ALLOWED_HOSTS=ip.mynk.org`. Configure the exact ingress peer CIDRs and
sanitized headers; see `deploy/nginx.conf` and the README proxy section.

Download `compose.yml` and `.env.example` as shown in README.md; the server needs
only these configuration files, not a source checkout. Pin a published version
in `.env` with `IMAGE_TAG=v0.1.0`, then:

```sh
docker compose config --quiet
docker compose pull
docker compose up -d
docker compose logs -f updater
```

Both containers pull `mayankfawkes/ip` at that tag. The API uses the default
entrypoint; the updater uses `/usr/local/bin/updater` from the same image and
disables the API healthcheck. Release metadata is stamped by the tag workflow,
not runtime settings. Pin a release/image digest in your deployment inventory.

For source development only, build locally with
`docker build --target production -t mayankfawkes/ip:dev .`, then run
`IMAGE_TAG=dev docker compose up -d --pull never`. This is not needed on servers.

The API runs non-root, with a read-only filesystem/data mount, dropped
capabilities and bounded resources. Only the updater writes the persistent
database volume. Compose grants 30 seconds for clean shutdown; HTTP requests
are drained within the app's 15-second shutdown deadline.

The API's published port is loopback-only by default. Port 9090 is not published.
For a containerized ingress, attach it to a dedicated network with known proxy
addresses rather than trusting every private IP. Configure HTTPS separately.

## Validation and rollback

```sh
curl -f https://ip.mynk.org/healthz
curl -i https://ip.mynk.org/122.168.197.166
curl -f https://ip.mynk.org/docs
docker compose exec api bench -ip 122.168.197.166 -n 10000
```

Check local flags/docs links use the public HTTPS origin. Test direct-origin
requests and spoofed forwarding headers against your actual ingress. Verify
RPM `429` responses and `Retry-After` without flooding the public service.

Record the deployed commit/image digest, provider mode, schema and configuration
changes. For code rollback, redeploy the previously verified image/commit while
preserving the data volume; schema compatibility must be checked before rollback.
Do not use `docker compose down -v` as a normal upgrade/rollback step: it removes
the databases and causes cold-start downtime.

Database update failures preserve the last working snapshot and readers.
Unreferenced snapshots are cleaned after the documented grace period. An updater
crash releases the OS lock automatically; a leftover `.updater.lock` filename is
normal and must not be deleted to force another active writer to run. Avoid
indefinite database backups that violate provider deletion requirements.

Audit metadata is persisted in `/data/state.sqlite`; back it up only while the
updater is stopped, or use a proper SQLite backup tool. Do not copy a live SQLite
file blindly. It contains no visitor IPs or credentials, but is still excluded
from Git/images. The private `:9090/databases` endpoint exposes current inventory
and integrity status without opening SQLite in the API. `updater --status`
reads the latest 20 persisted attempts. Schema changes require backup/rollback
planning separately from MMDB snapshot compatibility.

Rate limits are process-local memory budgets. Restarting resets them; multiple
replicas do not share a global quota. Keep edge controls or introduce a shared
limiter if a globally enforced quota is required. SQLite audits are not a
distributed rate-limit implementation.
