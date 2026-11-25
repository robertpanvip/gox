#!/usr/bin/env bash
# 把「GX 写的 playground 场景树 + Web 后端」编译成 wasm，并用 TypeScript 7
# （strict 全套）编译前端加载器。产出可直接用静态服务器预览的目录。
set -euo pipefail
cd "$(dirname "$0")"

GOTOOLCHAIN="${GOTOOLCHAIN:-go1.27.4}"
GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
export GOTOOLCHAIN GOPROXY

echo "[0/6] 校验 playground 示例（gox fmt -check + gox build -check 门禁）并生成 app.go..."
# demo 源码真相源在 ../ui/samples/*.gx。必须先过格式化与语义检查，
# 全部通过后 gen_samples.py 才写入 app.gx 并转译 app.go；任一失败立即中止构建。
UI_DIR="$(cd "$(dirname "$0")/../ui" && pwd)"
GOX_BIN="${GOX_BIN:-gox}"
if [ ! -x "$GOX_BIN" ] && ! command -v "$GOX_BIN" >/dev/null 2>&1; then
  echo "未找到 gox，先构建：go build -o gox ../../../cmd/gox"
  (cd "$(dirname "$0")/../../.." && GOTOOLCHAIN="$GOTOOLCHAIN" GOPROXY="$GOPROXY" go build -o /tmp/gox ./cmd/gox)
  GOX_BIN=/tmp/gox
fi
GOX="$GOX_BIN" python3 "$UI_DIR/gen_samples.py"

echo "[1/6] 前端严格类型检查 + 编译（TypeScript 7）..."
if [ ! -x node_modules/.bin/tsc ]; then
  echo "未找到本地 tsc，先安装依赖（typescript 7.0.2）..."
  npm install --no-audit --no-fund
fi
node_modules/.bin/tsc --version
node_modules/.bin/tsc --noEmit   # strict 全套；失败即中止
node_modules/.bin/tsc            # 产出 dist/main.js

echo "[2/6] 编译 Go wasm（js/wasm，含内建 LSP）..."
GOOS=js GOARCH=wasm go build -o app.wasm .

echo "[3/6] 拷贝 wasm_exec.js（Go 工具链运行时胶水）..."
GOROOT="$(go env GOROOT)"
cp "$GOROOT/lib/wasm/wasm_exec.js" ./wasm_exec.js

echo "[4/6] 编辑器样式已全部迁入 GX prop（animation/pointer-events/selection/font-family），无需宿主 CSS 同步。"

echo "[5/6] 打包单文件 playground.html（file:// 双击即开，无需服务器）..."
python3 pack.py

echo "[6/6] 完成。两种打开方式："
echo "    A) 直接双击 $(pwd)/playground.html （已内联 wasm，无需服务器）"
echo "    B) 服务器模式：cd $(pwd) && python3 -m http.server 8080"
echo "       浏览器打开 http://localhost:8080/index.html"
