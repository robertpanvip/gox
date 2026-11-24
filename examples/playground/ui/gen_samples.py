#!/usr/bin/env python3
"""从 samples/*.gx 生成 app.gx 的示例列表，并转译为 app.go。

强制门禁（任一步失败都中止，绝不写入 app.gx / app.go）：
  1. 每个 samples/*.gx 先过 `gox fmt -check`（已格式化）；
  2. 全部拼成一个临时 GX 程序后过 `gox build -check`（真实 Go 编译 +
     GX 语义诊断，能抓未定义变量等 gox check 抓不到的错误）。
全部通过后才用源文件原样重建 app.gx 的 sample 区域，再转译 app.go。

demo 片段本身没有 package/import；门禁检查时按需套头：
出现 `Signal.` 才引入 signal 包，避免"未使用 import"误报。

用法：
  python3 gen_samples.py
  GOX=/path/to/gox python3 gen_samples.py     # 指定 gox 二进制
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
APP_GX = os.path.join(HERE, "app.gx")
APP_GO = os.path.join(HERE, "app.go")
GOX = os.environ.get("GOX", "gox")
GOWORK = os.environ.get("GOWORK", "off")

# 显示名：文件名（去掉 .gx）→ playground 示例按钮上的名字
NAMES = {
    "01_hello": "01 Hello GX",
    "02_counter": "02 Counter",
    "03_for": "03 For 列表",
    "04_style": "04 样式卡片",
    "05_computed": "05 响应式 computed",
}

BEGIN = "    // @@BEGIN-GENERATED-SAMPLES@@"
END = "    // @@END-GENERATED-SAMPLES@@"


def fail(msg):
    print("gen_samples: " + msg, file=sys.stderr)
    sys.exit(1)


def run(cmd, env=None):
    full_env = os.environ.copy()
    full_env["GOWORK"] = GOWORK
    if env:
        full_env.update(env)
    p = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                       text=True, env=full_env)
    return p.returncode, p.stdout


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

    # ---- 门禁 1：逐个格式化检查 ----
    print("== 门禁 1/2：gox fmt -check（格式） ==")
    for f in files:
        code, out = run([GOX, "fmt", "-check", f])
        if code != 0:
            fail("格式检查未通过: %s\n%s\n请先运行: gox fmt -w %s" % (f, out, f))
        print("  OK  " + os.path.basename(f))

    # ---- 门禁 2：把所有 demo 合成一个程序，真实编译检查 ----
    # 每个 demo 的 view 改名，避免同包重名；按需 import Signal。
    print("== 门禁 2/2：gox build -check（语义 + 真实 Go 编译） ==")
    bodies = {}
    for f in files:
        stem = os.path.splitext(os.path.basename(f))[0]
        with open(f, "r", encoding="utf-8") as fh:
            bodies[stem] = fh.read()

    need_signal = any("Signal." in b for b in bodies.values())
    merged = ["package main", "", "import ("]
    merged.append('    gxscene "github.com/robertpanvip/gox/runtime/scene"')
    if need_signal:
        merged.append('    Signal "github.com/robertpanvip/gox/runtime/signal"')
    merged.append(")")
    merged.append("")
    for stem, body in bodies.items():
        merged.append("// ---- " + stem + " ----")
        # view → view_<stem>，sig/func 保持不变；内部调用同步改名
        merged.append(re.sub(r"\bview\b", "view_" + stem, body))
        merged.append("")
    merged.append("func main() {}")
    merged_src = "\n".join(merged)

    tmpdir = tempfile.mkdtemp(prefix="gx-samples-gate-")
    merged_gx = os.path.join(tmpdir, "all_samples.gx")
    merged_go = os.path.join(tmpdir, "all_samples.go")
    with open(merged_gx, "w", encoding="utf-8") as fh:
        fh.write(merged_src)

    env = {
        "GOTOOLCHAIN": os.environ.get("GOTOOLCHAIN", "go1.27.4"),
        "GOPROXY": os.environ.get("GOPROXY", "https://proxy.golang.org,direct"),
    }
    code, out = run([
        GOX, "build", merged_gx, "-o", merged_go, "-check",
        "-module", "github.com/robertpanvip/gox",
        "-replace", "github.com/robertpanvip/gox=" + REPO_ROOT,
    ], env=env)
    if code != 0:
        fail("编译检查未通过（示例源码有错误）:\n" + out)
    print("  OK  全部示例（合并编译通过）")

    # ---- 全部通过：用源文件原样重建 app.gx 的 sample 区域 ----
    print("== 校验通过，生成 app.gx / app.go ==")
    lines = [BEGIN]
    for f in files:
        stem = os.path.splitext(os.path.basename(f))[0]
        name = NAMES.get(stem, stem)
        with open(f, "r", encoding="utf-8") as fh:
            body = fh.read()
        # Go 字符串字面量：JSON 转义兼容 \n \" \\ 等
        lit = json.dumps(body, ensure_ascii=False)
        lines.append('        sample{name: "%s", source: %s},' % (name, lit))
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
