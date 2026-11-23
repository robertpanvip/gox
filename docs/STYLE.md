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

## 验证

- 单元测试：两端各有解析 / 定位 / 裁剪 / 插值 / 阴影单测（Go `go test ./...`、Rust `cargo test --release`）。
- 端到端截图：`examples/skia-style-demo` 展示 absolute 徽标、阴影卡片、transition 进度条、1000 项虚拟滚动列表；首帧与动画后各截一张确认。
