# Gox Language Specification (v0.3 Draft)

## 1. 语言定位

Gox 是一门基于 Go 的语法增强语言（DSL），通过转译生成标准 Go 代码。

### 设计目标

* 提供更现代、直观的语法（类似 TypeScript / Dart / Swift）
* 保持 100% 兼容 Go 生态
* 不引入新的运行时
* 不改变 Go 的核心语义（并发 / 内存 / error）

---

## 2. 核心特性概览

| 特性               | 状态      |
| ---------------- | ------- |
| public / private | ✅       |
| let / const      | ✅       |
| 数组 T[]           | ✅       |
| try / catch      | ✅       |
| nullable (T?)    | ✅       |
| ?? / ?.          | ✅       |
| named params     | ✅       |
| extend           | ✅       |
| async/await      |  ✅      |
| 闭包 (Closure)    | ✅       |
| 模板字符串        | ✅       |
| 数组字面量        | ✅       |
| 泛型             | ✅       |
| print/println    | ✅       |
| struct mixed     | ✅       |
| interface        | ✅       |
| if/else          | ✅       |
| for/while        | ✅       |
| switch/when      | ✅       |
| break/continue   | ✅       |
| TSX              | ✅       |
| 结构体字面量简写   | ✅       |
| Go 模块导入支持    | ✅       |

---

## 3. 可见性（Visibility）

### Gox

```
public struct User {
    public name string
    private age int
}

private func helper() {}
```

### 转换规则

| Gox     | Go    |
| ------- | ----- |
| public  | 首字母大写 |
| private | 首字母小写 |

---

## 4. 变量声明

### Gox

```
let a: int = 10
const b: string = "hi"
```

### Go

```
a := 10
const b string = "hi"
```

---

## 5. 类型系统

### 基础类型

保持 Go 原生：

```
int
string
bool
float64
```

---

## 6. 数组（TS 风格）

### Gox

```
let a: int[] = [1,2,3]
let b: string[]
```

### Go

```
a := []int{1,2,3}
var b []string
```

---

## 7. 函数定义

### Gox

```
public func add(a: int, b: int): int {
    return a + b
}
```

### Go

```
func Add(a int, b int) int {
    return a + b
}
```

---

## 7.1 闭包（Closure）

### 设计原则

* 类似 TypeScript 的箭头函数语法
* 捕获外部变量（闭包语义）
* 编译为 Go 的匿名函数

---

### 7.1.1 基本语法

#### Gox

```
let add = func(a: int, b: int): int {
    return a + b
}
```

#### Go

```
add := func(a int, b int) int {
    return a + b
}
```

---

### 7.1.2 箭头函数（单表达式）

#### Gox

```
let add = (a: int, b: int): int => a + b
let greet = (name: string): string => "hello " + name
```

#### Go

```
add := func(a int, b int) int { return a + b }
greet := func(name string) string { return "hello " + name }
```

---

### 7.1.3 闭包捕获外部变量

#### Gox

```
let x = 10
let adder = (y: int): int => x + y
x = 20
let result = adder(5)  // result = 25
```

#### Go

```
x := 10
adder := func(y int) int {
    return x + y
}
x = 20
result := adder(5)  // result = 25 (闭包捕获的是 x 的引用)
```

---

### 7.1.4 高阶函数

#### Gox

```
let numbers = [1, 2, 3, 4, 5]
let doubled = numbers.map((n: int): int => n * 2)
let evens = numbers.filter((n: int): bool => n % 2 == 0)
```

#### Go

```
numbers := []int{1, 2, 3, 4, 5}
doubled := slices.Map(numbers, func(n int) int { return n * 2 })
evens := slices.Filter(numbers, func(n int) bool { return n%2 == 0 })
```

---

### 7.1.5 类型推断

#### Gox

```
let add = (a, b) => a + b  // 推断为 (int, int) => int
let greet = (name) => "hello " + name  // 推断为 (string) => string
```

#### Go

```
add := func(a, b int) int { return a + b }
greet := func(name string) string { return "hello " + name }
```

---

### 7.1.6 规则

* `func` 关键字用于定义完整的匿名函数
* `=>` 箭头用于单表达式函数的简写
* 闭包捕获的是变量的引用（与 Go 语义一致）
* 类型标注可选，编译器可进行类型推断

---

## 7.2 泛型（Generics）

### 设计原则

* 类似 TypeScript 的泛型语法
* 编译为 Go 1.18+ 泛型
* 支持类型约束

---

### 7.2.1 基本语法

#### Gox

```
public func identity[T](x: T): T {
    return x
}
```

#### Go

```
func Identity[T any](x T) T {
    return x
}
```

---

### 7.2.2 类型约束

#### Gox

```
public func print[T any](x: T) {
    fmt.Println(x)
}

public func add[T Number](a: T, b: T): T {
    return a + b
}
```

#### Go

```
func Print[T any](x T) {
    fmt.Println(x)
}

func Add[T Number](a T, b T) T {
    return a + b
}
```

---

### 7.2.3 多类型参数

#### Gox

```
public func pair[T, U](t: T, u: U): [T, U] {
    return [t, u]
}
```

#### Go

```
func Pair[T any, U any](t T, u U) [2]interface{} {
    return [2]interface{}{t, u}
}
```

---

---

## 7.3 结构体方法（Go Receiver 风格）（新增 ✨）

### 设计原则

* Go 风格的结构体方法（在外部定义）
* 使用 receiver 语法
* 编译为 Go 的方法

---

### 7.3.1 基本语法

#### Gox

```
public struct User {
    public name: string
    private age: int
}

// 在结构体外部定义方法
public func (u: User) getName(): string {
    return u.name
}

public func (u: User) setAge(age: int) {
    u.age = age
}
```

#### Go

```
type User struct {
    Name string
    age int
}

func (u User) GetName() string {
    return u.Name
}

func (u User) SetAge(age int) {
    u.age = age
}
```

### 7.3.2 说明

* Receiver 语法：`func (receiver: Type) methodName(...)`
* Receiver 在函数定义的最前面，用括号包裹
* 方法名遵循 visibility 规则（public 首字母大写，private 首字母小写）
* Receiver 变量可以在方法体内访问结构体字段

---

## 7.4 结构体组合（mixed）（新增 ✨）

### 设计原则

* Go 风格的嵌入（embedding）
* 使用 `mixed` 关键字
* 实现组合而非继承

---

### 7.4.1 基本语法

#### Gox

```
public struct Base {
    public value: int
}

// 使用 mixed 关键字嵌入其他结构体
public struct Derived mixed Base {
    public name: string
}

// 支持多个嵌入
public struct C mixed A mixed B {
    public c: float64
}
```

#### Go

```
type Base struct {
    Value int
}

type Derived struct {
    Base
    Name string
}

type C struct {
    A
    B
    c float64
}
```

### 7.4.2 说明

* `mixed` 关键字用于嵌入其他结构体
* 嵌入的结构体字段和方法可以直接访问
* 类似 Go 的匿名嵌入字段
* 支持嵌入多个结构体

### 7.4.3 结构体字面量（新增 ✨）

#### Gox

```
// 带字段名（推荐）
let user = User{
    name: "Alice",
    age: 25
}

// 位置初始化（按字段顺序）
let point = Point{10, 20}

// 混合使用
let rect = Rect{
    x: 0,
    y: 0,
    width: 100,
    height: 200
}

// 使用表达式
let x = 10
let y = 20
let point2 = Point{x: x, y: y}
let point3 = Point{x + 5, y + 10}

// 嵌套结构体
let container = Container{
    user: User{name: "Bob", age: 30},
    point: Point{100, 200}
}
```

#### Go

```
// 带字段名
user := User{
    Name: "Alice",
    Age: 25,
}

// 位置初始化
point := Point{10, 20}

// 混合使用
rect := Rect{
    X: 0,
    Y: 0,
    Width: 100,
    Height: 200,
}

// 使用表达式
x := 10
y := 20
point2 := Point{X: x, Y: y}
point3 := Point{x + 5, y + 10}

// 嵌套结构体
container := Container{
    user: User{Name: "Bob", Age: 30},
    point: Point{100, 200},
}
```

**说明**:
- 支持带字段名的初始化（推荐）
- 支持位置初始化（按字段顺序）
- 字段名会自动转换大小写（public 字段首字母大写）
- 支持嵌套结构体
- 支持表达式作为字段值

---

## 7.5 接口（interface）（新增 ✨）

### 设计原则

* Go 风格的接口
* 隐式实现（不需要 implements 关键字）
* 接口可以组合其他接口

---

### 7.5.1 基本语法

#### Gox

```
public interface Writer {
    public func Write(data: string): int
    public func Close()
}
```

#### Go

```
type Writer interface {
    Write(data string) int
    Close()
}
```

### 7.5.2 接口组合

#### Gox

```
public interface ReadWriter {
    mixed Reader
    mixed Writer
}
```

#### Go

```
type ReadWriter interface {
    Reader
    Writer
}
```

### 7.5.3 说明

* 接口定义只包含方法签名，没有方法体
* 接口方法遵循 visibility 规则
* 支持使用 `mixed` 组合其他接口
* 结构体隐式实现接口（Go 的 duck typing）

---

## 7.6 控制流（新增 ✨）

### 7.6.1 if/else 条件语句

#### Gox

```
// 带括号（类似 C/Java）
if (x > 10) {
    println("big")
} else if (x > 5) {
    println("medium")
} else {
    println("small")
}

// 不带括号（也支持）
if x > 10 {
    println("big")
} else {
    println("small")
}
```

#### Go

```
if x > 10 {
    fmt.Sprintln("big")
} else if x > 5 {
    fmt.Sprintln("medium")
} else {
    fmt.Sprintln("small")
}
```

**说明**: 括号是可选的，两种语法都支持

### 7.6.2 while 循环

#### Gox

```
// 带括号
while (i < 10) {
    i = i + 1
}

// 不带括号
while i < 10 {
    i = i + 1
}
```

#### Go

```
for i < 10 {
    i = i + 1
}
```

**说明**: while 循环会转译为 for 循环，括号可选

### 7.6.3 for 循环

#### Gox

```
// 带括号
for (i < 10) {
    if (i == 5) {
        break
    }
    i = i + 1
}

// 不带括号
for i < 10 {
    i = i + 1
}
```

#### Go

```
for i < 10 {
    if i == 5 {
        break
    }
    i = i + 1
}
```

### 7.6.4 break/continue

```
for (i < 10) {
    if (i == 5) {
        break    // 跳出循环
    }
    if (i % 2 == 0) {
        continue // 跳过本次迭代
    }
    i = i + 1
}
```

### 7.6.5 switch 语句

#### Gox

```
// 带括号
switch (day) {
    case 1: {
        println("Monday")
    }
    case 2: {
        println("Tuesday")
    }
}

// 不带括号
switch day {
    case 1: {
        println("Monday")
    }
}
```

#### Go

```
switch day {
case 1:
    fmt.Sprintln("Monday")
case 2:
    fmt.Sprintln("Tuesday")
}
```

### 7.6.6 when 表达式（类似 Kotlin）

#### Gox

```
// 带括号
when (grade) {
    case "A": {
        println("Excellent")
    }
    case "B": {
        println("Good")
    }
}

// 不带括号
when grade {
    case "A": {
        println("Excellent")
    }
}
```

#### Go

```
switch grade {
case "A":
    fmt.Sprintln("Excellent")
case "B":
    fmt.Sprintln("Good")
}
```

**说明**: 
- when 是 switch 的另一种语法，语义相同
- 括号是可选的，两种风格都支持

---

## 8. 字符串

### 8.1 普通字符串

#### Gox

```
let name = "Alice"
```

#### Go

```
name := "Alice"
```

---

### 8.2 模板字符串（ES6 风格）（新增 ✨）

#### Gox

```
let name = "Alice"

// 方式 1：反引号模板字符串（推荐）
let greeting = `Hello, ${name}!`

// 方式 2：双引号字符串包含 ${...}（自动识别）
let message = "The value is ${x} and ${y}"

// print/println 中的模板字符串
println(`Hello, ${name}!`)      // 反引号
println("Hello, ${name}!")      // 双引号（也支持）
```

#### Go

```
name := "Alice"

// 反引号模板字符串
greeting := fmt.Sprintf("Hello, %v!", name)

// 双引号模板字符串
message := fmt.Sprintf("The value is %v and %v", x, y)

// print/println
fmt.Sprintln(fmt.Sprintf("Hello, %v!", name))
```

**说明**: 
- 反引号 `` ` `` 包裹的字符串，支持 `${expr}` 插值语法（类似 ES6 模板字符串）
- 双引号 `"` 包裹的字符串，如果包含 `${...}` 模式，也会自动转换为模板字符串
- 在 print/println 中两种语法都支持

---

### 8.3 Kotlin 风格 print/println（新增 ✨）

#### Gox

```
let name = "Alice"
println("Hello, ${name}!")
print("Value: ${x}")
```

#### Go

```
fmt.Sprintln(fmt.Sprintf("Hello, %v!", name))
fmt.Sprint(fmt.Sprintf("Value: %v", x))
```

**说明**: 当 print/println 包含模板字符串参数时，自动转换为 fmt.Sprintf 调用

---

## 7.7 TSX 语法（新增 ✨）

### 设计原则

* 类似 JSX/TSX 的组件语法
* 使用标签形式创建组件
* 自动推断 Props 类型
* 支持属性、子元素、嵌套、表达式插值

---

### 7.7.1 基本语法

#### Gox

```
// 定义 Props 结构体
public struct ViewProps {
    public id: string
    public class: string
}

// TSX 组件使用
public func Main() {
    return <View id="app" class="main" />
}
```

#### Go

```
type ViewProps struct {
    ID string
    Class string
}

func Main() {
    return View(ViewProps{
        ID: "app",
        Class: "main",
    })
}
```

---

### 7.7.2 带子元素的组件

#### Gox

```
public struct ContainerProps {
    public style: string
}

public func App() {
    return <Container style="flex">
        <Header title="My App" />
        <Text style="bold">Hello World</Text>
    </Container>
}
```

#### Go

```
func App() {
    return Container(ContainerProps{
        Style: "flex",
    },
        Header(HeaderProps{
            Title: "My App",
        }),
        Text(TextProps{
            Style: "bold",
        }, "Hello World"),
    )
}
```

---

### 7.7.3 表达式插值

#### Gox

```
public struct TextProps {
    public content: string
}

public func Greeting(name: string) {
    return <Text content={"Hello, " + name + "!"} />
}

// 子元素中使用表达式
public func Counter(count: int) {
    return <div>
        {"Count: " + count}
    </div>
}
```

#### Go

```
func Greeting(name string) {
    return Text(TextProps{
        Content: "Hello, " + name + "!",
    })
}

func Counter(count int) {
    return div(divProps{},
        "Count: " + count,
    )
}
```

---

### 7.7.4 自闭合标签

#### Gox

```
// 无属性的自闭合标签
public func Empty() {
    return <Image />
}

// 有属性的自闭合标签
public func Avatar(url: string) {
    return <Image src={url} />
}
```

#### Go

```
func Empty() {
    return Image(ImageProps{})
}

func Avatar(url string) {
    return Image(ImageProps{
        Src: url,
    })
}
```

---

### 7.7.5 布尔属性

#### Gox

```
public struct InputProps {
    public disabled: bool
    public required: bool
}

public func Form() {
    return <Input disabled required />
}
```

#### Go

```
func Form() {
    return Input(InputProps{
        Disabled: true,
        Required: true,
    })
}
```

---

### 7.7.6 嵌套组件

#### Gox

```
public func Layout() {
    return <Container>
        <Header>
            <Title>My App</Title>
            <Subtitle>Version 1.0</Subtitle>
        </Header>
        <Content>
            <List>
                <Item>Item 1</Item>
                <Item>Item 2</Item>
            </List>
        </Content>
    </Container>
}
```

#### Go

```
func Layout() {
    return Container(ContainerProps{},
        Header(HeaderProps{},
            Title(TitleProps{}, "My App"),
            Subtitle(SubtitleProps{}, "Version 1.0"),
        ),
        Content(ContentProps{},
            List(ListProps{},
                Item(ItemProps{}, "Item 1"),
                Item(ItemProps{}, "Item 2"),
            ),
        ),
    )
}
```

---

### 7.7.7 Props 类型推断规则

* 组件名首字母大写
* Props 类型名 = 组件名 + "Props"
* 例如：
  - `<View>` → `ViewProps`
  - `<Header>` → `HeaderProps`
  - `<MyComponent>` → `MyComponentProps`

### 7.7.8 属性名转换规则

* 属性名会自动转换为首字母大写（Go 导出字段）
* 例如：
  - `id` → `ID`
  - `className` → `ClassName`
  - `user_name` → `User_name`

### 7.7.9 说明

* TSX 标签会转译为组件函数调用
* 第一个参数是 Props 结构体
* 后续参数是子元素
* 属性名会自动大写转换
* Props 类型通过组件名自动推断
* 支持表达式作为属性值 `{...}`
* 支持文本和表达式作为子元素

---

## 7.8 结构体字面量简写（新增 ✨）

### 设计原则

* 类似 TypeScript 的对象字面量语法
* 在函数调用时省略结构体类型名
* 编译器根据函数参数类型自动推断
* 支持嵌套结构体字面量

---

### 7.8.1 基本语法

#### Gox

```
public struct Point {
    public x: int
    public y: int
}

public func show(p: Point) {
    println("Point: " + p.x)
}

public func Main() {
    // 直接使用对象字面量，省略结构体名称
    show({x: 10, y: 20})
}
```

#### Go

```
type Point struct {
    X int
    Y int
}

func Show(p Point) {
    println("Point: ", p.X)
}

func Main() {
    show(Point{X: 10, Y: 20})
}
```

---

### 7.8.2 嵌套结构体字面量

#### Gox

```
public struct Address {
    public city: string
}

public struct Person {
    public name: string
    public address: Address
}

public func showPerson(p: Person) {
    println("Name: " + p.name)
}

public func Main() {
    // 嵌套匿名结构体
    showPerson({
        name: "Alice",
        address: {city: "Beijing"}
    })
}
```

#### Go

```
type Address struct {
    City string
}

type Person struct {
    Name string
    Address Address
}

func ShowPerson(p Person) {
    println("Name: ", p.Name)
}

func Main() {
    showPerson(Person{
        Name: "Alice",
        Address: Address{City: "Beijing"},
    })
}
```

---

### 7.8.3 多个参数

#### Gox

```
public struct Config {
    public name: string
    public value: int
}

public func create(item: Config, title: string) {
    println("Create: " + title)
}

public func Main() {
    create({name: "test", value: 42}, "My Title")
}
```

#### Go

```
func Create(item Config, title string) {
    println("Create: ", title)
}

func Main() {
    create(Config{Name: "test", Value: 42}, "My Title")
}
```

---

### 7.8.4 类型推断规则

* 匿名结构体字面量：`{field: value, ...}`
* 编译器根据函数参数类型推断结构体类型
* 例如：参数类型为 `Point`，则 `{x: 10}` 推断为 `Point{x: 10}`
* 支持嵌套推断：内层结构体根据外层字段类型推断

### 7.8.5 说明

* 只在函数调用参数中支持简写语法
* 其他地方仍需使用完整形式：`Point{x: 10}`
* 保持与显式声明的兼容性：`show(Point{x: 10})` 仍然有效
* 嵌套结构体自动推断，无需手动指定类型

---

## 7.9 Go 模块导入支持（新增 ✨）

### 设计原则

* 显式声明导入的包类型（Go 或 Gox）
* Go 包自动转换函数可见性（小写转大写）
* Gox 包保持原样，不转换
* 默认不转换，显式转换最安全

---

### 7.9.1 导入语法

#### Gox

```
// 显式声明 Go 包
import go "fmt"
import go "strings"
import go "github.com/xxx/yyy"

// 显式声明 Gox 包（或省略，默认为 gox）
import gox "mygox/utils"
import "mygox/helper"  // 默认 gox
```

#### Go

```
import "fmt"
import "strings"
import "github.com/xxx/yyy"
import "mygox/utils"
import "mygox/helper"
```

---

### 7.9.2 可见性转换

#### Gox

```
import go "fmt"
import go "strings"
import "mygox/utils"

public func Main() {
    // Go 包 - 自动转换可见性（小写转大写）
    fmt.println("Hello")      // → fmt.Println
    strings.toupper("x")      // → strings.ToUpper
    
    // Gox 包 - 保持原样
    utils.helper()            // → utils.helper
}
```

#### Go

```
import "fmt"
import "strings"
import "mygox/utils"

func Main() {
    fmt.Println("Hello")
    strings.ToUpper("x")
    utils.helper()
}
```

---

### 7.9.3 转换规则

| 导入类型 | 语法 | 函数调用 | 转译结果 |
|---------|------|---------|---------|
| **Go 包** | `import go "fmt"` | `fmt.println()` | `fmt.Println()` ✅ |
| **Gox 包** | `import gox "utils"` | `utils.helper()` | `utils.helper()` ✅ |
| **Gox 包（默认）** | `import "helper"` | `helper.init()` | `helper.init()` ✅ |

---

### 7.9.4 说明

* **默认不转换** - 未指定类型时按 Gox 包处理，保持原样
* **显式转换** - 只有 `import go` 声明的包才会自动转换可见性
* **安全可靠** - 不会有歧义，完全由开发者控制
* **向后兼容** - 现有代码不需要修改
* **嵌套调用** - 支持嵌套函数调用的自动转换：`fmt.println(strings.toupper("x"))`

---

## 9. 数组字面量（新增 ✨）

### 设计原则

* 类似 TypeScript 的数组字面量语法
* 支持嵌套数组
* 支持空数组

---

### 9.1 基本语法

#### Gox

```
let numbers = [1, 2, 3]
let empty: int[] = []
let names = ["Alice", "Bob", "Charlie"]
```

#### Go

```
numbers := []interface{}{1, 2, 3}
empty := []interface{}{}
names := []interface{}{"Alice", "Bob", "Charlie"}
```

---

### 9.2 嵌套数组

#### Gox

```
let matrix = [[1, 2], [3, 4]]
```

#### Go

```
matrix := [][]interface{}{
    []interface{}{1, 2},
    []interface{}{3, 4},
}
```

---

### 9.3 TypeScript 风格数组方法

#### Gox

```
extend int[] {
    public func map(fn: func(int): int): int[] {
        return self
    }
    
    public func filter(fn: func(int): bool): int[] {
        return self
    }
}

let numbers = [1, 2, 3]
let doubled = numbers.map(func(x: int): int => x * 2)
let evens = numbers.filter(func(x: int): bool => x % 2 == 0)
```

#### Go

```
numbers := []interface{}{1, 2, 3}
doubled := numbers.map(func(x int) int { return x * 2 })
evens := numbers.filter(func(x int) bool { return x % 2 == 0 })
```

---

---

## 10. 错误处理（try / catch）

### 10.1 try（错误传播）

#### Gox

```
public func get(): string throws {
    let res = try foo()
    return res
}
```

#### Go

```
func Get() (string, error) {
    res, err := foo()
    if err != nil {
        return "", err
    }
    return res, nil
}
```

---

### 8.2 try/catch

#### Gox

```
try {
    let res = foo()
} catch err {
    log(err)
}
```

#### Go

```
{
    res, err := foo()
    if err != nil {
        log(err)
    }
}
```

---

### 规则

* `try` 仅适用于返回 `(T, error)` 的函数
* 使用 `try` 的函数必须声明 `throws`

---

## 9. Null Safety（可空类型）

### Gox

```
let name: string?
```

### Go

```
var name *string
```

---

## 10. 空操作符

### 10.1 空合并（??）

#### Gox

```
let name = input ?? "default"
```

#### Go

```
name := "default"
if input != nil {
    name = *input
}
```

---

### 10.2 安全访问（?.）

#### Gox

```
user?.name
```

#### Go

```
var name string
if user != nil {
    name = user.Name
}
```

---

## 11. extend（扩展方法）

### 设计原则

* 不真正扩展类型
* 编译为普通函数
* 方法调用语法 → 函数调用

---

### 11.1 定义

#### Gox

```
extend string {
    public func hello(): string {
        return "hello " + self
    }
}
```

---

### Go

```
func StringHello(s string) string {
    return "hello " + s
}
```

---

### 11.2 调用

#### Gox

```
let s = "pan"
s.hello()
```

---

#### Go

```
s := "pan"
StringHello(s)
```

---

### 11.3 链式调用

#### Gox

```
s.trim().upper()
```

---

#### Go

```
StringUpper(StringTrim(s))
```

---

### 11.4 规则

* `self` → 第一个参数
* 方法名 → 函数名（Type + Method）
* 不允许访问 private 字段
* 不允许覆盖已有方法

---

---
## 12 控制流设计（Control Flow） 
* 按照Ts的设计
## 13. 编译器架构

```
.gox
 ↓
Lexer
 ↓
Parser
 ↓
AST
 ↓
Transformer ⭐
 ↓
Go AST
 ↓
Go Code
```

---

## 14. AST 关键节点

### 示例

```
FunctionDecl
StructDecl
TryStmt
ExtendDecl
CallExpr
NullableType
ArrayType
```

---

## 15. MVP 实现范围（建议）

### 第一阶段

* public / private
* struct / func
* let
* T[]
* extend（基础版）

---

### 第二阶段

* try / catch
* nullable
* ?? / ?.

---

---

## 16. 示例（完整）

### Gox

```
public struct User {
    public name: string?
    private age: int
}

public func getUser(): User throws {
    let user = try fetchUser()
    return user
}

public func printUser(user: User?) {
    let name = user?.name ?? "unknown"
    log(name)
}

extend string {
    func hello(): string {
        return "hello " + self
    }
}

let s = "pan"
s.hello()
```

---

### Go

```
type User struct {
    Name *string
    age int
}

func GetUser() (User, error) {
    user, err := fetchUser()
    if err != nil {
        return User{}, err
    }
    return user, nil
}

func PrintUser(user *User) {
    name := "unknown"
    if user != nil && user.Name != nil {
        name = *user.Name
    }
    log(name)
}

func StringHello(s string) string {
    return "hello " + s
}

s := "pan"
StringHello(s)
```

---

## 17. 设计总结

Gox =

> Go + TypeScript 类型语法 + Swift 可见性

---

## 18. 下一步

* 定义完整语法（BNF）
* 实现 Parser
* 实现 Transformer（核心）
* CLI 工具（gox build）

---
