# Production and releases

For installation, use [DEPLOYMENT.md](../DEPLOYMENT.md). For variables, use
[ENVIRONMENT.md](../docs/ENVIRONMENT.md). The hardened [root Compose](../compose.yml)
adds resource limits, dropped capabilities and mounted-secret support.

## Before public deployment

- Use a pinned image tag or digest for both API and updater.
- Configure HTTPS, a firewall and an ingress that sanitizes forwarded headers.
  Trust only the actual proxy CIDRs; optionally restrict `ALLOWED_HOSTS`.
- Keep port **9090 private**; it has no authentication. Bypass CDN caching for
  API responses. Apply edge protection even when application limits are enabled.
  Verify aggregate/prefix admission limits: rotating IPv6 /64s can exhaust the
  app's client table. The Nginx example includes optional aggregate limiting.
- Supply provider credentials only to the updater. Prefer mounted `_FILE`
  secrets; never commit `.env`, credentials or database snapshots. Rotate any
  disclosed key. See [secret permissions](../docs/REFERENCE.md#maxmind-default-provider).
- Run one updater per data volume. Use a local filesystem with working locks;
  distributed filesystems require separate verification.
- Review [data licenses](../THIRD_PARTY.md), including attribution, refresh and
  deletion obligations. An available download is not blanket redistribution permission.
- Run the [checks and container smoke test](../TESTING.md). Exercise a real
  update, restart and graceful shutdown in staging.
- Configure private Prometheus scraping and load `deploy/alerts.yml`. Verify
  alert delivery and updater freshness, not just API readiness.
- Set proxy error-log access/retention/redaction; disabling access logs alone
  does not prevent visitor IPs and request URLs from entering error logs.

## Validate the deployment

```sh
docker compose config --quiet
docker compose pull
docker compose up -d
docker compose logs --tail=50 updater
curl -f http://127.0.0.1:8080/healthz
curl -f http://127.0.0.1:8080/8.8.8.8
docker compose exec updater updater --status
```

Also test your public HTTPS origin: flag/docs URLs, spoofed forwarding headers,
direct-origin restrictions and `429`/`Retry-After` behavior. Do not load-test a
public endpoint without authorization.

## Rate limiting

| Variable | Default | Effect |
| --- | --- | --- |
| `RATELIMIT_ENABLE` | `true` | `false` bypasses all built-in limits and client tracking |
| `RATE_LIMIT_RPM` | `300` | Rolling-minute budget; `0` rejects rate-limited requests when enabled |

Changes to `.env` need `docker compose up -d`, not just a container restart.
Limits are per-process memory budgets, reset on restart and are not shared
between replicas. Edge limits remain independent. See the
[complete settings](../docs/ENVIRONMENT.md#5-rate-limiting--no-authentication).

## Updates and rollback

Keep the same deployment directory and data volume. Do **not** use
`docker compose down -v` for an upgrade: it deletes the databases.

Record the deployed image digest, provider and configuration. To roll back code,
set both Compose `image:` entries to the previously verified release and run
`docker compose up -d`. Check SQLite/manifest compatibility before downgrading.

Failed updates retain the previous published snapshot. A leftover updater lock
filename is normal; never delete it to bypass an active writer. Back up
`state.sqlite` with the updater stopped or through a SQLite-aware backup tool.
Do not copy a live SQLite file blindly or retain provider data indefinitely.

## Release checklist

1. Review code, dependencies, licenses and the changelog. Enable branch
   protection, private vulnerability reporting and secret scanning in GitHub.
2. Run `make check`, `make audit`, the image smoke test and workflow actionlint.
   Push the reviewed commit to `main` and wait for the verification job to pass
   before tagging. Main/manual runs verify only; they do not publish artifacts.
3. Cross-build/checksum all targets with `make release VERSION=v1.0.0`.
4. Commit the reviewed changes, create the SemVer tag, and push atomically:

   ```sh
   git tag -a v1.0.0 -m 'Release v1.0.0'
   git push --atomic origin main refs/tags/v1.0.0
   ```

5. Verify both publishing jobs, Docker tags, GitHub assets and checksums. Test
   the published image before promoting it to your deployment.

The single [workflow](../.github/workflows/publish.yml) verifies main pushes,
manual dispatches and release tags. Both publishing jobs depend on successful
tests, audits and a production-image smoke test, and run only on tag pushes.
Docker publishing needs repository secrets `DOCKERHUB_USERNAME` and
`DOCKERHUB_TOKEN`; release uploads use `GITHUB_TOKEN`. Provider credentials are
not build inputs. There is no separate pull-request workflow.
Tag releases are serialized; only the numerically newest known stable Git tag
can promote Docker `latest`. Older maintenance tags/reruns and prereleases do
not move it backwards. Third-party Actions are SHA-pinned.

## Release artifacts

| Artifact | Platforms | Notes |
| --- | --- | --- |
| `mayankfawkes/ip` image | Linux amd64/arm64 | Same image for API, updater and benchmark commands |
| Raw API binaries | 15 Linux variants, macOS Intel/ARM64, Windows x86/x64/ARM64 | One executable per target plus SHA-256 checksums; no archives/helpers |

The [target manifest](../scripts/release-targets.txt) is authoritative. Linux
binaries are CGO-disabled/static and do not need distro-specific libc packages,
but still need compatible hardware and supported kernels. macOS/Windows builds
are not notarized/Authenticode-signed. Cross-builds are not native runtime tests.
Raw API binaries need a provisioned snapshot and a separate updater process;
native MaxMind updates also require the official `geoipupdate` executable.

Version metadata is available through `--version`, `/docs` and
`myip_build_info`. Build locally with `make build VERSION=v1.0.0`.
