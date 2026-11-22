# Go/Skia 后端 vs Rust 后端：纯 CPU 管线性能基准

> 结论先行：在**布局**这一两边都做、且可公平对比的 CPU 阶段，Rust 的 Taffy 0.5
> 比 Go/Skia 的 Yoga 3.x 快约 **1.3~1.7 倍**（155 节点场景 328µs vs 569µs）。
> 但**这不等于"Rust 后端整体更快"**——GPU 渲染、字体 shaping、事件回传未纳入，
> 且本环境是 llvmpipe 软件渲染，GPU 对比无意义。详见文末局限。

## 背景：为什么之前没有这个基准

在实现 Rust 后端的过程中，我一度以"选 Rust 是为了性能"作为架构理由，但**当时没有任何实测**，
只有静态库体积数据（80M→24M）。体积不是性能。本基准补上这个缺口，并诚实呈现结果——
包括"哪些地方 Rust 确实更快"和"哪些地方我无法证明"。

## 测试口径

选取两条后端**都执行、且都是纯 CPU** 的阶段做对等对比，排除窗口/GPU/字体 shaping 噪声：

| 阶段 | Go/Skia 后端 | Rust 后端 |
|---|---|---|
| 布局 | `computeLayout()`（Yoga 3.x，C++ via cgo） | `DisplayList::from_scene()`（Taffy 0.5） |
| 场景协议 | `serializeFrameBin()`（Go 编码） | `FrameView::parse()` + 镜像树重建 |
| 绘制指令 | painter 侧遍历 | DisplayList 生成 |

公平性措施：

- 两边用**完全相同的 Template 构造**的场景树（root + 标题 + N 张卡片，每张卡片 2 个文本）。
- 文本测宽注入**同口径估算函数**（ASCII 0.56em），排除 Skia Font shaping 与 glyphon 的差异。
- 两边都从已有场景树**新建布局树**（Skia 新建 Yoga 节点，Rust clone 场景树 + 新建 Taffy 节点），
  均包含"建树"成本。
- Rust 侧在 C ABI 内部循环 `iters=200` 摊销 cgo 调用开销，报告 `ns/op-rust`（单次管线）。

## 结果

环境：`AMD EPYC 9K65 192-Core`，Linux，Go 1.27，Rust release（LTO+fat+opt-level=z）。
两组各跑 2 次，数字稳定（波动 <5%），取第二次。

### 布局引擎（核心可比项）

| 场景规模 | 节点数 | Skia Yoga 3.x | Rust Taffy 0.5 | Rust 快 |
|---|---|---|---|---|
| 3 张卡片 | 20 | 74,988 ns | 59,050 ns | **1.27×** |
| 10 张卡片 | 55 | 197,302 ns | 118,251 ns | **1.67×** |
| 30 张卡片 | 155 | 569,034 ns | 328,215 ns | **1.73×** |

规模越大，Taffy 优势越明显。Skia 侧每次布局产生 99~729 次 Go 分配（cgo 边界 + Yoga 节点），
Rust 侧 Taffy 用 arena 分配，分配压力更低（cgo 边界不统计 Go 侧 alloc）。

### 场景协议编码（仅 Go 侧有此阶段）

| 场景规模 | 字节数 | Go serializeFrameBin |
|---|---|---|
| 20 节点 | 1,573 B | 12,849 ns |
| 55 节点 | 3,995 B | 27,400 ns |
| 155 节点 | 11,035 B | 94,403 ns |

Rust 侧对应的是解析 + 建镜像树 + 布局，已含在"布局引擎"一栏的 `ns/op-rust` 中
（该栏是"解析→clone→布局→指令生成"的完整链路，比 Skia 单独的布局阶段**吃得更多**，
因此 Taffy 的实际纯布局优势比表中数字还略大）。

### 端到端 CPU 管线合计（估算）

以 155 节点场景为例：

- Go/Skia：编码 94µs + 布局 569µs ≈ **663µs**（不含 painter 实际光栅化）
- Rust：解析+布局+指令 ≈ **328µs**（不含 tiny-skia 光栅化 + glyphon 文本）

CPU 前置管线 Rust 约快 2×。但两边的**实际光栅化/文本绘制**未纳入——这正是下一节局限。

## 局限与诚实声明

1. **GPU/光栅化未测**：本环境是 wgpu over llvmpipe（软件渲染）。Skia 用 C++ Skia 光栅化，
   Rust 用 tiny-skia（CPU）画图形 + wgpu/glyphon 叠字。两者在真实 GPU 上的表现需要硬件环境，
   本基准**不能**回答"谁渲染更快"。
2. **字体 shaping 未测**：两边文本测宽都用了同口径估算，绕开了真实字体差异。
   真实场景中 Skia 的 HarfBuzz shaping 与 glyphon/cosmic-text 的表现未对比。
3. **布局属性覆盖**：基准场景只用 column/row/gap/padding/width/height/align-items。
   Yoga 支持 flex-wrap/align-content/百分比/min-max/aspect-ratio 等完整 Flexbox，
   Taffy 0.5 也支持，但复杂属性的性能未逐一测。
4. **Taffy 优势的前提**：Rust 侧基准每次 clone 了场景树（`root.clone()`），
   这是为了模拟"增量打补丁后的树"。若生产路径能复用树（真正只打补丁不 clone），
   Rust 会更快；这里已把 clone 计入，属于**保守估计**。
5. **不是"整体后端"对比**：事件回传 JSON 解析、C ABI 往返、窗口系统集成等未测。

## 复现

```bash
# Rust 侧（解析→建树→Taffy 布局→指令，含 Go 编码输入）
cd examples/rust-backend/go
GOWORK=off CGO_ENABLED=1 go test -run '^$' -bench BenchmarkRustParseLayout -benchtime=1s .

# Go/Skia 侧（Yoga 布局）
cd backend/skia
GOWORK=off go test -run '^$' -bench BenchmarkSkiaLayout -benchtime=1s .

# Go 侧 GRX1 编码
cd examples/rust-backend/go
GOWORK=off CGO_ENABLED=1 go test -run '^$' -bench BenchmarkGXEncode -benchtime=1s .
```

基准源码：
- `examples/rust-backend/go/bench_test.go`
- `backend/skia/bench_test.go`
- Rust 纯 CPU 入口：`render.rs::bench_frame` + C ABI `gx_bench_frame`

---

# 补充基准：打包体积 / 编译时间 / 启动时间

> 上一版只量化了运行期 CPU 管线，遗漏了工程侧最常被问的三项：**打包体积**、**编译(打包)时间**、
> **冷启动到首帧**。本节补齐，口径与局限同前，全部在本环境实测。

## A. 打包体积

环境同一台 EPYC 机器，`rustc 1.93`，release profile = `opt-level=z + lto=fat + codegen-units=1 + strip=symbols`。

| 产物 | 体积 | 说明 |
|---|---|---|
| `libgxbackend_rust.a`（优化后） | **24.47 MB**（24,472,846 B） | 静态库，300 个归档成员，Rust LTO 已去符号 |
| demo 可执行文件（未优化 profile） | **30.0 MB**（31,033,136 B） | 初版：`opt-level` 默认 + 带 debug_info |
| demo 可执行文件（优化后） | **12.4 MB**（12,443,768 B） | 当前交付；相对初版 **↓60%** |
| demo 可执行文件（优化后再 `strip`） | **9.1 MB**（9,123,736 B） | Go 链接仍残留 debug_info，可再 ↓27% |
| Skia 后端 demo（对照） | **26 MB** | `examples/desktop-demo/gox-desktop-demo` |

体积优化三板斧（对应上面 30MB→12MB）：

1. Cargo release profile `opt-level="z" + lto="fat" + codegen-units=1 + strip="symbols"`（LTO 跨 crate 死代码消除）；
2. Go 侧 `-ldflags="-s -w"` 可再去掉符号表（若追求极致，9MB 档）；
3. 静态库本身已经 strip，Go 可执行文件仍带 `debug_info`，所以 `strip` 还能再砍 3MB。

动态依赖极简：只链 `libm / libgcc_s / libc / ld-linux`，**不依赖 libstdc++ 或 GPU 运行时**（wgpu 走 Vulkan/GL 动态加载），部署友好。

> 诚实提示：体积优化用 `opt-level=z`（体积优先）而非 `3`（速度优先），是以**运行期性能**换**分发体积**的取舍。
> 上一节布局基准是在该 `z` profile 下测的，若改 `opt-level=3` 布局数字还能更快，体积会回升。

## B. 编译 / 打包时间

| 阶段 | 耗时 | 口径 |
|---|---|---|
| **Rust 冷编译**（全量 release，LTO fat） | **98.0 s** | `cargo clean` 后全新 `CARGO_TARGET_DIR` 编译（含 wgpu/naga/glyphon/winit 全部依赖） |
| Rust 增量编译（无改动） | **0.18 s** | 源码未变，`cargo build --release` no-op |
| Rust 增量重链（改 lib.rs） | **58.0 s** | 改一个 C ABI 文件触发 LTO fat 全量重链——**fat LTO 的代价** |
| **Go demo 构建**（冷 GOCACHE） | **6.78 s** | 独立空 `GOCACHE`，首次全量 |
| Go demo 构建（热 GOCACHE） | **3.05 s** | 二次缓存命中 |

关键观察：

- **Rust 冷编译 98s** 是最大头，几乎全花在 wgpu/naga 着色器编译器与 LTO fat 上；对 CI 是可接受的一次性成本。
- **fat LTO 的重链很贵（58s）**：任何 Rust 源码改动都要重跑全量 LTO。开发迭代建议临时切 `lto="off"` 或用独立 target 目录，发版前再开 fat LTO。
- Go 侧构建很快（3–7s），因为只是 cgo 链接已编好的静态库。
- 对比：Skia 后端（纯 Go + cgo 编 Skia C++）首次构建需编译整套 Skia，通常远大于 Rust 静态库的冷编译；本环境未重新全量构建 Skia，故不列精确数字，避免编造。

## C. 冷启动到首帧

口径：从进程 `main` 入口到**首帧真正渲染完成**（不只是提交）。Go 侧 `gx_window_new`/`Draw` 几乎瞬时（<0.2ms），
真正大头在 `gx_window_run` 的 `resumed()` 里初始化 wgpu 适配器/设备 + glyphon 字体系统。为测到这个时刻，
加了一个**环境变量门控 `GX_STARTUP_PROBE=1`** 的首帧探针：首帧 `render()` 后经已有事件回调回传分阶段耗时
（默认关闭、零开销，作为可观测性设施保留，不改 C ABI）。基准程序 `go/cmd/startupbench` 消费该事件。

环境：Xvfb + llvmpipe（**软件渲染**，真实 GPU 上会更快）。场景为 10 卡片中等规模（启动时间与节点数基本无关，
主要是 GPU/字体初始化）。

| 阶段 | 耗时 | 说明 |
|---|---|---|
| exec → main（进程启动基线） | **~1 ms** | 极简 Go 程序实测 0.96–1.60ms |
| main → OpenWith（gx_window_new） | **0.03–0.05 ms** | 仅分配句柄 |
| OpenWith → Render 提交首帧 | **0.11–0.14 ms** | 建树 + GRX1 序列化 + memcpy |
| Render 提交 → 首帧可见（首次/冷） | **~418 ms** | Xvfb/llvmpipe 首次 shader 编译 + X server 懒初始化 |
| Render 提交 → 首帧可见（复用/热） | **59–75 ms**（中位 ~71ms） | 同一 Xvfb 会话内连续 8 次，稳定 |
| 其中 Rust `init_ms`（run→Renderer::new 完成） | **62–71 ms**（热） | wgpu 设备 + 字体系统初始化，占启动 99% |
| 其中首帧 `render()` | **<0.5 ms** | 10 卡片场景光栅化，llvmpipe 下仍很快 |

结论：

- **热启动到首帧 ~71ms**，其中 **99% 是 wgpu/字体初始化**，布局与光栅化可忽略。
- **冷启动 ~418ms** 主要来自 llvmpipe 首次着色器编译与 X server 懒初始化，属软件渲染环境特有；
  真实 GPU + 显卡驱动下首次通常远低于此（但本环境**无法证实**，不编造数字）。
- `exec→main ~1ms` + `Go 提交 <0.2ms` 说明 Go 侧不是瓶颈；优化启动应聚焦 Rust 的 GPU/字体初始化
  （如懒加载字体子集、复用 device、预编译 pipeline cache）。

## 复现（补充部分）

```bash
cd examples/rust-backend/rust

# B: Rust 冷编译（独立 target 目录，不破坏现有库）
CARGO_TARGET_DIR=/tmp/gx-clean-target bash -c 'rm -rf /tmp/gx-clean-target; \
  t0=$(date +%s%N); cargo build --release; t1=$(date +%s%N); \
  echo "cold_sec=$( python3 -c "print(($t1-$t0)/1e9)" )"'
cargo build --release                       # 增量 no-op

# B: Go 构建时间
cd go/cmd/demo
GOWORK=off go build -o /tmp/demo .          # 热缓存
GOWORK=off GOCACHE=/tmp/emptycache go build -o /tmp/demo2 .   # 冷缓存

# A: 体积
cd ../../../../   # 回到仓库根，或直接用绝对路径
ls -l examples/rust-backend/rust/target/release/libgxbackend_rust.a
strip -c /tmp/demo && ls -l /tmp/demo       # strip 前后

# C: 启动时间（需 Xvfb + llvmpipe）
cd examples/rust-backend/go
GOWORK=off CGO_ENABLED=1 go build -o /tmp/startupbench ./cmd/startupbench
Xvfb :99 -screen 0 1280x800x24 & XPID=$!; sleep 1
for i in 1 2 3 4 5; do
  GX_STARTUP_PROBE=1 DISPLAY=:99 /tmp/startupbench 2>/dev/null \
    | grep -E 'main_to_first_frame_ms|rust_stages'
done
kill $XPID
```

补充基准源码：
- 启动探针：`rust/src/lib.rs`（`GX_STARTUP_PROBE` 门控，`gx_window_run`/`resumed`/`RedrawRequested` 三处）
- 启动基准消费端：`go/cmd/startupbench/main.go`
