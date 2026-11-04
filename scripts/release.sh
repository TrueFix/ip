#!/bin/sh
set -eu
version=${1:?supply a version such as v0.1.0}
revision=${2:-unknown}
if ! printf '%s' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'; then
    echo 'Expected a version such as v0.1.0 or v0.1.0-rc.1' >&2
    exit 1
fi
case "$revision" in *[!0-9a-fA-F]*)
    if [ "$revision" != unknown ]; then echo 'Invalid revision' >&2; exit 1; fi
esac
date=${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}
flags="-s -w -X github.com/TrueFix/myip/internal/buildinfo.Version=$version -X github.com/TrueFix/myip/internal/buildinfo.Revision=$revision -X github.com/TrueFix/myip/internal/buildinfo.BuildDate=$date"
mkdir -p dist
scratch=$(mktemp -d "dist/.release-$version.XXXXXX")
trap 'rm -rf "$scratch"' EXIT
go mod download github.com/maxmind/geoipupdate/v8@v8.0.0
tool_source="$(go env GOMODCACHE)/github.com/maxmind/geoipupdate/v8@v8.0.0"

for target in amd64 arm64 armv7 386 riscv64; do
    arch=$target
    case "$target" in armv7) arch=arm; export GOARM=7 ;; *) unset GOARM ;; esac
    name="myip-$version-linux-$target"
    bundle="$scratch/$name"
    mkdir -p "$bundle"
    for binary in myip updater bench; do
        CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags="$flags" -o "$bundle/$binary" "./cmd/$binary"
    done
    # Bundle the official downloader too; no distro-specific package is needed.
    output="$(pwd)/$bundle/geoipupdate"
    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -C "$tool_source" -trimpath -o "$output" ./cmd/geoipupdate
    cp LICENSE THIRD_PARTY.md README.md SECURITY.md CONTRIBUTING.md .env.example compose.yml "$bundle/"
    cp -R licenses docs deploy "$bundle/"
    cat > "$bundle/RUNNING.txt" <<'TEXT'
All four executables are static Linux binaries for the archive's architecture.
Use a current, supported Linux kernel; binaries are not tested on every distro.

1. Add this extracted directory to PATH (needed by the MaxMind updater):
   export PATH="$PWD:$PATH"
2. Copy .env.example to .env, edit provider settings, then export them:
   set -a; . ./.env; set +a
3. Run updater --once to populate ./data, then start myip.
4. Run updater continuously in a separate service for scheduled updates.

The default ./data directory is relative to your working directory. Keep port
9090 private. See docs/REFERENCE.md and deploy/PRODUCTION.md for configuration.

Alternatively, compose.yml pulls the prebuilt mayankfawkes/ip Docker Hub image;
it does not run these local binaries or require the project source code.
TEXT
    tar -czf "dist/$name.tar.gz" -C "$scratch" "$name"
done
(
    cd dist
    sha256sum myip-"$version"-linux-*.tar.gz > "SHA256SUMS-$version.txt"
)
echo "Linux release archives and checksums are in dist/ (version $version)."
