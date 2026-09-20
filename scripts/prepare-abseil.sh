#!/usr/bin/env bash
set -euo pipefail

# The checked-in arm64 WebRTC bridge references absl::lts_20250512 symbols.
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="20250512.1"
SHA256="9b7a064305e9fd94d124ffa6cc358592eb42b5da588fb4e07d09254aa40086db"
PREFIX="$ROOT/dist/deps/abseil"
ARCHIVE="$ROOT/dist/cache/abseil-${VERSION}.tar.gz"
SOURCE="$ROOT/dist/build/abseil-${VERSION}"
MARKER="$PREFIX/.pudding-abseil"
RECIPE="version=${VERSION} arch=arm64 cxx=17 static=1 min-macos=14.0"

[[ "$(uname -s)" == Darwin && "$(uname -m)" == arm64 ]] || {
  echo "This dependency is for the macOS arm64 WebRTC bridge." >&2
  exit 1
}
if [[ -f "$MARKER" && "$(cat "$MARKER")" == "$RECIPE" && -f "$PREFIX/lib/libabsl_base.a" && -f "$PREFIX/lib/pkgconfig/absl_base.pc" ]]; then
  echo "Abseil ${VERSION} is ready in $PREFIX"
  exit 0
fi
for command in cmake clang++ curl shasum; do
  command -v "$command" >/dev/null || { echo "$command is required" >&2; exit 1; }
done

mkdir -p "$(dirname "$ARCHIVE")"
if [[ ! -f "$ARCHIVE" ]]; then
  curl --fail --location --output "$ARCHIVE" "https://github.com/abseil/abseil-cpp/archive/refs/tags/${VERSION}.tar.gz"
fi
echo "$SHA256  $ARCHIVE" | shasum -a 256 -c -
rm -rf "$SOURCE" "$PREFIX"
mkdir -p "$SOURCE"
tar -xzf "$ARCHIVE" --strip-components=1 -C "$SOURCE"
cmake -S "$SOURCE" -B "$SOURCE/build" \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_INSTALL_PREFIX="$PREFIX" \
  -DCMAKE_INSTALL_LIBDIR=lib \
  -DCMAKE_CXX_STANDARD=17 \
  -DCMAKE_OSX_ARCHITECTURES=arm64 \
  -DCMAKE_OSX_SYSROOT="${SDKROOT:-$(xcrun --sdk macosx --show-sdk-path)}" \
  -DCMAKE_OSX_DEPLOYMENT_TARGET=14.0 \
  -DBUILD_SHARED_LIBS=OFF \
  -DABSL_BUILD_TESTING=OFF \
  -DABSL_ENABLE_INSTALL=ON \
  -DABSL_PROPAGATE_CXX_STD=ON
cmake --build "$SOURCE/build" --parallel "$(sysctl -n hw.logicalcpu)"
cmake --install "$SOURCE/build"
printf '%s\n' "$RECIPE" > "$MARKER"
echo "Prepared Abseil ${VERSION}. Add $PREFIX/lib/pkgconfig to PKG_CONFIG_PATH."
