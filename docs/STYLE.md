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
- **非颜色令牌同样入主题**：圆角/控件高度/字号/阴影/间距（space1–space8）全量是 `Theme` 字段，`UseToken("radius-panel"/"shadow-card"/"font-headline"/"space-6"…)` 在明暗主题各取其所值（浅色阴影按 HIG 更淡：`0 2px 8px 0 #0000001a`）；apple_theme 的静态常量保留为兼容别名。注意可变别名（`ShadowCard` 等 var / `tones` 表）由 `SetTheme` 同步、只作用于全局通道，**子树级隔离请用 `ThemeProvider` + `UseToken`**——showcase 的「子树主题 · 明暗并置」分组即该能力的活示例（`TestUseTokenInsideProviderSubtree` 锁定）。

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

## 11. 窗口外观配置（无边框 / 透明 / 置顶 / 图标 / 鼠标穿透）

`gxapp.WindowConfig` 新增外观字段，两端各自实现：

| 字段 | 类型 | 语义 |
|---|---|---|
| `Decorated` | `*bool` | nil=后端默认；`NoDecorations()`=无边框；`WithDecorations()`=有边框。用指针区分"未设置"与"明确 false" |
| `Transparent` | bool | 透明 framebuffer；配合无边框 + 透明 bg 可做无背景悬浮面板 |
| `AlwaysOnTop` | bool | 窗口置顶 |
| `Opacity` | float32 | 窗口级不透明度 0..1；<=0 或 >1 表示不设置（winit 端暂未生效） |
| `IconPath` | string | 图标路径（字段已定义；skia 端待接 `SetIcon`，当前静默忽略） |
| `MousePassthrough` | bool | 透明区不拦截鼠标：点击落到后面的窗口，只有场景有内容处才接收点击。默认 false，见下文 |

- skia（GLFW）：`CreateWindow` 前设 `Decorated/TransparentFramebuffer/Floating` hints；透明时每帧 `gl.Clear(0,0,0,0)` 让场景透明区透出桌面。
- rust（winit）：新增 C ABI `gx_window_new_ex(w,h,title,decorated,transparent,always_on_top,opacity)`；`gx_window_new` 保留为兼容包装。Backend 存配置，`WinitApp::resumed` 应用 `with_decorations/with_transparent/with_window_level`；title 参数也真正生效（此前写死）。
- `GX_NODECOR=1` 运行 skia-style-demo 可验证无边框+透明配置（截图 `preview-context-theme.png` 含 Context/Theme 演示区）。

### 11.1 鼠标穿透（MousePassthrough）

**问题**：透明 framebuffer 只让像素透明，鼠标事件仍由 X11 窗口按矩形整体捕获——点击透明区不会落到后面的应用（"透明 ≠ 鼠标穿透"）。

**方案**：把"本帧真正绘制出的不透明内容"矩形集合设为窗口的可命中区，其余透明区让鼠标穿透到下层窗口——等价于 Web `pointer-events:none`。三端平台机制不同，共用同一份矩形口径：

- **统一口径（DrawLog 而非 hitbox）**：`passthrough_common.go` 的 `drawLogToInputRects` 把 `DrawLog` 转成物理像素矩形，跳过 `ARGB` alpha=0 的透明 fragment/隐藏节点。hitbox 对所有节点（含 `#root`/`#frag` 透明容器、无 bg 的纯透明 div）都记录，若据此设输入区会把整窗算作可点、失去穿透。`passthrough_common_test.go` 固化该口径与 DPR 换算。
- **每帧更新**：`paintNow` 绘制后调用 `applyInputShape(win, log, dpr, ...)`；`MousePassthrough=false` 时各平台恢复整窗可点，行为与既有完全一致。

| 平台 | 机制 | 实现文件 | 穿透粒度 | 验证状态 |
|---|---|---|---|---|
| Linux/X11 | `XShapeCombineRectangles(ShapeInput)` | `passthrough_linux.go`（cgo + libXext，OS 级区域形状） | 区域级 | ✅ 真机（xvfb+openbox 叠层实测） |
| Windows | WndProc 子类化，`WM_NCHITTEST` 透明区返回 `HTTRANSPARENT` | `passthrough_windows.go`（纯 Go syscall user32.dll） | 区域级 | ⚠️ 仅交叉编译验证（amd64/386），无真机 |
| macOS | `NSWindow.ignoresMouseEvents` + 全局鼠标位置轮询 | `passthrough_darwin.go`（cgo + AppKit；事件循环每帧 `pollPassthrough`） | 整窗动态开关（效果等价） | ⚠️ 仅代码审查，沙箱无 macOS SDK 无法编译/真机 |
| 其他 | no-op | `passthrough_other.go` | 不支持（静默忽略） | — |

**验证（Linux）**：`GX_PASSTHROUGH=1` 开启；透明窗口叠在 xev 上，透明区点击到达后面窗口（xev ButtonPress=1）、内容区点击被拦截（xev=0）。macOS/Windows 因沙箱无对应 SDK/工具链，只做了交叉编译与代码审查，需在真机上跑同样的叠层实验确认。

## 12. 用 GX 编写组件库（自举）

**widgets 标准库已全量迁移为 `.gx` 源 + `.go` 产物共存**：目录下每个组件都有同名 `.gx`（事实来源）与 `.go`（`make gxbuild` 生成并提交）。Go 工具链只认 `.go`，故源与产物共存，**改完 `.gx` 必须重跑 `make gxbuild`** 保持同步；产物可重复生成（幂等，diff 稳定）。

GX 语法本是 Go 的超集——`.gx` 里既能写 JSX，也能直接写原生 Go。组件因此分两种写法，同包混编、测试零改动：

- **JSX 写法**（如 `card.gx`）：`pub func` + JSX + `gxscene.get`。属性/文本/子节点编译成 `OpAttr`/`OpChildText`/`OpDynChild`+getter，事件 `onXxx={...}` 编译成 `OpEvent`，`if`/`<For>` 编译成 `OpBranch`/`OpFor`。适合纯展示/布局组件。
- **原生 Go 写法**（其余大多数组件）：手写 `gxscene.Op` 数组 + 事件/focus/浮层等命令式逻辑，整段放进 `.gx` 原样透传。适合依赖 `Owner/CreateRoot`（Context/Theme Provider）、稳定 `*Branch` 对象（Tooltip/TimePicker/Select 浮层）、焦点状态机（Input）等 JSX 抽象层不覆盖的运行时底层能力。

### 组件库源码遵循 GX 大小写规范

组件库 `.gx` 是 GX 源码，同样遵守 `agent.md` §1 的大小写分流：**外部 Go 包的函数/方法/opcode 常量写小写**（`gxscene.get` / `gxscene.render` / `gxscene.opElem` / `signal.new` / `fmt.sprintf`），由编译器 `upperFirst` 还原成 Go 导出名；**类型名与全大写缩写保持大写**（`gxscene.Props` / `gxscene.Instance` / `gxscene.Op` / `signal.Signal` / `atomic.Uint64`）；**本包导出的组件/API 名**（`Button` / `Text` / `NewContext`）是 Go 包对外契约与 JSX 组件约定，保持大写；注释描述的是 Go API 真实名字，也保留大写。从 Go 机械迁移的组件曾把外部符号原样带成大写，现已统一改小写——产物逐字节不变（纯源码风格），并由 `pkg/compiler/widgets_lowercase_test.go` 锁定。

### 切片提升改为按需（默认行为）

全局切片提升 `[]T → list.List[T]` 只在源码**真正用到** `.map` / `.filter` / `.reduce` 链式调用时才发生；没有链式调用的源码保留原生 `[]T`，不污染函数签名/类型断言、不引入 `runtime/list` 依赖。这样写原生 Go 的组件天然不被提升，普通 GX 代码用到链式方法时行为不变。

### `gx:nocollections` 原生 Go 模式开关

手写 opcode 的组件在源码首部声明 `// gx:nocollections`，强制整段按原生 Go 透传——核心是跳过**对象字面量补全**：

| 被跳过的变换 | 不跳过时的破坏 |
|---|---|
| 裸对象字面量补全 | `[]gxscene.Op{{Code:...},...}` 里省略类型的元素被误判成 GX 对象字面量，补成 `struct{...}{...}` 后无法赋回 `[]Op` |
| 集合变换（链式方法还原 / 切片提升 / 融合 / 接收者包装） | 兜底：即使源码出现链式调用也不 desugar |

其余 GX 变换（JSX / `pub` / 小写符号还原 / import 注入等）照常进行。**默认无此标记**，普通 `.gx` 应用行为零变化。回归测试见 `pkg/compiler/nocollections_test.go`。

## 13. 渐变与文字效果：三端统一契约

三端（Web WASM / Go 桌面 Skia+canvas / Rust 桌面 wgpu+tiny-skia+glyphon）消费**同一份声明式 CSS 数据**，不新增协议字段——所有样式走 `Props` 全量透传（见 `backend/rust/PROTOCOL.md`）。核心约定：

| 场景 | 声明方式 | Web | Go 桌面 | Rust 桌面 |
|---|---|---|---|---|
| 通用渐变背景 | `bg: linear-gradient(90deg, #f00 0%, #00f 100%)` | CSS 原生 | `PaintSetShader` 原生 shader | tiny-skia `LinearGradient` shader |
| 渐变文字 | `gradient-text="1"` + `bg: linear-gradient(90deg, #c 0ch 7ch, …)` | `background-clip:text` | `gradientToRich` → shader | glyphon `set_rich_text` 逐段 span |

关键语义（三端一致，见 `text/effects/effects.go` 与 `backend/rust/rust/src/text_effects.rs`）：

- **只有**带 `gradient-text="1"`（或 `background-clip:text`）标记时，`bg` 渐变才裁剪到文字；否则渐变是容器背景。不能仅凭存在 `linear-gradient` 就判定渐变文字。
- 编辑器代码高亮的渐变是**按 `ch` 边界的分段纯色**（不是平滑插值）。`runeWidth`/`gradientCells` 三端必须一致：ASCII=1ch，CJK（≥U+2E80）=2ch，否则色段与 rune 错位。
- 渐变标记在父节点，文本是子节点：emit/绘制时把渐变沿继承链传给文本节点再分段着色。改颜色只重排 span，不改布局。

Rust 端统一语义入口是 `text_effects.rs`（`parse_linear_gradient` / `gradient_spans` / `TextEffects::from_props`），与 Go `text/effects` 输入输出语义对齐。可视化验收程序 `backend/rust/go/cmd/gradcheck` 渲染分段渐变文字 + 平滑渐变背景，`preview/rust-backend-gradient.png` 为截图证据。

## 验证

- 单元测试：两端各有解析 / 定位 / 裁剪 / 插值 / 阴影单测（Go `go test ./...`、Rust `cargo test`）。渐变额外覆盖：`text_effects.rs` 7 项解析/CJK 边界/标记语义、`render.rs` 父子继承链路 + shader 构造 + 签名失效，`text/effects/effects_test.go` CSS 属性映射。
- 渐变端到端：`backend/rust/go/cmd/gradcheck` 经 xvfb 渲染后截图采样——文字 AB 红/CD 绿/EF 蓝分段正确，背景块 x=25 红→x=305 蓝平滑过渡，与 Web/Go 桌面端语义一致。
- 新增组件单测：`widgets/` 下 context/theme + tooltip/form_container/timepicker/table/filepicker 各自 `_test.go`；FilePicker 用 mock Dialog 验证契约回传；Theme 测试覆盖令牌解析、`SetTheme` 驱动插槽重绘。
- 窗口配置单测：`runtime/app/windowconfig_test.go`（默认值语义 / 三态 helper）、`backend/rust/go/windowconfig_test.go`（`decoratedC` 三态翻译）。
- 端到端截图：`examples/skia-style-demo` 展示 absolute 徽标、阴影卡片、transition 进度条、1000 项虚拟滚动列表、5 个企业级组件、Context/Theme 切换区；`preview-enterprise.png` 与 `preview-context-theme.png` 两张确认。无边框配置经 `GX_NODECOR=1` 验证无系统标题栏、无崩溃。

## 14. 三端消费矩阵：struct 白名单与后端实际消费面

host（struct/JSX）是三端样式白名单的**上界**，但三端对同一属性的消费面 historically 存在差异。现状（2026-11 补齐后）：

| 属性组 | Web/DOM | Skia/Yoga | Rust/Taffy |
|---|---|---|---|
| color/bg/width/height/font-size/padding/opacity | ✅ | ✅（1~4 值简写） | ✅（1~4 值简写） |
| border/border-radius/box-shadow/transform/transition/animation | ✅ | ✅ | ✅ |
| flex-direction/justify-content/align-items/gap | ✅ | ✅ | ✅ |
| flex-wrap/align-content/row-gap/column-gap | ✅ | ✅ | ✅（修正：旧版拿 column-gap 顶替 gap） |
| flex-grow/flex-shrink | ✅ | ✅ | ✅（显式 prop 覆盖 scrollable 默认值） |
| margin（简写 + 四边）/ min/max-width/height | ✅ | ✅ | ✅ |
| font-family / font-weight | ✅ | ✅（数值 ≥600 为粗体，font-weight 优先于 weight） | ✅（cosmic-text Weight） |
| text-align / line-height / white-space | ✅ | ✅（绘制落笔 + measure 同口径） | ✅（对齐偏移 + Metrics.line_height） |
| z-index | ✅ | ✅（绘制/命中顺序稳定排序） | ✅（emit 顺序稳定排序） |
| pointer-events / cursor | ✅ | ✅（HitBox 过滤 + Cursor 字段 → glwindow 切换系统光标） | ✅（hitbox 过滤 + cursor 随命中框透出） |
| 伪状态 hover/focus/active/disabled + ::selection | ✅ | ✅ | ✅ |

### 三端一致的文本语义细节

- **white-space**：默认（未声明/pre/pre-wrap）**逐字节渲染**（编辑器代码行缩进依赖原样空格）；显式 `normal`/`nowrap` 才折叠空白（连续空白 → 单空格、去首尾）。测量与绘制同口径（Go `collapseWhitespace` + `buildYogaTree` measure / Rust `collapse_whitespace` + `TextMeasure`）。
- **line-height**：无单位数值（<4）按字号倍数、带 px 的数值按绝对像素；沿继承链传递。Skia 的 measure 高度与绘制基线共用 `textStyle.lineH()`；Rust 的 `TextMeasure.line_height` 与 glyphon `Metrics.line_height` 同源。
- **text-align**：盒宽大于内容宽时 center/right 生效（Skia `alignOffset` + 落笔平移；Rust `align_offset` + TextDraw.x 偏移）。
- **z-index**：只重排绘制/命中顺序，不影响布局；负值垫底、正值置顶、同值保持文档序（稳定排序）。
- **pointer-events**：`none` 仅豁免节点**自身**命中（仍绘制），后代不受影响——与 CSS 命中语义一致。
- **cursor**：CSS 关键字映射（pointer/hand→手型、text→I-beam、crosshair、ew-resize/ns-resize 等），由窗口集成层消费（Go `glwindow.updateHoverCursor` / Rust hitbox 第 9 元素）。

### 验证

- Go：`backend/skia/style_text_parity_test.go`（字重数值/优先级、text-align 落笔、line-height 行盒、空白折叠、z-index 绘制顺序、pointer-events 命中豁免、cursor 透出）。
- Rust：`style.rs::parity_tests`（edges 简写/空白折叠/字重解析/对齐偏移/z-index 解析）+ `render.rs::parity_tests`（TextDraw 的对齐/行高/继承、z-index 绘制顺序、hitbox cursor/pointer-events、text_sig 字重失效）。
- **三端一致性矩阵**：`testdata/threeway/cases.json` + `scripts/threeway-parity.sh`。22 个共享用例（margin 简写/四值、min/max、flex-grow/shrink/wrap/默认方向、align-content、row/column-gap、padding、absolute inset、z-index、pointer-events 命中、cursor、text-align、line-height、white-space、scroll-top、样式透传）分别由三端无头消费并断言同一期望：
  - Skia（`backend/skia/threeway_test.go`）：无头 `Window.Draw` → DrawLog 矩形/文本/绘制序 + 命中框；
  - Rust（`render.rs::threeway_tests`）：`DisplayList::from_scene`（Taffy 布局同源）→ shape/text/hitbox；
  - Web（`backend/web/threeway_test.go`）：memDriver 镜像断言每个 prop **原样**到达元素属性面（透传链路）+ cssName/cssValue 转换抽查。
  跑 `bash scripts/threeway-parity.sh` 输出「用例 × 后端」矩阵，任一端断言失败即退出非零。三端任何一端"没实现/传值不对/语义漂移"都会在这里被点名。

### 已知默认值分歧（测试放行但需拍板统一）

矩阵显式声明属性以测传值链路；以下**默认值**三端不一致，改动任一侧都有大范围视觉回归风险，留待统一决策：

1. **white-space 默认值**：未声明时 Rust（`white_space_collapses("")`=false）**逐字节保留**；Skia（`whiteSpaceCollapses("")`=true）与浏览器 CSS 默认均**折叠空白**。STYLE.md 第 14 节文本语义细节描述的是 Rust 语义；编辑器等依赖原样空格的组件三端均须**显式声明 pre/pre-wrap** 才安全。
2. **flex-shrink 默认值**：Skia/Yoga 默认 0（不收缩），Rust/Taffy 与浏览器 CSS 默认 1（收缩）。内容超宽的容器三端行为不同；需要确定行为的容器应显式声明 flex-shrink。
3. **flex 容器默认主轴**：Skia/Rust 约定 column（零值 / `unwrap_or("column")`）；Web 端浏览器初始值 row，已由 js 驱动在 flex 意图属性出现时补 `flex-direction:column` 对齐（`flexDefaultColumn`，native 单测覆盖）。
