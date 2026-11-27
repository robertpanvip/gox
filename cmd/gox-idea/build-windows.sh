#!/usr/bin/env bash
# gox-idea.exe 的 Windows 构建（CI Windows job 与本地同路径）。
# 与 scripts/build-windows-msys2.sh 逻辑一致，区别只在构建的模块是 cmd/gox-idea：
#   - yoga-go-binding 自带 Linux ELF 库，Windows 需从 main 分支源码重编 COFF 静态库
#   - 通过临时 replace 把本地副本挂进 gox-idea/go.mod，构建后还原
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
 IDEA_DIR="$REPO_ROOT/cmd/gox-idea"

echo "[1/4] 编译 Windows 版 yoga 静态库..."
if [ ! -f /tmp/yoga-main/yoga/YGNodeLayout.h ]; then
  git clone --depth 1 https://github.com/facebook/yoga.git /tmp/yoga-main
fi
cd /tmp/yoga-main
find yoga -name '*.o' -delete
for f in $(find yoga -name '*.cpp'); do
  g++ -std=c++20 -O2 -DNDEBUG -I. -c "$f" -o "${f%.cpp}.o"
done
mkdir -p /tmp/yoga-win
ar rcs /tmp/yoga-win/libyogacore.a $(find yoga -name '*.o')
NM="$(command -v nm)"
"$NM" /tmp/yoga-win/libyogacore.a | grep -q "T YGNodeLayoutGetRawWidth" \
  || { echo "yoga 库缺 YGNodeLayoutGetRawWidth"; exit 1; }

echo "[2/4] 组装 yoga 绑定副本并临时挂 replace..."
BIND_SRC="$(go env GOMODCACHE)/github.com/ahati/yoga-go-binding@v0.1.0"
if command -v cygpath >/dev/null 2>&1; then
  BIND_SRC="$(cygpath -u "$(dirname "$BIND_SRC")")/yoga-go-binding@v0.1.0"
fi
[ -d "$BIND_SRC" ] || { echo "未找到 yoga-go-binding 缓存: $BIND_SRC"; exit 1; }
# 绑定副本必须放在 go.mod 同目录并用相对路径挂 replace：Windows 版 go.exe
# 对 "/tmp/..." 这类不带盘符的路径不认作绝对（filepath.IsAbs=false），
# 会拼接到 go.mod 所在目录去找，导致 "cannot find the path specified"。
# 相对路径 replace 的解析基准正是 go.mod 所在目录，MSYS2 与原生 Windows 一致。
rm -rf "$IDEA_DIR/tmp/yoga-win-binding"
mkdir -p "$IDEA_DIR/tmp"
cp -r "$BIND_SRC" "$IDEA_DIR/tmp/yoga-win-binding"
chmod -R u+w "$IDEA_DIR/tmp/yoga-win-binding"
cp /tmp/yoga-win/libyogacore.a "$IDEA_DIR/tmp/yoga-win-binding/lib/libyogacore.a"

cd "$IDEA_DIR"
cp go.mod /tmp/gox-idea-go.mod.bak
restore() { cp /tmp/gox-idea-go.mod.bak go.mod; rm -rf "$IDEA_DIR/tmp/yoga-win-binding"; }
trap restore EXIT
echo "replace github.com/ahati/yoga-go-binding => ./tmp/yoga-win-binding" >> go.mod

echo "[3/4] 编译 gox-idea.exe（GUI 子系统 + 全静态）..."
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 \
  CC="$(command -v gcc)" CXX="$(command -v g++)" \
  go build -ldflags "-H windowsgui -extldflags -static" -o gox-idea.exe .

echo "[4/4] 自检..."
OBJDUMP="$(command -v objdump)"
"$OBJDUMP" -p gox-idea.exe | grep "DLL Name" | sort -u || true
ls -la gox-idea.exe
echo "构建完成：$IDEA_DIR/gox-idea.exe"
