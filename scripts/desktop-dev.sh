#!/usr/bin/env bash
set -uo pipefail
cd "$(dirname "$0")/.."

DEV_PORT=9679
VITE_PORT=5174
BUILDTAGS="${BUILDTAGS:-sqlite_fts5 webrtcaec}"

listening() { lsof -nP -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1; }
wait_free() { for _ in $(seq 1 20); do listening "$1" || return 0; sleep 0.3; done; }

stop_old_daemon() {
  local pid
  pid=$(lsof -t -nP -iTCP:"$DEV_PORT" -sTCP:LISTEN 2>/dev/null | head -1)
  [ -z "$pid" ] && return 0
  echo ">> stopping old daemon on :$DEV_PORT (pid $pid)"
  kill "$pid" 2>/dev/null
  wait_free "$DEV_PORT" || { kill -9 "$pid" 2>/dev/null; wait_free "$DEV_PORT"; }
}

ensure_vite() {
  local pid vite_cwd
  pid=$(lsof -t -nP -iTCP:"$VITE_PORT" -sTCP:LISTEN 2>/dev/null | head -1)
  if [ -n "$pid" ]; then
    vite_cwd=$(lsof -a -p "$pid" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -1)
    if [ "$vite_cwd" = "$PWD/web" ]; then
      return 0
    fi
    echo ">> stopping Vite from another checkout on :$VITE_PORT (pid $pid)"
    kill "$pid" 2>/dev/null
    wait_free "$VITE_PORT" || { kill -9 "$pid" 2>/dev/null; wait_free "$VITE_PORT"; }
  fi
  echo ">> starting Vite on :$VITE_PORT"
  nohup npm --prefix web run dev -- --port "$VITE_PORT" --strictPort >/tmp/pudding-vite.log 2>&1 &
  for _ in $(seq 1 60); do listening "$VITE_PORT" && return 0; sleep 0.25; done
  echo "Vite failed to start; see /tmp/pudding-vite.log" >&2
  return 1
}

echo ">> building puddingd"
go build -tags "$BUILDTAGS" -o bin/puddingd ./cmd/puddingd || exit 1
if [ "$(uname -s)" = "Darwin" ]; then
  # Go's linker signature can be rejected when Electron launches the freshly
  # rebuilt binary. Replace it with an explicit local ad-hoc signature.
  codesign --force --sign - --identifier com.teatak.pudding.dev.daemon bin/puddingd || exit 1
fi

ensure_vite || exit 1
stop_old_daemon

echo ">> launching desktop shell (Vite :$VITE_PORT, daemon :$DEV_PORT)"
PUDDING_DAEMON_BIN="$PWD/bin/puddingd" \
PUDDING_DAEMON_ADDR="127.0.0.1:$DEV_PORT" \
PUDDING_DEV_URL="http://127.0.0.1:$VITE_PORT" \
PUDDING_OAUTH_RETURN_SCHEME="pudding-dev" \
node scripts/register-dev-oauth-protocol.cjs || exit 1
PUDDING_DAEMON_BIN="$PWD/bin/puddingd" \
PUDDING_DAEMON_ADDR="127.0.0.1:$DEV_PORT" \
PUDDING_DEV_URL="http://127.0.0.1:$VITE_PORT" \
PUDDING_OAUTH_RETURN_SCHEME="pudding-dev" \
npm --prefix web run desktop:dev
