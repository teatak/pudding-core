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
bash "$ROOT/packaging/macos/prepare-deps.sh" "$ARCH"
DEPS_PREFIX="$ROOT/dist/deps/macos-$ARCH"
PKG_CONFIG_DIRS="$DEPS_PREFIX/lib/pkgconfig:$DEPS_PREFIX/share/pkgconfig"
MACH_ARCH=x86_64
# The pinned Intel ONNX Runtime requires 15.5, matching Desktop's declaration.
MIN_MACOS_VERSION=15.5
if [[ "$ARCH" == arm64 ]]; then
  MACH_ARCH=arm64
  MIN_MACOS_VERSION=14.0
  PKG_CONFIG_DIRS="$PKG_CONFIG_DIRS:$ROOT/dist/deps/abseil/lib/pkgconfig"
fi
# cgo environment flags precede package flags: select the freshly built bridge,
# not the historical source-tree archive. Never fall back to Homebrew libraries.
# AEC/NS intentionally link the same archives; ignore only duplicate-library
# diagnostics, keeping deployment-target and other linker warnings fatal.
env \
  GOOS=darwin GOARCH="$GOARCH" CGO_ENABLED=1 CC=clang CXX=clang++ \
  MACOSX_DEPLOYMENT_TARGET="$MIN_MACOS_VERSION" \
  CGO_CFLAGS="-O2 -g -arch $MACH_ARCH -mmacosx-version-min=$MIN_MACOS_VERSION" \
  CGO_CXXFLAGS="-O2 -g -arch $MACH_ARCH -mmacosx-version-min=$MIN_MACOS_VERSION" \
  CGO_LDFLAGS="-L$DEPS_PREFIX/lib -arch $MACH_ARCH -mmacosx-version-min=$MIN_MACOS_VERSION -Wl,-no_warn_duplicate_libraries -Wl,-fatal_warnings" \
  PKG_CONFIG_PATH="$PKG_CONFIG_DIRS" PKG_CONFIG_LIBDIR="$PKG_CONFIG_DIRS" \
  go build -tags "$BUILD_TAGS" -ldflags "$LDFLAGS" -o "$DAEMON_PATH" ./cmd/puddingd

PUDDING_LANGUAGE_SERVER_OUT_DIR="$OUT_DIR/language-servers" \
 PUDDING_LANGUAGE_SERVER_GOOS=darwin PUDDING_LANGUAGE_SERVER_GOARCH="$GOARCH" \
 bash "$ROOT/scripts/prepare-language-servers.sh"
