# Third-party data and attribution

## Geolocation databases (downloaded at runtime, not redistributed here)

- **MaxMind GeoLite2 City and ASN:** GeoLite2 data created by MaxMind,
  available from https://www.maxmind.com/ . Subject to the current GeoLite EULA,
  attribution, update and deletion requirements. Read:
  https://www.maxmind.com/en/geolite2/eula and
  https://dev.maxmind.com/geoip/geolite2-free-geolocation-data/ .
- **DB-IP Lite City and ASN:** IP Geolocation by DB-IP,
  https://db-ip.com/ . CC BY 4.0; monthly free editions have reduced coverage.
  https://db-ip.com/db/lite.php and https://creativecommons.org/licenses/by/4.0/ .
  Applications displaying results must include a link to DB-IP. API responses
  include attribution Link headers; `/docs` also identifies the provider.
  Downstream sites must provide visible attribution on pages showing the data.
- **IPinfo Lite (optional):** IP address data powered by IPinfo,
  https://ipinfo.io/lite and https://ipinfo.io/developers/ipinfo-lite-database .
  Full downloads require a free token. CC BY-SA 4.0:
  https://creativecommons.org/licenses/by-sa/4.0/ . Review attribution and
  share-alike obligations when incorporating outputs in a public service. API
  responses include an IPinfo attribution Link when data contributes. IPinfo
  provides AS names/domains, not independently verified retail ISP names.

Do not commit credentials or production MMDB files. Confirm the current terms
permit your public lookup service and downstream usage before launch.

## Country metadata

Contains information from **mledoze/countries**, made available under the
**Open Database License (ODbL) 1.0**:
https://github.com/mledoze/countries .

The unmodified snapshot is `internal/assets/countries.json`, publicly accessible
at `/country-data.json` when deployed. Its commit is recorded in
`internal/assets/sources.json`. The license is in `licenses/countries-ODbL.txt`.
This dataset is licensed separately from the application source. EU membership
is an explicitly maintained country-code list in `internal/geo/model.go`.

## Flag assets

**flag-icons** by lipis and contributors, MIT:
https://github.com/lipis/flag-icons . Bundled 4:3 SVGs are in
`internal/assets/flags/`; pinned commit in `internal/assets/sources.json`.
License and copyright notice: `licenses/flag-icons-MIT.txt`.

## Timezone compatibility metadata

`internal/assets/zone.tab` is an unmodified, public-domain IANA timezone table
from https://github.com/eggert/tz . Its pinned commit is in
`internal/assets/sources.json`; the public-domain notice is in the file header.
Used only to decide whether a primary timezone is compatible with a fallback
location in a single-timezone country. Runtime timezone calculations still use
Go's embedded IANA timezone database.

## Subdivision codes

`internal/assets/subdivisions.json` is generated from **wooorm/iso-3166**'s
`2.js`, MIT licensed: https://github.com/wooorm/iso-3166 . Copyright and license
are preserved in `licenses/iso-3166-MIT.txt`. The commit and transformation are
recorded in `internal/assets/sources.json`; regenerate using
`scripts/update-subdivisions.py`. Only unambiguous, country-scoped first-level
names are used to supplement missing provider region codes.

## Test databases

Small synthetic MaxMind test fixtures (not production geolocation data) are
included under Apache 2.0 in `internal/geo/testdata/`. See that directory's
`SOURCE.txt` and `LICENSE-APACHE`.

## Go dependencies

Versioned in `go.mod` and `go.sum`; upstream licenses remain applicable. Notably
MaxMind DB readers (ISC), Prometheus client libraries (Apache 2.0), and Go
extended libraries (BSD-style). The official geoipupdate program (Apache 2.0)
is built from its pinned v8.0.0 Go module only in the updater image.
Tests generate synthetic network databases with MaxMind's mmdbwriter
(Apache 2.0); these fixtures are not real IPinfo data.
The updater embeds modernc.org/sqlite (BSD-style wrapper with SQLite's
public-domain engine and separately licensed dependencies); dependency versions
and upstream licenses remain applicable. SQLite is used for audit metadata only.
