#!/usr/bin/env bash
# GoX IDE 图形界面「交互广度」端到端测试。
#
# 与 gui_e2e.sh（两个定点回归）互补：本脚本用真实 X11 鼠标/键盘事件覆盖
# 拖动选区、分隔条拖拽、滚轮、菜单、主题、Run，以及完整的键盘编辑链路。
# 程序内状态通过 GOX_IDEA_STATE_FILE 快照读取，因此断言的是「状态确实变了」
# 而非仅仅「画面看起来变了」。
#
# 用法：
#   GOX_IDEA_BIN=/path/to/gox-idea ./gui_interactions_e2e.sh
# 依赖：Xvfb、xdotool、xclip、python3（沙箱已预装）。退出码 0 = 全绿。

set -u

DISPLAY_NO="${GOX_E2E_DISPLAY:-:151}"
BIN="${GOX_IDEA_BIN:-/tmp/gox-idea-lg}"
STATE="${GOX_E2E_STATE_FILE:-/tmp/gox-idea-interactions-state.json}"
export DISPLAY="$DISPLAY_NO"

PASS=0
FAIL=0
FAILED_NAMES=""

cleanup() {
  [ -n "${APP_PID:-}" ] && kill "$APP_PID" 2>/dev/null
  [ -n "${XVFB_PID:-}" ] && kill "$XVFB_PID" 2>/dev/null
}
trap cleanup EXIT

echo "== 启动 Xvfb $DISPLAY =="
Xvfb "$DISPLAY" -screen 0 1600x1000x24 -nolisten tcp >/tmp/gox-idea-i-xvfb.log 2>&1 &
XVFB_PID=$!
sleep 2

rm -f "$STATE"
echo "== 启动 IDE: $BIN =="
GOX_IDEA_STATE_FILE="$STATE" "$BIN" >/tmp/gox-idea-i-app.log 2>&1 &
APP_PID=$!

# 等状态文件就绪（原子 rename，读到即完整）。
for _ in $(seq 1 60); do
  [ -s "$STATE" ] && break
  sleep 0.2
done
if [ ! -s "$STATE" ]; then
  echo "FATAL: 状态快照未生成，测试钩子失效"; exit 2
fi

# ---- 通用断言 / 工具 ----
j() { python3 -c "import json;d=json.load(open('$STATE'));print(d['$1'])" 2>/dev/null; }
assert_eq() {
  local name="$1" got="$2" want="$3"
  if [ "$got" = "$want" ]; then
    echo "  [PASS] $name"; PASS=$((PASS+1))
  else
    echo "  [FAIL] $name"; echo "         got : $got"; echo "         want: $want"
    FAIL=$((FAIL+1)); FAILED_NAMES="$FAILED_NAMES\n  - $name"
  fi
}
assert_ne() {
  local name="$1" got="$2" bad="$3"
  if [ "$got" != "$bad" ]; then
    echo "  [PASS] $name"; PASS=$((PASS+1))
  else
    echo "  [FAIL] $name (不应等于 $bad)"; FAIL=$((FAIL+1)); FAILED_NAMES="$FAILED_NAMES\n  - $name"
  fi
}
assert_true() {
  local name="$1" val="$2"
  if [ "$val" = "True" ] || [ "$val" = "true" ]; then
    echo "  [PASS] $name"; PASS=$((PASS+1))
  else
    echo "  [FAIL] $name (期望真，实际 $val)"; FAIL=$((FAIL+1)); FAILED_NAMES="$FAILED_NAMES\n  - $name"
  fi
}
assert_ge() {
  local name="$1" got="$2" want="$3"
  if python3 -c "exit(0 if $got >= $want else 1)" 2>/dev/null; then
    echo "  [PASS] $name (got=$got)"; PASS=$((PASS+1))
  else
    echo "  [FAIL] $name (got=$got, want>=$want)"; FAIL=$((FAIL+1)); FAILED_NAMES="$FAILED_NAMES\n  - $name"
  fi
}
# 滚轮方向在不同 X11/GLFW 组合下可能相反，断言「确实滚动了」即可。
assert_abs_ge() {
  local name="$1" got="$2" want="$3"
  if python3 -c "exit(0 if abs($got) >= $want else 1)" 2>/dev/null; then
    echo "  [PASS] $name (delta=$got)"; PASS=$((PASS+1))
  else
    echo "  [FAIL] $name (delta=$got, want abs>=$want)"; FAIL=$((FAIL+1)); FAILED_NAMES="$FAILED_NAMES\n  - $name"
  fi
}
assert_contains() {
  local name="$1" hay="$2" needle="$3"
  if printf '%s' "$hay" | grep -qF "$needle"; then
    echo "  [PASS] $name"; PASS=$((PASS+1))
  else
    echo "  [FAIL] $name (未找到「$needle」)"; echo "         got : $hay"
    FAIL=$((FAIL+1)); FAILED_NAMES="$FAILED_NAMES\n  - $name"
  fi
}
sleep_s() { sleep "${1:-0.3}"; }
click() { xdotool mousemove "$1" "$2" click 1 >/dev/null 2>&1; sleep_s; }
key() { xdotool key --clearmodifiers "$@" >/dev/null 2>&1; sleep_s; }
# 分步拖动：GLFW 需要在 mousedown 与 mousemove 之间处理一帧，连写事件会丢失。
drag() {
  xdotool mousemove "$1" "$2" >/dev/null 2>&1; sleep 0.2
  xdotool mousedown 1 >/dev/null 2>&1; sleep 0.2
  xdotool mousemove --sync "$3" "$4" >/dev/null 2>&1; sleep 0.4
  xdotool mouseup 1 >/dev/null 2>&1; sleep 0.4
}
read_editor() {
  xdotool key --clearmodifiers ctrl+a >/dev/null 2>&1; sleep 0.2
  xdotool key --clearmodifiers ctrl+c >/dev/null 2>&1; sleep 0.2
  xclip -selection clipboard -o 2>/dev/null
}
split_center() {
  python3 -c "
import json
d=json.load(open('$STATE'))
s=[x for x in d['splitters'] if x['axis']=='$1'][0]
print(int(s['x']+s['w']/2), int(s['y']+s['h']/2))
"
}
# 底部工具窗标签的 y 从水平分隔条动态推算（分隔条底 + 标签头半高）。
# 写死绝对 y 会在布局/内容行数变化后失准，故按当前分隔条位置计算。
bottom_tab_y() {
  python3 -c "
import json
d=json.load(open('$STATE'))
s=[x for x in d['splitters'] if x['axis']=='h'][0]
print(int(s['y']+s['h'])+13)
"
}
BASE_LINE="    return <div>Hello, {name}!</div>"
# 代码文字起点：右侧编辑区 x=306 + 行号槽 42 + 间距 8 = 362。
# 注意垂直分隔条现在正确占据 x=300..306（此前 #frag 布局错位把它推到底部，
# 命中框高度 0，既拖不动也让编辑器少 6px），坐标必须按修复后的布局算。
CODE_X=362
CHAR_W=$(cd "$(dirname "$0")/.." && GOTOOLCHAIN=go1.27.4 go run ./tests/charwidth.go 2>/dev/null || true)
[ -z "$CHAR_W" ] && CHAR_W=7.8267
col_x() { python3 -c "print(int(round($CODE_X + $1 * $CHAR_W)))"; }
ROW10_Y=311

echo ""
echo "===== 鼠标交互 ====="

# --- 1. 点击定位（回归 gui_e2e 场景1，纳入统一套件） ---
echo "== 1. 点击定位 =="
click $(col_x 15) "$ROW10_Y"
key space
AFTER=$(read_editor | sed -n '10p')
assert_contains "点击 v 后空格落在 v 之后" "$AFTER" "return <div >Hello"
key ctrl+z

# --- 2. 真实鼠标拖动选区（左→右） ---
echo "== 2. 拖动选区（左→右） =="
X4=$(col_x 3); X10=$(col_x 10)
drag "$X4" "$ROW10_Y" "$X10" "$ROW10_Y"
assert_eq "正向拖动选中区间" "$(j selected_text)" " return"
xdotool key --clearmodifiers ctrl+c >/dev/null 2>&1; sleep 0.3
assert_eq "拖动后剪贴板含选中文本" "$(xclip -selection clipboard -o 2>/dev/null)" " return"

# --- 3. 真实鼠标拖动选区（右→左） ---
echo "== 3. 拖动选区（右→左） =="
drag "$X10" "$ROW10_Y" "$X4" "$ROW10_Y"
assert_eq "反向拖动选中区间" "$(j selected_text)" " return"

# --- 4. Shift+点击扩展选区 ---
echo "== 4. Shift 点击扩展 =="
click "$X4" "$ROW10_Y"
xdotool keydown Shift >/dev/null 2>&1
click "$X10" "$ROW10_Y"
xdotool keyup Shift >/dev/null 2>&1
sleep_s
assert_eq "Shift 点击扩展选区" "$(j selected_text)" " return"

# --- 5. 双击选词 ---
echo "== 5. 双击选词 =="
xdotool mousemove $(col_x 17) "$ROW10_Y" click --repeat 2 --delay 120 1 >/dev/null 2>&1
sleep_s
assert_eq "双击选中 Hello" "$(j selected_text)" "Hello"

# --- 6. 三击选行 ---
echo "== 6. 三击选行 =="
xdotool mousemove 500 "$ROW10_Y" click --repeat 3 --delay 120 1 >/dev/null 2>&1
sleep_s
# 命令替换会吞掉末尾换行，故用包含断言而非精确相等。
assert_contains "三击复制完整行" "$(j selected_text)" "$BASE_LINE"

# --- 7. 垂直分隔条拖动 + 拖后点击定位 ---
echo "== 7. 垂直分隔条拖动 =="
read VX VY <<< "$(split_center v)"
PW0=$(j project_w)
drag "$VX" "$VY" "$((VX+100))" "$VY"
PW1=$(j project_w)
assert_eq "左栏宽度随拖动增加" "$PW1" "$((PW0+100))"
assert_eq "拖拽结束后状态复位" "$(j split_active)" "False"
# 拖动后编辑器整体右移 100px，点击新起点仍应命中 col 15。
click $(( $(col_x 15) + 100 )) "$ROW10_Y"
key space
AFTER2=$(read_editor | sed -n '10p')
assert_contains "拖栏后点击定位仍正确" "$AFTER2" "return <div >Hello"
key ctrl+z
read VX2 VY2 <<< "$(split_center v)"
drag "$VX2" "$VY2" "$((VX2-100))" "$VY2"
assert_eq "左栏宽度拖回基线" "$(j project_w)" "$PW0"

# --- 8. 水平分隔条拖动 ---
echo "== 8. 水平分隔条拖动 =="
read HX HY <<< "$(split_center h)"
TH0=$(j terminal_h)
drag "$HX" "$HY" "$HX" "$((HY-80))"
assert_eq "终端高度随拖动增加" "$(j terminal_h)" "$((TH0+80))"
read HX2 HY2 <<< "$(split_center h)"
drag "$HX2" "$HY2" "$HX2" "$((HY2+80))"
assert_eq "终端高度拖回基线" "$(j terminal_h)" "$TH0"

# --- 9. 滚轮滚动 ---
echo "== 9. 滚轮滚动 =="
python3 -c "print('\n'.join('line%02d'%i for i in range(40)))" | xclip -selection clipboard
key ctrl+a
key ctrl+v
# 直接读受控滚动偏移：用命中行号反推会被光标跟随/自动滚到底干扰。
# 粘贴后光标在文末、滚动已在底部，故先向上滚再向下滚，双向都断言。
ST0=$(j scroll_top)
xdotool mousemove 500 300 >/dev/null 2>&1
xdotool click --repeat 12 --delay 60 4 >/dev/null 2>&1
sleep_s
ST1=$(j scroll_top)
xdotool click --repeat 12 --delay 60 5 >/dev/null 2>&1
sleep_s
ST2=$(j scroll_top)
D_UP=$(python3 -c "print(int(round(float('$ST1') - float('$ST0'))))")
D_DOWN=$(python3 -c "print(int(round(float('$ST2') - float('$ST1'))))")
echo "  滚动偏移: $ST0 -> $ST1 -> $ST2 (up=$D_UP down=$D_DOWN)"
assert_abs_ge "滚轮向上改变滚动偏移" "$D_UP" 40
assert_abs_ge "滚轮向下改变滚动偏移" "$D_DOWN" 40
key ctrl+z
assert_contains "撤销粘贴恢复示例" "$(j editor_text)" "$BASE_LINE"

# --- 10. 项目面板显隐 ---
echo "== 10. 项目面板显隐 =="
click 24 48
assert_eq "点击 folder 隐藏项目面板" "$(j project_panel_visible)" "False"
click 24 48
assert_eq "再次点击恢复项目面板" "$(j project_panel_visible)" "True"

# --- 11. 主题切换 ---
echo "== 11. 主题切换 =="
click 1374 48
assert_eq "点击月亮切到浅色主题" "$(j light_theme)" "True"
click 1374 48
assert_eq "再次点击切回深色主题" "$(j light_theme)" "False"

# --- 12. 菜单开合 ---
echo "== 12. 菜单开合 =="
click 26 15
assert_eq "点击 File 打开菜单" "$(j menu)" "File"
click 700 400
assert_eq "点击空白关闭菜单" "$(j menu)" ""

# --- 13. Run 按钮 ---
echo "== 13. Run 按钮 =="
click 226 48
sleep 1
RUN_INFO=$(j run_info)
assert_ne "Run 后有运行结果" "$RUN_INFO" "预览模式 · 点击 Run 解释当前源码"
if printf '%s' "$RUN_INFO" | grep -q '0x[0-9a-f]'; then
  echo "  [FAIL] 运行结果含指针乱码"; FAIL=$((FAIL+1)); FAILED_NAMES="$FAILED_NAMES\n  - 运行结果无指针乱码"
else
  echo "  [PASS] 运行结果无指针乱码"; PASS=$((PASS+1))
fi

echo ""
echo "===== 键盘交互 ====="

# --- 14. 全选 ---
echo "== 14. Ctrl+A 全选 =="
key ctrl+a
assert_eq "全选置位" "$(j has_selection)" "True"
SEL_LEN=$(python3 -c "import json;print(len(json.load(open('$STATE'))['selected_text']))")
assert_ge "全选覆盖整篇文本" "$SEL_LEN" 50

# --- 15. 复制 / 粘贴 ---
echo "== 15. Ctrl+C / Ctrl+V =="
xdotool key --clearmodifiers ctrl+c >/dev/null 2>&1; sleep 0.2
COPIED=$(xclip -selection clipboard -o 2>/dev/null)
printf '%s' "KEYBOARD-PASTE" | xclip -selection clipboard
key ctrl+v
assert_contains "剪贴板内容粘贴进编辑器" "$(j editor_text)" "KEYBOARD-PASTE"
assert_contains "复制拿到编辑器全文" "$COPIED" "package main"
key ctrl+z

# --- 16. 光标移动键 ---
echo "== 16. 方向键 / Ctrl+方向 / Home / End / Shift+End =="
click $(col_x 3) "$ROW10_Y"
C0=$(j editor_col)
key Right Right Right
C1=$(j editor_col)
assert_eq "Right 右移 3 列" "$((C1-C0))" 3
key ctrl+Right
C2=$(j editor_col)
assert_ge "Ctrl+Right 跳到下一个词" "$C2" "$((C1+1))"
key Home
assert_eq "Home 到首个非空白" "$(j editor_col)" 4
key End
assert_eq "End 到行尾" "$(j editor_col)" 36
key Home
key shift+End
assert_ne "Shift+End 产生选区" "$(j selected_text)" ""

# --- 17. 中文输入 ---
echo "== 17. 中文输入 =="
click $(col_x 4) "$ROW10_Y"
# xdotool type 对多字节中文支持不稳定（常丢字），走剪贴板粘贴走真实 Ctrl+V 路径。
printf '%s' '测试' | xclip -selection clipboard
key ctrl+v
assert_contains "中文按 rune 插入" "$(j editor_text)" "测试"
key ctrl+z

# --- 18. Backspace / Delete ---
echo "== 18. Backspace / Delete =="
click $(col_x 4) "$ROW10_Y"
xdotool type --clearmodifiers 'Z' >/dev/null 2>&1; sleep_s
L10=$(python3 -c "import json;print(json.load(open('$STATE'))['editor_text'].split(chr(10))[9])")
assert_contains "先插入 Z" "$L10" "Z"
key BackSpace
if printf '%s' "$(j editor_text)" | grep -qF '    Zreturn'; then
  echo "  [FAIL] Backspace 未删除插入字符"; FAIL=$((FAIL+1)); FAILED_NAMES="$FAILED_NAMES\n  - Backspace 删除"
else
  echo "  [PASS] Backspace 删除插入字符"; PASS=$((PASS+1))
fi
click $(col_x 4) "$ROW10_Y"
key Delete
if printf '%s' "$(j editor_text)" | grep -qF "$BASE_LINE"; then
  echo "  [FAIL] Delete 未删除字符"; FAIL=$((FAIL+1)); FAILED_NAMES="$FAILED_NAMES\n  - Delete 删除"
else
  echo "  [PASS] Delete 删除字符"; PASS=$((PASS+1))
fi
key ctrl+z

# --- 19. 撤销 / 重做 ---
echo "== 19. Ctrl+Z / Ctrl+Y =="
click $(col_x 4) "$ROW10_Y"
xdotool type --clearmodifiers 'UNDO ME' >/dev/null 2>&1; sleep_s
assert_contains "插入待撤销文本" "$(j editor_text)" "UNDO ME"
key ctrl+z
if printf '%s' "$(j editor_text)" | grep -qF 'UNDO ME'; then
  echo "  [FAIL] Ctrl+Z 未撤销"; FAIL=$((FAIL+1)); FAILED_NAMES="$FAILED_NAMES\n  - Ctrl+Z 撤销"
else
  echo "  [PASS] Ctrl+Z 撤销"; PASS=$((PASS+1))
fi
key ctrl+y
assert_contains "Ctrl+Y 重做" "$(j editor_text)" "UNDO ME"
key ctrl+z

# --- 20. Enter 换行 ---
echo "== 20. Enter 换行 =="
LINES0=$(python3 -c "import json;print(len(json.load(open('$STATE'))['editor_text'].split(chr(10))))")
click $(col_x 10) "$ROW10_Y"
key Return
LINES1=$(python3 -c "import json;print(len(json.load(open('$STATE'))['editor_text'].split(chr(10))))")
assert_eq "回车增加一行" "$LINES1" "$((LINES0+1))"
key ctrl+z

echo ""
echo "===== 外壳与窗口交互 ====="

# --- 21. 底部工具窗标签切换 ---
echo "== 21. 底部 Terminal / Run / Problems 标签 =="
assert_eq "默认底部标签为 Terminal" "$(j bottom_tab)" "Terminal"
BTY=$(bottom_tab_y)
click 410 "$BTY"
assert_eq "点击 Run 切换底部标签" "$(j bottom_tab)" "Run"
click 490 "$BTY"
assert_eq "点击 Problems 切换底部标签" "$(j bottom_tab)" "Problems"
click 345 "$BTY"
assert_eq "点击 Terminal 切回" "$(j bottom_tab)" "Terminal"

# --- 22. 项目树：点击文件打开标签 + 标签切换 ---
echo "== 22. 项目树打开标签 + 编辑器标签切换 =="
ROWS_BEFORE=$(j project_visible_rows)
click 100 183
TABS=$(python3 -c "import json;print('|'.join(json.load(open('$STATE'))['tabs']))")
assert_contains "点击项目树文件打开新标签" "$TABS" "Counter"
assert_eq "打开文件成为活动标签" "$(j active_tab)" "02 Counter.gx"
# 标签栏 y=66..98；两个标签分别在 x≈360 / x≈490。
click 360 82
assert_eq "点击第一个标签切回 Hello" "$(j active_tab)" "01 Hello GX.gx"
click 490 82
assert_eq "点击第二个标签切回 Counter" "$(j active_tab)" "02 Counter.gx"
assert_eq "标签切换不影响项目树可见行" "$(j project_visible_rows)" "$ROWS_BEFORE"

# --- 23. 项目树展开 / 折叠 ---
echo "== 23. 项目树展开 / 折叠 =="
EXP_BEFORE=$(j project_expanded_count)
VIS_BEFORE=$(j project_visible_rows)
# 工具按钮行：折叠全部 x≈90，展开全部 x≈60，y≈128。
click 90 128
VIS_COLLAPSED=$(j project_visible_rows)
EXP_COLLAPSED=$(j project_expanded_count)
if python3 -c "exit(0 if $VIS_COLLAPSED < $VIS_BEFORE else 1)"; then
  echo "  [PASS] 折叠后可见行数减少"; PASS=$((PASS+1))
else
  echo "  [FAIL] 折叠后可见行数未减少 ($VIS_BEFORE -> $VIS_COLLAPSED)"; FAIL=$((FAIL+1)); FAILED_NAMES="$FAILED_NAMES\n  - 折叠项目树"
fi
click 60 128
assert_eq "展开后恢复可见行数" "$(j project_visible_rows)" "$VIS_BEFORE"
assert_eq "展开后恢复展开目录数" "$(j project_expanded_count)" "$EXP_BEFORE"

# --- 24. 窗口 resize ---
echo "== 24. 窗口 resize =="
W0=$(j window_w); H0=$(j window_h)
WID=$(xdotool search --name 'GoX IDE' 2>/dev/null | head -1)
if [ -n "$WID" ]; then
  xdotool windowsize "$WID" 1200 700 >/dev/null 2>&1; sleep_s 0.5
  assert_eq "窗口宽度随 resize 变化" "$(j window_w)" 1200
  assert_eq "窗口高度随 resize 变化" "$(j window_h)" 700
  # resize 后界面仍可交互：底部标签点击照常生效（坐标按 resize 后布局重算）。
  BTY2=$(bottom_tab_y)
  click 490 "$BTY2"
  assert_eq "resize 后底部标签仍可点击" "$(j bottom_tab)" "Problems"
  xdotool windowsize "$WID" "$W0" "$H0" >/dev/null 2>&1; sleep_s 0.5
  assert_eq "窗口尺寸可恢复" "$(j window_w)" "$W0"
else
  echo "  [SKIP] 未找到窗口，resize 场景跳过"
fi

echo ""
echo "== 汇总：PASS=$PASS FAIL=$FAIL =="
if [ "$FAIL" -gt 0 ]; then
  printf '失败项:\n%b\n' "$FAILED_NAMES"
  echo "结论：检测到 $FAIL 项交互缺陷。"
  exit 1
fi
echo "结论：全部交互断言通过。"
exit 0
