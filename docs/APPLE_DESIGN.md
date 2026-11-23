# Apple 风格设计系统（预制控件库视觉规范）

预制控件库 `backend/skia/widgets` 的视觉语言以 **Apple Human Interface Guidelines** 为参照，目标是让深色界面在配色层次、圆角、阴影、材质与排版上接近 macOS / iOS 的原生观感。两个后端（Skia/Yoga、Rust/Taffy）语义一致——所有约定都走 `Node.Props` 全量透传，零协议格式改动。

## 1. 设计令牌（Design Tokens）

常量定义在 `backend/skia/widgets/apple_theme.go`，按语义分组：

### 系统灰阶（深色模式）

| 令牌 | 取值 | 用途 |
|---|---|---|
| `AppleWindowBg` | `#1c1c1e` | 窗口 / 页面底 |
| `AppleGroupedBg` | `#000000` | 分组列表底 |
| `AppleElevated` | `#2c2c2e` | 卡片 / 二级底 |
| `AppleFill` / `AppleFill2` | `#3a3a3c` / `#48484a` | 控件填充 / 高亮 |
| `AppleSeparator` / `AppleSeparator2` | `#38383a` / `#545458` | 描边 / 分隔线 |
| `AppleLabel` / `AppleLabel2` / `AppleLabel3` | `#ffffff` / `#98989f` / `#6e6e73` | 主 / 次 / 三级文字 |

### 系统语义色

`AppleBlue` `#0a84ff`（主强调/链接）、`AppleGreen` `#30d158`、`AppleRed` `#ff453a`、`AppleOrange` `#ff9f0a`、`AppleYellow` `#ffd60a`、`ApplePurple` `#bf5af2`、`ApplePink` `#ff375f`、`AppleTeal` `#40c8e0`、`AppleIndigo` `#5e5ce6`。

`tone` 名（primary/cyan/purple/orange/green/danger/info…）映射到这些系统色；业务传 `#rrggbb` 则原样使用。

### 半透明材质（毛玻璃近似）

| 令牌 | 取值 | 用途 |
|---|---|---|
| `AppleMaterialThin` | `#2c2c2ed9` | 工具栏 / 轻浮层 |
| `AppleMaterialReg` | `#3a3a3ce6` | 卡片 / Popover / Toast |
| `AppleMaterialThick` | `#1c1c1ef2` | Modal 面板 |
| `AppleScrim` | `#00000099` | 遮罩（60% 黑） |
| `AppleHairline` | `#ffffff1f` | 12% 白高光描边（材质边缘） |

> **毛玻璃说明**：后端当前没有真正的 `backdrop-filter`（读回背景像素做高斯模糊）。材质用「带 alpha 的半透明底色 + 细高光描边 + 柔和阴影」近似，在有背景内容的窗口里能呈现接近 macOS material 的层次感。Rust/TinySkia 端目前对 alpha 支持有限，会退化为接近实色的观感——这是已知限制，不影响语义与布局。

### 圆角 / 阴影 / 间距标度

- 圆角：`RadiusSmall=8`、`RadiusControl=10`、`RadiusCard=14`、`RadiusPanel=18`、`RadiusModal=22`、`RadiusPill=999`（连续圆角 squircle 用偏大值近似）。
- 阴影：`ShadowCard` / `ShadowFloat` / `ShadowModal`（大扩散、低 alpha、向下偏移的多层近似）。
- 间距：`Space1..Space8`（4/8/12/16/20/24/32），遵循 8pt 网格。

## 2. 兼容性

`Theme*` 常量（`ThemeBg`/`ThemePanel`/`ThemeAccent`…）保留旧名称，取值改为对应的 Apple 系统色。因此既有业务代码与测试（用符号常量比较）无需改动即可升级到 Apple 观感；需要更细粒度控制时用 `Apple*` 令牌。

## 3. 浮层三件套

浮层角色由 `gx-role` 标记，脱离 flex 流、最后绘制、浮层优先命中。见 `backend/skia/overlay.go` 与 `docs/STYLE.md`。

- **Popover**（`gx-role=popover`）：贴锚点定位，Dropdown/Select/Menu/Tooltip 的底座。
- **Modal**（`gx-role=modal`）：相对画布居中（`placeCentered`，尺寸超画布时 clamp），配 scrim 遮罩与 Apple 风格面板。`Widget` 层提供 `ModalState`、`ModalHandleKey`（ESC 关闭）。
- **Portal**（`gx-role=portal`）：传送门。布局层把整棵子树从 flex 流末尾平移到画布原点，根 box 铺满画布作为定位参考；子树用 `position:absolute` + `left`/`top` 自主定位，最后绘制在最上层。Toast / 全局提示 / 抽屉都可基于它。

> Portal 子树请使用 **`left`/`top`** 定位（相对画布左上角的固定像素），不要用 `right`/`bottom`——后者依赖父盒尺寸，而 Portal 根在 flex 流末尾的原始高度可能异常，平移后 `right`/`bottom` 会算出画布外坐标。

## 4. 截图参考

`examples/skia-gallery/` 下：

- `preview.png`：全组件 Gallery（Apple 深色风格）
- `preview-modal.png`：居中模态框 + 遮罩
- `preview-portal.png`：传送门浮层提示

用 `GX_DEMO_MODAL=1` / `GX_DEMO_PORTAL=1` 环境变量运行 demo 可让对应浮层初始打开，便于 Xvfb + scrot 截图验证。
