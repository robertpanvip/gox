#!/usr/bin/env python3
"""把 widgets/editor.css（唯一权威来源）同步进 index.html 的标记区块。

index.html 需要在 <style> 里内联编辑器 CSS，才能保证 file:// 单文件产物
（playground.html）离线可用。为避免 editor.css 与 index.html 两份副本漂移，
本脚本在构建时把 editor.css 的内容注入到 index.html 的标记区块之间。

用法：python3 sync_editor_css.py   （由 build.sh 在打包前调用）
幂等：重复运行输出相同；标记缺失时报错退出，避免静默不同步。
"""
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent
EDITOR_CSS = ROOT / "../../../widgets/editor.css"
INDEX_HTML = ROOT / "index.html"

BEGIN = "/* @@EDITOR-CSS-BEGIN@@ */"
END = "/* @@EDITOR-CSS-END@@ */"


def indent(css: str, spaces: int = 4) -> str:
    """给每行加缩进以嵌入 <style>，保留空行。"""
    pad = " " * spaces
    return "\n".join(pad + line if line.strip() else "" for line in css.splitlines())


def main() -> None:
    css = EDITOR_CSS.resolve().read_text(encoding="utf-8").strip()
    html = INDEX_HTML.read_text(encoding="utf-8")

    pattern = re.compile(
        re.escape(BEGIN) + r".*?" + re.escape(END), re.DOTALL
    )
    if not pattern.search(html):
        print(f"错误：index.html 缺少 {BEGIN} / {END} 标记区块", file=sys.stderr)
        sys.exit(1)

    block = f"{BEGIN}\n{indent(css)}\n    {END}"
    new_html = pattern.sub(lambda _: block, html)

    if new_html == html:
        print("editor.css 已是最新，无需同步")
        return
    INDEX_HTML.write_text(new_html, encoding="utf-8")
    print(f"已从 {EDITOR_CSS.name} 同步编辑器 CSS 到 index.html")


if __name__ == "__main__":
    main()
