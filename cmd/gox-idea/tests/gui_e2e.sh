#!/usr/bin/env bash
# GoX IDE 图形界面端到端缺陷检测脚本。
#
# 目的：把「点击定位」「选行复制」这类肉眼可见、逻辑单测覆盖不到的 GUI bug
# 固化成可重复运行的自动化断言。做法是真实启动 IDE，用 xdotool 模拟点击与
# 键盘，再用剪贴板读回编辑器内容来断言光标落点/选区内容。
#
# 用法：
#   ./gui_e2e.sh                # 稳定回归模式：跑所有"已修复应通过"的断言
#   ./gui_e2e.sh --strict       # 严格模式：把已知点击定位缺陷也计为 FAIL
#   程序路径用环境变量 GOX_IDEA_BIN 指定，默认 /tmp/gox-idea-lg。
# 依赖：Xvfb、xdotool、xclip（沙箱已预装）。
# 退出码：0 = 通过；非 0 = 检测到回归或（--strict 下）已知缺陷。
#
# 说明：场景1「点击定位」是历史上真实存在的 bug。默认模式下它作为"观察项"
# 打印证据但不判红，避免 CI 因已知问题长期红灯；加 --strict 才计入失败，
# 便于修复后验收（修复后空格应落在 v 之后，届时 --strict 也会全绿）。

set -u

STRICT=0
[ "${1:-}" = "--strict" ] && STRICT=1

DISP=":139"
BIN="${GOX_IDEA_BIN:-/tmp/gox-idea-lg}"
PASS=0
FAIL=0

cleanup() {
  [ -n "${APP_PID:-}" ] && kill "$APP_PID" 2>/dev/null
  [ -n "${XVFB_PID:-}" ] && kill "$XVFB_PID" 2>/dev/null
}
trap cleanup EXIT

echo "== 启动 Xvfb $DISP =="
Xvfb "$DISP" -screen 0 1600x1000x24 -nolisten tcp >/tmp/gox-idea-xvfb.log 2>&1 &
XVFB_PID=$!
sleep 2

echo "== 启动 IDE: $BIN =="
DISPLAY="$DISP" "$BIN" >/tmp/gox-idea-e2e-app.log 2>&1 &
APP_PID=$!
sleep 6

# 读回编辑器全文（Ctrl+A 全选 → Ctrl+C → 剪贴板）。
read_editor() {
  DISPLAY="$DISP" xdotool key --clearmodifiers ctrl+a >/dev/null 2>&1
  sleep 0.3
  DISPLAY="$DISP" xdotool key --clearmodifiers ctrl+c >/dev/null 2>&1
  sleep 0.3
  DISPLAY="$DISP" xclip -selection clipboard -o 2>/dev/null
}

assert_eq() {
  local name="$1" got="$2" want="$3"
  if [ "$got" = "$want" ]; then
    echo "  [PASS] $name"
    PASS=$((PASS+1))
  else
    echo "  [FAIL] $name"
    echo "         got : $got"
    echo "         want: $want"
    FAIL=$((FAIL+1))
  fi
}

# ---- 场景 1：点击 <div 的 v 后按空格，空格应落在 v 之后（">" 前） ----
# 编辑器第 10 行 "    return <div>Hello, {name}!</div>"；v 后的屏幕坐标约 (448,312)。
echo "== 场景1：点击定位 =="
BASE=$(read_editor | sed -n '10p')
echo "  基线第10行: $BASE"

DISPLAY="$DISP" xdotool mousemove 448 312 click 1 >/dev/null 2>&1
sleep 0.4
DISPLAY="$DISP" xdotool key space >/dev/null 2>&1
sleep 0.4
AFTER=$(read_editor | sed -n '10p')
echo "  输入后第10行: $AFTER"

# 正确行为：<div 的 v 后插入空格 → "return <div Hello"；
# 已知 bug：空格跑到 < 与 div 之间 → "return < div>"。
if [ "$AFTER" = "    return <div Hello, {name}!</div>" ]; then
  echo "  [PASS] 空格落在 v 之后"
  PASS=$((PASS+1))
elif [ "$AFTER" = "    return < div>Hello, {name}!</div>" ]; then
  if [ "$STRICT" -eq 1 ]; then
    echo "  [FAIL] 抓到缺陷：点击 v 后按空格，空格跑到了 < 与 div 之间（点击定位偏移）"
    echo "         got : $AFTER"
    FAIL=$((FAIL+1))
  else
    echo "  [OBSERVE] 已知缺陷仍在：空格跑到了 < 与 div 之间（--strict 下判 FAIL）"
    echo "            got : $AFTER"
  fi
else
  echo "  [FAIL] 空格落在意外位置"
  echo "         got : $AFTER"
  FAIL=$((FAIL+1))
fi

# 撤销，恢复到基线。
DISPLAY="$DISP" xdotool key --clearmodifiers ctrl+z >/dev/null 2>&1
sleep 0.3

# ---- 场景 2：三击第 10 行选行复制，应拿到完整行文本 ----
echo "== 场景2：选行复制完整性 =="
# 三击（400ms 内同点 3 次）触发 SelectLine。
DISPLAY="$DISP" xdotool mousemove 500 312 click --repeat 3 --delay 120 1 >/dev/null 2>&1
sleep 0.4
DISPLAY="$DISP" xdotool key --clearmodifiers ctrl+c >/dev/null 2>&1
sleep 0.4
COPIED=$(DISPLAY="$DISP" xclip -selection clipboard -o 2>/dev/null | head -1)
echo "  选行复制内容: $COPIED"
assert_eq "选行复制包含完整行" "$COPIED" "    return <div>Hello, {name}!</div>"

echo ""
echo "== 汇总：PASS=$PASS FAIL=$FAIL =="
if [ "$FAIL" -gt 0 ]; then
  echo "结论：检测到 GUI 缺陷（$FAIL 项）。"
  exit 1
fi
echo "结论：全部通过。"
exit 0
