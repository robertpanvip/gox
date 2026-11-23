# 高级样式系统：定位 / 溢出与虚拟滚动 / 动画 / 阴影

两个后端（Skia/Yoga 与 Rust/Taffy）对以下样式属性采用**完全一致**的语义。所有属性都走 `Node.Props` 的**原生全量透传**——host 只搬运字符串、不解释语义，因此**零协议格式改动**（GRX1/GRX2 二进制编码不变）。

## 1. 定位：`position: absolute` + inset

| 属性 | 含义 |
|---|---|
| `position` | `relative`（默认，参与 flex 流）/ `absolute`（脱离流） |
| `top`/`right`/`bottom`/`left` | inset 偏移（px 或 `"12px"`）；`auto`/缺省表示不设置 |

- absolute 节点相对**直接父节点的 padding box** 定位（CSS 语义），不占用父容器空间、不影响兄弟布局。
- Skia 用 Yoga `PositionTypeAbsolute` + `SetPosition(Edge)`；Rust 用 Taffy `Position::Absolute` + `inset`。
- **浮层是 absolute 的高层封装**：`gx-role="popover"` 节点同样脱离流，但位置由 `gx-anchor`/`gx-placement` 二次计算贴锚点；`gx-role="modal"` 相对画布居中；`gx-role="portal"` 平移到画布原点并铺满（子树 absolute 自主定位）。两者不冲突：浮层优先走锚点逻辑。三种浮层角色 + scrim 的完整约定见 `backend/skia/overlay.go` 与 [docs/APPLE_DESIGN.md](APPLE_DESIGN.md)。
- **注意**：`position:absolute` 不要放在 `For` / 组件包装产生的 `#frag`（display:contents）子节点上——display:contents 节点不产生布局盒，absolute 会失去定位参照（实测坐标塌成 0×0）。需要脱离流时用浮层机制，或把 absolute 节点直接挂在有尺寸的容器下。

## 2. 溢出与虚拟滚动

| 属性 | 含义 |
|---|---|
| `overflow` | `visible`（默认）/ `hidden` / `scroll` / `auto` |
| `scroll-top` / `scroll-left` | 滚动偏移（px），响应式 |

**滚动偏移在布局阶段解决**（两端统一）：overflow 容器的子树绝对坐标整体减去 scroll 偏移，绘制与命中天然一致；容器矩形作为裁剪边界向下传递：
- Skia：layout 阶段回填带裁剪的 layoutBox；绘制时 `canvas.Save/ClipRect/Restore`；命中检查裁剪矩形。
- Rust：emit 阶段把裁剪矩形随绘制指令传递；tiny-skia 对 shape 矩形求交裁剪、glyphon 用 `TextBounds` 裁剪文本；命中检查裁剪矩形。

**虚拟滚动的分工**（性能关键）：

```
widgets.VirtualList {
  count, item-height, viewport-height, scroll-top(getter), overscan, render-item
}
```

- **组件层**：根据 `scroll-top` 只渲染可见窗口 `[first, last)` 的项（+ overscan 缓冲）；用一个撑高 spacer（`height = first × item-height`）把可见项推到全局位置。场景树节点数与数据量**解耦**——1000 项只建 ~10 个节点。
- **后端**：overflow 容器应用 scroll 偏移与裁剪。
- 不用 absolute 定位列表项（见上节 display:contents 陷阱），改用 spacer + flex 流，跨后端一致。

## 3. 动画：`transition`

```
transition: "opacity 300ms ease, transform 200ms linear, width .4s ease-in-out"
```

- 支持数值属性（width/height/opacity/font-size/padding/border-radius/inset/gap…）与颜色属性（color/bg/background/border）插值；缓动：`linear` / `ease` / `ease-in` / `ease-out` / `ease-in-out`。`all` 暂不展开为具体属性。
- **后端有状态插值引擎**：渲染时按当前单调时钟对比「目标 props vs 上次目标」，变化则从**当前插值值**起新动画（平滑衔接中途改向），在 duration 内插值。
  - Skia：`Animator` 临时覆盖场景树 props，绘制后 restore，不污染 host 树；`Window` 每帧 Draw 推进。
  - Rust：`style::Animator` 返回一棵属性被覆盖的树副本供本帧布局绘制；常驻 `Renderer` 用 `Instant` 时钟。
- 持续重绘由各后端的帧循环驱动（winit / GLFW 每帧 redraw）。首帧不动画（直接采用初始值）。

## 4. 阴影：`box-shadow`

```
box-shadow: "offsetX offsetY blur spread color"
例: "0 6px 16px 2px rgba(0,0,0,0.55)"
```

- spread/color 可省略；颜色支持 `#rgb`/`#rrggbb`/`#rrggbbaa`/`rgb()`/`rgba()`/命名色。
- 两端采用**同一算法**：6 层半透明圆角矩形，从 `spread+blur` 扩张到 0，alpha 沿高斯近似衰减，得到柔和阴影。跨平台视觉一致（go-skia 0.1 未暴露 DrawLooper，wgpu 后端也无原生 shadow，故统一用近似）。
- 阴影绘制在元素背景之前，并受祖先 overflow 裁剪。

## 5. 颜色 alpha 约定

- `#rrggbbaa` 末两位是 alpha（CSS 标准）；`rgba(r,g,b,a)` 的 a 为 0..1 或 0..255。
- `opacity` 属性 0..1，作用于元素。

## 6. 预制组件与样式透传

`widgets.Col` / `widgets.Row` 会透传通用静态样式属性（`bg`/`height`/`padding`/`border-radius`/`box-shadow`/`position`/inset/`overflow`/`transition`/`transform` 等，见 `flexStyleKeys`），因此预制容器也能使用上述能力。响应式属性（getter）请在具体组件（如 VirtualList 的 scroll-top）或原生模板 `OpDynAttr` 中使用。

## 7. 本次一并修复的 host 核心 bug

`runtime/scene/for.go` 中 `For` 控制器运行时新建的两个透明包装节点（`#frag` 项包装、`#root` 根包装）**漏设 `Type: NodeFragment`**，零值恰为 `NodeText`。后果：任何理解 Type 语义的后端会把整棵列表项子树当空文本跳过——虚拟列表/For 列表在下游完全不渲染。已修复并加注释，host 全量回归通过。

## 8. img 与 canvas 原语（两端原生实现）

组件库分层：`div`/`img`/`canvas`/虚拟滚动/`popover` 属**后端原生层**（性能优先，各后端用各自渲染栈实现）；`button`/`card`/`input` 等属 **GX 通用组件层**（`gox/widgets`，后端无关，纯 gxscene 场景树组合）。后者由前者组合而成，一套组件在两端渲染。

### img

`<img src="...">` 产出 `Kind="img"` 的 NodeDiv，`src` 走 Props 透传，两端用同一个 `runtime/imgutil` 语义解析来源：

| src 形式 | 支持 |
|---|---|
| `data:image/png;base64,...` | 两端都支持（推荐，自包含） |
| 本地文件路径 | 两端都支持 |
| `http(s)://...` | Go 侧支持（带 5s 超时）；Rust 侧需 Go adapter 预转成 data URI |

样式属性：`width`/`height`、`object-fit`（`fill` 默认 / `contain` / `cover`）、`border-radius`（圆角裁剪）。两端解码结果按 src 缓存，响应式重绘不重复解码。GX 侧用 `widgets.Img(props)` 构建。

### canvas

`<canvas>` 产出 `Kind="canvas"` 的 NodeDiv；命令式绘制指令编码成 JSON 放 `Props["canvas-ops"]`（`runtime/canvas` 协议，**零 wire 协议改动**，随 Props 全量透传）。指令集（两端一致）：`rect` / `circle` / `line` / `path`。坐标相对 canvas 节点布局盒左上角，颜色语义与 CSS 一致。**文本不在 canvas 内**——用声明式 `<text>` 节点叠加，跨后端天然一致。

GX 侧用 `widgets.Canvas(props)` 构建（`ops` 传 `[]canvas.Op`）。两端绘制：Skia 用 `DrawPath`/`DrawCircle` 等原生 API；Rust 用 tiny-skia `fill_path`/`stroke_path`。

### 验证

- `runtime/imgutil`、`runtime/canvas`：来源解析与指令编解码单测。
- skia：`image_test.go` / `canvas_test.go`，DrawLog 可观测。
- Rust：`image.rs` / `canvas.rs` 单测（data URI 解码、path 解析、全指令不 panic）。
- 跨语言：`backend/rust/go/widgets_e2e_test.go`、`image.go`、`mirror_consistency_test.go`——GX 组件库场景树经 GRX1/GRX2 喂给 Rust 镜像探针，节点数与属性严格一致。
- 正式 GX 后端 demo：`backend/rust/go/cmd/demo`（widgets 构建场景 → FramePresenter → GRX 协议 → Rust 渲染）。`backend/rust/src/main.rs` 是纯 Rust 渲染栈 spike（IME/GPU），非 GX 后端 demo。

## 9. 组件分层决策（性能敏感走原生各自实现）

组件库扩充遵循一条原则：**纯声明式可表达且无性能瓶颈的组件，放 `gox/widgets` 两端共用；性能或平台能力敏感的，才交给后端原生各自实现**。

| 组件 | 分层 | 说明 |
|---|---|---|
| Tooltip | 声明式（Popover 变体） | hover 触发浮层，无 scrim；`onMouseEnter`/`onMouseLeave` 驱动 PopoverState |
| Form / FormItem | 声明式 | 布局 + label/必填/错误展示；errors 由业务 signal 提供 |
| TimePicker | 声明式（Popover + For） | 时/分/秒三段，12/24 小时制，选中经 `BindValue` 回传 |
| Table | 声明式 + VirtualList | 行虚拟化：1000 行数据场景树只建 ~12 个可见行节点，节点数与数据量解耦，不新增 wire 协议 |
| FilePicker | **原生能力通道（各自实现）** | OS 文件对话框不是场景树能画的：`widgets.FileDialog` 契约 + 注册表，各后端各自实现并注册 |

### FilePicker 契约与接入

- `widgets.FileDialog`：`OpenFile(FileOptions) ([]string, error)`，装配期用 `widgets.RegisterFileDialog(d)` 注册。
- `widgets.FilePicker(props)`：产出触发按钮，点击调注册实现；选中经 `widgets.BindPick(func([]string))` 回传（`ctx.Synthetic.Data["paths"]`），取消走 `onCancel`。
- 未注册实现时走 `noopFileDialog`（返回空选择，不 panic），headless/测试安全。
- 原生实现：`backend/skia/filedialog.go`（`skia.RegisterFileDialog()`）、`backend/rust/go/filedialog.go`（`rb.RegisterFileDialog()`）——两端各自调用平台原生对话框工具（zenity/kdialog / osascript / PowerShell），互不 import。Rust 端若要换纯 Rust `rfd`，只需新增 C ABI 并保持接口形态。

## 10. Context 与 Theme（响应式共享状态）

`widgets.Context[T]` 是**真正的 React Context 语义**：子树隔离、嵌套就近覆盖、值变化精确驱动消费者。它建立在 GX 已有的 Solid 式响应式 Owner 树上，绕开逐层 props 透传：

- `ctx := widgets.NewContext(def)` 创建；`ctx.Use()` 在组件函数里调用，返回 `func() T`（可直接作为 GX 动态插槽 getter，读取即订阅）；`ctx.Set(v)` 写**默认通道**（无 Provider 时的全局用法）；`ctx.Provider(props)` 声明式提供子树值（`value` 支持静态 `T` / `func() T` / `func() any` getter）。
- **作用域语义**：`Provider` 用 `signal.CreateRoot` 建独立 Owner 挂值，`Use()` 沿 Owner 的 parent 链**就近查找**最近的 Provider——
  - 子树隔离：兄弟 Provider 各读各的值，互不串台；Provider 不写全局默认值。
  - 就近覆盖：嵌套 Provider 内层遮蔽外层。
  - 动态子树不丢值：`if` 分支与 keyed 列表重建子树时恢复建树期 Owner（scene/branch.go、for.go + reactive.go 的 `CurrentOwner/RunWithOwner`），`Use()` 仍命中所属 Provider。
  - 响应式更新：`value` 为 getter 时订阅上游 signal，变化只让读到它的消费者重算。
- 没有任何 Provider 时回落到默认 signal（`Set` 写它），保留 `SetTheme` 这类全局单例用法。context 不进场景树/wire 协议，纯 Go 运行期通道，零后端改动。

`widgets.Theme` 是基于 Context 预制的整套设计令牌（配色/圆角/阴影/间距）：

- `DarkTheme` / `LightTheme` 两套预置；`SetTheme(t)` 全局切换默认通道；`ThemeProvider(props)` 为子树提供主题（子树隔离，不改全局）；`UseTheme()` 返回 `func() Theme`，`UseToken("accent")` 返回单个令牌 getter。
- 组件把 `UseToken(...)` 作为 `dynAttr` getter 即可随主题切换重绘（demo 底部有深色/浅色切换演示）。需要"一个窗口深色、某个面板浅色"时，用 `ThemeProvider` 包住该子树即可，全局主题不受影响。
- 既有 `Theme*` 常量（apple_theme.go）保持不变向后兼容；新代码优先用 `UseTheme` 以支持切换。

### 10.1 在 GX 语言里用 Context/Provider

Context/Provider 是 Go API（`widgets` 包），在 `.gx` 源码里平铺直用，编译器按 Go 包规则把小写方法名还原成大写、注入 import，零新增语法。完整可运行示例见 `examples/jsxapp-context.gx`。对照 React：

| React          | GX 写法                                        | 说明 |
|----------------|------------------------------------------------|------|
| `useContext`   | `v := ctx.use()`                               | 建树期调用，产物还原为 `ctx.Use()`，返回 getter |
| `<Provider>`   | `ctx.provider(p)`（包一层组件函数）            | 产物还原为 `ctx.Provider()`，子树隔离 |
| `Consumer`     | 不需要——组件函数本身就是 consumer             | 函数内 `ctx.use()` 即为读取 |
| `useState`     | `sig n = 0`                                    | 源信号，写 `n = v` 自动改写为 `n(v)` |
| `useMemo`      | `Signal.computed(() => ...)`                   | 派生信号 |
| `useEffect`    | `Signal.effect(() => {...})`                   | 副作用，自动依赖追踪，无 deps 数组 |

两个 GX 约定（踩坑点）：

- **顶层声明用 `var` 不用 `let`**：`var scoreCtx = widgets.NewContext[int](0)`；`let` 只在函数内。泛型调用 `NewContext[int]` 编译器原样保留。
- **Props 用带引号 key + 带类型变量再传**：`p := gxscene.Props{"value": ..., "children": ...}; ctx.provider(p)`。裸 key（`value:`）会被 `transformCompositeFields` 当成结构体字段名还原成大写；对象字面量直接作为方法调用参数时编译器无法推断目标类型（函数形式 `gxapp.open({...})` 可以，方法形态不行）。包级函数形式的 `ThemeProvider` 可直接写 `<widgets.ThemeProvider value={...}>...</widgets.ThemeProvider>`。
- 动态插槽里写 `score()`（带括号）才读取并登记依赖；写 `score`（函数值）是快照，不会随 context/sig 更新。

### 10.2 跨文件共享 Context

Context **可以跨文件共享**，且共享的是同一个响应式实例（指针），不是值拷贝。完整示例见 `examples/crossfile-context/`：

- 定义方（GX 模块）用 `pub var` 导出：`pub var labelCtx = widgets.NewContext[string]("default")`，编译产物为 `var LabelCtx = ...`。
- 使用方 `import "example.com/proj/theme"` 后写**小写源名** `theme.labelCtx`，编译器查该 GX 模块的 `pub` 导出表还原成 `theme.LabelCtx`；`.set()` / `.use()` / `.value()` 同样按 Go 包规则还原成大写。
- 一处 `theme.labelCtx.set("x")`，另一个文件里 `theme.labelCtx.use()` 的消费者随订阅重绘——跨文件的 Set/Use 走的是同一个 signal 通道。
- 构建走跨文件项目模式：`gox build main.gx -root <项目根> -replace github.com/robertpanvip/gox=<gox路径> -check`。运行时 import（`runtime/scene` 等）恒指向 gox module，不会被项目 module 前缀污染（已有回归测试 `TestBuildRuntimeImportUsesGoxModule`）。

**同一个 Context、不同子树各有各的值**（示例 `examples/crossfile-context` 已完整演示）：`pub var` 是包级单例，但 `Provider` 会为子树挂独立值，两者不冲突。把 `ctx.provider(p)` 也用 `pub func` 封成组件（如 `theme.Provide`），即可跨文件使用：

- 兄弟子树各给不同值 → 各自读到各自的（A/B 互不串台）。
- 嵌套 Provider → 内层就近覆盖，内层之外的部分仍读外层值。
- 没有 Provider 的子树 → 读 `setDefault` 写的全局默认；改全局只重绘这些子树，带 Provider 的不受影响。

集成测试 `TestIntegrationContextSubtreeIsolation` 覆盖兄弟隔离/嵌套覆盖/全局回落的运行值。

## 11. 窗口外观配置（无边框 / 透明 / 置顶 / 图标）

`gxapp.WindowConfig` 新增外观字段，两端各自实现：

| 字段 | 类型 | 语义 |
|---|---|---|
| `Decorated` | `*bool` | nil=后端默认；`NoDecorations()`=无边框；`WithDecorations()`=有边框。用指针区分"未设置"与"明确 false" |
| `Transparent` | bool | 透明 framebuffer；配合无边框 + 透明 bg 可做无背景悬浮面板 |
| `AlwaysOnTop` | bool | 窗口置顶 |
| `Opacity` | float32 | 窗口级不透明度 0..1；<=0 或 >1 表示不设置（winit 端暂未生效） |
| `IconPath` | string | 图标路径（字段已定义；skia 端待接 `SetIcon`，当前静默忽略） |

- skia（GLFW）：`CreateWindow` 前设 `Decorated/TransparentFramebuffer/Floating` hints；透明时每帧 `gl.Clear(0,0,0,0)` 让场景透明区透出桌面。
- rust（winit）：新增 C ABI `gx_window_new_ex(w,h,title,decorated,transparent,always_on_top,opacity)`；`gx_window_new` 保留为兼容包装。Backend 存配置，`WinitApp::resumed` 应用 `with_decorations/with_transparent/with_window_level`；title 参数也真正生效（此前写死）。
- `GX_NODECOR=1` 运行 skia-style-demo 可验证无边框+透明配置（截图 `preview-context-theme.png` 含 Context/Theme 演示区）。

## 12. 用 GX 编写组件库（自举）

widgets 标准库正在从"手写 `gxscene.Op` 数组"迁移到"用 GX 语言本身书写"。样板是 `widgets/card.gx`：

- **源文件** `widgets/card.gx` 用 JSX + `pub` + `gxscene.Get` 表达组件，是可读的事实来源。
- **产物** `widgets/card.go` 由 `make gxbuild`（遍历 `widgets/*.gx` 转译为同目录同名 `.go`）生成并提交——Go 工具链只认 `.go`，故源与产物共存，**改完 `.gx` 必须重跑 `make gxbuild`** 保持同步。
- 转译产物与手写实现行为等价：JSX 的属性/文本/子节点分别编译成 `OpAttr`/`OpChildText`/`OpDynChild`+getter，事件 `onXxx={...}` 编译成 `OpEvent`，`if`/`<For>` 编译成 `OpBranch`/`OpFor`。已验证产物可重复生成（diff 稳定）、全量测试零回归。

**迁移边界（哪些能用纯 JSX 表达，哪些保留 Go）**：

| 组件类别 | 能否用 GX 源 | 说明 |
|---|---|---|
| 纯展示/布局（Card/Text/Badge/Divider/Row/Col/Stat…） | ✅ 直接转 | JSX + `gxscene.Get` 足够 |
| 列表/条件（Table 表头、选项菜单） | ✅ `<For>`/`if` | 编译成 `OpFor`/`OpBranch` |
| 依赖**稳定 Branch 对象**（Tooltip/TimePicker/Select 浮层） | ⚠️ 需增强 | menu getter 每次需返回同一 `*Branch` 指针，JSX 的内联 BranchGetter 会重建；暂留 Go |
| 依赖 **Owner/CreateRoot**（Context/Theme Provider） | ❌ 保留 Go | `ProvideValue`/`LookupValue`/owner 栈是运行时底层 API，JSX 抽象层不暴露 |
| 焦点状态机（Input 的 InputChar/Backspace） | ⚠️ 部分 | 场景树可用 JSX，焦点/键盘派发逻辑留 Go |

即"能用 JSX 表达的逐步转，涉及运行时底层的保留 Go"——两者同包混编，测试零改动。

## 验证

- 单元测试：两端各有解析 / 定位 / 裁剪 / 插值 / 阴影单测（Go `go test ./...`、Rust `cargo test`）。
- 新增组件单测：`widgets/` 下 context/theme + tooltip/form_container/timepicker/table/filepicker 各自 `_test.go`；FilePicker 用 mock Dialog 验证契约回传；Theme 测试覆盖令牌解析、`SetTheme` 驱动插槽重绘。
- 窗口配置单测：`runtime/app/windowconfig_test.go`（默认值语义 / 三态 helper）、`backend/rust/go/windowconfig_test.go`（`decoratedC` 三态翻译）。
- 端到端截图：`examples/skia-style-demo` 展示 absolute 徽标、阴影卡片、transition 进度条、1000 项虚拟滚动列表、5 个企业级组件、Context/Theme 切换区；`preview-enterprise.png` 与 `preview-context-theme.png` 两张确认。无边框配置经 `GX_NODECOR=1` 验证无系统标题栏、无崩溃。
