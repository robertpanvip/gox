// app.gx —— 用 GX 语言编写的 playground 主界面。
//
// 与 editor.gx 一起构成"用 GX 写 GX playground"的自举案例：
// 所有 UI（工具栏 / 示例列表 / 编辑器 / 状态栏）都用 GX JSX 表达，
// 转译成标准 Go 场景树后，桌面（skia+GLFW）与网页（web+wasm）共用。
package ui

import (
    Signal "github.com/robertpanvip/gox/runtime/signal"
    gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// ---- 示例库：每个示例就是一段 GX 源码文本 ----

 type Sample struct {
     Name   string
     Source string
}

 var SampleIndex = Signal.New(0)

// 示例源码是常量，缓存一份切片，避免每次切换/渲染列表都重新构造 5 个结构体。
func buildSamples() []Sample {
    return []Sample{
        Sample{Name: "01 Hello GX", Source: "// 最小 GX 程序\nsig name = \"GX\"\nfunc view() *gxscene.Instance {\n    return <div>Hello, {name}!</div>\n}"},
        Sample{Name: "02 Counter", Source: "// signal 计数器\nsig count = 0\nfunc inc() {\n    count = count + 1\n}\nfunc view() *gxscene.Instance {\n    return <button onClick={inc}>{count}</button>\n}"},
        Sample{Name: "03 For 列表", Source: "// For 列表渲染\nsig todos = []string{\"a\", \"b\", \"c\"}\nfunc view() *gxscene.Instance {\n    return <div flex-direction=\"column\">\n        <For each={todos}>{(t string) => <span>{t()}</span>}</For>\n    </div>\n}"},
        Sample{Name: "04 样式卡片", Source: "// 深色卡片\nfunc view() *gxscene.Instance {\n    return <div bg=\"#0b1020\" padding=\"20\"\n        border-radius=\"12\" flex-direction=\"column\" gap=\"8\">\n        <b color=\"#7dd3fc\" font-size=\"24\">Card</b>\n        <span color=\"#94a3b8\">用 GX 写的样式</span>\n    </div>\n}"},
        Sample{Name: "05 语法高亮", Source: "// 关键字/字符串/注释都会着色\npub enum color { red, green, blue }\nfunc describe(v int | string) string {\n    if v is int { return \"int\" }\n    return \"string\"\n}"},
    }
}

var sampleStore []Sample = buildSamples()

 func Samples() []Sample { return sampleStore }

// loadSample 切换示例：把示例源码灌进编辑器。
 func LoadSample(i int) {
    xs := Samples()
    if i < 0 { i = 0 }
    if i >= len(xs) { i = len(xs) - 1 }
    // 一次批量提交：示例高亮、源码/行版本、光标、焦点合并为一帧 DOM 补丁。
    Signal.Batch(func() {
        SampleIndex(i)
        EditorSetText(xs[i].Source)
        EditorHome()
        EditorFocus()
    })
}

 func NextSample() { LoadSample(SampleIndex() + 1) }
 func PrevSample() { LoadSample(SampleIndex() - 1) }

// ---- 主界面 ----

// 选中态必须是读 sampleIndex 的 getter（不能是渲染时求值的局部变量快照），
// 否则切换示例后高亮不刷新——局部变量捕获的是渲染那一刻的 active 值。
func sampleRowBg(i int) string {
    if i == SampleIndex() { return "#094771" }
    return "#252526"
}
func sampleRowFg(i int) string {
    if i == SampleIndex() { return "#ffffff" }
    return "#cccccc"
}

func sampleRow(i int) *gxscene.Instance {
    xs := Samples()
    if i >= len(xs) { return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpClose,},},N:0,}) }
    name := xs[i].Name
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"bg",Slot:0,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"color",Slot:1,},gxscene.Op{Code:gxscene.OpAttr,Arg:"8",Key:"padding",},gxscene.Op{Code:gxscene.OpAttr,Arg:"13",Key:"font-size",},gxscene.Op{Code:gxscene.OpAttr,Arg:"4",Key:"border-radius",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:2,},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpClose,},},N:4,}, func() any{ return sampleRowBg(i) }, func() any{ return sampleRowFg(i) }, func() any{ return gxscene.Handler(func(ctx gxscene.EventContext) { LoadSample(i) }) }, func() any{ return name })
}

// 工具栏按钮：Next / Prev。显式写 *gxscene.Instance 注解。
 func NextButton() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"#0e639c",Key:"bg",},gxscene.Op{Code:gxscene.OpAttr,Arg:"#ffffff",Key:"color",},gxscene.Op{Code:gxscene.OpAttr,Arg:"8",Key:"padding",},gxscene.Op{Code:gxscene.OpAttr,Arg:"13",Key:"font-size",},gxscene.Op{Code:gxscene.OpAttr,Arg:"4",Key:"border-radius",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:0,},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpClose,},},N:2,}, func() any{ return gxscene.Handler(func(ctx gxscene.EventContext) { NextSample() }) }, func() any{ return "下一个 ▶" })
}

 func PrevButton() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"#3a3d45",Key:"bg",},gxscene.Op{Code:gxscene.OpAttr,Arg:"#ffffff",Key:"color",},gxscene.Op{Code:gxscene.OpAttr,Arg:"8",Key:"padding",},gxscene.Op{Code:gxscene.OpAttr,Arg:"13",Key:"font-size",},gxscene.Op{Code:gxscene.OpAttr,Arg:"4",Key:"border-radius",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:0,},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpClose,},},N:2,}, func() any{ return gxscene.Handler(func(ctx gxscene.EventContext) { PrevSample() }) }, func() any{ return "◀ 上一个" })
}

 func EditorPanel() *gxscene.Instance {
    return Editor()
}

 func SampleList() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"column",Key:"flex-direction",},gxscene.Op{Code:gxscene.OpAttr,Arg:"6",Key:"gap",},gxscene.Op{Code:gxscene.OpFor,Slot:0,},gxscene.Op{Code:gxscene.OpClose,},},N:1,}, gxscene.ForGetterOfSignal[int](func() []int { return Indices(len(Samples())) },nil,func(i func() int) *gxscene.Instance { return sampleRow(i()) }))
}

// Playground 返回完整界面场景树。两端装配层都调用这个函数。
 func Playground() *gxscene.Instance {
    title := "GX Playground"
    sub := "用 GX 写 GX —— 桌面与网页同源"
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"#1e1e1e",Key:"bg",},gxscene.Op{Code:gxscene.OpAttr,Arg:"#d4d4d4",Key:"color",},gxscene.Op{Code:gxscene.OpAttr,Arg:"20",Key:"padding",},gxscene.Op{Code:gxscene.OpAttr,Arg:"column",Key:"flex-direction",},gxscene.Op{Code:gxscene.OpAttr,Arg:"14",Key:"gap",},gxscene.Op{Code:gxscene.OpAttr,Arg:"600",Key:"height",},gxscene.Op{Code:gxscene.OpAttr,Arg:"920",Key:"width",},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"column",Key:"flex-direction",},gxscene.Op{Code:gxscene.OpAttr,Arg:"4",Key:"gap",},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"#4ec9b0",Key:"color",},gxscene.Op{Code:gxscene.OpAttr,Arg:"24",Key:"font-size",},gxscene.Op{Code:gxscene.OpAttr,Arg:"bold",Key:"weight",},gxscene.Op{Code:gxscene.OpDynChild,Slot:0,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"#858585",Key:"color",},gxscene.Op{Code:gxscene.OpAttr,Arg:"13",Key:"font-size",},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"row",Key:"flex-direction",},gxscene.Op{Code:gxscene.OpAttr,Arg:"14",Key:"gap",},gxscene.Op{Code:gxscene.OpAttr,Arg:"420",Key:"height",},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"#252526",Key:"bg",},gxscene.Op{Code:gxscene.OpAttr,Arg:"12",Key:"padding",},gxscene.Op{Code:gxscene.OpAttr,Arg:"8",Key:"border-radius",},gxscene.Op{Code:gxscene.OpAttr,Arg:"200",Key:"width",},gxscene.Op{Code:gxscene.OpAttr,Arg:"column",Key:"flex-direction",},gxscene.Op{Code:gxscene.OpAttr,Arg:"8",Key:"gap",},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"#9cdcfe",Key:"color",},gxscene.Op{Code:gxscene.OpAttr,Arg:"13",Key:"font-size",},gxscene.Op{Code:gxscene.OpAttr,Arg:"bold",Key:"weight",},gxscene.Op{Code:gxscene.OpChildText,Arg:"示例",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"column",Key:"flex-direction",},gxscene.Op{Code:gxscene.OpAttr,Arg:"6",Key:"gap",},gxscene.Op{Code:gxscene.OpAttr,Arg:"640",Key:"width",},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"row",Key:"flex-direction",},gxscene.Op{Code:gxscene.OpAttr,Arg:"8",Key:"gap",},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpDynChild,Slot:4,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"#858585",Key:"color",},gxscene.Op{Code:gxscene.OpAttr,Arg:"12",Key:"font-size",},gxscene.Op{Code:gxscene.OpAttr,Arg:"8",Key:"padding",},gxscene.Op{Code:gxscene.OpDynChild,Slot:5,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpDynChild,Slot:6,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"#6a9955",Key:"color",},gxscene.Op{Code:gxscene.OpAttr,Arg:"12",Key:"font-size",},gxscene.Op{Code:gxscene.OpChildText,Arg:"提示：点击「下一个」切换示例；键盘输入由装配层转发到编辑器命令。",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:7,}, func() any{ return title }, func() any{ return sub }, func() any{ return SampleList() }, func() any{ return PrevButton() }, func() any{ return NextButton() }, func() any{ return EditorStatus() }, func() any{ return EditorPanel() })
}
