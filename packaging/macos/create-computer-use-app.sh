#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 4 || $# -gt 5 ]]; then
  echo "usage: $0 <binary> <output-app> <bundle-id> <display-name> [info-plist]" >&2
  exit 2
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BINARY="$1"
OUTPUT_APP="$2"
BUNDLE_ID="$3"
DISPLAY_NAME="$4"
SOURCE_INFO_PLIST="${5:-$ROOT/native/macos/computer-use-helper/Sources/PuddingComputerUseHelper/Info.plist}"

if [[ ! -f "$BINARY" ]] || [[ ! -f "$SOURCE_INFO_PLIST" ]] || [[ "$OUTPUT_APP" != *.app ]] || [[ -z "$BUNDLE_ID" ]] || [[ -z "$DISPLAY_NAME" ]]; then
  echo "invalid Computer Use app bundle arguments" >&2
  exit 2
fi

CONTENTS="$OUTPUT_APP/Contents"
EXECUTABLE_NAME="$(basename "$BINARY")"
EXECUTABLE="$CONTENTS/MacOS/$EXECUTABLE_NAME"
INFO_PLIST="$CONTENTS/Info.plist"

rm -rf "$OUTPUT_APP"
mkdir -p "$CONTENTS/MacOS" "$CONTENTS/Resources"
cp "$BINARY" "$EXECUTABLE"
cp "$SOURCE_INFO_PLIST" "$INFO_PLIST"
printf 'APPL????' >"$CONTENTS/PkgInfo"
chmod 0755 "$EXECUTABLE"
plutil -replace CFBundleExecutable -string "$EXECUTABLE_NAME" "$INFO_PLIST"
plutil -replace CFBundleIdentifier -string "$BUNDLE_ID" "$INFO_PLIST"
plutil -replace CFBundleDisplayName -string "$DISPLAY_NAME" "$INFO_PLIST"
plutil -replace CFBundleName -string "$DISPLAY_NAME" "$INFO_PLIST"
