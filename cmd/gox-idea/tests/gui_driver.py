#!/usr/bin/env python3
"""跨平台 GUI 自动化驱动：把 xdotool / xclip / Xvfb / import 收敛到一处。

三个 GUI E2E 脚本一行不改。Windows 上把它们依赖的命令行工具替换为 shim
（shims/ 目录，加入 PATH 前面），shim 把原始参数转发给本驱动的 xdotool /
xclip / screenshot 子命令。Linux 上 shim 不启用，脚本照常调用系统原生工具。

子命令：
  env                          打印平台：linux 或 windows
  xdotool ARGS...              原样执行 xdotool 命令行（Linux 透传；Windows 模拟）
  xclip ARGS...                原样执行 xclip 命令行（stdin/stdout 为剪贴板内容）
  screenshot PATH              截全屏到 PATH（等价于 import -window root PATH）
  find-window                  找到 GoX IDE 窗口返回 1，否则 0
  windowsize W H               调整 GoX IDE 窗口尺寸（Windows 用 pygetwindow）

Windows 模拟支持的 xdotool 子命令（覆盖三个脚本全部用法）：
  mousemove X Y [--sync] [click BTN]
  click BTN [--repeat N --delay MS]
  mousedown BTN / mouseup BTN
  keydown KEY / keyup KEY
  key [--clearmodifiers] KEY [KEY...]
  type [--clearmodifiers] TEXT
  search --name NAME            （输出占位窗口 id）
  getwindowgeometry WID         （no-op）
  windowsize WID W H

依赖（仅 Windows）：pyautogui、pygetwindow；剪贴板走 PowerShell。
由 CI 在 Windows 侧安装；Linux 侧零额外依赖。
"""
import base64
import platform
import subprocess
import sys
import time

IS_WIN = platform.system() == "Windows"
DEVNULL = subprocess.DEVNULL

# 受测 GUI 程序标题关键字：gox-idea IDE 与 Desktop Playground 都要能被驱动定位。
WINDOW_TITLES = ("GoX IDE", "GX Playground")


def find_windows(gw):
    return [w for w in gw.getAllWindows() if any(t in w.title for t in WINDOW_TITLES)]


def run(args, **kw):
    return subprocess.run(args, check=False, **kw)


# ---------------- xdotool 命令行解析（供 Windows 模拟） ----------------

KEYMAP = {
    "space": "space", "Return": "enter", "Enter": "enter", "BackSpace": "backspace",
    "Backspace": "backspace", "Delete": "delete", "Home": "home", "End": "end",
    "Right": "right", "Left": "left", "Up": "up", "Down": "down", "Tab": "tab",
    "Escape": "esc", "Esc": "esc", "ctrl": "ctrl", "shift": "shift", "alt": "alt",
}


def win_press(pg, token):
    """按 xdotool 的 'ctrl+a' / 'shift+End' / 'Return' 语义按键。"""
    parts = token.split("+")
    keys = [KEYMAP.get(p, p.lower()) for p in parts]
    if len(keys) == 1:
        pg.press(keys[0])
    else:
        pg.hotkey(*keys)


def win_xdotool(args):
    import pyautogui as pg
    pg.FAILSAFE = False
    pg.PAUSE = 0.02
    i = 0
    n = len(args)
    while i < n:
        op = args[i]
        i += 1
        if op == "mousemove":
            # 可选 --sync；X Y；可选尾随 click BTN
            if i < n and args[i] == "--sync":
                i += 1
            x, y = int(args[i]), int(args[i + 1]); i += 2
            pg.moveTo(x, y)
            if i < n and args[i] == "click":
                i += 1
                btn = args[i]; i += 1
                _win_click(pg, [btn])
        elif op == "click":
            rest = args[i:]; i = n
            _win_click(pg, rest)
        elif op == "mousedown":
            pg.mouseDown(); i += 1
        elif op == "mouseup":
            pg.mouseUp(); i += 1
        elif op == "keydown":
            pg.keyDown(KEYMAP.get(args[i], args[i].lower())); i += 1
        elif op == "keyup":
            pg.keyUp(KEYMAP.get(args[i], args[i].lower())); i += 1
        elif op == "key":
            for t in args[i:]:
                if t == "--clearmodifiers":
                    continue
                win_press(pg, t)
            i = n
        elif op == "type":
            text = args[i]
            if text == "--clearmodifiers":
                text = args[i + 1]
            pg.write(text, interval=0.02)
            i = n
        elif op == "search":
            # --name NAME → 返回占位窗口 id（find-window 用真实查找）
            print("1")
            i = n
        elif op == "getwindowgeometry":
            # no-op：脚本只把它当信息打印，不参与断言
            i += 1
        elif op == "windowsize":
            # WID W H → 跳过 WID，用后两个参数
            i += 1
            import pygetwindow as gw
            wins = find_windows(gw)
            if wins and i + 1 < n:
                wins[0].resizeTo(int(args[i]), int(args[i + 1]))
            i = n
        else:
            # 未知子命令：忽略，避免脚本整体崩
            pass
    return 0


def _win_click(pg, rest):
    reps, delay = 1, 0
    btn = "1"
    j = 0
    while j < len(rest):
        if rest[j] == "--repeat":
            reps = int(rest[j + 1]); j += 2
        elif rest[j] == "--delay":
            delay = int(rest[j + 1]); j += 2
        else:
            btn = rest[j]; j += 1
    interval = delay / 1000.0 if delay else 0.0
    # xdotool: 4=滚轮上，5=滚轮下；pyautogui 用 scroll(+N/-N)。
    if btn in ("4", "5"):
        pg.scroll(reps if btn == "4" else -reps)
        return
    pg.click(clicks=reps, interval=interval, button="left")


# ---------------- Windows 剪贴板（PowerShell，规避引号转义） ----------------

def win_clip_set(text):
    b64 = base64.b64encode(text.encode("utf-8")).decode("ascii")
    ps = ("$t=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('%s'));"
          "Set-Clipboard -Value $t" % b64)
    run(["powershell", "-NoProfile", "-Command", ps], stdout=DEVNULL, stderr=DEVNULL)


def win_clip_get():
    r = run(["powershell", "-NoProfile", "-Command", "Get-Clipboard -Raw"],
            capture_output=True)
    txt = r.stdout.decode("utf-8-sig", errors="replace")
    if txt.endswith("\r\n"):
        txt = txt[:-2]
    elif txt.endswith("\n"):
        txt = txt[:-1]
    sys.stdout.write(txt)


# ---------------- 命令分发 ----------------

def main():
    if len(sys.argv) < 2:
        print(__doc__)
        return 2
    cmd, args = sys.argv[1], sys.argv[2:]

    if cmd == "env":
        print("windows" if IS_WIN else "linux")
        return 0

    if cmd == "xdotool":
        if IS_WIN:
            return win_xdotool(args)
        return run(["xdotool", *args]).returncode

    if cmd == "xclip":
        if IS_WIN:
            if "-o" in args:
                win_clip_get()
            else:
                win_clip_set(sys.stdin.read())
            return 0
        if "-o" in args:
            r = run(["xclip", "-selection", "clipboard", "-o"], capture_output=True)
            sys.stdout.buffer.write(r.stdout)
            return 0
        return run(["xclip", "-selection", "clipboard"], input=sys.stdin.buffer.read()).returncode

    if cmd == "screenshot":
        path = args[0]
        if IS_WIN:
            import pyautogui as pg
            pg.screenshot().save(path)
        else:
            run(["import", "-window", "root", path], stdout=DEVNULL, stderr=DEVNULL)
        return 0

    if cmd == "find-window":
        if IS_WIN:
            import pygetwindow as gw
            wins = find_windows(gw)
            print("1" if wins else "0")
        else:
            found = False
            for title in WINDOW_TITLES:
                r = run(["xdotool", "search", "--name", title], capture_output=True, text=True)
                if r.stdout.strip():
                    found = True
                    break
            print("1" if found else "0")
        return 0

    if cmd == "windowsize":
        if IS_WIN:
            import pygetwindow as gw
            wins = find_windows(gw)
            if wins:
                wins[0].resizeTo(int(args[0]), int(args[1]))
        else:
            wid = ""
            for title in WINDOW_TITLES:
                r = run(["xdotool", "search", "--name", title], capture_output=True, text=True)
                if r.stdout.strip():
                    wid = r.stdout.strip().splitlines()[0]
                    break
            if wid:
                run(["xdotool", "windowsize", wid, args[0], args[1]], stdout=DEVNULL)
        return 0

    print(f"未知命令: {cmd}", file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main())
