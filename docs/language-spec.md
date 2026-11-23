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

### 前缀式命名

常量 = `upperFirst(类型名)` + `upperFirst(成员名)`：

| enum 声明 | 类型名（Go） | 常量 | Parse |
|---|---|---|---|
| `pub enum color { red }` | `Color` | `ColorRed` | `ParseColor` |
| `enum status { pending }` | `status`（私有） | `StatusPending` | `ParseStatus` |

即使 enum 私有，常量与 Parse 名仍以大写前缀开头（遵循 Go 导出惯例）。

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

**JSX 可跨行**：闭合标签后的换行若属于排版缩进（换行后紧跟宿主调用的 `)` / `]` / `,`，中间可夹行注释），编译器会吞掉这段空白，避免 Go 在参数列表换行处要求 trailing comma。因此下面两种写法等价，无需为规避语法错误而把 JSX 挤成单行：

```jsx
app.render(() =>
    <div>
        <span>hi</span>
    </div> // 尾注也会被一并吞掉
)
```

> 只有紧邻调用闭合符号的排版换行会被吞；后接标识符或 `}`（函数体/语句结束）的换行是真正的语句分隔，保留——不会把 `return` 与函数闭合挤到同一行。

运行时 `runtime/scene/`：`Template` / `Op` / `Render` / `Instance`。`Render` 缓存静态骨架、clone 出常驻场景树；`Instance.Update()` 只回写变化的插槽（可观测指标 `Writes`），不含调度。host 不定义绘制后端接口：GUI 后端实现 `gxapp.Window`，可用 `Node.ID`（整数稳定身份）/ `Changes()` 做增量绘制；内置 `TextBackend` 只是文本工具。

### 3.1 `<For>` 列表

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

`x => x+1` → `func(x int) int { return x+1 }`。类型来源优先级：参数显式标注 → 上下文函数类型 → 集合回调元素类型 → 表达式反推（数值→int、比较→bool、len→int、字符串→string）；推断不出降级 `any`。仅支持单表达式 body，多语句用原生 func 字面量。

## 9. 对象字面量

`{ name: "x", age: 1 }` → Go 匿名结构体字面量。无期望类型时从值反推字段类型（`"x"`→string、`1`→int、`3.5`→float64、`true`→bool、嵌套 `{...}`→嵌套 struct、否则 any），生成 `struct{ name string; age int }{...}`。有期望类型时（函数实参 / 变量声明 / 赋值 / return——即明确标注参数或返回类型的上下文）省略结构体类型名，用期望类型补全为 `Point{...}` 并校验字段：未知字段 / 缺失字段 / 类型不兼容均报带 GX 坐标的编译错误。无类型常量按 Go 可赋值规则放宽。支持嵌套（由内向外逐层补全）。期望类型为 `any` / `interface{}` 时视为无有效期望类型（不携带字段结构，`any{...}` 非法），按匿名结构体处理。

## 10. 对象解构

`{ name, age } := p` / `var { name, age } = p` → 逐字段生成 `name := p.name; age := p.age` / `var name = p.name; var age = p.age`。复用 `var`/`:=` 语法，不引入新关键字。

- 解构模式是**纯标识符列表**（逗号分隔、无冒号）；与对象字面量（`key: value`）在词法上严格区分，互不干扰。
- **类型检查**：右值类型可从本文件推断时（具名 struct 或匿名 struct 字面量），每个解构字段必须属于 `fields(T)`，否则报带 GX 坐标的编译错误。
- **构造后立刻解构**：构造字段集 S₁ 由对象字面量层校验 ⊆ `fields(T)`，解构字段集 S₂ 由本层校验 ⊆ `fields(T)`，故 `S₁ ∪ S₂ ⊆ fields(T)`。
- 右值类型未知（如外部包函数返回）→ 宽松生成 `x.field`，字段存在性交由 Go 编译器检查。
- 解构变量遵循 Go 的作用域与 `:=` 重声明规则（同作用域重复声明由 Go 判错）。

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
- **作用域排除**：selector `obj.n`、复合字段键 `{n: ...}`、struct 字段、函数参数、已显式调用 `n()` 均不改写（注：struct 里显式声明的 `sig name T` 字段是另一项特性，见 §11.1）。
- **computed**：惰性求值 + 缓存，仅依赖变化时失效；链式派生正确传播。
- **effect**：立即执行一次，依赖变化时重跑；每次重跑动态重建依赖集合（条件分支依赖可增删）。
- **底层 API 禁止手写**：`Signal.New` / `Signal.Computed` / `Signal.Effect` / `Signal.NewWithEquals` 等首字母大写的运行时构造器是编译器内部实现细节，用户源码里直接调用会报编译错误。GX 只暴露语法糖：`sig n = v` 声明源信号、`Signal.computed(() => expr)` 派生、`Signal.effect(() => {...})` 副作用；迟早需要的自定义相等策略等仍由运行时保留，但不从语言层暴露。这样用户无需感知 sig 与普通变量的区别，也杜绝了手写值与编译器 binding 不一致引发的快照冻结 / 别名冲突 / 二次包装等一系列问题。小写语法糖（含作为函数值传递的 `Signal.effect`）由编译器在末尾还原成对应大写符号，是唯一合法入口。
- **`Signal` 是保留标识符**：作为占位包名的语义优先，勿用作变量名/类型名/字段名。`Signal.New` 形式的底层 API 调用会被拦截；若确需同名局部变量（如 `Signal := Holder{...}`），对其字段的访问（`Signal.New`）会按绑定表放行——判据是「`Signal` 是否解析为包名绑定」，不是纯词法匹配。
- **并发安全**：全局互斥锁保护观察者栈与依赖图，用户回调在锁外执行。
- **读取性能**：依赖追踪只在 effect/computed 重算期间需要，而这类场景是少数；普通读取走**无锁快路径**——用原子计数器 `activeObservers` 判断当前无 observer 重算时，直接原子读取当前值，跳过抓栈（goroutine id 解析）与加锁。sig 值用原子指针存储避免撕裂。实测读取 ~2 ns（约为普通变量的 2 倍），相较抓栈方案（~2900 ns）提速约 1400×；computed 缓存命中读 ~14 ns。
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
