#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MANIFEST_DIR="$ROOT/packaging/language-servers/typescript"
OUT_DIR="$ROOT/bin/language-servers"
TYPESCRIPT_OUT="$OUT_DIR/typescript"
GOPLS_VERSION="v0.22.0"

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
TYPESCRIPT_BIN="$TYPESCRIPT_OUT/node_modules/.bin/typescript-language-server"
if [[ "$GOEXE" == ".exe" ]]; then
  TYPESCRIPT_BIN+=".cmd"
fi

rm -rf "$TYPESCRIPT_OUT"
mkdir -p "$TYPESCRIPT_OUT"
cp "$MANIFEST_DIR/package.json" "$MANIFEST_DIR/package-lock.json" "$TYPESCRIPT_OUT/"
npm ci --prefix "$TYPESCRIPT_OUT" --omit=dev --ignore-scripts

mkdir -p "$OUT_DIR"
GOBIN="$OUT_DIR" GOTOOLCHAIN=local go install "golang.org/x/tools/gopls@$GOPLS_VERSION"
GOPLS_MODULE_DIR="$(go env GOMODCACHE)/golang.org/x/tools/gopls@$GOPLS_VERSION"
rm -f "$OUT_DIR/gopls.LICENSE"
cp "$GOPLS_MODULE_DIR/LICENSE" "$OUT_DIR/gopls.LICENSE"

"$GOPLS_BIN" version
"$TYPESCRIPT_BIN" --version

echo "Prepared bundled language servers in $OUT_DIR"
