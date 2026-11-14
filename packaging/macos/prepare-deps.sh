#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
[[ $# == 1 && ( "$1" == arm64 || "$1" == x64 ) ]] || { echo "usage: $0 <arm64|x64>" >&2; exit 2; }
ARCH="$1"
CPU=x86_64
MACH_ARCH=x86_64
MIN_MACOS_VERSION=12.0
if [[ "$ARCH" == arm64 ]]; then CPU=aarch64; MACH_ARCH=arm64; MIN_MACOS_VERSION=14.0; fi
CACHE_DIR="$ROOT/dist/cache/macos-$ARCH"
WORK_DIR="$ROOT/dist/build/macos-$ARCH"
PREFIX="$ROOT/dist/deps/macos-$ARCH"
MARKER="$PREFIX/.pudding-deps"
export SDKROOT="${SDKROOT:-$(xcrun --sdk macosx --show-sdk-path)}"
export MACOSX_DEPLOYMENT_TARGET="$MIN_MACOS_VERSION"
WEBRTC_VERSION="1.3"
WEBRTC_SHA256="95552fc17faa0202133707bbb3727e8c2cf64d4266fe31bfdb2298d769c1db75"
WEBRTC_URL="https://gstreamer.freedesktop.org/src/mirror/webrtc-audio-processing/webrtc-audio-processing-${WEBRTC_VERSION}.tar.gz"
PORTAUDIO_VERSION="19.7.0"
PORTAUDIO_SHA256="5af29ba58bbdbb7bbcefaaecc77ec8fc413f0db6f4c4e286c40c3e1b83174fa0"
PORTAUDIO_URL="https://github.com/PortAudio/portaudio/archive/refs/tags/v${PORTAUDIO_VERSION}.tar.gz"
BRIDGE_HASH="$(shasum -a 256 "$ROOT/internal/audio/dsp/webrtc_bridge/src/kopi_webrtc_aec_bridge.cc" "$ROOT/internal/audio/dsp/webrtc_bridge/include/kopi_webrtc_aec_bridge.h")"
EXPECTED_MARKER="recipe=4 arch=${ARCH} webrtc=${WEBRTC_VERSION} portaudio=${PORTAUDIO_VERSION} min-macos=${MIN_MACOS_VERSION} bridge=${BRIDGE_HASH}"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "desktop dependencies can only be prepared on macOS." >&2
  exit 1
fi

for command in clang clang++ curl libtool meson ninja pkg-config shasum; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "$command is required to prepare desktop dependencies." >&2
    exit 1
  fi
done

bridge_archive="$PREFIX/lib/libkopi_webrtc_aec_bridge.a"
portaudio_archive="$PREFIX/lib/libportaudio.a"
if [[ "$ARCH" == arm64 ]]; then bash "$ROOT/scripts/prepare-abseil.sh"; fi
install_legacy_x64_bridge() {
  # Preserve Intel development setup, without replacing the tracked arm64 archive.
  if [[ "$ARCH" == x64 ]]; then
    local bridge_dir="$ROOT/internal/audio/dsp/webrtc_bridge/lib/darwin-amd64"
    mkdir -p "$bridge_dir"
    cp -f "$bridge_archive" "$bridge_dir/"
  fi
}
if [[ -f "$MARKER" ]] \
  && [[ "$(cat "$MARKER")" == "$EXPECTED_MARKER" ]] \
  && [[ -f "$bridge_archive" ]] \
  && [[ -f "$portaudio_archive" ]] \
  && lipo -archs "$bridge_archive" | grep -qw "$MACH_ARCH" \
  && lipo -archs "$portaudio_archive" | grep -qw "$MACH_ARCH"; then
  install_legacy_x64_bridge
  echo "macOS $ARCH dependencies are ready in $PREFIX"
  exit 0
fi

download() {
  local url="$1"
  local sha256="$2"
  local destination="$3"

  mkdir -p "$(dirname "$destination")"
  if [[ ! -f "$destination" ]] || ! echo "$sha256  $destination" | shasum -a 256 -c - >/dev/null 2>&1; then
    curl --fail --location --output "$destination" "$url"
  fi
  echo "$sha256  $destination" | shasum -a 256 -c -
}

webrtc_archive="$CACHE_DIR/webrtc-audio-processing-${WEBRTC_VERSION}.tar.gz"
portaudio_source_archive="$CACHE_DIR/portaudio-${PORTAUDIO_VERSION}.tar.gz"
download "$WEBRTC_URL" "$WEBRTC_SHA256" "$webrtc_archive"
download "$PORTAUDIO_URL" "$PORTAUDIO_SHA256" "$portaudio_source_archive"

rm -rf "$WORK_DIR" "$PREFIX"
mkdir -p "$WORK_DIR/sources" "$PREFIX"
tar -xzf "$webrtc_archive" -C "$WORK_DIR/sources"
tar -xzf "$portaudio_source_archive" -C "$WORK_DIR/sources"

cross_file="$WORK_DIR/$ARCH-darwin.ini"
cat >"$cross_file" <<EOF
[binaries]
c = 'clang'
cpp = 'clang++'
ar = 'ar'
strip = 'strip'
pkg-config = 'pkg-config'

[properties]
needs_exe_wrapper = true

[built-in options]
c_args = ['-arch', '${MACH_ARCH}', '-mmacosx-version-min=${MIN_MACOS_VERSION}']
cpp_args = ['-arch', '${MACH_ARCH}', '-mmacosx-version-min=${MIN_MACOS_VERSION}']
c_link_args = ['-arch', '${MACH_ARCH}', '-mmacosx-version-min=${MIN_MACOS_VERSION}']
cpp_link_args = ['-arch', '${MACH_ARCH}', '-mmacosx-version-min=${MIN_MACOS_VERSION}']

[host_machine]
system = 'darwin'
cpu_family = '${CPU}'
cpu = '${CPU}'
endian = 'little'
EOF

webrtc_source="$WORK_DIR/sources/webrtc-audio-processing-${WEBRTC_VERSION}"
webrtc_build="$WORK_DIR/webrtc-build"
dependency_pc="$PREFIX/lib/pkgconfig"
wrap_mode=forcefallback
bridge_includes=()
if [[ "$ARCH" == arm64 ]]; then
  dependency_pc="$ROOT/dist/deps/abseil/lib/pkgconfig"
  wrap_mode=nofallback
  bridge_includes=(-I"$ROOT/dist/deps/abseil/include")
fi
PKG_CONFIG_PATH="$dependency_pc" PKG_CONFIG_LIBDIR="$dependency_pc" meson setup \
  "$webrtc_build" \
  "$webrtc_source" \
  --cross-file "$cross_file" \
  --prefix "$PREFIX" \
  --libdir lib \
  --buildtype release \
  --default-library static \
  --wrap-mode "$wrap_mode"
meson compile -C "$webrtc_build"
meson install -C "$webrtc_build"

webrtc_library="$PREFIX/lib/libwebrtc-audio-processing-1.a"
if [[ ! -f "$webrtc_library" ]]; then
  echo "WebRTC $ARCH archive was not installed: $webrtc_library" >&2
  exit 1
fi

bridge_object="$WORK_DIR/kopi_webrtc_aec_bridge.o"
clang++ \
  -arch "$MACH_ARCH" \
  -mmacosx-version-min="$MIN_MACOS_VERSION" \
  -std=c++17 \
  -I"$webrtc_source/webrtc" \
  -I"$PREFIX/include" \
  "${bridge_includes[@]}" \
  -I"$ROOT/internal/audio/dsp/webrtc_bridge/include" \
  -c "$ROOT/internal/audio/dsp/webrtc_bridge/src/kopi_webrtc_aec_bridge.cc" \
  -o "$bridge_object"
absl_archives=()
if [[ "$ARCH" == x64 ]]; then
  absl_build="$webrtc_build/subprojects/abseil-cpp-20230125.1"
  absl_archives=("$absl_build"/libabsl_*.a)
  if [[ ! -f "${absl_archives[0]}" ]]; then
    echo "Abseil x64 archives were not built in $absl_build" >&2
    exit 1
  fi
fi
libtool -static -o "$bridge_archive" "$bridge_object" "$webrtc_library" "${absl_archives[@]}"

portaudio_source="$WORK_DIR/sources/portaudio-${PORTAUDIO_VERSION}"
portaudio_build="$WORK_DIR/portaudio-build"
# PortAudio 19.7.0 forces -Werror on macOS. Newer Xcode releases add
# diagnostics that are harmless for this pinned source but break the build.
sed -i '' 's/ -Wno-deprecated -Werror/ -Wno-deprecated/' "$portaudio_source/configure"
mkdir -p "$portaudio_build"
(
  cd "$portaudio_build"
  env \
    CC=clang \
    CXX=clang++ \
    CFLAGS="-arch $MACH_ARCH -mmacosx-version-min=$MIN_MACOS_VERSION" \
    CXXFLAGS="-arch $MACH_ARCH -mmacosx-version-min=$MIN_MACOS_VERSION" \
    LDFLAGS="-arch $MACH_ARCH -mmacosx-version-min=$MIN_MACOS_VERSION" \
    "$portaudio_source/configure" \
      --host="$CPU-apple-darwin" \
      --prefix="$PREFIX" \
      --disable-mac-universal \
      --disable-shared \
      --enable-static
  make -j"$(sysctl -n hw.logicalcpu)"
  make install
)

for archive in "$bridge_archive" "$portaudio_archive"; do
  if [[ ! -f "$archive" ]] || ! lipo -archs "$archive" | grep -qw "$MACH_ARCH"; then
    echo "expected a $MACH_ARCH archive: $archive" >&2
    exit 1
  fi
done

install_legacy_x64_bridge
printf '%s\n' "$EXPECTED_MARKER" >"$MARKER"
echo "Prepared macOS $ARCH dependencies in $PREFIX"
