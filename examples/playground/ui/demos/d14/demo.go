package d14

import Signal "github.com/robertpanvip/gox/runtime/signal"

import (
    gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// struct sig 字段：显式标注才响应，构造后读到的是实时值
type state struct {
    count Signal.Signal[int]
}
var source = Signal.New(0)
func bump() { source(source() + 1 )}
 func View() *gxscene.Instance {
    s := state{count: source}
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpElem,Arg:"button",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:1,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:2,},gxscene.Op{Code:gxscene.OpChildText,Arg:"+1",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"span",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:3,},gxscene.Op{Code:gxscene.OpChildText,Arg:"count: ",},gxscene.Op{Code:gxscene.OpDynChild,Slot:4,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:5,}, func() any{ return gxscene.Style{FlexDirection:"column",Gap:"8",} }, func() any{ return bump }, func() any{ return gxscene.Style{Padding:"8",} }, func() any{ return gxscene.Style{Color:"#9cdcfe",} }, func() any{ return s.count() })
}
