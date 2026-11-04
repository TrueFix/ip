# myip · Reference

A self-hosted IP information API, built with Go's standard
`net/http` server and `http.ServeMux`. No web framework, external per-request
geolocation API, external database server, or Redis required. Embedded SQLite
stores updater audits and file inventories; IP lookups still use local MMDBs.

## Endpoints

| Endpoint | Description |
| --- | --- |
| `GET /` | Information about the caller's public IP |
| `GET /<ip>` | Lookup an IPv4 or IPv6 address |
| `GET /healthz` | Readiness; `503` until a valid database pair is loaded |
| `GET /metrics` | Prometheus metrics on **port 9090 only** |
| `GET /databases` | File inventory/integrity/update status on **port 9090 only** |
| `GET /docs` | JSON documentation and source attribution |
| `GET /flags/in.svg` | Bundled country flags (lowercase ISO alpha-2) |
| `GET /country-data.json` | Unmodified country metadata snapshot and ODbL access |

The public API listens on port 8080. `/metrics` on that port returns `404`;
scrape `http://api:9090/metrics` over the private Docker network instead. There
is no HTML frontend. `HEAD` and CORS preflight are supported.

Release/deployment checklist: [deploy/PRODUCTION.md](../deploy/PRODUCTION.md).
Contribution and security policies: [CONTRIBUTING.md](../CONTRIBUTING.md),
[SECURITY.md](../SECURITY.md). Git is initialized on `main`; real credentials and
database snapshots are excluded. `make hooks` enables the repository-local
staged-secret guard on a new clone.

## Quick start: no account required

Requirements: Docker Engine and Docker Compose v2.

The standard `compose.yml` pulls **`mayankfawkes/ip`** from Docker Hub; there is
no local image build or source checkout requirement. API and updater share the
same image/tag; the updater overrides its entrypoint and disables the API-only
healthcheck. Docker Hub images support Linux amd64 and arm64. Set optional
both Compose `image:` entries to a release tag to pin the containers. The README has
download commands for the two small configuration files needed on a fresh host.

For running without Docker, GitHub Releases contain raw API executables for Linux,
macOS and Windows. The [release target list](../scripts/release-targets.txt) is
authoritative; Windows downloads use `.exe`. No archives/helpers are bundled.
Linux builds are static and not tied to a distribution's libc. Choose a binary
for your CPU and a supported OS/kernel; provision the local MMDB snapshot before
starting it. See the [deployment guide](../deploy/PRODUCTION.md) for limitations.

```sh
cp .env.example .env
mkdir -p secrets
# Set GEOIP_PROVIDER=dbip in .env.
docker compose pull
docker compose up -d
docker compose logs -f updater
```

The first download may take several minutes. Until it finishes and the API's
30-second reload loop sees it, readiness and lookups return `503`.

```sh
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/8.8.8.8
curl 'http://127.0.0.1:8080/2401:4900:1ca2:48ff:5516:e857:6908:1227'
```

`curl /` from localhost has a non-public source address and deliberately returns
`400`; use an explicit public-IP lookup for local testing. A host/port mapping
does not automatically establish a trusted proxy.

## MaxMind (default provider)

1. Create a MaxMind account and a **new** license key. Revoke any key exposed in
   chat, logs, or source control.
2. Copy `.env.example` to `.env` and set the three settings below locally:

   ```dotenv
   GEOIP_PROVIDER=maxmind
   MAXMIND_ACCOUNT_ID=YOUR_ACCOUNT_ID
   MAXMIND_LICENSE_KEY=YOUR_NEW_LICENSE_KEY
   ```

3. Run `docker compose pull && docker compose up -d`.

**Fail-fast validation:** `maxmind` requires both credentials; a missing/invalid
account ID or license key exits the updater with a clear error before any
download. Unknown providers (including typos such as `madmin`) are rejected.
`dbip` does not require or read MaxMind credentials. The API itself deliberately
does not require credentials: it only reads locally published databases.

For production, prefer `MAXMIND_ACCOUNT_ID_FILE` and `MAXMIND_LICENSE_KEY_FILE`
pointing to mounted secret files instead of environment values. In Compose,
files under `./secrets/` are mounted at `/run/secrets/` **only in the updater**:

```dotenv
GEOIP_PROVIDER=maxmind
MAXMIND_ACCOUNT_ID_FILE=/run/secrets/maxmind_account_id
MAXMIND_LICENSE_KEY_FILE=/run/secrets/maxmind_license_key
```

Each file contains just its value (a trailing newline is allowed). Do not set a
value and its `_FILE` together. Ensure directory traversal and read permissions
for container UID/GID **65532:65532**, and restrict other host users. For example:

```sh
sudo chown 65532:65532 secrets secrets/maxmind_account_id secrets/maxmind_license_key
sudo chmod 0700 secrets
sudo chmod 0400 secrets/maxmind_account_id secrets/maxmind_license_key
```

Only the updater receives credential variables/mounts. It creates a mode-0600
temporary geoipupdate configuration outside the data directory and removes it
after each attempt. No credentials are placed in arguments, snapshots or images.
`.env.example` is version-controlled; `.env` and `secrets/` are ignored by Git
and excluded from Docker builds. Do not use the formerly exposed license key.

For existing deployments, explicitly setting `GEOIPUPDATE_CONFIG_FILE` still
accepts a legacy GeoIP.conf containing both credentials. Do not combine that
setting with the new credential settings. There is no implicit config-file path.

The updater uses the official **geoipupdate** executable. It seeds staging with
the previous files so geoipupdate can avoid downloading unchanged databases.
DB-IP publishes monthly HTTPS gzip downloads without credentials. Every scheduled
run checks its current-month objects (including same-month corrections), and
IPinfo's current object, using ETag/Last-Modified conditional requests where the
provider supplies them. `304` reuses a locally checksum-verified file. Missing
validators cause a full download; missing/corrupt cached files also force a
download. The first hybrid update downloads both sources.

To change providers, update `.env` and recreate **both** containers:

```sh
docker compose up -d --force-recreate
```

The new API stays unready until a valid snapshot for the selected mode is
published. Existing legacy MaxMind-only snapshots remain readable during an
upgrade; fallback becomes available after the new updater publishes a hybrid
snapshot. Restart/rebuild the API as well as running the updated updater.

### Provider modes

- **`GEOIP_PROVIDER=maxmind`: hybrid.** MaxMind is primary. If it has no city and
  DB-IP has one, select DB-IP's entire location record, including coordinates,
  region and country data. If MaxMind has no country, DB-IP can also
  recover a country-only result. Missing MaxMind ASN records can fall back to
  DB-IP's entire ASN record. A populated MaxMind city always wins.
- **`GEOIP_PROVIDER=dbip`: DB-IP only.** No MaxMind downloads, credentials,
  lookups or enrichment are used, even if old MaxMind files exist on the volume.

Do not mix a fallback city with unrelated primary coordinates. The only
cross-source location enrichment is preserving a primary timezone when the
country matches and the pinned IANA `zone.tab` lists that exact timezone as the
country's sole zone. Thus India can retain `Asia/Kolkata`; an uncertain US
fallback does not retain `America/Chicago`. Unknown/aliased zones fail closed.
This does not derive a timezone for DB-IP-only lookups.

Hybrid responses credit **both MaxMind and DB-IP** in attribution headers.
Downstream pages displaying hybrid results must include DB-IP's required link.
More populated data is not a guarantee of greater location accuracy.

### Optional third source: IPinfo Lite

IPinfo Lite is free, but its full database requires a free account/download
token: https://ipinfo.io/lite . Keep the token in `.env` or a mounted secret,
never paste it into source code or chat:

```dotenv
GEOIP_PROVIDER=maxmind
IPINFO_ENABLED=true
IPINFO_TOKEN=YOUR_FREE_IPINFO_TOKEN
# Or IPINFO_TOKEN_FILE=/run/secrets/ipinfo_token, not both.
ISP_FROM_ASN=false
```

The updater downloads `ipinfo_lite.mmdb` and validates/publishes it in the same
atomic snapshot as MaxMind and DB-IP. A missing IPinfo token fails fast when
enabled. A failed download/validation preserves the entire previous snapshot.
IPinfo is refreshed on each successful scheduled update; disabling it removes
it from the next published snapshot. API readers need no token and make no
external requests during lookups. `dbip` mode never loads or downloads IPinfo.

IPinfo supplements `connection.domain` when its ASN agrees with the selected
ASN. Conflicting networks are never merged. If all primary/fallback ASN data
is missing, an IPinfo ASN and organization may supply it. Existing organization
names are preserved. Invalid/non-hostname domains stay `null`.

IPinfo Lite does **not** provide an independently verified retail ISP name.
Keep `ISP_FROM_ASN=false` for honest nullable ISP data, or explicitly set it to
`true` to use **IPinfo's network operator name** for `connection.isp`. This is an
approximation, not a guarantee of the subscriber's retail provider. Missing
names/domains still remain `null`; nothing is fabricated. This option has no
effect in DB-IP-only mode.

Responses using IPinfo data include its attribution Link. IPinfo Lite's
CC BY-SA 4.0 terms are separate from the application license; review attribution
and share-alike obligations for your public service. No IPinfo database is
bundled in source or images.

## Response contract

Successful responses use an ipwho.is-style layout, with postal codes intentionally omitted:

```json
{
  "ip": "8.8.8.8",
  "success": true,
  "type": "IPv4",
  "continent": "North America",
  "continent_code": "NA",
  "country": "United States",
  "country_code": "US",
  "region": "California",
  "region_code": "CA",
  "city": "Mountain View",
  "latitude": 37.422,
  "longitude": -122.085,
  "is_eu": false,
  "calling_code": "1",
  "capital": "Washington D.C.",
  "borders": "CA,MX",
  "flag": {
    "img": "https://ip.mynk.org/flags/us.svg",
    "emoji": "🇺🇸",
    "emoji_unicode": "U+1F1FA U+1F1F8"
  },
  "connection": {
    "asn": 15169,
    "org": "Google LLC",
    "isp": null,
    "domain": null
  },
  "timezone": {
    "id": null,
    "abbr": null,
    "is_dst": null,
    "offset": null,
    "utc": null
  },
  "readme": "https://ip.mynk.org/docs"
}
```

This example was observed using DB-IP Lite; locations change with database
updates. Values are **not** expected to match another provider exactly.

| Data | GeoLite2 | DB-IP Lite |
| --- | --- | --- |
| Continent/country/city/approximate coordinates | Where available | Where available |
| Region code/timezone ID | Where available | Not in current Lite data |
| ASN and ASN organization | Where available | Where available |
| Retail ISP and organization domain | `null` | `null` |
| Capital/borders/calling code/flags/EU membership | Bundled metadata | Bundled metadata |

The table describes the two base datasets. Optional IPinfo enrichment adds AS
domains and, only when explicitly enabled, network-operator ISP approximations.
Postal codes are intentionally not part of the response schema.

Missing values are `null`. A known country with no land borders returns `""`.
When a provider supplies a region name but no code, a bundled ISO 3166-2
first-level subdivision table supplies the suffix where the country/name pair
matches unambiguously: India + Madhya Pradesh → `MP`, US + California → `CA`.
Matching ignores case, repeated whitespace and diacritics, not spelling differences.
Provider-supplied codes are preserved; unmatched/ambiguous names remain `null`.
This metadata enrichment applies to both hybrid and DB-IP-only modes without
network requests. The pinned dataset can be regenerated with
`python3 scripts/update-subdivisions.py`.
Calling codes are strings without `+`; for shared numbering plans such as NANP,
the response uses the shared root rather than listing area codes. Territories
are evaluated by their own country code for EU membership.

Timezone offsets, abbreviation and DST are calculated **at request time** using
the embedded IANA timezone database; no timezone is guessed from country alone.
An ASN owner is not necessarily the user's retail ISP. Coordinates are estimates,
not GPS, and cannot establish a user's physical address. VPN/mobile traffic may
resolve to the network exit rather than the user's city.

Anycast addresses can have no geographic data at all. For example, the tested
GeoLite2 record for `1.1.1.1` contains only `registered_country: AU`, not an
actual location. The API intentionally does not substitute registration country
for location. Hybrid mode can select DB-IP's estimated location instead, but
that is still a provider estimate, not the location of every anycast DNS node.
`success: true` means a record was found, not that every field is populated.

Errors use `{"success":false,"message":"..."}`:

| Status | Meaning |
| --- | --- |
| `400` | Invalid IP/proxy header, or non-public/reserved IP |
| `404` | No record, unknown endpoint or unavailable flag |
| `405` | Unsupported HTTP method |
| `429` | Rate limit or client-table capacity reached; see `Retry-After` |
| `500` | Unexpected lookup error |
| `503` | Required databases not loaded |

## Proxy trust and production exposure

**Default: trust no forwarded headers.** The socket peer is the caller unless
it matches an explicitly configured `TRUSTED_PROXIES` CIDR.

```ini
TRUSTED_PROXIES=172.30.0.2/32
PROXY_HEADER=X-Forwarded-For
```

Use the actual peer IP seen by the API, not an arbitrary private network. Never
trust `0.0.0.0/0` or `::/0`. Supported headers are `X-Forwarded-For`, `X-Real-IP`,
and `CF-Connecting-IP`. Choose **one**; the application does not guess between them.

- **Direct:** leave `TRUSTED_PROXIES` empty. Bind the Compose host port to `0.0.0.0` only if
  intentional; terminate HTTPS with your chosen ingress/load balancer. The app
  itself serves HTTP, not TLS.
- **Nginx → app:** overwrite inbound forwarding headers; see `deploy/nginx.conf`.
  Trust only Nginx's exact peer address. The rightmost untrusted address in an
  XFF chain is selected; attacker-provided addresses to its left are ignored.
- **Cloudflare → app:** use `CF-Connecting-IP` and trust only current Cloudflare
  ranges. Restrict origin ingress to Cloudflare and protect/terminate origin TLS.
  Official ranges: https://www.cloudflare.com/ips/ . Keep them updated; there is
  deliberately no hidden network fetch or stale built-in Cloudflare range list.
- **Cloudflare → Nginx → app:** Nginx must validate Cloudflare's source with its
  real-IP module, then forward the sanitized address. Configure the app for
   Nginx, not for Cloudflare headers arriving through an otherwise trusted proxy.

With a host Nginx and Docker's published port, the peer may be the Docker bridge
gateway. Other containers on that bridge must not be able to impersonate the
trusted ingress. Prefer a dedicated network and fixed proxy address for a
containerized ingress.

### Automatic response URLs

No base URL or documentation URL configuration is needed. `readme` and flag
links follow the current request's host, port and scheme:

- Local HTTP: `http://127.0.0.1:8080/docs`.
- Production HTTPS: `https://ip.mynk.org/docs` (or any other served hostname).
- Behind a trusted proxy: single, sanitized `X-Forwarded-Host` and
  `X-Forwarded-Proto` are accepted. Untrusted peers cannot override origin.

Configure **every trusted ingress to overwrite/remove client-supplied origin
headers**, not just IP headers. Nginx's example sets both origin headers. For
Cloudflare → Nginx, use Full (strict) origin TLS so Nginx's `$scheme` is correct.
Cloudflare alone may pass arbitrary client headers: configure it to remove or
overwrite `X-Forwarded-Host`, and set an `ALLOWED_HOSTS` allowlist in production.
The application does not interpret arbitrary forwarding chains for origin:
multiple/comma-separated values are rejected. Ingress should provide one
sanitized external authority and scheme.

`ALLOWED_HOSTS=ip.mynk.org` optionally restricts response origins to that exact
host. Include ports when nonstandard, for example `localhost:8080`. This is an
allowlist, **not** a forced base URL. With no list, valid request hosts are
accepted. Invalid hosts/schemes are rejected; no request-specific URL is cached.
Health and private metrics remain available without a configured public Host.

All API responses default to `Cache-Control: no-store`; **create a Cloudflare
cache-bypass rule for the API hostname** and do not enable "cache everything".
Only bundled flag SVGs receive public cache headers. Limit DDoS/bot traffic at
the ingress: in-process limiting is not a substitute for edge protection.

## Configuration

Settings are documented in separate tables by source and purpose in
[Environment variables](ENVIRONMENT.md), including requirements, defaults and
which Compose templates forward them. Most are optional, not a setup checklist.

| Group | Authentication | Settings and examples |
| --- | --- | --- |
| DB-IP | None | [DB-IP table](ENVIRONMENT.md#1-db-ip--no-authentication) |
| MaxMind + DB-IP fallback | MaxMind account ID + license key | [MaxMind table](ENVIRONMENT.md#2-maxmind--authentication-required) |
| Optional IPinfo Lite | IPinfo token, plus existing MaxMind credentials | [IPinfo table and ISP switch examples](ENVIRONMENT.md#3-ipinfo-lite--token-required) |
| Shared database settings | No additional authentication | [Update interval and storage](ENVIRONMENT.md#4-shared-database-settings--no-authentication) |
| Rate limiting | None | [Limits and how to disable them](ENVIRONMENT.md#5-rate-limiting--no-authentication) |
| Proxy, hostname and browser settings | These do not add authentication | [Security settings](ENVIRONMENT.md#6-proxy-hostname-and-browser-settings--no-authentication) |
| Process listeners | No built-in authentication; monitoring must stay private | [Listener settings](ENVIRONMENT.md#7-process-listeners--no-authentication) |

Both programs default to the same `./data` directory relative to their working
directory. No `DATA_DIR` is necessary when running them from the project root.
Docker images set `/data` as their default. Compose uses a persistent named
volume there. Edit Compose volume mounts directly if you need a host bind mount;
the non-root updater must have write permission. Edit Compose `ports:` directly
to change the host bind address/port, not the process listener.

Set `RATE_LIMIT_RPM=0` to bypass RPM, RPS and client-capacity limits entirely,
without tracking client buckets. Other rate-limit values still must be valid,
but have no limiting effect while RPM is zero.

RPM and RPS are cumulative restrictions when both are set. For example,
`RATE_LIMIT_RPM=60`, `RATE_LIMIT_RPS=2`, `RATE_LIMIT_BURST=5` allows at most 60
accepted requests in any rolling minute, plus token-bucket smoothing at 2/sec
with a burst of 5. With RPS unset, only the rolling-minute budget applies;
`RATE_LIMIT_BURST` does not restrict RPM-only bursts. `Retry-After` reflects the
active limit. Denied requests do not consume the minute budget. Timestamp rings
and the client table are bounded; IPv6 address rotation within a /64 shares a
budget. Limits apply across lookup/flag/docs routes, not individually per route.

IPv6 rate-limit keys are aggregated by `/64`. Idle keys expire after ten
minutes; once the bounded table fills, new clients receive `429`. Limits are
per API process, not globally coordinated across replicas. API readiness,
metrics and CORS preflight are exempt. Compose exposes only port 8080 on
localhost by default; port 9090 is reachable only on the container network.

**Rate limits are intentionally in memory**, not SQLite. Restarting a process
resets its budgets; N replicas can admit roughly N times the configured budget.
Use a shared edge policy (Cloudflare/Nginx) or a dedicated shared limiter such as
Redis if you need a global quota. A per-request SQLite write would add contention
and persistent IP storage without solving quotas across separate replicas.
Prometheus exposes current bucket count and rejections by `rpm`, `rps` and
`capacity`, with no client-IP labels. No rate-limit IPs are written to SQLite.

## Updates, readiness and recovery

Exactly **one updater** writes each data volume. API replicas mount it read-only.
An OS-backed `.updater.lock` rejects overlapping writers; it releases on process
exit. A leftover lock filename is normal. Local filesystem locking is required;
verify distributed-filesystem semantics separately. API reloads are serialized
so concurrent reload attempts cannot publish snapshots out of order.

1. Download into a fresh staging directory.
2. Verify the expected database types and MMDB integrity for all files (four in
   hybrid mode, five with IPinfo; two in DB-IP-only mode), and calculate SHA-256
   and byte sizes. Reject backwards build timestamps relative to known inventory.
3. Flush the files and publish a versioned snapshot.
4. Atomically replace `current.json` only after validation succeeds.
5. APIs poll the manifest and replace readers under a read/write lock.

### SQLite audit and checksum inventory

The updater automatically creates `DATA_DIR/state.sqlite` (pure-Go SQLite;
CGO is not required). Schema migrations use `PRAGMA user_version`. The database
uses a single writer, DELETE journaling, FULL synchronization, a busy timeout and
foreign keys; it is not a network service or a request-rate-limit database.
The API never opens it, so its existing read-only data mount remains sufficient.

Tables `update_runs` and `database_files` retain the newest **1,000 attempts**
(plus the currently published attempt if older),
including provider, outcome, snapshot/audit ID, SHA-256, byte size, build epoch,
last known download time, check/verification times, ETag and Last-Modified.
Only query-free source references are stored; no tokens, visitor IPs, lookups or
raw upstream error bodies are persisted. The manifest's `files` inventory and
`audit_run_id` link the served snapshot to that audit record.

An audit run is marked prepared before manifest publication and successful
after publication. After a crash, the next updater reconciles prepared/running
attempts against the current manifest: a matching published snapshot succeeds;
unpublished attempts become interrupted. The manifest remains serving truth;
SQLite and a filesystem rename are not one cross-system transaction. Unavailable,
corrupt or missing previously published audit history does not stop loaded API
readers, but stops the updater until repaired/restored. A missing database for a
legacy snapshot without an audit ID is automatically initialized on migration.

Inspect without credentials or an extra SQLite CLI installation:

```sh
go run ./cmd/updater --status
# Or read private status (never publish this port):
curl http://127.0.0.1:9090/databases
```

Docker: `docker compose exec updater updater --status`. Source `downloaded_at`
and `checked_at` are different: `304` preserves the download time but advances
the check time. A fresh `200` records a download even if content is identical.
MaxMind's official updater does not expose its HTTP validators, so unchanged
bytes preserve the last known download time. A legacy file's unknown historical
download time stays zero/empty rather than being invented; `imported` marks it.

Checksums are verified on new snapshot loads and every **six hours** for loaded
snapshots. A checksum failure in loaded files makes readiness/lookups return
`503` until a valid replacement or restored files pass verification. A rejected
new snapshot leaves the previous valid readers active. Legacy snapshots without
inventories still load for migration, but report `myip_database_integrity_ok=0`
until the new updater publishes a fully inventoried snapshot.

**This is integrity checking, not publisher-signature verification.** SHA-256
detects changes relative to the protected local manifest; an attacker who can
rewrite both files and manifest could forge that baseline. Use volume write
protection and trusted HTTPS downloads. No claim is made that ETags or MMDB
structural checks are cryptographic publisher signatures, or that a recent
successful check proves a provider's underlying data is universally current.

Lookup readers stay valid until in-flight lookups complete. Download/reload
failures preserve the last working pair. Health stays green when an older
working pair is loaded; freshness alerts identify stale data separately.
Failed updater attempts retry after 15 minutes. Unreferenced snapshots older
than one hour are cleaned during successful checks. Old files are never
published through the HTTP server. MaxMind data must be maintained/deleted in
accordance with its current EULA; don't retain stale backups indefinitely.

Recreate a corrupt volume only after diagnosing the problem and backing up any
needed configuration; deleting the volume forces full downloads and temporary
unreadiness. For a forced DB-IP refresh within the same month, stop the updater,
remove **only** `current.json` from the data volume, and restart the updater;
running API readers keep serving their loaded snapshot until replacement.

## Monitoring and privacy

See `deploy/prometheus.yml` and `deploy/alerts.yml`. Load the alert rule file
into your Prometheus deployment separately. Metrics include:

- Bounded-route request counts, status codes and latency histograms.
- Lookup outcomes, loaded database build timestamps and readiness.
- DB-IP fallback build timestamps in MaxMind mode (zero for legacy snapshots).
- Last successful updater check and cumulative update failures (gauge).
- Go runtime/process metrics.
- Per-source lookup histograms and IPinfo database build timestamp when applicable.
- Per-file checked/downloaded/verified timestamps, loaded-snapshot integrity,
  and bounded rate-limit rejection reasons. Unconfigured/unknown timestamps are 0.

### Lookup timing and benchmarking

Successful lookup responses expose `Server-Timing` with `lookup`, `maxmind`,
`dbip`, and `ipinfo` stages **only when consulted**. Durations are milliseconds.
The JSON schema is unchanged. Inspect actual request timing with:

```sh
curl -i http://127.0.0.1:8080/122.168.197.166
```

The `lookup` duration includes in-process lock wait, database reads and response
enrichment. It excludes JSON serialization, HTTP/proxy overhead, internet
latency and database download/startup time. The per-source stages measure MMDB
lookups/decoding; a skipped fallback is omitted, never reported as consulted.
Prometheus records `myip_geoip_lookup_duration_seconds{source=...}` histograms
with bounded `maxmind`, `dbip`, `ipinfo` and `total` labels (with standard
`_bucket`, `_sum`, `_count` suffixes), and no IP labels.

Once an IPinfo-enabled snapshot has been published, benchmark all three sources:

```sh
go run ./cmd/bench -ip 122.168.197.166 -n 10000
```

Or, after rebuilding the API image: `docker compose exec api bench -ip 122.168.197.166 -n 10000`.

The default benchmark explicitly probes **all three loaded sources**, including
both City/ASN files of MaxMind and DB-IP, without changing the source selection
policy. It requires all three; it fails clearly if IPinfo is not loaded, rather
than inventing a three-source result. Output gives per-source and combined mean,
p50, p95 and p99 in **microseconds**, after 1,000 unmeasured warm-up iterations.
This diagnostic path is more work than the optimized HTTP path, which skips
unnecessary fallback reads. To benchmark that normal path with the currently
loaded databases instead:

```sh
go run ./cmd/bench -ip 122.168.197.166 -n 10000 -all-sources=false
```

These are single-IP, serial, warm-process measurements, not mixed-IP throughput
or internet response-time guarantees. Use representative addresses and traffic
on deployment hardware for capacity planning. Do not use `go run -race` when
measuring performance; the race detector adds substantial overhead.

No IP, city, query, or raw request path is used as a label. The metrics listener
has no authentication: keep it on a private network and never publish it
unprotected. Application logs do not record raw visitor IPs or requests;
configure Nginx/Cloudflare logging and retention separately. The updater does
not log geoipupdate child output to avoid accidental credential disclosure.

## Development and verification

Requires Go 1.27+; static timezone data and assets are embedded in the binary.

```sh
make test
make vet
make build
GEOIP_PROVIDER=dbip go run ./cmd/updater --once
GEOIP_PROVIDER=dbip go run ./cmd/myip
```

For local MaxMind usage, export both credentials before `make update` (the
official `geoipupdate` executable must be on PATH), then `make run`. Alternatively
load your own `.env` once with `set -a; . ./.env; set +a`. Local commands do not
silently load dotenv files; Docker Compose does. Secret-file paths for local
commands must be host paths, not container `/run/secrets/` paths.

Migration: remove `BASE_URL` and `DOCS_URL` (now ignored), add `RATE_LIMIT_RPM`,
and optionally retain the old `RATE_LIMIT_RPS` as an extra throttle. When moving
from a temporary `DATA_DIR`, either keep that override or run the updater to
populate the new default directory. Existing snapshot formats are unchanged.

Tests cover real MaxMind test fixtures, schema enrichment, DST, proxy spoofing,
IPv6 aggregation, limiter capacity, readiness, metrics privacy, download errors,
failed update preservation, and concurrent reload/lookup behavior. Run these
checks manually before tagging; the single publishing workflow builds Docker
Hub images and GitHub Releases, not PR-test CI. Production deployment still requires TLS,
correct proxy/firewall configuration, monitoring, and a review of current data
licenses. The MaxMind live download path requires your replacement credentials.

To repeat lookup tests against a downloaded snapshot without making additional
provider requests:

```sh
MYIP_TEST_DATA_DIR=/path/to/data MYIP_TEST_PROVIDER=maxmind go test -race ./internal/geo -run TestLiveSnapshot -v
```

## Licensing and attribution

Source code: MIT. Data licenses are separate; see [THIRD_PARTY.md](../THIRD_PARTY.md).
No production IP databases are bundled in the repository or Docker images.
Review provider terms for your public API/redistribution use case before launch;
a free download alone is not permission for unrestricted redistribution.
