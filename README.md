# gox — 编译到 Go 的薄语法层语言

`gox`（源码后缀 `.gx`）是一门**以 Go 为基底**的语言：所有标准 Go 语法原样可用，在此之上只增加四项语法糖。转译器把 `.gx` 降级为**等价、可读、可 go build 的标准 Go 源码**，并产出双向 sourcemap。

> 设计原则：**零运行时魔法**。JSX 只是嵌套的函数调用，枚举只是 `type + const + iota`，`pub` 只是可见性首字母改写，集合链式只是 `List[T]` 上的泛型方法。生成的代码可以直接提交、阅读、调试。
>
> **心智负担只有一条：一律写小写。** 自己的符号用 `pub` 声明，外部符号由转译器自动还原大小写。

## 五项特性速览

| 特性 | GX 写法 | 生成的 Go |
|---|---|---|
| **pub 可见性** | `pub func greet(){}` | `func Greet(){}`（引用处同步还原） |
| **枚举** | `pub enum color { red, green }` | `type Color int` + `const ( Red Color = iota; Green )` |
| **JSX** | `<div class="box">hi {name}</div>` | `gxvdom.CreateElement("div", map[string]any{"class":"box"}, "hi", name)` |
| **外部符号小写调用** | `import "fmt"; fmt.println("x")` | `fmt.Println("x")`（`go/importer` 精确还原） |
| **集合链式** | `nums.map(f).filter(g)` | `List[T].Map(f).Filter(g)`（Go 1.27 泛型方法，可换型） |

## 1. 可见性：`pub`

Go 用**标识符首字母大小写**决定导出；gx 反过来——**统一用小写书写**，用 `pub` 显式声明公开。转译器在进入公开边界时把首字母大写。

```gx
package demo

pub func greet() {}          // → func Greet() {}
func helper() {}             // → func helper() {}   （私有，保持小写）

pub type point struct {      // → type Point struct {
    pub x int                // →     X int
    y int                    // →     y int
}

pub const max_size = 10      // → const Max_size = 10
```

规则：`pub` 作用于紧随其后的标识符（`func`/`type`/`var`/`const`/`enum`/struct 字段/interface 方法名）。已大写的标识符保持不动。

## 2. 枚举：`enum`

```gx
pub enum color { red, green, blue }
```

```go
type Color int
const (
    Red Color = iota
    Green
    Blue
)
```

- `pub enum` → 类型名与成员同时导出；`enum`（无 pub）→ 全部私有。
- 成员可用 `=` 显式赋值：`enum code { ok = 200, fail = 500 }`。
- 成员可用逗号或换行分隔。

## 3. JSX

任何 `<tag>` 形态的片段都会被转成 `gxvdom.createElement(tag, props, children...)` 的**嵌套调用**。不实现 diff、不引入虚拟树调度——生成的就是普通 Go 表达式。

```gx
pub func render(name string) any {
    return <div class="card" id={name}>
        hello {name}
        <span>world</span>
    </div>
}
```

生成（等价）：

```go
func Render(name string) any {
    return gxvdom.createElement("div", map[string]any{"class": "card", "id": name},
        "hello ", name,
        gxvdom.createElement("span", nil, "world"),
    )
}
```

| JSX 形态 | 生成 |
|---|---|
| `<div class="x">` | `createElement("div", map[string]any{"class":"x"}, ...)` |
| 无值属性 `<input disabled/>` | `map[string]any{"disabled": true}` |
| 表达式属性 `id={name}` | `map[string]any{"id": name}` |
| 表达式子节点 `{name}` | 作为 child 参数 `name` |
| 文本子节点 | 字符串字面量（纯缩进空白被丢弃） |
| 大写/含点标签 `<Foo/>` | 组件引用 `createElement(Foo, ...)`（不加引号） |

运行时在 [`vdom/`](vdom/) 包（`github.com/robertpanvip/gox/vdom`），导出 `CreateElement` 与 `Render`。转译器遇到 JSX 会自动注入该 import。

## 4. 导入 Go 包与小写调用

gx 认为「调用方一律小写开头更自然」。转译器从 `import` 收集包名，用标准库 `go/importer` 读取每个包的**导出符号表**（包级符号 + 结构体字段 + 方法），把 selector 右侧的小写名还原为真实大小写：

```gx
import (
    "fmt"
    "net/http"
    "net/url"
)

pub func demo() {
    req, _ := http.newrequest("GET", "http://x", nil)
    u, _ := url.parse("http://x")
    fmt.println(req.method, u.host, u.path)
}
```

```go
req, _ := http.NewRequest("GET", "http://x", nil)
u, _ := url.Parse("http://x")
fmt.Println(req.Method, u.Host, u.Path)
```

还原规则：

| 场景 | 查表 | 歧义 |
|---|---|---|
| `http.newrequest`（左侧是包名） | 精确查 `net/http` 符号表 | 零歧义 |
| `req.method`（左侧是变量） | 查全局唯一表 | 极小，撞名则回退 |

- 只作用于 selector 右侧，裸标识符 `println("x")` 不动；
- 已写大写的不动，可渐进迁移；
- 别名导入 `import h "net/http"` 支持；
- 第三方包未 `go build`（无 export data）时静默跳过。

## 5. 集合链式调用

依赖 **Go 1.27 泛型方法**（方法可声明自己的类型参数），`Map`/`Reduce` 可换型并保持链式，回调是原生闭包、原样透传：

```gx
nums := []int{1, 2, 3, 4, 5, 6}
labels := nums.map(strconv.itoa).filter(func(s string) bool { return len(s) == 1 })
total := nums.filter(even).reduce(0, func(acc int, x int) int { return acc + x })
```

```go
nums := gxcollections.List[int]{1, 2, 3, 4, 5, 6}
labels := nums.Map(strconv.Itoa).Filter(func(s string) bool { return len(s) == 1 })
total := nums.Filter(even).Reduce(0, func(acc int, x int) int { return acc + x })
```

- **按需提升**：只有被 `map`/`filter`/`reduce` 调用的接收者，其 `[]T` 才提升为 `gxcollections.List[T]`；其它切片保持原生。
- `go.mod` 必须为 `go 1.27`。运行时见 [`gxcollections/`](gxcollections/)。

## 目录结构

```
gox/
├── cmd/gox/          CLI: build / jsx / pub / lsp-map / sourcemap / map-lookup / lsp / test
├── cmd/gox-lsp/      独立 LSP server 入口（stdio）
├── pkg/
│   ├── compiler/     转译引擎（token / lexer / enum / pub / extsym / external /
│   │                    collections / jsx / pipeline）
│   └── sourcemap/     双向位置映射（无业务依赖）
├── internal/
│   ├── lsp/           LSP 协议 / server / 诊断 / 符号映射
│   └── buildcheck/    真实 go build 检查（可取消 + LRU）
├── bindings/cabi/     C-ABI 共享库（go build -buildmode=c-shared，供 IDEA JNA）
├── vdom/              createElement 运行时
├── gxcollections/     List[T] + 泛型 Map/Filter/Reduce 运行时
├── examples/ testdata/ docs/
```

详细文档：[语言规范](docs/language-spec.md) · [架构](docs/architecture.md) · [JNA 集成](docs/cabi.md)

## 快速开始

> 需 Go 1.27（泛型方法）。若本机工具链较旧，`GOTOOLCHAIN=auto` 会自动下载。

```bash
# 转译单个文件
go run ./cmd/gox build examples/hello.gx -o /tmp/hello.go

# 仅查看单步转换
go run ./cmd/gox jsx         examples/jsx.gx
go run ./cmd/gox pub         examples/pub.gx
go run ./cmd/gox collections examples/collections.gx

# 查看 sourcemap / 反查位置
go run ./cmd/gox sourcemap examples/hello.gx
go run ./cmd/gox map-lookup <map.json> <goLine> <goCol>

# 启动 LSP（stdio）
go run ./cmd/gox-lsp

# 构建 C-ABI 共享库
CGO_ENABLED=1 go build -buildmode=c-shared -o libgox.so ./bindings/cabi

# 内置自检 / 全量测试
go run ./cmd/gox test
go test ./...
```
