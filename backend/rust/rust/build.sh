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
mapfile -t LIBS < <(find target/release/deps -maxdepth 1 -name 'libgxbackend_rust-*.a' -type f)
if [ "${#LIBS[@]}" -eq 0 ]; then
  echo "[rust-backend] ERROR: 未找到 libgxbackend_rust-*.a" >&2
  exit 1
fi

for lib in "${LIBS[@]}"; do
  # 只对含 bitcode section 的库动手，避免无谓重写。
  if objdump -h "$lib" 2>/dev/null | grep -qE '\.llvmbc|\.llvmcmd'; then
    echo "[rust-backend] strip LTO sections from $(basename "$lib")"
    objcopy --remove-section=.llvmbc --remove-section=.llvmcmd "$lib"
  fi
done

echo "[rust-backend] done."
