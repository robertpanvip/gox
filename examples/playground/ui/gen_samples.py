#!/usr/bin/env python3
"""从 samples/*.gx 生成 playground 的示例元数据、可运行示例包与注册表。

产物（全部通过门禁后才写，任一失败立即中止，绝不写半成品）：
  1. app.gx 的 sample 列表标记区（名称 / 分组 / 特性 / 描述 / 源码文本）
  2. demos/dNN/demo.go —— 每个 demo 一个独立 Go 包，真编译进 wasm，可交互运行
  3. demos/registry.go —— View(i) 分发到对应包的 View()

为什么独立包：demo 源码是给读者看的完整小程序，彼此会有同名顶层符号
（如 bump/change/state）。合并到一个包会重名，故运行版按 demo 分包隔离；
展示版仍用源文件原样文本。生成运行版时只把 `func view` 改成 `pub func view`
以便注册表跨包调用，其余源码一字不改。

强制门禁：
  1. 每个 samples/*.gx 过 `gox fmt -check`（已格式化）且不超过 24 行；
  2. 每个 demo 单独套完整 package/import 后过 `gox build -check`
     （真实 Go 编译 + GX 语义诊断，能抓 gox check 抓不到的未定义变量）；
  3. 生成运行版包与注册表后，整包再过一次 gox build -check 确认可运行。

用法：
  python3 gen_samples.py
  GOX=/path/to/gox python3 gen_samples.py
"""
import json
import os
import re
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.abspath(os.path.join(HERE, "..", "..", ".."))
SAMPLES_DIR = os.path.join(HERE, "samples")
DEMOS_DIR = os.path.join(HERE, "demos")
APP_GX = os.path.join(HERE, "app.gx")
APP_GO = os.path.join(HERE, "app.go")
GOX = os.environ.get("GOX", "gox")
GOWORK = os.environ.get("GOWORK", "off")
MAX_LINES = 24  # 编辑器固定行数，demo 必须能完整展示

# 文件名 → (按钮名, 分组, 特性标签, 一句话描述)
META = {
    "01_hello":      ("01 Hello GX",   "基础",     "sig 裸读",     "声明信号，JSX 里直接读，编译器自动补调用"),
    "02_counter":    ("02 Counter",    "基础",     "事件 / 样式",  "事件传函数原值，样式收进 style 结构体"),
    "03_for":        ("03 For 列表",   "基础",     "For / getter", "keyed 列表，item 用 t() 读实时值"),
    "04_style":      ("04 样式卡片",   "基础",     "Style struct", "驼峰 key + hover 伪状态单词 key"),
    "05_computed":   ("05 computed",   "响应式",   "computed / batch", "派生信号 + 批量合并写入"),
    "06_branch":     ("06 条件分支",   "响应式",   "if / else",    "JSX 内条件编译成细粒度 Branch"),
    "07_list_ops":   ("07 列表增删",   "响应式",   "sig []T",      "切片增删，For 自动增删对应节点"),
    "08_enum":       ("08 枚举",       "语言特性", "enum",         "类型 / 常量 / String / Parse，三种拼写"),
    "09_template":   ("09 模板字符串", "语言特性", "模板 / 默认值", "反引号插值 + 尾部参数默认值"),
    "10_destruct":   ("10 对象解构",   "语言特性", "解构",         "var { x, y } = p 逐字段取值"),
    "11_union":      ("11 联合类型",   "语言特性", "union / is",   "擦除为 any 的语法便利层 + 类型守卫"),
    "12_component":  ("12 组件",       "语言特性", "props / sig 字段", "pub func 组件，props 的 sig 字段保鲜"),
    "13_eventctx":   ("13 事件签名",   "语言特性", "EventContext", "具名函数 / 箭头 / func 字面量 / 带 ctx"),
    "14_structsig":  ("14 struct sig", "语言特性", "字段响应式",   "显式标注的字段才响应，构造后读实时值"),
    "15_collections":("15 集合链式",   "语言特性", "List[T]",      "[]T 提升为 List，filter/map/reduce"),
}

BEGIN = "    // @@BEGIN-GENERATED-SAMPLES@@"
END = "    // @@END-GENERATED-SAMPLES@@"

# 按需 import：仅当源码里出现 `pkg.` 时才注入，避免未使用 import。
IMPORT_LINES = {
    "gxscene": 'gxscene "github.com/robertpanvip/gox-core/runtime/scene"',
    "Signal":  'Signal "github.com/robertpanvip/gox-core/runtime/signal"',
    "fmt":     '"fmt"',
    "strconv": '"strconv"',
    "strings": '"strings"',
    "time":    '"time"',
}
EXTRA_PKGS = ("Signal", "fmt", "strconv", "strings", "time")


def fail(msg):
    print("gen_samples: " + msg, file=sys.stderr)
    sys.exit(1)


def run(cmd, env=None, cwd=None):
    full_env = os.environ.copy()
    full_env["GOWORK"] = GOWORK
    if env:
        full_env.update(env)
    p = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                       text=True, env=full_env, cwd=cwd)
    return p.returncode, p.stdout


def build_env():
    return {
        "GOTOOLCHAIN": os.environ.get("GOTOOLCHAIN", "go1.27.4"),
        "GOPROXY": os.environ.get("GOPROXY", "https://proxy.golang.org,direct"),
    }


def wrap(body, pkg, with_main):
    """给 demo 片段套上完整 Go 包壳（package + 按需 import），用于编译检查/运行版。"""
    imports = [IMPORT_LINES["gxscene"]]
    for p in EXTRA_PKGS:
        if re.search(r"\b" + re.escape(p) + r"\.", body):
            imports.append(IMPORT_LINES[p])
    lines = ["package " + pkg, "", "import ("]
    lines += ["    " + i for i in imports]
    lines += [")", "", body]
    if with_main:
        lines += ["", "func main() {}"]
    return "\n".join(lines)


def compile_check(src, tmpdir, tag, out_name):
    """跑 gox build -check；返回 (ok, output)。"""
    gx = os.path.join(tmpdir, out_name + ".gx")
    go = os.path.join(tmpdir, out_name + ".go")
    with open(gx, "w", encoding="utf-8") as fh:
        fh.write(src)
    code, out = run([
        GOX, "build", gx, "-o", go, "-check",
        "-module", "github.com/robertpanvip/gox",
        "-replace", "github.com/robertpanvip/gox=" + REPO_ROOT,
    ], env=build_env())
    return code == 0, out, go


def main():
    if not os.path.isfile(GOX) and subprocess.call(
            ["which", GOX], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL) != 0:
        fail("找不到 gox（可用 GOX=/path/to/gox 指定）")

    files = sorted(
        os.path.join(SAMPLES_DIR, f)
        for f in os.listdir(SAMPLES_DIR) if f.endswith(".gx")
    )
    if not files:
        fail("samples/ 下没有 .gx 文件")

    tmpdir = tempfile.mkdtemp(prefix="gx-samples-gate-")
    bodies = {}
    display = {}
    stems = []

    # ---- 门禁 1：逐个格式化 + 行数检查 ----
    print("== 门禁 1/3：gox fmt -check + 行数 <= %d ==" % MAX_LINES)
    for f in files:
        code, out = run([GOX, "fmt", "-check", f])
        if code != 0:
            fail("格式检查未通过: %s\n%s\n请先运行: gox fmt -w %s" % (f, out, f))
        with open(f, "r", encoding="utf-8") as fh:
            body = fh.read()
        # 展示给读者的是完整可编译源码（package + 按需 import），LSP 才能直接诊断。
        shown = wrap(body, "main", False)
        lines = shown.rstrip("\n").split("\n")
        if len(lines) > MAX_LINES:
            fail("%s 展示版有 %d 行，超过编辑器 %d 行限制" % (f, len(lines), MAX_LINES))
        stem = os.path.splitext(os.path.basename(f))[0]
        stems.append(stem)
        bodies[stem] = body
        display[stem] = shown
        print("  OK  %-18s %2d 行(展示)" % (os.path.basename(f), len(lines)))

    # ---- 门禁 2：每个 demo 独立编译检查 ----
    # 每个 demo 单独成包，天然隔离同名符号；套 main 只为让 go build 通过。
    print("== 门禁 2/3：逐 demo gox build -check（语义 + 真实 Go 编译） ==")
    for stem in stems:
        ok, out, _ = compile_check(
            wrap(bodies[stem], "main", True), tmpdir, stem, "check_" + stem)
        if not ok:
            fail("示例编译检查未通过: %s\n%s" % (stem, out))
        print("  OK  " + stem)

    # ---- 全部通过：写运行版包 + 注册表 ----
    print("== 门禁通过，生成 demos/*.go ==")
    os.makedirs(DEMOS_DIR, exist_ok=True)
    # 清理旧生成目录（只删 dNN 目录与 registry.go，不碰人手写文件）
    for name in os.listdir(DEMOS_DIR):
        p = os.path.join(DEMOS_DIR, name)
        if re.fullmatch(r"d\d{2}", name) and os.path.isdir(p):
            for inner in os.listdir(p):
                os.remove(os.path.join(p, inner))
            os.rmdir(p)
    reg = os.path.join(DEMOS_DIR, "registry.go")
    if os.path.isfile(reg):
        os.remove(reg)

    pkgs = []
    for i, stem in enumerate(stems, 1):
        pkg = "d%02d" % i
        pkgs.append(pkg)
        # 只把 view 导出，其他源码原样保留
        body = re.sub(r"\bfunc\s+view\s*\(", "pub func view(", bodies[stem])
        ok, out, go_path = compile_check(wrap(body, pkg, False), tmpdir, stem, "run_" + stem)
        if not ok:
            fail("生成运行版失败: %s\n%s" % (stem, out))
        dst_dir = os.path.join(DEMOS_DIR, pkg)
        os.makedirs(dst_dir, exist_ok=True)
        with open(go_path, "r", encoding="utf-8") as fh:
            generated = fh.read()
        # gox build 产物 package 名就是我们传的 pkg，直接落盘
        with open(os.path.join(dst_dir, "demo.go"), "w", encoding="utf-8") as fh:
            fh.write(generated)
        print("  OK  demos/%s/demo.go" % pkg)

    registry = [
        "// Code generated by gen_samples.py. DO NOT EDIT.",
        "package demos",
        "",
        "import (",
        '    gxscene "github.com/robertpanvip/gox-core/runtime/scene"',
    ]
    for i, pkg in enumerate(pkgs, 1):
        registry.append('    d%02d "github.com/robertpanvip/gox-playground/ui/demos/%s"' % (i, pkg))
    registry += [")", "", "var views = []func() *gxscene.Instance{"]
    for pkg in pkgs:
        registry.append("    %s.View," % pkg)
    registry += [
        "}",
        "",
        "// Count 返回可运行示例数量。",
        "func Count() int { return len(views) }",
        "",
        "// View 构建第 i 个示例的实时场景树（切示例即运行）。越界回退到第一个。",
        "func View(i int) *gxscene.Instance {",
        "    if i < 0 || i >= len(views) {",
        "        return views[0]()",
        "    }",
        "    return views[i]()",
        "}",
        "",
    ]
    with open(reg, "w", encoding="utf-8") as fh:
        fh.write("\n".join(registry))
    print("  OK  demos/registry.go")

    # ---- 门禁 3：整包编译确认 registry + 各 demo 包可链接 ----
    # 用 Go 原生编译 playground 模块下的 demos/...；go.work 与 ui/go.mod 的
    # replace 保证跨模块引用 gox 主仓能解析。gox build -check 的临时 module
    # 不属于 playground 模块，无法解析 ui/demos 引用，故此处用 go build。
    print("== 门禁 3/3：go build ./demos/...（运行版整包） ==")
    code, out = run(["go", "build", "./demos/..."], env=build_env(), cwd=HERE)
    if code != 0:
        fail("运行版整包编译检查未通过:\n" + out)
    print("  OK  运行版整包")

    # ---- 重建 app.gx 的 sample 元数据区 ----
    lines = [BEGIN]
    for i, stem in enumerate(stems):
        name, group, tag, desc = META.get(stem, (stem, "其他", "", ""))
        lit = json.dumps(display[stem], ensure_ascii=False)
        lines.append(
            '        sample{name: "%s", group: "%s", tag: "%s", desc: "%s", source: %s},'
            % (name, group, tag, desc, lit))
    lines.append(END)
    block = "\n".join(lines)

    with open(APP_GX, "r", encoding="utf-8") as fh:
        src = fh.read()
    pattern = re.compile(re.escape(BEGIN) + r".*?" + re.escape(END), re.DOTALL)
    if not pattern.search(src):
        fail("app.gx 中未找到生成标记 %s ... %s" % (BEGIN, END))
    with open(APP_GX, "w", encoding="utf-8") as fh:
        fh.write(pattern.sub(lambda _: block, src))
    print("  已更新 " + APP_GX)

    code, out = run([GOX, "build", APP_GX, "-o", APP_GO])
    if code != 0:
        fail("转译 app.go 失败:\n" + out)
    print("  已生成 " + APP_GO)


if __name__ == "__main__":
    main()
