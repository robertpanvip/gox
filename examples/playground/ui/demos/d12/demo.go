package d12

import Signal "github.com/robertpanvip/gox-core/runtime/signal"

import (
    gxscene "github.com/robertpanvip/gox-core/runtime/scene"
)

// 组件：pub func 返回场景树，props 的 sig 字段保鲜（读到的是实时值）
type cardProps struct {
    title Signal.Signal[string]
}
 func Card(p cardProps) *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpElem,Arg:"b",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:3,}, func() any{ return gxscene.Style{Background:"#1e293b",Padding:"12",BorderRadius:"8",} }, func() any{ return gxscene.Style{Color:"#7dd3fc",} }, func() any{ return p.title() })
}
var text = Signal.New("Hello")
func change() { text("Changed" )}
 func View() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpElem,Arg:"button",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:1,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:2,},gxscene.Op{Code:gxscene.OpChildText,Arg:"改标题",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpComp,Arg:"Card",Slot:3,},gxscene.Op{Code:gxscene.OpClose,},},N:4,}, func() any{ return gxscene.Style{FlexDirection:"column",Gap:"8",} }, func() any{ return change }, func() any{ return gxscene.Style{Padding:"8",} }, func() any{ return Card(cardProps{title:text,}) })
}
