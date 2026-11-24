#!/usr/bin/env python3
"""把 wasm_exec.js + 严格 TS 编译产物 + app.wasm 内联进 index.html，
产出可用 file:// 双击打开的单文件 playground.html。

浏览器在 file:// 下禁止 fetch 本地文件，因此 wasm 以 base64 注入全局变量，
由 src/main.ts 的 decodeBase64 分支离线实例化（服务器部署仍走 fetch 回退）。
"""
import base64
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parent


def js_block(text: str, label: str) -> str:
    """Prepare JS source for inlining; avoid closing the HTML script tag early."""
    text = text.replace("</script>", "<\\/script>")
    return f"/* ==== {label} ==== */\n{text}"


def main() -> None:
    html = (ROOT / "index.html").read_text(encoding="utf-8")

    wasm_exec = js_block((ROOT / "wasm_exec.js").read_text(encoding="utf-8"),
                         "wasm_exec.js (Go runtime)")
    main_js = (ROOT / "dist" / "main.js").read_text(encoding="utf-8")
    main_js = re.sub(r"//[#@] sourceMappingURL=.*", "", main_js)
    main_js = js_block(main_js, "dist/main.js (strict TS 7)")

    wasm_b64 = base64.b64encode((ROOT / "app.wasm").read_bytes()).decode("ascii")
    inject = ('<script>window.__GX_PLAYGROUND_WASM_B64__="' + wasm_b64 + '";</script>')

    html = html.replace("<script src=\"wasm_exec.js\"></script>",
                        "<script>\n" + wasm_exec + "\n</script>")
    html = html.replace("<script type=\"module\" src=\"dist/main.js\"></script>",
                        inject + "\n  <script>\n" + main_js + "\n</script>")

    out = ROOT / "playground.html"
    out.write_text(html, encoding="utf-8")
    print(f"generated {out} ({out.stat().st_size / 1024 / 1024:.2f} MB)")


if __name__ == "__main__":
    main()
