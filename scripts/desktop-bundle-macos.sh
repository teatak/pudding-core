#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "desktop-bundle currently supports macOS only." >&2
  exit 1
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ ! -x "$ROOT/node_modules/.bin/electron-builder" ]]; then
  echo "electron-builder not found. Run: npm install" >&2
  exit 1
fi

cd "$ROOT"
npm run desktop:package
