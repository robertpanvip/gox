package d02

import Signal "github.com/robertpanvip/gox-core/runtime/signal"

import (
    gxscene "github.com/robertpanvip/gox-core/runtime/scene"
)

// 事件传函数原值 + sig 读写 + 样式进 style struct
var count = Signal.New(0)
func inc() {
    count(count() + 1)
}
 func View() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"button",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:0,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:1,},gxscene.Op{Code:gxscene.OpChildText,Arg:"Count: ",},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpClose,},},N:3,}, func() any{ return inc }, func() any{ return gxscene.Style{Background:"#3b82f6",Padding:"10",} }, func() any{ return count() })
}