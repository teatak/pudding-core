#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CACHE_DIR="$ROOT/dist/cache/macos-x64"
WORK_DIR="$ROOT/dist/build/macos-x64"
PREFIX="$ROOT/dist/deps/macos-x64"
BRIDGE_DIR="$ROOT/internal/audio/dsp/webrtc_bridge/lib/darwin-amd64"
MARKER="$PREFIX/.pudding-x64-deps"
MIN_MACOS_VERSION="12.0"
WEBRTC_VERSION="1.3"
WEBRTC_SHA256="95552fc17faa0202133707bbb3727e8c2cf64d4266fe31bfdb2298d769c1db75"
WEBRTC_URL="https://gstreamer.freedesktop.org/src/mirror/webrtc-audio-processing/webrtc-audio-processing-${WEBRTC_VERSION}.tar.gz"
PORTAUDIO_VERSION="19.7.0"
PORTAUDIO_SHA256="5af29ba58bbdbb7bbcefaaecc77ec8fc413f0db6f4c4e286c40c3e1b83174fa0"
PORTAUDIO_URL="https://github.com/PortAudio/portaudio/archive/refs/tags/v${PORTAUDIO_VERSION}.tar.gz"
EXPECTED_MARKER="recipe=3 webrtc=${WEBRTC_VERSION} portaudio=${PORTAUDIO_VERSION} min-macos=${MIN_MACOS_VERSION}"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "x64 desktop dependencies can only be prepared on macOS." >&2
  exit 1
fi

for command in clang clang++ curl libtool meson ninja pkg-config shasum; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "$command is required to prepare x64 desktop dependencies." >&2
    exit 1
  fi
done

bridge_archive="$PREFIX/lib/libkopi_webrtc_aec_bridge.a"
portaudio_archive="$PREFIX/lib/libportaudio.a"
if [[ -f "$MARKER" ]] \
  && [[ "$(cat "$MARKER")" == "$EXPECTED_MARKER" ]] \
  && [[ -f "$bridge_archive" ]] \
  && [[ -f "$portaudio_archive" ]] \
  && lipo -archs "$bridge_archive" | grep -qw x86_64 \
  && lipo -archs "$portaudio_archive" | grep -qw x86_64; then
  mkdir -p "$BRIDGE_DIR"
  cp -f "$bridge_archive" "$BRIDGE_DIR/"
  echo "macOS x64 dependencies are ready in $PREFIX"
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

cross_file="$WORK_DIR/x86_64-darwin.ini"
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
c_args = ['-arch', 'x86_64', '-mmacosx-version-min=${MIN_MACOS_VERSION}']
cpp_args = ['-arch', 'x86_64', '-mmacosx-version-min=${MIN_MACOS_VERSION}']
c_link_args = ['-arch', 'x86_64', '-mmacosx-version-min=${MIN_MACOS_VERSION}']
cpp_link_args = ['-arch', 'x86_64', '-mmacosx-version-min=${MIN_MACOS_VERSION}']

[host_machine]
system = 'darwin'
cpu_family = 'x86_64'
cpu = 'x86_64'
endian = 'little'
EOF

webrtc_source="$WORK_DIR/sources/webrtc-audio-processing-${WEBRTC_VERSION}"
webrtc_build="$WORK_DIR/webrtc-build"
PKG_CONFIG_LIBDIR="$PREFIX/lib/pkgconfig" meson setup \
  "$webrtc_build" \
  "$webrtc_source" \
  --cross-file "$cross_file" \
  --prefix "$PREFIX" \
  --libdir lib \
  --buildtype release \
  --default-library static \
  --wrap-mode forcefallback
meson compile -C "$webrtc_build"
meson install -C "$webrtc_build"

webrtc_library="$PREFIX/lib/libwebrtc-audio-processing-1.a"
if [[ ! -f "$webrtc_library" ]]; then
  echo "WebRTC x64 archive was not installed: $webrtc_library" >&2
  exit 1
fi

bridge_object="$WORK_DIR/kopi_webrtc_aec_bridge.o"
clang++ \
  -arch x86_64 \
  -mmacosx-version-min="$MIN_MACOS_VERSION" \
  -std=c++17 \
  -I"$webrtc_source/webrtc" \
  -I"$PREFIX/include" \
  -I"$ROOT/internal/audio/dsp/webrtc_bridge/include" \
  -c "$ROOT/internal/audio/dsp/webrtc_bridge/src/kopi_webrtc_aec_bridge.cc" \
  -o "$bridge_object"
absl_build="$webrtc_build/subprojects/abseil-cpp-20230125.1"
absl_archives=("$absl_build"/libabsl_*.a)
if [[ ! -f "${absl_archives[0]}" ]]; then
  echo "Abseil x64 archives were not built in $absl_build" >&2
  exit 1
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
    CFLAGS="-arch x86_64 -mmacosx-version-min=$MIN_MACOS_VERSION" \
    CXXFLAGS="-arch x86_64 -mmacosx-version-min=$MIN_MACOS_VERSION" \
    LDFLAGS="-arch x86_64 -mmacosx-version-min=$MIN_MACOS_VERSION" \
    "$portaudio_source/configure" \
      --host=x86_64-apple-darwin \
      --prefix="$PREFIX" \
      --disable-mac-universal \
      --disable-shared \
      --enable-static
  make -j"$(sysctl -n hw.logicalcpu)"
  make install
)

for archive in "$bridge_archive" "$portaudio_archive"; do
  if [[ ! -f "$archive" ]] || ! lipo -archs "$archive" | grep -qw x86_64; then
    echo "expected an x86_64 archive: $archive" >&2
    exit 1
  fi
done

mkdir -p "$BRIDGE_DIR"
cp -f "$bridge_archive" "$BRIDGE_DIR/"
printf '%s\n' "$EXPECTED_MARKER" >"$MARKER"
echo "Prepared macOS x64 dependencies in $PREFIX"
