package d08

import Signal "github.com/robertpanvip/gox/runtime/signal"

import (
    gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// enum 枚举：类型 + 常量 + String() + Parse<T>()，三种拼写指向同一符号
type Color string
const (
	ColorRed Color = "#ef4444"
	ColorGreen Color = "#22c55e"
	ColorBlue Color = "#3b82f6"
)
func (x Color) String() string { return string(x) }
func ParseColor(s string) (Color, bool) {
	switch s {
	case "#ef4444":
		return ColorRed, true
	case "#22c55e":
		return ColorGreen, true
	case "#3b82f6":
		return ColorBlue, true
	}
	return "", false
}

var idx = Signal.New(0)
func next() { idx((idx() + 1) % 3 )}
 func View() *gxscene.Instance {
    cs := []Color{ColorRed, ColorGreen, ColorBlue}
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpElem,Arg:"button",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:1,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:2,},gxscene.Op{Code:gxscene.OpChildText,Arg:"换色",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:3,},gxscene.Op{Code:gxscene.OpDynChild,Slot:4,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:5,}, func() any{ return gxscene.Style{FlexDirection:"column",Gap:"8",} }, func() any{ return next }, func() any{ return gxscene.Style{Padding:"8",} }, func() any{ return gxscene.Style{Background:string(cs[idx()]),Padding:"20",BorderRadius:"8",Color:"#fff",} }, func() any{ return cs[idx()] })
}
