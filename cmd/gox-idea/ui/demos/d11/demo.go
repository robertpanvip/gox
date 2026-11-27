package d11

import (
    gxscene "github.com/robertpanvip/gox-core/runtime/scene"
    "fmt"
)

// 联合类型 int | string + is 守卫（擦除为 any 的语法便利层）
type value = any
func show(v any) string {
switch v.(type) {
    case int, string:
    default:
        panic("gx: show: v expects int|string, got a non-matching type")
 }

    if __gx_v_11_5, __gx_ok_11_5 := v.(string); __gx_ok_11_5 { v := __gx_v_11_5; return fmt.Sprint("str:", v) }
    return fmt.Sprint("int:", v)
}
 func View() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpElem,Arg:"span",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"span",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:3,},gxscene.Op{Code:gxscene.OpDynChild,Slot:4,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:5,}, func() any{ return gxscene.Style{FlexDirection:"column",Gap:"6",} }, func() any{ return gxscene.Style{Color:"#9cdcfe",} }, func() any{ return show(7) }, func() any{ return gxscene.Style{Color:"#c3e88d",} }, func() any{ return show("gx") })
}
