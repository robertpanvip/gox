# Apple 风格设计系统（预制控件库视觉规范）

预制控件库 `widgets` 的视觉语言以 **Apple Human Interface Guidelines** 为参照，覆盖 macOS 桌面风格的明暗双主题。两个后端（Skia/Yoga、Rust/Taffy）共享同一套 `Node.Props` 样式协议，因此组件只描述语义与令牌，不绑定具体绘制后端。

## 1. 设计令牌（Design Tokens）

令牌定义分布在三个文件：

- `widgets/apple_theme.gx`：编译期系统色、圆角、控件高度、字号、阴影、间距标度。
- `widgets/theme.gx`：`Theme` 结构、`DarkTheme` / `LightTheme`、`SetTheme` / `UseToken` / `UseTone` / `ThemeProvider`。
- `widgets/tokens.gx`：给组件插槽使用的 `tk*` getter 快捷方式。

### 系统色（明暗双套）

| 语义 | 浅色 | 深色 |
|---|---|---|
| 主强调 Blue | `#007aff` | `#0a84ff` |
| 成功 Green | `#34c759` | `#30D158` |
| 危险 Red | `#ff3b30` | `#FF453A` |
| 警告 Orange | `#ff9500` | `#FF9F0A` |
| 紫 / 粉 / 靛 | `#af52de` / `#ff2d55` / `#5856d6` | `#BF5AF2` / `#FF375F` / `#5E5CE6` |
| 窗口底 | `#ffffff` | `#1e1e1e` |
| 分组底 | `#f2f2f7` | `#000000` |
| 卡片 / 抬升层 | `#ffffff` | `#2c2c2e` |
| 输入底 | `#ffffff` | `#1c1c1e` |
| 描边 | `#c6c6c8` | `#38383a` |
| 主文字 | `#1d1d1f` | `#f5f5f7` |
| 次文字 | `#86868b` | `#a1a1a6` |

`tone` 名（primary/success/warning/danger/info/purple/pink/teal/indigo…）映射到当前主题的系统色；传 `#rrggbb` / `#rrggbbaa` 则原样使用。

### 排版与布局

- 字号：largeTitle 26 / title1 22 / title2 17 / title3 15 / headline 13 bold / body 13 / callout 12 / subheadline 11 / footnote 10。
- 圆角：xsmall 4 / small 6 / control 6 / card 12 / panel 14 / modal 16 / pill 999。
- 控件高度：small 24 / regular 28 / large 32；列表行高 28。
- 间距：4 / 8 / 12 / 16 / 20 / 24 / 32，遵循 8pt 网格。
- 阴影：card / float / modal 为低 alpha、大扩散的多层近似，浅色主题自动减弱。

> 后端没有真正的 `backdrop-filter`，材质用带 alpha 的半透明底色 + hairline 描边 + 柔和阴影近似。Skia 支持 `#rrggbbaa`；Rust/TinySkia 对 alpha 的支持有限，可能退化为接近实色的观感。

### 单位：逻辑点（pt）与高 DPI 渲染

**设计系统里所有尺寸都是「逻辑点」(pt)，不是物理像素。** 上面列出的字号 13、控件高 28、圆角 6、间距 8……全是 pt。这与 Apple 的 point 体系一致：同样的 pt 值在任何屏幕上占据相同的视觉大小，屏幕越密、用来绘制它的物理像素越多，观感越清晰。

- **布局**在逻辑坐标系进行：可用宽高 = framebuffer 物理尺寸 ÷ DPR。因此一个 13pt 的字号在 2x / 3x 屏上仍占 13pt 的逻辑宽度，不会变小。
- **绘制**时后端先把画布缩放 DPR 倍，再用物理像素光栅化 glyph、描边与圆角（13pt → 26/39 物理像素），达到 Retina 清晰度。
- **命中测试**始终用逻辑坐标：GLFW 光标坐标（`GetCursorPos`）与布局同一坐标系，无需除以 DPR。

Skia GL 真实窗口通过 `skia.PaintToScaled(canvas, physW, physH, dpr, ...)` 接入该模型；DPR 取 `framebuffer 尺寸 / 窗口逻辑尺寸`。离屏 `PaintTo` 与 `Window`（测试/截图用途）默认 DPR=1，行为不变。Web 后端走 DOM/CSS，浏览器原生处理 devicePixelRatio，无需额外换算。

## 2. 主题切换与兼容性

```go
widgets.SetTheme(widgets.LightTheme)          // 全局切换
accent := widgets.UseToken("accent")          // 响应式 getter
inst := widgets.Button(gxscene.Props{ /* ... */ })
```

- `UseToken(name)` / `UseTone(name)` 返回随主题变化的 getter，接到 `dynAttr` 插槽上可自动重绘。
- `ThemeProvider` 提供子树级主题覆盖，嵌套时内层就近生效，不影响全局主题。
- 旧版 `Theme*`（`ThemeBg` / `ThemePanel` / `ThemeAccent` …）保留同名导出符号，但已从常量改为变量；`SetTheme` 会把它们同步为当前主题令牌。这样既有业务代码与测试仍可直接比较符号，浅色主题下也不会残留深色控件。
- `ShadowCard` / `ShadowFloat` / `ShadowModal` 同样随主题同步，浅色主题使用更轻的阴影。

### 阴影：三端像素对齐

Apple HIG 的阴影是**极低不透明度的多层柔和叠加**（大 blur、近零 offset），深色背景上需约 2× alpha 才可见。三端共用同一套 token 与同一套 **6 层矩形近似高斯算法**，因此同一 token 在三端视觉一致：

- **Skia（桌面）** `backend/skia/paint.go:paintShadow`：for i=6..1，`expand = spread + blur·(i/6)`，`a = baseA·(1−i/6)/6·1.8`，画圆角矩形。
- **Rust（原生）** `backend/rust/rust/src/render.rs:paint_shadow_tiny`：逐行同算法。
- **Web（浏览器）** `backend/web/stylemap.go:expandBoxShadow`：不再用浏览器原生高斯，而是把单层 token 展开成 6 层 CSS `box-shadow`（`ox oy 0px expand rgba(...,a/255)`），逐层复刻上面两端公式，输出顺序 i=1..6 对应 CSS 首个阴影在最上层。

token 灰度按叠加大致 `中心 alpha ≈ token alpha × 0.71` 校准，故取值略高于目标视觉值。测试 `TestExpandBoxShadowMatchesSkia` 逐层比对 expand 与 alpha，保证三端公式不漂移。

## 3. 浮层三件套

浮层角色由 `gx-role` 标记，脱离 flex 流、最后绘制、浮层优先命中。见 `docs/STYLE.md`。

- **Popover**：贴锚点定位，Dropdown / Select / Menu 的底座。
- **Modal**：居中面板 + scrim 遮罩，提供 `ModalState` 与 `ModalHandleKey`（ESC 关闭）。
- **Portal / Tooltip**：Portal 用 `left`/`top` 自主定位；Tooltip 是轻量浮层，无 scrim、hover 触发。

## 4. 可视化验证

`examples/widget-showcase/` 是完整的组件库展示程序，覆盖按钮、输入、表单控件、导航、滑块进度、下拉、统计标签头像、提示、卡片、表格、表单、空状态、骨架屏、时间选择器与浮层提示。

```bash
go run ./examples/widget-showcase
GX_THEME=light go run ./examples/widget-showcase
```

在沙箱/无头环境中用 Xvfb + scrot 截图：

```bash
xvfb-run -a --server-args="-screen 0 1100x3800x24" bash -c \
  'GX_THEME=dark GX_SHOT_MS=12000 go run ./examples/widget-showcase & \
   sleep 5; scrot /tmp/showcase-dark.png; wait'
```

仓库内保留的最终对照图：`examples/widget-showcase/preview-dark.png`、`preview-light.png`（若以截图产物形式提交）。
