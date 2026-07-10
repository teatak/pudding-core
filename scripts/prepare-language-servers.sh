#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MANIFEST_DIR="$ROOT/packaging/language-servers/typescript"
OUT_DIR="$ROOT/bin/language-servers"
TYPESCRIPT_OUT="$OUT_DIR/typescript"
GOPLS_VERSION="v0.22.0"
ENSURE_ONLY=false

case "${1:-}" in
  "") ;;
  --ensure) ENSURE_ONLY=true ;;
  *)
    echo "usage: $0 [--ensure]" >&2
    exit 2
    ;;
esac

if ! command -v npm >/dev/null 2>&1; then
  echo "npm is required to prepare the bundled TypeScript language server." >&2
  exit 1
fi
if ! command -v go >/dev/null 2>&1; then
  echo "Go is required to build bundled gopls." >&2
  exit 1
fi

GOEXE="$(go env GOEXE)"
GOPLS_BIN="$OUT_DIR/gopls$GOEXE"
TYPESCRIPT_LAUNCHER="$OUT_DIR/typescript-language-server"
TYPESCRIPT_SERVER_JS="$TYPESCRIPT_OUT/node_modules/typescript-language-server/lib/cli.mjs"
if [[ "$GOEXE" == ".exe" ]]; then
  TYPESCRIPT_LAUNCHER+=".cmd"
fi

write_typescript_launcher() {
  if [[ "$GOEXE" == ".exe" ]]; then
    cat >"$TYPESCRIPT_LAUNCHER" <<'CMD'
@echo off
node "%~dp0typescript\node_modules\typescript-language-server\lib\cli.mjs" %*
CMD
    return
  fi
  cat >"$TYPESCRIPT_LAUNCHER" <<'SH'
#!/bin/sh
set -eu

SERVER_ROOT="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
exec node "$SERVER_ROOT/typescript/node_modules/typescript-language-server/lib/cli.mjs" "$@"
SH
  chmod 755 "$TYPESCRIPT_LAUNCHER"
}

GOPLS_READY=false
if [[ -x "$GOPLS_BIN" ]] && "$GOPLS_BIN" version 2>/dev/null | grep -Fq "$GOPLS_VERSION"; then
  GOPLS_READY=true
fi

TYPESCRIPT_READY=false
if [[ -f "$TYPESCRIPT_SERVER_JS" ]] \
  && [[ -f "$TYPESCRIPT_OUT/package-lock.json" ]] \
  && cmp -s "$MANIFEST_DIR/package-lock.json" "$TYPESCRIPT_OUT/package-lock.json"; then
  TYPESCRIPT_READY=true
fi

if $ENSURE_ONLY && $GOPLS_READY && $TYPESCRIPT_READY; then
  mkdir -p "$OUT_DIR"
  write_typescript_launcher
  echo "Bundled language servers are ready."
  exit 0
fi

if ! $ENSURE_ONLY || ! $TYPESCRIPT_READY; then
  rm -rf "$TYPESCRIPT_OUT"
  mkdir -p "$TYPESCRIPT_OUT"
  cp "$MANIFEST_DIR/package.json" "$MANIFEST_DIR/package-lock.json" "$TYPESCRIPT_OUT/"
  npm ci --prefix "$TYPESCRIPT_OUT" --omit=dev --ignore-scripts
fi

mkdir -p "$OUT_DIR"
if ! $ENSURE_ONLY || ! $GOPLS_READY; then
  GOBIN="$OUT_DIR" GOTOOLCHAIN=local go install "golang.org/x/tools/gopls@$GOPLS_VERSION"
fi
GOPLS_MODULE_DIR="$(go env GOMODCACHE)/golang.org/x/tools/gopls@$GOPLS_VERSION"
rm -f "$OUT_DIR/gopls.LICENSE"
cp "$GOPLS_MODULE_DIR/LICENSE" "$OUT_DIR/gopls.LICENSE"

write_typescript_launcher

"$GOPLS_BIN" version
"$TYPESCRIPT_LAUNCHER" --version

echo "Prepared bundled language servers in $OUT_DIR"
