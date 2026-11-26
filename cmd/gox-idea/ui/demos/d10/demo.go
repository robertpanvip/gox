package d10

import (
    gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// 对象解构：{ x, y } := p 逐字段取值
type point struct { x int; y int }
func coord() point { return point{x: 3, y: 4} }
 func View() *gxscene.Instance {
    p := coord()
    var x = p.x
var y = p.y

    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpElem,Arg:"span",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:1,},gxscene.Op{Code:gxscene.OpChildText,Arg:"x = ",},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"span",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:3,},gxscene.Op{Code:gxscene.OpChildText,Arg:"y = ",},gxscene.Op{Code:gxscene.OpDynChild,Slot:4,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"span",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:5,},gxscene.Op{Code:gxscene.OpChildText,Arg:"sum = ",},gxscene.Op{Code:gxscene.OpDynChild,Slot:6,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:7,}, func() any{ return gxscene.Style{FlexDirection:"column",Gap:"6",} }, func() any{ return gxscene.Style{Color:"#9cdcfe",} }, func() any{ return x }, func() any{ return gxscene.Style{Color:"#c3e88d",} }, func() any{ return y }, func() any{ return gxscene.Style{Color:"#858585",} }, func() any{ return x + y })
}
