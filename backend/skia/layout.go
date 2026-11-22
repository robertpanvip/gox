package skia

import (
	yoga "github.com/ahati/yoga-go-binding"
	sk "github.com/pekim/go-skia"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// 本文件用 Facebook Yoga 3.x（github.com/ahati/yoga-go-binding，C++20
// libyogacore.a 的 Go 绑定）为场景树做布局。相比旧的 2017 版 kjk/flex，
// 新版支持完整 Flexbox 属性：原生 gap、flex-wrap、align-content、百分比尺寸、
// min/max、aspect-ratio 等，无需用 margin 模拟 gap。
//
// 设计边界：布局是纯粹的"表现层"职责，完全留在后端内部；host 只把 JSX 属性
// 原样塞进 Node.Props，核心运行时（runtime/）对 Yoga 零感知。这让我们可以在
// 不碰核心的前提下，把纵向流式布局替换成完整的 Flexbox 引擎。

// layoutBox 是一个节点经 Yoga 计算出的绝对矩形（相对画布左上角，左上角原点）。
type layoutBox struct {
	X, Y, W, H float32
}

// layoutCache 把一次布局的结果按节点缓存，供 painter 绘制时 O(1) 查询。
type layoutCache map[*gxscene.Node]layoutBox

// fontMeasurer 抽象文本测宽，便于测试注入；生产实现走 Skia Font.MeasureText。
type fontMeasurer func(text string, fontSize int, bold bool) (width float32)

// skiaMeasureFunc 返回绑定到指定文本/字体的 Yoga measure 回调。
// Yoga 在 measure 时可能传入可用宽度（AtMost/Exactly），这里文本不换行，直接返回固有尺寸。
func skiaMeasureFunc(text string, fontSize int, bold bool, measure fontMeasurer) yoga.MeasureFunc {
	w := measure(text, fontSize, bold)
	h := float32(fontSize) * 1.3
	return func(node *yoga.Node, width float32, widthMode yoga.MeasureMode,
		height float32, heightMode yoga.MeasureMode) yoga.Size {
		return yoga.Size{Width: w, Height: h}
	}
}

// yogaDirection 把后端的 flex-direction 语义映射到 Yoga 枚举。
// 默认（含未识别值）回落到 column，兼容历史纵向布局。
func yogaDirection(d flexDirection) yoga.FlexDirection {
	switch d {
	case directionRow:
		return yoga.FlexDirectionRow
	case directionRowReverse:
		return yoga.FlexDirectionRowReverse
	case directionColumnReverse:
		return yoga.FlexDirectionColumnReverse
	default:
		return yoga.FlexDirectionColumn
	}
}

func yogaJustify(j justifyContent) yoga.Justify {
	switch j {
	case justifyCenter:
		return yoga.JustifyCenter
	case justifyEnd:
		return yoga.JustifyFlexEnd
	case justifySpaceBetween:
		return yoga.JustifySpaceBetween
	case justifySpaceAround:
		return yoga.JustifySpaceAround
	case justifySpaceEvenly:
		return yoga.JustifySpaceEvenly
	default:
		return yoga.JustifyFlexStart
	}
}

func yogaAlign(a alignItems) yoga.Align {
	switch a {
	case alignFlexStart:
		return yoga.AlignFlexStart
	case alignFlexEnd:
		return yoga.AlignFlexEnd
	case alignCenter:
		return yoga.AlignCenter
	case alignBaseline:
		return yoga.AlignBaseline
	default:
		return yoga.AlignStretch
	}
}

func yogaWrap(w flexWrap) yoga.Wrap {
	switch w {
	case wrapWrap:
		return yoga.WrapWrap
	case wrapReverse:
		return yoga.WrapWrapReverse
	default:
		return yoga.WrapNoWrap
	}
}

func yogaAlignContent(a alignContent) yoga.Align {
	switch a {
	case contentFlexStart:
		return yoga.AlignFlexStart
	case contentFlexEnd:
		return yoga.AlignFlexEnd
	case contentCenter:
		return yoga.AlignCenter
	case contentSpaceBetween:
		return yoga.AlignSpaceBetween
	case contentSpaceAround:
		return yoga.AlignSpaceAround
	default:
		return yoga.AlignStretch
	}
}

// nodePair 把一个 gxscene 节点与其映射出的 Yoga 节点配对，供布局后回填矩形。
type nodePair struct {
	scene *gxscene.Node
	yoga  *yoga.Node
}

// buildYogaTree 递归把场景树映射成 Yoga 节点树，并返回所有 (gxscene, yoga) 配对。
// inherited 是祖先链继承来的文本样式；文本 measure 必须用与绘制相同的字号/字重，
// 否则布局算出的尺寸与实际绘制不一致，文字会溢出或错位。
func buildYogaTree(n *gxscene.Node, measure fontMeasurer, inherited textStyle) (*yoga.Node, []nodePair) {
	if n == nil {
		return nil, nil
	}

	yn := yoga.NewNode()
	pairs := []nodePair{{scene: n, yoga: yn}}
	eff := inheritedStyle(n, inherited)

	switch n.Type {
	case gxscene.NodeText:
		yn.SetMeasureFunc(skiaMeasureFunc(n.Text, eff.fontSize, eff.bold, measure))
		return yn, pairs
	case gxscene.NodeFragment:
		// 透明包装（#root/#frag/组件槽）：用 CSS display:contents 语义——自身不产生
		// 布局盒子，children 直接参与父容器的 flex 排版。否则多层组件包装会各自
		// 形成 column 盒子，破坏 row 容器的横排（预制控件库里组件套组件尤其明显）。
		yn.SetDisplay(yoga.DisplayContents)
		for i, c := range n.Children {
			yc, sub := buildYogaTree(c, measure, eff)
			if yc == nil {
				continue
			}
			yn.InsertChild(yc, i)
			pairs = append(pairs, sub...)
		}
		return yn, pairs
	default:
		// 元素容器：按样式设置 flex 属性。
		s := parseStyle(n.Props)
		yn.SetFlexDirection(yogaDirection(s.direction))
		yn.SetJustifyContent(yogaJustify(s.justify))
		yn.SetAlignItems(yogaAlign(s.align))
		yn.SetFlexWrap(yogaWrap(s.wrap))
		yn.SetAlignContent(yogaAlignContent(s.alignContent))

		if s.width > 0 {
			yn.SetWidth(s.width)
		}
		if s.height > 0 {
			yn.SetHeight(s.height)
		}
		if s.padding > 0 {
			yn.SetPadding(yoga.EdgeAll, s.padding)
		}
		// 原生 gap（新版 Yoga 3.x）：row-gap / column-gap / 统一 gap 分别映射。
		if s.gap > 0 {
			yn.SetGap(yoga.GutterAll, s.gap)
		}
		if s.rowGap > 0 {
			yn.SetGap(yoga.GutterRow, s.rowGap)
		}
		if s.columnGap > 0 {
			yn.SetGap(yoga.GutterColumn, s.columnGap)
		}

		for i, c := range n.Children {
			yc, sub := buildYogaTree(c, measure, eff)
			if yc == nil {
				continue
			}
			yn.InsertChild(yc, i)
			pairs = append(pairs, sub...)
		}
		return yn, pairs
	}
}

// computeLayout 对整棵场景树跑一次 Yoga 布局，返回按 gxscene 节点索引的矩形缓存。
// availW/availH 是画布可用空间（左上角原点坐标系）。
func computeLayout(root *gxscene.Node, availW, availH float32, measure fontMeasurer) layoutCache {
	cache := make(layoutCache)
	if root == nil {
		return cache
	}
	yn, _ := buildYogaTree(root, measure, defaultTextStyle())
	if yn == nil {
		return cache
	}
	// 根节点未显式指定尺寸时撑满画布宽度：保留"根 div 背景铺满窗口"的历史行为。
	// 高度不强制撑满，让内容按自然高度排布（与历史一致）。
	s := parseStyle(root.Props)
	if s.width <= 0 {
		yn.SetWidth(availW)
	}
	yn.CalculateLayout(availW, availH, yoga.DirectionLTR)

	// Yoga 的 LayoutLeft/Top 是相对父节点的坐标；painter 需要画布绝对坐标。
	// 递归累加父偏移，把相对坐标换算成绝对坐标后回填缓存。
	var walk func(scene *gxscene.Node, yn *yoga.Node, ox, oy float32)
	walk = func(scene *gxscene.Node, yn *yoga.Node, ox, oy float32) {
		if scene == nil || yn == nil {
			return
		}
		ax := ox + yn.LayoutLeft()
		ay := oy + yn.LayoutTop()
		cache[scene] = layoutBox{
			X: ax, Y: ay,
			W: yn.LayoutWidth(),
			H: yn.LayoutHeight(),
		}
		// 文本节点在 buildYogaTree 里没有建 yoga 子节点，跳过其 children。
		if scene.Type == gxscene.NodeText {
			return
		}
		for i, c := range scene.Children {
			walk(c, yn.ChildAt(i), ax, ay)
		}
	}
	walk(root, yn, 0, 0)
	return cache
}

// skiaFontMeasurer 用真实 Skia 字体测量文本宽度（生产用的 fontMeasurer 实现）。
// 字体由 font.go 的 resolveFont/splitRuns 按字符解析（含 CJK fallback），
// 与绘制端共用同一字体决策，避免测量宽度与落笔宽度不一致。
func skiaFontMeasurer() fontMeasurer {
	return func(text string, fontSize int, bold bool) float32 {
		if text == "" {
			return 0
		}
		// 与绘制端同口径：按字体分段累加宽度。中文走 CJK fallback 字体，
		// 其字宽与主字体不同，若这里仍用主字体测量，Yoga 给的文本框会偏窄、文字溢出。
		var total float32
		for _, r := range splitRuns(text, fontSize, bold) {
			if r.text == "" || r.font.IsNil() {
				continue
			}
			total += r.font.MeasureText([]byte(r.text), uint32(len(r.text)), sk.TextEncodingUTF8, nil)
		}
		return total
	}
}
