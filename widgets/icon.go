// gx:nocollections
// 本文件为原生 Go 组件源码，经 gox build 转译为 icon.go。
// gx:nocollections 声明跳过集合/裸对象字面量 desugar，保留手写 opcode 的原生 Go 语义。
// Icon 是 GX 通用组件层的矢量图标组件。
//
// 与 Img/Canvas 同构：只产出一个后端无关的 gxscene 场景树节点（kind="canvas"），
// 矢量路径编码进 Props["canvas-ops"]，由两后端已有的矢量 path 绘制（零后端改动）。
// SVG 解析全部在 host 层（runtime/icon）完成，全程矢量、放大不失真、零位图。
//
//   - name:    内置图标名（如 "house"/"search"/"heart"，支持别名 "home"/"user"）
//   - src:     外部 SVG（原始 SVG 文本 / data:image/svg+xml / .svg 文件路径 / http URL）
//              name 优先；两者都无则报错降级为空图标
//   - size:    显示尺寸 px（默认 20），viewBox 内容等比缩放居中
//   - color:   图标颜色（CSS 色）；缺省用 tone 对应的苹果系统色
//   - tone:    语义色名（primary/blue/red/... 默认 primary）
//   - stroke-width: 描边宽度（默认 1.8，随 size 缩放）
package widgets

import (
	"strconv"

	"github.com/robertpanvip/gox/runtime/canvas"
	gxicon "github.com/robertpanvip/gox/runtime/icon"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// IconOptions 是图标尺寸/颜色/描边的解析结果，供通用组件与具名组件共用。
type IconOptions struct {
	Size      float64
	Color     string
	StrokeW   float64
	HasSVG    bool
	SVG       string
}

// parseIconProps 统一从 props 解析 size/color/tone/stroke-width。
func parseIconProps(props gxscene.Props) IconOptions {
	o := IconOptions{Size: 20, StrokeW: 1.8}
	if v := gxicon.NumString(gxscene.Prop(props, "size")()); v != "" {
		if f, err := strconv.ParseFloat(v, 32); err == nil && f > 0 {
			o.Size = f
		}
	}
	o.Color = gxscene.Get[string](props, "color")()
	if o.Color == "" {
		tone := gxscene.Get[string](props, "tone")()
		if tone == "" {
			tone = "primary"
		}
		o.Color = appleToneColor(tone)
	}
	if v := gxicon.NumString(gxscene.Prop(props, "stroke-width")()); v != "" {
		if f, err := strconv.ParseFloat(v, 32); err == nil && f > 0 {
			o.StrokeW = f
		}
	}
	return o
}

// iconSVGNode 把已解析的 SVG + 选项编码成 kind="canvas" 矢量节点。
// name/src 都为空时降级为空白画布（不崩溃）。
func iconSVGNode(name, src string, o IconOptions) *gxscene.Instance {
	var ops []canvas.Op
	if name != "" || src != "" {
		parsed, err := gxicon.IconOps(name, src, gxicon.ParseOptions{
			Size:        float32(o.Size),
			Color:       o.Color,
			StrokeWidth: float32(o.StrokeW),
		})
		if err == nil {
			ops = parsed
		}
	}
	nodeOps := []gxscene.Op{
		el("canvas"),
		attr("width", strconv.FormatFloat(o.Size, 'f', -1, 64)),
		attr("height", strconv.FormatFloat(o.Size, 'f', -1, 64)),
		attr(canvas.PropKey, canvas.Encode(ops)),
		closeEl(),
	}
	return gxscene.Render(gxscene.Template{Ops: nodeOps, N: 0})
}

// Icon 通用矢量图标组件：按 name（内置名/别名）或 src（外部 SVG）解析。
//
//	widgets.Icon(gxscene.Props{"name": "house", "size": 24, "tone": "primary"})
//	widgets.Icon(gxscene.Props{"src": "<svg ...>", "size": 20, "color": "#fff"})
//
// 想用强类型具名组件（如 <IconHouse/>）见 iconSet.gx。
func Icon(props gxscene.Props) *gxscene.Instance {
	name := gxscene.Get[string](props, "name")()
	src := gxscene.Get[string](props, "src")()
	return iconSVGNode(name, src, parseIconProps(props))
}

// iconFromSVG 供 iconSet.gx 的具名组件调用：固定 SVG + 调用方 props。
func iconFromSVG(svg string, props gxscene.Props) *gxscene.Instance {
	return iconSVGNode("", svg, parseIconProps(props))
}
