package d07

import Signal "github.com/robertpanvip/gox/runtime/signal"

import (
    gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// sig 切片增删 + For 响应式列表 + item getter（t() 读值）
var items = Signal.New([]string{"alpha", "beta"})
func add() { items(append(items(), "item") )}
func clear() { items([]string{} )}
 func View() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:1,},gxscene.Op{Code:gxscene.OpElem,Arg:"button",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:2,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:3,},gxscene.Op{Code:gxscene.OpChildText,Arg:"添加",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"button",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:4,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:5,},gxscene.Op{Code:gxscene.OpChildText,Arg:"清空",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpFor,Slot:6,},gxscene.Op{Code:gxscene.OpClose,},},N:7,}, func() any{ return gxscene.Style{FlexDirection:"column",Gap:"8",} }, func() any{ return gxscene.Style{FlexDirection:"row",Gap:"8",} }, func() any{ return add }, func() any{ return gxscene.Style{Padding:"8",BorderRadius:"6",} }, func() any{ return clear }, func() any{ return gxscene.Style{Padding:"8",BorderRadius:"6",} }, gxscene.ForGetterOfSignal[string](func() []string { return items() },nil,func(t func() string) *gxscene.Instance { return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpClose,},},N:2,}, func() any{ return gxscene.Style{Color:"#9cdcfe",Padding:"4",} }, func() any{ return t() }) }))
}
