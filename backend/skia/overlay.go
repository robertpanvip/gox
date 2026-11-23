package skia

// 浮层（popover/dropdown/select/menu/tooltip）的协议约定。
//
// 设计目标：不改场景树节点类型、不改 GRX1/GRX2 二进制编码——所有约定都走
// Node.Props 的原生全量透传（host 只搬运属性，不解释语义），由后端识别并执行。
// 两个后端（Skia/Yoga 与 Rust/Taffy）对这几个属性采用完全一致的布局与绘制语义，
// 因此同一套 widgets 组件可跨后端渲染出相同的浮层行为。
//
// 属性：
//
//	gx-role       元素角色："popover"=浮层内容，"scrim"=点外部关闭的遮罩层
//	gx-anchor     浮层引用锚点的 Node.ID（引用带 gx-anchor-id 的节点）
//	gx-placement  定位方位：bottom / bottom-start / bottom-end / top / top-start / top-end
//	              留白优先；空间不足时后端自动翻转到反向（bottom<->top, start<->end）
//	gx-offset     浮层与锚点的间距（像素，缺省 6）
//
// 生命周期：浮层由组件用条件分支（OpBranch）动态挂载——open=false 时子树不存在，
// open=true 时整棵子树出现，天然走 GRX2 的 InsertBefore/RemoveChild 结构动作。
import (
	"strings"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

const (
	propGxRole     = "gx-role"
	propGxAnchor   = "gx-anchor"
	propGxAnchorID = "gx-anchor-id"
	propPlacement  = "gx-placement"
	propOffset     = "gx-offset"

	rolePopover = "popover"
	roleScrim   = "scrim"
)

// gxRole 返回节点的浮层角色（"" / "popover" / "scrim"）。
func gxRole(n *gxscene.Node) string {
	if n == nil || n.Props == nil {
		return ""
	}
	if v, ok := n.Props[propGxRole].(string); ok {
		return strings.ToLower(strings.TrimSpace(v))
	}
	return ""
}

// isOverlay 报告节点是否是需要脱离 flex 流、最后绘制的浮层/遮罩。
func isOverlay(n *gxscene.Node) bool {
	r := gxRole(n)
	return r == rolePopover || r == roleScrim
}

// isPopover / isScrim 细分角色。
func isPopover(n *gxscene.Node) bool { return gxRole(n) == rolePopover }
func isScrim(n *gxscene.Node) bool   { return gxRole(n) == roleScrim }

// collectAnchors 深度优先收集带 gx-anchor-id 的节点，按 id 索引。
// 锚点通常是 Popover 的 trigger 子树根。重复 id 后者覆盖（与命中测试一致）。
func collectAnchors(n *gxscene.Node, out map[string]*gxscene.Node) {
	if n == nil {
		return
	}
	if n.Props != nil {
		if id, ok := n.Props[propGxAnchorID].(string); ok && id != "" {
			out[id] = n
		}
	}
	for _, c := range n.Children {
		collectAnchors(c, out)
	}
}

// collectOverlays 深度优先收集所有浮层与遮罩，按绘制顺序（popover/scrim 在树中的先后）。
// 后端在主布局完成后遍历它们单独定位。
func collectOverlays(n *gxscene.Node, out *[]*gxscene.Node) {
	if n == nil {
		return
	}
	if isOverlay(n) {
		*out = append(*out, n)
		// 浮层子树内部仍可能有嵌套浮层，继续深入。
	}
	for _, c := range n.Children {
		collectOverlays(c, out)
	}
}

// placement 是浮层相对锚点的方位。
type placement struct {
	side  string // "bottom" / "top"
	align string // "start" / "center" / "end"
}

// parsePlacement 解析 gx-placement 字符串，如 "bottom-start" / "top" / "bottom-end"。
// 未知值回落到 bottom-start。
func parsePlacement(s string) placement {
	s = strings.ToLower(strings.TrimSpace(s))
	parts := strings.Split(s, "-")
	p := placement{side: "bottom", align: "start"}
	if len(parts) > 0 && (parts[0] == "top" || parts[0] == "bottom") {
		p.side = parts[0]
	}
	if len(parts) > 1 {
		switch parts[1] {
		case "center":
			p.align = "center"
		case "end":
			p.align = "end"
		default:
			p.align = "start"
		}
	}
	return p
}

// placeOverlay 计算浮层左上角绝对坐标。
// 入参：锚点矩形 a、浮层尺寸 pw/ph、方位 p、间距 offset、画布尺寸 availW/availH。
// 空间不足时按 side 翻转（bottom↔top）、按 align 翻转（start↔end），避免溢出画布。
func placeOverlay(a layoutBox, pw, ph float32, p placement, offset float32, availW, availH float32) (float32, float32) {
	var x, y float32

	// 主轴（垂直）：side 决定在锚点上方还是下方，空间不足则翻转。
	below := a.Y + a.H + offset + ph <= availH
	above := a.Y - offset - ph >= 0
	side := p.side
	if side == "bottom" && !below && above {
		side = "top"
	} else if side == "top" && !above && below {
		side = "bottom"
	}
	if side == "top" {
		y = a.Y - offset - ph
	} else {
		y = a.Y + a.H + offset
	}
	if y < 0 {
		y = 0
	}
	if y+ph > availH && ph <= availH {
		y = availH - ph
	}

	// 交叉轴（水平）：align 决定左对齐/居中/右对齐，溢出则翻转。
	startX := a.X
	endX := a.X + a.W - pw
	switch p.align {
	case "center":
		x = a.X + (a.W-pw)/2
	case "end":
		x = endX
	default: // start
		x = startX
	}
	// 水平溢出时反向对齐或贴边。
	if x < 0 {
		if p.align == "end" && startX >= 0 {
			x = startX // end 放不下就改 start
		} else {
			x = 0
		}
	} else if x+pw > availW {
		if p.align == "start" && endX >= 0 {
			x = endX // start 放不下就改 end
		} else {
			x = availW - pw
		}
	}
	return x, y
}
