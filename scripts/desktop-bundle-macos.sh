#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "desktop-bundle currently supports macOS only." >&2
  exit 1
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP_NAME="${PUDDING_APP_NAME:-Pudding}"
BUNDLE_ID="${PUDDING_BUNDLE_ID:-com.teatak.pudding}"
VERSION="${PUDDING_APP_VERSION:-0.1.0}"
ELECTRON_APP="$ROOT/web/node_modules/electron/dist/Electron.app"
DAEMON_BIN="$ROOT/bin/puddingd"
LANGUAGE_SERVERS_DIR="$ROOT/bin/language-servers"
GOPLS_BIN="$LANGUAGE_SERVERS_DIR/gopls"
TYPESCRIPT_SERVER_DIR="$LANGUAGE_SERVERS_DIR/typescript"
TYPESCRIPT_SERVER_JS="$TYPESCRIPT_SERVER_DIR/node_modules/typescript-language-server/lib/cli.mjs"
ICON="$ROOT/assets/macos/AppIcon.icns"
TRAY_ICON="$ROOT/assets/macos/TrayTemplate.png"
OUT_DIR="$ROOT/dist"
APP_PATH="$OUT_DIR/$APP_NAME.app"
APP_RESOURCES="$APP_PATH/Contents/Resources"
APP_ROOT="$APP_RESOURCES/app"
INFO_PLIST="$APP_PATH/Contents/Info.plist"
PLIST_BUDDY="/usr/libexec/PlistBuddy"

if [[ ! -d "$ELECTRON_APP" ]]; then
  echo "Electron.app not found: $ELECTRON_APP" >&2
  echo "Run: cd web && npm install" >&2
  exit 1
fi

if [[ ! -x "$DAEMON_BIN" ]]; then
  echo "daemon binary not found: $DAEMON_BIN" >&2
  echo "Run: make desktop-release" >&2
  exit 1
fi
if [[ ! -x "$GOPLS_BIN" ]]; then
  echo "bundled gopls not found: $GOPLS_BIN" >&2
  echo "Run: make language-servers" >&2
  exit 1
fi
if [[ ! -f "$TYPESCRIPT_SERVER_JS" ]]; then
  echo "bundled TypeScript language server not found: $TYPESCRIPT_SERVER_JS" >&2
  echo "Run: make language-servers" >&2
  exit 1
fi
if [[ ! -f "$ICON" ]]; then
  echo "app icon not found: $ICON" >&2
  exit 1
fi
if [[ ! -f "$TRAY_ICON" ]]; then
  echo "tray template icon not found: $TRAY_ICON" >&2
  exit 1
fi

rm -rf "$APP_PATH"
mkdir -p "$OUT_DIR"
cp -R "$ELECTRON_APP" "$APP_PATH"

rm -rf "$APP_ROOT"
mkdir -p "$APP_ROOT/bin" "$APP_ROOT/desktop" "$APP_ROOT/language-servers/typescript"
cp "$ROOT"/electron/*.cjs "$APP_ROOT/desktop/"
cp "$DAEMON_BIN" "$APP_ROOT/bin/puddingd"
cp "$GOPLS_BIN" "$APP_ROOT/language-servers/gopls"
cp "$LANGUAGE_SERVERS_DIR/gopls.LICENSE" "$APP_ROOT/language-servers/gopls.LICENSE"
cp "$TYPESCRIPT_SERVER_DIR/package.json" "$TYPESCRIPT_SERVER_DIR/package-lock.json" "$APP_ROOT/language-servers/typescript/"
cp -R "$TYPESCRIPT_SERVER_DIR/node_modules" "$APP_ROOT/language-servers/typescript/"
bash "$ROOT/scripts/macos-bundle-dylibs.sh" "$APP_ROOT/bin/puddingd" "$APP_ROOT/lib"

cat >"$APP_ROOT/language-servers/typescript-language-server" <<'SH'
#!/bin/sh
set -eu

SERVER_ROOT="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
export ELECTRON_RUN_AS_NODE=1
exec "$SERVER_ROOT/node" "$SERVER_ROOT/typescript/node_modules/typescript-language-server/lib/cli.mjs" "$@"
SH
chmod 755 "$APP_ROOT/language-servers/typescript-language-server"

cat >"$APP_ROOT/package.json" <<JSON
{
  "name": "pudding",
  "version": "$VERSION",
  "main": "desktop/main.cjs"
}
JSON

cp "$ICON" "$APP_RESOURCES/AppIcon.icns"
cp "$TRAY_ICON" "$APP_RESOURCES/TrayTemplate.png"
rm -f "$APP_RESOURCES/electron.icns"

if [[ -x "$APP_PATH/Contents/MacOS/Electron" ]]; then
  mv "$APP_PATH/Contents/MacOS/Electron" "$APP_PATH/Contents/MacOS/$APP_NAME"
fi
ln -s "../../../MacOS/$APP_NAME" "$APP_ROOT/language-servers/node"

"$PLIST_BUDDY" -c "Set :CFBundleExecutable $APP_NAME" "$INFO_PLIST"
"$PLIST_BUDDY" -c "Set :CFBundleName $APP_NAME" "$INFO_PLIST"
"$PLIST_BUDDY" -c "Set :CFBundleDisplayName $APP_NAME" "$INFO_PLIST" 2>/dev/null \
  || "$PLIST_BUDDY" -c "Add :CFBundleDisplayName string $APP_NAME" "$INFO_PLIST"
"$PLIST_BUDDY" -c "Set :CFBundleIdentifier $BUNDLE_ID" "$INFO_PLIST"
"$PLIST_BUDDY" -c "Set :CFBundleShortVersionString $VERSION" "$INFO_PLIST"
"$PLIST_BUDDY" -c "Set :CFBundleVersion $VERSION" "$INFO_PLIST"
"$PLIST_BUDDY" -c "Set :CFBundleIconFile AppIcon.icns" "$INFO_PLIST" 2>/dev/null \
  || "$PLIST_BUDDY" -c "Add :CFBundleIconFile string AppIcon.icns" "$INFO_PLIST"
"$PLIST_BUDDY" -c "Set :NSMicrophoneUsageDescription Pudding uses the microphone for local dictation." "$INFO_PLIST" 2>/dev/null \
  || "$PLIST_BUDDY" -c "Add :NSMicrophoneUsageDescription string Pudding uses the microphone for local dictation." "$INFO_PLIST"
"$PLIST_BUDDY" -c "Delete :CFBundleURLTypes" "$INFO_PLIST" 2>/dev/null || true
"$PLIST_BUDDY" -c "Add :CFBundleURLTypes array" "$INFO_PLIST"
"$PLIST_BUDDY" -c "Add :CFBundleURLTypes:0 dict" "$INFO_PLIST"
"$PLIST_BUDDY" -c "Add :CFBundleURLTypes:0:CFBundleURLName string $BUNDLE_ID" "$INFO_PLIST"
"$PLIST_BUDDY" -c "Add :CFBundleURLTypes:0:CFBundleURLSchemes array" "$INFO_PLIST"
"$PLIST_BUDDY" -c "Add :CFBundleURLTypes:0:CFBundleURLSchemes:0 string pudding" "$INFO_PLIST"

touch "$INFO_PLIST" "$APP_PATH"

if command -v codesign >/dev/null 2>&1; then
  codesign --force --deep --sign - "$APP_PATH" >/dev/null 2>&1
fi

echo "Created $APP_PATH"
