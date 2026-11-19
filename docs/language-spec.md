# GoX 语言规范

GoX（`.gx`）= Go 语法 + 十三项语法糖（pub / enum / JSX / 外部符号还原 / 集合链式 / is 守卫 / 联合类型 / 模板字符串 / 参数默认值 / Lambda / 对象字面量 / 对象解构 / Signal 响应式）。转译产物是标准 Go 源码，交给官方工具链编译。**需要 Go 1.27+**。

## 书写约定（最重要）

> **标识符按 Go 真实拼写书写；全大写缩写（如 `URL`、`StatusOK`）直接写大写、原样保留。声明用 `pub` 显式导出。**

转译器对**小写形式**做机械还原（`upperFirst`），对**已大写的标识符原样保留**，全程不读取第三方包符号表、不做类型推导。因此用别人的 Go 代码时按真实拼写写即可（`http.NewRequest`、`r.URL`、`http.StatusOK`）；也兼容旧的小写写法（`newRequest`、`uRL`、`itoa`），由 `upperFirst` 还原。收益是零依赖、零歧义、行为完全可预测。

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

`<tag>` → `gxvdom.CreateElement(tag, props, children...)` 嵌套调用，是普通 Go 表达式，不含 diff 调度。

| JSX 形态 | 生成 |
|---|---|
| 属性 `class="x"` / `id={name}` | `map[string]any{"class":"x", "id":name}` |
| 无值属性 `<input disabled/>` | `map[string]any{"disabled": true}` |
| 文本 / 表达式子节点 | 字符串字面量 / 表达式参数 |
| `<br/>` 空元素 | `CreateElement("br", map[string]any{})` |
| `<Foo/>` 组件 | `CreateElement(Foo, …)` |

运行时 `runtime/vdom/`：`VNode`（`Element` 为其别名）、`CreateElement`、`Render`、`Diff`。`Diff(old,new)` 产出 `[]Patch`（set-text / set-prop / remove-prop / replace / append / remove），只描述"做什么"，不含调度。

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

`{ name: "x", age: 1 }` → Go 匿名结构体字面量。无期望类型时从值反推字段类型（`"x"`→string、`1`→int、`3.5`→float64、`true`→bool、嵌套 `{...}`→嵌套 struct、否则 any），生成 `struct{ name string; age int }{...}`。有期望类型时（函数实参 / 变量声明 / 赋值 / return——即明确标注参数或返回类型的上下文）省略结构体类型名，用期望类型补全为 `point{...}` 并校验字段：未知字段 / 缺失字段 / 类型不兼容均报带 GX 坐标的编译错误。无类型常量按 Go 可赋值规则放宽。支持嵌套（由内向外逐层补全）。期望类型为 `any` / `interface{}` 时视为无有效期望类型（不携带字段结构，`any{...}` 非法），按匿名结构体处理。

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
- **import**：`Signal` 占位包名由 ImportManager 统一解析为 `import Signal "<module>/runtime/signal"`；源码已用别名导入则复用。

## 12. sourcemap / LSP / 编译检查

- `.gx ↔ .go` 双向位置映射（最近邻），可 JSON 序列化。
- LSP：initialize / didOpen / didChange / definition / hover / completion / documentSymbol / rename，诊断经 sourcemap 回投到 `.gx`。
- 编译检查：真实 `go build`（可取消 context + LRU 缓存），诊断回投到 `.gx` 行号。支持注入 go.mod `replace` 以解析本地运行时。

## 与 Go 的关系

- gx 标识符书写规则≠Go（大小写语义由 `pub` 与约定承担），但语法、类型系统、标准库、工具链完全复用。
- 生成代码是标准 `.go`，可与其他 Go 代码混编、提交、调试。
- 渐进迁移：已大写的 Go 标识符在 gx 中原样保留。
