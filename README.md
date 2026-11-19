# GoX（GX）— 编译到 Go 的薄语法层语言

`gox`（源码后缀 `.gx`）是一门**以 Go 为基底**的语言：所有标准 Go 语法原样可用，在此之上增加语法糖。转译器把 `.gx` 降级为**等价、可读、可 go build 的标准 Go 源码**，并产出双向 sourcemap。**需要 Go 1.27+**（集合方法使用泛型方法）。

> 设计原则：**零运行时魔法**。JSX 只是嵌套的函数调用，枚举只是具名类型 + const + String/Parse，集合只是切片类型提升 + 泛型方法。生成代码可直接提交、阅读、调试。
>
> **书写约定核心：声明用 `pub` 显式导出；标识符一律「只首字母小写，其余驼峰照抄」。** 转译器机械还原大小写，无需类型推导、无需读取第三方包符号表。

## 七项特性速览

| 特性 | GX 写法 | 生成的 Go |
|---|---|---|
| **pub 导出** | `pub func greet(){}` | `func Greet(){}`（引用处作用域感知还原） |
| **枚举** | `pub enum color { red, green }` | `type Color int` + `const(ColorRed…)` + `String()`/`ParseColor()` |
| **JSX** | `<div class="box">hi</div>` | `gxvdom.CreateElement("div", …)` 嵌套 |
| **外部符号** | `http.newRequest`、`uRL` | `http.NewRequest`、`URL`（机械首字母还原） |
| **集合链式** | `xs.map(f).filter(g)` | `list.List[T].Map(f).Filter(g)`，切片 `[]T` 全局提升 |
| **is 类型守卫** | `if (x is float64) { … }` | `if v, ok := x.(float64); ok { x := v; … }`（块内 x 窄化为 float64） |
| **联合类型** | `func f(v int \| string)` | `func f(v any)`（擦除为 any，配合 is 守卫窄化） |

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

`<tag>` 转成 `gxvdom.CreateElement(tag, props, children...)` 嵌套调用。运行时 [`vdom/`](vdom/) 提供 `VNode` / `CreateElement` / `Render` / `Diff`（Diff 产出结构化补丁，不含调度）。

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

## 4. 外部 Go 符号：只首字母小写

**约定：按 Go 真实名字照抄，只把首字母改成小写。** 还原因此是机械的 `upperFirst`，零依赖、零歧义。

| GX 书写 | 生成 Go | 说明 |
|---|---|---|
| `http.newRequest(...)` | `http.NewRequest(...)` | 多词保留驼峰 |
| `uRL` | `URL` | 全大写缩写只压首字母 |
| `fmt.println(...)` | `fmt.Println(...)` | 单词 |
| `req.method` / `u.host` | `req.Method` / `u.Host` | 外部结构体字段 |

- **作用域感知**：自己的私有字段/变量（同样小写书写）不会被误改——转译器收集本文件的私有符号加以排除。
- 裸标识符（内置 `println`/`len`/`make` 等）不碰；已写大写的不动。
- 别名导入 `import h "net/http"` 支持。
- 代价：需按 Go 真实名字照抄驼峰（`Itoa` 要写 `itoa`，不能写 `itoA`）。

## 5. 集合链式调用

`[]T` **全局提升**为 `list.List[T]`，直接获得链式方法。依赖 **Go 1.27 泛型方法**，`Map`/`Reduce` 可换型，回调是原生闭包、原样透传。

```gx
nums := []int{1, 2, 3, 4, 5, 6}
labels := nums.map(strconv.itoa).filter(func(s string) bool { return len(s) == 1 })
total := nums.filter(even).reduce(0, func(acc, x int) int { return acc + x })
```

- `List[T]` 与 `[]T` 底层相同，可双向赋值；标准库返回的 `[]T` 也能用方法。
- **不误伤**：数组 `[3]T`、索引 `a[i]`、切片表达式 `a[1:2]`、`map[K]V`、`Foo[T]` 都不是切片类型形态，不会被提升。
- 用 `-module` 指定运行时 import 路径（默认 `github.com/robertpanvip/gox` → `.../list`）。运行时见 [`list/`](list/)。

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
- 支持 `else if` 链与嵌套；`else` 分支内 `x` 保持原类型。
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
  - **守卫在运行时**，不是编译期——传错类型能编译，但调用时立即 panic；
  - **无穷尽性检查**：联合有 3 个成员但只守卫 2 个，编译器不会提示遗漏；
  - `any` 上不能直接运算，须先 is 守卫窄化；
  - 不处理 interface/struct 内联、函数类型签名内部、以及「嵌套联合」（`[]int|string` 按 Go 优先级解析为 `([]int)|string`，整段擦除）。
- 若需要真正的编译期类型安全，后续可升级为泛型约束方案（`[T int|string]`）。

## 8. Sourcemap / LSP / 编译检查

- 自带 `.gx ↔ .go` **双向精确位置映射**（`sourcemap` / `map-lookup`）。
- LSP：跳转 / 补全 / 悬停 / 文档符号 / **重命名**（枚举成员三种拼写派生新名）。
- 真实 `go build` 检查（`buildcheck`，可取消 + LRU），错误经 sourcemap **回投到 `.gx`**。

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
│   ├── lsp/            LSP 协议 / server / 诊断 / 符号映射 / 重命名
│   └── buildcheck/     真实 go build 检查（可取消 + LRU）
├── bindings/cabi/      C-ABI 共享库（供 IDEA 插件经 JNA 调用）
├── examples/           .gx 示例
├── testdata/           golden 产物 + LSP 端到端驱动脚本
├── docs/               设计与集成文档
├── vdom/               VNode / CreateElement / Render / Diff
└── list/               List[T] + 泛型 Map/Filter/Reduce 运行时
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
