# Gox Icon Gallery

用 GX 语言编写的**图标组件 showcase**，展示 `widgets` 组件库预制的矢量图标组件集。

## 运行

```bash
# 1. 转译 GX → Go（图标库变动后需重新转译本示例）
cd <仓库根>
gox build examples/icon-gallery/app.gx -o examples/icon-gallery/main.go
# 或用仓库工具链：
GOTOOLCHAIN=go1.27.4 go run ./cmd/gox build examples/icon-gallery/app.gx -o examples/icon-gallery/main.go

# 2. 运行（headless 后端，直接把场景树打印到 stdout）
cd examples/icon-gallery
go run .
```

无需 GUI / OpenGL：使用 `gxapp.open` 的 headless 默认后端，`app.render` 构建场景树后即以文本形式打印，可验证图标解析与场景树构建全链路。输出中每个图标都是带 `canvas-ops` 矢量指令的 `<canvas>` 节点。

## 演示内容

1. **具名组件强类型用法**（`NamedDemo`）：直接以组件形式引用预制图标，GX 源码写小写 `widgets.iconHouse` / `widgets.iconSearch` / `widgets.iconHeart` / `widgets.iconStar` / `widgets.iconGear` / `widgets.iconBolt`，编译器 upperFirst 还原为 `widgets.IconHouse` 等；`size` / `color` / `tone` 由 props 决定。

2. **全量图标网格**：`<For each={names}>` 遍历 `runtime/icon.Names()` 的全部内置图标名（当前 85 个），每项用通用 `<widgets.icon name={name} size="24"/>` 渲染 + 名称文本，验证整套图标都能走通 host 解析 → canvas 矢量指令管线。

> 注意：GX 组件名里的连字符不会被还原为驼峰（写 `widgets.icon-house` 会生成非法的 `widgets.Icon-house`）。具名图标组件本身是 PascalCase（`IconEyeOff` / `IconTextField`），源码写 `widgets.iconEyeOff` / `widgets.iconTextField` 即可正常还原。带连字符的图标名（如 `eye-off`）用通用 `<widgets.icon name="eye-off"/>` 传字符串。

## 文件

- `app.gx` — GX 源（唯一手写源）
- `main.go` — `gox build` 生成的转译产物
- `go.mod` — 独立模块，`replace github.com/robertpanvip/gox-sample => ../..`
