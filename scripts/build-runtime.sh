#!/usr/bin/env bash
set -euo pipefail
if [[ $# -ne 2 || ( "$1" != arm64 && "$1" != x64 ) ]]; then
  echo "usage: scripts/build-runtime.sh <arm64|x64> <absolute-output-dir>" >&2
  exit 2
fi
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARCH="$1"
OUT_DIR="$2"
[[ "$OUT_DIR" = /* && "$OUT_DIR" != / ]] || { echo "output directory must be absolute" >&2; exit 2; }
[[ "$(uname -s)" = Darwin ]] || { echo "macOS runtime requires macOS" >&2; exit 1; }
export SDKROOT="${SDKROOT:-$(xcrun --sdk macosx --show-sdk-path)}"
GOARCH="$ARCH"
if [[ "$ARCH" = x64 ]]; then GOARCH=amd64; fi
MODULE=github.com/teatak/pudding-core
BUILD_TAGS="sqlite_fts5 webrtcaec"
LDFLAGS="-X $MODULE/internal/buildinfo.channel=release"
DAEMON_PATH="$OUT_DIR/puddingd"
mkdir -p "$OUT_DIR"
cd "$ROOT"
if [[ "$ARCH" == "x64" ]]; then
  bash "$ROOT/packaging/macos/prepare-x64-deps.sh"
  DEPS_PREFIX="$ROOT/dist/deps/macos-x64"
  PKG_CONFIG_DIRS="$DEPS_PREFIX/lib/pkgconfig:$DEPS_PREFIX/share/pkgconfig"
  env \
    GOOS=darwin \
    GOARCH=amd64 \
    CGO_ENABLED=1 \
    CC=clang \
    CXX=clang++ \
    CGO_CFLAGS="-arch x86_64 -mmacosx-version-min=12.0" \
    CGO_CXXFLAGS="-arch x86_64 -mmacosx-version-min=12.0" \
    CGO_LDFLAGS="-arch x86_64 -mmacosx-version-min=12.0" \
    PKG_CONFIG_PATH="$PKG_CONFIG_DIRS" \
    PKG_CONFIG_LIBDIR="$PKG_CONFIG_DIRS" \
    go build -tags "$BUILD_TAGS" -ldflags "$LDFLAGS" -o "$DAEMON_PATH" ./cmd/puddingd
else
  env \
    GOOS=darwin \
    GOARCH=arm64 \
    CGO_ENABLED=1 \
    go build -tags "$BUILD_TAGS" -ldflags "$LDFLAGS" -o "$DAEMON_PATH" ./cmd/puddingd
fi

PUDDING_LANGUAGE_SERVER_OUT_DIR="$OUT_DIR/language-servers" \
 PUDDING_LANGUAGE_SERVER_GOOS=darwin PUDDING_LANGUAGE_SERVER_GOARCH="$GOARCH" \
 bash "$ROOT/scripts/prepare-language-servers.sh"
