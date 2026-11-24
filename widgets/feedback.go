// gx:nocollections
// 本文件为原生 Go 组件源码，经 gox build 转译为 feedback.go。
// gx:nocollections 声明跳过集合/裸对象字面量 desugar，保留手写 opcode 的原生 Go 语义。
package widgets

import (
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// 反馈类组件：Alert / EmptyState / Skeleton。
// 这些组件展示状态信息，通常配合 signal 条件挂载（OpBranch）显示/隐藏。

// Alert 预制提示条。props:
//
//	title    string  标题
//	message  string  正文
//	tone     info/success/warning/danger（决定左侧色条与图标色）
//	width    宽度
func Alert(props gxscene.Props) *gxscene.Instance {
	title := gxscene.Get[string](props, "title")
	message := gxscene.Get[string](props, "message")
	tone := gxscene.Get[string](props, "tone")
	width := gxscene.Get[string](props, "width")

	accent := toneColor(tone())
	const (
		titleSlot = iota
		msgSlot
		nSlots
	)
	ops := []gxscene.Op{
		el("div"), attr("gx-role", "alert"),
		attr("bg", ThemePanel), attr("border-radius", RadiusCard),
		attr("padding", "12"), attr("flex-direction", "row"), attr("gap", "10"),
	}
	if w := width(); w != "" {
		ops = append(ops, attr("width", w))
	}
	ops = append(ops,
		// 左侧色条
		el("div"), attr("width", "4"), attr("height", "36"),
		attr("border-radius", "2"), attr("bg", accent), closeEl(),
		// 文本列
		el("div"), attr("flex-direction", "column"), attr("gap", "3"),
		el("span"), attr("font-size", FontHeadline), attr("weight", "bold"),
		attr("color", accent), dynChild(titleSlot), closeEl(),
		el("span"), attr("font-size", FontBody), attr("color", ThemeMuted),
		dynChild(msgSlot), closeEl(),
		closeEl(),
		closeEl(),
	)
	return gxscene.Render(gxscene.Template{Ops: ops, N: nSlots},
		func() any { return title() },
		func() any { return message() },
	)
}

// EmptyState 空状态占位。props: icon, title, description, width。
func EmptyState(props gxscene.Props) *gxscene.Instance {
	icon := gxscene.Get[string](props, "icon")
	title := gxscene.Get[string](props, "title")
	desc := gxscene.Get[string](props, "description")
	width := gxscene.Get[string](props, "width")

	const (
		titleSlot = iota
		descSlot
		nSlots
	)
	ops := []gxscene.Op{
		el("div"), attr("gx-role", "empty"),
		attr("flex-direction", "column"), attr("align-items", "center"),
		attr("justify-content", "center"), attr("gap", "8"), attr("padding", "28"),
	}
	if w := width(); w != "" {
		ops = append(ops, attr("width", w))
	}
	ic := icon()
	if ic == "" {
		ic = "◻"
	}
	ops = append(ops,
		el("span"), attr("font-size", "34"), attr("color", ThemeMuted),
		gxscene.Op{Code: gxscene.OpChildText, Arg: ic}, closeEl(),
		el("span"), attr("font-size", FontTitle2), attr("weight", "bold"),
		attr("color", ThemeText), dynChild(titleSlot), closeEl(),
		el("span"), attr("font-size", FontBody), attr("color", ThemeMuted),
		dynChild(descSlot), closeEl(),
		closeEl(),
	)
	return gxscene.Render(gxscene.Template{Ops: ops, N: nSlots},
		func() any { return title() },
		func() any { return desc() },
	)
}

// Skeleton 骨架屏占位块。props: width, height, tone, radius。
func Skeleton(props gxscene.Props) *gxscene.Instance {
	tone := gxscene.Get[string](props, "tone")
	width := gxscene.Get[string](props, "width")
	height := gxscene.Get[string](props, "height")
	radius := gxscene.Get[string](props, "radius")

	ops := []gxscene.Op{
		el("div"), attr("gx-role", "skeleton"),
		attr("bg", resolveColor(tone())), attr("opacity", "0.55"),
	}
	if w := width(); w != "" {
		ops = append(ops, attr("width", w))
	}
	if h := height(); h != "" {
		ops = append(ops, attr("height", h))
	} else {
		ops = append(ops, attr("height", "14"))
	}
	r := radius()
	if r == "" {
		r = RadiusXSmall
	}
	ops = append(ops, attr("border-radius", r), closeEl())
	return gxscene.Render(gxscene.Template{Ops: ops, N: 0})
}
