#!/usr/bin/env bash
# 竞态冒烟门禁：用竞态检测器构建桌面 playground，在 xvfb 下极快打字，
# 断言 stderr 无 DATA RACE / concurrent map 致命错误。
#
# 背景：widgets 的光标闪烁曾由后台 ticker 直接读写 blinkOn，与 UI 线程打字
# 时的依赖图重建并发，偶发 fatal error: concurrent map writes（信号核心是
# 单线程设计，后台写入必须经 Signal.safe/Dispatch 投递）。该缺陷靠人工极快
# 打字偶然复现；本脚本把它固化成可重复的自动门禁。
#
# 用法：bash scripts/race_typing_smoke.sh [批次] [每批字符]
#   例：bash scripts/race_typing_smoke.sh 30 36
# 退出码：0=无竞态（PASS），1=检测到竞态（FAIL），2=环境缺依赖/构建失败。
#
# 依赖：go、xvfb-run、xdotool、openbox（窗口管理器，可选但推荐）。
# 不修改任何源码或产物；所有临时文件落在 $TMPDIR。

set -u
BATCHES="${1:-30}"
PER_BATCH="${2:-36}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$(mktemp -u /tmp/gox-race-desktop.XXXXXX)"
WORK="$(mktemp -d /tmp/gox-race-typing.XXXXXX)"
ERR_LOG="$WORK/stderr.log"

cleanup() {
  [ -n "${APP_PID:-}" ] && kill "$APP_PID" 2>/dev/null || true
  [ -n "${WM_PID:-}" ] && kill "$WM_PID" 2>/dev/null || true
  rm -f "$BIN"
  rm -rf "$WORK"
}
trap cleanup EXIT

fail_env() { echo "SMOKE_SKIP: $1" >&2; exit 2; }

command -v go >/dev/null 2>&1        || fail_env "缺少 go"
command -v xvfb-run >/dev/null 2>&1  || fail_env "缺少 xvfb-run"
command -v xdotool >/dev/null 2>&1   || fail_env "缺少 xdotool"

echo "[1/3] 竞态检测构建桌面 playground ..."
( cd "$ROOT" && go build -race -o "$BIN" ./examples/playground/desktop ) \
  || fail_env "go build -race 失败"

echo "[2/3] xvfb 启动 + 极快打字（${BATCHES} 批 × ${PER_BATCH} 字符）..."
CHARS="$(printf 'abcdefghijklmnopqrstuvwxyz0123456789%.0s' $(seq 1 40))"
CHARS="${CHARS:0:PER_BATCH}"

timeout 120 xvfb-run -a -s "-screen 0 1600x1000x24" bash -c '
  BIN="$1"; ERR="$2"; CHARS="$3"; N="$4"
  if command -v openbox >/dev/null 2>&1; then
    openbox >/dev/null 2>&1 & WM=$!
  fi
  "$BIN" >/dev/null 2>"$ERR" & APP=$!
  WID=""
  for i in $(seq 1 50); do
    WID=$(xdotool search --name "GX Playground" 2>/dev/null | head -1)
    [ -n "$WID" ] && break
    sleep 0.1
  done
  [ -z "$WID" ] && { echo "窗口未出现" >&2; kill $APP 2>/dev/null; exit 2; }
  xdotool windowactivate --sync "$WID" 2>/dev/null || true
  for i in $(seq 1 "$N"); do
    xdotool type --delay 0 -- "$CHARS"
  done
  sleep 2
  kill $APP $WM 2>/dev/null || true
  wait $APP 2>/dev/null || true
' _ "$BIN" "$ERR_LOG" "$CHARS" "$BATCHES"
RUN_RC=$?

if [ "$RUN_RC" -eq 2 ]; then
  fail_env "GUI 运行时环境异常（窗口未出现）"
fi

echo "[3/3] 扫描竞态报告 ..."
COUNT=$(grep -c -E 'WARNING: DATA RACE|fatal error: concurrent map (write|read)' "$ERR_LOG" 2>/dev/null || true)
if [ "${COUNT:-0}" -gt 0 ]; then
  echo "SMOKE_FAIL: 检测到 $COUNT 处竞态" >&2
  grep -E -A 12 'WARNING: DATA RACE|fatal error: concurrent map' "$ERR_LOG" | head -80 >&2
  exit 1
fi
echo "SMOKE_OK: ${BATCHES} 批极快打字无数据竞争"
exit 0
