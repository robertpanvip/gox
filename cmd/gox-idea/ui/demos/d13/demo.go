package d13

import Signal "github.com/robertpanvip/gox-core/runtime/signal"

import (
    gxscene "github.com/robertpanvip/gox-core/runtime/scene"
)

// 事件传函数原值：具名函数 / 箭头 / func 字面量 / 带 EventContext 签名
var last = Signal.New("none")
func named() { last("named" )}
func onCtx(e gxscene.EventContext) { if e.Synthetic != nil { last(string(e.Synthetic.Type) )} }
 func View() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpElem,Arg:"button",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:1,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:2,},gxscene.Op{Code:gxscene.OpChildText,Arg:"具名函数",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"button",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:3,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:4,},gxscene.Op{Code:gxscene.OpChildText,Arg:"箭头",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"button",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:5,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:6,},gxscene.Op{Code:gxscene.OpChildText,Arg:"func 字面量",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"button",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:7,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:8,},gxscene.Op{Code:gxscene.OpChildText,Arg:"带 ctx",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"span",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:9,},gxscene.Op{Code:gxscene.OpChildText,Arg:"last: ",},gxscene.Op{Code:gxscene.OpDynChild,Slot:10,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:11,}, func() any{ return gxscene.Style{FlexDirection:"column",Gap:"6",} }, func() any{ return named }, func() any{ return gxscene.Style{Padding:"8",} }, func() any{ return func() { last("arrow" )} }, func() any{ return gxscene.Style{Padding:"8",} }, func() any{ return func() { last("literal" )} }, func() any{ return gxscene.Style{Padding:"8",} }, func() any{ return onCtx }, func() any{ return gxscene.Style{Padding:"8",} }, func() any{ return gxscene.Style{Color:"#c3e88d",} }, func() any{ return last() })
}
