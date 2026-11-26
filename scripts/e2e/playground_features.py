#!/usr/bin/env python3
"""Playground 按功能划分的 E2E 测试套件（Playwright + Chromium）。

每个用例对应一个用户可感知的功能点，独立判定、失败互不影响。
退出码 0 = 全部通过；非 0 = 有失败，CI 直接判红。

用法：
    python3 scripts/e2e/playground_features.py [--url http://127.0.0.1:8081/index.html]

前置：目标地址已可访问（服务器模式见 scripts/e2e/run_playground_e2e.sh，
它会负责构建 wasm 并起静态服务器）。依赖：pip install playwright，
playwright install chromium。
"""
from __future__ import annotations

import argparse
import asyncio
import sys
import time

from playwright.async_api import Page, async_playwright

LONG_DOC = "\n".join(f"// L{i} 示例行内容" for i in range(80))


# ---------------------------------------------------------------- 页面就绪

async def wait_ready(page: Page, url: str) -> list[str]:
    """打开页面并等编辑器行节点出现；返回收集到的页面错误。"""
    errs: list[str] = []
    page.on("pageerror", lambda e: errs.append(str(e)))
    await page.goto(url, wait_until="networkidle")
    await page.wait_for_selector(".gx-editor", timeout=30_000)
    for _ in range(60):
        if await page.evaluate("!!document.querySelector('.gx-editor [data-line]')"):
            break
        await page.wait_for_timeout(500)
    await page.wait_for_timeout(800)
    return errs


async def first_visible_lines(page: Page, n: int = 3) -> list[str]:
    return await page.evaluate(
        f"""() => [...document.querySelectorAll('.gx-editor [data-line]')]
              .slice(0,{n}).map(l => l.getAttribute('data-line'))"""
    )


# ---------------------------------------------------------------- 用例

async def case_boot(page: Page) -> tuple[bool, str]:
    """功能：页面启动。wasm 加载、场景树挂载、编辑器行渲染。"""
    rows = await page.evaluate(
        "() => document.querySelectorAll('.gx-editor [data-line]').length"
    )
    if rows <= 0:
        return False, f"编辑器没有渲染出行节点（rows={rows}）"
    return True, f"编辑器渲染 {rows} 行"


async def case_no_page_errors(page: Page, errs: list[str]) -> tuple[bool, str]:
    """功能：运行期无未捕获 JS 异常。"""
    if errs:
        return False, f"pageerror x{len(errs)}: {errs[0][:120]}"
    return True, "无 pageerror"


async def case_sample_switch(page: Page) -> tuple[bool, str]:
    """功能：示例切换复用。01→04→01 来回点，编辑器内容无残留。"""
    snap = lambda: page.evaluate(
        """() => [...document.querySelectorAll('.gx-editor [data-line]')]
            .map(l => (l.innerText||'').replace(/\\s+/g,' ').trim()).join('\\n')"""
    )
    find = """() => [...document.querySelectorAll('div,span,button')].filter(e => {
          const t = (e.textContent || '').trim();
          return /^0[1-9]\\s/.test(t) && t.length < 40 && e.children.length <= 1;
        }).slice(0, 15)"""

    async def click_sample(idx: int) -> None:
        # 每次重新查询并滚动到视口内再取坐标：列表自身可滚，坐标会过期。
        await page.evaluate(
            f"""(i) => {{ const el = ({find})()[i]; if (el) el.scrollIntoView({{block: 'nearest'}}); }}""",
            idx,
        )
        await page.wait_for_timeout(150)
        pos = await page.evaluate(
            f"""(i) => {{ const el = ({find})()[i];
                  if (!el) return null;
                  const b = el.getBoundingClientRect();
                  return {{x: b.left + b.width/2, y: b.top + b.height/2}}; }}""",
            idx,
        )
        if not pos:
            raise AssertionError(f"示例项 {idx} 不存在")
        await page.mouse.click(pos["x"], pos["y"])
        await page.wait_for_timeout(700)

    n_items = await page.evaluate(f"{find}.length")
    if n_items < 2:
        return False, f"示例列表项识别不足（found={n_items}）"
    s0 = await snap()
    await click_sample(0)
    await click_sample(min(4, n_items - 1))
    s1 = await snap()
    await click_sample(0)
    s2 = await snap()
    if not s1 or s1 == s0:
        return False, f"切换后内容未变化（s0 前 60 字符={s0[:60]!r}）"
    if s2 != s0:
        return False, f"切回 01 后内容有残留（s2 前 60 字符={s2[:60]!r}）"
    return True, "01→05→01 内容正确往返、无残留"


async def case_editor_wheel_scroll(page: Page) -> tuple[bool, str]:
    """功能：编辑器滚轮虚拟滚动。长文档 + 真实滚轮事件，窗口行号应移动。"""
    await page.evaluate("(s) => window.Gox.setSource(s)", LONG_DOC)
    await page.wait_for_timeout(1000)
    rows = await page.evaluate(
        "() => document.querySelectorAll('.gx-editor [data-line]').length"
    )
    if rows < 20:
        return False, f"长文档写入后窗口未扩展（rows={rows}，期望 ≥20）"
    ed = page.locator(".gx-editor").first
    await ed.scroll_into_view_if_needed()
    box = await ed.bounding_box()
    await page.mouse.move(box["x"] + box["width"] / 2, box["y"] + 100)
    before = await first_visible_lines(page)
    for _ in range(5):
        await page.mouse.wheel(0, 220)
        await page.wait_for_timeout(120)
    await page.wait_for_timeout(700)
    after = await first_visible_lines(page)
    if before == after:
        return False, f"滚轮后窗口未移动 before={before} after={after}"
    return True, f"窗口行号 {before[0]} → {after[0]}"


async def case_click_after_scroll(page: Page) -> tuple[bool, str]:
    """功能：滚动后点击定位。点击中间行，caret 应落在编辑器视口内。"""
    rows = await page.evaluate(
        """() => [...document.querySelectorAll('.gx-editor [data-line]')].map(l => {
            const b = l.getBoundingClientRect();
            return {line: l.getAttribute('data-line'),
                    y: b.top + b.height/2, x: b.left + 50};
        })"""
    )
    if not rows:
        return False, "没有可点击的行"
    target = rows[len(rows) // 2]
    await page.mouse.click(target["x"], target["y"])
    await page.wait_for_timeout(600)
    caret = await page.evaluate(
        """() => { const c = document.querySelector('.gx-caret');
                   return c ? Math.round(c.getBoundingClientRect().top) : null; }"""
    )
    if caret is None:
        return False, "点击后未找到光标元素"
    ed_box = await page.locator(".gx-editor").first.bounding_box()
    top, bot = ed_box["y"], ed_box["y"] + ed_box["height"]
    if not (top - 2 <= caret <= bot + 2):
        return False, f"光标 top={caret} 超出编辑器视口 [{top:.0f}..{bot:.0f}]"
    return True, f"点第 {target['line']} 行，光标 top={caret} 在视口内"


async def case_button_pointer_cursor(page: Page) -> tuple[bool, str]:
    """功能：按钮 cursor:pointer（小手）。"""
    n = await page.evaluate(
        """() => [...document.querySelectorAll('div,button')]
              .filter(e => getComputedStyle(e).cursor === 'pointer').length"""
    )
    if n <= 0:
        return False, "没有任何元素是 pointer 光标"
    return True, f"{n} 个元素为 pointer"


async def case_scrollbar_style(page: Page) -> tuple[bool, str]:
    """功能：滚动条自定义样式（webkit-scrollbar 规则注入）。"""
    n = await page.evaluate(
        """() => { let n = 0;
            for (const sheet of document.styleSheets) {
              try { for (const r of sheet.cssRules)
                      if (r.cssText.includes('-webkit-scrollbar')) n++;
              } catch (e) {} }
            return n; }"""
    )
    if n < 5:
        return False, f"webkit-scrollbar 规则只有 {n} 条（期望 ≥5）"
    return True, f"{n} 条滚动条样式规则"


async def case_instance_area_wheel(page: Page) -> tuple[bool, str]:
    """功能：实例区域（预览面板）滚轮滚动。"""
    cands = """[...document.querySelectorAll('div')].filter(d => {
        const s = getComputedStyle(d);
        return (s.overflowY === 'auto' || s.overflowY === 'scroll')
            && d.scrollHeight > d.clientHeight + 5
            && !d.querySelector('.gx-editor') && !d.classList.contains('gx-editor');
    })"""
    before = await page.evaluate(f"(() => {{ const c = {cands}; return c.length ? c[0].scrollTop : null; }})()")
    if before is None:
        return False, "没找到可滚动的实例区域容器"
    box = await page.evaluate(
        f"""(() => {{ const c = {cands}[0]; const b = c.getBoundingClientRect();
              return {{x: b.left + b.width/2, y: b.top + 100}}; }})()"""
    )
    await page.mouse.move(box["x"], box["y"])
    for _ in range(3):
        await page.mouse.wheel(0, 200)
        await page.wait_for_timeout(100)
    await page.wait_for_timeout(400)
    after = await page.evaluate(
        f"(() => {{ const c = {cands}; return c.length ? c[0].scrollTop : -1; }})()"
    )
    if after <= before:
        return False, f"实例区域滚轮无效 scrollTop {before} → {after}"
    return True, f"scrollTop {before} → {after}"


async def case_no_tofu(page: Page) -> tuple[bool, str]:
    """功能：文本渲染无缺字形（方块字/替换符）。"""
    bad = await page.evaluate(
        """() => (document.body.innerText.match(/\\uFFFD/g) || []).length"""
    )
    if bad > 0:
        return False, f"页面出现 {bad} 个替换符（tofu）"
    return True, "无 tofu"


# ---------------------------------------------------------------- 主流程

CASES = [
    ("no_page_errors", "运行期无 JS 异常"),
    ("boot", "页面启动与编辑器渲染"),
    ("sample_switch", "示例切换无残留"),
    ("editor_wheel_scroll", "编辑器滚轮虚拟滚动"),
    ("click_after_scroll", "滚动后点击光标定位"),
    ("button_pointer_cursor", "按钮小手光标"),
    ("scrollbar_style", "滚动条自定义样式"),
    ("instance_area_wheel", "实例区域滚轮"),
    ("no_tofu", "文本渲染无 tofu"),
]


async def run(url: str) -> int:
    async with async_playwright() as p:
        b = await p.chromium.launch(args=["--no-sandbox"])
        page = await b.new_page(viewport={"width": 1400, "height": 900})
        errs = await wait_ready(page, url)

        results = []
        await case_boot(page)
        for name, desc in CASES:
            t0 = time.time()
            try:
                if name == "no_page_errors":
                    ok, detail = await case_no_page_errors(page, errs)
                elif name == "boot":
                    ok, detail = await case_boot(page)
                else:
                    ok, detail = await globals()[f"case_{name}"](page)
            except Exception as e:  # 用例自身崩溃也计失败
                ok, detail = False, f"用例异常: {type(e).__name__}: {e}"
            results.append((name, desc, ok, detail, time.time() - t0))

        await b.close()

    print()
    print(f"{'结果':<4} {'功能':<24} {'耗时':>6}  说明")
    print("-" * 78)
    failed = 0
    for name, desc, ok, detail, dt in results:
        mark = "PASS" if ok else "FAIL"
        failed += 0 if ok else 1
        print(f"{mark:<4} {desc:<24} {dt:>5.1f}s  {detail}")
    print("-" * 78)
    print(f"共 {len(results)} 项，失败 {failed} 项")
    return 1 if failed else 0


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--url", default="http://127.0.0.1:8081/index.html")
    args = ap.parse_args()
    sys.exit(asyncio.run(run(args.url)))


if __name__ == "__main__":
    main()
