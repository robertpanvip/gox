# GX 语言规范

GX（后缀 `.gx`）= 标准 Go 语法 + 四项语法糖。转译器把它降级为**等价、可读、可直接 `go build` 的 Go 源码**，并产出双向 sourcemap。

> 心智负担只有一条：**一律写小写**。自己的符号用 `pub` 声明（决定导出），外部的符号（包级函数、字段、方法）由转译器自动还原真实大小写。极少数歧义情况 IDE 会提示，那时写真实大小写兜底。

## 1. 可见性 `pub`

Go 用首字母大小写决定导出；GX 反过来——统一小写书写，`pub` 显式标记公开。转译时 `pub` 目标的首字母大写，并在**所有引用处**同步还原。

```gx
pub type point struct {
    pub x int   // → X int（导出）
    y int       // → y int（私有）
}
pub const max_size = 100          // → const Max_size = 100
pub func greet() {}               // → func Greet() {}
func main() { greet() }           // → Greet()（引用还原）
```

`pub` 可作用于：`func`（含方法，会跳过接收者找方法名）、`type`、`var`、`const`（含分组）、`enum`、struct 字段、interface 方法。

## 2. 枚举 `enum`

```gx
pub enum color { red, green, blue }
enum status { pending = 1, active = 2 }
```

```go
type Color int
const (
    Red   Color = iota
    Green Color = iota
    Blue  Color = iota
)
type status int
const (
    pending status = 1
    active  status = 2
)
```

- `pub enum` → 类型名与成员均导出；无 `pub` → 全部私有。
- 成员用 `=` 显式赋值；成员间以逗号或换行分隔。
- 无显式值的成员用 `iota`（按成员序号）。

## 3. JSX

任何 `<tag>...</tag>` 片段被转成 `gxvdom.createElement` 的**嵌套调用**（不实现 diff）。

```gx
return <div class="card" id={name}>
    hello {name}
    <span>world</span>
    <br/>
</div>
```

```go
return gxvdom.CreateElement("div", map[string]any{"class": "card", "id": name},
    "hello", name,
    gxvdom.CreateElement("span", nil, "world"),
    gxvdom.CreateElement("br", nil))
```

| JSX 形态 | 生成 |
|---|---|
| 字符串属性 `class="x"` | `map[string]any{"class": "x"}` |
| 表达式属性 `id={name}` | `map[string]any{"id": name}` |
| 无值属性 `disabled` | `map[string]any{"disabled": true}` |
| 文本子节点 | 字符串字面量（纯缩进空白被丢弃） |
| 表达式子节点 `{expr}` | 直接作为 child 参数 |
| 嵌套元素 | 递归 `CreateElement` |
| 自闭合 `<br/>` | `CreateElement("br", props)` |
| 大写/含点标签 `<Foo/>` | 组件引用（不加引号） |

遇到 JSX 时转译器自动注入 `import gxvdom "github.com/robertpanvip/gox/vdom"`。运行时 `Render` 把 Element 树渲染为 HTML。

## 4. 导入 Go 与小写调用

GX 认为调用方一律小写更自然。转译器从 `import` 收集包名，用 `go/importer` 读取每个包的**导出符号表**（包级符号 + 结构体字段 + 接口/类型方法），把 selector 右侧的小写名还原为真实大小写。

```gx
import (
    "fmt"
    "net/http"
    "net/url"
)

req, _ := http.newrequest("GET", "http://x", nil)
u, _ := url.parse("http://x")
fmt.println(req.method, u.host, u.path)
```

```go
req, _ := http.NewRequest("GET", "http://x", nil)
u, _ := url.Parse("http://x")
fmt.Println(req.Method, u.Host, u.Path)
```

### 还原规则

| 场景 | 查表方式 | 歧义 |
|---|---|---|
| `http.newrequest`（左侧是包名） | 精确查 `net/http` 符号表 | 零歧义 |
| `req.method`（左侧是变量） | 查全局唯一表 | 极小：只有同一小写名对应多个真实名才回退 |

- **只作用于 selector 右侧**：裸标识符 `println("x")` 不会被误改。
- **已大写不动**：`fmt.Println` 原样保留，可渐进迁移。
- **别名导入**：`import h "net/http"` → `h.newrequest` 正常还原。
- **歧义回退**：同一小写名映射到多个真实名（如 `Foo`/`FOO`）时丢弃该映射，需写真实大小写。
- **失败降级**：第三方包没 `go build` 过（无 export data）时跳过该包，不影响其它包。

## 5. 集合链式调用 `map`/`filter`/`reduce`

依赖 **Go 1.27 泛型方法**（方法可声明自己的类型参数），`Map`/`Reduce` 可换型并保持链式。回调是 Go 原生闭包，原样透传。

```gx
nums := []int{1, 2, 3, 4, 5, 6}

// 换型：int → string，再过滤
labels := nums.map(strconv.itoa).filter(func(s string) bool { return len(s) == 1 })

// 换累加器类型 + 链式
total := nums.filter(even).reduce(0, func(acc int, x int) int { return acc + x })
```

```go
nums := gxcollections.List[int]{1, 2, 3, 4, 5, 6}
labels := nums.Map(strconv.Itoa).Filter(func(s string) bool { return len(s) == 1 })
total := nums.Filter(even).Reduce(0, func(acc int, x int) int { return acc + x })
```

- **按需提升**：只有被 `map`/`filter`/`reduce` 调用的接收者变量，其声明处的 `[]T` 才提升为 `gxcollections.List[T]`；其它切片保持原生 `[]T`。`List[E]` 与 `[]E` 双向可直接赋值/传参。
- 自动注入 `import "github.com/robertpanvip/gox/gxcollections"`。
- `Map[R any]`、`Reduce[R any]` 换型；`Filter` 同型。

## 6. go.mod

必须 `go 1.27`（泛型方法所需）。
