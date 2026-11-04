# 🐳 Deploy myip

You only need **Docker + Docker Compose**. No source checkout, Go or separate
database server. Pick one option below; both use `mayankfawkes/ip:latest`.

## 1 · Choose your data

| Source | Account / authentication required? | Role |
| --- | --- | --- |
| DB-IP | **No** | Standalone location + ASN data, or fallback in MaxMind mode |
| MaxMind | **Yes:** account ID + license key | Primary location + ASN data with DB-IP fallback |
| IPinfo Lite | **Yes:** download token, plus MaxMind credentials for the current mode | Optional network/domain enrichment, not a standalone location provider |

For each source's variables, required values, defaults and ISP examples, see
[environment variables](docs/ENVIRONMENT.md).

### 🟢 DB-IP — no account needed

```bash
mkdir -p myip && cd myip
curl -fsSLo compose.yml https://raw.githubusercontent.com/TrueFix/ip/main/deploy/compose.dbip.yml
docker compose pull
docker compose up -d
```

That’s it. No credentials or `.env` required. This mode uses DB-IP only.

### 🔵 MaxMind + DB-IP — two required credentials

Get a free [MaxMind account and license key](https://www.maxmind.com/en/geolite-free-ip-geolocation-data), then:

```bash
mkdir -p myip && cd myip
curl -fsSLo compose.yml https://raw.githubusercontent.com/TrueFix/ip/main/deploy/compose.maxmind.yml
```

Create `.env` **next to `compose.yml`**, replacing both placeholders:

You can copy the separate [MaxMind example](deploy/env/maxmind.env.example).

```dotenv
MAXMIND_ACCOUNT_ID=YOUR_ACCOUNT_ID
MAXMIND_LICENSE_KEY=YOUR_LICENSE_KEY
```

```bash
chmod 600 .env
docker compose config --quiet
docker compose pull
docker compose up -d
```

**Both values are mandatory.** Compose refuses to start with a clear error if
either is missing or empty. The updater additionally validates the account ID
and credentials before downloading. Keys go to the updater only—not the API.
This mode uses MaxMind first and DB-IP as the location fallback.

### Optional · Add IPinfo

With the **MaxMind template**, add these to `.env`:

The separate [IPinfo add-on example](deploy/env/ipinfo.env.example) contains only
these optional settings; it does not replace the MaxMind credentials.

```dotenv
IPINFO_ENABLED=true
IPINFO_TOKEN=YOUR_FREE_IPINFO_TOKEN
```

Get a free token at [ipinfo.io](https://ipinfo.io/signup). The updater rejects
`IPINFO_ENABLED=true` without a token. Then run `docker compose up -d` again.
To explicitly use the network operator’s name as an ISP approximation, also set
`ISP_FROM_ASN=true`; it is not independently verified retail ISP data.

## 2 · Check it

```bash
docker compose logs -f updater
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/8.8.8.8
```

The initial download takes a few minutes. Health returns `503` until the full
database snapshot is loaded. Lookup data and SQLite audits persist in a named
volume; **don’t use `docker compose down -v` during upgrades**.

The API binds to loopback by default. Put HTTPS/Nginx in front for public access;
configure `TRUSTED_PROXIES` for the actual ingress CIDRs and sanitize forwarded
headers. Port **9090 stays private**, not published by either template.

## 3 · Update or pin a version

```bash
docker compose pull
docker compose up -d
```

To pin a release instead of `latest`, edit **both** `image:` entries in
`compose.yml`, for example to `mayankfawkes/ip:v1.0.0`. Host port, bind address
and storage are configured directly in Compose, not through `.env`.

### Optional · Enable or disable rate limiting

| Variable | Default | Effect |
| --- | --- | --- |
| `RATELIMIT_ENABLE` | `true` | `false` disables all built-in RPM/RPS limits, capacity restrictions and client tracking. |
| `RATE_LIMIT_RPM` | `300` | Rolling-minute request budget per client when limiting is enabled. |

To disable limiting, add this to `.env` beside `compose.yml`:

```dotenv
RATELIMIT_ENABLE=false
```

Then run `docker compose up -d`. To re-enable, set `RATELIMIT_ENABLE=true`
and run the command again. Use a positive `RATE_LIMIT_RPM` (for example `300`)
to admit requests. When enabled, RPM `0` rejects every rate-limited request with
HTTP 429. Only `RATELIMIT_ENABLE=false` disables limiting.

The switch requires **v0.1.4 or newer**: older images ignore the boolean setting.
Pull an updated image before relying on it. Keep edge-level protection
for a public service; this setting does not change Cloudflare/Nginx limits.

These are standalone, simplified templates—not Compose override files. Choose
one and save it as `compose.yml`. Keep the same deployment directory/volume
when changing providers. Don’t commit `.env` or share tokens in chat/logs.

For mounted `_FILE` secrets, resource limits and additional hardening, use the
[full Compose file](compose.yml) and [production guide](deploy/PRODUCTION.md).
See the [reference](docs/REFERENCE.md) for all application settings.
See [environment variables](docs/ENVIRONMENT.md) for every variable's purpose,
default and template support. Most installations need only their source credentials.
