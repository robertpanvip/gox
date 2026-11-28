#!/usr/bin/env bash
# GoX IDE 图标渲染 + 主流程端到端测试。
#
# 与 gui_interactions_e2e.sh（碎片化交互）互补，本脚本覆盖两类此前盲区：
#   A. 图标正确性——命中框结构断言 + 深浅主题像素采样（调用 icon_check.py），
#      确认每个图标真的画出来、不是空白/乱码/色块。
#   B. 主流程正确性——「打开文件 → 编辑 → Run → 终端输出」整条链路，
#      断言每一步状态确实推进，且运行无指针乱码。
#
# 用法：
#   GOX_IDEA_BIN=/path/to/gox-idea ./gui_visual_flow_e2e.sh
# 依赖：Xvfb、xdotool、import(ImageMagick)、python3 + Pillow。退出码 0 = 全绿。
set -u

DISPLAY_NO="${GOX_E2E_DISPLAY:-:190}"
BIN="${GOX_IDEA_BIN:-/tmp/gox-idea-lg3}"
STATE="${GOX_E2E_STATE_FILE:-/tmp/gox-idea-visual-state.json}"
SHOT_DARK="/tmp/gox-idea-visual-dark.png"
SHOT_LIGHT="/tmp/gox-idea-visual-light.png"
export DISPLAY="$DISPLAY_NO"

PASS=0; FAIL=0; FAILED_NAMES=""
cleanup() {
  [ -n "${APP_PID:-}" ] && kill "$APP_PID" 2>/dev/null
  [ -n "${XVFB_PID:-}" ] && kill "$XVFB_PID" 2>/dev/null
}
trap cleanup EXIT

j() { python3 -c "import json;d=json.load(open('$STATE'));print(d['$1'])" 2>/dev/null; }
pass() { echo "  [PASS] $1"; PASS=$((PASS+1)); }
fail() { echo "  [FAIL] $1"; FAIL=$((FAIL+1)); FAILED_NAMES="$FAILED_NAMES\n  - $1"; }
assert_eq() { if [ "$2" = "$3" ]; then pass "$1"; else echo "         got=$2 want=$3"; fail "$1"; fi; }
assert_ne() { if [ "$2" != "$3" ]; then pass "$1"; else fail "$1 (不应等于 $3)"; fi; }
assert_contains() { if printf '%s' "$2" | grep -qF "$3"; then pass "$1"; else echo "         got: $2"; fail "$1 (缺少「$3」)"; fi; }
sleep_s() { sleep "${1:-0.35}"; }
click() { xdotool mousemove "$1" "$2" click 1 >/dev/null 2>&1; sleep_s; }
# 工具栏按钮中心从 testid_boxes 动态取值（写死坐标会随布局变化失准）。
testid_center() {
  python3 -c "
import json
d=json.load(open('$STATE'))
b=[x for x in d['testid_boxes'] if x['testid']=='$1'][0]
print(int(b['x']+b['w']/2), int(b['y']+b['h']/2))
"
}

echo "== 启动 Xvfb $DISPLAY_NO =="
Xvfb "$DISPLAY_NO" -screen 0 1600x1000x24 -nolisten tcp >/tmp/gox-idea-v-xvfb.log 2>&1 &
XVFB_PID=$!; sleep 2
rm -f "$STATE"
GOX_IDEA_STATE_FILE="$STATE" "$BIN" >/tmp/gox-idea-v-app.log 2>&1 &
APP_PID=$!
for _ in $(seq 1 60); do [ -s "$STATE" ] && break; sleep 0.2; done
[ -s "$STATE" ] || { echo "FATAL: 状态快照未生成"; exit 2; }

echo ""
echo "===== A. 图标正确性 ====="

# A1. 命中框结构：13 个图标按钮都在、尺寸非零、分区正确。
echo "== A1. 图标命中框结构 =="
BOX_SUMMARY=$(python3 -c "
import json
d=json.load(open('$STATE')); b=d.get('testid_boxes',[])
tool=[x for x in b if x['testid'].startswith('btn-')]
stripe=[x for x in b if x['testid'].startswith('stripe-')]
bad=[x for x in b if x['w']<10 or x['h']<10]
print(len(b), len(tool), len(stripe), len(bad))
")
read NBT NTOOL NSTRIPE NBAD <<< "$BOX_SUMMARY"
echo "  图标框总数=$NBT 工具栏=$NTOOL 竖栏=$NSTRIPE 尺寸异常=$NBAD"
[ "$NBT" -ge 13 ] && pass "图标命中框齐全 (>=13)" || fail "图标命中框不足 ($NBT)"
[ "$NTOOL" -ge 8 ] && pass "工具栏图标齐全 (>=8)" || fail "工具栏图标不足 ($NTOOL)"
[ "$NSTRIPE" -ge 5 ] && pass "竖栏图标齐全 (>=5)" || fail "竖栏图标不足 ($NSTRIPE)"
[ "$NBAD" -eq 0 ] && pass "所有图标框尺寸非零" || fail "存在尺寸异常的图标框 ($NBAD)"

# A2. 深色主题像素采样：每个图标区域确实绘制了内容。
echo "== A2. 深色主题图标像素 =="
import -window root "$SHOT_DARK" 2>/dev/null
python3 "$(dirname "$0")/icon_check.py" "$STATE" "$SHOT_DARK" --min 8
[ $? -eq 0 ] && pass "深色主题 13 图标渲染正确" || fail "深色主题有图标渲染异常"

# A3. 浅色主题像素采样：换肤后图标仍可见（主题色正确跟随）。
echo "== A3. 浅色主题图标像素 =="
read MX MY <<< "$(testid_center btn-moon)"
click "$MX" "$MY"   # moon 切浅色
assert_eq "切换到浅色主题" "$(j light_theme)" "True"
import -window root "$SHOT_LIGHT" 2>/dev/null
python3 "$(dirname "$0")/icon_check.py" "$STATE" "$SHOT_LIGHT" --min 8
[ $? -eq 0 ] && pass "浅色主题 13 图标渲染正确" || fail "浅色主题有图标渲染异常"
click "$MX" "$MY"   # 切回深色
assert_eq "切回深色主题" "$(j light_theme)" "False"

echo ""
echo "===== B. 主流程正确性 ====="

# B1. 初始态：尚未运行。
echo "== B1. 初始态 =="
assert_eq "初始未运行" "$(j run_info)" "预览模式 · 点击 Run 解释当前源码"
assert_eq "初始终端为空" "$(j terminal_text)" ""

# B2. 打开文件：项目树点击 Counter.gx 生成新标签并激活。
echo "== B2. 打开文件 =="
click 100 183
assert_contains "打开文件生成标签" "$(python3 -c "import json;print('|'.join(json.load(open('$STATE'))['tabs']))")" "Counter"
assert_eq "打开文件成为活动标签" "$(j active_tab)" "02 Counter.gx"

# B3. 编辑：在编辑器输入字符，文本快照反映修改。
echo "== B3. 编辑源码 =="
BEFORE=$(j editor_text)
click 500 311
xdotool type --clearmodifiers 'Q' >/dev/null 2>&1; sleep_s
AFTER=$(j editor_text)
assert_ne "编辑后源码文本发生变化" "$AFTER" "$BEFORE"
assert_contains "输入字符进入源码" "$AFTER" "Q"

# B4. Run：运行状态推进 + 终端输出解释成功 + 无指针乱码。
echo "== B4. Run 解释执行 =="
read RX RY <<< "$(testid_center btn-run)"
click "$RX" "$RY"
sleep_s 1
assert_eq "Run 后运行状态更新" "$(j run_info)" "已运行 · 解释执行当前源码"
TERM=$(j terminal_text)
assert_contains "终端标记 Run 起点" "$TERM" "── Run ──"
assert_contains "终端报告解释成功" "$TERM" "解释成功"
if printf '%s' "$TERM" | grep -qE '0x[0-9a-f]{4,}'; then
  fail "主流程输出含指针乱码"
else
  pass "主流程输出无指针乱码"
fi

# B5. 再次 Run：终端追加第二条 Run 记录（可重复执行）。
echo "== B5. 再次 Run =="
read RX RY <<< "$(testid_center btn-run)"
click "$RX" "$RY"
sleep_s 1
RUN_MARKS=$(python3 -c "import json;print(json.load(open('$STATE'))['terminal_text'].count('── Run ──'))")
assert_eq "终端累计两次 Run 记录" "$RUN_MARKS" "2"

echo ""
echo "== 汇总：PASS=$PASS FAIL=$FAIL =="
if [ "$FAIL" -gt 0 ]; then
  printf '失败项:\n%b\n' "$FAILED_NAMES"
  exit 1
fi
echo "结论：图标渲染与主流程断言全部通过。"
exit 0
