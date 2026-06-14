#!/usr/bin/env bash
# 开发循环。两种模式,都先停掉占 dev 端口的旧实例(壳或 daemon),
# 启动用 perl setsid 进新会话彻底 detached(关终端/CI 也不被带走):
#
#   scripts/dev.sh desktop   壳直连 Vite(HMR):壳内嵌 daemon,改 web/src 即时生效
#   scripts/dev.sh daemon     headless:起 puddingd + Vite,浏览器开脚本打印的带 token URL
set -uo pipefail
cd "$(dirname "$0")/.."

DEV_PORT=9679   # dev 通道单端口(internal/home);release 是 9669
VITE_PORT=5174  # 与 .claude/launch.json / vite 一致
MODE="${1:-desktop}"

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
  desktop)
    go build -o bin/pudding-desktop ./cmd/pudding-desktop || exit 1
    ensure_vite
    stop_old
    echo ">> launching pudding-desktop against Vite :$VITE_PORT (HMR; log /tmp/pudding-desktop.log)"
    PUDDING_DEV_URL="http://127.0.0.1:$VITE_PORT" \
      nohup perl -e 'use POSIX qw(setsid); setsid(); exec @ARGV' ./bin/pudding-desktop \
      >/tmp/pudding-desktop.log 2>&1 &
    echo "   edit web/src for instant HMR; no shell restart needed"
    ;;
  daemon)
    go build -o bin/puddingd ./cmd/puddingd || exit 1
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
