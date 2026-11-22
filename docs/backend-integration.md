# 后端接入：host + 协议模式

本文说明本仓库（host）与外部 GUI 后端项目之间的边界，以及把后端拆到独立仓库后如何接入。结论先行：**依赖方向是单向的（后端 → host），host 不需要为任何具体后端改代码；能否跑通取决于 module 解析、注入方式和协议版本一致性，而不是 host 的改动。**

## 黄金规则：协议是唯一的耦合面

> **host 内部代码可以随便改；只要协议不动，后端项目一行都不用动。** 反过来，只有协议真的变了，后端才必须跟着实现。

把改动分成两类，边界一目了然：

| 改动类型 | 例子 | 后端是否要动 |
|---|---|---|
| **host 内部实现**（协议不变） | 编译器优化、opcode 增删内部指令、`Update()` 算法、`signal` 调度、内置 Headless/Text 实现、`App` 内部字段、新增不暴露的辅助 API | **不用动** |
| **协议变更**（契约面变了） | `Window` 接口增删方法或改签名、**已有** `Node` 字段语义变化或删除、`NodeType` 枚举变化、`Handler`/`EventContext` 签名变化、`WindowConfig` 字段语义变化 | **必须跟着实现** |
| **additive 字段/方法**（协议面扩展但向后兼容） | 新增 `Node.ID`/`Node.Layout`、合成事件、`Changes()` 等——只加不删、不改已有签名，旧后端忽略即可 | **不用动**（可选采用） |

**协议面**就是第 2 节列出的那几样：`gxapp.Window`、`gxapp.WindowConfig`、`gxscene.Node` / `NodeType`、事件约定（`Handlers` / `Handler` / `EventContext`）。它们是后端唯一依赖的东西，其余 host 代码对后端透明。host **不定义绘制后端接口**——曾有的 `gxscene.Backend interface{Paint(root) string}` 已删除（返回字符串只适合文本，对 Skia/wgpu 是误导）；`TextBackend` 降级为具体文本工具，不是要实现的协议。

其中 `SyntheticEvent` / `DispatchBubble` / `DispatchCapture` / `NormalizeEventName` / `Modifier`（§2.3）以及 `Node.ID` / `Node.Layout` / `Changes()`（§2.4）属于 **additive 扩展**：它们只新增类型、字段与方法，`Window`/`Handler` 的既有签名一个没动，因此归入"additive 字段/方法"一类 —— 后端不采用就完全无感。

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
│  - signal（响应式）       │   │  - 胶水入口 run(app)          │
└──────────────────────────┘   └───────────────────────────────┘
               ▲                          │
               └────── import 协议类型 ────┘
                    （单向：后端依赖 host）
```

**host 负责**：把 GX 编成 Go；维护 opcode 常量骨架与常驻场景树；signal 变化时做细粒度 `Update()` 并驱动一次重绘；定义节点形状与事件协议；管理 Owner / Cleanup / 生命周期。

**后端负责**：窗口创建与事件循环；遍历 `Node` 树做布局；命中测试；取 handler 派发事件；把场景树绘制成实际像素（Skia / wgpu / GLFW / 文本等）。**布局、命中、派发归后端**，host 不参与。

host 内置的 `HeadlessWindow` + `TextBackend` 只是参考实现/占位实现，用于测试和 CI，不是必须使用的后端。

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
}
```

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

**事件回路（重要）**：`Window` 接口没有专门的事件输入通道。约定是后端在 `Run()` 的事件循环里收到原生事件 → 自己做命中测试找到目标 `Node` → 调 `Handler()` / `Dispatch()` → handler 内写 signal → host 的 `bindReactive` 自动 `Update()` 并 `Draw()` 一帧。这条路径目前是**约定**，未固化为接口方法，接入方需按此实现。

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

> 这里没有引入任何新的**必做**契约，因此不触发"协议变更 → 后端必须跟进"：`Window` 接口没动、`Handler`/`EventContext` 的既有用法没变。新增的 `Node.ID`/`Node.Layout`（§2.4）与合成事件一样是 additive 字段，后端不读则无感。后端可以一直用 `Dispatch` + `Raw`，也可按需切到 `Synthetic` + `DispatchBubble` 或增量变更通道。

### 2.4 节点身份、布局挂载点与增量变更通道（全部 additive）

host **不定义绘制后端接口**（旧 `gxscene.Backend interface{Paint}` 已删除——返回字符串只适合文本，对 Skia/wgpu 是误导）。真实 GUI 后端实现 `Window`，直接吃 `*Node` 树。为让后端少做全量遍历，host 提供三样**可选**能力：

**① 稳定节点 ID** — `Node.ID string`
- host 按结构位置/key 派生，跨帧稳定：`r/0/1`（静态）、`r/[key]`（keyed 列表项）、`.../tN`（动态文本）。
- 用途：后端做命中缓存、焦点/hover 跟踪时按 ID 定位节点，不依赖易失效的 `*Node` 指针。**host 只生成、不解释**。

**② 布局挂载点** — `Node.Layout any`
- host 给后端存自己布局结果（rect/尺寸等）的空槽位，host 不解释/不填充/不计算。
- 用途：避免每个后端维护 `map[*Node]any` 旁路结构。不用则为 nil。

**③ 增量变更通道** — `Instance.Changes()/TakeChanges()`（App 同名转发）
```go
app.EnableChanges()                  // 打开记录（不调用则零开销、行为不变）
// ... signal 变化触发更新 ...
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
- **For 同 key 值变化**：编译器生成的 `<For>` 通常走 item signal（`ForGetterOfSignal`），值变化只写 item signal → 依赖它的叶子精确重跑 → 发 `SetText/SetAttr`，**不发** `ReplaceChildren`、项内 ID 不变。两类例外仍走销毁重建并发 `ReplaceChildren`：手写的旧 `NewFor`（值快照），以及编译器把 item 传入组件 props 时自动回退的 `ForGetterOf`（值快照，见 reactive-ui-roadmap 的组件边界限制）。后端两种都支持即可。

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

> 三样都不触碰命中/布局/绘制：ID 是身份、Layout 是空容器、Changes 是"什么变了"的数据描述。决策与实现仍 100% 归后端。

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

**边界澄清（本轮已落实）**：host **不做命中/布局/绘制**，也不定义绘制后端接口（旧 `Backend` 已删）。`Node.ID`/`Layout`/`Changes()` 只是给后端提供身份、存放位置与"什么变了"的数据，决策仍在后端。剩下的隐式点是事件回路——它属后端职责，靠 `Run()` 内的胶水层与本文约定即可，不需要在 host 侧加命中接口。

## 5. 一句话总结

方向正确、能跑通、依赖单向。**协议是唯一耦合面：host 内部随便改，协议不动后端零改动；只有协议变更才需后端跟进。** 把胶水层放进后端项目、把本文作为契约维护，即可在不动 host 的前提下稳定接入任意 GUI 后端。
