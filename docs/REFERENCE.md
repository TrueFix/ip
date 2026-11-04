# API reference

myip serves IP information from local MMDB databases. API requests never call
provider services. The separate updater downloads and publishes snapshots;
embedded SQLite stores update audits, not visitor lookups or rate-limit state.

- [Install with Docker](../DEPLOYMENT.md)
- [Environment variables by source and purpose](ENVIRONMENT.md)
- [Production and releases](../deploy/PRODUCTION.md)
- [Testing and benchmarks](../TESTING.md)

## Endpoints

| Endpoint | Listener | Purpose |
| --- | --- | --- |
| `GET /` | Public `8080` | Look up the caller's public IP |
| `GET /<ip>` | Public `8080` | Look up a public IPv4 or IPv6 address |
| `GET /healthz` | Public `8080` | Readiness; `503` until usable databases are loaded |
| `GET /docs` | Public `8080` | JSON documentation, build information and attribution |
| `GET /flags/in.svg` | Public `8080` | Country flag; lowercase ISO alpha-2 code |
| `GET /country-data.json` | Public `8080` | Bundled country metadata with ODbL access |
| `GET /metrics` | Private `9090` | Prometheus metrics |
| `GET /databases` | Private `9090` | Snapshot inventory, integrity and update status |

`HEAD` and CORS preflight are supported. Monitoring paths return `404` on the
public listener. Port 9090 has **no authentication**; keep it private. There is
no HTML frontend. For localhost testing use `/8.8.8.8`; `/` rejects private caller IPs.

## MaxMind (default provider)

| Mode | Authentication | Data selection |
| --- | --- | --- |
| `GEOIP_PROVIDER=dbip` | None | DB-IP City + ASN only |
| `GEOIP_PROVIDER=maxmind` | Account ID + license key, updater only | MaxMind primary + DB-IP fallback |
| Optional IPinfo in MaxMind mode | IPinfo token, updater only | Network/domain enrichment; optional ISP approximation |

The application/root Compose defaults to MaxMind; the root `.env.example` selects
DB-IP for credential-free setup. Provider-specific templates fix their own mode.
Unknown providers are rejected. The API needs no download credentials.

### Selection rules

- A populated MaxMind city wins. Otherwise DB-IP may supply the **whole**
  location record, including coordinates, region and country. Country-only
  fallback is also possible when the primary city and country are missing;
  a populated primary city never triggers country field stitching. Missing ASN
  data can independently fall back to DB-IP.
- A primary timezone is preserved across fallback only when countries agree and
  pinned IANA metadata identifies it as that country's sole timezone. Country
  alone never generates a new timezone.
- IPinfo enrichment requires matching ASNs, or an otherwise missing ASN.
  Existing organization names are preserved. Invalid domains remain `null`.
- `ISP_FROM_ASN=true` uses IPinfo's operator name for ISP. It makes no external
  request and does not establish the subscriber's retail ISP.
- DB-IP mode does not load MaxMind/IPinfo, even when old files remain on disk.

Changing provider requires recreating **both** containers. The API remains
unready until a snapshot for the selected mode is published. Hybrid attribution
credits MaxMind and DB-IP; IPinfo is credited when its data contributes.

### Mounted secrets

Root Compose mounts host `./secrets` at `/run/secrets` in the updater only:

```dotenv
GEOIP_PROVIDER=maxmind
MAXMIND_ACCOUNT_ID_FILE=/run/secrets/maxmind_account_id
MAXMIND_LICENSE_KEY_FILE=/run/secrets/maxmind_license_key
```

Each file contains one value; a trailing newline is allowed. Use the value or
its `_FILE` alternative, never both. The legacy `GEOIPUPDATE_CONFIG_FILE` cannot
be combined with MaxMind credential settings. Restrict host access while allowing
container UID/GID 65532 to traverse/read the files:

```sh
sudo chown 65532:65532 secrets secrets/maxmind_account_id secrets/maxmind_license_key
sudo chmod 0700 secrets
sudo chmod 0400 secrets/maxmind_account_id secrets/maxmind_license_key
```

Simple source-specific templates use environment credentials, not mounted files.
Native binaries need actual host file paths. Never share or commit real credentials.

## Response contract

Example shape; provider records and populated fields vary:

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
    "img": "https://ip.example.com/flags/us.svg",
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
  "readme": "https://ip.example.com/docs"
}
```

| Field group | Meaning / limitations |
| --- | --- |
| Location | Provider estimates, not GPS. Registration country is not substituted for actual location. |
| Region code | Provider value, or an unambiguous country-scoped first-level subdivision match. Unmatched names stay `null`. |
| Country metadata | Bundled capital, calling code, land borders, EU membership and flags. Calling codes are strings without `+`. |
| Connection | ASN owner is not necessarily the retail ISP. No VPN, proxy, Tor, cloud or residential classification is implemented. |
| Timezone | Available provider timezone ID; offset/abbreviation/DST calculated at request time from embedded IANA data. |

Missing values are `null`; a known country with no land borders has `"borders": ""`.
Postal codes are intentionally omitted. `success: true` means a record was
found, not that every field is populated. Anycast/VPN/mobile location may
represent an exit or provider estimate, not the user.

Errors use `{"success":false,"message":"..."}`:

| Status | Meaning |
| --- | --- |
| `400` | Invalid IP/proxy header or non-public/reserved address |
| `404` | No record, unknown endpoint or unavailable flag |
| `405` | Unsupported method |
| `429` | Rate budget or client capacity exhausted; see `Retry-After` |
| `500` | Unexpected lookup failure |
| `503` | Required databases unavailable or loaded-file integrity failed |

## Proxy trust and production exposure

By default no forwarding headers are trusted. Configure the actual ingress peer
CIDRs, not all private networks or a trust-all range:

```dotenv
TRUSTED_PROXIES=172.30.0.2/32
PROXY_HEADER=X-Forwarded-For
ALLOWED_HOSTS=ip.example.com
```

Choose one client-IP header: `X-Forwarded-For`, `X-Real-IP` or `CF-Connecting-IP`.
For XFF, the rightmost untrusted address is selected. Your ingress must overwrite
or sanitize client-supplied forwarding headers. See [Nginx example](../deploy/nginx.conf).

For Cloudflare → Nginx → API, Nginx must validate Cloudflare's source and forward
the sanitized client IP; the API trusts Nginx, not arbitrary incoming Cloudflare
headers. Restrict direct origin ingress and use Full (strict) TLS as appropriate.
Maintain current [Cloudflare ranges](https://www.cloudflare.com/ips/); the app
does not fetch or embed a potentially stale list. Docker's actual peer may be
the bridge gateway; isolate any containers able to impersonate a trusted proxy.

### Automatic response URLs

Flag/docs links follow the request host, port and scheme. No `BASE_URL` or
`DOCS_URL` is needed; those old settings are ignored. A trusted proxy may supply
single sanitized `X-Forwarded-Host` and `X-Forwarded-Proto` values; untrusted peers
cannot override origin. Multiple/comma-separated origin values are rejected.

`ALLOWED_HOSTS` optionally restricts exact `host[:port]` values. It is not a forced
base URL or authentication. Ingress must sanitize origin headers as well as
client-IP headers. API responses use `Cache-Control: no-store`; configure CDN
cache bypass. Bundled flag SVGs receive public cache headers.

## Configuration

[ENVIRONMENT.md](ENVIRONMENT.md) is the authoritative variable reference. It
groups source credentials, shared storage, rate limits, proxy settings and
listeners in separate tables, with defaults and Compose forwarding support.

| `RATELIMIT_ENABLE` | `RATE_LIMIT_RPM` | Result |
| --- | --- | --- |
| `true` (default) | Positive; default `300` | Rolling-minute limit, plus optional RPS throttle |
| `true` | `0` | Reject all rate-limited requests with `429` |
| `false` | Any valid value | Bypass all built-in limiting and client tracking |

Readiness, private monitoring and CORS preflight are exempt. Limits apply across
public routes, not separately per route. Denied requests do not consume RPM;
burst settings only affect the optional RPS bucket. IPv6 /64 addresses share a
budget. Client storage is bounded, idle keys expire, and new clients receive
`429` when capacity is full. Budgets are per-process, reset on restart and are
not coordinated across replicas. SQLite does not persist visitor IPs or quotas.

## Updates, readiness and recovery

One updater writes each volume under an OS-backed lock. It downloads into staging,
validates MMDB types/integrity, computes SHA-256 inventories, flushes files, and
publishes a versioned directory by atomically replacing `current.json`.
Build dates are checked for rollback within the same provider/edition, not
between different providers. APIs poll for new snapshots every 30 seconds.

DB-IP/IPinfo use conditional HTTP requests when validators are available. A
`304` reuses only checksum-verified cached bytes. Missing/corrupt cache or absent
validators forces a download. MaxMind's official `geoipupdate` handles its own
conditional requests. Failed updates preserve the previous published snapshot.

### SQLite audit and checksum inventory

`DATA_DIR/state.sqlite` records update attempts and file inventories with source
references, hashes, byte sizes, build/download/check/verification times and HTTP
validators. It retains the newest 1,000 attempts plus the active published run.
The API does not open SQLite. No credentials, visitor lookups or raw upstream
error bodies are intentionally stored.

The manifest is serving truth. Audit preparation/finalization is reconciled on
the next updater start after a crash; SQLite and rename are not a single
transaction. Missing/corrupt audit history may stop updates without stopping
already loaded readers. Inspect with `updater --status` or private `/databases`.

Inventoried files are verified at load and every six hours. Loaded corruption
makes readiness/lookups return `503` until repaired or replaced; a rejected new
snapshot preserves previous valid readers. Legacy snapshots without inventory
are readable but report `myip_database_integrity_ok=0` until refreshed.

**Checksums detect changes against the local manifest, not publisher signatures.**
An attacker able to rewrite both can forge that baseline. Protect the volume and
use trusted HTTPS downloads. Readiness does not guarantee fresh provider data.
Failed scheduled updates retry after 15 minutes; unused snapshots older than an
hour are cleaned during successful updates. See [rollback guidance](../deploy/PRODUCTION.md#updates-and-rollback).

## Monitoring and privacy

Use private `/metrics` and `/databases`; examples are in
[prometheus.yml](../deploy/prometheus.yml) and [alerts.yml](../deploy/alerts.yml).
Metrics include readiness, integrity, loaded source builds, file timestamps,
updater success/failure, limiter clients/rejections and bounded HTTP/lookup timings.
No visitor IP, location, raw path or query appears in metric labels.

Successful lookups expose `Server-Timing` in milliseconds for total lookup and
each provider actually consulted. It excludes JSON/HTTP/proxy overhead. For
microsecond distributions and diagnostic all-source probes, see
[benchmarking](../TESTING.md#benchmarking).

Application logs do not record normal visitor requests; configure ingress
logging/retention separately. The updater suppresses geoipupdate child output
to avoid credential disclosure. Keep private monitoring off the public internet.

## Development and verification

Use the Go version in `go.mod` and Python 3. Native commands read exported
environment variables, not `.env` automatically:

```sh
make build
GEOIP_PROVIDER=dbip go run ./cmd/updater --once
GEOIP_PROVIDER=dbip go run ./cmd/myip
```

Native MaxMind updates also need the official `geoipupdate` executable on PATH.
See [testing](../TESTING.md), [contributing](../CONTRIBUTING.md) and
[release checks](../deploy/PRODUCTION.md#release-checklist).

## Licensing and attribution

Application source is MIT. Provider data and bundled metadata have separate
licenses and attribution obligations: [THIRD_PARTY.md](../THIRD_PARTY.md).
No production IP databases or provider credentials are bundled in releases.
