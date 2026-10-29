#!/bin/bash
set -euo pipefail

cd "$(dirname "$0")"
[[ "$(uname -s)" == Darwin ]] || { echo "Mieru archive requires macOS and Xcode" >&2; exit 1; }
command -v go >/dev/null
GO_VERSION="$(go env GOVERSION)"
[[ "$GO_VERSION" == go1.26.* ]] || { echo "Mieru archive requires Go 1.26.x" >&2; exit 1; }
mkdir -p build
MODFILE="$(go run ./cmd/prepare)"
SDKROOT="$(xcrun --sdk macosx --show-sdk-path)"
CC="$(xcrun --sdk macosx --find clang)"
export SDKROOT CC
export CGO_ENABLED=1 GOOS=darwin GOARCH=arm64
export MACOSX_DEPLOYMENT_TARGET=12.0
export CGO_CFLAGS="-arch arm64 -isysroot $SDKROOT -mmacosx-version-min=12.0"
export CGO_LDFLAGS="$CGO_CFLAGS"
STAMP="$( { find bridge cmd include patches -type f ! -name '*_test.go' -exec shasum -a 256 {} + | LC_ALL=C sort; shasum -a 256 go.mod go.sum build.sh; go version; xcodebuild -version; printf '%s\n' "$SDKROOT"; } | shasum -a 256 | awk '{print $1}')"
if [[ -f build/libmieru.a && -f build/libmieru.h && -f build/source.stamp && "$(cat build/source.stamp)" == "$STAMP" ]]; then
    echo "Mieru archive unchanged"
    exit 0
fi
go build -modfile="$MODFILE" -mod=readonly -trimpath -buildmode=c-archive -o build/libmieru.a ./cmd/carchive
printf '%s\n' "$STAMP" > build/source.stamp
echo "Mieru macOS arm64 archive ready"
