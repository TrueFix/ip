# Environment variables

**Only configure what you need.** Most settings have defaults. Source credentials
are separate from optional server tuning below.

All API lookups read **local, downloaded databases**. Only the updater contacts
data providers. These are three data sources, **not three independent modes**:
DB-IP works alone; MaxMind mode includes DB-IP fallback; IPinfo is an optional
add-on in MaxMind mode.

## 1. DB-IP — no authentication

Free location and ASN databases. Works on its own.

| Variable | Required? | Default / purpose |
| --- | --- | --- |
| `GEOIP_PROVIDER` | Select `dbip` | Use DB-IP only; no MaxMind or IPinfo downloads/lookups. The DB-IP Compose template already selects this. |

**API key/account:** Not required.

**Example:** [dbip.env.example](../deploy/env/dbip.env.example). With
[`compose.dbip.yml`](../deploy/compose.dbip.yml), no `.env` file is needed.

## 2. MaxMind — authentication required

Primary location and ASN databases, with DB-IP fallback included automatically.

| Variable | Required? | Default / purpose |
| --- | --- | --- |
| `GEOIP_PROVIDER` | Select `maxmind` | Application/root Compose default. MaxMind primary + DB-IP fallback; the MaxMind Compose template already selects this. |
| `MAXMIND_ACCOUNT_ID` | **Yes** | Your positive numeric MaxMind account ID. |
| `MAXMIND_LICENSE_KEY` | **Yes** | Your MaxMind download license key. |
| `MAXMIND_ACCOUNT_ID_FILE` | Alternative | Read the account ID from a mounted file instead of its environment value. |
| `MAXMIND_LICENSE_KEY_FILE` | Alternative | Read the license key from a mounted file instead of its environment value. |
| `GEOIPUPDATE_CONFIG_FILE` | No; legacy alternative | GeoIP.conf containing MaxMind credentials. Cannot combine with MaxMind credential values or their `_FILE` alternatives. Unnecessary for new deployments. |

**Authentication:** Free [MaxMind account + license key](https://www.maxmind.com/en/geolite-free-ip-geolocation-data).
Credentials are used by the **updater only**, not the API.

**Example:** [maxmind.env.example](../deploy/env/maxmind.env.example).
The simple MaxMind template refuses missing/empty credentials; the updater
validates the account ID and key format before attempting a download.

## 3. IPinfo Lite — token required

Optional network/domain enrichment in **MaxMind mode**, not a standalone
location provider. Your MaxMind credentials are still required.

| Variable | Required? | Default / purpose |
| --- | --- | --- |
| `IPINFO_ENABLED` | Set `true` to enable | Default `false`; updater downloads IPinfo Lite into the shared snapshot. Ignored in DB-IP mode. |
| `IPINFO_TOKEN` | **Yes, when enabled** | Your IPinfo database download token; updater only. |
| `IPINFO_TOKEN_FILE` | Alternative | Read the token from a mounted file instead of its environment value. |
| `ISP_FROM_ASN` | No | Default `false`; API uses IPinfo's network operator name for `connection.isp` when enabled. An approximation, not verified retail ISP data. |

**Authentication:** Free [IPinfo account + download token](https://ipinfo.io/lite).
No extra authentication or external request is performed by `ISP_FROM_ASN`.

**Example:** Append [ipinfo.env.example](../deploy/env/ipinfo.env.example) to your
MaxMind `.env`; do not replace the MaxMind credentials.

### What changes with `ISP_FROM_ASN`?

Observed against the locally downloaded snapshot during development; provider
records can change in later updates:

| IP | `ISP_FROM_ASN=false` | `ISP_FROM_ASN=true` |
| --- | --- | --- |
| `8.8.8.8` | `"isp": null` | `"isp": "Google LLC"` |
| `1.1.1.1` | `"isp": null` | `"isp": "Cloudflare, Inc."` |
| `122.168.197.166` | `"isp": null` | `"isp": "Bharti Airtel Ltd., Telemedia Services"` |

Only `connection.isp` changes. Location, ASN, organization and domain enrichment
are unaffected by this switch. IPinfo's ASN must agree with the selected ASN, or
supply an otherwise missing ASN; conflicting network owners are never merged.
Without a valid IPinfo record/name, ISP remains `null`. Enabling this setting
without IPinfo data does nothing. **Both settings use local reads only.**

## 4. Shared database settings — no authentication

| Variable | Default | Purpose |
| --- | --- | --- |
| `UPDATE_INTERVAL` | `24h` | Updater checks for new databases; Go duration such as `12h`, minimum `1h`. Does not control providers' publication schedules. |
| `DATA_DIR` | `./data` locally; `/data` in Docker | Shared snapshot/audit directory. Updater writes; API reads. Both processes must use the same location. |

No additional key/token is needed for these settings. Downloads still require
the credentials of whichever sources you enabled.

## 5. Rate limiting — no authentication

| Variable | Default | Purpose |
| --- | --- | --- |
| `RATELIMIT_ENABLE` | `true` | Set `false` to disable **all** built-in throttling and client tracking, regardless of RPM/RPS settings. |
| `RATE_LIMIT_RPM` | `300` | Accepted requests per client per rolling minute; integer 0–100000. **`0` allows no rate-limited requests** when limiting is enabled (HTTP 429). |
| `RATE_LIMIT_RPS` | Unset | Optional additional per-second token bucket; positive number up to 100000. Does not replace RPM. |
| `RATE_LIMIT_BURST` | `20` | RPS burst allowance; integer 1–100000. No throttling effect unless RPS is set. |
| `RATE_LIMIT_MAX_CLIENTS` | `50000` | Maximum tracked client keys; integer 1–1000000. New keys receive 429 when full. Bounds memory usage. |

Disable all built-in limits with:

```dotenv
RATELIMIT_ENABLE=false
```

This requires **v0.1.4 or newer**; older releases ignore the boolean switch and
reject zero RPM. Other rate-limit settings must still contain
valid values, but do not enforce limits when disabled. `RATELIMIT_ENABLE` is the
only on/off switch. With it set to `true`, RPM zero rejects all rate-limited
requests; readiness, private monitoring and CORS preflight remain exempt.

| `RATELIMIT_ENABLE` | `RATE_LIMIT_RPM` | Result |
| --- | --- | --- |
| `true` | `300` | Enforce 300 requests per client per rolling minute, plus any RPS limit |
| `true` | `0` | Reject every rate-limited request with HTTP 429 |
| `false` | Any valid value, including `0` | Bypass all built-in limiting and client tracking |

After changing the switch in `.env`, run `docker compose up -d` to recreate the
API service. A container restart alone does not load changed Compose environment
values. For native binaries, update the exported environment/service settings and
restart the process. The switch does not change limits at an upstream proxy.

Limits are per process, reset on restart, and group IPv6 addresses by /64.
No rate-limit visitor IPs are written to SQLite. Keep edge-level protection for
a public service, especially if you disable application limits.

## 6. Proxy, hostname and browser settings — no authentication

| Variable | Default | Purpose |
| --- | --- | --- |
| `TRUSTED_PROXIES` | Empty | Comma-separated proxy CIDRs allowed to supply forwarded client/origin headers. Empty trusts no proxy. Never use a trust-all range. |
| `PROXY_HEADER` | `X-Forwarded-For` | Choose `X-Forwarded-For`, `X-Real-IP`, or `CF-Connecting-IP`. Only honored from trusted peers; ingress must sanitize headers. |
| `ALLOWED_HOSTS` | Empty | Optional comma-separated exact `host[:port]` allowlist, e.g. `ip.example.com`. Prevents unwanted hostnames in generated flag/docs URLs. Not authentication or a forced base URL. |
| `CORS_ORIGIN` | `*` | Browser cross-origin policy, e.g. `https://app.example.com`. Not authentication or protection against non-browser clients. No credentialed CORS. |

These settings do **not** add API-key authentication. See the
[proxy security guide](REFERENCE.md#proxy-trust-and-production-exposure).

## 7. Process listeners — no authentication

| Variable | Default | Purpose |
| --- | --- | --- |
| `LISTEN_ADDR` | `:8080` | API process listener; use e.g. `127.0.0.1:8080` for a local binary behind a proxy. |
| `METRICS_ADDR` | `127.0.0.1:9090` locally; `:9090` in Docker | Private monitoring/status listener. **No authentication; keep private.** |

The supplied Compose files do not publish port 9090.

## Which settings reach the containers?

Compose reads `.env` for `${...}` substitutions. It does **not** forward every
variable automatically. An unsupported override in `.env` alone has no effect.

| Settings | DB-IP template | MaxMind template | Root Compose |
| --- | --- | --- | --- |
| Provider | Fixed `dbip` | Fixed `maxmind` | Configurable |
| MaxMind account ID + license key | Not used | Required | Required in MaxMind mode |
| IPinfo enable + token + ISP switch | Not used | Supported | Supported in MaxMind mode |
| Update interval | Supported | Supported | Supported |
| Proxy trust/header, allowed hosts, rate-limit switch, RPM | Supported | Supported | Supported |
| RPS, burst, client capacity, CORS | Application defaults | Application defaults | Configurable |
| Credential `_FILE` settings / legacy config | Not forwarded | Not forwarded | Supported; host `./secrets` mounted at `/run/secrets` |
| Data directory and process listeners | Image defaults | Image defaults | Fixed data directory; image listener defaults |

Image tags, host bind address/port and volume mounts are configured **directly in
Compose**, not through Docker-only environment variables. Defaults are
`mayankfawkes/ip:latest`, `127.0.0.1:8080:8080`, and the persistent `databases`
named volume. Edit both `image:` entries to pin a release.

Native binaries read exported environment variables, not `.env` files. Use your
service manager or export settings explicitly. Secret-file paths must be host
paths rather than container paths.

## Secret handling and obsolete settings

- Use a credential value **or** its `_FILE` alternative, never both. Files must
  be readable by the updater; a trailing newline is allowed. See
  [secret permissions](REFERENCE.md#maxmind-default-provider).
- Never commit `.env` or real tokens. Changing an example does not change your
  deployment; edit your `.env`, then run `docker compose up -d`.
- `BASE_URL` and `DOCS_URL` are obsolete and ignored. Links follow the request
  origin with [trusted-proxy safeguards](REFERENCE.md#automatic-response-urls).
- `MYIP_TEST_DATA_DIR`, `MYIP_TEST_PROVIDER` and `MYIP_TEST_IPINFO_FILE` are test
  inputs, not service settings. Build/release variables do not belong in `.env`.
