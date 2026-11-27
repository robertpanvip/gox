package d04

import (
    gxscene "github.com/robertpanvip/gox-core/runtime/scene"
)

// style={{...}} 强类型 struct + 驼峰 key + 伪状态单词 key
 func View() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpElem,Arg:"b",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:1,},gxscene.Op{Code:gxscene.OpChildText,Arg:"Card",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"span",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:2,},gxscene.Op{Code:gxscene.OpChildText,Arg:"用 GX style struct 写的样式",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:3,}, func() any{ return gxscene.Style{Background:"#0b1020",Padding:"20",BorderRadius:"12",FlexDirection:"column",Gap:"8",Hover:&gxscene.Style{Background:"#1e293b",},} }, func() any{ return gxscene.Style{Color:"#7dd3fc",FontSize:"24",} }, func() any{ return gxscene.Style{Color:"#94a3b8",} })
}