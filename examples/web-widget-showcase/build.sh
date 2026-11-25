#!/usr/bin/env bash
# 构建 Web 版 Apple 组件库 showcase：严格 TypeScript 前端 + Go wasm 后端。
# 产出 index.html + dist/main.js + app.wasm + wasm_exec.js，浏览器可直接打开。
set -euo pipefail
cd "$(dirname "$0")"

GOTOOLCHAIN="${GOTOOLCHAIN:-go1.27.4}"
GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
export GOTOOLCHAIN GOPROXY

echo "[1/4] 严格 TypeScript 类型检查 + 编译（strict 全套，失败即中止）..."
if [ ! -x node_modules/.bin/tsc ]; then
  echo "未找到本地 tsc，先安装依赖（typescript 7.0.2）…"
  npm install --no-audit --no-fund
fi
node_modules/.bin/tsc --version
node_modules/.bin/tsc --noEmit
node_modules/.bin/tsc

echo "[2/4] 编译 Go wasm ..."
GOOS=js GOARCH=wasm go build -o app.wasm .

echo "[3/4] 拷贝 wasm_exec.js（Go 工具链胶水）..."
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" ./wasm_exec.js

echo "[4/4] 完成。本地预览："
echo "    cd $(pwd) && python3 -m http.server 8080"
echo "    浏览器打开 http://localhost:8080/"
echo "    浅色主题： http://localhost:8080/?theme=light"
