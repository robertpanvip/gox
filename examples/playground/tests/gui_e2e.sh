#!/usr/bin/env bash
# Desktop Playground GUI 端到端测试：真实鼠标/键盘驱动 + 状态快照硬断言。
#
# 覆盖 gox-idea 三套 E2E 之外的桌面 Playground 本体，且专门锁定最初那两个
# 严重 bug：点击定位随列号偏移、框选/三击一行复制被截断。
#
# 与 gox-idea E2E 同源的跨平台方案：Linux 用系统原生 X11 工具；Windows 复用
# cmd/gox-idea/tests/shims/ + gui_driver.py（pyautogui），脚本主体不用改。
#
# 用法：
#   GOX_PLAYGROUND_BIN=/path/to/gox-playground-desktop ./gui_e2e.sh
# 依赖：Linux 需 Xvfb/xdotool/xclip/python3；Windows 需 pyautogui/pillow。
# 退出码：0 = 全绿。
set -u

DISPLAY_NO="${GOX_E2E_DISPLAY:-:210}"
BIN="${GOX_PLAYGROUND_BIN:-/tmp/pg-desktop-e2e}"
STATE="${GOX_E2E_STATE_FILE:-/tmp/pg-gui-state.json}"
# 从脚本自身位置推导仓库根（tests/ → playground/ → examples/ → 仓库根），
# 复用 gox-idea 的跨平台 shims，避免重复维护。
REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
SHIM_DIR="${GOX_GUI_SHIM_DIR:-$REPO_ROOT/cmd/gox-idea/tests/shims}"
export DISPLAY="$DISPLAY_NO"

# Windows：把 gox-idea 的 shims 放到 PATH 最前，脚本里的 xdotool/xclip/Xvfb/import
# 全部被拦截转发给 pyautogui；Linux 不启用，走系统原生工具。
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*) export PATH="$SHIM_DIR:$PATH" ;;
esac

PASS=0; FAIL=0; FAILED=""
cleanup() { [ -n "${APP_PID:-}" ] && kill "$APP_PID" 2>/dev/null; [ -n "${XVFB_PID:-}" ] && kill "$XVFB_PID" 2>/dev/null; }
trap cleanup EXIT

j() { python3 -c "import json;d=json.load(open('$STATE'));print(d['$1'])" 2>/dev/null; }
pass() { echo "  [PASS] $1"; PASS=$((PASS+1)); }
fail() { echo "  [FAIL] $1 (got=${2:-} want=${3:-})"; FAIL=$((FAIL+1)); FAILED="$FAILED\n  - $1"; }
assert_eq() { [ "$(j "$1")" = "$2" ] && pass "$3" || fail "$3" "$(j "$1")" "$2"; }
sleep_s() { sleep "${1:-0.4}"; }

# 从快照命中框按 label+kind 取按钮/行中心；动态坐标，布局调整也不失效。
box_center() {
  python3 -c "
import json,sys
d=json.load(open('$STATE')); lab=sys.argv[1]; kind=sys.argv[2]
c=[x for x in d['boxes'] if x['label']==lab and (not kind or x['kind']==kind)]
if not c: c=[x for x in d['boxes'] if x['label']==lab and x['w']<300 and x['h']<70]
if not c: sys.exit(1)
x=c[0]; print(int(x['x']+x['w']/2), int(x['y']+x['h']/2))
" "$1" "${2:-}"
}
click_box() { local c; c=$(box_center "$1" "$2") || return 1; xdotool mousemove ${c% *} ${c#* } click 1 >/dev/null 2>&1; sleep_s; }
# 点击某代码行的第 col 列：用该行文本 span 框 x 起点 + 实测字宽。
click_col() {
  local text="$1" col="$2" c
  c=$(python3 -c "
import json,sys
d=json.load(open('$STATE')); t=sys.argv[1]
b=[x for x in d['boxes'] if t in x['label'] and x['kind']=='span']
if not b: sys.exit(1)
x=b[0]; W=float(sys.argv[3]); col=int(sys.argv[2]); print(int(x['x']+col*W), int(x['y']+x['h']/2))
" "$text" "$col" "$CHAR_W") || return 1
  xdotool mousemove ${c% *} ${c#* } click 1 >/dev/null 2>&1; sleep_s
}

echo "== 启动 Xvfb $DISPLAY_NO =="
Xvfb "$DISPLAY_NO" -screen 0 1600x1000x24 -nolisten tcp >/tmp/pg-e2e-xvfb.log 2>&1 &
XVFB_PID=$!; sleep 2
rm -f "$STATE"
echo "== 启动 IDE: $BIN =="
GOX_PLAYGROUND_STATE_FILE="$STATE" "$BIN" >/tmp/pg-e2e-app.log 2>&1 &
APP_PID=$!
for _ in $(seq 1 80); do [ -s "$STATE" ] && break; sleep 0.2; done
[ -s "$STATE" ] || { echo "FATAL: 状态快照未生成"; exit 2; }

CHAR_W=$(cd "$(dirname "$0")/.." && go run ./tests/charwidth.go 2>/dev/null | awk '{print $1}')
[ -z "$CHAR_W" ] && CHAR_W=7.8267
echo "实测字宽 CHAR_W=$CHAR_W"

echo ""
echo "===== 1. 快照与命中框 ====="
NBOX=$(python3 -c "import json;print(len(json.load(open('$STATE'))['boxes']))")
[ "$NBOX" -ge 20 ] && pass "命中框已导出 ($NBOX 个)" || fail "命中框过少" "$NBOX" ">=20"
for b in 运行 主题 上一个 下一个; do
  box_center "$b" button >/dev/null 2>&1 && pass "按钮命中框存在: $b" || fail "按钮命中框缺失: $b"
done

echo ""
echo "===== 2. 编辑器点击定位（历史 bug 回归）====="
click_col "package main" 0
assert_eq editor_col 0 "点击 package 行 col0"
click_col "package main" 7
assert_eq editor_col 7 "点击 package 行 col7（不随列号偏移）"
click_col "package main" 11
assert_eq editor_col 11 "点击 package 行 col11（行尾）"

echo ""
echo "===== 3. 三击选行复制完整（历史 bug 回归）====="
c=$(box_center "package main" span)
xdotool mousemove ${c% *} ${c#* }; sleep 0.2
xdotool click --repeat 3 --delay 120 1 >/dev/null 2>&1; sleep_s
SEL=$(j selected_text)
printf '%s' "$SEL" | grep -qF 'package main' && pass "三击选行包含完整行文本" || fail "三击选行内容被截断" "$SEL" "含 package main"

echo ""
echo "===== 4. 拖动选区 + Shift 扩展 ====="
click_col "package main" 0
xdotool key --clearmodifiers shift+Right shift+Right shift+Right >/dev/null 2>&1; sleep_s
[ "$(j has_selection)" = "True" ] && pass "Shift+Right 产生选区" || fail "Shift+Right 无选区"
SEL=$(j selected_text)
[ "$SEL" = "pac" ] && pass "选区内容=pac" || fail "Shift 选区内容错误" "$SEL" "pac"

echo ""
echo "===== 5. 键盘编辑链路 ====="
# 全选 → 复制 → 粘贴不改变内容
xdotool key --clearmodifiers ctrl+a >/dev/null 2>&1; sleep_s
xdotool key --clearmodifiers ctrl+c >/dev/null 2>&1; sleep_s
CLIP=$(xclip -selection clipboard -o 2>/dev/null)
printf '%s' "$CLIP" | grep -qF 'package main' && pass "Ctrl+A/C 复制到完整源码" || fail "剪贴板内容异常"
xdotool key --clearmodifiers ctrl+v >/dev/null 2>&1; sleep_s
printf '%s' "$(j editor_text)" | grep -qF 'package main' && pass "Ctrl+V 粘贴保持源码" || fail "粘贴后源码异常"
# Home/End
xdotool key --clearmodifiers Home >/dev/null 2>&1; sleep_s
assert_eq editor_col 0 "Home 到行首"
xdotool key --clearmodifiers End >/dev/null 2>&1; sleep_s
[ "$(j editor_col)" -gt 0 ] && pass "End 到行尾" || fail "End 未到行尾"
# 中文输入 + 撤销
xdotool type --clearmodifiers '中' >/dev/null 2>&1; sleep_s
printf '%s' "$(j editor_text)" | grep -qF '中' && pass "中文按 rune 插入" || fail "中文插入失败"
xdotool key --clearmodifiers ctrl+z >/dev/null 2>&1; sleep_s
printf '%s' "$(j editor_text)" | grep -qF '中' && fail "撤销未生效" || pass "Ctrl+Z 撤销中文插入"

echo ""
echo "===== 6. 滚轮滚动 ====="
python3 -c "print('\n'.join('line%02d'%i for i in range(60)))" | xclip -selection clipboard 2>/dev/null
xdotool key --clearmodifiers ctrl+a >/dev/null 2>&1; sleep 0.2
xdotool key --clearmodifiers ctrl+v >/dev/null 2>&1; sleep_s
ST0=$(j scroll_top)
xdotool mousemove 600 300 >/dev/null 2>&1
xdotool click --repeat 8 --delay 50 4 >/dev/null 2>&1; sleep_s
ST1=$(j scroll_top)
DELTA=$(python3 -c "print(int(round(float('$ST0')-float('$ST1'))))" 2>/dev/null)
[ "${DELTA:-0}" -gt 20 ] && pass "滚轮上滚改变滚动偏移 ($ST0→$ST1)" || fail "滚轮未改变滚动偏移" "$ST0→$ST1" "delta>20"

echo ""
echo "===== 7. 主题切换 ====="
T0=$(j light_theme)
click_box 主题 button
T1=$(j light_theme)
[ "$T0" != "$T1" ] && pass "主题按钮切换明暗 ($T0→$T1)" || fail "主题未切换"
click_box 主题 button
assert_eq light_theme "$T0" "主题可切回"

echo ""
echo "===== 8. Run 主流程 ====="
# 场景6 粘贴了测试文本破坏了源码，先重载有效示例再 Run。
click_box '01 Hello GX' span
assert_eq run_info "预览模式 · 点击 Run 解释当前源码" "重载示例后为预览模式"
click_box 运行 button
printf '%s' "$(j run_info)" | grep -qF '已运行' && pass "Run 后进入运行状态" || fail "Run 未推进状态" "$(j run_info)"
printf '%s' "$(j console_text)" | grep -qF 'Run' && pass "Console 有 Run 输出" || fail "Console 无输出"
printf '%s' "$(j console_text)" | grep -qE '0x[0-9a-fA-F]+' && fail "运行输出含指针乱码" || pass "运行输出无指针乱码"

echo ""
echo "===== 9. 示例切换 ====="
LEN0=$(python3 -c "import json;print(len(json.load(open('$STATE'))['editor_text']))")
click_box 下一个 button
LEN1=$(python3 -c "import json;print(len(json.load(open('$STATE'))['editor_text']))")
[ "$LEN0" != "$LEN1" ] && pass "下一个示例切换源码 ($LEN0→$LEN1)" || fail "下一个示例未切换"
click_box '02 Counter' span
printf '%s' "$(j editor_text)" | grep -qF 'sig' && pass "点击列表项加载 Counter 示例" || fail "列表项点击未加载示例"

echo ""
echo "===== 10. 窗口 resize + resize 后交互 ====="
WID=$(xdotool search --name 'GX Playground' 2>/dev/null | head -1)
WW0=$(j window_w)
xdotool windowsize "$WID" 1200 720 >/dev/null 2>&1; sleep_s
WW1=$(j window_w)
[ "$WW0" != "$WW1" ] && pass "窗口尺寸变化 ($WW0→$WW1)" || fail "resize 未生效" "$WW0→$WW1" "变化"
click_box 主题 button && pass "resize 后按钮仍可点击" || fail "resize 后按钮失效"
xdotool windowsize "$WID" 1400 860 >/dev/null 2>&1; sleep_s

echo ""
echo "================ 汇总：PASS=$PASS FAIL=$FAIL ================"
[ -n "$FAILED" ] && printf '%b\n' "$FAILED"
[ "$FAIL" -eq 0 ]
