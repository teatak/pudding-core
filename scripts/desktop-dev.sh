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
  listening "$VITE_PORT" && return 0
  echo ">> starting Vite on :$VITE_PORT"
  nohup npm --prefix web run dev -- --port "$VITE_PORT" --strictPort >/tmp/pudding-vite.log 2>&1 &
  for _ in $(seq 1 60); do listening "$VITE_PORT" && break; sleep 0.25; done
}

echo ">> building puddingd"
go build -tags "$BUILDTAGS" -o bin/puddingd ./cmd/puddingd || exit 1

ensure_vite
stop_old_daemon

echo ">> launching desktop shell (Vite :$VITE_PORT, daemon :$DEV_PORT)"
PUDDING_DAEMON_BIN="$PWD/bin/puddingd" \
PUDDING_DAEMON_ADDR="127.0.0.1:$DEV_PORT" \
PUDDING_DEV_URL="http://127.0.0.1:$VITE_PORT" \
PUDDING_OAUTH_RETURN_SCHEME="pudding-dev" \
npm --prefix web run desktop:dev
