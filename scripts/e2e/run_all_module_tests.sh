#!/usr/bin/env bash
# 全 module 单测统一入口：补齐 CI 里 `go test ./...` 的覆盖盲区。
#
# 背景：仓库是 go.work 多 module 结构，`go test ./...` 从仓库根跑只会覆盖
# 主 module，examples/playground、cmd/gox-tui、cmd/gox-term、cmd/gox-idea、
# backend/* 这些嵌套 module 的包根本不会被 `./...` 匹配到。本脚本自动发现
# go.work 里所有含 _test.go 的 module，逐个 cd 进去跑 go test ./...。
#
# 用法：
#   bash scripts/e2e/run_all_module_tests.sh
# 退出码：所有 module 全绿返回 0，任一失败返回 1。
set -u

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

# 从 go.work 的 use (...) 块解析 module 路径（去行首空白 + ./ 前缀）。
MODULES=$(awk '/^use[[:space:]]*\(/{f=1;next} f&&/^\)/{f=0} f{gsub(/^[[:space:]]+/,"");gsub(/^\.\//,"");if(length($0))print}' go.work)

PASS=0
FAIL=0
SKIP=0
FAILED=""
for m in $MODULES; do
  # 只跑含测试文件的 module，空 module 跳过以免噪声。
  n=$(find "$m" -name '*_test.go' -not -path '*/.git/*' 2>/dev/null | wc -l)
  [ "$n" -eq 0 ] && continue

  # 可选后端：Rust 静态库未编译时跳过（需 cargo 预编译 libgxbackend_rust，
  # 否则链接期报 cannot find -lgxbackend_rust）。CI 没装 cargo 时不应卡住全链路。
  if [ "$m" = "../gox-backend/rust/go" ] && \
     ! ls ../gox-backend/rust/rust/target/release/libgxbackend_rust.* >/dev/null 2>&1; then
    echo ""
    echo "================ module: $m —— SKIP（Rust 静态库未编译） ================"
    SKIP=$((SKIP+1))
    continue
  fi

  # GUI 模块：桌面 cgo 依赖（go-gl/glfw 要 X11 头文件、go-gl/gl 要 pkg-config gl、
  # x/mobile 要 GLES3 头文件）。精简环境（CI runner 默认无这些 dev 包）下
  # 连编译期都过不去，跳过而不是误报构建失败——装好依赖的环境仍然全跑。
  if [ "$m" = "examples/playground" ] || [ "$m" = "cmd/gox-idea" ]; then
    missing=""
    [ -f /usr/include/X11/Xlib.h ] || missing="$missing libx11-dev"
    # GLFW 还依赖 X11 扩展头文件（Xcursor/Xrandr/Xinerama/Xi），
    # 只装 libx11-dev 会在编译 glfw 时报 Xcursor.h 缺失。
    [ -f /usr/include/X11/Xcursor/Xcursor.h ] || missing="$missing libxcursor-dev"
    [ -f /usr/include/X11/extensions/Xrandr.h ] || missing="$missing libxrandr-dev"
    [ -f /usr/include/X11/extensions/XInput2.h ] || missing="$missing libxi-dev"
    [ -f /usr/include/GLES3/gl3.h ] || missing="$missing libgles2-mesa-dev"
    pkg-config --exists gl 2>/dev/null || missing="$missing libgl-dev/pkg-config"
    if [ -n "$missing" ]; then
      echo ""
      echo "================ module: $m —— SKIP（缺 GUI 编译依赖:$missing，apt 安装后全跑） ================"
      SKIP=$((SKIP+1))
      continue
    fi
  fi

  echo ""
  echo "================ module: $m （$n 个测试文件） ================"
  if (cd "$m" && go test ./... -count=1); then
    echo ">>> $m : OK"; PASS=$((PASS+1))
  else
    echo ">>> $m : FAILED"; FAIL=$((FAIL+1)); FAILED="$FAILED $m"
  fi
done

echo ""
echo "================ module 测试汇总 ================"
echo "通过=$PASS 失败=$FAIL 跳过=$SKIP${FAILED:+ ; 失败:$FAILED}"
[ "$FAIL" -eq 0 ]
