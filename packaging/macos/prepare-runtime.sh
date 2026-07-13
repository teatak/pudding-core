#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]] || [[ "$1" != "arm64" && "$1" != "x64" ]]; then
  echo "usage: $0 <arm64|x64>" >&2
  exit 2
fi
if [[ "${PUDDING_PACKAGING_PIPELINE:-}" != "1" ]]; then
  echo "run the runtime build through make desktop-bundle." >&2
  exit 1
fi
if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "desktop runtimes can only be built on macOS." >&2
  exit 1
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ARCH="$1"
GOARCH="$ARCH"
if [[ "$ARCH" == "x64" ]]; then
  GOARCH="amd64"
fi

OUT_DIR="$ROOT/dist/runtime/$ARCH"
LANGUAGE_SERVER_DIR="$OUT_DIR/language-servers"
DAEMON_PATH="$OUT_DIR/puddingd"
MODULE="github.com/teatak/pudding-core"
BUILD_TAGS="sqlite_fts5 webrtcaec"
LDFLAGS="-X $MODULE/internal/buildinfo.channel=release"

cd "$ROOT"
rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"

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

PUDDING_LANGUAGE_SERVER_OUT_DIR="$LANGUAGE_SERVER_DIR" \
  PUDDING_LANGUAGE_SERVER_GOOS=darwin \
  PUDDING_LANGUAGE_SERVER_GOARCH="$GOARCH" \
  bash "$ROOT/scripts/prepare-language-servers.sh"

expected_arch="$ARCH"
if [[ "$ARCH" == "x64" ]]; then
  expected_arch="x86_64"
fi
for binary in "$DAEMON_PATH" "$LANGUAGE_SERVER_DIR/gopls"; do
  if ! lipo -archs "$binary" | grep -qw "$expected_arch"; then
    echo "runtime binary has the wrong architecture: $binary" >&2
    exit 1
  fi
done

echo "Prepared Pudding desktop runtime: arch=$ARCH output=$OUT_DIR"
