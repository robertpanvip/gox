#!/usr/bin/env bash
# 构建 Rust 后端静态库，供 Go 侧 cgo 链接。
#
# 关键步骤：fat LTO 会在 .a 里嵌入 .llvmbc / .llvmcmd section（LLVM bitcode）。
# Go 1.24+ 的 cgo 安全检查（cmd/go/internal/work/security.go）会拒绝带这些
# section 的静态库，报 "disallowed LTO section \".llvmbc\""。因此 cargo build
# 之后必须用 objcopy 把这两个 section 剥离——剥离只移除给 LTO 用的中间表示，
# 机器码不受影响，运行行为与优化级别不变。
#
# 用法（在任意目录）：bash backend/rust/rust/build.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$HERE"

echo "[rust-backend] cargo build --release ..."
cargo build --release

# 找到本次产出的 libgxbackend_rust-*.a（cargo 在 deps/ 下），逐个剥离 bitcode section。
# 同时剥离依赖 crate 的归档（libgx_gpu-*.a 等）：rustc 链接 staticlib 时这些
# 依赖产物也会进入 cgo 链接扫描范围，只剥主库会让 Go 报错转移（实测踩坑：
# "disallowed LTO section" 逐个文件出现——libgx_gpu → libgxbackend_rust-<hash>）。
mapfile -t LIBS < <(find target/release/deps -maxdepth 1 \( -name 'libgxbackend_rust-*.a' -o -name 'libgx_gpu-*.a' \) -type f)
if [ "${#LIBS[@]}" -eq 0 ]; then
  echo "[rust-backend] ERROR: 未找到 libgxbackend_rust-*.a" >&2
  exit 1
fi

# LDFLAGS 实际链接的是 target/release/libgxbackend_rust.a（cargo 生成的硬链接），
# 也要一并纳入——它与 deps/ 下的是同一 inode，但显式列出更稳妥。
LIBS+=(target/release/libgxbackend_rust.a)

for lib in "${LIBS[@]}"; do
  [ -f "$lib" ] || continue
  # 先存 objdump 结果再 grep：避免 pipefail 下 grep -q 提前退出触发 objdump SIGPIPE，
  # 导致条件恒假、剥离被静默跳过（曾踩过这个坑）。
  sections="$(objdump -h "$lib" 2>/dev/null || true)"
  if printf '%s' "$sections" | grep -qE '\.llvmbc|\.llvmcmd'; then
    echo "[rust-backend] strip LTO sections from $(basename "$lib")"
    objcopy --remove-section=.llvmbc --remove-section=.llvmcmd "$lib"
  fi
done

echo "[rust-backend] done."
