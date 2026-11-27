#!/usr/bin/env python3
"""Playground Android 端 E2E 测试套件（adb + logcat 驱动）。

依赖应用内的 GXTEST 测试桥（examples/playground/android/main.go 周期输出）：
    GXTEST state  {json}   应用状态快照（text/sample/count/line/col/scrollTop...）
    GXTEST dpr    <float>  物理→逻辑像素缩放（触摸注入需乘回 dpr）
    GXTEST elems  {json}   关键可点元素命中框（逻辑坐标）：
                            key-<data-key> / tool-<data-action> /
                            sample-<data-index> / editor-top / editor-bottom

每个用例对应一个用户可感知的功能点，独立判定、失败互不影响。
退出码 0 = 全部通过；非 0 = 有失败。

用法：
    python3 scripts/e2e/playground_android.py [--serial emulator-5554]
    [--apk /tmp/GX-Playground.apk] [--no-install]

前置：模拟器已引导（sys.boot_completed=1），adb 可用。
"""
from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
import time

DEFAULT_SERIAL = "emulator-5554"
APP_PKG = "org.golang.todo.android"

STATE_RE = re.compile(r"GXTEST state (\{.*\})")
ELEMS_RE = re.compile(r"GXTEST elems (\{.*\})")
DPR_RE = re.compile(r"GXTEST dpr ([0-9.]+)")


class Bridge:
    """GXTEST 测试桥：adb 交互 + logcat 解析。"""

    def __init__(self, serial: str):
        self.serial = serial

    def sh(self, *args: str, timeout: int = 20) -> str:
        r = subprocess.run(
            ["adb", "-s", self.serial, *args],
            capture_output=True, text=True, timeout=timeout,
        )
        return r.stdout

    def clear_log(self) -> None:
        self.sh("logcat", "-c")

    def latest(self, regex: re.Pattern, since_clear: bool = True) -> str | None:
        out = self.sh("logcat", "-d", "-s", "GoLog:I", timeout=30)
        hits = regex.findall(out)
        return hits[-1] if hits else None

    def state(self, wait: float = 0) -> dict | None:
        if wait:
            time.sleep(wait)
        raw = self.latest(STATE_RE)
        if not raw:
            return None
        try:
            return json.loads(raw)
        except json.JSONDecodeError:
            return None

    def elems(self, wait: float = 0) -> dict | None:
        if wait:
            time.sleep(wait)
        raw = self.latest(ELEMS_RE)
        if not raw:
            return None
        try:
            return json.loads(raw)
        except json.JSONDecodeError:
            return None

    def dpr(self) -> float:
        raw = self.latest(DPR_RE)
        if not raw:
            return 1.0
        try:
            return float(raw)
        except ValueError:
            return 1.0

    def tap_logical(self, bbox: str) -> None:
        """点击一个逻辑坐标 bbox（"x y w h"）：换算为物理像素注入触摸。"""
        x, y, w, h = (int(v) for v in bbox.split())
        d = self.dpr()
        cx, cy = int((x + w / 2) * d), int((y + h / 2) * d)
        self.sh("shell", "input", "tap", str(cx), str(cy))

    def tap_key(self, elems: dict, key: str) -> bool:
        b = elems.get("key-" + key)
        if not b:
            return False
        self.tap_logical(b)
        return True

    def screen_size(self) -> tuple[int, int]:
        out = self.sh("shell", "wm", "size")
        m = re.search(r"(\d+)x(\d+)", out)
        return (int(m.group(1)), int(m.group(2))) if m else (0, 0)

    def wait_state(self, timeout: float = 60) -> dict | None:
        deadline = time.time() + timeout
        while time.time() < deadline:
            st = self.state()
            if st:
                return st
            time.sleep(2)
        return None


# ---------------------------------------------------------------- 用例

def case_boot(br: Bridge) -> tuple[bool, str]:
    """功能：应用启动。进程存活、state 测试桥输出、场景树挂载。"""
    st = br.wait_state(45)
    if not st:
        return False, "60s 内未收到 GXTEST state（应用未渲染或测试桥未输出）"
    n = int(st.get("count", "0") or 0)
    if n <= 0:
        return False, f"state.count={n}，编辑器无内容"
    return True, f"启动成功，编辑器 {n} 行"


def case_keyboard_layout(br: Bridge) -> tuple[bool, str]:
    """功能：虚拟键盘渲染。按键数量、命名完整、全部落在视口内。"""
    elems = br.elems(2)
    if not elems:
        return False, "未收到 GXTEST elems"
    keys = {k: v for k, v in elems.items() if k.startswith("key-")}
    if len(keys) < 20:
        return False, f"虚拟键盘按键不足（{len(keys)} < 20）"
    sw, sh = br.screen_size()
    d = br.dpr()
    out = []
    for name, bbox in keys.items():
        x, y, w, h = (int(v) for v in bbox.split())
        # 逻辑坐标 → 物理坐标，检查是否溢出屏幕（回归：键盘被挤出视口）
        if y * d >= sh * 0.98 or (y + h) * d > sh * 1.02 or x < -1:
            out.append(f"{name}={bbox}")
    if out:
        return False, f"按键超出视口：{', '.join(out[:5])}"
    expected = {"key-shift", "key-backspace", "key-space", "key-enter",
                "key-undo", "key-redo", "key-home", "key-end"}
    missing = expected - set(keys)
    if missing:
        return False, f"功能键缺失：{sorted(missing)}"
    return True, f"键盘 {len(keys)} 键全部在视口内，功能键齐全"


def case_vk_type(br: Bridge) -> tuple[bool, str]:
    """功能：虚拟键盘输入。点击字符键，编辑器 text 增长。"""
    elems = br.elems()
    if not elems or "key-a" not in elems:
        return False, "elems 中无 key-a"
    before = br.state(1) or {}
    t0 = str(before.get("text", ""))
    if not br.tap_key(elems, "a"):
        return False, "key-a 点击失败"
    st = br.state(2)
    if not st:
        return False, "点击后未收到 state"
    t1 = str(st.get("text", ""))
    if "a" not in t1 or len(t1) != len(t0) + 1:
        return False, f"输入未生效（{len(t0)} → {len(t1)}）"
    return True, "字符键输入生效"


def case_vk_shift(br: Bridge) -> tuple[bool, str]:
    """功能：Shift 切换。点击 shift 后输入为大写。"""
    elems = br.elems()
    if not br.tap_key(elems or {}, "shift"):
        return False, "key-shift 不存在"
    time.sleep(0.5)
    elems = br.elems()
    if not br.tap_key(elems or {}, "q"):
        return False, "key-q 不存在"
    st = br.state(2)
    if not st:
        return False, "点击后未收到 state"
    t = str(st.get("text", ""))
    if not t.endswith("Q"):
        return False, f"Shift 后应输入 'Q'，实际 text 末尾：{t[-10:]!r}"
    return True, "Shift → 大写输入生效"


def case_vk_backspace(br: Bridge) -> tuple[bool, str]:
    """功能：退格删除。"""
    elems = br.elems()
    if not br.tap_key(elems or {}, "backspace"):
        return False, "key-backspace 不存在"
    st = br.state(2)
    if not st:
        return False, "点击后未收到 state"
    t = str(st.get("text", ""))
    if t.endswith("Q") or t.endswith("q"):
        return False, "退格未删除字符"
    return True, "退格删除生效"


def case_sample_switch(br: Bridge) -> tuple[bool, str]:
    """功能：示例切换。点击 tool-next，state.sample 变化。"""
    elems = br.elems()
    if not elems or "tool-next" not in elems:
        return False, "elems 中无 tool-next"
    before = (br.state(1) or {}).get("sample", "")
    br.tap_logical(elems["tool-next"])
    st = br.state(3)
    if not st:
        return False, "点击后未收到 state"
    after = st.get("sample", "")
    if after == before:
        return False, f"示例未切换（仍为 {after!r}）"
    return True, f"示例切换 {before!r} → {after!r}"


def load_sample(br: Bridge, idx: int = 0, timeout: float = 6) -> bool:
    """点击示例列表第 idx 行重载源码，确保编辑器非空。"""
    elems = br.elems(1)
    if not elems or f"sample-{idx}" not in elems:
        return False
    br.tap_logical(elems[f"sample-{idx}"])
    deadline = time.time() + timeout
    while time.time() < deadline:
        st = br.state()
        if st and str(st.get("text", "")) != "":
            return True
        time.sleep(1)
    return False


def case_run(br: Bridge) -> tuple[bool, str]:
    """功能：代码运行。加载示例后点击 tool-run，runErr 为空且 run 戳更新。"""
    if not load_sample(br, 0):
        return False, "无法加载示例 0（编辑器无源码）"
    elems = br.elems(1)
    if not elems or "tool-run" not in elems:
        return False, "elems 中无 tool-run"
    before = br.state(1) or {}
    br.tap_logical(elems["tool-run"])
    st = br.state(5)
    if not st:
        return False, "点击后未收到 state"
    if st.get("runErr"):
        return False, f"run 报错：{st['runErr']}"
    if before and st.get("runStamp") == before.get("runStamp"):
        return False, "runStamp 未更新（运行未执行）"
    return True, "运行成功，无报错"


def case_swipe_scroll(br: Bridge) -> tuple[bool, str]:
    """功能：编辑器滚动。先造足行数（连按 Enter），再上滑检查 scrollTop。"""
    if not load_sample(br, 0):
        return False, "无法加载示例 0"
    elems = br.elems(1)
    if not elems or "editor-top" not in elems or "editor-bottom" not in elems:
        return False, "elems 中无 editor-top/editor-bottom"
    top = [int(v) for v in elems["editor-top"].split()]
    bot = [int(v) for v in elems["editor-bottom"].split()]
    d = br.dpr()
    # 聚焦编辑器后连按 Enter 生成 ~40 行
    cx = int((top[0] + top[2] / 2) * d)
    cy = int((top[1] + top[3] / 2) * d)
    br.sh("shell", "input", "tap", str(cx), str(cy))
    time.sleep(1)
    if "key-enter" not in (br.elems() or {}):
        return False, "key-enter 不存在"
    for _ in range(40):
        br.tap_key(br.elems() or {}, "enter")
    time.sleep(1)
    cx = int((top[0] + top[2] / 2) * d)
    y0 = int((bot[1] + bot[3] * 0.6) * d)
    y1 = int((top[1] + top[3] * 0.4) * d)
    before = (br.state(1) or {}).get("scroll", "0")
    br.sh("shell", "input", "swipe", str(cx), str(y0), str(cx), str(y1), "300")
    st = br.state(3)
    if not st:
        return False, "滑动后未收到 state"
    after = str(st.get("scroll", "0"))
    try:
        moved = float(after) != float(before)
    except ValueError:
        moved = after != before
    if not moved:
        return False, f"scroll 未变化（{before} → {after}）"
    return True, f"编辑器滚动生效（{before} → {after}）"


CASES = [
    ("启动与测试桥", case_boot),
    ("虚拟键盘布局", case_keyboard_layout),
    ("虚拟键盘输入", case_vk_type),
    ("Shift 大写", case_vk_shift),
    ("退格删除", case_vk_backspace),
    ("示例切换", case_sample_switch),
    ("代码运行", case_run),
    ("编辑器滚动", case_swipe_scroll),
]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--serial", default=DEFAULT_SERIAL)
    ap.add_argument("--apk", default="/tmp/GX-Playground.apk")
    ap.add_argument("--no-install", action="store_true")
    args = ap.parse_args()

    br = Bridge(args.serial)

    # 设备就绪检查
    state = br.sh("get-state", timeout=10).strip()
    if state != "device":
        print(f"设备 {args.serial} 不可用（{state}）", file=sys.stderr)
        return 2

    if not args.no_install:
        print("[prep] 安装 APK ...")
        out = br.sh("install", "-r", "-g", args.apk, timeout=180)
        if "Success" not in out:
            print(f"安装失败：{out}", file=sys.stderr)
            return 2

    print("[prep] 启动应用 ...")
    br.clear_log()
    br.sh("shell", "am", "force-stop", APP_PKG)
    br.sh("shell", "am", "start", "-n", f"{APP_PKG}/org.golang.app.GoNativeActivity",
          timeout=30)
    br.wait_state(60)

    passed = failed = 0
    for name, fn in CASES:
        try:
            ok, msg = fn(br)
        except Exception as e:  # adb 超时等环境错误不影响后续用例
            ok, msg = False, f"异常：{e}"
        mark = "PASS" if ok else "FAIL"
        print(f"[{mark}] {name}: {msg}")
        if ok:
            passed += 1
        else:
            failed += 1
        # 用例间复位：清空文本便于下一个用例断言
        elems = br.elems()
        if elems and "key-selectall" in elems:
            br.tap_key(elems, "selectall")
            elems = br.elems()
            if elems and "key-backspace" in elems:
                br.tap_key(elems, "backspace")
        time.sleep(1)

    print(f"\n合计：{passed} 通过，{failed} 失败")
    return 0 if failed == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
