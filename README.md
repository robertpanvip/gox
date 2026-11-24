# GoX（GX）— 编译到 Go 的薄语法层语言

`gox`（源码后缀 `.gx`）是一门**以 Go 为基底**的语言：所有标准 Go 语法原样可用，在此之上增加语法糖。转译器把 `.gx` 降级为**等价、可读、可 go build 的标准 Go 源码**，并产出双向 sourcemap。**需要 Go 1.27+**（集合方法使用泛型方法）。

> 设计原则：**零运行时魔法**。JSX 编译成 opcode 模板 + getter（常驻场景树），枚举只是具名类型 + const + String/Parse，集合只是切片类型提升 + 泛型方法。生成代码可直接提交、阅读、调试。
>
> **书写约定核心：声明用 `pub` 显式导出；标识符按 Go 真实拼写书写，全大写缩写（如 `URL`）直接写大写、原样保留。** 小写形式兼容旧写法、机械还原，无需类型推导、无需读取第三方包符号表。

## 特性速览

| 特性 | GX 写法 | 生成的 Go |
|---|---|---|
| **pub 导出** | `pub func greet(){}` | `func Greet(){}`（引用处作用域感知还原） |
| **枚举** | `pub enum color { red, green }` | `type Color int` + `const(ColorRed…)` + `String()`/`ParseColor()` |
| **JSX** | `<div class="box">hi</div>` | `gxscene.Render(gxscene.Template{Ops:[…]})` opcode 模板 + getter |
| **外部符号** | `http.NewRequest`、`URL` | 原样保留；小写形式 `http.newRequest`、`newRequest` 机械还原 |
| **集合链式** | `xs.map(f).filter(g)` | `list.List[T].Map(f).Filter(g)`，切片 `[]T` 全局提升 |
| **is 类型守卫** | `if (x is float64) { … }` | `if v, ok := x.(float64); ok { x := v; … }`（块内 x 窄化为 float64） |
| **联合类型** | `func f(v int \| string)` | `func f(v any)`（擦除为 any，配合 is 守卫窄化） |
| **参数默认值** | `func f(x int = 5)` | 调用点补全：`f()` → `f(5)`（TS 语义，区分未传与零值） |
| **Lambda** | `x => x+1` | `func(x int) int { return x+1 }`（类型从上下文/表达式推断） |
| **模板字符串** | `` `hi ${name}` `` | `"hi " + fmt.Sprint(name)`（拼接，自动 import fmt） |
| **对象字面量** | `{ name: "x", age: 1 }` | `struct{ name string; age int }{...}`，有期望类型时用 `point{...}` |
| **对象解构** | `{ name, age } := p` / `var { name, age } = p` | `name := p.name; age := p.age`（逐字段，校验字段存在） |
| **Signal 响应式** | `sig n = 0` / `Signal.effect(() => ...)` | `Signal.New(0)` + 函数式读写 + 依赖追踪自动重跑 |
| **条件分支** | `if cond { <A/> } else { <B/> }`（JSX 内） | `gxscene.OpBranch` + `BranchGetter`，切换时销毁旧 Owner、只重建该区域 |
| **Keyed 列表** | `<For each={xs} key={...}>{(x) => <Item/>}</For>` | `gxscene.OpFor` + `ForGetterOfSignal[T]`，元素类型从 `each` 自动推断（可显式注解），按 key 复用节点、每元素独立 Owner；item sig 使同 key 值变化只更新叶子、不重建 |
| **应用入口** | `gxapp.open(cfg)` + `app.render(() => <App/>)` | `gxapp.Open(cfg)` 返回 App + `App.Render(func() any{...})`；open 只收配置，render 收建树函数（SolidJS 风格）并在内部建根 Owner + sig 自动重绘（源码小写，编译器还原大写） |

> 响应式 UI 的 Owner / Cleanup / 条件分支 / Keyed For / 结构变化重绘等机制，详见
> [响应式 UI 路线图落地说明](docs/reactive-ui-roadmap.md)。

> **语言状态与兼容性**：GX 目前是 v0，语法与 API 仍可能调整。近期一处源码级不兼容变更——
> 底层运行时 API 不再允许手写：`Signal.New` / `Signal.Computed` / `Signal.Effect` 等大写构造器
> 改为编译错误，统一走语法糖（`sig n = v` / `Signal.computed(...)` / `Signal.effect(...)`）；
> 相应地 `view.BindReactive(signal.Effect)` 的手写调用改为小写 `Signal.effect` 函数值传递。
> `Signal` 是保留标识符（占位包名语义优先），勿用作变量名。详见
> [语言规范 §11](docs/language-spec.md#11-响应式-sig) 与 §11.1。

## 1. 可见性：`pub`

Go 用首字母大小写决定导出；gx 用小写书写 + `pub` 显式声明公开。作用域感知保证：自己的 pub 符号在**声明处与引用处**都大写，局部变量与私有符号保持小写。

```gx
pub func greet() {}          // → func Greet() {}
func helper() {}             // → func helper() {}   （私有）
pub type point struct {
    x int
    y int
}
pub const max_size = 10      // → const Max_size = 10
func main() { greet() }      // → func main() { Greet() }
```

## 2. 枚举：`enum`

展开为**具名类型 + const 组 + `String()` + `Parse<T>()`**；常量采用**前缀式命名**（`Color` + `red` → `ColorRed`），与 `http.MethodGet` 一致。

```gx
pub enum color { red, green, blue }        // 整数：iota 0,1,2
enum status { pending = 1, done = 3 }      // 显式值（私有类型 status）
pub enum method string { get = "GET" }     // 字符串枚举
pub enum perm { read = 1 << iota, write }  // 位标志 1,2
```

使用处写完整名 `ColorRed`、或首字母小写 `colorRed`（跨包）、或声明处简名 `green`——LSP 三种拼写都能跳转到同一符号。

## 3. JSX

JSX 统一编译成 **opcode 模板 + getter**：`<tag>` 变成 `gxscene.Template{Ops:[…]}`，静态结构编译期只解析一次并缓存成骨架；每个 `{expr}` / `attr={expr}` 是一个 `func() any` getter 插槽。运行时 [`runtime/scene/`](runtime/scene/) 提供 `Render` / `Instance` / `Update`（零 Diff，只回写变化的插槽）。

```gx
pub func render(name string) any {
    return <div class="card" id={name}>hello {name}<span>world</span></div>
}
```

| JSX 形态 | 生成 |
|---|---|
| `<div class="x">` | `gxscene.OpElem Arg:"div"` + `gxscene.OpAttr Key:"class" Arg:"x"` |
| `<input disabled/>` | `gxscene.OpAttr Key:"disabled" Arg:"true"` |
| `{name}` 表达式子节点 | `gxscene.OpDynChild` + getter `func() any{ return name }` |
| `<Foo/>` 组件 | `gxscene.OpComp` + `Foo(gxscene.Props{…})`（不加引号） |

借鉴 lit-html 的「静态模板 + 动态 part」：`gxscene.Render` 按 opcode 结构缓存静态骨架，clone 得到常驻场景树；数据变化后调 `Update()` 只回写变化节点（可观测指标 `Instance.Writes`）。**host 不定义绘制后端接口**——真实 GUI 后端（Skia / wgpu / GLFW）实现 `gxapp.Window` 直接吃 `*Node` 树自行布局/绘制；host 只通过 `Node.ID`（整数稳定身份）、`Changes()`（增量变更流）提供可选能力，命中/布局/绘制全归后端（布局缓存由后端自建 `map[NodeID]any`，不放在 Node 上）。详见[后端接入契约](docs/backend-integration.md)。

#### 使用指南

一个完整例子，覆盖**组件定义 → JSX 组合 → 数据更新 → 约束**（可直接对照 `examples/jsxscene.gx`）：

```gx
// 构建：gox build demo.gxpackage main

import (
    gxscene "github.com/robertpanvip/gox/runtime/scene"
    "fmt"
)

// 【约束 1】复合组件签名必须是 func(gxscene.Props) *gxscene.Instance。
// 【约束 2】组件只执行一次——函数体是「建树」而非「每次渲染」，别在这里放依赖刷新的副作用。
pub func Card(props gxscene.Props) *gxscene.Instance {
    // 【约束 3】动态属性用 Get[T]()() 读：先取 getter，再在 JSX 里调用 title()。
    //   title 是 func() string，不是 string；写成 string 就变成了不会更新的快照。
    title := gxscene.Get[string](props, "title")
    n     := gxscene.Get[int](props, "n")
    return <div class="card"><h1>{title()}</h1><span>{n()}</span></div>
}

func main() {
    title := "GX"
    count := 1

    // 【约束 4】大写/带点标签 = 复合组件；其余 = 原生标签。
    // title={title} 编成 getter（捕获变量本身），不是把 "GX" 拷贝进去。
    view := <div id="app"><Card title={title} n={count}/></div>

    fmt.Println(view)   // 首次渲染：挂载常驻场景树

    // ——— 数据更新 ———
    title = "Gox"       // 改外部变量
    count = 2
    view.Update()       // 触发更新：递归进 Card，只写变化的节点（此处 2 个）
    fmt.Println(view)
}
```

##### 数据怎么更新

scene 后端**没有 Diff、没有重建**。更新 = 重新求值 getter + 只把变化的插槽写回节点。三种更新途径：

| 方式 | 用法 | 适用 |
|---|---|---|
| **闭包 getter** | `title={title}` 编成 `func() any{return title}`；改变量后调 `view.Update()` | 外部普通变量；手动驱动 |
| **sig 自动接线（推荐）** | `view.BindReactive(signal.Effect)` 一行接线；之后 `count(5)` 写 sig 即自动精确更新，无需 `Update()` | 响应式 UI；sig 沿 Props 传到叶子，叶子在插槽里读 |
| **命令式 `SetPart`** | `view.SetPart(slot, value)` 按插槽下标直接写，绕过 getter | 事件回调里精确改一个值；需知道 slot 下标，较少用 |

`Update()` 返回本次**实际写入的节点数**，值没变则写 0（`equalValue` 按类型严格比较）。观测指标：`Instance.Writes`（累计写入，各 `Instance` 独立，不跨子树累加）、`Instance.Updates`（Update 调用次数）。

##### sig 自动接线（底层 Instance 用法）

> 若走**应用入口** `gxapp.open(cfg)` + `app.render(() => <App/>)`，下面这一步已由 `render` 内部完成，无需手写。本节仅针对直接使用 `gxscene.Instance`（不经 App）的场景。

scene 核心**不依赖**任何响应式库——`BindReactive(effect)` 接受一个形如 `signal.Effect` 的 effect 原语注入。它为每个插槽建一个 effect：普通插槽读 getter 后直接 `SetPart`（sig 变化只精确命中依赖它的插槽，不遍历全树）；复合组件插槽则递归进子组件接线（组件不重建）。sig 从根经 Props getter 一路传到叶子、在叶子的动态插槽里读取，写入即自动刷新。

完整可运行示例见 [`examples/jsxscene-signal.gx`](examples/jsxscene-signal.gx)：

```gx
// gox build demo.gximport (
    gxscene "github.com/robertpanvip/gox/runtime/scene"
    "github.com/robertpanvip/gox/runtime/signal"
)

func main() {
    sig count = 0
    let doubled = Signal.computed(() => count * 2)

    // JSX 插值 {count} / {doubled} 由编译器编成 getter 并自动补读调用：
    //   func() any { return count() } / { return doubled() }
    view := <div id="app"><span>{count}</span><b>{doubled}</b></div>

    view.BindReactive(signal.Effect)  // 一行接线

    count = 5   // 自动更新：count→"5"、doubled→"10"，无需 view.Update()
}
```

> **编译器细节**：`sig` 语法糖在管线早期消除声明并改写读写，但 JSX 插值当时是整体 token、信号改写穿透不到。scene 后端重新词法化插值时会拿到同一个 sig 变量集合，补上读调用——因此 `{count}` 正确编成 `count()` 而非把 sig 函数对象当值（且按 JSX 位置解析绑定，被局部变量遮蔽的同名 sig 不会被误改写）。

验证见 [`runtime/scene/reactive_test.go`](runtime/scene/reactive_test.go)（运行时：sig 经两层组件 Props 传到叶子、写入自动刷新、组件不重建、写同值零冗余写入）与 `pkg/compiler/jsxscene_test.go`（编译器：插值/属性/组件 Props 里的 sig 读补全）。

##### 强类型组件 Props（省略类型名 + 字段校验）

复合组件有两种契约形态，由组件函数签名自动判定：

| 形态 | 组件签名 | JSX 属性编译为 | 语义 |
|---|---|---|---|
| **强类型 struct**（推荐要类型安全时） | `func Card(props CardProps)`，`type CardProps struct{...}` | `Card(CardProps{title:"x", n:42})` | 属性是**快照值**，编译期校验字段名/类型/缺失 |
| **getter-map**（要保鲜/响应式时） | `func Card(props gxscene.Props)` | `Card(gxscene.Props{"title": func() any{...}})` | 属性是 getter，值变化随 `Update()`/`BindReactive` 刷新 |

强类型形态复用了[对象字面量](#11-对象字面量)的「省略类型名 + 期望类型补全 + 校验」机制：JSX 编译期生成裸对象字面量 `Card({title:"x", n:42})`，transpiler 从 `func Card(props CardProps)` 签名推断期望类型 `CardProps`，补全为 `Card(CardProps{...})`，并校验——**字段拼错、类型不符、缺字段都会编译报错（带 GX 坐标）**。

```gx
// gox build demo.gximport gxscene "github.com/robertpanvip/gox/runtime/scene"

type CardProps struct {
    title string
    n     int
}
func Card(props CardProps) *gxscene.Instance {
    return <div><h1>{props.title}</h1><span>{props.n}</span></div>  // 直接强类型字段访问
}
func main() {
    view := <Card title="Gox" n={42}/>   // → Card(CardProps{title:"Gox", n:42})
    // <Card title={123} n={42}/> → error: 字段 "title" 类型不匹配：期望 string，实际 int
    // <Card title="Gox"/>         → error: 缺少字段 "n"（类型 CardProps）
}
```

> **取舍**：强类型属性是快照值（组件只执行一次、字段是普通值），写变量后**不会**自动刷新——要响应式请用 getter-map + `BindReactive`。两种形态可在同一文件混用，按组件各自签名判定。完整示例见 [`examples/jsxscene-typed.gx`](examples/jsxscene-typed.gx)。

```go
view := gxscene.Render(tpl, getters...) // 返回 *Instance，常驻场景树
fmt.Println(view)                       // Instance.String() 用 TextBackend 绘制
n := view.Update()                      // 更新，返回写入节点数
// 只需渲染一次、不更新时：gxscene.RenderText(tpl, getters...) 直接返回 string
```

##### 约束清单

| # | 约束 | 说明 |
|---|---|---|
| 1 | 组件签名二选一 | `func(gxscene.Props) *gxscene.Instance`（getter-map，保鲜）或 `func(T) *gxscene.Instance`（T 为本文件具名 struct，强类型快照）；返回类型不匹配则不是合法组件 |
| 2 | 组件只执行一次 | 组件体是建树逻辑；不要把「每次刷新要做的事」放这里，放到 getter 里 |
| 3 | getter-map 形态动态数据必须是 getter | 属性传变量（`title={title}`）而非快照字面量；组件体用 `Get[T]()()` 读。`Get` 断言失败返回零值，不 panic |
| 3b | 强类型形态字段编译期校验 | 属性名必须存在于 struct、值类型须兼容、所有字段须提供；否则编译报错（带 GX 坐标）。属性是快照值，不随变量变化刷新 |
| 4 | 大写/带点 = 组件，其余 = 原生标签 | JSX 后端的统一判定 |
| 5 | children 用 `gxscene.Child(props)()` 读（getter-map） | 值是子 `Instance`，透明摊平，无多余标签层；强类型形态 children 作为 struct 字段传入 |
| 6 | 更新靠 `Update()` 或 `BindReactive` 驱动 | scene 不内置响应式；`BindReactive(signal.Effect)` 可把 sig 直接接到插槽，免手动 Update |
| 7 | `SetPart` / `SetGetter` 用插槽下标 | 下标由编译期分配，手写 opcode 时才需要；JSX 开发中优先用 getter |
| 8 | host 不定义绘制后端接口 | `TextBackend` 只是文本工具；GUI 后端实现 `gxapp.Window`，可用 `Node.ID`/`Layout`/`Changes()` 做增量绘制，编译器无需改动 |

API：`CompileScene` / `CompileSceneWithModule`（对应默认的 `Compile` / `CompileWithModule`）；调试单步用 `gox jsxscene`。运行时见 [`runtime/scene/`](runtime/scene/)，完整示例见 `examples/jsxscene.gx`。

##### 应用入口：`gxapp.open` + `app.render`（类比 `ReactDOM.createRoot(...).render(...)`）

`gxapp.open(cfg)` 先创建「窗口外壳」并返回 `App`；`app.render(() => <App/>)` 收一个**建树函数**，把 JSX 场景树挂进窗口——类比 SolidJS / React 的 `render(() => <App/>, root)`。建树函数在内部建立的**根 Owner** 下运行，组件里的 `Signal.effect` / `onMount` / `onCleanup` 都归属它，`app.close()` 时级联销毁。

> **源码小写、生成大写**：`gxapp` 是外部 Go 包，遵循 GX 对 Go 来源的「标识符一律小写书写、编译器 `upperFirst` 还原」约定（本地 GX 模块则严格大小写，见 §4.1），源码写 `gxapp.open` / `app.render` / `app.close`，生成的 Go 是 `gxapp.Open` / `App.Render` / `App.Close`。配置写**裸对象字面量**且字段也小写，如 `gxapp.open({ title: "Demo", width: 800 })`，编译器从 `open` 的真实 Go 源码 AST 推断为 `gxapp.WindowConfig` 并把字段还原成真实拼写（通用机制，非特判；大小写混写、省略可选字段也支持）。

| 步骤 | 参数 / 返回 | 含义 |
|---|---|---|
| `gxapp.open(cfg)` | 参数：**窗口配置裸对象** `{ title, width, height, ... }`（小写，编译器还原真实字段名）；返回 `(*App, error)` | 只创建窗口外壳，不收 JSX、不绘制 |
| `app.render(build)` | 参数：**建树函数** `() => <App/>`（编译为 `func() any`，返回 `*gxscene.Instance`） | 在根 Owner 下建树、画首帧、完成响应式接线 |

```gx
// gox build demo.gximport gxapp "github.com/robertpanvip/gox/runtime/app"

app, err := gxapp.open({ title: "Demo", width: 800, height: 600 })
if err != nil { panic(err) }
defer app.close()
app.render(() => <div id="app">hello</div>)
app.run()
```

`WindowConfig` 字段：`Title` / `Width` / `Height` / `X` / `Y` / `Resizable` / `Fullscreen` / `Backend`。零值尺寸自动补 800×600，`Backend` 空值补 `"headless"`；`gxapp.defaultConfig()` 返回一份带默认值的配置。

**窗口后端是接口**，第一版内置 Headless（用 `TextBackend` 绘制到 `io.Writer`，沙箱/CI 可跑通）：

| API | 作用 |
|---|---|
| `gxapp.Open(cfg)` | 按配置打开默认 Headless 窗口，返回 `(*App, error)`（不绘制） |
| `gxapp.OpenWith(cfg, win)` | 注入自定义窗口后端（实现 `Window` 接口） |
| `Window` 接口 | `Open(cfg)` / `Draw(root)` / `Run()` / `Close()` / `Config()` |
| `App.Render(build)` | 收建树函数 `() => <App/>`：根 Owner 下建树 + 画首帧 + 内部响应式接线（sig 变化自动更新 + 重绘） |
| `App.Draw()` / `App.Update()` | 手动重绘一帧 / 更新场景树并重绘 |
| `App.Run()` / `App.Close()` | 进入事件循环 / 关闭（幂等，级联清理所有订阅与 Owner） |
| `gxapp.RenderText(view)` | 不建窗口，直接把场景树绘制成字符串（测试用） |

**sig 自动重绘（已封装进 render）**：`app.render` 内部建一个聚合 effect——`Instance.Update()` 递归读所有 getter 登记依赖并裁剪写入，随后 `Draw()` 一帧；条件分支 / keyed 列表的结构变化另有独立 effect 接线，重建子树后同样自动画一帧。一次 sig 写入 = **一帧**，不会出现「部分插槽已更新」的中间态画面（这是应用入口该有的批处理语义，区别于底层 `Instance.BindReactive` 的逐插槽精确模式）。源码**无需手写 `BindReactive`**。

```gx
sig count = 0
let doubled = Signal.computed(() => count * 2)
app, _ := gxapp.open({ title: "Demo", width: 800, height: 600 })
app.render(() => <div><span>{count}</span><b>{doubled}</b></div>)   // 根 Owner 下建树 + 接线
count = 5                          // 自动更新 + 只画一帧：count=5、doubled=10 一致
```

**有状态组件（SolidJS 风格）**：组件函数**只在建树时执行一次**，状态写在组件内部、不在 `main` 里；`sig` / `Signal.computed` / `Signal.effect` 在组件内声明，靠响应式保鲜；`Signal.onMount` 启动副作用、`Signal.onCleanup` 清理，二者归属 App 根 Owner，`app.close()` 时级联执行。完整可运行示例见 [`examples/jsxapp-solid.gx`](examples/jsxapp-solid.gx)：

```gx
pub func Counter(props gxscene.Props) *gxscene.Instance {
    sig count = 0   // 组件局部状态；读一律裸写 count，编译器自动补 ()
    let doubled = Signal.computed(() => count * 2)
    Signal.effect(() => { fmt.println("tick", count, doubled) })
    stop := make(chan struct{})
    Signal.onMount(() => {
        go func() { /* 定时器里 count += 1 */ }()
    })
    Signal.onCleanup(() => { close(stop); fmt.println("unmounted") })  // close 时触发
    return <div>{count} / {doubled}</div>
}
app.render(() => <div><Counter/></div>)
app.close()   // 级联执行 Counter 的 onCleanup
```

> **接入真实 GUI**：实现 `Window` 接口的 `Open/Draw/Run/Close/Config`，用 `OpenWith` 注入即可——Skia / wgpu / GLFW 后端无需改动 `gxapp` 或编译器。当前 Headless 后端让整条链路在无 GUI 环境端到端可验证。最小示例见 [`examples/jsxapp.gx`](examples/jsxapp.gx)；**复合组件 + sig 响应式**见 [`examples/jsxapp-full.gx`](examples/jsxapp-full.gx)；**SolidJS 风格有状态组件**（组件内局部状态 + 定时器自增 + onMount/onCleanup 生命周期）见 [`examples/jsxapp-solid.gx`](examples/jsxapp-solid.gx)，运行时见 [`runtime/app/`](runtime/app/)。
>
> **后端预制控件库**：`backend/skia/widgets` 提供 `widgets.button` / `input` / `card` /
> `badge` / `progress` / `row` / `col` 等预制组件，外观全部沉淀在后端，业务 JSX 只写语义；
> 其中 `input` 支持真实键盘输入（sig 双向绑定 + 焦点 + 光标闪烁）。完整可运行示例见
> [`examples/desktop-demo`](examples/desktop-demo)（GX → Skia 真实窗口 → 可输入看板）。

## 4. 外部 Go 符号：按真实拼写书写

**约定：按 Go 真实名字照抄即可。** 全大写缩写（`URL`、`StatusOK`）直接写大写、原样保留；小写形式（`newRequest`）也兼容，由机械的 `upperFirst` 还原。零依赖、零歧义。

| GX 书写 | 生成 Go | 说明 |
|---|---|---|
| `http.NewRequest(...)` | `http.NewRequest(...)` | 真实拼写，原样保留 |
| `r.URL` / `http.StatusOK` | `r.URL` / `http.StatusOK` | 全大写缩写，原样保留 |
| `http.newRequest(...)` | `http.NewRequest(...)` | 小写形式，机械还原（兼容旧写法） |
| `req.method` / `u.host` | `req.Method` / `u.Host` | 外部结构体字段 |
| `fmt.println(...)` | `fmt.Println(...)` | 单词 |

- **全大写不改写**：已经是大写的标识符（`URL`、`StatusOK`）在整条管线里透明通过，不会被首字母化破坏——用别人的 Go 代码时按真实拼写写即可。
- **作用域感知**：自己的私有字段/变量（小写书写）不会被误改——转译器收集本文件的私有符号加以排除。
- 裸标识符（内置 `println`/`len`/`make` 等）不碰。
- 别名导入 `import h "net/http"` 支持。
- 小写形式需按 Go 真实名字照抄驼峰（`Itoa` 写 `itoa`，不能写 `itoA`）；全大写写法无此约束。

### 4.1 大小写按 import 来源分流（本地 GX 模块严格大小写）

上面的 `upperFirst` 规则只针对**外部 Go 包**。`import` 命中项目内另一组 `.gx` 时走另一套语义：

| 来源 | 判定 | selector / 字段还原 |
|---|---|---|
| 外部 Go 包 | 路径不是本地 `.gx`（或 `-prefer go`） | 小写形式机械 `upperFirst`，兼容旧写法 |
| 本地 GX 模块 | 路径命中项目内某组 `.gx` | **严格查被导入方 `pub` 导出表**，不猜测 |

本地 GX 模块：`util.add(...)` → 查导出表 `add→Add` → `util.Add(...)`；写错大小写 `util.ADD(...)` 不改产物，GX 检查器报 `import-case: 符号 "ADD" 大小写错误：包 … 导出的是 "Add"`（带源码坐标），struct 字段写错报 `import-field-case`。诊断由 `gox check -root` / `gox build`（项目模式）与 LSP 直接报出，无需等 `go build`。用 `gox build <entry.gx> -root <项目根>` 启用项目模式（模块路径从 `<root>/go.mod` 读取，缺失回退 `-module`）；同名时默认本地 GX 优先，`-prefer go` 切到 Go 来源。可运行示例见 [`examples/crossfile/`](examples/crossfile/)，权威定义见 [`docs/language-spec.md`](docs/language-spec.md) §4.1 / §13。

## 5. 集合链式调用

`[]T` **全局提升**为 `list.List[T]`，直接获得链式方法。依赖 **Go 1.27 泛型方法**，`Map`/`Reduce` 可换型，回调是原生闭包、原样透传。

```gx
nums := []int{1, 2, 3, 4, 5, 6}
labels := nums.map(strconv.itoa).filter(func(s string) bool { return len(s) == 1 })
total := nums.filter(even).reduce(0, func(acc, x int) int { return acc + x })
```

- `List[T]` 与 `[]T` 底层相同，可双向赋值；标准库返回的 `[]T` 也能用方法。
- **不误伤**：数组 `[3]T`、索引 `a[i]`、切片表达式 `a[1:2]`、`map[K]V`、`Foo[T]` 都不是切片类型形态，不会被提升。
- 用 `-module` 指定运行时 import 路径（默认 `github.com/robertpanvip/gox` → `.../runtime/list`）。运行时见 [`runtime/list/`](runtime/list/)。

## 6. is 类型守卫

`if (x is T) { … }` 判断接口值 `x` 的动态类型是否为 `T`，成立时**块内 `x` 自动窄化为 `T`**，可直接按 `T` 使用。转译为 Go 原生 comma-ok 断言 + 块首 shadow，零运行时魔法。

```gx
var x any = float64(3.5)
if (x is float64) {
    fmt.println(x + 1.5)   // 此处 x 是 float64，可直接运算
}
// → if __v, __ok := x.(float64); __ok { x := __v; fmt.println(x + 1.5) }
```

- `is` 不是全局关键字，仅在 if 守卫模式中识别；作普通变量名不受影响。
- 支持 `if (x is T)` 与 `if x is T` 两种写法；`T` 支持任意 Go 类型（`int`、`[]int`、`*T`、`pkg.Type`、`map[K]V` 等）。
- 支持 `else if` 链与嵌套。`else` 分支对联合类型做**排除法收窄**：`x: A|B` 经 `if x is A` 后，`else` 内 `x` 自动窄化为 `B`（注入 `x := x.(B)`）；仅当剩余为单一具体类型时注入，剩余仍是联合（如 `A|B|C` 只守卫 A）则不注入。
- **限制**：被守卫者须为简单标识符且静态类型是接口（如 `any`）——`x.(T)` 要求接口，否则生成的 Go 编译不过（转译器不做类型检查）；暂不支持 `obj.field` 作被守卫者、复合条件 `x is T && y`。

## 7. 联合类型

类型位置的 `A | B | C` 被**擦除为 `any`**，配合 is 守卫在分支内窄化为具体类型。这是一项「弱联合」语法糖——Go 侧没有原生联合类型，擦除后由 is 守卫承担运行时类型判断。

```gx
pub func describe(v int | string | bool) string {
    if (v is int)     { return fmt.sprint("int:", v+100) }
    if (v is string)  { return fmt.sprint("str:", v) }
    return "other"
}
type Num = int | float64   // → type Num = any
```

- 支持位置：`var`/`const`/`type` 声明（含分组）、函数参数/返回值/接收者、struct 字段。
- 成员可为任意 Go 类型：`int`、`pkg.Type`、`[]T`、`*T`、`map[K]V`、`Foo[T]` 等。
- 消歧：`|` 仅在类型标注位置视为联合；函数体的位运算 `a | b`、`var x = a|b`（无类型初始化）不受影响。

**参数运行时守卫**：函数参数声明为联合类型时，入口会自动插入 `switch v.(type)` 检查——动态类型不在成员列表内立即 `panic`，把弱联合的契约从「静默错误」变成「显式失败」：

```go
// 上述 describe 生成的 Go（守卫 + is 窄化）
func Describe(v any) string {
    switch v.(type) {
    case int, string, bool:
    default:
        panic("gx: describe: v expects int|string|bool, got a non-matching type")
    }
    // ... 原有 is 守卫窄化代码
}
```

- 守卫只加在**函数参数**的联合上；局部变量/类型别名/只有返回值的联合仅擦除，不插守卫（无入口可检查）。
- panic 消息含函数名、参数名与期望类型，不打印实际类型（避免引入 `fmt` 依赖）。
- **限制（务必知晓）**：
  - **参数守卫在运行时**——静态传错类型由下一节的 `gox check` 拦，绕过检查直接编译时调用时立即 panic；
  - `any` 上不能直接运算，须先 is 守卫窄化；
  - 不处理 interface/struct 内联、函数类型签名内部、以及「嵌套联合」（`[]int|string` 按 Go 优先级解析为 `([]int)|string`，整段擦除）。
- 若需要真正的编译期类型安全，后续可升级为泛型约束方案（`[T int|string]`）。

## 8. 参数默认值

函数参数可写 `= 默认值`，采用 **TS 语义**：调用点按缺失的**尾部参数**补全默认值。关键区别是「未传」用默认值、「显式传零值」用零值——Go 原生零值无法替代这一点。

```gx
func greet(name string = "world", times int = 2) string {
    out := ""
    for i := 0; i < times; i++ { out += name + " " }
    return out
}
// greet()          → greet("world", 2)
// greet("gx")      → greet("gx", 2)
// greet("gx", 1)   → 原样
// greet("", 1)     → 原样（显式空串不被默认值覆盖）
```

- 声明处默认值被剥离，生成干净的 Go 签名；调用点补全发生在类型分析之后、联合擦除之前，默认值表达式仍流经全部语法糖变换（可含外部符号调用、模板、联合等）。
- **限制**：只补全尾部连续缺失参数（中间无默认值的参数不补）；仅顶层函数调用匹配，方法调用 `obj.f()` 不补（避免同名误伤）；同名局部遮蔽/函数值间接调用不补。

## 9. Lambda 表达式

最简箭头 `params => expr` 编译为 Go 函数字面量。参数与返回类型由转译器推断：

```gx
double := x => x * 2          // func(x int) int { return x*2 }
isBig := x => x > 10          // func(x int) bool { return x>10 }
add := (a int, b int) => a+b  // 显式参数类型
answer := () => 42            // func() int { return 42 }
apply(x => x + 1, 41)         // 函数实参：从形参签名推断
xs.map(x => x * 2)            // 集合回调：从元素类型推断
```

**类型推断来源**（优先级从高到低）：参数显式标注 → 上下文函数类型（`func(int) int` 形参 / 变量类型标注）→ 集合回调元素类型 → 表达式反推（数值字面量→`int`、比较→`bool`、`len`→`int`、字符串→`string`）。

- 推断不出时降级 `any`（联合参数也擦除为 `any`），仍产出合法 Go。
- 只能写**单表达式** body（`=>` 后一个表达式）；多语句体请写原生 `func(...) T { ... }`。

## 10. 模板字符串

反引号内含 `${...}` 插值时识别为模板字符串，编译为**字符串拼接**（`fmt.Sprint`），静态段自动转义。`fmt` 由 ImportManager 统一托管：源码已导入则复用别名，否则自动注入。

```gx
name := "gx"; score := 95
msg := `hello ${name}, score=${score}`   // "hello " + fmt.Sprint(name) + ", score=" + fmt.Sprint(score)
```

- 无插值的反引号串仍是 Go 原生 raw string，原样输出。
- 插值内是完整表达式，可含外部符号调用、集合链式、嵌套字面量；花括号深度与字符串/反引号内的 `}` 会被正确跳过。
- **限制**：暂不处理 `\${` 转义。

## 11. 对象字面量

`{ name: "x", age: 1 }` 编译为 Go 匿名结构体字面量。两种情况：

**无期望类型**（如 `p := {...}`）：从字段值反推类型（`"x"`→`string`、`1`→`int`、`3.5`→`float64`、`true`→`bool`、嵌套 `{...}`→嵌套匿名 struct、推断不出→`any`），生成匿名结构体：

```gx
p := { name: "x", age: 1, ok: true }
// → p := struct{ name string; age int; ok bool }{ name: "x", age: 1, ok: true }
```

**有期望类型**（明确标注了参数/返回/变量类型的上下文）：省略结构体类型名，由期望类型补全并校验字段：

```gx
type Point struct { name string; age int }
var p Point = { name: "x", age: 1 }   // → var p Point = Point{ name: "x", age: 1 }
f({ name: "x", age: 1 })              // f(p Point) → f(Point{...})
p = { name: "x", age: 1 }             // 赋值：按 p 的声明类型补全
func make() Point { return { name: "x", age: 1 } }  // 按返回类型补全
```

- 校验：字段名未知、缺失字段、值类型不兼容均报**编译错误**（带 GX 源码坐标）。
- 无类型常量按 Go 可赋值规则放宽（整数字面量可赋给整数/浮点族）。
- **期望类型为 `any` / `interface{}` 时不补全**：这类类型不携带字段结构，`any{...}` 是非法 Go，故按匿名结构体处理（如 `func make() any { return { name: "x" } }` → `return struct{ name string }{ name: "x" }`）。
- 支持嵌套对象字面量（由内向外逐层补全）。

## 12. 对象解构

`{ name, age } := p` / `var { name, age } = p` 把结构体字段解构为同名的局部变量，逐字段生成 Go 声明：

```gx
type Student struct { name string; age int; city string }
var p Student = { name: "x", age: 1, city: "SZ" }
{ name, age } := p        // → name := p.name; age := p.age
var { name, city } = q    // → var name = q.name; var city = q.city
```

- **类型检查**：右值类型可从本文件推断时（具名 struct 或匿名 struct 字面量），每个解构字段必须真实存在，否则报**编译错误**（带 GX 坐标）。
- **构造后立刻解构**：构造侧字段集 S₁ 由对象字面量层校验，解构侧 S₂ 由本层校验，二者都是 `fields(T)` 的子集，故 `S₁ ∪ S₂ ⊆ fields(T)`。
- **右值类型未知**（如外部包函数返回）：宽松生成 `x.field`，字段存在性交给 Go 编译期检查。
- 复用 `var`/`:=` 语法，不引入新关键字；解构变量遵循 Go 的作用域与 `:=` 重声明规则。

## 13. Signal 响应式

`sig` 声明源信号，`Signal.computed` 派生只读信号，`Signal.effect` 注册副作用；依赖变化时 effect 自动重跑。底层是函数式 sig（`func(...T) T`：无参读、带参写）+ 依赖追踪运行时（`runtime/signal`）。

```gx
sig count = 0
func main() {
    let doubled = Signal.computed(() => count * 2)
    Signal.effect(() => println("count=", count))   // count 变化时自动重跑
    count = 5                                         // 写 → 触发 effect + computed 失效
    println(doubled)                                  // → 10（读自动加 ()）
}
```

降级为标准 Go：

```go
var count = Signal.New(0)
doubled := Signal.Computed(func() int { return count() * 2 })
Signal.Effect(func() { println("count=", count()) })
count(5)
println(doubled())
```

- **语法糖**：`sig n = v` → `var n = Signal.New(v)`；`let n = v` → `n := v`；sig 变量**读**自动补 `()`、**写**（`=`/`+=`/`++` 等）改写为调用式。
- **作用域安全**：selector `obj.count`、复合字段键 `{count: 1}`、struct 字段、函数参数、已显式调用 `count()` 均不误改。
- **依赖追踪**：`computed` 惰性缓存，`effect` 立即执行一次并在依赖变化时重跑，条件分支依赖会动态重建；运行时并发安全。
- `Signal` 作为运行时包名由 ImportManager 统一注入 `import Signal "<module>/runtime/signal"`，源码已用别名导入则复用。

## 14. Sourcemap / LSP / 编译检查

- 自带 `.gx ↔ .go` **双向精确位置映射**（`sourcemap` / `map-lookup`）。
- LSP：跳转 / 补全 / 悬停 / 文档符号 / **重命名**（枚举成员三种拼写派生新名）。
- 真实 `go build` 检查（`buildcheck`，可取消 + LRU），错误经 sourcemap **回投到 `.gx`**。

## 15. GX 类型检查（方案 B：AST + side table）

GX 把联合 `A|B` 擦除为 `any` 后，`go/types` 眼里只剩 `any`，**永远看不到联合契约被违反**。因此类型检查的主战场放在 desugar 之前的 GX 层——这正是 TS 对 JS 的关系：类型是本层的责任，`go build` 只兜底「降级代码是否合法」。

```
GX 源码 ─lex→ token ─Analyze→ side table（符号 / 联合 / 收窄，GX 坐标索引）
                                    │
                    desugar（擦除 A|B→any，is→comma-ok）
                                    ↓
                              Go 源码 ─go/parser→ 完整 AST
                                    ↓
              AST 节点 Go 坐标 ─sourcemap→ GX 坐标 ─查 side table→ GX 诊断
```

- **结构**用 `go/parser` 白拿（嵌套 / 作用域 / 控制流都现成），**类型语义**用我们自己的 side table，两者靠 sourcemap 坐标缝合，**不自己写 parser**。
- 诊断统一为 **GX 源码坐标**（不经回投），CLI 与 LSP 同源呈现。

运行 `gox check <file.gx>` 查看诊断；示例见 `examples/typecheck.gx`。

| 诊断码 | 规则 | 触发示例 |
|---|---|---|
| `union-assign` | 联合变量赋值的成员兼容性 | `var x int\|string = true` |
| `union-call-arg` | 函数调用实参落在联合参数内 | `f(true)` 而 `f(v int\|string)` |
| `union-return` | 返回值落在联合返回类型内 | `return true` 而函数返回 `int\|string` |
| `union-operand` | 联合参与运算前须 is 收窄 | `x+1` 而 x 仍是 `int\|string` |
| `non-exhaustive` | type switch / if-is 链的联合穷尽性 | 漏掉某成员的分支 |
| `union-member` | is 守卫目标不是联合成员 | `x is bool` 而 x 是 `int\|string` |
| `redundant-guard` | 已收窄类型上的重复守卫 | 嵌套 `if x is int { if x is int {} }` |

**类型兼容规则**（严格按 Go 可赋值语义，不是字符串比较）：
- **无类型常量**（字面量 / `const`）：`1` 可赋给 `int/int64/uint/float64` 等数值族，`3.5` 可赋给浮点族，`"x"` 可赋给 `string`；但负数（`-1`）不能赋给无符号类型；
- **具类型值**：必须精确匹配联合成员；
- **命名别名**：`type MyU = int|string` 会展开参与检查（含链式别名、循环检测）。
- **多值**：`x, y = true, false` 逐项检查；命名返回值 `func f() (v int|string)` 的赋值也检查。

**防误报原则**：右侧类型推断不出来（如未知函数调用、跨包 selector、struct 字段）一律放过；非联合变量交给 `go/types`；`any`/`unknown` 不做成员约束。跨包/方法调用按名字匹配会误判同名函数，故只检查本包直接调用。

**收窄规则**：
- **then 块**：注入 `x := __gx_v` shadow，`x` 窄化为守卫目标类型；块内未引用 `x` 时自动改用 `_` 绑定，避免未使用变量。
- **else 块**：对联合做排除法收窄——`x: A|B` 经 `if x is A` 后，`else` 内注入 `x := x.(B)`，`x` 窄化为剩余类型。仅当剩余是**单一具体类型**且块内确实引用了 `x` 时注入；剩余仍是联合（如 `A|B|C` 只守卫 A）、或块内未用到 `x` 时不注入。检查器与生成代码始终一致：能收窄则不报 `union-operand`，不能收窄而直接运算则报。
- **守卫后直线代码**：守卫链之后（非块内）的 `x` 不收窄，仍是原联合/any——要在块内使用。

**is 语法限制**：`is` 仅支持 `if x is T` 简单守卫形态。复合条件（如 `if x is int && x > 0`）不被改写，会在转译时报明确错误，而非静默产出非法 Go。

## 目录结构

```
gox/
├── cmd/
│   ├── gox/            CLI：build / union / typeguard / jsx / pub / enum /
│   │                       collections / sourcemap / map-lookup / lsp-map / lsp / test
│   └── gox-lsp/        独立 LSP server 入口（stdin/stdout JSON-RPC）
├── pkg/                对外稳定 API
│   ├── compiler/       转译引擎：词法 / union / typeguard / JSX / pub / enum / scope / external / collections / pipeline
│   └── sourcemap/      双向位置映射（无业务依赖）
├── internal/           工具内部实现
│   ├── check/          GX 类型检查器：go/parser AST + side table + 7 类诊断规则
│   ├── lsp/            LSP 协议 / server / 诊断 / 符号映射 / 重命名
│   └── buildcheck/     真实 go build 检查（可取消 + LRU）
├── bindings/cabi/      C-ABI 共享库（供 IDEA 插件经 JNA 调用）
├── backend/            可独立拆分的后端实现（统一契约 Contract + Adapter）
│   ├── skia/           Go Backend（module gox-backend-skia）
│   └── rust/           Rust Backend（Go Adapter + Rust Runtime + Wire 协议）
├── examples/           .gx 示例（含 jsxscene.gx）
├── testdata/           golden 产物 + LSP 端到端驱动脚本
├── docs/               设计与集成文档
└── runtime/            运行时组件
    ├── scene/          opcode 模板 + 常驻场景树 + 细粒度 Update（JSX 后端）
    ├── app/            窗口外壳 + Owner/Cleanup + 响应式重绘
    ├── sig/         Signal / Computed / Effect / Batch
    └── list/           List[T] + 泛型 Map/Filter/Reduce 运行时
```

分层：`pkg` 是对外稳定 API，`internal` 仅供本仓库 cmd 使用；IDEA 插件等前端通过 `bindings/cabi` 或直接 import `pkg` 接入，不碰 `internal`。

详细文档：[语言规范](docs/language-spec.md) · [架构](docs/architecture.md) · [后端架构（契约与拆分）](docs/backend-architecture.md) · [后端接入（host + 协议）](docs/backend-integration.md) · [JNA 集成](docs/CABI.md)

## 构建与使用

```bash
make build          # gox、gox-lsp、libgox.so（需 Go 1.27+）
make test           # 24 项转译自检 + go test ./...
make lsp-test       # LSP 单元测试 + 端到端脚本

gox build demo.gx -o out.go                       # 转译
gox build demo.gx -check -module example.com/app \
    -replace example.com/app=/path/to/runtime     # 转译 + 真实编译检查（错误回投 .gx）
gox sourcemap demo.gx -o map.json                 # sourcemap
gox map-lookup -to-go map.json 5 16               # 位置正查
gox fmt demo.gx                                   # 格式化 JSX（输出到 stdout）
gox fmt -w ./examples                             # 递归格式化目录内 .gx 并写回
gox fmt -check demo.gx                            # CI 检查是否已格式化
gox lsp-map ./examples                            # 项目符号 JSON
gox lsp                                           # 启动 LSP
```
