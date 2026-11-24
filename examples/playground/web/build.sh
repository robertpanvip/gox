#!/usr/bin/env bash
# 把「GX 写的 playground 场景树 + Web 后端」编译成 wasm，并用 TypeScript 7
# （strict 全套）编译前端加载器。产出可直接用静态服务器预览的目录。
set -euo pipefail
cd "$(dirname "$0")"

GOTOOLCHAIN="${GOTOOLCHAIN:-go1.27.4}"
GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
export GOTOOLCHAIN GOPROXY

echo "[1/4] 前端严格类型检查 + 编译（TypeScript 7）..."
if [ ! -x node_modules/.bin/tsc ]; then
  echo "未找到本地 tsc，先安装依赖（typescript 7.0.2）..."
  npm install --no-audit --no-fund
fi
node_modules/.bin/tsc --version
node_modules/.bin/tsc --noEmit   # strict 全套；失败即中止
node_modules/.bin/tsc            # 产出 dist/main.js

echo "[2/4] 编译 Go wasm（js/wasm）..."
GOOS=js GOARCH=wasm go build -o app.wasm .

echo "[3/4] 拷贝 wasm_exec.js（Go 工具链运行时胶水）..."
GOROOT="$(go env GOROOT)"
cp "$GOROOT/lib/wasm/wasm_exec.js" ./wasm_exec.js

echo "[4/4] 完成。本地预览："
echo "    cd $(pwd) && python3 -m http.server 8080"
echo "    浏览器打开 http://localhost:8080/"
