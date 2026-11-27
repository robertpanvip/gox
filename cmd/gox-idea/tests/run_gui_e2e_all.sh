#!/usr/bin/env bash
# GUI E2E 统一入口（本地与 CI 共用）：依次跑三套脚本并汇总退出码。
#
# 平台差异收敛在这里和 shims/：
#   - Linux：直接调用系统原生 Xvfb/xdotool/xclip/import，三套脚本零改动。
#   - Windows：把 shims/ 放到 PATH 最前，脚本调用的 xdotool/xclip/Xvfb/import/
#     python3 全部被 shim 拦截，转发给 gui_driver.py（pyautogui 实现）。
#
# 用法：
#   GOX_IDEA_BIN=/path/to/gox-idea ./run_gui_e2e_all.sh
# CI 里 Windows 侧调用前需：pip install pyautogui pygetwindow pillow
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
PLATFORM="$(python3 "$HERE/gui_driver.py" env 2>/dev/null || echo linux)"

if [ "$PLATFORM" = windows ]; then
  # shims 优先：拦截脚本里的 X11 工具调用。
  export PATH="$HERE/shims:$PATH"
  echo "== 平台=Windows，已启用 shims/ =="
else
  echo "== 平台=Linux，使用系统原生 X11 工具 =="
fi

SCRIPTS=(gui_e2e.sh gui_interactions_e2e.sh gui_visual_flow_e2e.sh)
PASS=0
FAIL=0
FAILED=""
for s in "${SCRIPTS[@]}"; do
  echo ""
  echo "================ $s ================"
  if bash "$HERE/$s" ${1:-}; then
    echo ">>> $s : OK"; PASS=$((PASS+1))
  else
    echo ">>> $s : FAILED"; FAIL=$((FAIL+1)); FAILED="$FAILED $s"
  fi
done

echo ""
echo "================ 汇总 ================"
echo "套件通过=$PASS 失败=$FAIL${FAILED:+ ; 失败套件:$FAILED}"
[ "$FAIL" -eq 0 ]
