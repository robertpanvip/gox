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

## 增量帧（FramePresenter，`present.go`）

`Window` 现在实现 `gxapp.FramePresenter`，消费 Host 的 `Frame.Changes` 增量流——补齐了三端里 skia 唯一没接增量的缺口（Web/Rust 早已接）。App 装配时自动走 `Present`，老 `Window.Draw` 路径完全不变（headless 测试、PaintTo/Cached 零影响）。

- **快速路径**：`len(Changes)==0 && 无动画 && 非 FullSync && 根/尺寸不变` 时，画面必然未变，直接复用上帧 log/hits 与画布，**跳过每帧 O(N) 的 `snapshotTree`**（O(N)→O(1)）。
- **慢速路径**：有 Change / 动画 / FullSync 时仍走 `renderCached` 的全量快照 + 脏矩形局部重绘，保持正确性。
- **正确性依据**：所有画面变化都经 Change 流（signal 写回 `record(ChangeSetAttr/Text/结构指令)`，命令式 CanvasContext 写回也 `record`）；唯一例外是 transition 动画——`Animator.apply` 渲染期临时写 props、不产生 Change，故用 `Animator.Active()` 显式排除。FullSync 是丢帧恢复兜底，强制全量。

**基准**（1000 节点树，无变化帧，3 轮）：

| 路径 | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `Draw`（每帧全量 snapshotTree） | ~2,180,000 | 1,858,212 | 23,067 |
| `Present`（O(1) 快速复用） | ~35,000 | 155,648 | 2 |

约 **62× 快、11,553× 少分配**。复跑：`go test ./backend/skia -run '^$' -bench BenchmarkNoChangeFrame -benchmem`。

## 桌面 glWindow 的两级增量

桌面 `glWindow`（`examples/playground/desktop/glwindow.go`）实现了 `FramePresenter`，两级增量叠加：

1. **帧级短路**（主要收益）：无 Change 帧不置 `dirty`，`paintNow` 跳过整条绘制/blit/swap 链，屏幕保持上帧。首帧强制绘制（建树期不记录 Change）；标准动画（signal/RAF/命令式写回）都产生 Change、自动放行，`ForceRedraw()` 给旁路动画留逃生口。
2. **像素级脏矩形**（架构接通）：桌面 canvas 是跨帧持久的 CPU raster（`SurfacesWrapBackendRenderTarget` 实为 `NewCanvasWithDimensions` 的 Pixmap，像素可读、不随 swap 失效），因此有 Change 帧走 `PaintToCachedScaled` → `renderCachedKnownChange`，复用 `FrameCache` 的布局差分，几何变化（光标/选区/滚动）只重画脏区。该路径由 Present 已确认有变化，跳过 O(N) 快照比对；resize 重建画布时清空缓存。

**收益要据实看**（`BenchmarkDesktopDprDirtyRect` / `BenchmarkDesktopGeometryDirty`，DPR=2、1600×1000、40 节点）：

| 场景 | 全量 `PaintToScaled` | 缓存 `PaintToCachedScaled` | 结论 |
|---|---:|---:|---|
| 无变化帧（帧级短路） | 全量绘制链 | 整帧跳过 | 主要收益，上一轮已落地 |
| 纯文本变化（几何不变） | ~1.38 ms | ~1.40 ms | **基本持平**：脏区为 0 → 全量重画，但省了快照抵消 diff 开销 |
| 几何变化（光标移动） | ~1.48 ms | ~1.49 ms | **基本持平**：纯 Go canvas 绘制调用相对廉价，O(N) 遍历/log 记录省不掉 |

原因：`renderCached` 需为整棵树记录完整 `DrawLog`/`HitBox`，`paintTree` 遍历、`inheritedStyle`/`parseStyle` 解析、`computeLayout`+`diffLayout` 都是 O(N) 固定成本，脏矩形只能省下不相交节点的 `DrawString`/`DrawRect` 调用——在纯 Go canvas（字形有缓存）下这部分不够贵，收益被固定开销淹没。**脏矩形基础设施已接通且正确，未来若换回真 Skia GL surface 或字体 shaping 变昂贵即可释放收益**；当前桌面端真正的性能收益来自帧级短路。勿把本节数据宣传为"N 倍提速"。

## 已知边界

- `NewCanvasWithDimensions` 产生的 canvas 像素在本后端可直接读回（`canvas.Pixmap().RGBA8888Bytes()`），`DrawLog` 仍作为主要可观测面。
- 链接顺序修复用 GNU ld 的 `--start-group`，仅 Linux；macOS 需去掉该 directive（`linkflags.go` 已可按平台加 build tag）。
- 布局是简单纵向流式，仅为证明节点分派；生产布局可替换 `painter` 的布局器。
