// gx:nocollections
// 本文件为原生 Go 组件源码，经 gox build 转译为 form.go。
// gx:nocollections 声明跳过集合/裸对象字面量 desugar，保留手写 opcode 的原生 Go 语义。
package widgets

import (
	"fmt"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// 表单交互组件：Checkbox / Switch / RadioGroup / Slider。
//
// 交互约定与 Input 一致——组件只负责"点击时把新值通过 onChange handler 回传"，
// 业务用 Bind* 包装自己的 setter 接收新值并写回 signal。这样组件保持无状态，
// 信号所有权始终在业务侧，与响应式系统天然契合。
//
//	checked := signal.New(false)
//	widgets.Checkbox(Props{
//	    "checked": func() any { return checked() },
//	    "onChange": widgets.BindBool(func(v bool) { checked(v) }),
//	    "label": "我已阅读",
//	})

const (
	dataChecked = "checked"
	dataValue   = "value"
)

// ---------- 事件绑定辅助（与 BindInput 对称）----------

// BindBool 把 func(bool) 包装成 Checkbox / Switch 的 onChange Handler。
func BindBool(fn func(bool)) gxscene.Handler {
	return gxscene.Handler(func(ctx gxscene.EventContext) {
		v := false
		if ctx.Synthetic != nil {
			if b, ok := ctx.Synthetic.Data[dataChecked].(bool); ok {
				v = b
			}
		}
		fn(v)
	})
}

// BindValue 把 func(string) 包装成 RadioGroup / Select / Segmented 的 onChange Handler。
func BindValue(fn func(string)) gxscene.Handler {
	return gxscene.Handler(func(ctx gxscene.EventContext) {
		v := ""
		if ctx.Synthetic != nil {
			if s, ok := ctx.Synthetic.Data[dataValue].(string); ok {
				v = s
			}
		}
		fn(v)
	})
}

// BindNumber 把 func(float64) 包装成 Slider 的 onChange Handler。
func BindNumber(fn func(float64)) gxscene.Handler {
	return gxscene.Handler(func(ctx gxscene.EventContext) {
		var v float64
		if ctx.Synthetic != nil {
			if f, ok := toFloat(ctx.Synthetic.Data[dataValue]); ok {
				v = f
			}
		}
		fn(v)
	})
}

// withData 在事件上下文里附加数据键，保留原始事件信息（Raw/Synthetic）不丢。
func withData(ctx gxscene.EventContext, key string, val any) gxscene.EventContext {
	if ctx.Synthetic == nil {
		ctx.Synthetic = &gxscene.SyntheticEvent{Data: map[string]any{}}
	}
	if ctx.Synthetic.Data == nil {
		ctx.Synthetic.Data = map[string]any{}
	}
	ctx.Synthetic.Data[key] = val
	return ctx
}

func emitChecked(h gxscene.Handler, ctx gxscene.EventContext, v bool) {
	if h != nil {
		h(withData(ctx, dataChecked, v))
	}
}

func emitValue(h gxscene.Handler, ctx gxscene.EventContext, v string) {
	if h != nil {
		h(withData(ctx, dataValue, v))
	}
}

func emitNumber(h gxscene.Handler, ctx gxscene.EventContext, v float64) {
	if h != nil {
		h(withData(ctx, dataValue, v))
	}
}

// ---------- Checkbox ----------

// Checkbox 复选框。props:
//
//	checked  func() bool      当前是否选中（通常传 signal getter）
//	onChange Handler          变更回传，用 BindBool 包装
//	label    string           右侧文字（可选）
//	tone     强调色（默认 primary）
//	size     方框边长（默认 18）
func Checkbox(props gxscene.Props) *gxscene.Instance {
	checked := gxscene.Get[bool](props, "checked")
	label := gxscene.Get[string](props, "label")
	tone := gxscene.Get[string](props, "tone")
	onChange := gxscene.Get[gxscene.Handler](props, "onChange")
	box := fmtNum(propFloat(props, "size", 18))

	const (
		clickSlot = iota // 0
		boxBgSlot        // 1
		boxBorderSlot    // 2
		markOpacitySlot  // 3
		labelTextSlot    // 4
		nSlots           // 5
	)
	ops := []gxscene.Op{
		el("div"), attr("gx-role", "checkbox"),
		attr("flex-direction", "row"), attr("align-items", "center"), attr("gap", "9"),
		event("onClick", clickSlot),
		// 方框（relative 定位参照）
		el("div"), attr("width", box), attr("height", box),
		attr("border-radius", "4"), attr("border-width", "2"), attr("position", "relative"),
		dynAttr("bg", boxBgSlot), dynAttr("border", boxBorderSlot),
		// 选中勾：absolute 居中 + opacity 控制显隐，不占 flex 主轴空间。
		el("span"), attr("position", "absolute"), attr("top", "-2"), attr("left", "2"),
		attr("font-size", "13"), attr("weight", "bold"), attr("color", "#07130f"),
		dynAttr("opacity", markOpacitySlot),
		gxscene.Op{Code: gxscene.OpChildText, Arg: "✓"}, closeEl(),
		closeEl(),
		// 标签
		el("span"), attr("font-size", "14"), attr("color", ThemeText),
		dynChild(labelTextSlot), closeEl(),
		closeEl(),
	}
	return gxscene.Render(gxscene.Template{Ops: ops, N: nSlots},
		func() any { // 0 onClick
			return gxscene.Handler(func(ctx gxscene.EventContext) {
				emitChecked(onChange(), ctx, !checked())
			})
		},
		func() any { // 1 box bg
			if checked() {
				return toneColor(tone())
			}
			return ThemeInputBg
		},
		func() any { // 2 box border
			if checked() {
				return toneColor(tone())
			}
			return ThemeMuted
		},
		func() any { // 3 勾显隐
			if checked() {
				return "1"
			}
			return "0"
		},
		func() any { return label() }, // 4 标签
	)
}

// ---------- Switch ----------

// Switch 开关。props: checked, onChange(BindBool), label, tone, width(轨道宽,默认44)。
func Switch(props gxscene.Props) *gxscene.Instance {
	checked := gxscene.Get[bool](props, "checked")
	label := gxscene.Get[string](props, "label")
	tone := gxscene.Get[string](props, "tone")
	onChange := gxscene.Get[gxscene.Handler](props, "onChange")
	trackW := propFloat(props, "width", 44)

	const trackH, thumb = 24.0, 18.0
	pad := (trackH - thumb) / 2
	travel := trackW - thumb - 2*pad

	const (
		clickSlot = iota // 0
		trackBgSlot      // 1
		thumbLeftSlot    // 2
		labelSlot        // 3
		nSlots           // 4
	)
	ops := []gxscene.Op{
		el("div"), attr("gx-role", "switch"),
		attr("flex-direction", "row"), attr("align-items", "center"), attr("gap", "10"),
		event("onClick", clickSlot),
		// 轨道
		el("div"), attr("width", fmtNum(trackW)), attr("height", fmtNum(trackH)),
		attr("border-radius", fmtNum(trackH/2)), attr("position", "relative"),
		dynAttr("bg", trackBgSlot),
		// 滑块
		el("div"), attr("position", "absolute"), attr("top", fmtNum(pad)),
		attr("width", fmtNum(thumb)), attr("height", fmtNum(thumb)),
		attr("border-radius", fmtNum(thumb/2)), attr("bg", "#f8fafc"),
		attr("box-shadow", "0 1px 3px 0 rgba(0,0,0,0.4)"),
		attr("transition", "left 180ms ease"),
		dynAttr("left", thumbLeftSlot),
		closeEl(), closeEl(),
		// 标签
		el("span"), attr("font-size", "14"), attr("color", ThemeText),
		dynChild(labelSlot), closeEl(),
		closeEl(),
	}
	return gxscene.Render(gxscene.Template{Ops: ops, N: nSlots},
		func() any {
			return gxscene.Handler(func(ctx gxscene.EventContext) {
				emitChecked(onChange(), ctx, !checked())
			})
		},
		func() any {
			if checked() {
				return toneColor(tone())
			}
			return ThemeBorder
		},
		func() any {
			left := pad
			if checked() {
				left = pad + travel
			}
			return fmtNum(left)
		},
		func() any { return label() },
	)
}

// ---------- RadioGroup ----------

// RadioGroup 单选组。props:
//
//	value      func() string   当前选中值
//	options    []Option 等     候选项（见 optionsFrom 支持的形态）
//	onChange   Handler         变更回传，用 BindValue 包装
//	direction  "row"/"column"  排列方向（默认 column）
//	tone       强调色
//	gap        间距（默认 10）
func RadioGroup(props gxscene.Props) *gxscene.Instance {
	value := gxscene.Get[string](props, "value")
	options := optionsFrom(props, "options")
	onChange := gxscene.Get[gxscene.Handler](props, "onChange")
	direction := gxscene.Get[string](props, "direction")
	tone := gxscene.Get[string](props, "tone")
	gap := gxscene.Get[string](props, "gap")

	dir := direction()
	if dir == "" {
		dir = "column"
	}
	g := gap()
	if g == "" {
		g = "10"
	}

	forCtl := gxscene.NewFor(
		func() []any {
			opts := options()
			out := make([]any, len(opts))
			for i, o := range opts {
				out[i] = o
			}
			return out
		},
		func(v any) any { return v.(Option).Value },
		func(v any) *gxscene.Instance { return radioItem(v.(Option), value, tone(), onChange()) },
	)
	ops := []gxscene.Op{
		el("div"), attr("gx-role", "radiogroup"),
		attr("flex-direction", dir), attr("gap", g),
		gxscene.Op{Code: gxscene.OpFor, Slot: 0},
		closeEl(),
	}
	return gxscene.Render(gxscene.Template{Ops: ops, N: 1},
		func() any { return forCtl },
	)
}

func radioItem(o Option, value func() string, tone string, onChange gxscene.Handler) *gxscene.Instance {
	selected := func() bool { return value() == o.Value }
	ops := []gxscene.Op{
		el("div"), attr("gx-role", "radio"),
		attr("flex-direction", "row"), attr("align-items", "center"), attr("gap", "8"),
		event("onClick", 0),
		// 外圈
		el("div"), attr("width", "18"), attr("height", "18"),
		attr("border-radius", "9"), attr("border-width", "2"),
		attr("align-items", "center"), attr("justify-content", "center"),
		dynAttr("border", 1),
		// 内点
		el("div"), attr("width", "8"), attr("height", "8"), attr("border-radius", "4"),
		dynAttr("bg", 2), closeEl(),
		closeEl(),
		// 标签
		el("span"), attr("font-size", "14"), dynAttr("color", 3),
		gxscene.Op{Code: gxscene.OpChildText, Arg: optionLabel(o)}, closeEl(),
		closeEl(),
	}
	disabled := o.Disabled
	return gxscene.Render(gxscene.Template{Ops: ops, N: 4},
		func() any {
			return gxscene.Handler(func(ctx gxscene.EventContext) {
				if disabled {
					return
				}
				emitValue(onChange, ctx, o.Value)
			})
		},
		func() any {
			if selected() {
				return toneColor(tone)
			}
			return ThemeMuted
		},
		func() any {
			if selected() {
				return toneColor(tone)
			}
			return ThemeInputBg
		},
		func() any {
			if selected() {
				return ThemeText
			}
			return ThemeMuted
		},
	)
}

// ---------- Slider ----------

// Slider 滑块。props:
//
//	value    func() float64   当前值
//	min,max  number           范围（默认 0..100）
//	step     number           步长（默认 1）
//	onChange Handler          变更回传，用 BindNumber 包装
//	width    number           轨道宽（默认 260）
//	tone     强调色
//	label    string           顶部标题（可选，显示当前值）
//
// 交互用"分段命中条"：轨道上覆盖若干等宽透明区段，点击某段即把值落到该段，
// 不依赖点击坐标换算，跨后端稳定。视觉层由填充条 + 滑块给出。
func Slider(props gxscene.Props) *gxscene.Instance {
	value := gxscene.Get[float64](props, "value")
	onChange := gxscene.Get[gxscene.Handler](props, "onChange")
	tone := gxscene.Get[string](props, "tone")
	label := gxscene.Get[string](props, "label")
	min := numberGet(props, "min", 0)
	max := numberGet(props, "max", 100)
	step := propFloat(props, "step", 1)
	width := propFloat(props, "width", 260)

	lo, hi := min(), max()
	if hi <= lo {
		hi = lo + 1
	}
	if step <= 0 {
		step = 1
	}
	segments := int((hi-lo)/step + 0.5)
	if segments < 1 {
		segments = 1
	}
	if segments > 50 {
		segments = 50
	}
	segW := width / float64(segments)
	thumb := 16.0
	trackH := 8.0

	pct := func() float64 { return clamp((value()-lo)/(hi-lo), 0, 1) }
	curValue := func() float64 {
		v := lo + pct()*(hi-lo)
		return lo + float64(int((v-lo)/step+0.5))*step
	}

	// 命中段用 OpFor 渲染，key 用段号。
	hitFor := gxscene.NewFor(
		func() []any {
			out := make([]any, segments)
			for i := range out {
				out[i] = i
			}
			return out
		},
		func(v any) any { return v },
		func(v any) *gxscene.Instance {
			i := v.(int)
			target := clamp(lo+float64(i)*step, lo, hi)
			return sliderHitSegment(i, segW, trackH, target, onChange())
		},
	)

	const (
		fillWidthSlot = iota // 0
		thumbLeftSlot        // 1
		valueTextSlot        // 2
		hitForSlot           // 3
		fillBgSlot           // 4
		nSlots               // 5
	)
	ops := []gxscene.Op{
		el("div"), attr("gx-role", "slider"),
		attr("flex-direction", "column"), attr("gap", "8"), attr("width", fmtNum(width)),
	}
	if label() != "" {
		ops = append(ops,
			el("span"), attr("font-size", "13"), attr("color", ThemeMuted),
			dynChild(valueTextSlot), closeEl(),
		)
	}
	ops = append(ops,
		// 视口
		el("div"), attr("position", "relative"),
		attr("width", fmtNum(width)), attr("height", fmtNum(trackH+16)),
		// 轨道
		el("div"), attr("position", "absolute"), attr("top", fmtNum(8)),
		attr("width", fmtNum(width)), attr("height", fmtNum(trackH)),
		attr("border-radius", fmtNum(trackH/2)), attr("bg", ThemeInputBg), closeEl(),
		// 填充
		el("div"), attr("position", "absolute"), attr("top", fmtNum(8)),
		attr("height", fmtNum(trackH)), attr("border-radius", fmtNum(trackH/2)),
		dynAttr("bg", fillBgSlot), dynAttr("width", fillWidthSlot), closeEl(),
		// 滑块头
		el("div"), attr("position", "absolute"), attr("top", fmtNum(4)),
		attr("width", fmtNum(thumb)), attr("height", fmtNum(thumb)),
		attr("border-radius", fmtNum(thumb/2)), attr("bg", "#f8fafc"),
		attr("box-shadow", "0 1px 4px 0 rgba(0,0,0,0.5)"),
		attr("transition", "left 120ms linear"),
		dynAttr("left", thumbLeftSlot), closeEl(),
		// 命中段
		gxscene.Op{Code: gxscene.OpFor, Slot: hitForSlot},
		closeEl(),
	)

	return gxscene.Render(gxscene.Template{Ops: ops, N: nSlots},
		func() any { return fmtNum(pct() * width) },             // 0 fill width
		func() any { return fmtNum(pct() * (width - thumb)) },   // 1 thumb left
		func() any { return fmt.Sprintf("%s  %s", label(), fmtNum(curValue())) }, // 2 标题+值
		func() any { return hitFor },                            // 3 hit segments
		func() any { return toneColor(tone()) },                 // 4 fill bg
	)
}

func sliderHitSegment(i int, segW, trackH, target float64, onChange gxscene.Handler) *gxscene.Instance {
	left := float64(i) * segW
	ops := []gxscene.Op{
		el("div"), attr("gx-role", "slider-hit"),
		attr("position", "absolute"), attr("top", "0"),
		attr("left", fmtNum(left)), attr("width", fmtNum(segW)),
		attr("height", fmtNum(trackH+16)), attr("opacity", "0"),
		event("onClick", 0), closeEl(),
	}
	return gxscene.Render(gxscene.Template{Ops: ops, N: 1},
		func() any {
			return gxscene.Handler(func(ctx gxscene.EventContext) {
				emitNumber(onChange, ctx, target)
			})
		},
	)
}
