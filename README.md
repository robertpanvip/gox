# GoX（GX）— 编译到 Go 的薄语法层语言

`gox`（源码后缀 `.gx`）是一门**以 Go 为基底**的语言：所有标准 Go 语法原样可用，在此之上增加语法糖。转译器把 `.gx` 降级为**等价、可读、可 go build 的标准 Go 源码**，并产出双向 sourcemap。**需要 Go 1.27+**（集合方法使用泛型方法）。

> 设计原则：**零运行时魔法**。JSX 只是嵌套的函数调用，枚举只是具名类型 + const + String/Parse，集合只是切片类型提升 + 泛型方法。生成代码可直接提交、阅读、调试。
>
> **书写约定核心：声明用 `pub` 显式导出；标识符按 Go 真实拼写书写，全大写缩写（如 `URL`）直接写大写、原样保留。** 小写形式兼容旧写法、机械还原，无需类型推导、无需读取第三方包符号表。

## 十一项特性速览

| 特性 | GX 写法 | 生成的 Go |
|---|---|---|
| **pub 导出** | `pub func greet(){}` | `func Greet(){}`（引用处作用域感知还原） |
| **枚举** | `pub enum color { red, green }` | `type Color int` + `const(ColorRed…)` + `String()`/`ParseColor()` |
| **JSX** | `<div class="box">hi</div>` | `gxvdom.CreateElement("div", …)` 嵌套 |
| **外部符号** | `http.NewRequest`、`URL` | 原样保留；小写形式 `http.newRequest`、`newRequest` 机械还原 |
| **集合链式** | `xs.map(f).filter(g)` | `list.List[T].Map(f).Filter(g)`，切片 `[]T` 全局提升 |
| **is 类型守卫** | `if (x is float64) { … }` | `if v, ok := x.(float64); ok { x := v; … }`（块内 x 窄化为 float64） |
| **联合类型** | `func f(v int \| string)` | `func f(v any)`（擦除为 any，配合 is 守卫窄化） |
| **参数默认值** | `func f(x int = 5)` | 调用点补全：`f()` → `f(5)`（TS 语义，区分未传与零值） |
| **Lambda** | `x => x+1` | `func(x int) int { return x+1 }`（类型从上下文/表达式推断） |
| **模板字符串** | `` `hi ${name}` `` | `"hi " + fmt.Sprint(name)`（拼接，自动 import fmt） |
| **对象字面量** | `{ name: "x", age: 1 }` | `struct{ name string; age int }{...}`，有期望类型时用 `point{...}` |
| **对象解构** | `{ name, age } := p` / `var { name, age } = p` | `name := p.name; age := p.age`（逐字段，校验字段存在） |

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

`<tag>` 转成 `gxvdom.CreateElement(tag, props, children...)` 嵌套调用。运行时 [`runtime/vdom/`](runtime/vdom/) 提供 `VNode` / `CreateElement` / `Render` / `Diff`（Diff 产出结构化补丁，不含调度）。

```gx
pub func render(name string) any {
    return <div class="card" id={name}>hello {name}<span>world</span></div>
}
```

| JSX 形态 | 生成 |
|---|---|
| `<div class="x">` | `CreateElement("div", map[string]any{"class":"x"}, …)` |
| `<input disabled/>` | `map[string]any{"disabled": true}` |
| `{name}` 表达式子节点 | 作为 child 参数 `name` |
| `<Foo/>` 组件 | `CreateElement(Foo, …)`（不加引号） |

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
type point struct { name string; age int }
var p point = { name: "x", age: 1 }   // → var p point = point{ name: "x", age: 1 }
f({ name: "x", age: 1 })              // f(p point) → f(point{...})
p = { name: "x", age: 1 }             // 赋值：按 p 的声明类型补全
func make() point { return { name: "x", age: 1 } }  // 按返回类型补全
```

- 校验：字段名未知、缺失字段、值类型不兼容均报**编译错误**（带 GX 源码坐标）。
- 无类型常量按 Go 可赋值规则放宽（整数字面量可赋给整数/浮点族）。
- **期望类型为 `any` / `interface{}` 时不补全**：这类类型不携带字段结构，`any{...}` 是非法 Go，故按匿名结构体处理（如 `func make() any { return { name: "x" } }` → `return struct{ name string }{ name: "x" }`）。
- 支持嵌套对象字面量（由内向外逐层补全）。

## 12. 对象解构

`{ name, age } := p` / `var { name, age } = p` 把结构体字段解构为同名的局部变量，逐字段生成 Go 声明：

```gx
type student struct { name string; age int; city string }
var p student = { name: "x", age: 1, city: "SZ" }
{ name, age } := p        // → name := p.name; age := p.age
var { name, city } = q    // → var name = q.name; var city = q.city
```

- **类型检查**：右值类型可从本文件推断时（具名 struct 或匿名 struct 字面量），每个解构字段必须真实存在，否则报**编译错误**（带 GX 坐标）。
- **构造后立刻解构**：构造侧字段集 S₁ 由对象字面量层校验，解构侧 S₂ 由本层校验，二者都是 `fields(T)` 的子集，故 `S₁ ∪ S₂ ⊆ fields(T)`。
- **右值类型未知**（如外部包函数返回）：宽松生成 `x.field`，字段存在性交给 Go 编译期检查。
- 复用 `var`/`:=` 语法，不引入新关键字；解构变量遵循 Go 的作用域与 `:=` 重声明规则。

## 13. Sourcemap / LSP / 编译检查

- 自带 `.gx ↔ .go` **双向精确位置映射**（`sourcemap` / `map-lookup`）。
- LSP：跳转 / 补全 / 悬停 / 文档符号 / **重命名**（枚举成员三种拼写派生新名）。
- 真实 `go build` 检查（`buildcheck`，可取消 + LRU），错误经 sourcemap **回投到 `.gx`**。

## 14. GX 类型检查（方案 B：AST + side table）

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
├── examples/           .gx 示例
├── testdata/           golden 产物 + LSP 端到端驱动脚本
├── docs/               设计与集成文档
└── runtime/            运行时组件（统一收纳，便于未来扩展）
    ├── vdom/           VNode / CreateElement / Render / Diff
    └── list/           List[T] + 泛型 Map/Filter/Reduce 运行时
```

分层：`pkg` 是对外稳定 API，`internal` 仅供本仓库 cmd 使用；IDEA 插件等前端通过 `bindings/cabi` 或直接 import `pkg` 接入，不碰 `internal`。

详细文档：[语言规范](docs/language-spec.md) · [架构](docs/architecture.md) · [JNA 集成](docs/CABI.md)

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
gox lsp-map ./examples                            # 项目符号 JSON
gox lsp                                           # 启动 LSP
```
