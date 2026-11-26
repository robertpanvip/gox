#!/usr/bin/env bash
# Windows 版 Playground exe 构建（在 MSYS2 MINGW64 shell 内运行，CI 的
# build-windows job 与本地完全同路径）。完整背景见 docs/windows-build.md。
#
# 产出：examples/playground/desktop 同级的 GX-Playground.exe（自包含，
# 只依赖 Windows 系统 DLL），随后由 CI 上传为 artifact / release。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

echo "[1/5] 从源码编译 Windows 版 yoga 静态库..."
# yoga-go-binding 模块只带 Linux ELF 版 libyogacore.a，Windows 链接不了；
# 且其头文件对齐 yoga main 分支（比 3.2.1 多 YGNodeLayoutGetRawWidth 等 API）。
if [ ! -f /tmp/yoga-main/yoga/YGNodeLayout.h ]; then
  git clone --depth 1 https://github.com/facebook/yoga.git /tmp/yoga-main
fi
cd /tmp/yoga-main
find yoga -name '*.o' -delete
for f in $(find yoga -name '*.cpp'); do
  g++ -std=c++20 -O2 -DNDEBUG -I. -c "$f" -o "${f%.cpp}.o"
done
mkdir -p /tmp/yoga-win
# 工具名兼容：Linux 交叉环境是 x86_64-w64-mingw32-*，MSYS2 MINGW64 里是裸 nm/objdump。
NM="$(command -v x86_64-w64-mingw32-nm || command -v nm)"
OBJDUMP="$(command -v x86_64-w64-mingw32-objdump || command -v objdump)"
ar rcs /tmp/yoga-win/libyogacore.a $(find yoga -name '*.o')
"$NM" /tmp/yoga-win/libyogacore.a | grep -q "T YGNodeLayoutGetRawWidth" \
  || { echo "yoga 库缺 YGNodeLayoutGetRawWidth，头文件与源码分支不匹配"; exit 1; }

echo "[2/5] 组装本地 yoga 绑定副本并挂 replace..."
BIND_SRC="$(go env GOMODCACHE)/github.com/ahati/yoga-go-binding@v0.1.0"
# MSYS2 下 go 输出 Windows 风格路径（C:\...），转成 msys2 可用的 /c/... 形式。
if command -v cygpath >/dev/null 2>&1; then
  BIND_SRC="$(cygpath -u "$(dirname "$BIND_SRC")")/yoga-go-binding@v0.1.0"
fi
[ -d "$BIND_SRC" ] || { echo "未找到 yoga-go-binding 模块缓存: $BIND_SRC"; exit 1; }
rm -rf /tmp/yoga-win-binding
cp -r "$BIND_SRC" /tmp/yoga-win-binding
chmod -R u+w /tmp/yoga-win-binding
cp /tmp/yoga-win/libyogacore.a /tmp/yoga-win-binding/lib/libyogacore.a

cd "$REPO_ROOT/examples/playground"
cp go.mod /tmp/go.mod.ci-backup
restore() { cp /tmp/go.mod.ci-backup go.mod; }
trap restore EXIT
echo "replace github.com/ahati/yoga-go-binding => /tmp/yoga-win-binding" >> go.mod

echo "[3/5] 构建 gox 编译器并转译示例..."
(cd "$REPO_ROOT" && go build -o /tmp/gox-win ./cmd/gox)

echo "[4/5] 交叉链接 Windows exe（GUI 子系统 + 全静态）..."
# -H windowsgui：免控制台黑窗；-extldflags -static：内联 libstdc++/libgcc，
# 不依赖 libstdc++-6.dll（Windows 系统不带）。两个参数缺一不可。
cd "$REPO_ROOT"
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 \
  CC="$(command -v x86_64-w64-mingw32-gcc || command -v gcc)" \
  CXX="$(command -v x86_64-w64-mingw32-g++ || command -v g++)" \
  go build -ldflags "-H windowsgui -extldflags -static" \
  -o GX-Playground.exe ./examples/playground/desktop

echo "[5/5] 自检：依赖必须只剩 Windows 系统 DLL..."
"$OBJDUMP" -p GX-Playground.exe | grep "DLL Name" | sort -u
ls -la GX-Playground.exe
echo "构建完成：$REPO_ROOT/GX-Playground.exe"
