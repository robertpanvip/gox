# gox-skia-backend

基于真实 [Skia](https://skia.org/)（`github.com/pekim/go-skia` CGO 绑定）的 gox 渲染后端，实现 `gxapp.Window`。

**本目录是完全自包含的独立 Go module**，可整体切到其他仓库：

- 依赖方向严格单向：`本后端 → gox host runtime`，host 不知道后端存在。
- 接入点只有 `gxapp.Window` 一个接口，**host 代码零改动**。
- 真 Skia CGO 绘制（画矩形/文字/背景），非模拟实现。

## 目录结构

```
backend/skia/
  go.mod         独立 module（github.com/robertpanvip/gox-backend-skia）
  linkflags.go   修复上游 go-skia 的静态库链接顺序 bug（Linux）
  window.go      gxapp.Window 实现 + DrawLog 观测面
  paint.go       场景树遍历 → 布局 → Skia 绘制原语
  style.go       Node.Props 样式解析（本后端 additive 约定）
  run.go         接入胶水层 Run / OpenAndRender
  *_test.go      单元 + 端到端解耦验证（7 项全绿）
```

## 在本仓库内开发 / 测试

仓库根有 `go.work` 把 host 与本 module 联调：

```bash
# 后端测试（真 Skia CGO）
cd backend/skia && go test ./...

# host 全量回归（不受 go.work 影响）
cd ../.. && go test ./...
```

前置条件（真 Skia CGO）：
- Go 1.27+、clang/gcc、pkg-config、`freetype2` 与 `gl` 开发库。
- Linux 下 `libGL.so` + `libfreetype` headers（本沙箱已具备）。
- `go get github.com/pekim/go-skia@v0.1.0` 会拉取含预编译静态库（`lib/linux-amd64/*.a`，约 50MB）的模块，无需自行编译 Skia。

## 接入方式（业务侧）

GX 代码（外部符号小写，编译器还原大写）：

```gx
import gxapp "github.com/robertpanvip/gox/runtime/app"
import skia "github.com/robertpanvip/gox-backend-skia"

skia.run({ title: "Demo", width: 800, height: 600 }, () =>
  <div bg="#1a1a1a">
    <h1 color="#ff5555" font-size={32}>Hello Skia</h1>
  </div>
)
```

Go 侧等价：

```go
w, err := skia.Run(gxapp.WindowConfig{Title: "Demo", Width: 800}, func() any {
    return buildScene()
})
_ = w.DrawLog() // 最近一帧的绘制指令（观测面）
```

## 样式约定（本后端 additive，host 不解释）

| 属性 | 含义 | 示例 |
|---|---|---|
| `color` | 文字颜色 | `"#e63946"` / `"red"` / `"rgb(1,2,3)"` |
| `bg` / `background` | 元素背景色 | `"white"` |
| `width` / `height` | 尺寸（px） | `400` / `"300px"` |
| `font-size` | 字号 | `28` / `"28px"` |
| `padding` | 内边距 | `8` |
| `weight` | 字重 | `"bold"` |

未列出的属性被忽略。这些是后端与业务之间的约定，**不属于 host 协议**——换后端时样式约定可完全不同。

高级样式（`position:absolute` + inset、`overflow` + 虚拟滚动、`transition` 动画、`box-shadow`、`transform`、`opacity`）的完整约定见 [docs/STYLE.md](../../docs/STYLE.md)，两个后端语义一致。

## 预制控件库（`widgets` 子包）

`backend/skia/widgets` 提供一组跨后端通用的预制组件。组件是纯函数 `func(gxscene.Props) *gxscene.Instance`，产出「带样式属性的场景树」，后端零侵入；所有交互组件都是**受控**的：业务用 signal 持有状态，通过 `Bind*` 把 setter 包装成事件 handler 回传。

**视觉风格贴近 Apple HIG 深色模式**：中性灰系统色分层（窗口/分组/卡片/填充）、系统语义色（蓝/绿/红/橙/紫…）、连续大圆角、柔和多层阴影、`#rrggbbaa` 半透明材质近似毛玻璃、8pt 间距标度。常量集中在 `apple_theme.go`（`Apple*` / `Radius*` / `Shadow*` / `Space*`）；`Theme*` 保留旧名兼容业务，取值指向 Apple 系统色。详见 [docs/APPLE_DESIGN.md](../../docs/APPLE_DESIGN.md)。

| 分类 | 组件 | 关键 props |
|---|---|---|
| 基础 | `Button` / `Input` / `Text` / `Heading` / `Divider` | `label` `variant` `value` `onInput` `level` |
| 容器 | `Card` / `Row` / `Col` / `VirtualList` | `title` `children` `gap` `count` `render-item` |
| 反馈展示 | `Badge` / `Tag` / `Progress` / `Avatar` / `Stat` / `Alert` / `EmptyState` / `Skeleton` / `Spinner` | `label` `tone` `value` `title` |
| 表单交互 | `Checkbox` / `Switch` / `RadioGroup` / `Slider` | `checked` / `value` + `onChange`(BindBool/BindValue/BindNumber) |
| 导航选择 | `Tabs` / `Segmented` / `Select` | `value` `tabs`/`segments`/`options` `onChange` |
| 浮层 | `Popover` / `Modal` / `Portal` | `state`/`open` `trigger` `content` `title` `actions` `placement` |

事件绑定辅助：`BindInput(func(string))`、`BindBool(func(bool))`、`BindValue(func(string))`、`BindNumber(func(float64))`，与受控 signal 配合形成闭环。

**浮层三件套**（共用 `gx-role` 协议、两个后端语义一致，见 `overlay.go`）：

- `Popover`：贴锚点定位的轻量浮层，Dropdown/Menu/Tooltip 的底座；`Select` 基于它实现。
- `Modal`：`gx-role=modal` 视口居中 + scrim 遮罩 + 标题/内容/操作区；点遮罩关闭（`maskClosable`）、关闭按钮（`closable`）、ESC 关闭（`ModalHandleKey(state, key)` 由 GUI 键盘回调调用）。
- `Portal`：`gx-role=portal` 纯传送门——子树脱离父级 overflow 裁剪与 flex 流、铺满画布作为定位参考、最后绘制在最上层；子树用 `position:absolute` + `left`/`top` 自主定位。Modal/Toast/全局提示的共同底座。

`VirtualList` 只渲染可见窗口项（+ overscan），节点数与数据量解耦。

可运行示例见 `examples/skia-gallery/`：[全组件](../../examples/skia-gallery/preview.png) / [模态框](../../examples/skia-gallery/preview-modal.png) / [传送门](../../examples/skia-gallery/preview-portal.png) 截图。`GX_DEMO_MODAL=1` / `GX_DEMO_PORTAL=1` 环境变量可让对应浮层初始打开以便截图。

## 切到独立仓库

1. 把本 `backend/skia/` 目录整体移到新仓库 `gox-skia-backend`。
2. 编辑 `go.mod`：删除 `replace github.com/robertpanvip/gox => ../..`，把 host 依赖改为固定版本：
   ```
   require github.com/robertpanvip/gox vX.Y.Z
   ```
3. `go mod tidy && go test ./...`。

因接入只依赖 `gxapp.Window` 协议面，只要 host 的协议面保持兼容，切仓库后无需改任何绘制逻辑。

## 接入真实 GUI 窗口（当前为离屏后端）

当前 `Window` 用 `NewCanvasWithDimensions` 画到离屏 raster canvas（不依赖窗口系统，便于测试/批处理）。要接真实窗口：

- 用 glfw 创建窗口 + GL context，仿 `pekim/go-skia/demo`：
  `GrGLMakeNativeInterface → GrDirectContextsMakeGLInterface → SurfacesWrapBackendRenderTarget`
  得到绑定到 GL framebuffer 的 `Surface`，从 `surface.GetCanvas()` 取 canvas。
- 把 `Window.Open` 里创建的 canvas 换成该 GL canvas，`Draw` 的遍历/绘制逻辑不用改。
- 在 `Run()` 里驱动事件循环：命中测试 → `node.Dispatch(name, EventContext{...})` → signal 写入触发 host 自动重绘（`App.bindReactive` 已接好）。

协议面不变，host 依旧零改动。

## 已知边界

- `NewCanvasWithDimensions` 产生的 canvas 在本绑定里像素不可读（属 picture-recording 类），故本后端用 `DrawLog` 作为可观测面而非读回像素。真实 GL surface 可正常 `ReadPixels`。
- 链接顺序修复用 GNU ld 的 `--start-group`，仅 Linux；macOS 需去掉该 directive（`linkflags.go` 已可按平台加 build tag）。
- 布局是简单纵向流式，仅为证明节点分派；生产布局可替换 `painter` 的布局器。
