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

while read -r os arch target arm; do
    case "$os" in ''|'#'*) continue ;; esac
    case "$arm" in -) unset GOARM ;; *) export GOARM="$arm" ;; esac
    suffix=''
    case "$os" in windows) suffix='.exe' ;; esac
    name="myip-$version-$os-$target$suffix"
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags="$flags" -o "$scratch/$name" ./cmd/myip
    (cd "$scratch" && sha256sum "$name") >> "$scratch/SHA256SUMS-$version.txt"
done < scripts/release-targets.txt
while read -r _ name; do
    mv "$scratch/$name" "dist/$name"
done < "$scratch/SHA256SUMS-$version.txt"
mv "$scratch/SHA256SUMS-$version.txt" "dist/SHA256SUMS-$version.txt"
echo "Standalone Linux, macOS and Windows API binaries and checksums are in dist/ (version $version)."
