package widgets

import Signal "github.com/robertpanvip/gox/runtime/signal"

// Modal / Portal —— 基于协议级浮层的模态对话框与传送门。
//
// 与 Popover（贴锚点的轻量浮层）不同：
//
//	Modal  视口居中的对话框，带遮罩、标题栏、内容区、底部操作区；常用于确认、
//	       表单、详情。可配置点遮罩关闭、显示关闭按钮、ESC 关闭。
//	Portal 更基础的"传送"原语：把子树渲染到浮层层，脱离父级 overflow 裁剪与
//	       flex 流、最后绘制在最上层；子树用普通 position:absolute + inset
//	       自主定位。Modal / Toast / 全局提示都可建立在它之上。
//
// 二者与 Popover 共用浮层协议（见 backend/skia/overlay.go）：
// gx-role=modal/portal/scrim，脱离主树最后绘制，浮层优先命中。
// 生命周期用 OpBranch 动态挂载——closed 时子树不存在，open 时整棵出现。
//
// 场景树结构（Modal）：
//
//	#frag (Modal)
//	└── #frag (OpBranch, open?1:0)
//	    ├── [1] div[gx-role=scrim, onClick=maybeClose]
//	    │       div[gx-role=modal, 居中, 材质/阴影]
//	    │       └── 标题栏 + 内容 + 底部操作
//	    └── [0] (空)
//
// 用法（非受控，内部 state）：
//
//	st := widgets.NewModalState(false)
//	widgets.Modal(Props{
//	    "state": st,
//	    "title": func() any { return "删除确认" },
//	    "content": func() any { return Text(...) },
//	    "actions": func() any { return []any{cancelBtn, okBtn} },
//	})
//	st.Open() // 业务在别处打开
//
// ESC 关闭：GUI 层在键盘回调里调用 Modal.HandleKey(state, key)；
// 组件不直接监听键盘，保持 host / 后端零侵入（与 Input 的 InputChar 对称）。
import (
	"strconv"
	"sync/atomic"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

var modalSeq atomic.Uint64

// ModalState 是 Modal 的开关状态，内部持有 bool sig 字段。
type ModalState struct {
	open Signal.Signal[bool]
}

func NewModalState(initial bool) *ModalState {
	var open = Signal.New(initial)
	return &ModalState{open: open}
}

func (s *ModalState) Open()        { s.open(true) }
func (s *ModalState) Close()       { s.open(false) }
func (s *ModalState) Toggle()      { s.open(!s.open()) }
func (s *ModalState) Set(v bool)   { s.open(v) }
func (s *ModalState) IsOpen() bool { return s.open() }
func (s *ModalState) Getter() func() bool {
	return func() bool { return s.open() }
}

// ModalHandleKey 供 GUI 层在键盘回调里调用：ESC/Escape 关闭模态框。
// 返回是否消费了该按键（true 表示模态框当前打开且被 ESC 关闭）。
func ModalHandleKey(s *ModalState, key string) bool {
	if s == nil || !s.IsOpen() {
		return false
	}
	switch key {
	case "Escape", "Esc", "escape", "esc":
		s.Close()
		return true
	}
	return false
}

// Modal 构造 Apple 风格的模态对话框。Props:
//
//	state          *ModalState（非受控；与 open/onOpenChange 二选一）
//	open           func() bool（受控开关）
//	onOpenChange   func(bool)（受控变更回调）
//	title          string / func() any（标题，默认空）
//	content        func() any → *Instance（内容子树）
//	actions        func() any → []any/*Instance（底部操作按钮，默认无底部区）
//	width          string（面板宽度，默认 "420"）
//	closable       bool（显示右上角关闭按钮，默认 true）
//	maskClosable   bool（点遮罩关闭，默认 false——Apple 模态通常需显式操作）
//	scrim          string（遮罩色，默认 ThemeScrim）
func Modal(props gxscene.Props) *gxscene.Instance {
	// 唯一 id 写入根 attr：既便于外部引用，也让模板编译缓存按实例区分骨架，
	// 避免同结构模板（如 Portal）共用 slot 骨架导致的动态插槽绑定错位。
	uid := modalSeqName("modal")
	stateGet := gxscene.Get[*ModalState](props, "state")
	openGet := boolGet(props, "open", false)
	title := gxscene.Get[string](props, "title")
	contentGet := gxscene.Get[any](props, "content")
	actionsGet := gxscene.Get[any](props, "actions")
	width := gxscene.Get[string](props, "width")
	scrimColor := gxscene.Get[string](props, "scrim")

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

	w := width()
	if w == "" {
		w = "420"
	}
	scrim := scrimColor()
	if scrim == "" {
		scrim = ThemeScrim
	}
	closableGet := boolGet(props, "closable", true)
	maskCloseGet := boolGet(props, "maskClosable", false)
	hasTitle := title() != ""

	// builder 内插槽：0=scrim onClick, 1=close 按钮 onClick, 2=title,
	// 3=content, 4=actions, 5=close 按钮 opacity（closable 控制）
	const (
		scrimClickSlot = iota
		closeClickSlot
		titleSlot
		contentSlot
		actionsSlot
		closeOpacitySlot
		nSlots
	)

	branch := gxscene.NewBranch(
		func() int {
			if isOpen() {
				return 1
			}
			return 0
		},
		func() *gxscene.Instance { return nil }, // 关闭：空
		func() *gxscene.Instance {
			// 模板结构恒定（与 props 无关）：始终生成 scrim + panel + 标题栏
			// + 关闭按钮 + 内容区 + 操作区。有无由 getter 返回空值控制显隐。
			// 这样 N 与 Op 严格一一对应，避免动态模板骨架导致的插槽错位。
			ops := []gxscene.Op{
				// 遮罩
				gxscene.Op{Code: gxscene.OpElem, Arg: "div"},
				gxscene.Op{Code: gxscene.OpAttr, Key: "gx-role", Arg: "scrim"},
				gxscene.Op{Code: gxscene.OpAttr, Key: "bg", Arg: scrim},
				gxscene.Op{Code: gxscene.OpEvent, Key: "onClick", Slot: scrimClickSlot},
				gxscene.Op{Code: gxscene.OpClose},
				// 模态面板（协议层负责居中）
				gxscene.Op{Code: gxscene.OpElem, Arg: "div"},
				gxscene.Op{Code: gxscene.OpAttr, Key: "gx-role", Arg: "modal"},
				gxscene.Op{Code: gxscene.OpAttr, Key: "width", Arg: w},
				gxscene.Op{Code: gxscene.OpAttr, Key: "bg", Arg: ThemeMaterial},
				gxscene.Op{Code: gxscene.OpAttr, Key: "border-radius", Arg: RadiusModal},
				gxscene.Op{Code: gxscene.OpAttr, Key: "border", Arg: ThemeHairline},
				gxscene.Op{Code: gxscene.OpAttr, Key: "border-width", Arg: HairlineWidth},
				gxscene.Op{Code: gxscene.OpAttr, Key: "box-shadow", Arg: ShadowModal},
				gxscene.Op{Code: gxscene.OpAttr, Key: "padding", Arg: Space6},
				gxscene.Op{Code: gxscene.OpAttr, Key: "flex-direction", Arg: "column"},
				gxscene.Op{Code: gxscene.OpAttr, Key: "gap", Arg: Space4},
				// 标题栏（row：标题 + 关闭按钮）
				gxscene.Op{Code: gxscene.OpElem, Arg: "div"},
				gxscene.Op{Code: gxscene.OpAttr, Key: "flex-direction", Arg: "row"},
				gxscene.Op{Code: gxscene.OpAttr, Key: "align-items", Arg: "center"},
				gxscene.Op{Code: gxscene.OpAttr, Key: "justify-content", Arg: "space-between"},
				gxscene.Op{Code: gxscene.OpElem, Arg: "span"},
				gxscene.Op{Code: gxscene.OpAttr, Key: "font-size", Arg: FontTitle2},
				gxscene.Op{Code: gxscene.OpAttr, Key: "weight", Arg: "bold"},
				gxscene.Op{Code: gxscene.OpAttr, Key: "color", Arg: ThemeText},
				gxscene.Op{Code: gxscene.OpDynChild, Slot: titleSlot},
				gxscene.Op{Code: gxscene.OpClose},
				// 关闭按钮（×）
				gxscene.Op{Code: gxscene.OpElem, Arg: "button"},
				gxscene.Op{Code: gxscene.OpAttr, Key: "gx-role", Arg: "modal-close"},
				gxscene.Op{Code: gxscene.OpAttr, Key: "width", Arg: "26"},
				gxscene.Op{Code: gxscene.OpAttr, Key: "height", Arg: ControlHeightSmall},
				gxscene.Op{Code: gxscene.OpAttr, Key: "border-radius", Arg: RadiusPill},
				gxscene.Op{Code: gxscene.OpAttr, Key: "bg", Arg: ThemeFill},
				gxscene.Op{Code: gxscene.OpAttr, Key: "color", Arg: ThemeMuted},
				gxscene.Op{Code: gxscene.OpAttr, Key: "font-size", Arg: FontBody},
				gxscene.Op{Code: gxscene.OpAttr, Key: "align-items", Arg: "center"},
				gxscene.Op{Code: gxscene.OpAttr, Key: "justify-content", Arg: "center"},
				gxscene.Op{Code: gxscene.OpEvent, Key: "onClick", Slot: closeClickSlot},
				gxscene.Op{Code: gxscene.OpDynAttr, Key: "opacity", Slot: closeOpacitySlot},
				gxscene.Op{Code: gxscene.OpChildText, Arg: "×"},
				gxscene.Op{Code: gxscene.OpClose},
				gxscene.Op{Code: gxscene.OpClose}, // 标题栏
				// 内容区
				gxscene.Op{Code: gxscene.OpElem, Arg: "div"},
				gxscene.Op{Code: gxscene.OpAttr, Key: "flex-direction", Arg: "column"},
				gxscene.Op{Code: gxscene.OpDynChild, Slot: contentSlot},
				gxscene.Op{Code: gxscene.OpClose},
				// 底部操作区
				gxscene.Op{Code: gxscene.OpElem, Arg: "div"},
				gxscene.Op{Code: gxscene.OpAttr, Key: "flex-direction", Arg: "row"},
				gxscene.Op{Code: gxscene.OpAttr, Key: "justify-content", Arg: "flex-end"},
				gxscene.Op{Code: gxscene.OpAttr, Key: "gap", Arg: Space2},
				gxscene.Op{Code: gxscene.OpDynChild, Slot: actionsSlot},
				gxscene.Op{Code: gxscene.OpClose},
				gxscene.Op{Code: gxscene.OpClose}, // modal panel
			}

			getters := []func() any{
				func() any { // 0 scrim onClick
					return gxscene.Handler(func(gxscene.EventContext) {
						if maskCloseGet() {
							setOpen(false)
						}
					})
				},
				func() any { // 1 close 按钮 onClick
					return gxscene.Handler(func(gxscene.EventContext) { setOpen(false) })
				},
				func() any { // 2 标题（无标题时空串）
					if hasTitle {
						return title()
					}
					return ""
				},
				func() any { return contentGet() }, // 3 内容
				func() any { return actionsGet() }, // 4 操作
				func() any { // 5 关闭按钮 opacity
					if closableGet() {
						return "1"
					}
					return "0"
				},
			}
			return gxscene.Render(gxscene.Template{Ops: ops, N: nSlots}, getters...)
		},
	)

	return gxscene.Render(gxscene.Template{
		Ops: []gxscene.Op{
			gxscene.Op{Code: gxscene.OpElem, Arg: "div"}, // 透明根
			gxscene.Op{Code: gxscene.OpAttr, Key: "gx-modal-id", Arg: uid},
			gxscene.Op{Code: gxscene.OpAttr, Key: "flex-direction", Arg: "column"},
			gxscene.Op{Code: gxscene.OpBranch, Slot: 0},
			gxscene.Op{Code: gxscene.OpClose},
		},
		N: 1,
	}, func() any { return branch })
}

// Portal 构造一个"传送门"：子树被提升到浮层层，脱离父级 overflow 裁剪与 flex 流，
// 最后绘制在最上层。子树自身用 position:absolute + inset 定位（相对画布）。
// Props:
//
//	open     func() bool（可选；缺省视为常开）
//	children func() any → *Instance/[]any（要传送的子树）
//
// 注意：Portal 根节点本身透明（无 bg），尺寸由子树撑开；调用方如需铺满，
// 给子树节点设置 position:absolute + top/right/bottom/left=0。
func Portal(props gxscene.Props) *gxscene.Instance {
	uid := modalSeqName("portal")
	openGet := boolGet(props, "open", false)
	children := gxscene.Child(props)
	_, hasOpen := props["open"]
	isOpen := func() bool {
		if !hasOpen {
			return true // 无 open 属性视为常开
		}
		return openGet()
	}

	branch := gxscene.NewBranch(
		func() int {
			if isOpen() {
				return 1
			}
			return 0
		},
		func() *gxscene.Instance { return nil },
		func() *gxscene.Instance {
			return gxscene.Render(gxscene.Template{
				Ops: []gxscene.Op{
					gxscene.Op{Code: gxscene.OpElem, Arg: "div"},
					gxscene.Op{Code: gxscene.OpAttr, Key: "gx-role", Arg: "portal"},
					// 铺满画布作为子树的定位上下文：子树用 absolute + inset 相对它定位。
					gxscene.Op{Code: gxscene.OpAttr, Key: "position", Arg: "absolute"},
					gxscene.Op{Code: gxscene.OpAttr, Key: "top", Arg: "0"},
					gxscene.Op{Code: gxscene.OpAttr, Key: "left", Arg: "0"},
					gxscene.Op{Code: gxscene.OpAttr, Key: "right", Arg: "0"},
					gxscene.Op{Code: gxscene.OpAttr, Key: "bottom", Arg: "0"},
					gxscene.Op{Code: gxscene.OpDynChild, Slot: 0},
					gxscene.Op{Code: gxscene.OpClose},
				},
				N: 1,
			}, func() any { return children() })
		},
	)
	return gxscene.Render(gxscene.Template{
		Ops: []gxscene.Op{
			gxscene.Op{Code: gxscene.OpElem, Arg: "div"},
			gxscene.Op{Code: gxscene.OpAttr, Key: "gx-portal-id", Arg: uid},
			gxscene.Op{Code: gxscene.OpAttr, Key: "flex-direction", Arg: "column"},
			gxscene.Op{Code: gxscene.OpBranch, Slot: 0},
			gxscene.Op{Code: gxscene.OpClose},
		},
		N: 1,
	}, func() any { return branch })
}

// modalSeqName 生成一个稳定 id（预留给需要锚点引用的扩展）。
func modalSeqName(prefix string) string {
	return prefix + "-" + strconv.FormatUint(modalSeq.Add(1), 10)
}
