# GoX 语言规范

GoX（`.gx`）= Go 语法 + 十三项语法糖（pub / enum / JSX / 外部符号还原 / 集合链式 / is 守卫 / 联合类型 / 模板字符串 / 参数默认值 / Lambda / 对象字面量 / 对象解构 / Signal 响应式，含 struct sig 字段）。转译产物是标准 Go 源码，交给官方工具链编译。**需要 Go 1.27+**。

## 书写约定（最重要）

> **标识符按 Go 真实拼写书写；全大写缩写（如 `URL`、`StatusOK`）直接写大写、原样保留。声明用 `pub` 显式导出。**

转译器对**小写形式**做机械还原（`upperFirst`），对**已大写的标识符原样保留**，全程不读取第三方包符号表、不做类型推导。因此用别人的 Go 代码时按真实拼写写即可（`http.NewRequest`、`r.URL`、`http.StatusOK`）；也兼容旧的小写写法（`newRequest`、`uRL`、`itoa`），由 `upperFirst` 还原。收益是零依赖、零歧义、行为完全可预测。

**大小写还原按 import 来源分流**（见 §4.1）：外部 Go 包维持上面的 `upperFirst` 兼容规则；本地 GX 模块则严格查被导入方的 `pub` 导出表，不做猜测——写错大小写时，GX 检查器直接报出「符号/字段大小写错误，应为 X」的语义诊断（含正确拼写与源码坐标），无需等 `go build` 的 `undefined`/`unexported`。产物始终原样保留书写形式，严格不改赌。

## 1. pub 可见性

- `pub` 作用于紧随的声明：`func` / `type` / `var` / `const` / `enum` / struct 字段 / interface 方法。
- 声明处首字母大写；**引用处作用域感知还原**：自己的 pub 符号引用同步大写，局部变量与私有符号保持小写。
- 已写大写的标识符不动（支持渐进迁移）。

```gx
pub func greet() {}   // func Greet() {}
func helper() {}      // func helper() {}
func main() { greet() } // func main() { Greet() }
```

## 2. enum 枚举

展开为具名类型 + const 组 + `String()` + `Parse<T>()`。

### 前缀式命名与可见性

**enum 的可见性决定它生成的全部符号的可见性**——不允许私有 enum 泄漏出公开常量
或公开解析函数这类「部分公开 API」（评审 P1）。常量 = 前缀 + `upperFirst(成员名)`：

| enum 声明 | 类型名（Go） | 常量前缀 | Parse |
|---|---|---|---|
| `pub enum color { red }` | `Color` | `Color` → `ColorRed` | `ParseColor` |
| `enum status { pending }` | `status`（私有） | `status` → `statusPending` | `parseStatus` |

即 `pub` 时常量/Parse 全程导出；非 `pub` 时类型、常量、Parse **全部私有**（小写）。
私有 enum 的引用在 GX 源码里也写小写全名（如 `statusPending`、`parseStatus(...)`）。

### 三种底层类型

```gx
pub enum color { red, green, blue }              // int，iota 0,1,2
enum code { ok = 200, fail = 500 }               // 显式值
pub enum method string { get = "GET", post = "POST" }  // 字符串枚举
pub enum perm { read = 1 << iota, write, exec }  // 位标志 1,2,4
```

- 整数枚举无值成员重复上一表达式并递增 `iota`；`String()` 返回成员简名，未知值返回数字字符串。
- 字符串枚举 `String()` 返回底层字符串；`Parse` 用显式值匹配。
- `Parse<T>()` 返回 `(T, bool)`。

### 三种拼写与使用

成员声明处写简名 `green`；使用处写完整名 `ColorGreen`、首字母小写 `colorGreen`（跨包），LSP 三种拼写命中同一符号。重命名时声明处写新简名，使用处自动派生新全名（`yellow` → `ColorYellow` / `colorYellow`），编辑范围按各自拼写宽度精确计算。

## 3. JSX

`<tag>` → `gxscene.Render(gxscene.Template{Ops:[…]}, getters...)`：静态结构编译期编成 opcode 模板并缓存，动态插值变成 getter 插槽；是普通 Go 表达式，不含 Diff（零整树重建）。

| JSX 形态 | 生成 |
|---|---|
| 属性 `class="x"` / `id={name}` | `gxscene.OpAttr Key:"class" Arg:"x"` / `gxscene.OpDynAttr Key:"id" Slot:n` + getter |
| 无值属性 `<input disabled/>` | `gxscene.OpAttr Key:"disabled" Arg:"true"` |
| 文本 / 表达式子节点 | `gxscene.OpChildText Arg:"…"` / `gxscene.OpDynChild Slot:n` + getter |
| `<br/>` 空元素 | `gxscene.OpElem Arg:"br"` + `gxscene.OpClose` |
| `<Foo/>` 组件 | `gxscene.OpComp` + `Foo(gxscene.Props{…})` |

#### `style={{…}}` 对象字面量（强类型结构体样式）

样式既可逐个写成散属性（`bg="#fff" font-size="12"`），也可收进一个 `style` 对象字面量——key 用驼峰，值为普通 GX 表达式（静态字面量或动态 getter），`bg` 语义写作 `background`。`gxscene.Style` 是**字段固定的强类型结构体**，不是 `map[string]any`：

```jsx
<div style={{
    background: lineGutterColor(idx),   // 动态值：随 signal 重新求值
    color: "#858585",
    width: 42,                           // 数字字面量 → StyleValue 字符串 "42"
    fontSize: 12,                        // 驼峰 → FontSize 字段
    textAlign: "right",                  // 驼峰 → TextAlign 字段
    padding: 4,
    hover: { bg: "#eee" },               // 伪状态用单词 key → Hover 字段
}}>
```

编译为单个 `gxscene.Style{...}` 结构体字面量、整块作为一个 `OpDynAttr Slot` 的 getter：

```go
func() any {
    return gxscene.Style{
        Background: lineGutterColor(idx), Color: "#858585", Width: "42",
        FontSize: "12", TextAlign: "right", Padding: "4",
        Hover: &gxscene.Style{Bg: "#eee"},
    }
}
```

- **强类型字段**：`gxscene.Style` 的字段集合固定（见 `runtime/scene/style.go`），标量统一 `StyleValue`（string 别名）。字段名拼错在 Go 编译期即报错，不再靠运行时校验；未知 key 由编译器报 `style-unknown-prop` 诊断。
- **key 映射**：驼峰 key 按 kebab→PascalCase 映射到导出字段（`fontSize`→`FontSize`、`textAlign`→`TextAlign`、`borderRadius`→`BorderRadius`）；数字字面量自动加引号成字符串。
- **动态值**：整块 getter 每次求值重建 Style，**运行时** `writeAttr` 展平为后端 props 后按 key 做细粒度 diff（只发变化的属性增量）；后端看到的仍是 kebab 属性名和 `&:hover` 伪状态串，协议不变。
- **伪状态**：用单词 key `hover` / `active` / `focus` / `disabled`（**源码不写 `&:` 符号**，避免对象 key 的特殊字符转译问题），映射到 `Hover` / `Active` / `Focus` / `Disabled *Style` 字段；值为对象字面量时递归。后端 props 里的 `&:hover` 由 `Props()` 生成，与源码写法无关。
- **JSON 写法**：带引号 key 的 JSON 对象字面量同样映射到强类型字段，但伪状态仍用单词 key（`style={{"bg":"#fff","hover":{"bg":"#eee"}}}`）；写 `&:hover` 会报 `style-unknown-prop`。`class` 仍不支持对象字面量。

**JSX 可跨行**：闭合标签后的换行若属于排版缩进（换行后紧跟宿主调用的 `)` / `]` / `,`，中间可夹行注释），编译器会吞掉这段空白，避免 Go 在参数列表换行处要求 trailing comma。因此下面两种写法等价，无需为规避语法错误而把 JSX 挤成单行：

```jsx
app.render(() =>
    <div>
        <span>hi</span>
    </div> // 尾注也会被一并吞掉
)
```

> 只有紧邻调用闭合符号的排版换行会被吞；后接标识符或 `}`（函数体/语句结束）的换行是真正的语句分隔，保留——不会把 `return` 与函数闭合挤到同一行。

#### `gox fmt`：JSX 格式化

`gox fmt <file.gx>|<dir>...` 格式化 GX 源码：默认输出到 stdout，`-w` 写回文件，`-check` 只检查是否已格式化（CI 用，需要格式化时返回非零）。目录参数递归收集其中的 `.gx`。API 是 `compiler.Format(src) string`。

- **范围**：Go 片段原样保留（编译产物由 gofmt 保证规范）；只重排 JSX 区域。格式化与编译器共用同一个 `jsxParser`，语义一致。
- **属性**：单行宽度（含缩进）超过 100 列时逐行排列，`>` / `/>` 独占一行；短属性列表保持单行。
- **子节点**：逐行缩进 4 空格；仅一个文本/插值子节点且宽度允许时保持单行；连续文本/插值（如 `Hello, {name}!`）合并为一行；排版用的纯空白文本丢弃（与 `normalizeText` 一致）。
- **条件分支**：`if cond { … } else if … { … } else { … }` 按 Go 风格缩进。
- **lambda**：`{(t string) => <span>…</span>}` 中 `=>` 后的 JSX 递归格式化。
- **语义保证**：属性/插值的表达式原文原样保留，文本折叠规则与编译器 `normalizeText` 相同；格式化是幂等的，前后编译产物的有效 token 序列一致。

运行时 `runtime/scene/`：`Template` / `Op` / `Render` / `Instance`。`Render` 缓存静态骨架、clone 出常驻场景树；`Instance.Update()` 只回写变化的插槽（可观测指标 `Writes`），不含调度。host 不定义绘制后端接口：GUI 后端实现 `gxapp.Window`，可用 `Node.ID`（整数稳定身份）/ `Changes()` 做增量绘制；内置 `TextBackend` 只是文本工具。

#### 事件 handler（`onClick` 等）

`onXxx` 属性（`onClick` / `onInput` / `onMouseEnter` …，无白名单）统一编译成 `gxscene.OpEvent`，**传入的就是你写的函数原值，编译器不加任何包装**。运行时派发按函数签名自动分派：`func()` 无参调用，`func(gxscene.EventContext)` 传入事件上下文：

```jsx
sig count = 0
func inc() { count = count + 1 }

<button onClick={inc}>具名函数 —— 编译后传入的就是 inc</button>
<button onClick={() => inc()}>箭头 → func() { inc() }</button>
<button onClick={() => count = count + 1}>箭头 + 赋值 → func() { count(count()+1) }</button>
<button onClick={func() { count = count + 1 }}>func 字面量，原样传入</button>
<button onClick={func(ctx gxscene.EventContext) { count = count + 1 }}>带 ctx 的完整签名</button>
```

- 编译器只把箭头 `=>` 展开成 Go func 字面量（JSX 属性路径不做 lambda 转换）；具名函数、`func` 字面量、显式 `gxscene.Handler(...)` 全部原样透传。**编译产物不含 `gxscene.Handler(...)` 包装**。
- 箭头可带事件参数：`(e) => ...` 展开成 `func(e gxscene.EventContext) {...}`，可读 `e.Synthetic`（归一化事件对象，含 `X`/`Y`/`Key`/`Mods` 等）。
- 函数体里的 sig 读写照常按普通变量写（`count = count + 1`），编译器补 `()`；跨线程更新用 `app.dispatch(() => {...})`（单 App 兼容写法 `Signal.safe`）。
- `gxscene.Handler` 是 `func(EventContext)` 的类型别名，老代码里显式写的 `gxscene.Handler(func(ctx ...){...})` 仍然合法、原样透传，保持兼容。

### 3.1 `ref` 模板引用（命令式逃生口）

`<tag ref={fn}/>` 里的 `fn` 是 `func(*gxscene.Element)`（或兼容形态 `func(any)`）。编译器编成 `gxscene.OpRef` 插槽：建树时该节点 ID 分配完毕后回调一次 `*Element` 句柄，实例销毁（重渲染 / 关闭 / 分支切换）时回调 `nil`。ref 是一次性挂载/卸载语义，**不进响应式重新绑定**——signal 变化不会重跑 ref。

`*Element` 是命令式句柄，不暴露裸 `NodeID`。按 GX 规范用小写书写方法，编译器经 `transformExternal` 还原为首字母大写的 Go 方法；类型名 `*gxscene.Element` 保持大写：

| GX 写法 | Go 方法 | 语义 |
|---|---|---|
| `el.focus()` | `Focus() bool` | 请求后端把焦点移到该节点；未实现焦点能力时返回 false |
| `el.boundingClientRect()` | `BoundingClientRect() gxscene.Rect` | 同步读最近一次布局矩形；无布局返回零矩形（宽高 0） |
| `el.measure(cb)` | `Measure(func(w, h float32))` | 登记异步读布局：下一帧绘制后由宿主回填 `cb`；无布局能力以 `(0,0)` 回填，不悬挂 |
| `el.getComputedStyle(pseudo?)` | `GetComputedStyle(...string) *gxscene.ComputedStyle` | 读计算样式快照，用 `style.getPropertyValue("bg")` 取值；可选伪状态参数读 hover/active 覆盖值 |

```jsx
import gxscene "github.com/robertpanvip/gox/runtime/scene"

func onMount(el *gxscene.Element) {
    el.focus()
    el.measure(func(w, h float32) { println(w, h) })
}

func view() any {
    return <input ref={onMount} placeholder="自动聚焦" />
}
```

后端能力是**可选契约**：实现 `gxapp.Focuser` 即获得 focus，实现 `gxapp.Measurer` 即获得布局读取；两者都不实现（含 headless）时行为零改动——Focus 为 no-op、Measure 回填零值。`App.Measure(id, cb)` 是等价的 App 级 API，供只知道 NodeID 的高级调用方登记异步读取。

内置两个后端均已接 `gxapp.Focuser`，且**不引入第二套焦点状态**，各自复用既有路径：

- **Rust 后端**：`Focus(id)` 把聚焦意图作为 `focus` 事件投进 `events → Dispatcher` 通道，与「点击输入框」走完全相同的闭环——应用在 `SetDispatcher` 里对 `"focus"` 事件调 `widgets.Focus(findNode(app, ev.NodeID))` 即可，node_id 就是 ref 句柄持有的那个 `NodeID.String()`。Focuser 本身不 import 组件库，分派权始终在应用层。
- **Skia 后端**：离屏光栅后端没有平台控件，`Focus(id)` 记录焦点目标，经 `FocusedID()` 查询或 `SetFocusHandler(fn)` 回调交给真实窗口集成层，由它把键盘焦点落到原生输入框。

> 所以 `el.focus()` 不是把焦点写进某个全局变量，而是把「让这个节点获得焦点」的意图交给后端；具体如何落到平台控件由后端决定，host 核心与组件库都不感知。

> 节点身份（`NodeID`）只在 host 与后端协议层流转，不通过 `ref` 暴露给业务代码；`Element` 是唯一对外的命令式句柄。

#### `el.getComputedStyle()`：计算样式快照

仿 Web `getComputedStyle`：`el.getComputedStyle()` 返回 `*gxscene.ComputedStyle`——一份 DOM 风格的只读样式快照，属性名即受限 CSS sheet 的白名单属性（`bg` / `color` / `padding` / `font-size` …），值是字符串。它不暴露 `NodeID` 或 `Props`，只用小写方法读数（编译器还原为大写）：

| GX 写法 | Go 方法 | 语义 |
|---|---|---|
| `style.getPropertyValue(name)` | `GetPropertyValue(string) string` | 取值；属性不存在返回空串，名字大小写不敏感 |
| `style.has(name)` | `Has(string) bool` | 该属性是否在快照中有值 |
| `style.length()` | `Length() int` | 快照属性个数 |
| `style.propertyNames()` | `PropertyNames() []string` | 所有属性名（小写、字典序） |

```jsx
func onMount(el *gxscene.Element) {
    let cs = el.getComputedStyle()
    let bg = cs.getPropertyValue("bg")          // "#3b82f6"
    let hoverBg = el.getComputedStyle("hover").getPropertyValue("bg") // 读 &:hover 覆盖值
}
```

- **快照语义**：每次调用生成一份独立快照，不随后续属性更新自动变化；要最新值再调一次。
- **伪状态参数**：`getComputedStyle("hover")` / `":hover"` / `"&:hover"` 等价，在基础属性之上叠加对应 `&:hover` / `&:active` 子表的同名覆盖（与 `Style.Flatten` 同语义），非冲突属性保留。缺省为空读基础样式。
- **两级数据源**：实现了可选契约 `gxapp.ComputedStyler` 的后端可提供**真正计算后**的值（含默认值、继承、命中伪状态、动画插值——绘制时实际采用的值），`getComputedStyle` 优先读它；后端返回 `nil` 表示放弃该节点/状态，回退到节点 `Props` 里已展平的声明样式。当前内置 Skia / Rust 后端**未实现**该契约，走 Props 回退——拿到的是声明样式而非含默认值的最终值，headless / 纯 scene 零改动。

```go
// 后端可选实现：返回 nil 表示放弃，framework 回退到 Props
type ComputedStyler interface {
    ComputedStyle(id gxscene.NodeID, state string) map[string]string
}
```

#### `el.getContext()`：命令式 canvas 绘制

对 `Kind="canvas"` 的节点（`<canvas .../>`），`el.getContext()` 返回一个**有状态的 2D 绘制上下文**，可以像 Web canvas 那样命令式绘制，不必手写 `canvas.Encode`。同一节点多次调用返回同一上下文，绘制状态连续。非 canvas 节点 / 已销毁句柄返回 nil。

| GX 写法 | Go 方法 | 语义 |
|---|---|---|
| `ctx.setFillStyle(c)` / `setStrokeStyle` / `setLineWidth` | `SetFillStyle` / `SetStrokeStyle` / `SetLineWidth` | 设置后续绘制的样式状态 |
| `ctx.fillRect(x,y,w,h)` / `strokeRect(...)` | `FillRect` / `StrokeRect` | 填充 / 描边矩形 |
| `ctx.clearRect(x,y,w,h)` | `ClearRect` | 清空整幅指令（协议无局部擦除原语，语义为整幅重置） |
| `ctx.beginPath()` / `moveTo` / `lineTo` / `closePath` / `arc` | `BeginPath` / `MoveTo` / `LineTo` / `ClosePath` / `Arc` | 累积路径（`arc` 用贝塞尔近似） |
| `ctx.stroke()` / `fill()` | `Stroke` / `Fill` | 用当前样式把路径提交为一条 `path` 指令 |

ctx 内部维护一份 `[]canvas.Op`；每次绘制后重新编码写回该节点的 `Props["canvas-ops"]`、记一条 `ChangeSetAttr` 增量、并请求重绘。后端照既有 `canvas-ops` 协议绘制——**wire 协议与两个后端都零改动**。这是立即模式：画了就在，直到被覆盖或 `clearRect`。

```jsx
func onMount(el *gxscene.Element) {
    ctx := el.getContext()
    ctx.setFillStyle("#e11d48")
    ctx.fillRect(10, 10, 80, 40)
    ctx.beginPath()
    ctx.moveTo(0, 0)
    ctx.lineTo(100, 50)
    ctx.stroke()
}

func view() any {
    return <canvas ref={onMount} width="200" height="100" />
}
```

> 注意：`width` / `height` 决定画板布局盒尺寸，指令坐标相对盒子左上角；不写宽高画布是 0×0，画不出东西（见 `runtime/canvas` 协议说明）。

#### `el.requestAnimationFrame(fn)`：下一帧

`el.requestAnimationFrame(fn)`（App 级等价：`app.RequestAnimationFrame(fn)`）登记一个**下一帧回调**，返回 `gxscene.RAFHandle` 取消句柄：它在下一帧绘制之前执行，回调里画 canvas / 写 signal 的结果当帧可见；回调里再次登记即形成动画循环。

- **取消**：`el.cancelAnimationFrame(handle)`（App 级 `app.CancelAnimationFrame(handle)`）取消一个尚未开始执行的回调。语义对齐浏览器：同批里取消还没轮到的回调有效；已开始执行的回调取消为 no-op；重复取消 / 未知句柄 / 句柄 0 静默忽略。实例 `Dispose`（重渲染 / 关闭 / 分支切换）会丢弃其全部待执行回调，不会在销毁后触发。
- 句柄是全局单调正整数（0 无效），跨重渲染的陈旧句柄不会误伤新树里的回调。
- 队列挂在共享 sink 上，因此在 `build` 期的 `ref` 回调里登记也能入队，不依赖能力注入时序。
- drain 期间的写回会抑制重绘请求（本帧会画出写回结果），帧末若队列非空则异步预约下一帧——避免同步派发器下动画循环重入自锁。
- `rafPending` 去抖，高频登记不会堆积帧；单个回调 panic 不影响整帧（与浏览器 rAF 语义一致）。

```jsx
func onMount(el *gxscene.Element) {
    var tick func()
    var handle gxscene.RAFHandle
    x := 0
    tick = func() {
        x++
        ctx := el.getContext()
        ctx.fillRect(float32(x), 0, 2, 2)
        if x < 120 {
            handle = el.requestAnimationFrame(tick)
        } else {
            el.cancelAnimationFrame(handle) // 循环自然收尾时清掉最后一个句柄
        }
    }
    handle = el.requestAnimationFrame(tick)
}
```

### 3.2 `<For>` 列表

`<For each={xs} key={(item) => ...}>{(item) => ...}</For>` 是 keyed 列表控制器（对齐 SolidJS `<For>`）。`each` 在读 sig 的响应式上下文求值，列表变化时做 keyed diff：同 key 节点复用（移动不重建），删除项销毁其 Owner（级联停止内部 effect / cleanup），重排只调顺序。

| 写法 | 语义 |
|---|---|
| `key={(item) => item.id}` | 用稳定键 diff：同 key 值变化只写 item sig，精确重跑依赖它的叶子，不重建子树、ID 稳定 |
| **省略 `key`** | `key = nil` → **用元素下标作 key**（仅适合纯追加 / 尾部删除；中间插入/删除会导致后续项按新下标重建）。这是最自然的写法，运行时以 `indexKey` 模式处理，不会出现"塌成一项"或死循环 |
| render 参数 `item` | 是元素值的 getter（`func() T`）：子树动态 getter 必须经它读值以登记依赖。`item` 逃逸到组件 props 等无法信号化的场景时，编译器回退值快照模式（同 key 值变化重建该项） |

> 注意（第十轮 P1-1）：早期实现在 `key=nil` 时把 nil 包成"恒返回 nil 的闭包"，导致 `indexKey` 判定失效、所有元素 key 坍缩、列表塌成最后一项；叠加 `BindReactive` 会每帧重建而死循环。现已修复——省略 `key` 是可用的常规写法。

> 已知限制（第十三轮 P3-E）：`each` 用**具名切片类型**（`type Rows []Row`，底层被提升为 `list.List[Row]`）时，元素类型推断会退成 `any`、each getter 的 `[]T` 返回类型与具名类型不兼容。绕过方式：用匿名切片 `[]Row`，或给 render 形参加显式类型注解。这是 each getter 返回类型生成的问题，非 keyed diff 语义问题，后续随类型系统一起处理。`each` 是 `map` / `string` 等非列表时会报 `for-not-list` 诊断（函数返回形态同样覆盖）。

## 4. 外部 Go 符号

按 Go 真实拼写书写即可：全大写标识符原样保留，小写形式由 selector 右侧的机械 `upperFirst` 还原。

| GX | Go | 说明 |
|---|---|---|
| `http.NewRequest(...)` | `http.NewRequest(...)` | 真实拼写，原样保留 |
| `r.URL` / `http.StatusOK` | `r.URL` / `http.StatusOK` | 全大写缩写，原样保留 |
| `http.newRequest(...)` | `http.NewRequest(...)` | 小写形式，机械还原（兼容旧写法） |
| `uRL` | `URL` | 小写形式，机械还原 |
| `fmt.sprintf(...)` | `fmt.Sprintf(...)` | 小写形式 |
| `req.statusCode` | `req.StatusCode` | 小写形式 |

作用域防误伤：转译器在 pub 转换后收集本文件所有小写声明符号（参数、局部变量、私有字段/方法）。selector 右侧若命中自己的 pub 符号用其导出名；命中私有符号保持小写；否则（外部字段/方法）`upperFirst`。裸标识符（内置函数等）一律不碰。

### 4.1 import 来源与大小写还原

`import` 的来源决定 selector 右侧与复合字面量字段如何还原大小写：

| 来源 | 判定 | selector / 字段还原 |
|---|---|---|
| **外部 Go 包** | 路径不是本地 `.gx` 包（或 `-prefer go`） | 小写形式机械 `upperFirst`，兼容旧写法 |
| **本地 GX 模块** | 路径命中项目内某组 `.gx`（模块路径 + 目录推导） | **严格查被导入方 `pub` 导出表**，不猜测 |

GX 来源规则：

- 顶层符号：`util.add(...)` → 查被导入方导出表 `add → Add` → `util.Add(...)`。
- 已用导出名：`util.Add(...)` 命中「已用导出名」，原样保留（支持渐进迁移）。
- **写错大小写**：`util.ADD(...)` 不在表中，但对导出表做大小写不敏感匹配能**唯一**定位到 `Add` → GX 检查器报 `import-case: 符号 "ADD" 大小写错误：包 … 导出的是 "Add"`（带源码坐标）。产物仍原样保留，不改赌。若存在多个大小写候选则不猜测、不诊断。
- struct 字段：被导入方 `pub type box struct { pub value int }` 提供限定名导出 `box.value → Value`；导入方 `util.box{value: 9}` → `util.Box{Value: 9}`。字段写错大小写 `util.box{VALUE: 9}` 同样按限定名大小写不敏感唯一匹配，报 `import-field-case: 字段 "VALUE" 大小写错误：类型 box 导出的是 "Value"`。
- 私有符号 / 真拼写错误：被导入方未 `pub` 或书写与任何导出名都不近似，不进大小写诊断，保持原样由 Go 可见性规则兜底。
- **检查器可见**：大小写诊断进入 GX 语义层 `Analysis`，`gox check -root`、`gox build`（项目模式）与 LSP 都能直接报出，无需等 `go build`。
- 歧义裁决：本地 `.gx` 与 Go 包同名时默认本地 GX 优先，`-prefer go` 切到 Go 来源（逃生开关）。

## 5. 集合链式调用

所有切片类型 `[]T` **全局提升**为 `list.List[T]`，获得链式方法（Go 1.27 泛型方法，Map/Reduce 可换型）：

```gx
nums := []int{1, 2, 3}
labels := nums.map(fn).filter(keep).reduce(0, add)
```

- `List[T]` 与 `[]T` 底层相同，双向可直接赋值/传参。
- **仅匹配空方括号 `[]` 紧跟类型**（切片类型独有形态），故不误伤：数组 `[3]T`、索引 `a[i]`、切片表达式 `a[1:2]`、`map[K]V`、`Foo[T]`。
- 只有实际发生提升时才注入 `<module>/runtime/list` import；`-module` 指定 module 路径。

## 6. 模板字符串

反引号内含 `${...}` 时为模板字符串，编译为字符串拼接：` `hi ${name}` ` → `"hi " + fmt.Sprint(name)`。静态段用 `strconv.Quote` 转义；插值内是完整表达式，递归词法分析后流经全部 transform；`fmt` import 由 ImportManager 统一托管（已导入则复用别名）。无插值的反引号串仍是 Go raw string。

## 7. 参数默认值

`func f(x int = 5)` 采用 TS 语义：调用点在缺失的**尾部参数**处补全默认值，`f()` → `f(5)`，而 `f(0)` 保留显式的零值。声明处默认值被剥离，Analyze 前完成（避免默认值被算进参数类型），调用点在 Analyze 后补全。仅顶层函数调用匹配，方法/selector 调用不补。

## 8. Lambda 表达式

`x => x+1` → `func(x int) int { return x+1 }`。类型来源优先级：参数显式标注 → 上下文函数类型 → 集合回调元素类型 → 表达式反推（数值→int、比较→bool、len→int、字符串→string）→ 绑定推断（body 是**本文件函数调用**取函数返回类型、是**已知类型参数的字段访问** `p.Field` 取字段声明类型）；推断不出降级 `any`。仅支持单表达式 body，多语句用原生 func 字面量。

跨包函数调用（如 `strconv.Itoa(x)`）、方法调用（`p.Get()`）等签名不在本文件 token 流中的形态仍保守降级 `any`——不猜类型，避免猜错生成非法 Go（与对象字面量"右侧推断不出一律放过"原则一致）。这类场景若需要精确返回类型，直接用原生 func 字面量 `func(x int) string { return strconv.Itoa(x) }`，或把 lambda 放进有显式签名的上下文（函数实参 / `var f func(int) string = ...`）。

## 9. 对象字面量

`{ name: "x", age: 1 }` → Go 匿名结构体字面量。无期望类型时从值反推字段类型（`"x"`→string、`1`→int、`3.5`→float64、`true`→bool、嵌套 `{...}`→嵌套 struct、否则 any），生成 `struct{ name string; age int }{...}`。有期望类型时（函数实参 / 变量声明 / 赋值 / return——即明确标注参数或返回类型的上下文）省略结构体类型名，用期望类型补全为 `Point{...}` 并校验字段：未知字段 / 缺失字段 / 类型不兼容均报带 GX 坐标的编译错误。无类型常量按 Go 可赋值规则放宽。支持嵌套（由内向外逐层补全）。期望类型为 `any` / `interface{}` 时视为无有效期望类型（不携带字段结构，`any{...}` 非法），按匿名结构体处理。

## 10. 对象解构

`{ name, age } := p` / `var { name, age } = p` → 逐字段生成 `name := p.name; age := p.age` / `var name = p.name; var age = p.age`。复用 `var`/`:=` 语法，不引入新关键字。

- 解构模式是**纯标识符列表**（逗号分隔、无冒号）；与对象字面量（`key: value`）在词法上严格区分，互不干扰。
- **类型检查**：右值类型可从本文件推断时（具名 struct 或匿名 struct 字面量），每个解构字段必须属于 `fields(T)`，否则报带 GX 坐标的编译错误。
- **构造后立刻解构**：构造字段集 S₁ 由对象字面量层校验 ⊆ `fields(T)`，解构字段集 S₂ 由本层校验 ⊆ `fields(T)`，故 `S₁ ∪ S₂ ⊆ fields(T)`。
- 右值类型未知（如外部包函数返回）→ 宽松生成 `x.field`，字段存在性交由 Go 编译器检查。
- 解构变量遵循 Go 的作用域与 `:=` 重声明规则（同作用域重复声明由 Go 判错）。

## 10.1 联合类型与 `is` 守卫（擦除语义边界）

联合类型 `int | string` 与类型守卫 `x is T` 是**语法便利层**，复用 Go 的 `any`
与类型断言，**不是一套独立的静态联合类型系统**。这一点需要明确边界，避免误当成
TypeScript 式的联合类型。

```gx
type Value = int | string          // → type Value = any（擦除为 any）
func show(v int | string) string { // → func show(v any) string
    if v is string { return v }    // → 类型断言，块内 v 收窄为 string
    return str(v)
}
```

- **擦除规则**：所有出现联合类型的位置（变量声明、函数参数/返回值、类型别名、
  struct 字段）都擦除为 `any`；`x is T` 改写为 Go 类型断言 / type switch。类型别名
  `type V = int|string` 生成 `type V = any`。`is` 守卫链由编译器的语义层（Analysis）
  做合法性检查：不在联合成员内的守卫报 `union-member`、冗余守卫报 `redundant-guard`、
  覆盖不全报 `non-exhaustive`、类型不匹配报 `type-mismatch`。
- **定位**：当前路线刻意保留 `any` 擦除、不自建 Union Runtime，实现成本低、复用 Go
  运行时。它是**语法便利层**，尚未演进为真正的静态类型系统。
- **静态检查边界（当前不做，需知晓）**：因为擦除为 `any`，以下约束被削弱或不具备——
  ① 不同联合类型之间的赋值兼容性不再检查（`type A=int|string` 与 `type B=int|bool`
  擦除后都是 `any`，可互相赋值）；② 函数调用可能接受联合声明之外的类型（底层是
  `any`）；③ 不经过 `is` 守卫时编译器不知道当前具体类型；④ 守卫的控制流分析只覆盖
  本文件可直接判定的形态。需要强约束的场景请用 Go 原生类型与 `interface`。

## 11. Signal 响应式

`sig` 声明源信号，`Signal.computed` 派生只读信号，`Signal.effect` 注册副作用；依赖变化时 effect 自动重跑。底层为函数式 sig（`Signal[T] = func(...T) T`，无参读、带参写）+ 依赖追踪运行时 `runtime/signal`。

```gx
sig count = 0
let doubled = Signal.computed(() => count * 2)   // → Signal[int] 派生信号
Signal.effect(() => println(count))               // → func(){ println(count()) }，count 变化时重跑
count = 5                                          // → count(5)，触发依赖失效
println(doubled)                                   // → println(doubled())，惰性重算得 10
```

- **声明**：`sig n = v` → `var n = Signal.New(v)`；`let n = v` → `n := v`（let 即短声明，无新语义）。
- **读写改写**：sig 变量的读（裸标识符）自动补 `()`；写改写 `n = v` → `n(v)`、`n += v` → `n(n() + v)`、`n++` → `n(n() + 1)`。

#### 线程模型：单线程响应式 + `Signal.safe`

GoX 的核心线程规则：**响应式系统是单线程的，业务任务可以是多线程的。** Go 负责并发执行网络请求、IO、计算；UI Runtime 负责单线程响应式更新（Signal 读写、Computed 重算、Effect 执行、场景树变更、帧提交）。因此 Signal 读热路径只有一次切片判空 + 一次 map 写入——没有 goroutine id、没有 `runtime.Stack()` 抓栈、没有全局锁、没有原子装箱。

`runtime/signal` 的 benchmark（`go test ./runtime/signal -run '^$' -bench . -benchmem`）前后对比：

| 基准 | 重构前（多 goroutine 锁模型） | 重构后（单线程） | 变化 |
|---|---|---|---|
| Signal 读（无观察者） | 2.0 ns/op | 1.6 ns/op | 接近裸变量读（1.2 ns） |
| Computed 读（缓存干净） | 13.8 ns/op | 2.8 ns/op | **约 5× 快**（去掉 goid 抓栈 + 锁快路径判定） |
| Signal 写（无订阅者） | 72 ns / 47 B / 5 alloc | 44 ns / 24 B / 2 alloc | 分配减半（去掉 boxedValue 原子装箱；余下是函数式 sig 的可变参数 slice） |

依赖追踪热路径（Effect 内读，含依赖登记）约 12 ns/op、0 分配。写信号成本随订阅者数量线性增长（这是依赖图传播本身，1 / 10 / 100 / 1000 订阅者约 0.2 / 1.2 / 11.7 / 115 µs）。

- **后台 goroutine**：不得直接读写响应式 Signal，必须把更新闭包投递给 UI Runtime。推荐入口是每个 App 独有的 `app.Dispatch(func(){ ... })`（GX: `app.dispatch(() => {...})`）——它绑定到该 App 的 `signal.Runtime`，多 App 互不覆盖、互不影响。`app.SetUIExecutor` 注入投递器；`app.Close` 只关闭本 App 的 Runtime，关闭后投递返回 `ErrUIRuntimeClosed` 且闭包不执行。
- **`Signal.safe` 兼容层**：编译器把 `Signal.safe` 映射为 `signal.Safe`（通用首字母大写规则）。它是「全局默认 Runtime」的语法糖，供单 App 场景兼容；多 App 请一律用 `app.Dispatch`。`Safe` / `Dispatch` 都是**异步投递**——调用后立即返回，不等待执行。
- **禁止静默降级（评审 P0）**：未注入 UI Runtime 时，`Safe` / `Dispatch` 返回 `ErrUIRuntimeNotInitialized`，闭包**不会**在调用方（可能是后台）goroutine 同步执行。需要同步语义的 headless / 测试必须显式 `signal.NewSyncRuntime()` 或 `signal.SetUIPost(signal.SyncPoster)`。第一版不提供同步等待的 `SafeSync`。
- **UI Runtime 内**（事件回调、effect、`Signal.effect` 等）：直接读写，或用 `Signal.batch(() => {...})` 合并一批写入——`Batch` 只负责合并、不做线程切换，一批写完统一调度 effect 并只提交一帧。去重按批次 epoch 打标，每个 Effect 同批最多入队一次（O(1) 去重）；`Batch` 内 panic 时已写入的值保留、已收集的 Effect 仍冲刷，随后 panic 原样向上传播。
- **职责分离**：`Dispatch`/`Safe` 只管线程切换（后台 → UI Runtime），`Batch` 只管合并更新。Owner 的创建 / Dispose / Cleanup / Mount 全部属于响应式生命周期，必须在 UI Runtime 线程执行；后台请用 `app.Dispatch` 包装 `Dispose`。`withObserver` 先注册 defer 再执行用户 Cleanup，保证 Cleanup panic 时观察者栈与 Owner 栈必定复位；Computed 内 panic 原样传播（不吞用户异常），内部状态复位后下次读取可正常重算。

```gx
// 后台任务：网络回包后更新 UI
go func() {
    let data = fetch()
    Signal.safe(() => { items = data })   // 投递到 UI Runtime，不等待
}()

// UI Runtime 内：批量更新，合并为一帧
Signal.batch(() => {
    count = count + 1
    name = "Pan"
})
```
- **作用域排除**：selector `obj.n`、复合字段键 `{n: ...}`、struct 字段、函数参数、已显式调用 `n()` 均不改写（注：struct 里显式声明的 `sig name T` 字段是另一项特性，见 §11.1）。
- **computed**：惰性求值 + 缓存，仅依赖变化时失效；链式派生正确传播。
- **effect**：立即执行一次，依赖变化时重跑；每次重跑动态重建依赖集合（条件分支依赖可增删）。
- **底层 API 禁止手写**：`Signal.New` / `Signal.Computed` / `Signal.Effect` / `Signal.NewWithEquals` 等首字母大写的运行时构造器是编译器内部实现细节，用户源码里直接调用会报编译错误。GX 只暴露语法糖：`sig n = v` 声明源信号、`Signal.computed(() => expr)` 派生、`Signal.effect(() => {...})` 副作用；迟早需要的自定义相等策略等仍由运行时保留，但不从语言层暴露。这样用户无需感知 sig 与普通变量的区别，也杜绝了手写值与编译器 binding 不一致引发的快照冻结 / 别名冲突 / 二次包装等一系列问题。小写语法糖（含作为函数值传递的 `Signal.effect`）由编译器在末尾还原成对应大写符号，是唯一合法入口。
- **`Signal` 是保留标识符**：作为占位包名的语义优先，勿用作变量名/类型名/字段名。`Signal.New` 形式的底层 API 调用会被拦截；若确需同名局部变量（如 `Signal := Holder{...}`），对其字段的访问（`Signal.New`）会按绑定表放行——判据是「`Signal` 是否解析为包名绑定」，不是纯词法匹配。
- **并发安全**：响应式核心（观察者栈、依赖图、Owner 栈、批处理）严格单线程，无锁、无 goroutine id。跨线程只经 `app.Dispatch` / `signal.Safe` 投递；`sync.Once`（句柄 Dispose）、任务队列、App Close 等并发控制点仍使用必要的同步原语。「响应式单线程」不等于「整个程序无需锁」。
- **读取性能**：依赖追踪只在 effect/computed 重算期间需要，而这类场景是少数；读热路径走**无锁快路径**——只做一次 `len(observers)` 切片判空：为空（无 observer 重算）就直接返回值，非空才登记依赖。没有 goroutine id 解析、没有锁、没有原子操作，实测读 ~1.6 ns（接近普通变量的 1.2 ns）；computed 缓存命中读 ~2.8 ns。响应式核心不跨线程，因此值不需要原子指针防撕裂——跨线程更新统一走 `app.Dispatch` / `Signal.safe` 投递到 UI Runtime 后再读写。
- **import**：`Signal` 占位包名由 ImportManager 统一解析为 `import Signal "<module>/runtime/signal"`；源码已用别名导入则复用。

**已知边界与绕法**（能跑但需按规则写，编译器不改写也不会自动报警的三处）：

- **sig 作实参传的是当前值**：裸标识符读一律补 `()`，所以 `show(n)` → `show(n())`，传进去的是当前值而非 getter，`show` 内部拿不到响应式。这是函数式 sig 的既定语义（读即解包），不是 bug。想传 getter 保鲜，用 `func() T { return n }` 包一层、或改用 §11.1 的 struct `sig` 字段 / 组件 `Props`（getter-map）传递。
- **props getter 只读**：`Get[T](props, "k")` 返回 `func() T`，只能读；`rows(append(rows(), x))` 带参写会编译错（`want ()`）。这是单向数据流的设计选择——组件内要改父状态，由父级额外传一个 `func(...)` 回调，而不是写 prop getter 本身。
- **sig 持复合值的「深写」不刷新、且已诊断化**：`sig s = S{n:1}; s.n = 2` 与 `sig rows = []Row{...}; rows[0].name = "b"` 会改写为 `s().n = 2` / `rows()[0].name = "b"`——值确实变了，但没有调用 sig 写函数、依赖它的 slot 不刷新。编译器对这两种形态报 `signal-deep-write` 诊断并给出修法：整体替换后重新赋值（`s(S{n:2})` / `rows(新切片)`）；只读（如插值里的 `rows()[0].name`）与普通非 sig 变量的字段赋值不受影响。

### 11.1 struct sig 字段（响应式 props）

struct 的字段可以声明为 `sig`，编译器生成一个 getter 字段（`Signal.Signal[T]`），并自动改写读写。这样持有该 struct 的地方（含组件/持有者函数）读到的是**实时值**而非构造时的一次性快照——值可由外部 sig 驱动保鲜。

> **sig 字段的核心语义规则（三条）**
>
> 1. **谁被修饰谁响应式**：`sig` 修饰哪个字段，就只有那个字段具备响应式读（`p.f` → `p.f()`）与响应式写（`p.f = v` → `p.f(v)`、`+=`/`++/--` 同理）语义。未修饰的普通字段没有任何响应式行为。
> 2. **语义沿嵌套访问链保留**：响应式语义沿 struct 嵌套访问链 `a.b.c` 逐段保留——链上每一个 `sig` 字段段都在读取处补 `()`（含中间段，如 `o.mid.b().d()` 中 `b`、`d` 各自解包；写入目标段只写不读，省略 `()`）。因此中间任意 `sig` 段值变化时，链尾读到的是最新值。
> 3. **不自动传染给其他字段**：响应式语义不沿链扩散到普通字段。链上的普通字段段、以及「同名但未被 `sig` 修饰」的字段都不补 `()`，按普通 Go 字段读取。判据始终是「该字段在其 receiver 类型上是否被声明为 `sig`」，不是按字段名猜测，也不是从某个 `sig` 祖先传播而来。
>
> 这三条共同保证：显式标注即响应、嵌套链上处处保鲜、未标注字段零副作用。
>
> **零值与复制语义（评审 P1，必须明确）**
>
> 4. **零值不可用，必须初始化**：`sig` 字段是函数类型（`Signal.Signal[T]`），Go 零值为
>    `nil`，直接读取会触发 nil function panic。因此 `sig` 字段**必须初始化**，不能依赖零值。
>    两条受支持的构造路径都由编译器自动包装并覆盖全部 sig 字段：对象字面量
>    `CardProps{name: v}` 与 JSX props（档位表，见下文）；因此经这两条路径得到的实例
>    不会有 nil sig 字段。**已知边界**：`var p CardProps` 这种纯零值声明不会自动初始化
>    sig 字段，随后读 `p.name` 会 panic——需用户保证在读取前完成构造赋值。不要用复杂的
>    自动初始化包装对象去掩盖这一点，保持「显式标注 + 显式构造」的语义清晰。
> 5. **struct 复制 = 字段值复制，sig 字段共享底层 Signal**：`a := CardProps{name: "A"};
>    b := a` 之后 `a.name` 与 `b.name` **共享同一个 Signal**——这与 Go 复制一个含指针的
>    struct 的直觉一致（函数闭包是多字段值，复制的是同一份引用）。改 `b.name` 会反映到
>    `a.name`。如需独立副本，用 `sig` 源语法糖重新构造字段。
> 6. **嵌套指针按普通 Go nil 语义**：`sig child *ChildProps` 为 nil 时访问 `props.child.name`
>    按普通 Go 指针规则处理（nil 解引用），Signal 只负责字段本身的依赖追踪，不隐藏 nil、
>    不在 `child` 换成另一个指针时自动重绑旧对象的依赖——这些交给普通 Go 代码显式管理。

```gx
type CardProps struct {
    sig name string    // 响应式字段
    age int               // 普通字段
}
func show(p CardProps) string { return p.name }

func main() {
    sig src = "x"
    p := CardProps{name: src, age: 3}  // → name: src（传 getter 本身，保鲜）
    println(show(p))                   // x
    src("changed")
    println(show(p))                   // changed —— 不是快照
    p.name = "direct"                  // → p.name("direct")
}
```

- **声明**：`sig name T` → `name Signal.Signal[T]`（函数字段，无参读/带参写）。
- **读**：`p.name` → `p.name()`；**写**：`p.name = v` → `p.name(v)`；复合写 `+=` 与 `++/--` 同样展开。
- **构造（六档，手写与 JSX 一致）**：sig 字段的值按来源分档处理。**档位表的单一事实来源是 `pkg/compiler/signalfields.go` 顶部注释**，此处仅摘要，改动以实现为准——
  
  | 档 | 形态 | 产物 |
  |---|---|---|
  | 1 透传 | `v` 本身是 sig（sig 变量/字段/链读） | 直通 getter 本身，零开销；链中间含 sig 段时走读写合一 lambda 保鲜 |
  | 2 字面量 | 纯常量（数字/字符串/bool/nil） | `Signal.New(v)` |
  | 3 构造 | 复合字面量（如 `Leaf{v:"a"}`） | `Signal.New(v)`，一次构造、可写、可响应 |
  | 4 派生 | 含 sig 读的表达式（如 `p.a + p.b`） | `Signal.Computed(func() T { return v })`，登记依赖、只读（带参写入显式 panic，不再静默丢弃） |
  | 5 裸变量 | 单个普通变量/字段（如 `v`、`q.title`） | 可写跟随 getter（内部为按变量共享锁的 `LockedGetterShared`）：读跟随、写落回变量；**不登记依赖，UI 不自动刷新** |
  | 6 其它 | 普通表达式（无 sig 读、非上述形态） | `Signal.New(v)` 快照 |
  
  **硬判据**：凡是生成出的 `func(...T) T` 忽略参数、不登记依赖的，都不是合法 sig。档 3 的复合字面量必须走 `Signal.New`（一次构造、可写），不能包成每次读重建的裸闭包——否则中间 sig 段变化时 UI 不刷新、写入静默丢失。档 5 用可写跟随 getter（内部 `LockedGetterShared`）保留"读跟随、写落回变量"：早期版本是裸闭包 `v=arg[0]`/`return v`，无锁读写捕获变量会造成数据竞争（并发读写同一字段 `-race` 实证）；加锁后经由该字段的读写安全，且同一被跟随变量的多个字段/多个实例共享同一把锁（按根标识符的声明位置生成 key），避免"各持一把锁彼此不互斥"的竞态。**边界 1（并发）**：并发安全只覆盖经由 getter 的访问——若代码在别处直接读写同一变量 `v`（与字段并发），需自行同步（编译器无法给用户的裸变量读写加锁）。**边界 2（响应式）**：档 5 不登记依赖，写入不会自动通知 UI 刷新；需要写自动通知依赖的完整响应式，请用 `sig` 源（档 1 透传）或 `Signal.New`（档 2/3）。**边界 3（性能）**：档 5 每次读都要加锁（约 14ns，真 sig 约 2ns），且不应登记依赖、不刷 UI——只用于"跟随外部普通变量"的场景，不要用在列表渲染的热路径；列表项请用 `sig` 源（档 1）。
  
  > 上述表格中的 `Signal.New` / `Signal.Computed` 等是实现内部产物，用户不应在 `.gx` 里手写底层大写 API（会被 `checkNoRawSignalAPI` 拦下）；源信号一律用 `sig n = v` 语法糖。
  
  手写 `CardProps{name: v}` 与 JSX `<Card name={v}/>` 共用 `buildSignalFieldValue`，走同一套六档语义。派生档（档 4）是惰性求值的（首次读字段时才计算）。
- **类型安全**：只有 receiver 类型已知、且该字段确为某 struct 的 sig 字段时才解包；普通字段、同名但非 sig 的字段、外部包符号都不受影响。JSX 插值里的 selector 补 `()` 也按 receiver 断言类型判定，不会因同文件别处有同名字段而误伤普通字段读取。
- **与局部 sig 共存**：两者独立，互不干扰；降级在绑定表构建之前完成，避免 `sig` 关键字被局部逻辑误判。

> Go 版本说明：需要 Go 1.27+ 只为 `runtime/list` 的泛型方法（集合链式特性）。不使用集合的 `.gx` 项目在更低版本的 Go 工具链上通常也能编译；工具链要求以官方发布为准。

## 12. sourcemap / LSP / 编译检查

- `.gx ↔ .go` 双向位置映射（最近邻），可 JSON 序列化。
- LSP：initialize / didOpen / didChange / definition / hover / completion / documentSymbol / rename，诊断经 sourcemap 回投到 `.gx`。
- 编译检查：真实 `go build`（可取消 context + LRU 缓存），诊断回投到 `.gx` 行号。支持注入 go.mod `replace` 以解析本地运行时；跨文件项目模式下，多份产物一起 `go build`，诊断按产物文件回投到**对应 `.gx`（含子目录包，如 `util/math.gx`）**的正确行列。
- **产物落点**：单文件模式按 `-o <file.go>` 落盘（省略时默认 `<源文件>.go`，如 `app.gx` → `app.gx.go`）；项目模式（`-root`）把每份 `.gx` 落成**同目录同名 `.go`**（如 `util/math.gx` → `util/math.go`），不读 `-o`。两种模式的产物命名规则不同，脚本化时注意区分。

## 13. 跨文件项目（GX 导入 GX）

单文件转译是默认路径（零回归）。要导入另一份 `.gx`，用项目模式：

```bash
gox build main.gx -root <项目根> [-prefer gx|go] [-check]
```

- `-root` 指定项目根（含 `go.mod`），扫描其下所有 `.gx`，按 `package` 分组为 GX 包；包的逻辑 import 路径 = `go.mod` 的 module 路径 + 目录相对路径。
- `-prefer` 裁决本地 `.gx` 与 Go 包同名冲突（`gx` 默认本地优先 / `go` 走 Go 来源）。
- 依赖图按 import 建立，拓扑序决定转译顺序（被依赖方先转译，拿到导出表后再转译导入方）；循环依赖报出完整 `.gx` 路径链。
- 每份 `.gx` 转译为同目录的 `.go`（如 `util/math.gx` → `util/math.go`），产物是标准 Go 包，交给 Go 工具链跨包类型检查与链接。
- `-check` 对所有产物一起 `go build`，类型错误回投到正确的 `.gx` 文件与行。
- **跨文件大小写错误由检查器直接报出**：`gox check main.gx -root <项目根>` 逐文件跑 GX 语义检查，符号/字段大小写错误报 `import-case` / `import-field-case`（含正确拼写与 `.gx` 坐标）；`gox build`（项目模式，无论是否带 `-check`）也会把这些诊断与生成结果一起输出。诊断不依赖 `go build`，产物严格保持书写原样。

示例（`go.mod`: `module example.com/proj`）：

```gx
// util/math.gx
package util
pub func add(a, b int) int { return a + b }
```
```gx
// main.gx
package main
import "example.com/proj/util"
func main() { println(util.add(1, 2)) }   // → util.Add(1, 2)
```

## 与 Go 的关系

- gx 标识符书写规则≠Go（大小写语义由 `pub` 与约定承担），但语法、类型系统、标准库、工具链完全复用。
- 生成代码是标准 `.go`，可与其他 Go 代码混编、提交、调试。
- 渐进迁移：已大写的 Go 标识符在 gx 中原样保留。
