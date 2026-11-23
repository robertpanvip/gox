// gx:nocollections
// 本文件为原生 Go 组件源码，经 gox build 转译为 virtualList.go。
// gx:nocollections 声明跳过集合/裸对象字面量 desugar，保留手写 opcode 的原生 Go 语义。
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
//   - count            int 或 func() any→int，总项数
//   - item-height      number/string，等高档的单项高度（默认 40）
//   - viewport-height  number/string，视口高度（默认 400）
//   - scroll-top       func() any → float64，当前滚动偏移（响应式 getter）
//   - overscan         视口上下额外渲染的项数（默认 3）
//   - render-item      func(int) *Instance，渲染第 index 项
//   - height           可选 func(int) float64 / func(int) any —— 第 i 项真实高度。
//                      提供后启用不定高模式：内部维护前缀和 offsets 并用二分查找
//                      可见区间，spacer 高度 = 首项之前的累计高度。
//   - estimate-height  可选 number，height(i) 未知时的估算高度（默认 item-height）
//   - width / bg       容器样式
//
// 不定高模式：scrollTop → item 的映射由前缀和 + 二分查找完成，替代等高的除法。
// 高度来源当前为显式 height(i)（精确、无跳动）；后端测量回填的钩子见 x-measure
// 预留（后续版本接入），届时可在不传 height 的情况下由后端实测自动修正。
func VirtualList(props gxscene.Props) *gxscene.Instance {
	countGet := gxscene.Get[int](props, "count")
	scrollGet := gxscene.Get[float64](props, "scroll-top")

	itemHeight := propFloat(props, "item-height", 40)
	viewportHeight := propFloat(props, "viewport-height", 400)
	overscan := propInt(props, "overscan", 3)
	renderItemAny := gxscene.Get[any](props, "render-item")

	// 不定高支持：
	//   - "height"        func(int) float64 / func(int) any —— 返回第 i 项真实高度
	//   - "estimate-height" 未知项的估算高度（默认沿用 item-height）
	// 未提供 "height" 时退化为原来的等高等距路径（零额外开销、完全兼容）。
	heightAny := gxscene.Get[any](props, "height")
	estimate := propFloat(props, "estimate-height", itemHeight)
	variableHeight := heightAny != nil && heightAny() != nil

	// itemHeightAt 返回第 i 项高度：优先用显式 height(i)，否则固定 itemHeight。
	itemHeightAt := func(i int) float64 {
		if !variableHeight {
			return itemHeight
		}
		switch fn := heightAny().(type) {
		case func(int) float64:
			if h := fn(i); h > 0 {
				return h
			}
		case func(int) any:
			switch h := fn(i).(type) {
			case float64:
				if h > 0 { return h }
			case int:
				if h > 0 { return float64(h) }
			}
		}
		return estimate
	}

	// offsets[i] = 第 0..i-1 项累计高度；offsets[n] = 总高度。
	// 每次求值重算（count 通常几千，O(n) 可忽略），保证 height(i) 依赖的数据变化时即时生效。
	buildOffsets := func(n int) []float64 {
		off := make([]float64, n+1)
		for i := 0; i < n; i++ {
			off[i+1] = off[i] + itemHeightAt(i)
		}
		return off
	}

	// firstIndexAt 用二分查找 scrollTop 落在哪一项（offsets[k] <= st < offsets[k+1]）。
	firstIndexAt := func(off []float64, st float64) int {
		lo, hi := 0, len(off)-1
		for lo < hi {
			mid := int(uint(lo+hi) >> 1)
			if off[mid+1] <= st {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		return lo
	}

	// windowAt 返回可见区间 [first,last) 与首项之前的累计高度 spacer。
	windowAt := func() (first, last int, spacer float64) {
		count := countGet()
		if count <= 0 {
			return 0, 0, 0
		}
		st := scrollGet()
		if st < 0 {
			st = 0
		}
		off := buildOffsets(count)
		first = firstIndexAt(off, st) - overscan
		if first < 0 {
			first = 0
		}
		// last：第一个 offsets[k] >= st+viewport 的 k，再加 overscan。
		bottom := st + viewportHeight
		last = firstIndexAt(off, bottom) + 1 + overscan
		if last > count {
			last = count
		}
		if last < first {
			last = first
		}
		spacer = off[first]
		return
	}

	// 可见窗口 [first, last)。
	visibleIndices := func() []int {
		first, last, _ := windowAt()
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
		func() any { // slot 1: spacer height = 可见首项之前的累计高度（不等高时为前缀和）
			_, _, spacer := windowAt()
			return fmt.Sprintf("%g", spacer)
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
