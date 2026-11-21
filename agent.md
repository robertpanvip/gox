# Gox 项目规则（给 AI Agent / 贡献者）

本文件是本项目的最高约定。**任何人在写 `.gx` 源码、示例、文档、README、演示代码时，都必须参照本项目自己设计的 GX 语法，而不是照搬生成后的 Go 写法。** 违反这条规则是本项目最典型的错误。

## 0. 一句话

Gox 是一个把 `.gx` 转译成标准 Go 的编译器。`.gx` = Go 语法 + 一组语法糖。你写的是 **GX 源码**，产出的是 **Go 代码**——两者写法不同，文档和示例必须展示前者。

- 语言规范权威来源：`docs/language-spec.md`
- 书写约定的活例子：`examples/lowerext.gx`（「GX 心智负担：一律写小写」）
- 需要 Go **1.27+**（泛型方法）；本仓库用 `GOTOOLCHAIN=go1.27.4`

## 1. 最高规则：标识符一律小写书写（最重要）

> **在 GX 源码里，标识符一律写小写；编译器会在 selector 右侧机械地 `upperFirst` 还原成 Go 的导出名。全大写缩写直接写大写、原样保留。两个硬编码例外：`Signal` 前缀和 `pub` 组件/类型名必须大写开头。**

这是本项目的设计选择：转译器不读第三方包符号表、不做类型推导，只对小写形式做机械首字母大写还原，因此行为完全可预测、零歧义。

| 你在 GX 里写 | 生成的 Go | 规则 |
|---|---|---|
| `fmt.println(...)` | `fmt.Println(...)` | 包级函数：小写书写，还原 |
| `strconv.itoa(v)` | `strconv.Itoa(v)` | 同上 |
| `req.method` / `u.path` | `req.Method` / `u.Path` | 结构体字段：小写书写，还原 |
| `r.URL` / `http.StatusOK` | 原样 | **全大写缩写写大写，原样保留** |
| `gxscene.get[T](props, "x")` | `gxscene.Get[T](...)` | runtime 符号：小写书写，还原 |
| `view.bindReactive(Signal.effect)` | `view.BindReactive(signal.Effect)` | 底层 scene 实例方法：小写书写；signal API 用大写 `Signal.` 前缀 |
| `gxapp.open({ ... })` + `app.render(() => <App/>)` | `gxapp.Open(gxapp.WindowConfig{...})` + `app.Render(func() any {...})` | **open 只收配置**（裸对象字面量，从签名推断类型）返回 App；**render 收建树函数**（SolidJS 风格），内部建根 Owner + 接线，无需手写 bindReactive |
| `Signal.createRoot(...)` / `Signal.onCleanup(...)` | `signal.CreateRoot` / `signal.OnCleanup` | **signal API 一律大写 `Signal.` 前缀** |
| `Signal.computed(() => expr)` | `signal.Computed(func() T { return expr })` | **computed/回调首选箭头函数**（SolidJS 风格）；body 是单点 signal getter 时也能推断返回类型 |
| `pub func Stat(...)` | `func Stat(...)` | **组件名 `pub func` 大写开头** |

**边界与例外**（务必遵守，否则会写出无法编译的示例）：

1. **多词符号只小写首字母，其余照抄 Go 拼写**：`bindReactive` → `BindReactive`、`newWithEquals` → `NewWithEquals`（不能写 `bindreactive`，那会生成 `Bindreactive`，拼错）。
2. **所有 signal API 用大写 `Signal.` 前缀**：`Signal.computed` / `Signal.effect` / `Signal.createRoot` / `Signal.onCleanup` / `Signal.onMount` / `Signal.batch` / `Signal.getOwner` / `Signal.runWithOwner` / `Signal.newWithEquals` 等一律以大写 `Signal` 开头。原因有二：`Signal` 是语法糖占位包名（ImportManager 解析为 `import Signal "<module>/runtime/signal"`），且**避免与 `signal` 声明关键字（`signal n = 0`）混淆**。不要写裸 `computed(...)`（裸标识符不会被还原，会生成不存在的函数）。生成产物统一是 `signal.XXX`（小写包名）。
3. **`pub` 组件名与 JSX 标签都大写开头**：组件声明写 `pub func Stat(...)`，调用写 `<Stat/>`。JSX 里首字母大写 = 组件、小写 = 原生 HTML 标签，这是硬编码判定，不能把组件写成小写标签。
4. **本文件私有类型写小写**：`type cardProps struct{...}` 保持私有小写；`pub type` 才导出。
5. **裸标识符不碰**：内置函数等裸标识符一律不改，不要给它们加大小写。
6. **回调用箭头函数**：`Signal.computed` / `Signal.effect` / `batch` / `onMount` / `onCleanup` / 集合回调（`map`/`filter`）等一律写箭头——单语句 `() => expr` / `x => expr`，多语句 `() => { stmt; stmt }`（花括号 body，生成 `func(){...}`），贴近 SolidJS/JS 习惯，别写 `func() T { return ... }`。编译器从 body 反推参数与返回类型，包括 `computed(() => count)` 单点 signal getter（裸写，从 signal 声明类型反查，不降级 `any`）；带参数的 `createRoot((o *Signal.owner) => { ... })` 保留显式类型标注。只有极少数需要显式多返回值的场合才用 `func` 字面量。

### 反面教材（本项目真实犯过的错，禁止重演）

```gx
// ❌ 错：signal API 用小写前缀（与 signal 关键字混淆）/ 组件小写 / 配置写全类型名 / open 传 JSX / render 收已建好的树
signal.createRoot(func(o *signal.owner) { signal.onCleanup(...) })
view.bindReactive(signal.effect)
pub func stat(props gxscene.props) *gxscene.instance { ... }
app, _ := gxapp.open(view, gxapp.windowConfig{...})
app.bindReactive(signal.effect)
app.render(view)

// ✅ 对：signal API 大写 Signal. 前缀；pub 组件大写；配置用裸对象字面量；
//      open 只收配置、render 收建树函数（圆括号箭头）且自带响应式与生命周期
Signal.createRoot((o *Signal.owner) => { Signal.onCleanup(() => { ... }) })
view.bindReactive(Signal.effect)
pub func Stat(props gxscene.props) *gxscene.instance { ... }
app, _ := gxapp.open({ Title: "Gox", Width: 800 })
app.render(() => <div><Stat/></div>)
```

## 2. 必须优先使用 GX 自身的语法糖

示例和文档要体现 GX 的设计能力，不要把 GX 当成「小写版 Go」来写。十三项语法糖见 `docs/language-spec.md`，高频项：

| 语法糖 | GX 写法 | 要点 |
|---|---|---|
| pub 可见性 | `pub func greet()` / `pub type point` / `pub enum color` | 声明处导出的开关；组件名也要大写开头如 `pub func Stat` |
| enum 枚举 | `pub enum color { red, green, blue }`，用 `ColorRed` | 常量前缀大写 |
| 外部符号还原 | `http.newRequest(...)`、`fmt.sprintf(...)` | 小写书写，机械还原 |
| 集合链式 | `nums.map(fn).filter(fn).reduce(0, add)` | `[]T` 全局提升为 `list.List[T]`（Go 1.27 泛型方法） |
| 模板字符串 | `` `hi ${name}` `` | 编译为字符串拼接 |
| 参数默认值 | `func f(x int = 5)` | TS 语义，补尾部缺失参数 |
| Lambda | `x => x+1` | 单表达式 body；类型可推断 |
| 对象字面量 | `{ title: "Gox", n: 1 }` | 无期望类型→匿名 struct；有期望类型→补全具名类型并校验字段。作为外部包函数实参时（如 `gxapp.open({ Title: ... })`），编译器用 `go list` + `go/parser` 读该包真实 Go 源码 AST 取参数类型（通用机制，非特判；失败则回退匿名 struct） |
| 对象解构 | `{ title, n } := cfg` | 纯标识符列表，逐字段生成赋值 |
| is 守卫 | `if (v is int) { ... }` | 块内窄化为具体类型 |
| 联合类型 | `v int \| string \| bool` | 擦除为 any + 入口守卫 |
| Signal 响应式 | `signal n = 0`、`n = 5`、`Signal.computed(...)`、`Signal.effect(...)` | 读自动补 `()`，写改写为 `n(v)`；**API 一律大写 `Signal.` 前缀** |

## 3. Scene / 响应式 UI（scene 后端 + 四阶段增强）

JSX 默认走 scene 后端（`gox build -jsx=scene`）：编译期 opcode + 常驻场景树 + getter 保鲜 + 递归细粒度更新，不是 vdom diff。

四阶段响应式增强的**源码写法**（详见 `docs/reactive-ui-roadmap.md`）：

```gx
// 阶段1：Owner / Cleanup / Effect 句柄（signal API 一律大写 Signal. 前缀）
Signal.createRoot((o *Signal.owner) => {
    h := Signal.effect(() => {
        fmt.println(count)
        Signal.onCleanup(() => { /* 重跑前 / 销毁时 */ })
    })
    _ = h // h.dispose() 幂等
})

// 阶段2：条件分支（selector 读 signal，切换时销毁重建局部子树 Owner）
view := <div>
    if visible {
        <span>{count}</span>
    } else {
        <b>Hidden</b>
    }
</div>

// 阶段3：Keyed For（render 参数类型可从 each 元素推断，无需显式注解）
view := <ul>
    <For each={users} key={(u) => u.id}>
        {(u) => <li>{u.name}</li>}
    </For>
</ul>

// 阶段4：应用入口 open（只收配置）+ render（收建树函数，内部建根 Owner + 接线）
// 配置用裸对象字面量，编译器从 gxapp.open 签名推断为 gxapp.WindowConfig
app, err := gxapp.open({ Title: "Gox", Width: 800 })
if err != nil { panic(err) }
defer app.close()
app.render(() => <div>{...}</div>)     // 在根 Owner 下建树 + 响应式接线
Signal.batch(() => { count(5); visible(false) })
```

### 有状态组件（SolidJS 风格，推荐）

组件函数**只在建树时执行一次**，状态写在组件内部（不在 main 里），靠 signal + effect 保鲜；副作用用 `onMount` 启动、`onCleanup` 清理，二者归属 App 根 Owner，`app.close()` 时级联执行。完整可运行示例见 `examples/jsxapp-solid.gx`：

```gx
pub func Counter(props gxscene.props) *gxscene.instance {
    signal count = 0
    let doubled = Signal.computed(() => count * 2)
    Signal.effect(() => { fmt.println("tick", count, doubled) })
    stop := make(chan struct{})
    Signal.onMount(() => {
        go func() { /* 定时器里 count += 1 */ }()
    })
    Signal.onCleanup(() => { close(stop); fmt.println("unmounted") })
    return <div>{count} / {doubled}</div>
}
func main() {
    app, _ := gxapp.open({ Title: "Solid", Width: 2, Height: 2 })
    app.render(() => <div><Counter/></div>)   // 根 Owner 在此建立
    app.close()                                // 级联执行 Counter 的 onCleanup
}
```

约束：组件名大写开头（`pub func Counter` + `<Counter/>`）；**signal 读一律裸写**（`count * 2`、`if count >= 3`、`{count}`），编译器自动补 `()` 并建立依赖，不要手写 `count()`；`onCleanup/onMount` 必须写在组件函数体内（建树时注册到 Owner），不要在 effect 回调外提前调用。

约束：分支 selector 必须读 signal 才有响应式；`<For>` 必须提供 `key`；组件内 signal 要经 Props getter 传到叶子插槽才能在写入时更新；app 场景的响应式接线（叶子聚合 effect + 分支/列表结构重绘）全部封装在 `app.render` 内部，源码无需手写 bindReactive；外部包函数的配置参数（如 `gxapp.open({ ... })`）可直接写裸对象字面量——编译器从该包真实 Go 源码 AST 读取期望参数类型并补全（通用机制，见 `pkg/compiler/gosig.go`）。

## 4. 环境与命令

```bash
export GOTOOLCHAIN=go1.27.4        # 必设：泛型方法依赖

go build ./...                      # 全量编译
go test ./...                       # 全量测试（必须全绿）
gox build examples/xxx.gx -jsx=scene -o main.go && go run .   # 端到端验证单个示例
```

修改编译器 / runtime 后：先 `go test ./...`，再用真实 `.gx` 端到端 `go run` 验证生成代码能编译运行。写文档示例时同理——**示例必须是你验证过能真实编译运行的小写 GX 写法**，不要凭空写 Go 大写 API。

## 5. 提交约定

- 分支：`feat/scene-gxapp`
- 提交前确保 `go test ./...` 全绿、工作树无临时调试文件（如 `zz_*_test.go`）。
- 推送 `git push origin feat/scene-gxapp`；GitHub 偶发 502，重试即可。
