# 后端接入：host + 协议模式

本文说明本仓库（host）与外部 GUI 后端项目之间的边界，以及把后端拆到独立仓库后如何接入。结论先行：**依赖方向是单向的（后端 → host），host 不需要为任何具体后端改代码；能否跑通取决于 module 解析、注入方式和协议版本一致性，而不是 host 的改动。**

## 黄金规则：协议是唯一的耦合面

> **host 内部代码可以随便改；只要协议不动，后端项目一行都不用动。** 反过来，只有协议真的变了，后端才必须跟着实现。

把改动分成两类，边界一目了然：

| 改动类型 | 例子 | 后端是否要动 |
|---|---|---|
| **host 内部实现**（协议不变） | 编译器优化、opcode 增删内部指令、`Update()` 算法、`sig` 调度、内置 Headless/Text 实现、`App` 内部字段、新增不暴露的辅助 API | **不用动** |
| **协议变更**（契约面变了） | `Window` 接口增删方法或改签名、**已有** `Node` 字段语义变化或删除、`NodeType` 枚举变化、`Handler`/`EventContext` 签名变化、`WindowConfig` 字段语义变化 | **必须跟着实现** |
| **additive 字段/方法**（协议面扩展但向后兼容） | 新增 `Node.ID`（整数身份）/`Changes()`、合成事件 等——只加不删、不改已有签名，旧后端忽略即可 | **不用动**（可选采用） |

**协议面**就是第 2 节列出的那几样：`gxapp.Window`（窗口生命周期）、`gxapp.FramePresenter`（可选的场景提交）、`gxapp.UIExecutor`（可选的 UI 线程投递）、`gxapp.WindowConfig`、`gxscene.Node` / `NodeType`、事件约定（`Handlers` / `Handler` / `EventContext`）。它们是后端唯一依赖的东西，其余 host 代码对后端透明。

需澄清「host 不定义绘制后端接口」的准确含义（避免误导）：host **不定义具体的渲染器抽象**（如何实现布局/命中/绘制），也不替后端做这些事；它只定义**窗口生命周期**（`Window`）与**场景树提交协议**（`FramePresenter`/`Frame`）。曾有的 `gxscene.Backend interface{Paint(root) string}` 已删除（返回字符串只适合文本，对 Skia/wgpu 是误导）；`TextBackend` 降级为具体文本工具，不是要实现的协议。线程执行环境同理：host 不管理具体 GUI 线程，只通过 `UIExecutor` 要求后端把绘制回调投递到正确的执行环境。

其中 `SyntheticEvent` / `DispatchBubble` / `DispatchCapture` / `NormalizeEventName` / `Modifier`（§2.3）以及 `Node.ID` / `Changes()`（§2.4）属于 **additive 扩展**：它们只新增类型、字段与方法，`Window`/`Handler` 的既有签名一个没动，因此归入"additive 字段/方法"一类 —— 后端不采用就完全无感。

这条规则是拆库的价值所在：host 可以持续迭代渲染核心和编译器，后端只在协议升级时才需要跟进。因此**协议变更要当作 breaking change 对待**——尽量 additive（加可选字段/新方法给默认实现），避免改动已有签名，并在 host 侧 bump 版本、后端侧同步升级。

## 1. 分层与职责

```
┌─────────────────────────────────────────────────────────────┐
│ 业务项目（.gx → Go）                                         │
│   gxapp.openWith(cfg, backend.newWindow(...))                │
│   app.render(() => <App/>)                                   │
└──────────────┬──────────────────────────┬────────────────────┘
               │ import                   │ import
               ▼                          ▼
┌──────────────────────────┐   ┌───────────────────────────────┐
│ host：github.com/.../gox │   │ 外部后端项目                   │
│  - compiler（GX → Go）   │◀──│  - 实现 gxapp.Window          │
│  - gxscene（场景树/协议） │   │  - 读 Node.ID / Layout（可选） │
│  - gxapp（窗口接口/调度） │   │  - 布局 / 命中 / 事件派发      │
│  - sig（响应式）       │   │  - 胶水入口 run(app)          │
└──────────────────────────┘   └───────────────────────────────┘
               ▲                          │
               └────── import 协议类型 ────┘
                    （单向：后端依赖 host）
```

**host 负责**：把 GX 编成 Go；维护 opcode 常量骨架与常驻场景树；sig 变化时做细粒度 `Update()` 并驱动一次重绘；定义节点形状与事件协议；管理 Owner / Cleanup / 生命周期。

**后端负责**：窗口创建与事件循环；遍历 `Node` 树做布局；命中测试；取 handler 派发事件；把场景树绘制成实际像素（Skia / wgpu / GLFW / 文本等）。**布局、命中、派发归后端**，host 不参与。

host 内置的 `HeadlessWindow` + `TextBackend` 只是参考实现/占位实现，用于测试和 CI，不是必须使用的后端。

### 1.1 线程、提交与生命周期契约（Host 架构评审 P0）

这一节明确三个所有权问题——它们是 Host 接口稳定的前提：

**① 谁保证 Node 更新与 Backend 读取不竞争？——Host 的串行提交门。**

- Signal 可在任意 goroutine 写入；响应式 effect 的调度由 `runtime/signal` 负责。
- 但**所有 Scene Mutation**（effect body 里的 `SetPart`/子树 graft、`App.Update()`）与 **Backend Draw**（`Window.Draw` / `FramePresenter.Present` 遍历 Node）都在 Host 的 `commitGate` 内串行执行，二者绝不并发。`commitGate` 基于 goroutine id 可重入——effect body 内同步写 signal 触发的嵌套 effect 不会自锁。
- 结论：**后端遍历 `*Node` 时，Node 树一定处于一次完整 Mutation 提交后的稳定状态**，不会有 effect 并发写入。后端无需自己加锁保护读树。
- 派发器负责把 Draw 请求投递到正确的执行环境，`commitGate` 负责 Node 读写的互斥。两者正交、缺一不可（只调度 Draw 而不串行 Mutation 仍然会竞争）。

**谁保证 UI 操作发生在正确的 OS Thread？——后端，不是 Host。** Host 不猜测线程、也不比较 goroutine id（goroutine 可在 OS Thread 间迁移，goid 相等不能证明线程亲和）。真实 GUI 后端实现 `UIExecutor.Post`，用它自己的事件循环（`postMessage` / `glfw.PostEmptyEvent` / 平台 run loop 等）把绘制回调投递到正确线程；需要锁定 OS Thread 的框架由后端自己负责。Host 侧用 `app.SetUIExecutor(exec)` 或 `NewUIExecutorDispatcher(exec)` 接入。旧的 `QueueDispatcher`（goroutine id + post 函数）保留仅作源码兼容，已标注弃用；`post == nil` 不再静默同线程降级，而是安全失败（不绘制），避免在不正确的线程调用后端。

**② 更新的所有权——Frame 提交。**

- Host 负责把响应式变化提交成 `Frame{Root, Changes, Version}`（见 §2.4），通过 `FramePresenter.Present` 主动推给后端。增量提交是 Host 的职责，后端不反向访问 App。
- Draw 与 Update 等所有绘制入口统一经过 `FrameDispatcher`（`InlineDispatcher` 同步 / `UIExecutorDispatcher` 投递 UI 线程），不存在绕过派发器的绘制路径。需要本次同步绘制的确定错误时用 `DrawNow()`（同步执行并返回本次错误）；`Draw()` 返回最近一次绘制的错误，异步派发下不保证属于本次请求。

**③ Node 与 App 的所有权——生命周期。**

- Node 由 Host 创建/销毁：clone 骨架时分配整数 ID，节点移出树（`RemoveChild`）后其 ID 不再复用；后端按 ID 维护的缓存应在收到 `RemoveChild` 时主动释放。
- `App` 生命周期独立于原生窗口：`gxapp.Open` 建外壳 → `Render` 在根 Owner 下建树 → `Run` 进事件循环 → `Close` 级联销毁 Owner/Cleanup、关闭派发器与窗口。
- `Close` 用 `sync.Once` + 原子关闭标记实现幂等且并发安全：并发的 `Run`/`Draw`/`Update` 能立即观察到关闭状态并 no-op。`Render` 失败会统一清理本次创建的 Owner，不残留响应式资源。

**④ 事件边界（重申）**：后端负责原生事件采集、命中测试、坐标转换、焦点/Hover 状态；Host 只提供事件对象、冒泡/捕获、停止传播等通用机制。Host 不承担命中/布局/焦点管理。

## 2. 协议清单（host 暴露给后端的全部契约）

所有类型都在 host 的 runtime 包中导出，后端 import 后即可使用。

### 2.1 窗口接口 `gxapp.Window`

```go
type Window interface {
    Open(cfg WindowConfig) error       // 按配置创建/打开窗口
    Draw(root *gxscene.Node) error     // 绘制一帧：拿到场景树根，布局+绘制
    Run() error                        // 进入事件循环，直到窗口关闭
    Close() error                      // 关闭并释放资源
    Config() WindowConfig              // 返回实际生效配置
}
```

`WindowConfig` 字段：`Title / Width / Height / X / Y / Resizable / Fullscreen / Backend`。零值尺寸由 `normalize()` 补默认值（800×600）；`Backend` 是后端提示串（`"headless"` / `"skia"` / `"wgpu"` …），`Open` 忽略它，`OpenWith` 由调用方按此挑选实现。

### 2.2 场景树节点 `gxscene.Node`

```go
type Node struct {
    Type     NodeType           // Text / Div / Fragment
    Kind     string             // 元素标签名；内部节点为 "#text"/"#root"/"#frag"
    Props    map[string]any     // 属性（事件不在其中）
    Handlers map[string]any     // 事件处理器：key=JSX 原样事件名，value=handler 原值
    Children []*Node
    Text     string             // 仅 NodeText 使用
    ID       NodeID             // host 分配的稳定整数身份；0 表示尚未分配
}
```

`ID`（`NodeID` 为整数类型）是节点的稳定身份（详见 §2.4）：clone 骨架时分配，节点移出
树后不再复用。后端可用它作命中/焦点/布局缓存的 map key，也可完全忽略——它只是身份
标识，不携带布局语义。后端私有状态（布局结果等）不放在 Node 上，用 `map[NodeID]any`
自行维护。

`Type` 三态：

| Type | 含义 | 后端处理 |
|---|---|---|
| `NodeText` | 文本节点 | 读 `Text` 画文字 |
| `NodeDiv` | 元素节点 | 读 `Kind` 作标签、`Props` 作属性，递归 `Children` |
| `NodeFragment` | 内部透明包装（`#root`/`#frag`，含条件分支/keyed 列表） | **不画**，透传 `Children` |

后端通过 `Window.Draw(root *gxscene.Node)` 拿到整棵树，字段全部导出，可自由遍历。

### 2.3 事件协议

- JSX 里任何 `on*`（如 `onClick`、`onMouseEnter`，含后端自定义的 `onFoo`）都**无条件透传**进 `Node.Handlers`，key 为**原样事件名**（`"onClick"`）。前端不设白名单、不校验、不拦截。
- `StandardEvents`（`click`/`mouseEnter`/…，**驼峰值**）**仅供 IDE 提示和文档**，不是白名单，表外事件一样透传。
- `Handler func(ctx EventContext)` 是**推荐签名**，非强制。`EventContext` 的 `Raw` 由后端填平台原生事件对象，前端完全不碰。
- 派发两种方式：
  - `node.Handler(name) any`：取原值，后端按自己约定的签名调用（自定义事件走这条）。
  - `node.Dispatch(name, EventContext) bool`：若 handler 恰为推荐签名 `Handler` 则调用并返回 `true`，否则 `false`（后端改用自定义签名处理）。**只在命中节点本地派发，不冒泡。**

**事件回路（重要）**：`Window` 接口没有专门的事件输入通道。约定是后端在 `Run()` 的事件循环里收到原生事件 → 自己做命中测试找到目标 `Node` → 调 `Handler()` / `Dispatch()` → handler 内写 sig → host 的 `bindReactive` 自动 `Update()` 并 `Draw()` 一帧。这条路径目前是**约定**，未固化为接口方法，接入方需按此实现。

#### 统一合成事件与冒泡 / 捕获（additive，可选）

host 额外提供一套**跨后端一致的合成事件 + 冒泡/捕获派发**，解决"`Raw` 形状由后端决定、业务 handler 写 `raw.(xxxEvent)` 就被绑死"的问题。这部分是**纯增量**：不填 `Synthetic` 就完全退回上面的裸透传，老后端零改动；命中测试、布局、"先捕获还是先冒泡"的调度仍归后端。

- `gxscene.SyntheticEvent`：host 定义的统一事件对象，字段有 `Type`(规范化事件类型) / `NativeName` / `Target` / `CurrentTarget` / `X`,`Y` / `Button` / `Key` / `Mods` / `Raw`(原生对象不丢) / `Data`(后端扩展位)。方法 `StopPropagation()` / `PreventDefault()` 及其 `Is*` 查询。
- `gxscene.EventContext` 多一个可选字段 `Synthetic *SyntheticEvent`（`Raw` 保留）。后端把原生事件映射进来即可；业务 handler 从此读统一字段，换后端不改业务代码。
- `gxscene.NormalizeEventName("onMouseEnter") → "mouseEnter"`（去 `on` + 首字母小写、**保留内部驼峰**，与 `EvMouseEnter` 等常量同值可直接 `==`；`Event.Is` 做大小写不敏感比较）。保留驼峰是硬要求：自定义事件 `onCustomThing → customThing`，名字不丢，否则等于变相拦截。表外事件同样适用——不是白名单。`NewSyntheticEvent(name, target, raw)` 便捷构造。
- `Modifier` 位掩码（`ModShift`/`ModCtrl`/`ModAlt`/`ModMeta` + `Has`），统一各平台修饰键。
- 两个可选派发方法（后端按需调用，替代/补充 `Dispatch`）：
  - `node.DispatchBubble(name, ctx)`：从命中节点沿 `parent` 链向根冒泡，逐节点更新 `CurrentTarget` 并调 handler；某 handler 调 `StopPropagation()` 即停止。
  - `node.DispatchCapture(name, ctx)`：自根向命中节点捕获派发，`StopPropagation` 语义相同。
  - 两者都自动补 `Synthetic`（若后端没填），`Target` 固定为命中节点；自定义签名 handler 不被拦截，仍可经 `Handler(name)` 取原值。

> 这里没有引入任何新的**必做**契约，因此不触发"协议变更 → 后端必须跟进"：`Window` 接口没动、`Handler`/`EventContext` 的既有用法没变。新增的 `Node.ID`/`Changes()`（§2.4）与合成事件一样是 additive 字段，后端不读则无感。后端可以一直用 `Dispatch` + `Raw`，也可按需切到 `Synthetic` + `DispatchBubble` 或增量变更通道。

### 2.4 节点身份、布局缓存与增量变更通道（全部 additive）

host **不定义绘制后端接口**（旧 `gxscene.Backend interface{Paint}` 已删除——返回字符串只适合文本，对 Skia/wgpu 是误导）。真实 GUI 后端实现 `Window`，直接吃 `*Node` 树。为让后端少做全量遍历，host 提供三样**可选**能力：

**① 稳定节点身份** — `Node.ID`（类型 `gxscene.NodeID`，底层 `uint64`）
- host 在节点创建时分配**全局唯一、不复用**的整数身份；身份绑定到逻辑节点对象，而非结构位置。因此在相邻位置插入兄弟节点不会改变其他节点的 ID（解决了字符串结构路径 ID 的"插入即漂移"问题）。
- 稳定性规则：静态节点在实例生命周期内固定；keyed 列表项按 key 复用 ID（同 key 重排/增删身份不变，重复 key 唯一化）；条件分支各实例独立；动态文本对应固定 part；真正重建才分配新 ID。
- 用途：后端做命中缓存、焦点/hover 跟踪时按 ID 定位节点（整数是高效 map key），不依赖易失效的 `*Node` 指针。**host 只生成、不解释**。
- `NodeID.String()` 给出十进制字符串，供 `Change` 字段与二进制/JSON 协议编码；`0`（`NoNode`）表示"无节点/末尾锚点"（对应协议的空字符串）。
- **Key 类型安全**：For 的 key 用 `map[any]` 管理，Go 按动态类型+值区分，整数 `1` 与字符串 `"1"` 不会碰撞（消除了 `fmt.Sprint(key)` 的类型混淆）。

**② 布局缓存归后端所有** — `map[NodeID]any`
- Node **不承载后端私有状态**（Host 架构评审 P1）：旧的 `Node.Layout any` 挂载点已移除。布局结果（rect/尺寸等）由后端用自己的 `map[NodeID]any` 维护，整数 ID 作 key。
- 所有权与销毁规则完全归后端：节点销毁（后端收到 `RemoveChild`）时按 ID 清理对应缓存。host 不解释、不填充、不计算布局。
- 这样避免 Node 退化成"什么都能塞"的公共对象，也消除多后端共享 Node 时的类型冲突与生命周期不明。

**③ Frame 提交契约（推荐）+ 增量变更通道**

Host 把每帧的完整提交打包成 `gxapp.Frame`，通过可选的 `gxapp.FramePresenter` 接口主动推给后端——后端**无需反向持有 App**：
```go
type Frame struct {
    Root     *gxscene.Node      // 当前完整场景树（建/重建全量镜像）
    Changes  []gxscene.Change   // 自上次成功 Present 以来的有序增量（FramePresenter 自动启用）
    Version  uint64             // Host 提交序号，单调递增（去重/排序/判漏帧；非成功序号）
    FullSync bool               // true：忽略 Changes，必须用 Root 全量重建镜像
}
type FramePresenter interface { Present(frame *Frame) error }
```
后端实现 `Present` 即自动生效（App.drawNow 检测到后优先调用，否则回退 `Draw(root)` 兼容路径，老后端零改动；FramePresenter 模式由 Host 自动打开变更记录，无需调用 `EnableChanges`）：
```go
func (w *Backend) Present(frame *gxapp.Frame) error {
    // 首帧、FullSync 或本地镜像丢失时，用 Root 建全量基线。
    if !w.hasBase || frame.FullSync {
        w.buildMirror(frame.Root)
        w.hasBase = true
        w.appliedVersion = frame.Version
        return nil
    }
    for _, ch := range frame.Changes { w.apply(ch) } // 常规增量
    w.appliedVersion = frame.Version
    return nil
}
```
Rust 后端（`backend/rust/go/window.go`）已采用此契约，移除了旧的 `BindApp` 反向引用。

底层增量流仍可独立使用（不实现 FramePresenter 时，或需要自行控制取变化时）：
```go
app.EnableChanges()                  // 打开记录（不调用则零开销、行为不变）
// ... sig 变化触发更新 ...
for _, ch := range app.TakeChanges() { // 取走自上帧以来的有序变更
    switch ch.Kind {
    case gxscene.ChangeSetAttr:    /* ch.NodeID / ch.Key / ch.Value */
    case gxscene.ChangeSetText:    /* ch.Value 为新文本 */
    case gxscene.ChangeSetHandler: /* 事件换绑 */
    case gxscene.ChangeInsertBefore: /* ch.NodeID 下，把 ch.Value(*Node) 插到 ch.BeforeID 之前；BeforeID="" 即追加末尾 */
    case gxscene.ChangeMoveChild:    /* 把 ch.ChildID 子节点移到 ch.BeforeID 之前 */
    case gxscene.ChangeRemoveChild:  /* 移除 ch.ChildID 子节点 */
    case gxscene.ChangeReplaceChildren: /* 删 ch.NodeID 下全部旧 children，重读实时树 */
    }
}
```

**全量与补丁的关系**：`Frame.Root` 永远是最新完整快照，`Frame.Changes` 是两次**成功** Present 之间的差量。后端正常路径只需 apply Changes；当 `FullSync=true`、本地镜像丢失、Version 不连续或遇到无法处理的 Change 时，用 Root 重建全量基线。首帧（Version=1）Changes 可能为空，必须用 Root 建基线。

**Version 与失败恢复契约**（Host 评审 P0/文档统一）：`Version` 是 **Host 提交序号**，每次提交尝试都递增，不保证对应一次成功提交——例如 Version 1 成功、Version 2 的 `Present` 返回错误、Version 3 成功，后端会看到 1 和 3 但不经历 2。判断连续性不要只看 Version，后端应用成功后自行记录 `AppliedVersion`。`Present` 失败时 Host **不消费** Changes 缓冲（避免永久丢帧），并在下一帧置 `FullSync=true`：后端收到后必须忽略 Changes、用 Root 全量重建，从而恢复到一致状态。因此后端恢复策略应是：

1. 成功 apply 后更新本地 `AppliedVersion = frame.Version`；
2. 遇到 `FullSync=true` 无条件全量重建；
3. 发现 `frame.Version` 与 `AppliedVersion+1` 不连续（中间帧失败），等待 `FullSync` 帧或用 Root 重建，而非猜测缺失的补丁。

`Frame.Root` 消费契约：Root 是可变指针，仅在 `Present` 调用期间稳定；后端必须在 `Present` 返回前完成读取，需要异步延迟时必须自行复制。Host 不复制整棵树。`FramePresenter` 自动启用增量记录这一行为，对老 Draw 兼容路径无影响（未实现 `Present` 的后端仍按零开销全量 Draw）。

**`ChangeKind` 全表与字段语义**（`BeforeID` 为空表示末尾）：

| Kind | NodeID | ChildID | BeforeID | Value | 触发场景 |
|------|--------|---------|----------|-------|----------|
| `SetAttr` | 属性所在节点 | — | — | 新值 | 动态属性变化 |
| `SetText` | 文本节点 | — | — | 新文本 | 标量/文本插值变化 |
| `SetHandler` | 事件所在节点 | — | — | handler | 事件换绑 |
| `InsertBefore` | 父节点 | 新子节点 ID | 锚点 ID（`""`=末尾） | 完整 `*Node` 子树 | 列表新增、动态 children 插入、追加末尾 |
| `MoveChild` | 父节点 | 被移动节点 ID | 锚点 ID（`""`=末尾） | — | keyed 列表重排（节点复用，只调顺序） |
| `RemoveChild` | 父节点 | 被移除节点 ID | — | 被移除 `*Node` | 列表删项、children 移除 |
| `ReplaceChildren` | 目标节点自身（常是 `#frag` 透明容器，见下方说明） | — | — | — | 整块兜底：文本↔容器互转、旧值快照模式 `NewFor` 的同 key 重建（新 `ForGetterOfSignal` 同 key 值变化改发 `SetText`） |

> 协议表只列**实际会发出**的指令。追加末尾用 `InsertBefore(BeforeID="")`，节点销毁用 `RemoveChild`（后端自行释放该 ID 的缓存）——曾有 `AppendChild`/`Destroy` 两个常量但实现从不发出，已从协议面移除，避免后端按表实现时漏更新。

**`ReplaceChildren` 的消费语义（易错）**：它的 `NodeID` 指向**目标节点自身**，通常是列表项的 `#frag` 透明包装节点（如 `i1/[1]`），不是它的父容器。后端若不物化 `#frag`、只认可见节点，会找不到目标。收到后必须：① 删除该节点下**所有**旧 children（按当前镜像树，指令不带旧列表）；② 从全量 `Draw(root)` 的对应节点重读新的 children 子树并建立映射。仅"重读 children"而不删旧的，会残留旧节点。此指令不带 `Value`，新内容需回读实时树。

**首帧契约（重要）**：`EnableChanges()` 之前产生的变更（含首帧 `Render` 与接线期 effect 的首次执行）**不会记录**，`TakeChanges()` 返回空。因此后端**必须在首帧做一次全量 `Draw(root)`** 建立镜像树，之后才用增量补丁跟进。增量通道默认是关闭的（未 `EnableChanges()` 时零分配、零开销）。

**逐节点增删/移动是真实的，不是占位符**：`For` 的 keyed diff 在增删/重排时会精确发出 `InsertBefore`/`RemoveChild`/`MoveChild`；动态 children 变化会先对旧/新子节点做基于 `*Node` 指针身份的 diff，只发变化的部分。为把移动次数降到最少，复用节点按旧索引求最长递增子序列（LIS），LIS 上的节点保持原位，其余发 `MoveChild`；指令从右往左生成、`BeforeID` 始终指向右侧已就位的节点，后端顺序 apply 即得目标顺序。

- 变更用 `Node.ID` 定位（`ChildID`/`BeforeID` 同为跨帧稳定 ID）；整棵实例树（组件/分支/列表项）共享同一 sink，一次 `TakeChanges` 取全。**嵌套场景同样覆盖**：组件嵌在 For 列表项或 Branch 分支内时（`For.buildEntry` / `Branch.Resolve` 先建子树后接 sink），运行时用 `adoptSinkDeep` 递归把项内/分支内已挂载的组件与更深层控制器改指到共享 sink，因此 `<li><Card text={u.name}/></li>` 这类写法里组件子树的 `SetText` 也会出现在根 `TakeChanges()` 中（曾遗漏，第六轮修复）。
- 这是 opcode 常驻树 + 细粒度 `Update()` 的自然产物——`Update` 本来就算出"哪些插槽变了"，这里只是暴露出来，**不像被移除的 vdom 后端那样每次重建整树**；细粒度 diff 只作用于单个动态插槽的 children，不产生 `createElement`、不重建整棵树。
- 后端要么**完全不消费**增量、每次全量 `Draw(root)`（零改动），要么**完整消费**上面全部指令做增量重绘。二者择一即可；**不能只实现其中一条**：例如新增列表项只发 `InsertBefore`、不发 `ReplaceChildren`，只实现后者的后端会永远不显示新增项。`ReplaceChildren` 只是少数场景（文本↔容器互转、旧值快照模式的同 key 重建）的兜底指令，不是"一条通用替代"。
- **For 同 key 值变化**：编译器生成的 `<For>` 通常走 item sig（`ForGetterOfSignal`），值变化只写 item sig → 依赖它的叶子精确重跑 → 发 `SetText/SetAttr`，**不发** `ReplaceChildren`、项内 ID 不变。两类例外仍走销毁重建并发 `ReplaceChildren`：手写的旧 `NewFor`（值快照），以及编译器把 item 传入组件 props 时自动回退的 `ForGetterOf`（值快照，见 reactive-ui-roadmap 的组件边界限制）。后端两种都支持即可。

**diff 策略可替换**：host 内建的 LIS diff 只是默认实现。若后端已维护自己的镜像树、或有更契合自身渲染模型的高效 diff，可用 `SetChildDiffer` 注入自己的算法直接产出协议 `Change`，覆盖内建——host 不再跑那一遍 LIS：

```go
app.SetChildDiffer(func(parent *gxscene.Node, old, next []*gxscene.Node, emit func(gxscene.Change)) {
    // 用后端自己的 diff 算出增删/移动，通过 emit 产出 Change
    emit(gxscene.Change{Kind: gxscene.ChangeReplaceChildren, NodeID: parent.ID})
})
app.SetChildDiffer(nil) // 恢复 host 内建
```

- 策略作用于整棵共享 sink（组件/分支/列表项全覆盖），任意时刻可设；不设则行为不变。
- 注入只替换"如何算 diff"，`Change` 协议与字段不变；策略只描述结构变化，命中/布局/绘制仍归后端。

> 三样都不触碰命中/布局/绘制：ID 是身份、Changes 是"什么变了"的数据描述、布局缓存归后端自建 map[NodeID]any。决策与实现仍 100% 归后端。

## 3. 拆成两个项目如何跑通

假设后端项目为 `github.com/yourorg/gox-desktop`，module 独立。

### 3.1 module 解析

业务项目的 `go.mod` 同时 require host 与后端：

```
require (
    github.com/robertpanvip/gox vX.Y.Z
    github.com/yourorg/gox-desktop vA.B.C
)
```

本地联调用 `replace` 或 `go.work` 指向两仓库本地路径。编译器注入的 import 路径来自 `-module` 参数（默认 `github.com/robertpanvip/gox`），生成的 Go 只依赖 host runtime，不含后端路径——后端由业务代码自己 import。

> 环境前提：所有参与方都需要 **Go 1.27+**（泛型方法）。

### 3.2 后端项目实现接口

```go
package desktop

import (
    gxapp "github.com/robertpanvip/gox/runtime/app"
    gxscene "github.com/robertpanvip/gox/runtime/scene"
)

type skiaWindow struct{ /* 平台句柄 */ }

func NewWindow() gxapp.Window { return &skiaWindow{} }

func (w *skiaWindow) Open(cfg gxapp.WindowConfig) error { /* 创建窗口 */; return nil }
func (w *skiaWindow) Draw(root *gxscene.Node) error     { /* 布局+绘制 */; return nil }
func (w *skiaWindow) Run() error                        { /* 事件循环+命中+派发 */; return nil }
func (w *skiaWindow) Close() error                      { return nil }
func (w *skiaWindow) Config() gxapp.WindowConfig        { return gxapp.WindowConfig{} }
```

### 3.3 GX 侧接入（不改 host）

源码按 GX 约定写（外部符号小写，编译器还原大写）：

```gx
import gxapp "github.com/robertpanvip/gox/runtime/app"
import desktop "github.com/yourorg/gox-desktop"

app, err := gxapp.openWith({ title: "Demo", width: 800 }, desktop.newWindow())
if err != nil { panic(err) }
defer app.close()
app.render(() => <div id="app">...</div>)
app.run()
```

`openWith` 的签名从外部 Go AST 读取（非特判），第二参数类型为 `gxapp.Window`，后端返回的实例满足该接口即可。

### 3.4 胶水层：把接入面收窄到一处

建议在后端项目里再包一个入口，业务侧只 import 它，避免到处写 `openWith`：

```go
// 后端项目 desktop 包
func Run(cfg gxapp.WindowConfig, build func() any) error {
    app, err := gxapp.OpenWith(cfg, NewWindow())
    if err != nil { return err }
    defer app.Close()
    if err := app.Render(build); err != nil { return err }
    return app.Run()
}
```

这样"不改 host 代码"成立，且协议细节集中在后端项目维护。

## 4. 已知风险与建议

| 风险 | 说明 | 建议 |
|---|---|---|
| **协议靠代码隐式约定** | 没有 IDL / 独立规范，后端必须 import host 包才能拿到 `Window`/`Node`/`Handler` | 用胶水层隔离；把本文作为接入契约随仓库维护 |
| **协议变更触发后端跟进** | 只有协议面（`Node`/`Window`/事件签名）改了后端才需改；host 内部实现改动对后端透明。两仓库独立发布时协议版本易错配 | 后端 pin 明确的 host 版本；host 把协议变更当 breaking change（优先 additive），改协议时同步发版 |
| **事件回路隐式** | `Window` 无事件输入方法，命中→派发全靠约定 | 后端在 `Run()` 内集中实现这条回路，并写清命中策略 |
| **增量变更可选导致两套路径** | 后端可选消费 `Changes()` 或继续全量 `Draw`；混用需注意幂等 | 一个后端固定一种策略；消费增量的后端每帧 `TakeChanges()` 一次，配合 `Node.ID` 定位 |

**边界澄清（本轮已落实）**：host **不做命中/布局/绘制**，也不定义绘制后端接口（旧 `Backend` 已删）。`Node.ID`（整数稳定身份）/`Frame{Changes,Version}` 只是给后端提供身份与"什么变了"的数据，布局缓存归后端自建 `map[NodeID]any`，决策仍在后端。剩下的隐式点是事件回路——它属后端职责，靠 `Run()` 内的胶水层与本文约定即可，不需要在 host 侧加命中接口。线程与生命周期契约见 §1.1。

## 5. 一句话总结

方向正确、能跑通、依赖单向。**协议是唯一耦合面：host 内部随便改，协议不动后端零改动；只有协议变更才需后端跟进。** 把胶水层放进后端项目、把本文作为契约维护，即可在不动 host 的前提下稳定接入任意 GUI 后端。
