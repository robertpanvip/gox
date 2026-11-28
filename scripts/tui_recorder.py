#!/usr/bin/env python3
"""录制 gox-tui 交互过程：真 PTY 启动 → 逐步按键 → 每步快照 → 渲染 GIF + 长图 PNG。

定位：gox-tui 的可视化回归/演示工具。曾用它定位三处渲染 bug：
emoji 贴图超单元格遮挡相邻字、逐格画背景擦笔画、pyte 十六进制色解析缺 #。

用法：
    python3 scripts/tui_recorder.py \
        --bin /tmp/gox-tui-linux --demo /tmp/demo \
        --gif out.gif --png out.png

依赖：pip install pyte pillow；系统需 Noto Sans CJK / Noto Color Emoji 字体。
"""
import argparse
import fcntl
import os
import pty
import select
import struct
import termios
import time

import pyte
from PIL import Image, ImageDraw, ImageFont


def parse_args():
    p = argparse.ArgumentParser(description="gox-tui PTY 录制器")
    p.add_argument("--bin", default="/tmp/gox-tui-linux", help="gox-tui 可执行文件路径")
    p.add_argument("--demo", default="/tmp/demo", help="示例目录（-path 参数）")
    p.add_argument("--gif", default="/workspace/gox-tui-demo.gif", help="GIF 输出路径")
    p.add_argument("--png", default="/workspace/gox-tui-demo-long.png", help="长图输出路径")
    return p.parse_args()


ARGS = parse_args()
BIN = ARGS.bin
DEMO = ARGS.demo
COLS, ROWS = 110, 30
# 单元格尺寸必须与字体严格匹配：22px 等宽 CJK 的 ASCII advance=11px(CW)、
# 中文=22px(2*CW)，总高(ascent26+descent7)=33px <= CH，墨迹不会侵入上下行。
CW, CH = 11, 34

screen = pyte.Screen(COLS, ROWS)
stream = pyte.ByteStream(screen)

os.environ["COLORTERM"] = "truecolor"
master, slave = pty.openpty()
fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
proc_pid = os.fork()
if proc_pid == 0:
    os.close(master)
    os.setsid()
    fcntl.ioctl(slave, termios.TIOCSCTTY, 0)
    env = {k: v for k, v in os.environ.items()}
    env["TERM"] = "xterm-256color"
    env["COLORTERM"] = "truecolor"
    os.dup2(slave, 0); os.dup2(slave, 1); os.dup2(slave, 2)
    os.execve(BIN, [BIN, "-path", DEMO], env)
os.close(slave)

buf_out = b""
pending = bytearray()

def pump(duration):
    """读取 duration 秒内的输出喂给 pyte，自动应答探测查询。"""
    global buf_out
    end = time.time() + duration
    while True:
        remain = end - time.time()
        if remain <= 0:
            break
        r, _, _ = select.select([master], [], [], min(0.05, remain))
        if not r:
            continue
        try:
            data = os.read(master, 65536)
        except OSError:
            break
        if not data:
            break
        # 应答 OSC11 背景色查询与 DSR 光标位置查询
        if b"\x1b]11;?" in data:
            os.write(master, b"\x1b]11;rgb:1c1c/1c1c/1e1e\x1b\\")
        if b"\x1b[6n" in data:
            os.write(master, b"\x1b[1;1R")
        buf_out += data
        stream.feed(data)

def send(keys, settle=1.3):
    os.write(master, keys.encode())
    pump(settle)

frames = []

def snap(label):
    img = render(screen)
    frames.append((img, label))

COLOR_CACHE = {}
def rgb(color):
    """pyte 存颜色为无 # 的 6 位 hex 或 'default'/命名色。"""
    if color is None or color == "default":
        return None
    c = color.lstrip("#")
    if len(c) == 6:
        try:
            return tuple(int(c[i:i+2], 16) for i in (0, 2, 4))
        except ValueError:
            return None
    NAMED = {"black":(0,0,0),"red":(205,49,49),"green":(13,188,121),"yellow":(229,229,16),
             "blue":(36,114,200),"magenta":(188,63,188),"cyan":(17,168,205),"white":(229,229,229)}
    return NAMED.get(color)

FONT_PATH = "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc"
# 关键：必须用 TTC 中的等宽 face（index=7 = Noto Sans Mono CJK SC），
# 而非默认 face。默认 face 是比例字体：实测 'm' 宽 14.8px > 单元格 11px，
# 'i' 仅 4px，逐格定位时宽字溢出、窄字留白 → 字间忽宽忽窄、行间上下重叠。
# 等宽 face 下 ASCII 恒为 size/2=11px、CJK 恒为 size=22px(=2格)，与 pyte 网格严格对齐。
FONT_INDEX = 7
FONT_SIZE = 2 * CW  # 22
font = ImageFont.truetype(FONT_PATH, FONT_SIZE, index=FONT_INDEX)
# Emoji 字体在不同系统位于 truetype/ 或 opentype/，fallback 查找避免写死路径。
import glob as _glob
_emoji_candidates = [
    "/usr/share/fonts/truetype/noto/NotoColorEmoji.ttf",
    "/usr/share/fonts/opentype/noto/NotoColorEmoji.ttf",
] + _glob.glob("/usr/share/fonts/**/NotoColorEmoji.ttf", recursive=True)
emoji_font = None
for _p in _emoji_candidates:
    if os.path.exists(_p):
        try:
            emoji_font = ImageFont.truetype(_p, 109)
            break
        except OSError:
            pass

def is_emoji(ch):
    if not ch:
        return False
    o = ord(ch[0])
    return o >= 0x1F000 or (0x2600 <= o <= 0x27BF) or o in (0x25B8, 0x25BE, 0x2B50)

emoji_cache = {}
def emoji_tile(ch, max_w, max_h):
    """把 109px 彩色 emoji 缩到 max_w×max_h 以内（避免遮挡相邻字符）。"""
    if ch in emoji_cache:
        return emoji_cache[ch]
    tile = Image.new("RGBA", (150, 150), (0, 0, 0, 0))
    d = ImageDraw.Draw(tile)
    d.text((5, 5), ch, font=emoji_font, embedded_color=True)
    bbox = tile.getbbox()
    if bbox:
        tile = tile.crop(bbox)
    tile.thumbnail((max_w, max_h), Image.LANCZOS)
    emoji_cache[ch] = tile
    return tile

def render(sc):
    # 底色：采样出现最多的非 default 背景（适配明暗主题切换）
    from collections import Counter
    cnt = Counter()
    for y in range(ROWS):
        for x in range(COLS):
            if sc.buffer[y][x].bg != "default":
                cnt[sc.buffer[y][x].bg] += 1
    base = rgb(cnt.most_common(1)[0][0]) if cnt else (28, 28, 30)
    W, H = COLS * CW, ROWS * CH
    img = Image.new("RGB", (W, H), base)
    d = ImageDraw.Draw(img)
    # 第一遍：全部背景矩形。若逐格"画背景→画字"交替进行，后一格的背景
    # 矩形会擦掉前一字符溢出到本格的抗锯齿边缘（笔画缺失/截断的根源）。
    for y in range(ROWS):
        last_bg = base
        for x in range(COLS):
            ch = sc.buffer[y][x]
            bg = rgb(ch.bg)
            if bg is None:
                bg = last_bg
            else:
                last_bg = bg
            d.rectangle([x*CW, y*CH, (x+1)*CW - 1, (y+1)*CH - 1], fill=bg)
    # 第二遍：全部文字。emoji 统一缩进 2 格宽内居中，不横向溢出遮挡邻字。
    for y in range(ROWS):
        for x in range(COLS):
            ch = sc.buffer[y][x]
            data = ch.data
            if not data or data == " ":
                continue
            if is_emoji(data) and emoji_font:
                tile = emoji_tile(data, max_w=2*CW - 2, max_h=CH - 10)
                # 宽 emoji 占 2 格：在 2 格区域内居中贴放
                ox = x*CW + (2*CW - tile.width)//2
                img.paste(tile, (ox, y*CH + (CH - tile.height)//2), tile)
                continue
            fg = rgb(ch.fg) or (230, 230, 232)
            # 用 lm（left-middle）锚点：水平仍是单元格左边缘（保持 pyte
            # 单元格模型，CJK 占 2 格的定位不变），垂直按字形视觉中线对齐
            # 到单元格中心——避免默认 la 锚点下中英字形 ascent 差异导致的
            # "上下错乱"（中文比英文高、行间基线参差）。
            d.text((x*CW, y*CH + CH/2), data, fill=fg, font=font, anchor="lm")
    return img

# ---- 录制脚本 ----
pump(2.0)
snap("启动：macOS 风格 gox-tui")

send("\x10", 0.8)            # Ctrl+P 快速打开
send("m", 0.5); snap("快速打开浮层：输入 m")
send("\r", 1.3); snap("打开 main.gx（CRLF + tab + 中文）")

send("\x1b[B", 0.4); send("\x1b[B", 0.4); send("\x1b[B", 0.4); send("\x1b[B", 0.4)
snap("光标下移：中文注释 + tab 缩进正常")

send("\x10", 0.8); send("d", 0.5); send("\r", 1.3)
snap("多标签：打开 data.gx")

send("\x14", 1.4)            # Ctrl+T 切主题
snap("Ctrl+T：切换到亮色主题")

send("\x06", 0.8)            # Ctrl+F 搜索（data.gx 里有 let，有匹配）
send("let", 0.6); snap("Ctrl+F：搜索 let")

send("\x14", 1.4)            # 切回暗色
snap("再按 Ctrl+T：切回暗色主题")

# ---- 输出 GIF ----
durations = [1500] + [1200] * (len(frames) - 1)
frames[0][0].save(ARGS.gif, save_all=True,
                  append_images=[f[0] for f in frames[1:]],
                  duration=durations, loop=0)

# ---- 长图：帧间加分隔条与标注 ----
label_font = ImageFont.truetype(FONT_PATH, 20)
pad, bar = 12, 34
total_h = sum(f[0].height + bar + pad for f in frames) + pad
W = frames[0][0].width
long = Image.new("RGB", (W, total_h), (18, 18, 20))
d = ImageDraw.Draw(long)
y = pad
for img, label in frames:
    d.text((pad, y), label, fill=(180, 220, 255), font=label_font)
    y += bar
    long.paste(img, (0, y))
    d.line([(0, y + img.height), (W, y + img.height)], fill=(60, 60, 64), width=2)
    y += img.height + pad
long.save(ARGS.png)

print(f"frames={len(frames)} gif={ARGS.gif} png={ARGS.png} size={long.size}")
try:
    os.kill(proc_pid, 9)
except Exception:
    pass
