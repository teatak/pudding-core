#!/usr/bin/env bash
# 开发循环。两种模式,都先停掉占 dev 端口的旧实例(壳或 daemon),
# 启动用 perl setsid 进新会话彻底 detached(关终端/CI 也不被带走):
#
#   scripts/dev.sh desktop  Wails 托管壳 + Vite(HMR):壳内嵌 daemon,后台 detached
#   scripts/dev.sh daemon   headless:起 puddingd + Vite,浏览器开脚本打印的带 token URL
set -uo pipefail
cd "$(dirname "$0")/.."

DEV_PORT=9679   # dev 通道单端口(internal/home);release 是 9669
VITE_PORT=5174  # 与 .claude/launch.json / vite 一致
MODE="${1:-desktop}"
BUILDTAGS="${BUILDTAGS:-sqlite_fts5 webrtcaec}"
DEV_CODESIGN_IDENTITY="${PUDDING_DEV_CODESIGN_IDENTITY:-${PUDDING_CODESIGN_IDENTITY:-Pudding Dev Local}}"

listening() { lsof -nP -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1; }
wait_free() { for _ in $(seq 1 20); do listening "$1" || return 0; sleep 0.3; done; }

stop_old() {
  local pid
  pid=$(lsof -t -nP -iTCP:"$DEV_PORT" -sTCP:LISTEN 2>/dev/null | head -1)
  [ -z "$pid" ] && return 0
  echo ">> stopping old process on :$DEV_PORT (pid $pid)"
  kill "$pid" 2>/dev/null
  wait_free "$DEV_PORT" || { kill -9 "$pid" 2>/dev/null; wait_free "$DEV_PORT"; }
}

ensure_vite() {
  listening "$VITE_PORT" && return 0
  echo ">> starting Vite on :$VITE_PORT"
  nohup npm --prefix web run dev -- --port "$VITE_PORT" --strictPort >/tmp/pudding-vite.log 2>&1 &
  for _ in $(seq 1 60); do listening "$VITE_PORT" && break; sleep 0.25; done
}

print_codesign_help() {
  cat >&2 <<EOF
error: missing local code signing identity "$DEV_CODESIGN_IDENTITY"

Run once:
  make dev-cert

If you prefer Keychain Access, create it manually:
Create it once in Keychain Access:
  1. Keychain Access -> Certificate Assistant -> Create a Certificate...
  2. Name: Pudding Dev Local
  3. Identity Type: Self Signed Root
  4. Certificate Type: Code Signing
  5. Create, then set the certificate trust for Code Signing to Always Trust

Or set PUDDING_DEV_CODESIGN_IDENTITY to an existing local code signing identity.
EOF
}

have_codesign_identity() {
  security find-identity -v -p codesigning 2>/dev/null | grep -F "\"$DEV_CODESIGN_IDENTITY\"" >/dev/null
}

sign_desktop_app() {
  local app="$1"
  if ! have_codesign_identity; then
    print_codesign_help
    return 1
  fi
  echo ">> signing dev app with \"$DEV_CODESIGN_IDENTITY\"" >&2
  codesign --force --timestamp=none --sign "$DEV_CODESIGN_IDENTITY" "$app" >&2 || return 1
  codesign --verify --strict --verbose=2 "$app" >&2 || return 1
}

desktop_app_binary() {
  if [[ "$(uname -s)" != "Darwin" ]]; then
    echo "./bin/pudding-desktop"
    return 0
  fi

  local app=".tmp/PuddingDev.app"
  local repo="$PWD"
  rm -rf "$app"
  mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
  cp bin/pudding-desktop "$app/Contents/MacOS/pudding-desktop"
  chmod 0755 "$app/Contents/MacOS/pudding-desktop"
  if [[ -f build/macos/AppIcon.icns ]]; then
    cp build/macos/AppIcon.icns "$app/Contents/Resources/AppIcon.icns"
  fi
  cat > "$app/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDevelopmentRegion</key><string>en</string>
  <key>CFBundleExecutable</key><string>pudding-desktop</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundleIdentifier</key><string>com.teatak.pudding.dev</string>
  <key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
  <key>LSEnvironment</key>
  <dict>
    <key>PUDDING_DESKTOP_LOG</key><string>/tmp/pudding-desktop.log</string>
    <key>PUDDING_DEV_URL</key><string>http://127.0.0.1:$VITE_PORT</string>
    <key>PUDDING_REPO_DIR</key><string>$repo</string>
  </dict>
  <key>CFBundleName</key><string>Pudding Dev</string>
  <key>CFBundleDisplayName</key><string>Pudding Dev</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleVersion</key><string>0.0.0-dev</string>
  <key>CFBundleShortVersionString</key><string>0.0.0-dev</string>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>NSHighResolutionCapable</key><true/>
  <key>NSCameraUsageDescription</key><string>Pudding Dev uses the camera only when you choose to take a photo as chat context.</string>
  <key>NSMicrophoneUsageDescription</key><string>Pudding Dev uses the microphone only when you turn on voice dictation.</string>
  <key>NSSupportsAutomaticGraphicsSwitching</key><true/>
</dict>
</plist>
EOF
  plutil -lint "$app/Contents/Info.plist" >/dev/null || return 1
  sign_desktop_app "$app" || return 1
  echo "$repo/$app"
}

case "$MODE" in
  desktop)
    go build -tags "$BUILDTAGS" -o bin/pudding-desktop ./cmd/pudding-desktop || exit 1
    ensure_vite
    stop_old
    if ! desktop_bin="$(desktop_app_binary)"; then
      exit 1
    fi
    echo ">> launching pudding-desktop with Wails/Vite :$VITE_PORT (HMR; API :$DEV_PORT; log /tmp/pudding-desktop.log)"
    if [[ "$(uname -s)" == "Darwin" ]]; then
      : >/tmp/pudding-desktop.log
      open -n "$desktop_bin" --args \
        "--pudding-dev-url=http://127.0.0.1:$VITE_PORT" \
        "--pudding-repo-dir=$PWD" \
        "--pudding-desktop-log=/tmp/pudding-desktop.log"
    else
      PUDDING_DEV_URL="http://127.0.0.1:$VITE_PORT" \
        nohup perl -e 'use POSIX qw(setsid); setsid(); exec @ARGV' "$desktop_bin" \
        >/tmp/pudding-desktop.log 2>&1 &
    fi
    echo "   edit web/src for instant HMR; no shell restart needed"
    ;;
  daemon)
    go build -tags "$BUILDTAGS" -o bin/puddingd ./cmd/puddingd || exit 1
    ensure_vite
    stop_old
    echo ">> launching puddingd on :$DEV_PORT (log /tmp/puddingd.log)"
    nohup perl -e 'use POSIX qw(setsid); setsid(); exec @ARGV' ./bin/puddingd \
      >/tmp/puddingd.log 2>&1 &
    for _ in $(seq 1 40); do listening "$DEV_PORT" && break; sleep 0.25; done
    echo "   open in browser: http://127.0.0.1:$VITE_PORT/?token=$(cat "$HOME/.pudding-dev/daemon.token" 2>/dev/null)"
    ;;
  *)
    echo "usage: $0 {desktop|daemon}" >&2
    exit 2
    ;;
esac
