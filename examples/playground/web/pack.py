#!/usr/bin/env python3
"""把 wasm_exec.js + 严格 TS 编译产物 + app.wasm 内联进 index.html，
产出可用 file:// 双击打开的单文件 playground.html。

浏览器在 file:// 下禁止 fetch 本地文件，因此 wasm 以 base64 注入全局变量，
由 src/main.ts 离线实例化（服务器部署仍走 fetch 回退）。wasm 先 gzip 压缩
再 base64：单文件从 ~6.8MB 降到 ~1.9MB，加载更快；前端用原生
DecompressionStream('gzip') 解压。
"""
import base64
import gzip
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
    # TS 在 verbatimModuleSyntax 下会把纯 type import 编成尾部 `export {}`，
    # 用于标记 ES 模块。但单文件把它内联进普通 <script>（非 module），
    # `export` 是非法 token，会导致整段脚本不执行。删掉这个无副作用标记。
    main_js = re.sub(r"^\s*export\s*\{\s*\}\s*;?\s*$", "", main_js, flags=re.MULTILINE)
    main_js = js_block(main_js, "dist/main.js (strict TS 7)")

    # 先 gzip 再 base64：wasm 从 ~5MB 压到 ~1.4MB，单文件体积大幅下降，
    # 浏览器加载/传输更快；前端用原生 DecompressionStream('gzip') 还原。
    wasm_gz = gzip.compress((ROOT / "app.wasm").read_bytes(), compresslevel=9)
    wasm_b64 = base64.b64encode(wasm_gz).decode("ascii")
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
