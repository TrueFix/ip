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

for target in amd64 arm64 armv7 386 riscv64; do
    arch=$target
    case "$target" in armv7) arch=arm; export GOARM=7 ;; *) unset GOARM ;; esac
    name="myip-$version-linux-$target"
    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags="$flags" -o "$scratch/$name" ./cmd/myip
    (cd "$scratch" && sha256sum "$name") >> "$scratch/SHA256SUMS-$version.txt"
done
for target in amd64 arm64 armv7 386 riscv64; do
    mv "$scratch/myip-$version-linux-$target" "dist/myip-$version-linux-$target"
done
mv "$scratch/SHA256SUMS-$version.txt" "dist/SHA256SUMS-$version.txt"
echo "Standalone Linux API binaries and checksums are in dist/ (version $version)."
