package d01

import Signal "github.com/robertpanvip/gox-core/runtime/signal"

import (
    gxscene "github.com/robertpanvip/gox-core/runtime/scene"
)

// sig 声明 + 裸读（编译器自动补 ()）
var name = Signal.New("GX")
 func View() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpChildText,Arg:"Hello, ",},gxscene.Op{Code:gxscene.OpDynChild,Slot:0,},gxscene.Op{Code:gxscene.OpChildText,Arg:"!",},gxscene.Op{Code:gxscene.OpClose,},},N:1,}, func() any{ return name() })
}