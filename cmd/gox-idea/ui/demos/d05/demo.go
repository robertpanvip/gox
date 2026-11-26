package d05

import (
    gxscene "github.com/robertpanvip/gox/runtime/scene"
    Signal "github.com/robertpanvip/gox/runtime/signal"
)

// Signal.computed 派生 + Signal.batch 合并 + 事件传函数原值
var price = Signal.New(10)
var qty = Signal.New(2)
var total = Signal.Computed(func() any {return price() * qty()})
func bump() {
    Signal.Batch(func() {
        price(price() + 1)
        qty(qty() + 1)
    })
}
 func View() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpElem,Arg:"span",},gxscene.Op{Code:gxscene.OpChildText,Arg:"Total: ",},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"button",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:2,},gxscene.Op{Code:gxscene.OpChildText,Arg:"加价加量",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:3,}, func() any{ return gxscene.Style{FlexDirection:"column",Gap:"6",} }, func() any{ return total() }, func() any{ return bump })
}