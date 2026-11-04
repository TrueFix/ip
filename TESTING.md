# Testing

Requires the Go version in `go.mod`, Python 3, and Docker Compose v2.
Tests use synthetic or upstream test fixtures, not production credentials.

## Required checks

```sh
make check
make audit
go build ./cmd/...
go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/publish.yml
git diff --check
```

`make check` runs the secret/data tracking guard, Go race tests, `go vet`, Python
tests and Compose validation. `make audit` scans both the application and the
separately built official `geoipupdate` command using the pinned vulnerability checker.
Compose validation does not require a running daemon; container checks do.

## Container smoke test

```sh
docker build --target production -t mayankfawkes/ip:dev .
SMOKE_IMAGE=mayankfawkes/ip:dev python3 scripts/smoke-container.py
```

The smoke test uses synthetic databases to check the non-root/read-only image,
readiness, lookups and private monitoring. Test real provider downloads separately
with private credentials. Do not publish logs containing credentials.

## Release binaries

```sh
make release VERSION=v1.0.0
(cd dist && sha256sum -c SHA256SUMS-v1.0.0.txt)
./dist/myip-v1.0.0-linux-amd64 --version
```

The [target manifest](scripts/release-targets.txt) defines 20 raw API executables.
Cross-compilation and checksums do not establish native runtime support for
every OS/CPU. Test on target systems before claiming native compatibility.

## Optional local snapshot tests

These read an existing snapshot without fetching provider data:

```sh
MYIP_TEST_DATA_DIR=/path/to/data MYIP_TEST_PROVIDER=maxmind \
  go test -race ./internal/geo -run TestLiveSnapshot -v
MYIP_TEST_IPINFO_FILE=/path/to/IPinfo.mmdb \
  go test ./internal/geo -run TestLiveIPInfoSample -v
```

## Coverage and limits

| Area | What tests exercise |
| --- | --- |
| Geo data | Whole-record fallback, ASN agreement, region codes, timezones, null fields and attribution |
| HTTP | IPv4/IPv6, request-origin URLs, spoofed proxy headers, readiness and private metrics |
| Rate limiting | Rolling RPM/RPS, explicit disable switch, zero budget, IPv6 /64 grouping and bounded clients |
| Updates | Conditional downloads, corrupt-cache repair, atomic publication, locking and failure preservation |
| Integrity/audits | SHA-256 inventory, reload failures, audit recovery and bounded history |
| Deployment | Required credentials, credential isolation, environment forwarding and release targets |

Local tests do not validate production TLS, ingress trust, firewall rules,
provider licensing, alert delivery or distributed-filesystem locking. Validate
those in staging using the [production checklist](deploy/PRODUCTION.md).

## Benchmarking

```sh
go run ./cmd/bench -ip 8.8.8.8 -n 10000 -all-sources=false
go run ./cmd/bench -ip 1.1.1.1 -n 10000 -all-sources=false
go run ./cmd/bench -ip 2401:4900:1ca2:48ff:5516:e857:6908:1227 -n 10000 -all-sources=false
go test ./internal/geo -run '^$' -bench '^BenchmarkEnrichMixed' -benchmem -count=5 -cpu=4
go test ./internal/httpapi -run '^$' -bench '^BenchmarkLimiterParallel' -benchmem -count=3 -cpu=4
```

Omit `-all-sources=false` to force probes of all three providers; that mode needs
a MaxMind + DB-IP + IPinfo snapshot. Results are warm, serial, single-IP lookup
timings, not HTTP throughput or internet latency. Do not benchmark with `-race`.

README lookup measurements used Linux amd64, Ryzen 7 5800XT (16 logical CPUs),
Go 1.27.3, the locally downloaded MaxMind/DB-IP/IPinfo snapshot, default 1,000
warm-ups and three runs per IP. Reported p50/p95/p99 are the median of each run's
percentile. No provider downloads occur during benchmarking. Data coverage and
machine load affect results; repeat on deployment hardware.

Enrichment-only results used five runs at `-cpu=4`, comparing the previous model
with precomputed borders/flags and a 512-entry timezone cache. Parallel benchmark
ns/op measures aggregate operation throughput, not request latency. Limiter
microbenchmarks include synthetic clocks and rejection paths; do not present
them as accepted HTTP-request capacity. Capacity/reload claims require a separate
mixed-IP HTTP load test with successful responses, 429s and memory tracked separately.
