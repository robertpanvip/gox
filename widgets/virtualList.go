// gx:nocollections
// 本文件为原生 Go 组件源码，经 gox build 转译为 virtualList.go。
// gx:nocollections 声明跳过集合/裸对象字面量 desugar，保留手写 opcode 的原生 Go 语义。
package widgets

import (
	"fmt"
	"strconv"
	"strings"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "px"), 64)
}

// VirtualList 虚拟滚动列表。核心性能约定：无论数据总量多大，场景树里只存在
// 当前视口可见的项（+ overscan 缓冲），节点数与数据量解耦——这才是"虚拟滚动"。
//
// 分工：
//   - 组件层（此处）：根据 scrollTop 计算可见索引区间，只渲染这些项；
//     用第一个「撑高 spacer」把可见项推到全局位置。
//   - 后端：overflow 容器读 scroll-top，把子树坐标整体减去 scrollTop 并按容器
//     矩形裁剪（见 style.go / layout.go / paint.go 与 rust 端同构实现）。
//
// 高度来源（两种模式，自动选择）：
//   - 显式：传 height(i)。精确、零回流，适合高度可提前算出的场景。
//   - 自动测量：不传 height，由框架在布局后把每项真实高度回填。适合高度依赖内容
//     （多行文本/图片）调用方无法预知的场景。回填走 scene 回流总线（见 measure.go），
//     并用 VIRTUA 风格锚定补偿消除跳动，见下方「自动测量与锚定」。
//
// Props:
//   - count            int 或 func() any→int，总项数
//   - item-height      number/string，等高档的单项高度（默认 40）
//   - viewport-height  number/string，视口高度（默认 400）
//   - scroll-top       func() any → float64，当前滚动偏移（响应式 getter）
//   - overscan         视口上下额外渲染的项数（默认 3）
//   - render-item      func(int) *Instance，渲染第 index 项
//   - height           可选 func(int) float64 / func(int) any —— 第 i 项真实高度。
//                      提供后启用显式模式，关闭自动测量。
//   - estimate-height  可选 number，自动测量模式下高度未知项的估算高度（默认 item-height）
//   - on-scroll        可选 func(float64)：锚定补偿写回 scrollTop 的回调（见下）
//   - width / bg       容器样式
//
// 自动测量与锚定（对齐 virtua/src/core/store.ts 的 ACTION_ITEM_RESIZE 流程）：
//
//	anchor := 当前视口顶部项
//	oldOff := offsets[anchor]          // 回填前
//	measured[index] = 真实高度          // → offsets 重算
//	jump += offsets[anchor] - oldOff   // 视口上方项变高 → 补偿，视觉不动
//
// 只有落在锚点**之前**的项变高才会改变 offsets[anchor]，从而产生 jump——这正是
// virtua 的 shouldKeep 判定用 offset 差自动表达的等价形式。jump 通过 on-scroll
// 写回外部 scrollTop 并清零（virtua 的 _flushJump），避免回顶时残留偏移。
// 未提供 on-scroll 时退化为内部累积，回顶可能有少量偏差——文档已如实说明。
func VirtualList(props gxscene.Props) *gxscene.Instance {
	countGet := gxscene.Get[int](props, "count")
	scrollGet := gxscene.Get[float64](props, "scroll-top")

	itemHeight := propFloat(props, "item-height", 40)
	viewportHeight := propFloat(props, "viewport-height", 400)
	overscan := propInt(props, "overscan", 3)
	renderItemAny := gxscene.Get[any](props, "render-item")

	// 高度来源：
	//   - "height"           func(int) float64 / func(int) any —— 显式高度，关闭自动测量
	//   - "estimate-height"  自动测量模式下未知项的估算高度（默认 item-height）
	heightAny := gxscene.Get[any](props, "height")
	estimate := propFloat(props, "estimate-height", itemHeight)
	explicitHeight := heightAny != nil && heightAny() != nil

	// on-scroll：把锚定补偿后的 scrollTop 写回外部（受控滚动推荐提供）。
	onScrollAny := gxscene.Get[any](props, "on-scroll")

	// ---- 自动测量状态（仅 !explicitHeight 时使用）----
	// measured[i] > 0 表示第 i 项已被后端实测；未测量项用 estimate。
	measured := make(map[int]float64)
	// itemRootID → index：把后端回流的节点 ID 映射回列表索引。
	itemRootID := make(map[gxscene.NodeID]int)
	// measureSeq 是回填版本号：每次回填 +1，作为信号驱动窗口重算（融入 app 帧边界）。
	measureSeq := signal.New(0.0)
	// jump 是 VIRTUA 风格锚定补偿，累积后经 on-scroll flush 回外部并清零。
	var jump float64

	// itemHeightAt 返回第 i 项当前采用的高度。
	itemHeightAt := func(i int) float64 {
		if explicitHeight {
			switch fn := heightAny().(type) {
			case func(int) float64:
				if h := fn(i); h > 0 {
					return h
				}
			case func(int) any:
				switch h := fn(i).(type) {
				case float64:
					if h > 0 {
						return h
					}
				case int:
					if h > 0 {
						return float64(h)
					}
				}
			}
			return estimate
		}
		if h, ok := measured[i]; ok && h > 0 {
			return h
		}
		return estimate
	}

	// offsets[i] = 第 0..i-1 项累计高度；offsets[n] = 总高度。
	// 显式模式读 height(i)；自动测量模式读 measured[i]（未测为 estimate）。
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

	// effectiveScroll = 外部 scrollTop + 未 flush 的锚定补偿。
	effectiveScroll := func() float64 {
		st := scrollGet() + jump
		if st < 0 {
			st = 0
		}
		return st
	}

	// flushJump 把累积补偿写回外部 scrollTop（virtua _flushJump），随后清零。
	flushJump := func() {
		if jump == 0 || onScrollAny == nil || onScrollAny() == nil {
			return
		}
		target := effectiveScroll()
		switch fn := onScrollAny().(type) {
		case func(float64):
			fn(target)
		case func(any):
			fn(target)
		}
		jump = 0
	}

	// onItemMeasured 是单个列表项的回流回调：写入实测高度 + VIRTUA 锚定补偿。
	onItemMeasured := func(index int, m gxscene.Measured) {
		if m.H <= 0 {
			return
		}
		oldH := itemHeightAt(index)
		if mabs(m.H-oldH) < measureSnap { // 与总线去重呼应，二次防抖
			return
		}
		n := countGet()
		if n <= 0 || index >= n {
			return
		}

		// 回填前的锚点偏移。锚点取当前视口顶部项（effectiveScroll 所在项）。
		before := buildOffsets(n)
		anchor := firstIndexAt(before, effectiveScroll())
		if anchor >= n {
			anchor = n - 1
		}
		oldAnchorOff := before[anchor]

		// 写入实测高度 → offsets 重算。
		measured[index] = m.H
		after := buildOffsets(n)

		// 只有锚点上方的项变高才会改变 offsets[anchor]；offset 差天然表达 shouldKeep。
		jump += after[anchor] - oldAnchorOff
		flushJump()

		// 触发窗口重算（登记了 measureSeq 依赖的 effect 会在帧边界重绘画）。
		measureSeq(measureSeq() + 1)
	}

	// ---- 可见窗口 ----
	windowAt := func() (first, last int, spacer float64) {
		count := countGet()
		if count <= 0 {
			return 0, 0, 0
		}
		st := effectiveScroll()
		off := buildOffsets(count)
		first = firstIndexAt(off, st) - overscan
		if first < 0 {
			first = 0
		}
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

	visibleIndices := func() []int {
		// 登记对回填版本的依赖：measureSeq 变化 → 本 getter 所在的 effect 重跑。
		_ = measureSeq()
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

		// 自动测量：订阅该项根节点的回流。For 在 per-item Owner 下调 render，
		// signal.OnCleanup 保证项滚出窗口销毁时退订 + 清理映射，防止注册表膨胀。
		if !explicitHeight && child.Root() != nil {
			root := child.Root()
			id := root.ID
			if id != gxscene.NoNode {
				itemRootID[id] = index
				unsub := gxscene.SubscribeMeasured(id, func(m gxscene.Measured) {
					onItemMeasured(index, m)
				})
				signal.OnCleanup(func() {
					unsub()
					delete(itemRootID, id)
					gxscene.ForgetMeasured(id)
				})
			}
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

	// 外层：固定视口高度 + overflow:scroll；scroll-top 用 effectiveScroll（含锚定补偿）。
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
	// spacer：高度 = 可见首项之前的累计高度（自动测量下随实测收敛）。
	ops = append(ops, gxscene.Op{Code: gxscene.OpElem, Arg: "div"})
	ops = append(ops, gxscene.Op{Code: gxscene.OpDynAttr, Key: "height", Slot: 1})
	ops = append(ops, closeEl())
	ops = append(ops, gxscene.Op{Code: gxscene.OpFor, Slot: 2}, closeEl())

	return gxscene.Render(gxscene.Template{Ops: ops, N: 3},
		func() any { return fmt.Sprintf("%g", effectiveScroll()) }, // slot 0: scroll-top（含 jump）
		func() any { // slot 1: spacer height
			_, _, spacer := windowAt()
			return fmt.Sprintf("%g", spacer)
		},
		func() any { return forCtl }, // slot 2: *For
	)
}

// measureSnap 与 scene.measureEpsilon 对齐：小于此高度的回填变化视为无变化。
const measureSnap = 0.5

func mabs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
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
