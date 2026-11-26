#!/usr/bin/env bash
# 三端实现一致性测试（Web/DOM、Skia/Yoga、Rust/Taffy）
#
# 三端共享同一份用例（testdata/threeway/cases.json），各自以无头方式渲染/消费
# 并按用例断言；本脚本顺序跑三端、汇总成「用例 × 后端」矩阵报告。
# 任何一端某用例失败，都意味着该端该样式属性未实现、Taffy/Yoga 映射传值错误
# 或三端语义不一致。
#
# 用法：bash scripts/threeway-parity.sh [--quiet]
# 退出码：三端全绿 0；任何失败 1。

set -u
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${GX_THREEWAY_DIR:-/tmp/threeway}"
mkdir -p "$OUT_DIR"
QUIET=0
[ "${1:-}" = "--quiet" ] && QUIET=1

SKIA_RC=1
WEB_RC=1
RUST_RC=1

# 1) Skia：无头 Window 渲染 → DrawLog/命中框 观测面
if (cd "$ROOT/backend/skia" && GX_THREEWAY_OUT="$OUT_DIR/out-skia.json" \
    go test -count=1 -run TestThreeWayMatrix . ) ; then SKIA_RC=0; fi

# 2) Web：memDriver 镜像消费 → 样式透传 + css 转换断言
if (cd "$ROOT/backend/web" && GX_THREEWAY_OUT="$OUT_DIR/out-web.json" \
    go test -count=1 -run 'TestThreeWayMatrixWeb|TestFlexDefaultColumn' . ) ; then WEB_RC=0; fi

# 3) Rust：GRX 同源消费 → Taffy 布局 + 绘制指令 + 命中框 观测面
RUSTCargo="$(ls -d "$HOME"/.rustup/toolchains/stable-x86_64-unknown-linux-gnu/bin 2>/dev/null || true)"
if [ -n "$RUSTCargo" ]; then export PATH="$RUSTCargo:$PATH"; fi
if GX_THREEWAY_OUT="$OUT_DIR/out-rust.json" \
   cargo test --manifest-path "$ROOT/backend/rust/rust/Cargo.toml" threeway_matrix ; then RUST_RC=0; fi

# 4) 汇总矩阵报告
python3 - "$OUT_DIR" "$QUIET" <<'PYEOF'
import json, os, sys

out_dir, quiet = sys.argv[1], sys.argv[2] == "1"

# Rust 端失败时 cargo 不一定写出 JSON；缺文件则按整体 RC 标注。
files = {
    "skia": os.path.join(out_dir, "out-skia.json"),
    "web":  os.path.join(out_dir, "out-web.json"),
    "rust": os.path.join(out_dir, "out-rust.json"),
}
results = {}   # backend -> {case: (pass, fails)}
for be, path in files.items():
    m = {}
    if os.path.exists(path):
        try:
            d = json.load(open(path))
            for c in d.get("cases", []):
                m[c["name"]] = (bool(c.get("pass")), c.get("fails", []))
        except Exception as e:
            print(f"[warn] 解析 {path}: {e}")
    results[be] = m

order = [c["name"] for c in json.load(open(os.path.join(out_dir, "..", "..", "..", "workspace", "gox", "testdata", "threeway", "cases.json")))["cases"]] \
    if False else None

# 用例名直接从三份报告并集推导，保持与用例文件解耦。
names = []
for be in ("skia", "web", "rust"):
    for n in results[be]:
        if n not in names:
            names.append(n)

fails_total = 0
lines = []
for n in names:
    cells = []
    row_fail = False
    for be in ("skia", "web", "rust"):
        if n in results[be]:
            ok, fails = results[be][n]
            if ok:
                cells.append("✅")
            else:
                row_fail = True
                fails_total += len(fails)
                cells.append("❌ " + "; ".join(fails[:2]))
        else:
            cells.append("➖")
    lines.append(f"  {n:<26} {cells[0]:<4} {cells[1]:<4} {cells[2]}")

print()
print("=== 三端一致性矩阵（testdata/threeway/cases.json）===")
print(f"  {'用例':<26} Skia  Web   Rust")
for l in lines:
    print(l)
print()
if fails_total:
    print(f"❌ 共 {fails_total} 项断言失败")
    sys.exit(1)
print("✅ 三端全部一致")
PYEOF

# Rust 失败时可能无 out-rust.json；用 RC 兜底判定。
if [ "$SKIA_RC" -ne 0 ] || [ "$WEB_RC" -ne 0 ] || [ "$RUST_RC" -ne 0 ]; then
  echo "❌ 测试进程退出码：skia=$SKIA_RC web=$WEB_RC rust=$RUST_RC"
  exit 1
fi
