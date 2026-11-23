// Gox 桌面 Demo —— PULSE 实时数据看板（预制控件版）
// 用 GX 编写，控件外观全部来自 skia 后端预制的 widgets 库，核心运行时零改动。
// 演示：signal 源 + computed 派生 + 预制控件库 + 真实键盘输入 + 事件写 signal + goroutine 数据流。
package main

import (
    gxscene "github.com/robertpanvip/gox/runtime/scene"
    gxapp "github.com/robertpanvip/gox/runtime/app"
    "github.com/robertpanvip/gox/runtime/signal"
    widgets "github.com/robertpanvip/gox/widgets"
    "fmt"
    "time"
)

// Brand 品牌标题（业务专属，不进通用控件库）。
 func Brand(props gxscene.Props) *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"column",Key:"flex-direction",},gxscene.Op{Code:gxscene.OpAttr,Arg:"6",Key:"gap",},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"#eef3fb",Key:"color",},gxscene.Op{Code:gxscene.OpAttr,Arg:"30",Key:"font-size",},gxscene.Op{Code:gxscene.OpAttr,Arg:"bold",Key:"weight",},gxscene.Op{Code:gxscene.OpChildText,Arg:"PULSE",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"#6b7a94",Key:"color",},gxscene.Op{Code:gxscene.OpAttr,Arg:"13",Key:"font-size",},gxscene.Op{Code:gxscene.OpChildText,Arg:"Real-time signal dashboard · widgets",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:0,})
}

func main() {
    var count = signal.New(42)
    var name = signal.New("operator")
    doubled := signal.Computed(func() int {return count() * 2})
    loadPct := signal.Computed(func() int {return (count() % 50) * 2})

    // 模拟实时数据流：每 1.4 秒 +1
    go func() {
        for {
            time.Sleep(1400 * time.Millisecond)
            count(count() + 1)
        }
    }()

    app, err := gxapp.OpenWith(gxapp.WindowConfig{Title:"PULSE Dashboard", Width:760, Height:640}, openGL())
    if err != nil { panic(err) }
    defer app.Close()

    // 紧凑单行规避 GX 编译器跨行缩进边界；signal 写用 () 显式读。
    // 所有按钮/卡片/徽章/进度条/输入框均来自后端预制 widgets 库——JSX 只描述语义。
    app.Render(func() any {return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"#0b1020",Key:"bg",},gxscene.Op{Code:gxscene.OpAttr,Arg:"640",Key:"height",},gxscene.Op{Code:gxscene.OpAttr,Arg:"28",Key:"padding",},gxscene.Op{Code:gxscene.OpAttr,Arg:"column",Key:"flex-direction",},gxscene.Op{Code:gxscene.OpAttr,Arg:"18",Key:"gap",},gxscene.Op{Code:gxscene.OpComp,Arg:"widgets.row",Slot:0,},gxscene.Op{Code:gxscene.OpComp,Arg:"widgets.row",Slot:1,},gxscene.Op{Code:gxscene.OpComp,Arg:"widgets.card",Slot:2,},gxscene.Op{Code:gxscene.OpComp,Arg:"widgets.card",Slot:3,},gxscene.Op{Code:gxscene.OpComp,Arg:"widgets.row",Slot:4,},gxscene.Op{Code:gxscene.OpClose,},},N:5,}, func() any{ return widgets.Row(gxscene.Props{"gap":"12","align":"center","justify":"space-between","children":func() any{ return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"#frag",},gxscene.Op{Code:gxscene.OpComp,Arg:"Brand",Slot:0,},gxscene.Op{Code:gxscene.OpComp,Arg:"widgets.badge",Slot:1,},gxscene.Op{Code:gxscene.OpClose,},},N:2,}, func() any{ return Brand(gxscene.Props{}) }, func() any{ return widgets.Badge(gxscene.Props{"label":"LIVE","tone":"green","dot":true,}) }) },}) }, func() any{ return widgets.Row(gxscene.Props{"gap":"14","children":func() any{ return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"#frag",},gxscene.Op{Code:gxscene.OpComp,Arg:"widgets.card",Slot:0,},gxscene.Op{Code:gxscene.OpComp,Arg:"widgets.card",Slot:1,},gxscene.Op{Code:gxscene.OpComp,Arg:"widgets.card",Slot:2,},gxscene.Op{Code:gxscene.OpClose,},},N:3,}, func() any{ return widgets.Card(gxscene.Props{"title":"REQUESTS","tone":"cyan","width":"196","children":func() any{ return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"#frag",},gxscene.Op{Code:gxscene.OpElem,Arg:"b",},gxscene.Op{Code:gxscene.OpAttr,Arg:"#2dd4bf",Key:"color",},gxscene.Op{Code:gxscene.OpAttr,Arg:"34",Key:"font-size",},gxscene.Op{Code:gxscene.OpAttr,Arg:"bold",Key:"weight",},gxscene.Op{Code:gxscene.OpDynChild,Slot:0,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:1,}, func() any{ return fmt.Sprint(count()) }) },}) }, func() any{ return widgets.Card(gxscene.Props{"title":"THROUGHPUT","tone":"purple","width":"196","children":func() any{ return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"#frag",},gxscene.Op{Code:gxscene.OpElem,Arg:"b",},gxscene.Op{Code:gxscene.OpAttr,Arg:"#a78bfa",Key:"color",},gxscene.Op{Code:gxscene.OpAttr,Arg:"34",Key:"font-size",},gxscene.Op{Code:gxscene.OpAttr,Arg:"bold",Key:"weight",},gxscene.Op{Code:gxscene.OpDynChild,Slot:0,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:1,}, func() any{ return fmt.Sprint(doubled()) }) },}) }, func() any{ return widgets.Card(gxscene.Props{"title":"UPTIME","tone":"orange","width":"196","children":func() any{ return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"#frag",},gxscene.Op{Code:gxscene.OpElem,Arg:"b",},gxscene.Op{Code:gxscene.OpAttr,Arg:"#fb923c",Key:"color",},gxscene.Op{Code:gxscene.OpAttr,Arg:"34",Key:"font-size",},gxscene.Op{Code:gxscene.OpAttr,Arg:"bold",Key:"weight",},gxscene.Op{Code:gxscene.OpChildText,Arg:"99.98%",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:0,}) },}) }) },}) }, func() any{ return widgets.Card(gxscene.Props{"title":"SYSTEM LOAD","tone":"info","width":"624","children":func() any{ return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"#frag",},gxscene.Op{Code:gxscene.OpComp,Arg:"widgets.progress",Slot:0,},gxscene.Op{Code:gxscene.OpClose,},},N:1,}, func() any{ return widgets.Progress(gxscene.Props{"value":func() any{ return loadPct() },"width":"560","tone":"cyan","label":func() any{ return fmt.Sprint(loadPct()) + "%" },}) }) },}) }, func() any{ return widgets.Card(gxscene.Props{"title":"OPERATOR CONSOLE","tone":"primary","width":"624","children":func() any{ return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"#frag",},gxscene.Op{Code:gxscene.OpComp,Arg:"widgets.input",Slot:0,},gxscene.Op{Code:gxscene.OpElem,Arg:"span",},gxscene.Op{Code:gxscene.OpAttr,Arg:"#8b9bb4",Key:"color",},gxscene.Op{Code:gxscene.OpAttr,Arg:"13",Key:"font-size",},gxscene.Op{Code:gxscene.OpChildText,Arg:"Hello, ",},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpChildText,Arg:" — the text box is backed by a signal (type and press backspace).",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:2,}, func() any{ return widgets.Input(gxscene.Props{"id":"operator","value":func() any{ return name() },"placeholder":"Type your name…","width":"300","onInput":func() any{ return widgets.BindInput(func(s string) { name(s )}) },}) }, func() any{ return name() }) },}) }, func() any{ return widgets.Row(gxscene.Props{"gap":"12","children":func() any{ return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"#frag",},gxscene.Op{Code:gxscene.OpComp,Arg:"widgets.button",Slot:0,},gxscene.Op{Code:gxscene.OpComp,Arg:"widgets.button",Slot:1,},gxscene.Op{Code:gxscene.OpComp,Arg:"widgets.button",Slot:2,},gxscene.Op{Code:gxscene.OpClose,},},N:3,}, func() any{ return widgets.Button(gxscene.Props{"label":"+ BOOST","variant":"primary","onClick":func() any{ return gxscene.Handler(func(ctx gxscene.EventContext) { count(count() + 10 )}) },}) }, func() any{ return widgets.Button(gxscene.Props{"label":"- RESET","variant":"secondary","onClick":func() any{ return gxscene.Handler(func(ctx gxscene.EventContext) { count(count() - 10 )}) },}) }, func() any{ return widgets.Button(gxscene.Props{"label":"CLEAR","variant":"danger","onClick":func() any{ return gxscene.Handler(func(ctx gxscene.EventContext) { name("" )}) },}) }) },}) })})

    app.Run()
}
