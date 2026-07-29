#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
app_icon_svg="${repo_root}/assets/macos/AppIcon.svg"
tray_icon_svg="${repo_root}/assets/macos/TrayTemplate.svg"
web_icon_svg="${repo_root}/assets/brand/PuddingWebIcon.svg"
open_graph_svg="${repo_root}/assets/brand/PuddingOpenGraph.svg"

for command_name in rsvg-convert node; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "missing required command: ${command_name}" >&2
    exit 1
  fi
done

rsvg-convert -w 256 -h 256 \
  -o "${repo_root}/workers/oauth/public/logo.png" \
  "${web_icon_svg}"

rsvg-convert -w 1200 -h 630 \
  -o "${repo_root}/workers/oauth/public/og.png" \
  "${open_graph_svg}"

rsvg-convert -w 1024 -h 1024 \
  -o "${repo_root}/assets/macos/AppIcon.png" \
  "${app_icon_svg}"

rsvg-convert -w 36 -h 36 \
  -o "${repo_root}/assets/macos/TrayTemplate.png" \
  "${tray_icon_svg}"

render_dir="$(mktemp -d)"
trap 'rm -rf "${render_dir}"' EXIT
icon_png_dir="${render_dir}/AppIcon"
mkdir -p "${icon_png_dir}"

for size in 16 32 64 128 256 512 1024; do
  rsvg-convert -w "${size}" -h "${size}" \
    -o "${icon_png_dir}/icon_${size}x${size}.png" \
    "${app_icon_svg}"
done

node "${repo_root}/scripts/build-icns.mjs" \
  "${icon_png_dir}" \
  "${repo_root}/assets/macos/AppIcon.icns"

bash "${repo_root}/packaging/macos/render-dmg-background.sh"
