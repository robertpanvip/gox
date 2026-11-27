# GOX 后端架构 V2

> 状态：阶段 1 与阶段 2 的核心机制已落地并通过测试；阶段 3–6 为路线图。
> 本文档是后端架构的正式设计契约，先定边界再写代码——避免边改边漂移。

## 〇、定位与总原则

**Gox 是一套跨平台 UI Runtime，不是多个绘图 Backend 的集合。**
跨平台统一的是 **UI Runtime 与渲染协议**，不强行统一各平台的渲染实现。

三条铁律（不变量，任何重构不得违反）：

1. **不让 Rust 接管 Signal。** GX 的响应式语义（Signal/Owner/Effect）归 Go Runtime。
2. **不让 Go 参与每一帧的绘制。** GPU、Layout、Display List、资源缓存归 Renderer。
3. **不把 Surface、VSync 和窗口生命周期塞进统一的 Window 接口。** 这些由平台适配层负责。

## 一、模块所有权与拆库边界（重要）

后续规划把 **GX 语言独立为语言库，Gox 保持 UI 框架定位**。为此现在就固化边界：

| 模块（当前路径）                    | 归属（未来拆库后） | 说明 |
| ---------------------------------- | ------------------ | ---- |
| `compiler/`、`cmd/gox`             | **gx**（语言库）   | Parser/Analyzer/Transform/Codegen，理解 `sig`/JSX 语法但不依赖任何 UI 实现 |
| `runtime/signal`                   | **gx/runtime/reactive** | Signal/Computed/Effect/Owner，严格单线程模型，不 import scene/app/scheduler |
| `runtime/scheduler`                | **gox**（UI 框架） | FrameClock/FrameScheduler，零依赖（只 stdlib）——语言库可复用它做 CLI TUI 调度 |
| `runtime/scene`、`runtime/canvas`  | **gox**            | Scene/JSX Runtime |
| `runtime/app`（gxapp）             | **gox**            | App 外壳、Commit Gate、帧调度集成；依赖 signal 的方向是「Gox 依赖 GX」，合法 |
| `backend/*`、`examples/*`          | **gox**            | 各平台后端 |

依赖方向规则（CI 可执行）：

```
gx/compiler  ──✗──→  gx/runtime/reactive、gox/*   （编译器不依赖任何运行时实现）
gx/runtime/reactive ──✗──→  gox/*                （Signal 不依赖 Scene/App/Scheduler）
gox/runtime/scheduler ──✗──→  gx/*、gox/runtime/* （scheduler 零依赖，纯 stdlib）
gox/runtime/app  ──✓──→  gx/runtime/reactive、gox/runtime/scheduler（UI 框架消费语言库）
```

未来目录形态（评审 §七 的目录结构 + 拆库后）：

```
gx/                          # 独立语言库
├── cmd/gx/  compiler/  syntax/
├── runtime/reactive/        # Signal · Computed · Effect · Owner
└── examples/

gox/                         # 跨平台 UI 框架
├── runtime/
│   ├── scene/  app/  scheduler/  protocol/
├── renderer/rust/           # 独立渲染内核（不依赖 winit）
├── platform/
│   ├── web/  desktop/  android/  ios/
├── backend/                 # skia（兼容）/ web（DOM）/ rust（桥接）
└── widgets/
```

实施顺序：**先在单仓库内划清边界 → GX 编译器独立编译无 UI 项目验证 → 再拆仓库**。
不一次性拆五六个仓库。

## 二、四层架构

```
┌────────────────────────────────────────────────┐
│ GX Application                                  │
│   GX/Go · Components · Signal · Owner · Scene   │
├────────────────────────────────────────────────┤
│ Frame Runtime                                   │
│   Commit Gate · ChangeSet · FrameScheduler      │
│   FrameClock(VSync/RAF/Choreographer) · 动画     │
├────────────────────────────────────────────────┤
│ Renderer Runtime（Rust 为核心）                  │
│   Scene Mirror · Layout · Style · Text · Paint  │
│   Display List · GPU Resource Manager           │
├────────────────────────────────────────────────┤
│ Platform Runtime（每平台一个）                    │
│   Surface · VSync · Input · 生命周期 · GPU 上下文 │
│   Web / Windows / macOS / Linux / Android / iOS │
└────────────────────────────────────────────────┘
```

## 三、统一帧调度（✅ 已落地）

### 3.1 问题与现状修正

对评审文档的两处修正（以代码事实为准）：

- 「每次 Signal 更新立即 Present」**不准确**：AfterEffects 帧边界 + Dispatcher
  去抖已合并同批更新。真正缺的是 **平台帧时机对齐**（vsync）与 **统一动画续帧**
  （host goroutine 续帧 / skia dirty 维持 / Android paint.Event 三套并存）。
- 「Rust 队列过载丢帧导致失步」**不准确**：过载原本只计数不丢帧；缺的是
  **有界背压**——无限 FIFO 让画面越来越落后。

### 3.2 落地机制

`runtime/scheduler`（零依赖包）：

```go
type FrameTime struct { Timestamp, Delta time.Duration; FrameID uint64 }

type FrameClock interface {
    RequestFrame()      // 可高频并发调用，pending 合并
    CancelFrame()
    Now() time.Duration
    OnFrame(fn func(FrameTime))
}

type Scheduler struct{...}
// RequestFrame：标脏 + 预约一帧（dirty 门控：无变化绝不派发）
// handleFrame：脏 → 恰好执行一次绘制回调；无脏 → 空跑
// Pause/Resume：窗口后台暂停预约，恢复补帧
```

`gxapp.SetFrameClock(clock)`（Render 前调用）启用后：

- Signal 更新 → `requestDrawFrame()` → 标脏 + 时钟预约一帧，**不再立即绘制**；
- 同一刷新周期内多次更新合并为一次提交（验收：`TestFrameClockBatchesSignalsInSameTick`）；
- 无变化零绘制（`TestFrameClockIdleStopsPainting`，100 次空闲 Tick 0 帧）；
- RAF 动画续帧经同一时钟（`TestFrameClockRAFAnimationLoopStopsWhenIdle`），
  取代三套后端各自的续帧机制；
- 后台 `app.Dispatch` 的多次更新串行落定后合并一帧
  （`TestFrameClockConcurrentWritersBatch`）；
- 未调用 `SetFrameClock` 时行为完全不变（Inline/UIExecutor 老路径兼容）。

### 3.3 FrameClock 平台矩阵

| 平台 | 实现 | 状态 |
| ---- | ---- | ---- |
| Web | `rafClock`（requestAnimationFrame 驱动，backend/web/clock.go） | ✅ |
| Android | `paintClock`（x/mobile paint.Event = Choreographer/vsync） | ✅ |
| Desktop | `loopClock`（GLFW 主循环驱动 + PostEmptyEvent 即时唤醒；vsync 由 SwapInterval 承担） | ✅ |
| Headless/测试 | `ManualClock`（手动 Tick，验收工具） | ✅ |
| 桌面兜底 | `TickerClock`（空闲自动停 goroutine） | ✅（未接线，备用） |
| macOS 精确对齐 | CVDisplayLink | 阶段 4 |
| Linux | Wayland frame callback | 阶段 4 |
| iOS | CADisplayLink | 阶段 5 |

## 四、Frame 协议与背压（✅ 已落地）

`Frame{Root, Changes, Version, FullSync}` 契约不变。

**Go → Rust 有界队列 + FullSync 自动恢复**（BACKEND 架构评审 §六.2）：

- Rust `FrameQueue.enqueue`：pending ≥ 120 时执行背压——丢弃全部未消费增量帧、
  置 `needs_fullsync`（不再无限堆积）；
- 新 C ABI `gx_window_take_fullsync(handle)`（读后清零）；
- Go `Window.Present` 开头查询：置 `frame.FullSync=true` → Host 用最新 Root
  全量重建 Rust 镜像，双方恢复一致。

为什么不「只删中间某一帧」：后续增量可能依赖它，必须配 FullSync 才能安全恢复。
高刷 UI 的正确目标是**及时显示最新的正确状态**，不是把中间画面排队播完。

恢复不变量：连续更新、丢帧、队列过载、恢复全流程不出现 Scene 永久失步
（Go 侧 Version 跳号检查 + Rust needs_fullsync 双通道，见 frame_recovery_test.go）。

## 五、Renderer Runtime（阶段 3，路线图）

目标：Rust 渲染内核从 winit 解耦——`renderer/rust` 只暴露：

```rust
trait Renderer { fn attach(&mut self, surface: &dyn Surface); fn submit(&mut self, frame: &Frame); fn render(&mut self, t: FrameTime); }
trait Surface { /* 平台注入：Android Surface / CAMetalLayer / winit window / Canvas */ }
```

内部模块：scene（retained mirror）→ layout（Taffy + 缓存）→ style → text
（cosmic-text/glyphon）→ paint（Display List + 缓存）→ gpu（wgpu + 资源管理）。

不变量：改文本颜色不得重建布局树；transform/opacity 动画走 GPU；
文字布局与动态内容仍允许 CPU 参与。

## 六、迁移路线（评审 §九 修订版）

| 阶段 | 内容 | 状态 |
| ---- | ---- | ---- |
| 1 | 统一帧调度（FrameClock/FrameScheduler + 三平台时钟接入） | ✅ 已完成 |
| 2 | Wire 背压 + FullSync 自动恢复 | ✅ 已完成（NodeID uint64 留待协议 v3） |
| 3 | Rust Renderer 从 winit 解耦，Surface 注入 | 待做（核心） |
| 4 | Desktop 正式迁移（Windows 优先；验证 60/120/144Hz；对比 Skia） | 待做 |
| 5 | Android 原生 Surface + iOS 首次技术验证（Go Framework 集成 + C ABI 链接） | 待做 |
| 6 | WebGPU（Rust→WASM，保留 DOM Driver） | 待做 |

NodeID 二进制协议 uint64 化（评审 §六.3）需要协议版本升级（GRX3）：牵动 GRX1/GRX2
格式与 Rust binprotocol 解析，随阶段 3 一起做，避免两度破坏 wire 兼容。

## 七、性能指标（阶段 4 起建立监控）

| 指标 | 目标 |
| ---- | ---- |
| 同周期 Signal 更新 | 合并为一次提交（✅ 已验收） |
| 无变化帧 | 不触发绘制/Present（✅ 已验收） |
| 队列过载 | 有界背压 + FullSync 恢复（✅ 已验收） |
| 帧预算 | 120Hz ≤ 8.33ms / 144Hz ≤ 6.94ms |
| 稳态提交 | 只传增量 |
| GPU 资源 | 跨帧复用 |
| 统计口径 | P50/P95/P99 帧耗时 + 掉帧率，阶段耗时分解（Signal→Commit→Encode→Layout→Paint→Submit→Present） |

## 八、线程模型

- **Signal/Effect**：严格单线程（UI Runtime 上下文）。后台任务经 `app.Dispatch` 投递。
- **drawNowErr（commit gate）**：与全部 Scene Mutation 互斥，gate 可重入。
- **FrameClock 回调**：必须在平台 UI 线程（Web=RAF 回调、Android=paint 事件、
  Desktop=GLFW 主循环、TickerClock 例外——仅适用于无 UI 线程约束的后端）。
- **Rust FrameQueue**：draw（任意线程）→ proxy 唤醒 → 事件循环 drain（主线程 apply）。

## 九、验收清单（本阶段）

- [x] 同一刷新周期内多次 Signal 更新只提交一次 Frame（ManualClock 断言 Version/draws）
- [x] 无变化零绘制（100 空闲 Tick 0 帧）
- [x] RAF 动画续帧走统一时钟、结束自动停帧
- [x] Pause/Resume（后台暂停、恢复补帧）
- [x] 并发 Dispatch 合并提交
- [x] 队列过载 → FullSync 恢复（cargo check + cgo 链接 + Go 测试通过）
- [x] 老路径零回归（未 SetFrameClock 时全量测试通过）
