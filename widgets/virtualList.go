package widgets

import (
	"fmt"
	"strconv"
	"strings"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "px"), 64)
}

// VirtualList 虚拟滚动列表。核心性能约定：无论数据总量多大，场景树里只存在
// 当前视口可见的项（+ overscan 缓冲），节点数与数据量解耦——这才是"虚拟滚动"。
//
// 分工：
//   - 组件层（此处）：根据 scrollTop 计算可见索引区间，只渲染这些项；
//     用第一个「撑高 spacer」(height = first*itemHeight) 把可见项推到全局位置，
//     避免依赖 absolute——因为 For/组件包装是 display:contents，absolute 子节点
//     会失去定位参照（实测坐标塌成 0×0）。
//   - 后端：overflow 容器读 scroll-top，把子树坐标整体减去 scrollTop 并按容器
//     矩形裁剪（见 style.go / layout.go / paint.go 与 rust 端同构实现）。
//
// 叠加后：项屏幕 y = (first*itemHeight + 局部偏移) - scrollTop，落入视口才可见。
//
// Props:
//   - count           int 或 func() any→int，总项数
//   - item-height     number/string，单项高度（默认 40）
//   - viewport-height number/string，视口高度（默认 400）
//   - scroll-top      func() any → float64，当前滚动偏移（响应式 getter）
//   - overscan        视口上下额外渲染的项数（默认 3）
//   - render-item     func(int) *Instance，渲染第 index 项
//   - width / bg      容器样式
func VirtualList(props gxscene.Props) *gxscene.Instance {
	countGet := gxscene.Get[int](props, "count")
	scrollGet := gxscene.Get[float64](props, "scroll-top")

	itemHeight := propFloat(props, "item-height", 40)
	viewportHeight := propFloat(props, "viewport-height", 400)
	overscan := propInt(props, "overscan", 3)
	renderItemAny := gxscene.Get[any](props, "render-item")

	// 可见窗口 [first, last)。
	visibleIndices := func() []int {
		count := countGet()
		if count <= 0 {
			return nil
		}
		st := scrollGet()
		if st < 0 {
			st = 0
		}
		first := int(st/itemHeight) - overscan
		if first < 0 {
			first = 0
		}
		span := int(viewportHeight/itemHeight) + 2*overscan + 1
		last := first + span
		if last > count {
			last = count
		}
		out := make([]int, 0, last-first)
		for i := first; i < last; i++ {
			out = append(out, i)
		}
		return out
	}

	render := func(index int) *gxscene.Instance {
		var child *gxscene.Instance
		switch fn := renderItemAny().(type) {
		case func(int) *gxscene.Instance:
			child = fn(index)
		case func(int) any:
			if inst, ok := fn(index).(*gxscene.Instance); ok {
				child = inst
			}
		}
		if child == nil {
			child = gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
				el("div"), attr("width", "100"), attr("height", fmt.Sprintf("%g", itemHeight)), closeEl(),
			}})
		}
		return child
	}

	forCtl := gxscene.NewFor(
		func() []any {
			idx := visibleIndices()
			out := make([]any, len(idx))
			for i, v := range idx {
				out[i] = v
			}
			return out
		},
		func(v any) any { return v }, // index 作 key
		func(v any) *gxscene.Instance { return render(v.(int)) },
	)

	// 外层：固定视口高度 + overflow:scroll；scroll-top 动态绑定。
	// 内层 spacer（动态高度 = firstIdx*itemHeight）把可见项推到全局位置。
	ops := []gxscene.Op{
		el("div"),
		attr("height", fmt.Sprintf("%g", viewportHeight)),
		attr("overflow", "scroll"),
	}
	if v, ok := propStringVal(props, "width"); ok {
		ops = append(ops, attr("width", v))
	}
	if v, ok := propStringVal(props, "bg"); ok {
		ops = append(ops, attr("bg", v))
	}
	ops = append(ops, gxscene.Op{Code: gxscene.OpDynAttr, Key: "scroll-top", Slot: 0})
	// spacer：高度由 firstIdx 决定（在 For 的 each 求值后得到）。
	ops = append(ops, gxscene.Op{Code: gxscene.OpElem, Arg: "div"})
	ops = append(ops, gxscene.Op{Code: gxscene.OpDynAttr, Key: "height", Slot: 1})
	ops = append(ops, closeEl())
	// 可见项 For。
	ops = append(ops, gxscene.Op{Code: gxscene.OpFor, Slot: 2}, closeEl())

	return gxscene.Render(gxscene.Template{Ops: ops, N: 3},
		func() any { return fmt.Sprintf("%g", scrollGet()) }, // slot 0: scroll-top
		func() any { // slot 1: spacer height——独立从 scrollTop 计算，不依赖 For 求值顺序
			st := scrollGet()
			if st < 0 {
				st = 0
			}
			first := int(st/itemHeight) - overscan
			if first < 0 {
				first = 0
			}
			return fmt.Sprintf("%g", float64(first)*itemHeight)
		},
		func() any { return forCtl }, // slot 2: *For
	)
}

func propFloat(p gxscene.Props, key string, def float64) float64 {
	if v, ok := p[key]; ok {
		switch x := v.(type) {
		case float64:
			return x
		case int:
			return float64(x)
		case string:
			if f, err := parseFloat(x); err == nil {
				return f
			}
		}
	}
	return def
}

func propInt(p gxscene.Props, key string, def int) int {
	if v, ok := p[key]; ok {
		switch x := v.(type) {
		case int:
			return x
		case float64:
			return int(x)
		}
	}
	return def
}

func propStringVal(p gxscene.Props, key string) (string, bool) {
	if v, ok := p[key]; ok {
		if s, ok := v.(string); ok {
			return s, true
		}
	}
	return "", false
}
