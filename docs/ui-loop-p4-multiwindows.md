# P4 多窗口设计：共享单一 UI Loop + 窗口级故障隔离

> 状态：设计方案（待评审，尚未实现）
> 前置：`ui-loop-p0.md`（UIExecutor / Runtime 隔离）、`ui-loop-p2-desktop.md`（桌面 Loop）、`ui-loop-p3-web.md`（Web 边界）
> 硬约束（用户拍板）：**一个窗口崩溃，不得带崩整个应用**；所有窗口**共享同一个 UI Loop**。

---

## 1. 目标与非目标

### 目标
- 一个进程内同时打开 N 个窗口，各自独立场景树 / 事件 / 生命周期。
- N 个窗口共享**一个** UI Loop、一个响应式执行上下文、一个主事件循环。
- 任一窗口的用户回调 panic / 绘制失败，只**熔断该窗口**，其余窗口继续正常响应。
- 单窗口 API（`gxapp.Open` / `OpenWith` / `App.Run`）行为不变，向后兼容。

### 非目标（本阶段明确不做）
- 用户自己 `go func(){ panic() }()` 的跨 goroutine panic —— Go 运行时行为，框架无法 recover。
- 原生层 segfault（Skia/GLFW 的 C++ 崩溃）—— Go `recover` 无能为力，只能靠进程隔离，超出本模型。
- Web 端真正的多窗口共享 Loop —— 浏览器天然一 tab 一文档，见 §8。
- 响应式核心并发化 —— 仍是单线程唯一，符合 P0 评审决议。

---

## 2. 现状盘点：为什么现在"开不了第二个窗口"

| 层 | 现状 | 障碍 |
| --- | --- | --- |
| App | `App{win Window}` 单字段；`OpenWith` 每次 `new App` + `new signal.Runtime` | 有"多 App 隔离"雏形，但没有宿主把它们管起来 |
| Loop | `loop.Queue` 是并发安全的共享队列，含 `DrainTurn` / `Wake` / 任务级 `recover+OnTaskPanic` | 基础设施齐全，但每个 `glWindow` 各自 `loop.New()`，没有进程级单例 |
| 响应式 | `observers/ownerStack/batch` 包级单实例，但 panic 后**全部 defer 复位**（`signal.go:129-157`、`review_fixes_test.go`） | 单线程唯一是**特性**不是缺陷——多个 App 串行 drain 同一 Loop 天然安全 |
| 后端 | `glWindow` 只有一个 `*glfw.Window`；`Run()` 阻塞单循环；`Close()` 调**全局** `glfw.Terminate()` | 关任意窗口会终结整个 GLFW；无窗口池；无统一主循环 |
| API | 全仓无 `Host` / `WindowManager` / `OpenWindow` | 没有任何面向用户的"再开一扇窗"入口 |

**结论**：不缺底层能力，缺的是「宿主层（Loop 单例 + 窗口集合 + 统一主循环 + 故障监督）」和「后端从一窗口一循环改成一循环多窗口」。

---

## 3. 核心模型：Host = 一个 Loop + N 个 App

```
进程
└── Host（唯一）
    ├── UI Loop（*loop.Queue，单例，GLFW 主线程 drain）
    │   └── 所有窗口的 app.Dispatch / RAF / 网络回调都投递到这里
    ├── 主事件循环 Host.Run()（唯一）
    │   └── WaitEvents → Loop.DrainTurn → 逐窗 Poll/Paint → 收 ShouldClose
    ├── windows []*App（有序集合）
    └── 故障监督 FaultHandler
        ├── Window A → App A → Scene A → Runtime A → Owner A
        ├── Window B → App B → Scene B → Runtime B → Owner B
        └── Window C → App C → Scene C → Runtime C → Owner C
```

**三条不变量**：
1. **一个 Loop**：进程内只有一个 `*loop.Queue`；所有窗口的 UI 任务进同一队列，串行执行，响应式核心单线程假设成立。
2. **一个主循环**：`Host.Run()` 是唯一阻塞循环；窗口后端不再各自 `Run()`，改为被 Host 轮询。
3. **故障隔离域 = 窗口**：recover 的边界落在"窗口任务 / 窗口绘制 / 窗口事件"三层，panic 归因到具体 App，只熔断它。

---

## 4. API 设计

### 4.0 接入层已落地：gx.use(Backend) → gxapp.Use / Handle.Render

GX 接入形态与 Go 落点的对应关系（首版已实现）：

```
const app = gx.use(Backend)              →  app := gxapp.Use(rustbackend.NewBackend())  // 接口注入
app.render(() => <App />, {              →  app.Render(func() any { return ui.App() },
    title: "Main Window",                    gxapp.WindowConfig{Title: "Main Window", Width: 1200, Height: 800})
    width: 1200, height: 800,
})
app.render(() => <Settings />, {...})    →  app.Render(func() any { return ui.Settings() }, gxapp.WindowConfig{...})
app.run()                                →  app.Run()   // 全部窗口退出才返回
```

**Backend 是接口注入，不是字符串分发**（gxapp.Backend，runtime/app/backend.go）：

```go
// gxapp 只定义接口形状，后端实现它即可插拔；核心不依赖任何具体后端。
// Use(接口值) 是唯一接入路径——没有工厂函数、没有按名字分发。
type Backend interface {
    Name() string                                             // 故障归因 / 调试标识
    NewWindow(cfg WindowConfig) (Window, error)               // 每窗一次，独立实例
}
```

后端侧实现（rustbackend/backend.go 为参考）：`type Backend struct{}` + `Name() string` +
`NewWindow(cfg) (Window, error)`。

**Backend 可选能力段**（按需实现 + Host 探测，与 Window 的 UIExecutor / WindowBinder 同一模式；
没有某能力的后端不实现即可，接口不做肥）：

| 可选接口 | 方法 | 语义 |
|---|---|---|
| `BackendInitializer` | `InitBackend() error` / `ShutdownBackend()` | 进程级生命周期：首个窗口创建前幂等 Init（失败阻断开窗）；Host/Handle 全关后 Shutdown |
| `BackendDescriptor` | `Describe() BackendInfo` | 静态描述：版本 / 协议（GRX3…）/ 渲染器，供诊断与协议分支 |
| `BackendCapabilities` | `Capabilities() Capabilities` | 能力声明：MultiWindow / Transparent / MousePassthrough / Incremental / VSyncControl |

**窗口实例访问**：装配层需要调后端专属扩展（rustbackend 的 `SetDispatcher` / `BindApp` /
`RequestClose`）时，经 `handle.WindowOf(app)`（Host.WindowOf 同名代理）取回 Window 再自行断言
类型——接入层接口本身保持后端无关。

demo 接入现状：examples/desktop-demo、examples/widget-showcase、rustbackend cmd（demo /
gradcheck / overlaydemo / startupbench）、examples/wasm-demo、examples/web-widget-showcase
（gxweb.NewBackend，挂 "#app" DOM 容器）均已切 `Use(接口值)` 路径。
Window 的可选扩展（UIExecutor / WindowBinder）由 Host 装配时按能力探测注入，后端无需转发。

首版语义：每次 `Render` = 一扇独立窗口 + 独立 App（独立场景树 / signal Runtime）；
后端实现 `UIExecutor` 时自动注入（Dispatch/RAF 进窗口事件泵）；实现 `WindowBinder`
（rustbackend.BindApp）时 Render 后自动启用增量流。窗口 Run panic 由 Host recover
归因上报 `FaultHandler`，只熔断该窗口。已知边界：响应式帧边界（signal.AfterEffects）
是进程级全局，跨窗 flush 会唤醒其它窗口的帧边界——但树内容不变，增量后端对无变化
帧短路，不产生真实绘制/传输；GLFW 主线程亲和后端的多窗口待目标态改造。

### 4.1 Host（新增，`runtime/app/host.go`）

```go
// Host 是进程级应用宿主：持有唯一 UI Loop 与所有窗口，负责统一主循环与故障隔离。
type Host struct { /* loop, windows, fault handler, closed */ }

type HostOption func(*Host)

func NewHost(opts ...HostOption) *Host
func WithLoop(q *loop.Queue) HostOption            // 复用外部 Loop；缺省自建
func WithFaultHandler(fn func(Fault)) HostOption   // 窗口故障回调

func (h *Host) Loop() *loop.Queue
func (h *Host) OpenWindow(cfg WindowConfig, win Window) (*App, error) // win 需实现 HostedWindow
func (h *Host) OpenBackendWindow(cfg WindowConfig) (*App, error)      // 走注册表
func (h *Host) CloseWindow(app *App) error                            // 幂等；只关这一个
func (h *Host) Windows() []*App                                        // 快照
func (h *Host) Run() error                                             // 唯一主循环，直到所有窗口关闭
func (h *Host) Close() error                                           // 关所有窗口 → 停 Loop → 后端 Terminate（一次）

// Fault 描述一次窗口级故障，供应用层记录/展示/决定是否重开。
type Fault struct {
    App    *App
    Err    error
    Stack  string
    Source FaultSource // dispatch | raf | draw | event
}
type FaultSource uint8
const (
    FaultDispatch FaultSource = iota
    FaultRAF
    FaultDraw
    FaultEvent
)
```

### 4.2 窗口后端新角色（可选接口，向后兼容）

```go
// HostedWindow 由 Host 单一主循环驱动；实现后 Window.Run() 退化为 no-op。
// 未实现的老后端仍走原路径（单窗口兼容）。
type HostedWindow interface {
    Window
    Poll() error        // 非阻塞处理本窗口待处理事件
    Paint() error       // 脏则绘制一帧
    ShouldClose() bool  // 本窗口是否请求关闭
}
```

### 4.3 App 侧最小增补

```go
// SetFaultHandler 注入窗口级故障处理器：经本 App 投递的任务 panic / Draw panic
// 先在这里归因，再决定是否熔断。nil 时退回 Loop 全局兜底（现有行为）。
func (a *App) SetFaultHandler(fn func(err any, src FaultSource)) // 内部使用，Host 注入
```

单窗口入口 `gxapp.Open` / `OpenBackend` 保持不变——它们仍返回裸 `*App`，`App.Run()` 仍可独立跑。Host 是**叠加层**，不改旧语义。

---

## 5. 事件循环改造

### 5.1 桌面 GLFW（先落地）

`Host.Run()` 取代各窗口的 `glWindow.Run()`：

```go
for h.hasOpenWindows() {
    glfw.WaitEventsTimeout(0.02)   // Loop.Wake 用 PostEmptyEvent 即时打断
    h.loop.DrainTurn()             // 一个轮次：宏任务 + 微任务检查点
    for _, app := range h.snapshot() {
        w := app.win.(HostedWindow)
        if err := w.Poll(); err != nil    { h.fault(app, err, FaultEvent) }
        if err := w.Paint(); err != nil   { h.fault(app, err, FaultDraw) }
        if w.ShouldClose()                { _ = h.CloseWindow(app) }
    }
}
```

要点：
- `glfw.Init()` 由 Host 一次完成；`runtime.LockOSThread()` 仍锁主 goroutine。
- 每个 `OpenWindow` 调一次 `glfw.CreateWindow`，得到独立 `*glfw.Window`。
- **`glfw.Terminate()` 从 `glWindow.Close()` 移到 `Host.Close()`**（所有窗口归零后调一次）——这是多窗口不互相带崩的关键。
- `glWindow.Post()` 转发给 `host.Loop().Post`，而非自己 `loop.New()`。

### 5.2 Rust / winit（P2）

winit 一个 event loop 可创建多个 window（`ActiveEventLoop::create_window`）。当前 `WinitApp::resumed()` 里 `if self.state.is_some() { return }` 的单窗口假设改成 `HashMap<WindowId, Renderer>`；`gx_window_run` 从"每 handle 一个 loop"改成"每 Host 一个 loop、多 handle 注册到同一 loop"。

### 5.3 Web（P3，边界说明）

Web 复用浏览器事件循环，`loop.Queue` 只作队列（P3 决议不切 Loop）。"多窗口"在浏览器里是多 tab / iframe，各自一个 Go WASM 实例，进程内无法共享 Go 侧 Loop。本阶段 Web 维持单 App；Host 在 Web 退化为单窗口 façade。跨 tab 协调（BroadcastChannel）列为 P3+ 可选项，不承诺。

---

## 6. 故障隔离（一等约束）

### 6.1 三类 panic 入口与边界

| 入口 | 现有保护 | 缺口 | 方案 |
| --- | --- | --- | --- |
| `App.Dispatch` 后台回调（fetch/effect 异步） | Loop `exec` 有全局 `recover+OnTaskPanic` | panic 被全局吞掉，崩掉的窗口无人清理 | `bindUITaskPoster` 把 poster 包一层**窗口级 guard**：`defer recover → fault(app)`，熔断该窗口 |
| RAF 回调 | `capabilities.go:119-124` 每回调 `recover` | 只吞不归因 | recover 里调 `fault(app, r, FaultRAF)` |
| Draw / Present | `commit gate` defer unlock（不锁死门） | `drawNowErr` 无 recover，主循环里 panic 会带崩所有窗口 | `drawNowErr` 顶部 `defer recover → 转 error + fault(FaultDraw)` |
| 事件回调（GLFW→FireEvent） | 无 | callback 在 GLFW 线程，panic 带崩主循环 | 后端事件入口 / `HostedWindow.Poll` 边界 `recover → fault(FaultEvent)` |

### 6.2 熔断语义

`h.fault(app, err, src)` 做四件事：
1. 记录 `Fault{App, Err, Stack, Source}`（栈用 `debug.Stack()`，在 recover 现场取）。
2. 调 `WithFaultHandler` 注册的回调——应用层可弹错误页 / 写日志 / 决定重开。
3. **只关闭故障窗口**：`CloseWindow(app)` → 级联关它的 `Owner`（停 effect/computed）→ `Runtime.Close()` → `win.Close()`（Destroy 该窗口，但**不** Terminate 全局 GLFW）。
4. Host 主循环继续驱动其余窗口；最后窗口关闭时 `Host.Close()` 才 `Stop Loop` + `glfw.Terminate()`。

### 6.3 为什么不会污染其他窗口

- **Loop 不中断**：panic 在任务内部被窗口 guard recover，`DrainTurn` 继续取下一任务；未归因的框架 panic 由 Loop 自己的 `OnTaskPanic` 兜底，也 recover 不往外冒。
- **signal 状态自愈**：`withObserver` / `runWithOwner` / `computed` 的 `observers/ownerStack/computing/batch` 全部 defer 复位，已有 `TestEffectRecoversAfterPanic`、`TestComputedPanicPropagatesAndRecovers` 固化。窗口 A 的 effect panic 后，窗口 B 的 signal 读写不受影响。
- **资源隔离**：每个 App 独立 `commit gate`、独立 `closed/closeOnce`、独立 `Runtime`；关闭 A 不触碰 B 的投递器（`runtime_isolation_test.go` 已验证）。

### 6.4 明确兜不住的边界（诚实标注）

- 用户代码自起 goroutine 的 panic：跨 goroutine 无法 recover，Go 直接终止进程。提供 `host.Go(fn)` 受保护投递入口引导，但不能强制。
- 原生 segfault / GPU 崩溃 / OOM：需要进程级隔离（多进程 + IPC），超出本模型，另立议题。
- 死循环 / 内存爆炸：现有 `loop.Watchdog` 只**报告**不恢复；可作为 fault 触发器扩展，但第一阶段不自动强杀。

---

## 7. 响应式隔离语义

- 多窗口**共享一个** `signal.Runtime` 的执行上下文（observers/ownerStack 单实例，串行 drain）——符合 P0 "共享一个 UI 事件循环的多个窗口共用一个 Runtime" 的评审决议。
- 每个 App 仍保留独立 `signal.Runtime` 实例，用于**投递器 / 关闭状态隔离**（`app.Dispatch` 归因到各自窗口）；这不违背"核心单实例"，P0 已明确区分两者。
- `InstallGlobalPost` 的"最后一次为准"在多窗口下无害：所有 App 注入的是同一个 `host.Loop().Post`，poster 等价；租约计数保证只有最后一个 App 关闭才关默认 Runtime。

---

## 8. 分阶段实施

| 阶段 | 内容 | 验收 |
| --- | --- | --- |
| P0 | `Host` + Loop 单例化：`OpenWindow` 注入同一 `*loop.Queue`；窗口 guard + fault 归因 | 单测：两 App 共享 Loop，App A 的 Dispatch panic 只熔断 A，B 继续响应 |
| P1 | GLFW 后端 `HostedWindow` 化：CreateWindow 多次、`Host.Run()` 统一主循环、Terminate 移到 Host | xvfb 实测：开两窗，杀一窗另一窗仍可点击；关一窗不 Terminate |
| P2 | Rust/winit 对齐：一 event loop 多 window | winit 交叉编译 + 双窗口创建烟测 |
| P3 | Web 边界文档 + `host.Go` 受保护入口 + Watchdog→fault 可选联动 | 文档 + 测试 |

---

## 9. 测试矩阵（TDD）

1. `TestSharedLoopSingleInstance`：两 App 的 `Dispatch` 任务 FIFO 进同一队列。
2. `TestWindowPanicIsolated`：App A 的 Dispatch 回调 panic → A 被熔断关闭，B 的 signal 更新与 Dispatch 正常。
3. `TestDrawPanicFaultsWindowOnly`：`win.Draw` panic → 该窗口 fault + Close，其他窗口帧号继续增长。
4. `TestTerminateOnlyOnLastWindow`：关 A 不 Terminate，关最后一个才 Terminate（后端注入计数器断言）。
5. `TestSignalStateHealsAcrossFault`：A effect panic 后，B 的 computed 正常重算（复用/扩展 `review_fixes_test.go`）。
6. `TestHostedWindowFallback`：未实现 `HostedWindow` 的老后端走原 `Run()` 路径，行为不变。

---

## 10. 风险与取舍

- **改动面**：P1 要动所有示例里的 `glwindow.go`（playground / skia-style-demo / gallery 等）， 改动较大但模式统一。可先只在 playground 做样板，其余跟进。
- **Loop 任务归因**：需要在 `App.bindUITaskPoster` 里包 guard（最小侵入），不改 `loop.Queue` 核心；Loop 的 `OnTaskPanic` 保留为未归因兜底。
- **`glfw.Terminate` 时机**：这是最易出错点——必须确保 Host 是唯一调用方，且幂等；`glWindow.Close()` 里那行要删。
- **单窗口兼容**：`gxapp.Open` 不经过 Host，其 `App.Run()` 仍用老路径；需保证老的"全局 OnTaskPanic 兜底"仍在，panic 不会被新 guard 吞掉后无人知晓。
