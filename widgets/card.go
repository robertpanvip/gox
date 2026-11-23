package widgets

// Card 预制卡片：深色面板 + 细描边 + 柔和阴影 + 可选标题与任意子节点。
// 本组件用 GX 语言（JSX）编写，经 gox build 转译为 card.go 后供 Go 工具链编译。
// 这是 widgets 标准库"用 GX 自身书写"的样板组件。
//
// props:
//
//	title    标题文本（可传 getter）
//	width    宽度
//	padding  内边距（默认 16）
//	tone     标题强调色
//	children 子节点（gxscene.Child）
import gxscene "github.com/robertpanvip/gox/runtime/scene"

 func Card(props gxscene.Props) *gxscene.Instance {
	title := gxscene.Get[string](props, "title")
	width := gxscene.Get[string](props, "width")
	padding := gxscene.Get[string](props, "padding")
	tone := gxscene.Get[string](props, "tone")
	children := gxscene.Child(props)

	pad := padding()
	if pad == "" {
		pad = "16"
	}

	// 标题行 + 内容区。标题为空时仍保留空 span 占位，维持稳定的插槽结构。
	return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"card",Key:"gx-role",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"bg",Slot:0,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"border",Slot:1,},gxscene.Op{Code:gxscene.OpAttr,Arg:"1",Key:"border-width",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"border-radius",Slot:2,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"box-shadow",Slot:3,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"padding",Slot:4,},gxscene.Op{Code:gxscene.OpAttr,Arg:"column",Key:"flex-direction",},gxscene.Op{Code:gxscene.OpAttr,Arg:"8",Key:"gap",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"width",Slot:5,},gxscene.Op{Code:gxscene.OpElem,Arg:"span",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"color",Slot:6,},gxscene.Op{Code:gxscene.OpAttr,Arg:"12",Key:"font-size",},gxscene.Op{Code:gxscene.OpAttr,Arg:"bold",Key:"weight",},gxscene.Op{Code:gxscene.OpDynChild,Slot:7,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"span",},gxscene.Op{Code:gxscene.OpDynChild,Slot:8,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:9,}, func() any{ return ThemePanel }, func() any{ return AppleSeparator }, func() any{ return RadiusCard }, func() any{ return ShadowCard }, func() any{ return pad }, func() any{ return width() }, func() any{ return toneColor(tone()) }, func() any{ return title() }, func() any{ return children() })
}
