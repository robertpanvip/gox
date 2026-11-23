// gx:nocollections
// 本文件为原生 Go 组件源码，经 gox build 转译为 tooltip.go。
// gx:nocollections 声明跳过集合/裸对象字面量 desugar，保留手写 opcode 的原生 Go 语义。
package widgets

// Tooltip 悬浮提示——Popover 的轻量变体。与 Popover 的差异：
//   - 触发方式是 hover（onMouseEnter / onMouseLeave），不是点击；
//   - 没有全屏 scrim 遮罩，不拦截点击（提示浮层须 pointer-events:none 语义，
//     由后端按 gx-role=tooltip 处理，见 STYLE.md）；
//   - 鼠标移出 trigger 即关闭，不提供"点外部关闭"。
//
// 事件驱动：锚点包装节点上绑定 onMouseEnter/onMouseLeave 两个 handler，分别
// 打开/关闭一个 PopoverState；浮层子树用 OpBranch 按 open 动态挂载，与 Popover
// 共用同一套 gx-anchor/gx-placement/gx-offset 定位协议，因此跨后端天然一致。
// 后端只需在命中 tooltip-anchor 子树时派发标准 mouseEnter/mouseLeave 事件即可。
//
// Props:
//   - state        *PopoverState（非受控；与 open/onOpenChange 二选一）
//   - open         func() bool（受控开关）
//   - onOpenChange func(bool)（受控变更回调）
//   - trigger      func() any → *Instance（hover 目标子树）
//   - content      func() any → *Instance（提示气泡子树）
//   - placement    string（默认 "top"）
//   - offset       string（像素，默认 "6"）
//   - width        string（可选）
import (
	"strconv"
	"sync/atomic"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

var tooltipSeq atomic.Uint64

// Tooltip 构造一个悬浮提示组件实例。
func Tooltip(props gxscene.Props) *gxscene.Instance {
	uid := "tooltip-" + strconv.FormatUint(tooltipSeq.Add(1), 10)

	stateGet := gxscene.Get[*PopoverState](props, "state")
	openGet := gxscene.Get[bool](props, "open")
	placement := gxscene.Get[string](props, "placement")
	offset := gxscene.Get[string](props, "offset")
	width := gxscene.Get[string](props, "width")
	triggerGet := gxscene.Get[any](props, "trigger")
	contentGet := gxscene.Get[any](props, "content")

	// 变更回调与开关查询：优先内部 state，否则走受控 open/onOpenChange。
	setOpen := func(v bool) {
		if st := stateGet(); st != nil {
			st.Set(v)
		}
		if cb, ok := props["onOpenChange"].(func(bool)); ok && cb != nil {
			cb(v)
		}
	}
	isOpen := func() bool {
		if st := stateGet(); st != nil {
			return st.IsOpen()
		}
		return openGet()
	}

	// 插槽：0=trigger(OpComp), 1=branch(OpBranch), 2=onMouseEnter, 3=onMouseLeave。
	const (
		triggerSlot = iota
		branchSlot
		enterSlot
		leaveSlot
		nSlots
	)
	ops := []gxscene.Op{
		el("div"),
		attr("gx-role", "tooltip-anchor"),
		attr("gx-anchor-id", uid),
		attr("flex-direction", "column"),
		// trigger 包装：hover 事件挂在这一层，命中子树冒泡至此或命中本节点均可。
		el("div"),
		gxscene.Op{Code: gxscene.OpEvent, Key: "onMouseEnter", Slot: enterSlot},
		gxscene.Op{Code: gxscene.OpEvent, Key: "onMouseLeave", Slot: leaveSlot},
		gxscene.Op{Code: gxscene.OpComp, Arg: "TooltipTrigger", Slot: triggerSlot},
		closeEl(),
		gxscene.Op{Code: gxscene.OpBranch, Slot: branchSlot},
		closeEl(),
	}

	buildChild := func(v any) any {
		if inst, ok := v.(*gxscene.Instance); ok && inst != nil {
			return inst
		}
		return nil
	}

	branch := gxscene.NewBranch(
		func() int {
			if isOpen() {
				return 1
			}
			return 0
		},
		// [0] 关闭：空子树
		func() *gxscene.Instance { return nil },
		// [1] 打开：提示气泡（无 scrim）
		func() *gxscene.Instance {
			p := placement()
			if p == "" {
				p = "top"
			}
			off := offset()
			if off == "" {
				off = "6"
			}
			floatOps := []gxscene.Op{
				el("div"),
				attr("gx-role", "tooltip"),
				attr("gx-anchor", uid),
				attr("gx-placement", p),
				attr("gx-offset", off),
				attr("bg", AppleMaterialThick),
				attr("border-radius", RadiusSmall),
				attr("border", AppleHairline),
				attr("border-width", "1"),
				attr("box-shadow", ShadowFloat),
				attr("padding", "6"),
				attr("flex-direction", "column"),
			}
			if w := width(); w != "" {
				floatOps = append(floatOps, attr("width", w))
			}
			floatOps = append(floatOps,
				gxscene.Op{Code: gxscene.OpElem, Arg: "div"},
				gxscene.Op{Code: gxscene.OpComp, Arg: "TooltipContent", Slot: 0},
				closeEl(),
				closeEl(),
			)
			return gxscene.Render(gxscene.Template{Ops: floatOps, N: 1},
				func() any { return buildChild(contentGet()) },
			)
		},
	)

	return gxscene.Render(gxscene.Template{Ops: ops, N: nSlots},
		func() any { return buildChild(triggerGet()) },
		func() any { return branch },
		func() any {
			return gxscene.Handler(func(gxscene.EventContext) { setOpen(true) })
		},
		func() any {
			return gxscene.Handler(func(gxscene.EventContext) { setOpen(false) })
		},
	)
}
