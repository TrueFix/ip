# Local verification

Verified on 2026-11-04:

- `go test -race -cover ./...` — passed.
- `go vet ./...` — passed.
- `go build ./cmd/...` — passed.
- `docker compose config --quiet` — passed.
- Real DB-IP Lite City + ASN download, validation and publication — passed.
- Real MaxMind City + ASN download through official geoipupdate v8.0.0 — passed.
- A second MaxMind updater run using the existing snapshot — passed.
- Opt-in live-snapshot race tests for both providers — passed.
- HTTP smoke tests: IPv4/IPv6, readiness, private metrics, trusted-proxy caller
  detection, personalized-response cache headers, and response schema — passed.
- The example IPv6 resolved to Bhopal/MP, ASN 24560, Asia/Kolkata using the
  downloaded MaxMind snapshot. Values may change as upstream databases change.
- Hybrid MaxMind updater published a validated four-file snapshot with DB-IP
  fallback. `122.168.197.166` selected Gwalior/Madhya Pradesh with DB-IP's
  coordinates, preserved compatible `Asia/Kolkata`, and did not retain unrelated
  primary location data. `1.1.1.1` selected DB-IP's Sydney estimate with no inferred
  timezone. The original IPv6 kept its primary Bhopal/MP record.
- Hybrid responses included attribution for both sources and exposed fallback
  database build metrics. Unit tests cover whole-record selection, timezone
  conflicts, primary-city preservation and DB-IP-only isolation.
- Subdivision enrichment maps Madhya Pradesh to `MP` without an additional IP
  database download. Tests cover country scoping, case/whitespace/diacritic normalization,
  ambiguous names, administrative levels and preserving provider-supplied codes.
- Runtime configuration no longer includes base/docs URLs. Unit tests verify
  per-request HTTP/HTTPS/IPv6/port origins, trusted versus spoofed forwarded
  host/scheme, host allowlists and multiple hostnames on the same server.
- RPM is a strict rolling-minute budget, with optional RPS token-bucket limits.
  Tests cover expiration boundaries, ring growth/wrap, denied-request accounting,
  IPv6 aggregation and capacity limits.
- API and updater share an optional `DATA_DIR`, defaulting to `./data` locally
  and `/data` in Docker. A live update with the new two-setting credentials
  successfully populated the default local directory, and HTTP smoke tests
  verified request-specific URLs and strict RPM `429`/`Retry-After` behavior.
  Credential tests cover missing values, invalid/typoed
  providers, environment/file conflicts, configuration injection, private
  temporary files and cleanup. DB-IP does not read MaxMind secrets.
- Optional IPinfo integration tests use **synthetic** MMDB records, covering
  matching/mismatching ASNs, domain validation, optional operator-as-ISP naming,
  and all-source timing probes without changing the chosen location. Download
  errors are checked to ensure tokens never appear in returned errors.
- The official IPinfo Lite **sample** schema can be validated with
  `MYIP_TEST_IPINFO_FILE=/path/to/sample.mmdb go test ./internal/geo -run TestLiveIPInfoSample`.
  A sample/synthetic fixture is not a full-database performance result.
- The full IPinfo Lite database was subsequently downloaded and validated with
  a locally supplied token. All-three-source measurements are listed below;
  the benchmark still refuses to mislabel an incomplete two-source snapshot.
- Git tracking guards and their unit tests pass. Release metadata is stamped in
  build outputs, version commands, `/docs` and Prometheus. Regression tests cover
  local metrics privacy, updater writer exclusion, missing fallback repair and
  snapshot-name validation. A manual non-root/read-only container smoke test is available.
- Module/import/linker paths now use `github.com/TrueFix/myip`. Tests, vet,
  vulnerability checks and CGO-disabled API/updater builds pass after the rename.
- Two real five-file updates populated `data/state.sqlite`, with prepared/success
  audit records, source build/check/download times, byte sizes, validators and
  64-character SHA-256 values in both SQLite and the active manifest. The repeat
  run reported MaxMind unchanged and DB-IP/IPinfo `not_modified` (HTTP 304).
- Live API startup verified all five checksums. Private `/databases`, the
  read-only `updater --status` CLI, per-file freshness metrics and the loaded vs
  published snapshot gauge passed. No SQLite access was added to public lookups.
- Regression tests cover conditional 304 reuse, same-month correction fetching,
  corrupt-cache repair, loaded-file corruption causing `503`, restoration,
  missing/invalid inventories, audit crash recovery, missing audit-history
  rejection, bounded retention preserving the active snapshot, and process-local
  rate-limit metrics. Tests do not assert publisher-signature authenticity.
- The response schema no longer contains `postal`; serialization tests enforce
  its absence even when source databases contain a postal code.
- There is one workflow, `publish.yml`, with Docker Hub publishing and GitHub
  Release jobs. Its syntax passes actionlint. Local release builds succeeded for
  Linux amd64/arm64/ARMv7/386/RISC-V 64; archive checksums passed. These are
  cross-compilation checks, not runtime coverage of every distribution/CPU.

Warm baseline for `122.168.197.166` on this development machine, 10,000 measured
iterations after 1,000 warm-ups, **MaxMind + DB-IP only**, normal lookup path:

| Stage | Mean µs | p50 µs | p95 µs | p99 µs |
| --- | ---: | ---: | ---: | ---: |
| MaxMind | 3.36 | 3.15 | 4.24 | 7.51 |
| DB-IP | 3.96 | 3.65 | 5.15 | 8.23 |
| Combined lookup + enrichment | 23.15 | 21.92 | 29.49 | 49.95 |

Full **MaxMind + DB-IP + IPinfo Lite** snapshot, same IP and iteration counts,
with forced probes of all providers (including both City/ASN pairs):

| Stage | Mean µs | p50 µs | p95 µs | p99 µs |
| --- | ---: | ---: | ---: | ---: |
| MaxMind | 3.39 | 3.16 | 4.18 | 7.37 |
| DB-IP | 4.42 | 4.06 | 5.82 | 8.85 |
| IPinfo | 0.53 | 0.51 | 0.68 | 0.95 |
| Combined lookup + enrichment | 24.81 | 23.45 | 31.57 | 52.76 |

These measurements exclude startup/download time, JSON, HTTP/proxy overhead and
network latency. They do not predict mixed-IP capacity on production hardware.

**Not verified locally:** Docker image builds and container startup. Docker
daemon access was denied for this session. Base image tags resolve in the
registry and the pinned geoipupdate Go module cross-builds locally. The tag
workflow is configured to build/push `mayankfawkes/ip` and create a GitHub Release;
neither remote operation has been executed from this session. Run these checks
on a Docker-capable host before launch:

```sh
docker build --target production -t mayankfawkes/ip:dev .
SMOKE_IMAGE=mayankfawkes/ip:dev python3 scripts/smoke-container.py
IMAGE_TAG=dev docker compose up -d --pull never
docker compose logs updater
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/8.8.8.8
```

Also verify the real deployment's TLS, proxy CIDRs, Cloudflare cache bypass,
firewall, data-license requirements and Prometheus alerts. Local HTTP tests do
not validate your ingress configuration.

Credentials used for local testing are kept in ignored `.env` (mode 0600) and
the legacy ignored `secrets/GeoIP.conf`. That directory/file use host-user
permissions, not container UID 65532 ownership.
The file remains usable through the explicit legacy `GEOIPUPDATE_CONFIG_FILE`
option. Prefer the new two-setting credentials or `_FILE` secret configuration
described in README.md for deployment. Revoke the chat-exposed key after testing
and replace it before deployment. Test databases can be in temporary storage or
the ignored local `data/` directory; neither is part of the published image.
