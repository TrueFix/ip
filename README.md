![myip — Your IP. Your infrastructure.](docs/assets/readme-banner.png)

<p align="center">
  A fast, self-hosted IP intelligence API. Local lookups. No per-request upstream calls.
</p>

<p align="center">
  <a href="docs/REFERENCE.md">Documentation</a> ·
  <a href="deploy/PRODUCTION.md">Deployment</a> ·
  <a href="CONTRIBUTING.md">Contributing</a>
</p>

## ✨ Small API. Solid foundations.

- **IPv4 + IPv6** — location, network, country flags and timezone information.
- **Local-first** — MaxMind + DB-IP, with optional IPinfo enrichment.
- **Verified updates** — SHA-256 inventories, atomic snapshots and SQLite audit history.
- **Built to operate** — Docker, Prometheus, trusted proxies and RPM/RPS limits.

Built with Go’s standard `net/http` and `http.ServeMux`. No external database server required.

## 🚀 Quick start

Requires Docker and Docker Compose—no source checkout or Go installation.

```bash
mkdir -p myip && cd myip
curl -fsSLo compose.yml https://raw.githubusercontent.com/TrueFix/ip/main/compose.yml
curl -fsSLo .env.example https://raw.githubusercontent.com/TrueFix/ip/main/.env.example
cp -n .env.example .env
mkdir -p secrets
```

Set this in `.env` for credential-free lookups:

```dotenv
GEOIP_PROVIDER=dbip
```

```bash
docker compose pull
docker compose up -d
curl http://localhost:8080/8.8.8.8
```

The first database download takes a little time; `/healthz` returns `503` until ready.
Localhost itself is not a public IP, so use an explicit IP for local testing.

Both containers use **[mayankfawkes/ip](https://hub.docker.com/r/mayankfawkes/ip)**
(amd64/arm64). Pin a release with `IMAGE_TAG=v0.1.0`; otherwise `latest` selects
the latest stable release. Images become available after the first version-tag push.

### Choose your data

| Setting | Behavior |
| --- | --- |
| `GEOIP_PROVIDER=maxmind` | MaxMind primary + DB-IP fallback; MaxMind credentials required. |
| `GEOIP_PROVIDER=dbip` | DB-IP only; no account needed. |
| `IPINFO_ENABLED=true` | Optional network enrichment in MaxMind mode; free IPinfo token required. |

Locations are approximate. Missing data stays `null`—never guessed.
See the [configuration guide](docs/REFERENCE.md#configuration) for credentials and options.

## 🔌 Endpoints

| Route | Purpose | Port |
| --- | --- | --- |
| `GET /` | Look up the caller | `8080` |
| `GET /<ip>` | Look up any public IPv4 or IPv6 | `8080` |
| `GET /healthz` | Readiness | `8080` |
| `GET /metrics` | Prometheus metrics | `9090` · private |
| `GET /databases` | File inventory and update status | `9090` · private |

Response links automatically follow the request’s host and scheme. No base URL configuration needed.

## 🛡️ Before going public

Keep port **9090 private**, configure HTTPS and trust only your actual reverse proxy.
Rate limits are **per-process memory budgets**; they reset on restart and are not shared across replicas.

Keep `.env`, tokens and downloaded data out of Git. Data defaults to `./data` locally and `/data` in Docker.

Follow the [production checklist](deploy/PRODUCTION.md) before exposing the service.

## 🛠️ Development

Requires Go 1.27+ and Python 3.

```bash
make hooks
make check
make audit
```

For local runs, benchmarks, SQLite history and the full response schema, see the [reference](docs/REFERENCE.md).

Prefer binaries? [GitHub Releases](https://github.com/TrueFix/ip/releases) include
one standalone API binary per Linux architecture—amd64, arm64, ARMv7, 386 and
RISC-V—with SHA-256 checksums. No archives or helper executables. The raw binary
needs provisioned databases; Docker remains the easiest option for automatic updates.

---

[MIT license](LICENSE) · [Data licenses & attribution](THIRD_PARTY.md) · [Security](SECURITY.md)
