package d15

import "github.com/robertpanvip/gox-core/runtime/list"
import Signal "github.com/robertpanvip/gox-core/runtime/signal"

import (
    gxscene "github.com/robertpanvip/gox-core/runtime/scene"
)

// 集合链式：[]T 提升为 List[T]，filter/map/reduce 可换型
var nums = Signal.New(list.List[int]{1, 2, 3, 4, 5})
func shuffle() { nums(list.List[int]{2, 4, 6, 8} )}
 func View() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpElem,Arg:"button",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:1,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:2,},gxscene.Op{Code:gxscene.OpChildText,Arg:"换数据",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"span",},gxscene.Op{Code:gxscene.OpChildText,Arg:"偶数个数: ",},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:4,}, func() any{ return gxscene.Style{FlexDirection:"column",Gap:"8",} }, func() any{ return shuffle }, func() any{ return gxscene.Style{Padding:"8",} }, func() any{ return len(nums().Filter(func(n int) bool { return n % 2 == 0 })) })
}
