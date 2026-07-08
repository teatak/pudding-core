#!/usr/bin/env bash
# Headless daemon 开发循环:
#   停掉占 dev 端口的旧实例 -> 起 puddingd + Vite -> 打印带 token 的 URL。
set -uo pipefail
cd "$(dirname "$0")/.."

DEV_PORT=9679   # dev 通道单端口(internal/home);release 是 9669
VITE_PORT=5174  # 与 .claude/launch.json / vite 一致
MODE="${1:-daemon}"
BUILDTAGS="${BUILDTAGS:-sqlite_fts5 webrtcaec}"

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

case "$MODE" in
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
    echo "usage: $0 [daemon]" >&2
    exit 2
    ;;
esac
