#!/usr/bin/env python3
"""图标渲染像素校验（GUI E2E 助手）。

输入：状态快照 JSON（含 testid_boxes）+ 当前屏幕截图 PNG。
对每个带 testid 的图标按钮，在命中框内采样像素，硬断言：
  1. 图标确实被绘制——区域相对该图标按钮底色的「异色像素」达到下限；
  2. 图标不是糊成一片——异色像素占比不过高（否则是色块/乱码填充）；
  3. 主题切换后图标仍可见——由 E2E 分别在深/浅主题各跑一次本脚本。

用法：icon_check.py <state.json> <screenshot.png> [--min N]
退出码 0 = 全部图标通过，1 = 有图标异常。
"""
import json
import sys
from collections import Counter

from PIL import Image


def is_icon(testid):
    """只校验真正的图标按钮；文本标签/装饰条不做图标像素断言。

    - btn-*            ：工具栏图标按钮
    - stripe-*         ：竖栏图标（stripe-indicator 是 3px 装饰指示条，排除）
    chip-*（文本标签）、st-*（状态栏文本）、tabs-split（分隔条）、
    tb-bottom-close（文本关闭标签）均非图标，不在本校验范围。
    """
    if testid.startswith("btn-"):
        return True
    if testid.startswith("stripe-") and testid != "stripe-indicator":
        return True
    return False


def sample_icon(im, box):
    """返回 (异色像素数, 采样总数, 区域主色)。"""
    x, y, w, h = (int(box[k]) for k in ("x", "y", "w", "h"))
    # 图标主体居中，取内缩后的中心区，避开 hover 边框/圆角。
    pad_x, pad_y = max(2, w // 5), max(2, h // 5)
    # 小尺寸框内缩后可能翻转（right<left）——健壮回退，不抛异常中断整套 E2E。
    left, top = x + pad_x, y + pad_y
    right, bottom = x + w - pad_x, y + h - pad_y
    if right <= left or bottom <= top:
        return 0, 0, (0, 0, 0)
    crop = im.crop((left, top, right, bottom)).convert("RGB")
    px = list(crop.get_flattened_data()) if hasattr(crop, "get_flattened_data") else list(crop.getdata())
    if not px:
        return 0, 0, (0, 0, 0)
    # 底色 = 区域内出现最多的颜色（图标占少数，主色即按钮底色）。
    base = Counter(px).most_common(1)[0][0]
    diff = sum(1 for p in px if max(abs(p[i] - base[i]) for i in range(3)) > 20)
    return diff, len(px), base


def main():
    if len(sys.argv) < 3:
        print("用法: icon_check.py <state.json> <screenshot.png> [--min N]")
        return 2
    state_path, shot_path = sys.argv[1], sys.argv[2]
    min_diff = 8
    if "--min" in sys.argv:
        min_diff = int(sys.argv[sys.argv.index("--min") + 1])

    with open(state_path) as f:
        boxes = json.load(f).get("testid_boxes", [])
    if not boxes:
        print("[FAIL] 快照里没有 testid_boxes，图标观测钩子失效")
        return 1

    im = Image.open(shot_path)
    failed = 0
    icons = [b for b in boxes if is_icon(b["testid"])]
    if not icons:
        print("[FAIL] 快照里没有图标按钮 testid，图标观测钩子失效")
        return 1
    for b in sorted(icons, key=lambda v: v["testid"]):
        diff, total, base = sample_icon(im, b)
        ratio = diff / total if total else 0
        # 异色像素至少 min_diff 个才认为图标画出来了；占比超过 0.85 视为色块填充。
        ok = diff >= min_diff and ratio <= 0.85
        tag = "PASS" if ok else "FAIL"
        print(f"  [{tag}] {b['testid']:<18} 异色={diff}/{total} ({ratio:.0%}) base={base}")
        if not ok:
            failed += 1

    print(f"-- 图标像素校验: {len(icons) - failed}/{len(icons)} 通过")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
