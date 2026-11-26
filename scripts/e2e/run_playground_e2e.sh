#!/usr/bin/env bash
# Playground 按功能 E2E 一键脚本：构建 gox 编译器 → 构建 web 产物 → 起静态服务 → 跑用例。
# 退出码非 0 即有功能用例失败。CI（.github/workflows/ci.yml）与本地均可直接运行。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
WEB_DIR="$REPO_ROOT/examples/playground/web"
PORT="${PORT:-8081}"
URL="http://127.0.0.1:$PORT/index.html"
# Python 命令探测：Windows Git Bash 通常只有 python。
PY="${PYTHON:-python3}"
command -v "$PY" >/dev/null 2>&1 || PY=python

cd "$REPO_ROOT"

echo "[1/4] 构建 gox 编译器..."
if [ ! -x /tmp/gox-e2e ] || [ -n "${FORCE_BUILD_GOX:-}" ]; then
  go build -o /tmp/gox-e2e ./cmd/gox
fi
GOX_BIN=/tmp/gox-e2e

echo "[2/4] 构建 playground web 产物（Go wasm + TS）..."
(cd "$WEB_DIR" && GOX_BIN="$GOX_BIN" ./build.sh >/dev/null)

echo "[3/4] 起静态服务器（:$PORT）..."
(cd "$WEB_DIR" && python3 -m http.server "$PORT" --bind 127.0.0.1 >/dev/null 2>&1) &
SERVER_PID=$!
cleanup() { kill "$SERVER_PID" 2>/dev/null || true; }
trap cleanup EXIT

for i in $(seq 1 30); do
  curl -sf -o /dev/null "$URL" && break
  [ "$i" = 30 ] && { echo "服务器未就绪"; exit 1; }
  sleep 0.5
done

echo "[4/4] 运行按功能 E2E 用例..."
"$PY" "$REPO_ROOT/scripts/e2e/playground_features.py" --url "$URL"
