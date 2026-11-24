package d06

import Signal "github.com/robertpanvip/gox/runtime/signal"

import (
    gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// JSX 条件分支：if/else 编译成细粒度 Branch，条件变化只换分支子树
var on = Signal.New(true)
func toggle() { on(!on() )}
 func View() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpElem,Arg:"button",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:1,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:2,},gxscene.Op{Code:gxscene.OpChildText,Arg:"切换分支",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:3,},gxscene.Op{Code:gxscene.OpBranch,Slot:4,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:5,}, func() any{ return gxscene.Style{FlexDirection:"column",Gap:"8",} }, func() any{ return toggle }, func() any{ return gxscene.Style{Background:"#3b82f6",Padding:"8",BorderRadius:"6",} }, func() any{ return gxscene.Style{Padding:"16",BorderRadius:"8",} }, gxscene.BranchGetter(func() int{if on() { return 0 }
return 1 }, func() *gxscene.Instance { return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"#frag",},gxscene.Op{Code:gxscene.OpElem,Arg:"b",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpChildText,Arg:"ON · 分支 A",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:1,}, func() any{ return gxscene.Style{Color:"#4ec9b0",FontSize:"18",} }) }, func() *gxscene.Instance { return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"#frag",},gxscene.Op{Code:gxscene.OpElem,Arg:"b",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpChildText,Arg:"OFF · 分支 B",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:1,}, func() any{ return gxscene.Style{Color:"#f87171",FontSize:"18",} }) }))
}
