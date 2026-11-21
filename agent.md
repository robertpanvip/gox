# Gox 项目规则（给 AI Agent / 贡献者）

本文件是本项目的最高约定。**任何人在写 `.gx` 源码、示例、文档、README、演示代码时，都必须参照本项目自己设计的 GX 语法，而不是照搬生成后的 Go 写法。** 违反这条规则是本项目最典型的错误。

## 0. 一句话

Gox 是一个把 `.gx` 转译成标准 Go 的编译器。`.gx` = Go 语法 + 一组语法糖。你写的是 **GX 源码**，产出的是 **Go 代码**——两者写法不同，文档和示例必须展示前者。

- 语言规范权威来源：`docs/language-spec.md`
- 书写约定的活例子：`examples/lowerext.gx`（「GX 心智负担：一律写小写」）
- 需要 Go **1.27+**（泛型方法）；本仓库用 `GOTOOLCHAIN=go1.27.4`

## 1. 最高规则：标识符一律小写书写（最重要）

> **在 GX 源码里，标识符一律写小写；编译器会在 selector 右侧机械地 `upperFirst` 还原成 Go 的导出名。全大写缩写直接写大写、原样保留。**

这是本项目的设计选择：转译器不读第三方包符号表、不做类型推导，只对小写形式做机械首字母大写还原，因此行为完全可预测、零歧义。

| 你在 GX 里写 | 生成的 Go | 规则 |
|---|---|---|
| `fmt.println(...)` | `fmt.Println(...)` | 包级函数：小写书写，还原 |
| `strconv.itoa(v)` | `strconv.Itoa(v)` | 同上 |
| `req.method` / `u.path` | `req.Method` / `u.Path` | 结构体字段：小写书写，还原 |
| `r.URL` / `http.StatusOK` | 原样 | **全大写缩写写大写，原样保留** |
| `gxscene.get[T](props, "x")` | `gxscene.Get[T](...)` | runtime 符号：小写书写，还原 |
| `view.bindReactive(signal.effect)` | `view.BindReactive(signal.Effect)` | 方法：小写书写，还原 |
| `gxapp.open(view, gxapp.windowConfig{...})` | `gxapp.Open(view, gxapp.WindowConfig{...})` | 驼峰符号：只首字母小写，其余照抄 |
| `signal.createRoot(...)` / `signal.onCleanup(...)` | `signal.CreateRoot` / `signal.OnCleanup` | 多词符号：只首字母小写 |

**边界与例外**（务必遵守，否则会写出无法编译的示例）：

1. **多词符号只小写首字母，其余照抄 Go 拼写**：`windowConfig` → `WindowConfig`（不能写 `windowconfig`，那会生成 `Windowconfig`，拼错）。`bindReactive`、`createRoot`、`onCleanup`、`newWithEquals` 同理。
2. **`Signal` 是语法糖占位包名，必须首字母大写**：派生写 `Signal.computed(...)`、副作用写 `Signal.effect(...)`；**不要**写裸 `computed(...)`（裸标识符不会被还原，会生成不存在的函数）。它会被 ImportManager 解析为 `import Signal "<module>/runtime/signal"`。
3. **`pub func` 声明写小写，但 JSX 组件标签首字母必须大写**：`pub func card(...)` + `<Card/>`。JSX 里首字母大写 = 组件、小写 = 原生 HTML 标签，这是硬编码判定，不能把组件写成小写标签。
4. **本文件私有类型写小写**：`type cardProps struct{...}` 保持私有小写；`pub type` 才导出。
5. **裸标识符不碰**：内置函数等裸标识符一律不改，不要给它们加大小写。

### 反面教材（本项目真实犯过的错，禁止重演）

```gx
// ❌ 错：直接照搬生成后的 Go 大写 API
signal.CreateRoot(func(o *signal.Owner) { signal.OnCleanup(...) })
view.BindReactive(signal.Effect)
app, _ := gxapp.Open(view, gxapp.WindowConfig{...})

// ✅ 对：GX 源码一律小写（多词符号只小写首字母，Signal 占位名除外）
signal.createRoot(func(o *signal.owner) { signal.onCleanup(...) })
view.bindReactive(signal.effect)
app, _ := gxapp.open(view, gxapp.windowConfig{...})
```

## 2. 必须优先使用 GX 自身的语法糖

示例和文档要体现 GX 的设计能力，不要把 GX 当成「小写版 Go」来写。十三项语法糖见 `docs/language-spec.md`，高频项：

| 语法糖 | GX 写法 | 要点 |
|---|---|---|
| pub 可见性 | `pub func greet()` / `pub type point` / `pub enum color` | 声明处导出的开关 |
| enum 枚举 | `pub enum color { red, green, blue }`，用 `ColorRed` | 常量前缀大写 |
| 外部符号还原 | `http.newRequest(...)`、`fmt.sprintf(...)` | 小写书写，机械还原 |
| 集合链式 | `nums.map(fn).filter(fn).reduce(0, add)` | `[]T` 全局提升为 `list.List[T]`（Go 1.27 泛型方法） |
| 模板字符串 | `` `hi ${name}` `` | 编译为字符串拼接 |
| 参数默认值 | `func f(x int = 5)` | TS 语义，补尾部缺失参数 |
| Lambda | `x => x+1` | 单表达式 body；类型可推断 |
| 对象字面量 | `{ title: "Gox", n: 1 }` | 无期望类型→匿名 struct；有期望类型→补全并校验字段 |
| 对象解构 | `{ title, n } := cfg` | 纯标识符列表，逐字段生成赋值 |
| is 守卫 | `if (v is int) { ... }` | 块内窄化为具体类型 |
| 联合类型 | `v int \| string \| bool` | 擦除为 any + 入口守卫 |
| Signal 响应式 | `signal n = 0`、`n = 5`、`Signal.computed(...)` | 读自动补 `()`，写改写为 `n(v)` |

## 3. Scene / 响应式 UI（scene 后端 + 四阶段增强）

JSX 默认走 scene 后端（`gox build -jsx=scene`）：编译期 opcode + 常驻场景树 + getter 保鲜 + 递归细粒度更新，不是 vdom diff。

四阶段响应式增强的**源码写法**（详见 `docs/reactive-ui-roadmap.md`）：

```gx
// 阶段1：Owner / Cleanup / Effect 句柄
signal.createRoot(func(o *signal.owner) {
    h := signal.effect(func() {
        fmt.println(count)
        signal.onCleanup(func() { /* 重跑前 / 销毁时 */ })
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

// 阶段4：应用入口 + 一行接线（结构变化也会自动重绘）
app, err := gxapp.open(view, gxapp.windowConfig{ Title: "Gox", Width: 800 })
if err != nil { panic(err) }
defer app.close()
app.bindReactive(signal.effect)
```

约束：分支 selector 必须读 signal 才有响应式；`<For>` 必须提供 `key`；组件内 signal 要经 Props getter 传到叶子插槽才能在写入时更新；app 场景结构变化（分支/列表）由 `bindReactive` 自动重绘。

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
