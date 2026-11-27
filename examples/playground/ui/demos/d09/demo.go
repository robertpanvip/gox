package d09

import Signal "github.com/robertpanvip/gox-core/runtime/signal"
import "fmt"

import (
    gxscene "github.com/robertpanvip/gox-core/runtime/scene"
)

// 模板字符串（反引号内插值）+ 参数默认值（尾部缺失参数自动补全）
func greet(name string , mark string ) string {
    return "你好, "+fmt.Sprint(name)+fmt.Sprint(mark)
}
var who = Signal.New("world")
func fullText() string { return greet(who(),"!") }
func defaultText() string { return greet("GX","!") }
func change() { who("GX" )}
 func View() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpElem,Arg:"button",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:1,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:2,},gxscene.Op{Code:gxscene.OpChildText,Arg:"换名字",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"span",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:3,},gxscene.Op{Code:gxscene.OpDynChild,Slot:4,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"span",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:5,},gxscene.Op{Code:gxscene.OpDynChild,Slot:6,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:7,}, func() any{ return gxscene.Style{FlexDirection:"column",Gap:"8",} }, func() any{ return change }, func() any{ return gxscene.Style{Padding:"8",} }, func() any{ return gxscene.Style{Color:"#c3e88d",FontSize:"16",} }, func() any{ return fullText() }, func() any{ return gxscene.Style{Color:"#858585",} }, func() any{ return defaultText() })
}
