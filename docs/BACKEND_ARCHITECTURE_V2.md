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

## 五、Renderer Runtime（阶段 3）

**已落地（第一步：Surface 注入解耦）**：`render.rs` 渲染内核不再 import
任何窗口库（契约测试 `render_kernel_has_no_window_library_dependency`
守护，重新引入即编译失败）。内核侧唯一平台接口：

```rust
pub trait SurfaceProvider: Send + Sync {
    fn pixel_size(&self) -> (u32, u32);          // 物理像素
    fn raw_display_handle(&self) -> RawDisplayHandle;
    fn raw_window_handle(&self) -> RawWindowHandle;
    fn request_redraw(&self);                     // 平台事件循环唤醒
}
```

`Renderer::new(Arc<dyn SurfaceProvider>)` 经 wgpu `create_surface_unsafe`
挂载平台原生 Surface；`request_redraw()` 转发 provider（内核不感知
winit/Choreographer/CADisplayLink）。winit 侧由 `lib.rs::WinitSurface`
适配器实现——未来 Android NDK / iOS CAMetalLayer 宿主各写一个同款适配器，
Renderer 一行不改。

安全契约：Renderer 同时持有 provider 本体；surface 字段先于 provider 字段
声明（Rust 按声明顺序 drop：surface 先销毁，句柄来源最后销毁）。

**第二步已落地（模块物理拆分）**：render.rs 由 2635 行拆为三个职责模块——
`paint.rs`（1450 行：DisplayList::from_scene = Taffy 布局 + 绘制指令编译，
含浮层/滚动条/裁剪/文本绘制计划及其测试）、`util.rs`（106 行：文本测量
Taffy 回调 + 颜色/尺寸/字体解析，paint/render 共用）、`render.rs`（保留
Renderer GPU 执行 + SurfaceProvider）。模块可见性经 pub(crate) 精确控制；
cargo test 88 项全过（含测试搬迁后的 5 个 paint 指令域测试组）。
拆分过程中顺带修正了阶段 2 遗留的 FrameQueue 旧断言（原「过载不丢帧」
与新背压语义矛盾），并新增持续过载二次背压用例。

**布局缓存已落地（阶段 3 增强）**：新增 `layout.rs`（LayoutCache + 布局签名）——
布局相关属性白名单参与值哈希，其余 props（color/transform/opacity/scroll-top
等纯绘制域）仅 key 存在性哈希。签名命中 → 跳过 build+compute_layout，只重跑
emit。验收测试 4 项（改颜色复用 / 布局属性重建 / 滚动复用 / 命中与全量路径
布局结果逐项一致）+ CPU 基准 `gx_bench_frame_cached`：155 节点下命中路径
684μs → 346μs（**2.0× 加速**），颜色过渡 / opacity 动画 / 滚动帧全部免布局。

**emit 指令缓存已落地（阶段 3 收尾）**：`EmitCache`（layout.rs）+ `NodeCmds` /
`append_cmds` / 子树渲染签名（paint.rs）——布局命中帧的 emit 按「子树渲染签名」
复用上帧指令段，跳过 parse_style / collapse_whitespace / 渐变切分 / 递归编译，
只做段拷贝。正确性构造：

- 签名 = 继承链滚动哈希（EMIT_INHERIT_PROPS 白名单：color/font-size/字重/
  行高/对齐/空白折叠/渐变/selection/font-family/overflow）⊕ 本节点结构身份 +
  全 props 值（顺序无关求和哈希）+ events + Taffy 绝对坐标 + 收到的裁剪矩形 ⊕
  孩子签名。归纳保证：签名相等 ⇔ emit 全部输入相等 ⇔ 输出指令段等价；
- 裁剪传播与 emit 共用单一来源（child_clip_of），防止两处逻辑漂移；
- key = Taffy NodeId（布局复用帧间稳定）；rebuild 帧签名表为空 → 查询自然
  miss、帧末旧槽全淘汰，语义自动退化为"不缓存"；
- 淘汰与 glyph_cache 同模式（每帧 live 集，帧末 retain）；
- 含 video 的子树禁缓存（video 当前帧每帧变化，签名无法覆盖解码产物）；
- 浮层三遍遍历（scrim/popover/modal/portal）不参与缓存（节点数少，
  overrides 定位不进签名，规避第二遍布局的输入耦合）。

验收测试 4 项：静态场景命中且输出与全量路径逐项一致（shapes/texts/
hitboxes/inputs/scrollbars/measured 六通道）/ 继承 color 变化失效且文字
颜色更新 / 滚动帧失效且子项整体平移 + thumb 下移 / 文本变化 rebuild 后
无旧内容残留。CPU 基准（release，155 节点）：全量 from_scene 670μs →
布局缓存 346μs → emit 指令缓存 **243μs**（累计 **2.76×**，本步 1.42×）；
cgo 基准 `gx_bench_frame_cached` 306μs（含场景树 clone 47μs）。

**GRX3 协议已落地（NodeID uint64 直传）**：Go runtime 的 Node.ID 本就是
uint64（gxscene.NodeID，NoNode=0 保留值），旧协议把它字符串化进池纯属传输
负担。GRX3（magic 0x3358_5247）：

- **全量帧**：节点结构 32→40 字节，尾部新增 `node_id: u64` 直传；不再携带
  id 字符串（id 池槽指向空串）；
- **增量帧**：动作 24→48 字节，node/child/before id 全部 u64 直传
  （Change.NodeID 字符串经 strconv.ParseUint 还原，空串=NoNode=0；非数字 id
  → 编码失败回退全量快照，与 GRX2 失败语义一致）；结构动作的内嵌子树 blob
  同步用 GRX3 帧；
- **能力探测**：`gx_window_version()` 升至 0.3.0，Go 宿主探测 minor≥3 选择
  GRX3 编码，旧后端自动走 GRX1/GRX2——双版本并行，无破坏性切换；
- **Rust 内部热路径 u64 化**：SceneNode 增加 `nid: u64`（id: String 保留兜底），
  TextDraw.id / hitboxes / inputs / measured 全部改用 u64——每帧省去数百次
  String clone/hash；glyph_cache key String→u64；查找（find_mut / child_index）
  改为 nid 优先 + id 字符串兜底（matches 单一来源）；
- **边界兼容**：C ABI 回传（click/focus 事件的 node_id、measured JSON）仍是
  十进制字符串——Go 宿主（parseNodeID / publishMeasuredWire）零改动；
- 验收：Rust 100 项测试（含 GRX3 帧解析 / delta u64 动作 / NoNode 锚点 /
  video 禁缓存等）；Go 端到端 5 项（GRX3 全帧镜像 roundtrip 100 节点 /
  帧不含 id 字符串 / GRX3 delta 补丁 SetText+Remove+Insert / 非数字 id 回退
  / 空锚点追加末尾）。

**后续（待做）**：GPU 资源管理器独立化（tex_cache 已具雏形）；DisplayList
段零拷贝复用（dl 改段列表结构，消除 append 时的 String clone）。

内部模块：scene（retained mirror）→ layout（Taffy + 缓存）→ style → text
（cosmic-text/glyphon）→ paint（Display List + 缓存）→ gpu（wgpu + 资源管理）。

不变量：改文本颜色不得重建布局树；transform/opacity 动画走 GPU；
文字布局与动态内容仍允许 CPU 参与。

## 六、迁移路线（评审 §九 修订版）

| 阶段 | 内容 | 状态 |
| ---- | ---- | ---- |
| 1 | 统一帧调度（FrameClock/FrameScheduler + 三平台时钟接入） | ✅ 已完成 |
| 2 | Wire 背压 + FullSync 自动恢复 | ✅ 已完成 |
| 3 | Rust Renderer 从 winit 解耦，Surface 注入 | ✅ 完成：SurfaceProvider 注入 + 契约测试 + paint/util 模块拆分 + 布局缓存（2.0×）+ emit 指令缓存（累计 2.76×）+ GRX3 协议（NodeID uint64 直传） |
| 4 | Desktop 正式迁移（Windows 优先；验证 60/120/144Hz；对比 Skia） | 🟢 代码迁移完成：**全部消费方**切 `gxapp.Use(接口值)` 接入（desktop-demo / widget-showcase / wasm-demo / web-widget-showcase / gox-idea / playground-desktop / rustbackend cmd / **skia 三 demo**：gallery / style-demo / overlay-demo 切 rustbackend，四份重复 glwindow+GLFW 装配删除）；**P4 目标态落地**——Host 唯一 UI Loop（loop.Queue）+ 轮询主循环 + `HostedWindow` 强制接口（无自驱回退），Rust 侧 `gx_window_run` 删除、改为 `gx_window_pump`（winit pump_events 嵌入泵，事件循环随窗创建、Host 主循环逐轮驱动）。**P4.5 高刷专项**：`VSyncControl` 落地（`gx_window_set_vsync` 运行时切换 Fifo/Mailbox/Immediate，不支持的模式自动回退 + 日志）、`examples/fpsbench` 实测工具（RAF 连帧 + 帧时间直方图 + V 键切模式 + 关窗汇总）。backend/skia 现仅剩离屏光栅验证后端（测试/截图/Playground-Android 渲染库），GUI 消费方为零。遗留：① present（get_current_texture）阻塞占住 UI 线程——多窗口高刷互相拖累，Xvfb 无 vblank 环境下 Fifo 永久阻塞已实测确认，真机硬件 vsync 实测与 present 线程化待做；② 默认 idleWait 16ms 的输入感知延迟（WithIdleWait 可调）。**MousePassthrough 已接线**（`gx_window_set_mouse_passthrough`，整窗级开关：X11 经 dlopen libXext 调 XShape ShapeInput（不引入链接期依赖）/ Windows 扩展样式 WS_EX_TRANSPARENT / macOS setIgnoresMouseEvents，窗口未创建时缓存请求由 resumed 应用）；区域级精确穿透（透明区穿透、内容区可点，旧 glwindow 按 DrawLog 逐帧重设 XShape 的语义）待渲染端内容框日志补齐 |
| 5 | Android 原生 Surface + iOS 首次技术验证（Go Framework 集成 + C ABI 链接） | 🟡 Rust 侧就绪：android_surface.rs（AndroidNdkWindowHandle）/ ios_surface.rs（UiKitWindowHandle）provider + lifecycle 重建 API + 8 项无头测试；交叉编译验证 aarch64-linux-android（--features android-native-activity）与 aarch64-apple-ios 双 target 零错误；C ABI 接线待 NDK/真机环境 |
| 6 | WebGPU（Rust→WASM，保留 DOM Driver） | 🟡 编译面验证完成：gx_gpu 三种构建（WebGPU/WebGL 兜底/CPU 软光栅）与渲染内核整体均零错误编译到 wasm32-unknown-unknown；web_surface.rs（WebWindowHandle）provider + 4 项无头测试；wasm 导出 ABI（wasm-bindgen/JS glue）与浏览器实测待后续 |

NodeID 二进制协议 uint64 化（评审 §六.3）已作为独立变更落地（GRX3，见上）。

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
