# GoX 语言规范

GoX（`.gx`）= Go 语法 + 十三项语法糖（pub / enum / JSX / 外部符号还原 / 集合链式 / is 守卫 / 联合类型 / 模板字符串 / 参数默认值 / Lambda / 对象字面量 / 对象解构 / Signal 响应式）。转译产物是标准 Go 源码，交给官方工具链编译。**需要 Go 1.27+**。

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

运行时 `runtime/scene/`：`Template` / `Op` / `Render` / `Instance`。`Render` 缓存静态骨架、clone 出常驻场景树；`Instance.Update()` 只回写变化的插槽（可观测指标 `Writes`），不含调度。host 不定义绘制后端接口：GUI 后端实现 `gxapp.Window`，可用 `Node.ID` / `Node.Layout` / `Changes()` 做增量绘制；内置 `TextBackend` 只是文本工具。

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

`signal` 声明源信号，`Signal.computed` 派生只读信号，`Signal.effect` 注册副作用；依赖变化时 effect 自动重跑。底层为函数式 signal（`Signal[T] = func(...T) T`，无参读、带参写）+ 依赖追踪运行时 `runtime/signal`。

```gx
signal count = 0
let doubled = Signal.computed(() => count * 2)   // → Signal[int] 派生信号
Signal.effect(() => println(count))               // → func(){ println(count()) }，count 变化时重跑
count = 5                                          // → count(5)，触发依赖失效
println(doubled)                                   // → println(doubled())，惰性重算得 10
```

- **声明**：`signal n = v` → `var n = Signal.New(v)`；`let n = v` → `n := v`（let 即短声明，无新语义）。
- **读写改写**：signal 变量的读（裸标识符）自动补 `()`；写改写 `n = v` → `n(v)`、`n += v` → `n(n() + v)`、`n++` → `n(n() + 1)`。
- **作用域排除**：selector `obj.n`、复合字段键 `{n: ...}`、struct 字段、函数参数、已显式调用 `n()` 均不改写。
- **computed**：惰性求值 + 缓存，仅依赖变化时失效；链式派生正确传播。
- **effect**：立即执行一次，依赖变化时重跑；每次重跑动态重建依赖集合（条件分支依赖可增删）。
- **并发安全**：全局互斥锁保护观察者栈与依赖图，用户回调在锁外执行。
- **读取性能**：依赖追踪只在 effect/computed 重算期间需要，而这类场景是少数；普通读取走**无锁快路径**——用原子计数器 `activeObservers` 判断当前无 observer 重算时，直接原子读取当前值，跳过抓栈（goroutine id 解析）与加锁。signal 值用原子指针存储避免撕裂。实测读取 ~2 ns（约为普通变量的 2 倍），相较抓栈方案（~2900 ns）提速约 1400×；computed 缓存命中读 ~14 ns。
- **import**：`Signal` 占位包名由 ImportManager 统一解析为 `import Signal "<module>/runtime/signal"`；源码已用别名导入则复用。

## 12. sourcemap / LSP / 编译检查

- `.gx ↔ .go` 双向位置映射（最近邻），可 JSON 序列化。
- LSP：initialize / didOpen / didChange / definition / hover / completion / documentSymbol / rename，诊断经 sourcemap 回投到 `.gx`。
- 编译检查：真实 `go build`（可取消 context + LRU 缓存），诊断回投到 `.gx` 行号。支持注入 go.mod `replace` 以解析本地运行时；跨文件项目模式下，多份产物一起 `go build`，诊断按产物文件回投到**对应 `.gx`（含子目录包，如 `util/math.gx`）**的正确行列。

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
