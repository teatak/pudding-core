#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SOURCE="$ROOT/assets/macos/dmg-background.svg"
OUTPUT="$ROOT/assets/macos/dmg-background.tiff"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

command -v rsvg-convert >/dev/null || {
  echo "rsvg-convert is required to render the DMG background." >&2
  exit 1
}

rsvg-convert --width 640 --height 400 "$SOURCE" --output "$WORK/dmg-background.png"
rsvg-convert --width 1280 --height 800 "$SOURCE" --output "$WORK/dmg-background@2x.png"
tiffutil -cathidpicheck "$WORK/dmg-background.png" "$WORK/dmg-background@2x.png" -out "$OUTPUT"

echo "Rendered $OUTPUT"
