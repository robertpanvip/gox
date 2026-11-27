// Gox 图标画廊 —— 具名矢量图标组件集 showcase。
// 用 GX 语言编写，展示 widgets 组件库预制的具名图标组件（IconXxx）与全部内置图标。
//
// 演示两点：
//   1. 具名组件强类型用法：<widgets.iconHouse size="24" tone="blue"/>（源码小写，
//      编译器 upperFirst 还原为 widgets.IconHouse）；
//   2. 全量图标网格：<For each={names}> + 通用 <widgets.icon name={name}/> 遍历
//      runtime/icon.Names() 的全部内置图标名。
//
// 全部图标走 host SVG 解析 → canvas 矢量指令 → 各后端矢量绘制，放大不失真、零位图。
// 本示例用 headless app 后端，无需 GUI 即可编译运行验证场景树构建。
// 转译：gox build examples/icon-gallery/app.gx -o main.go && go run .
package main

import (
    gxscene "github.com/robertpanvip/gox-core/runtime/scene"
    gxapp "github.com/robertpanvip/gox-core/runtime/app"
    gxicon "github.com/robertpanvip/gox-core/runtime/icon"
    widgets "github.com/robertpanvip/gox-core/widgets"
    "fmt"
)

// NamedDemo 具名组件演示行：直接以组件形式引用预制图标，size/color/tone 由 props 决定。
 func NamedDemo(props gxscene.Props) *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"row",Key:"flex-direction",},gxscene.Op{Code:gxscene.OpAttr,Arg:"18",Key:"gap",},gxscene.Op{Code:gxscene.OpAttr,Arg:"center",Key:"align",},gxscene.Op{Code:gxscene.OpComp,Arg:"widgets.iconHouse",Slot:0,},gxscene.Op{Code:gxscene.OpComp,Arg:"widgets.iconSearch",Slot:1,},gxscene.Op{Code:gxscene.OpComp,Arg:"widgets.iconHeart",Slot:2,},gxscene.Op{Code:gxscene.OpComp,Arg:"widgets.iconStar",Slot:3,},gxscene.Op{Code:gxscene.OpComp,Arg:"widgets.iconGear",Slot:4,},gxscene.Op{Code:gxscene.OpComp,Arg:"widgets.iconBolt",Slot:5,},gxscene.Op{Code:gxscene.OpClose,},},N:6,}, func() any{ return widgets.IconHouse(gxscene.Props{"size":"28","tone":"blue",}) }, func() any{ return widgets.IconSearch(gxscene.Props{"size":"28","tone":"primary",}) }, func() any{ return widgets.IconHeart(gxscene.Props{"size":"28","color":"#ff453a",}) }, func() any{ return widgets.IconStar(gxscene.Props{"size":"28","tone":"orange",}) }, func() any{ return widgets.IconGear(gxscene.Props{"size":"28","tone":"primary",}) }, func() any{ return widgets.IconBolt(gxscene.Props{"size":"28","tone":"yellow",}) })
}

func main() {
    // 显式类型注解，让 <For> 推断 ForGetterOf[string]（否则外部函数返回值退化为 any）。
    var names []string = gxicon.Names()

    app, err := gxapp.Open(gxapp.WindowConfig{Title:"Gox Icon Gallery", Width:980, Height:760})
    if err != nil { panic(err) }
    defer app.Close()

    // 头部 + 具名演示 + 全量网格。网格每项 = 图标 + 名称文本。
    app.Render(func() any {return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"#1c1c1e",Key:"bg",},gxscene.Op{Code:gxscene.OpAttr,Arg:"24",Key:"padding",},gxscene.Op{Code:gxscene.OpAttr,Arg:"column",Key:"flex-direction",},gxscene.Op{Code:gxscene.OpAttr,Arg:"16",Key:"gap",},gxscene.Op{Code:gxscene.OpAttr,Arg:"760",Key:"height",},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"column",Key:"flex-direction",},gxscene.Op{Code:gxscene.OpAttr,Arg:"6",Key:"gap",},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"#ffffff",Key:"color",},gxscene.Op{Code:gxscene.OpAttr,Arg:"26",Key:"font-size",},gxscene.Op{Code:gxscene.OpAttr,Arg:"bold",Key:"weight",},gxscene.Op{Code:gxscene.OpChildText,Arg:"Gox Icon Gallery",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"#98989f",Key:"color",},gxscene.Op{Code:gxscene.OpAttr,Arg:"13",Key:"font-size",},gxscene.Op{Code:gxscene.OpDynChild,Slot:0,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpComp,Arg:"widgets.card",Slot:1,},gxscene.Op{Code:gxscene.OpComp,Arg:"widgets.card",Slot:2,},gxscene.Op{Code:gxscene.OpClose,},},N:3,}, func() any{ return fmt.Sprint(len(names)) + " 个内置矢量图标 · 组件库预制具名组件" }, func() any{ return widgets.Card(gxscene.Props{"title":"NAMED COMPONENTS","tone":"primary","width":"932","children":func() any{ return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"#frag",},gxscene.Op{Code:gxscene.OpComp,Arg:"NamedDemo",Slot:0,},gxscene.Op{Code:gxscene.OpClose,},},N:1,}, func() any{ return NamedDemo(gxscene.Props{}) }) },}) }, func() any{ return widgets.Card(gxscene.Props{"title":"ALL ICONS","tone":"info","width":"932","children":func() any{ return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"#frag",},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"row",Key:"flex-direction",},gxscene.Op{Code:gxscene.OpAttr,Arg:"12",Key:"gap",},gxscene.Op{Code:gxscene.OpAttr,Arg:"wrap",Key:"flex-wrap",},gxscene.Op{Code:gxscene.OpAttr,Arg:"900",Key:"width",},gxscene.Op{Code:gxscene.OpFor,Slot:0,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:1,}, gxscene.ForGetterOf[string](func() []string { return names },nil,func(name string) *gxscene.Instance { return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"column",Key:"flex-direction",},gxscene.Op{Code:gxscene.OpAttr,Arg:"4",Key:"gap",},gxscene.Op{Code:gxscene.OpAttr,Arg:"center",Key:"align",},gxscene.Op{Code:gxscene.OpAttr,Arg:"88",Key:"width",},gxscene.Op{Code:gxscene.OpAttr,Arg:"8",Key:"padding",},gxscene.Op{Code:gxscene.OpComp,Arg:"widgets.icon",Slot:0,},gxscene.Op{Code:gxscene.OpElem,Arg:"span",},gxscene.Op{Code:gxscene.OpAttr,Arg:"#98989f",Key:"color",},gxscene.Op{Code:gxscene.OpAttr,Arg:"10",Key:"font-size",},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:2,}, func() any{ return widgets.Icon(gxscene.Props{"name":func() any{ return name },"size":"24","color":"#0a84ff",}) }, func() any{ return name }) })) },}) })})
}
