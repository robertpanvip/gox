package widgets

import (
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// 数据展示类组件：Text / Heading / Avatar / Stat / Tag / Spinner。
// 全部只产出带样式属性的场景树，不含交互状态。

// Text 预制文本。props:
//
//	value  string  文本内容（可传 getter）
//	tone   颜色（tone 名或 #rrggbb；默认 ThemeText）
//	size   字号（默认 14）
//	weight normal/bold（默认 normal）
//	align  left/center/right
func Text(props gxscene.Props) *gxscene.Instance {
	value := gxscene.Get[string](props, "value")
	tone := gxscene.Get[string](props, "tone")
	size := gxscene.Get[string](props, "size")
	weight := gxscene.Get[string](props, "weight")
	align := gxscene.Get[string](props, "align")

	sz := size()
	if sz == "" {
		sz = "14"
	}
	w := weight()
	if w == "" {
		w = "normal"
	}
	ops := []gxscene.Op{
		el("span"), attr("font-size", sz), attr("weight", w),
		dynAttr("color", 0), dynChild(1),
	}
	if a := align(); a != "" {
		ops = append(ops, attr("text-align", a))
	}
	ops = append(ops, closeEl())
	return gxscene.Render(gxscene.Template{Ops: ops, N: 2},
		func() any {
			if c := resolveColor(tone()); c != "" {
				return c
			}
			return ThemeText
		},
		func() any { return value() },
	)
}

// resolveColor 把 tone 名解析为颜色；已是 #rrggbb/#rrggbbaa 的原样返回。
func resolveColor(tone string) string {
	if tone == "" {
		return ""
	}
	if len(tone) > 0 && tone[0] == '#' {
		return tone
	}
	c, ok := tones[tone]
	if ok {
		return c
	}
	// 主题名
	switch tone {
	case "text":
		return ThemeText
	case "muted":
		return ThemeMuted
	case "border":
		return ThemeBorder
	case "panel":
		return ThemePanel
	case "bg":
		return ThemeBg
	}
	return ""
}

// Heading 预制标题。props: value, level(1/2/3), tone。
func Heading(props gxscene.Props) *gxscene.Instance {
	value := gxscene.Get[string](props, "value")
	level := gxscene.Get[int](props, "level")
	tone := gxscene.Get[string](props, "tone")

	sz := "18"
	switch level() {
	case 1:
		sz = "26"
	case 2:
		sz = "20"
	case 3:
		sz = "16"
	}
	ops := []gxscene.Op{
		el("span"), attr("font-size", sz), attr("weight", "bold"),
		dynAttr("color", 0), dynChild(1), closeEl(),
	}
	return gxscene.Render(gxscene.Template{Ops: ops, N: 2},
		func() any {
			if c := resolveColor(tone()); c != "" {
				return c
			}
			return ThemeText
		},
		func() any { return value() },
	)
}

// Avatar 预制头像（首字母圆形色块）。props:
//
//	text    string  显示文字/首字母（默认取 name 首字符）
//	name    string  用于取首字母
//	tone    背景强调色
//	size    直径（默认 40）
func Avatar(props gxscene.Props) *gxscene.Instance {
	text := gxscene.Get[string](props, "text")
	name := gxscene.Get[string](props, "name")
	tone := gxscene.Get[string](props, "tone")
	size := propFloat(props, "size", 40)
	d := fmtNum(size)

	const (
		bgSlot = iota
		textSlot
		nSlots
	)
	ops := []gxscene.Op{
		el("div"), attr("gx-role", "avatar"),
		attr("width", d), attr("height", d),
		attr("border-radius", fmtNum(size/2)),
		attr("align-items", "center"), attr("justify-content", "center"),
		dynAttr("bg", bgSlot),
		el("span"), attr("font-size", fmtNum(size*0.4)), attr("weight", "bold"),
		attr("color", ThemeOnAccent), dynChild(textSlot), closeEl(),
		closeEl(),
	}
	return gxscene.Render(gxscene.Template{Ops: ops, N: nSlots},
		func() any {
			if c := resolveColor(tone()); c != "" {
				return c
			}
			return ThemeAccent
		},
		func() any {
			if t := text(); t != "" {
				return t
			}
			n := name()
			if n == "" {
				return "?"
			}
			return n[:1]
		},
	)
}

// Stat 预制统计卡：大数字 + 说明 + 可选变化量。props:
//
//	value   string  主数值
//	label   string  说明
//	delta   string  变化量（如 "+12.5%"）
//	tone    主数值/变化量颜色
//	width   宽度
func Stat(props gxscene.Props) *gxscene.Instance {
	value := gxscene.Get[string](props, "value")
	label := gxscene.Get[string](props, "label")
	delta := gxscene.Get[string](props, "delta")
	tone := gxscene.Get[string](props, "tone")
	width := gxscene.Get[string](props, "width")

	accent := toneColor(tone())
	hasDelta := delta() != ""
	ops := []gxscene.Op{
		el("div"), attr("gx-role", "stat"),
		attr("bg", ThemePanel), attr("border", ThemeBorder), attr("border-width", HairlineWidth),
		attr("border-radius", RadiusCard), attr("box-shadow", ShadowCard), attr("padding", "14"),
		attr("flex-direction", "column"), attr("gap", "4"),
	}
	if w := width(); w != "" {
		ops = append(ops, attr("width", w))
	}
	ops = append(ops,
		el("span"), attr("font-size", "12"), attr("color", ThemeMuted), dynChild(0), closeEl(),
		el("span"), attr("font-size", "26"), attr("weight", "bold"),
		attr("color", accent), dynChild(1), closeEl(),
	)
	if hasDelta {
		ops = append(ops,
			el("span"), attr("font-size", "12"), attr("weight", "bold"),
			attr("color", accent), dynChild(2), closeEl(),
		)
	}
	ops = append(ops, closeEl())

	getters := []func() any{
		func() any { return label() },
		func() any { return value() },
	}
	n := 2
	if hasDelta {
		getters = append(getters, func() any { return delta() })
		n = 3
	}
	return gxscene.Render(gxscene.Template{Ops: ops, N: n}, getters...)
}

// Tag 预制标签（彩色小 chip，与 Badge 不同：Tag 带实色背景）。props:
//
//	label string, tone 颜色, outline bool（描边风格）
func Tag(props gxscene.Props) *gxscene.Instance {
	label := gxscene.Get[string](props, "label")
	tone := gxscene.Get[string](props, "tone")
	outline := gxscene.Get[bool](props, "outline")

	const (
		bgSlot = iota
		colorSlot
		borderSlot
		textSlot
		nSlots
	)
	ops := []gxscene.Op{
		el("span"), attr("gx-role", "tag"),
		attr("border-radius", "6"), attr("padding", "4"),
		attr("font-size", "12"), attr("weight", "bold"),
		dynAttr("bg", bgSlot), dynAttr("color", colorSlot),
		dynAttr("border", borderSlot), attr("border-width", HairlineWidth),
		dynChild(textSlot), closeEl(),
	}
	return gxscene.Render(gxscene.Template{Ops: ops, N: nSlots},
		func() any {
			if outline() {
				return ThemeInputBg
			}
			return toneColor(tone())
		},
		func() any {
			if outline() {
				return toneColor(tone())
			}
			return ThemeOnAccent
		},
		func() any {
			if outline() {
				return toneColor(tone())
			}
			return toneColor(tone())
		},
		func() any { return label() },
	)
}

// Spinner 预制加载指示器。props: size(直径,默认 24), tone。
//
// 用预制矢量 reload 图标（环形箭头）表达"加载中"，替代原先的 ◌ 字符——
// 字符在不同字体下会渲染成奇怪的小图形，矢量图标跨后端一致且清晰。
// 外层不再画圆环描边，避免矢量图标与描边叠成两个圆圈。
func Spinner(props gxscene.Props) *gxscene.Instance {
	tone := gxscene.Get[string](props, "tone")
	size := propFloat(props, "size", 24)
	toneName := tone()
	if toneName == "" {
		toneName = "accent"
	}
	ops := []gxscene.Op{
		el("span"), attr("gx-role", "spinner"),
		attr("width", fmtNum(size)), attr("height", fmtNum(size)),
		attr("align-items", "center"), attr("justify-content", "center"),
		gxscene.Op{Code: gxscene.OpComp, Arg: "SpinnerIcon", Slot: 0},
		closeEl(),
	}
	return gxscene.Render(gxscene.Template{Ops: ops, N: 1},
		func() any { return iconNode("reload", size*0.85, toneName) },
	)
}
