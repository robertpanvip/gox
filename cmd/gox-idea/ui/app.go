// app.gx —— 用 GX 语言编写的 playground 主界面。
//
// 与 editor.gx 一起构成"用 GX 写 GX playground"的自举案例：
// 所有 UI（工具栏 / 示例列表 / 编辑器 / 实时预览 / 状态栏）都用 GX JSX 表达，
// 转译成标准 Go 场景树后，桌面（skia+GLFW）与网页（web+wasm）共用。
package ui

import (
    "os"
    "path/filepath"
    "strconv"
    demos "github.com/robertpanvip/gox-idea/ui/demos"
    Signal "github.com/robertpanvip/gox/runtime/signal"
    gxscene "github.com/robertpanvip/gox/runtime/scene"
    widgets "github.com/robertpanvip/gox/widgets"
)

// ---- 编辑器实例（widgets.Editor 公共组件）----
// 从 ui/editor.gx 抽离到 widgets 后，playground 持有一个 EditorState 句柄，
// 通过下列转发函数把装配层（desktop/web）的 ui.Editor* 调用委托给实例方法。
// 这样装配层零改动，同时编辑器本身成为可多实例/多 App 隔离的公共组件。
var editorState = widgets.NewEditorState("")

// Ed 暴露编辑器实例句柄（供需要直接访问 state 的装配层使用）。
 func Ed() *widgets.EditorState { return editorState }

// Editor 构造编辑器视图（替代原 ui.Editor()）。
// 建树期先把视口高度对齐到当前布局（编辑区高 - 容器上下 padding 12），
// 拖动水平分隔条后 widgets.Editor 的 viewH 信号驱动编辑器伸缩填满编辑区。
func Editor() *gxscene.Instance {
    editorState.SetViewportHeight(768 - 32 - 6 - 26 - LayoutTerminalH() - 12)
    return widgets.Editor(editorState)
}

// Indices 生成 [0,n) 索引切片（<For> 逐行/逐项渲染用）。
func Indices(n int) []int {
    out := []int{}
    for i := 0; i < n; i = i + 1 { out = append(out, i) }
    return out
}

// ---- 运行态：Run 按钮解释源码后的预览实例与错误 ----
// 装配层（web wasm / desktop）调用 RunCurrent 解释当前源码，把结果存这里；
// 预览槽读 runInstance 决定渲染解释结果还是预编译 demo。
var runInstance *gxscene.Instance
var runErr = ""
var runStamp = 0     // 每次 Run 自增，驱动预览 getter 重算
 var RunningSig = Signal.New(false)
var runHandler func()  // 装配层注入的 Run 回调（解释源码 + 重渲染）

// ---- 主题切换（Apple 明暗双主题）----
// 装配层注入切换回调：内部 SetTheme + app.Render 重建整棵树换肤。
var themeHandler func(bool) // 参数 true=浅色
 func SetThemeHandler(fn func(bool)) { themeHandler = fn }
// SetLightTheme 由装配层在启动时同步 URL 指定的初始主题到 UI 状态。
 func SetLightTheme(v bool) { Signal.Batch(func() { LightTheme(v )}) }
 var LightTheme = Signal.New(false)
 func ToggleTheme() {
    next := !LightTheme()
    Signal.Batch(func() { LightTheme(next )})
    if themeHandler != nil { themeHandler(next) }
}

// SetRunHandler 由装配层注入 Run 动作。
 func SetRunHandler(fn func()) { runHandler = fn }
 func TriggerRun() {
    if runHandler != nil {
        terminalState.RunMark("Run")
        Signal.Batch(func() { RunningSig(true )})
        runHandler()
    }
}

// SetRunResult 由装配层在解释完成后调用；inst=nil 表示回落到预编译 demo。
 func SetRunResult(inst *gxscene.Instance, err string) {
    runInstance = inst
    runErr = err
    runStamp = runStamp + 1
    if err != "" {
        terminalState.Println("✗ " + err)
    } else {
        terminalState.Println("✓ 解释成功 · 场景树已加载到预览")
    }
    Signal.Batch(func() { RunningSig(false )})
}

// RunInfo 返回运行状态文本（给状态栏展示）。
 func RunInfo() string {
    if runErr != "" { return "运行错误: " + runErr }
    if runInstance != nil { return "已运行 · 解释执行当前源码" }
    return "预览模式 · 点击 Run 解释当前源码"
}

func RunInfoColor() string {
    if runErr != "" { return widgets.UseToken("danger")() }
    if runInstance != nil { return widgets.UseToken("success")() }
    return mutedColor()
}

func hasRunResult() bool { return runInstance != nil }
func currentRunErr() string { return runErr }
func currentRunStamp() int { return runStamp }

// ---- WebStorm 三区布局尺寸（信号驱动，装配层拖拽分隔条时写入）----
// 左 Project 宽、下 Terminal 高、终端开合。所有 setter 带边界 clamp，
// 保证拖拽时任一区域不会被拉到不可见。
 var LayoutProjectW = Signal.New(260)
 var LayoutTerminalH = Signal.New(180)
 var LayoutTerminalOpen = Signal.New(true)
 var LayoutSplitActive = Signal.New(false )// 分隔条拖拽进行中（高亮反馈）

 func SetLayoutProjectW(w int) {
    v := w
    if v < 160 { v = 160 }
    if v > 520 { v = 520 }
    Signal.Batch(func() { LayoutProjectW(v )})
}
 func SetLayoutTerminalH(h int) {
    v := h
    if v < 90 { v = 90 }
    if v > 420 { v = 420 }
    Signal.Batch(func() { LayoutTerminalH(v )})
}
 func ToggleTerminal() { Signal.Batch(func() { LayoutTerminalOpen(!LayoutTerminalOpen() )}) }
// SetSplitDragging 由装配层在分隔条按下/松开时切换高亮反馈。
 func SetSplitDragging(on bool) { Signal.Batch(func() { LayoutSplitActive(on )}) }

// ---- 多标签编辑器（WebStorm 式 editor tabs）----
// tabs/activeTabName 是响应面（tab 条 For 渲染）；文件内容缓存在普通 map，
// 切换时读回，关闭即清理，避免结构 sig 装进切片的引用语义坑。

var tabs = Signal.New([]string{})
var activeTabName = Signal.New("")
var tabSources = map[string]string{}
var tabPaths = map[string]string{}

func tabIndexOf(name string) int {
    for i, n := range tabs() {
        if n == name { return i }
    }
    return -1
}

 func TabNames() []string { return tabs() }
 func ActiveTabName() string { return activeTabName() }

func saveActiveTab() {
    if activeTabName() != "" {
        tabSources[activeTabName()] = EditorText()
    }
}

// OpenTab 打开（或激活已开的）标签。同名复用不重复追加。
 func OpenTab(name string, path string, source string) {
    if name == "" { return }
    if tabIndexOf(name) >= 0 {
        SwitchTab(name)
        return
    }
    saveActiveTab()
    tabSources[name] = source
    tabPaths[name] = path
    tabs(append(tabs(), name))
    activeTabName(name)
    Signal.Batch(func() {
        EditorSetText(source)
        EditorHome()
        EditorFocus()
    })
}

 func SwitchTab(name string) {
    if name == "" || name == activeTabName() { return }
    if tabIndexOf(name) < 0 { return }
    saveActiveTab()
    activeTabName(name)
    Signal.Batch(func() {
        EditorSetText(tabSources[name])
        EditorHome()
        EditorFocus()
    })
}

 func CloseTab(name string) {
    idx := tabIndexOf(name)
    if idx < 0 { return }
    wasActive := activeTabName() == name
    tabs(append(tabs()[:idx], tabs()[idx+1:]...))
    tabSources[name] = ""
    tabPaths[name] = ""
    if !wasActive { return }
    if len(tabs()) > 0 {
        pick := idx - 1
        if pick < 0 { pick = 0 }
        if pick >= len(tabs()) { pick = len(tabs()) - 1 }
        SwitchTab(tabs()[pick])
    } else {
        activeTabName("")
        EditorSetText("")
    }
}

 func CloseAllTabs() {
    for len(tabs()) > 0 {
        CloseTab(tabs()[0])
    }
}

// ---- 底部工具窗标签（Terminal / Run / Problems 真切换）----

var bottomTab = Signal.New("Terminal")

 func SetBottomTab(t string) { bottomTab(t )}
 func BottomTab() string { return bottomTab() }

// problemsText 是 Problems 页数据源：当前 Run 的真实错误，没有就报无问题。
func problemsText() string {
    if runErr != "" { return "✗ " + runErr }
    return "No problems detected"
}

// ---- 项目面板显隐（工具栏 folder 按钮的真动作）----

var projectPanelVisible = Signal.New(true)

 func SetProjectPanelVisible(v bool) { projectPanelVisible(v )}
 func ToggleProjectPanel() { projectPanelVisible(!projectPanelVisible() )}

// ---- 进程内终端（代码区下方，Run 输出落地于此）----
// 与 gox-term 独立 shell 不同：这是 IDEA 的控制台面板，展示解释/构建输出。
var terminalState = widgets.NewTerminal()

// TerminalOut 供装配层在 Run 前后写入输出行。
 func TerminalOut(s string) { terminalState.Println(s) }
 func TerminalClear() { terminalState.Clear() }

// 布局尺寸/分隔条状态的 getter（函数体内裸读 sig，交给 GX 生成响应式 getter）。
func projectWidthPx() string { return strconv.Itoa(LayoutProjectW()) }
func terminalHeightPx() string { return strconv.Itoa(LayoutTerminalH()) }
// editorAreaPx 让编辑器区高度随终端高度反向变化（主区 768 - 标签栏32 - 分隔条6 - 终端头26）。
// 显式高度而非 flexGrow，避免编辑器内置固定高度阻碍 flex 收缩导致水平分隔条拖不动。
func editorAreaPx() string { return strconv.Itoa(768 - 32 - 6 - 26 - LayoutTerminalH()) }
func splitActiveBg() string {
    if LayoutSplitActive() { return accentColor() }
    return borderColor()
}
func terminalLines() string { return strconv.Itoa(terminalState.LineCount()) + " 行" }
func terminalText() string  { return terminalState.Text() }

// 终端区可读性：10px footnote 加粗笔画在屏幕上会碎成点阵（"Terminal 像
// 有空格"的观感根源），tab 标签统一抬到 12px；输出区声明等宽栈，与控制台
// 语义一致。局部常量，不动全局 FontFootnote token（其他组件仍在用）。
const termTabSize = "12"
const termOutFont = "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace"

// TerminalView 用 JSX 自绘终端面板：height 走 GX 响应式 style 属性，
// 拖拽水平分隔条写 layoutTerminalH 即可实时改变高度（widgets.Terminal 内部
// height 是静态 attr，无法响应信号，故这里不用该组件）。
func TerminalView() *gxscene.Instance {
    // WebStorm 底部工具窗口：无圆角卡片、无 Mac 红黄绿灯，贴紧分隔条的细标签头。
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpDynChild,Slot:4,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:5,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpDynChild,Slot:6,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:7,},gxscene.Op{Code:gxscene.OpBranch,Slot:8,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:9,}, func() any{ return gxscene.Style{FlexDirection:"column",Gap:"0",Overflow:"hidden",} }, func() any{ return gxscene.Style{FlexDirection:"row",Gap:"16",AlignItems:"center",Padding:"4 12",Background:panelBg(),} }, func() any{ return bottomTabItem("Terminal") }, func() any{ return bottomTabItem("Run") }, func() any{ return bottomTabItem("Problems") }, func() any{ return gxscene.Style{FlexGrow:"1",} }, func() any{ return widgets.Text(gxscene.Props{"value": terminalLines(), "size": termTabSize, "tone": "muted"}) }, func() any{ return gxscene.Style{Background:canvasBg(),Padding:"6 12",Height:terminalHeightPx(),Overflow:"auto",FontSize:termTabSize,FontFamily:termOutFont,Color:widgets.UseToken("success")(),} }, gxscene.BranchGetter(func() int{if bottomTab() == "Problems" { return 0 }
return 1 }, func() *gxscene.Instance { return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"#frag",},gxscene.Op{Code:gxscene.OpDynChild,Slot:0,},gxscene.Op{Code:gxscene.OpClose,},},N:1,}, func() any{ return widgets.Text(gxscene.Props{"value": problemsText(), "size": termTabSize, "tone": "muted"}) }) }, func() *gxscene.Instance { return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"#frag",},gxscene.Op{Code:gxscene.OpDynChild,Slot:0,},gxscene.Op{Code:gxscene.OpClose,},},N:1,}, func() any{ return widgets.Text(gxscene.Props{"value": terminalText(), "size": termTabSize, "tone": "muted"}) }) }))
}

// bottomTabItem 底部工具窗标签：点击切换，激活态 accent 加粗。
func bottomTabItem(label string) *gxscene.Instance {
    act := bottomTab() == label
    tone := "muted"
    weight := "normal"
    if act {
        tone = accentColor()
        weight = "bold"
    }
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:0,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpClose,},},N:3,}, func() any{ return func() { SetBottomTab(label) } }, func() any{ return gxscene.Style{Padding:"2 6",Cursor:"pointer",BorderRadius:widgets.RadiusSmall,Hover:&gxscene.Style{Background:widgets.UseToken("panelalt")(),},} }, func() any{ return widgets.Text(gxscene.Props{"value": label, "size": termTabSize, "weight": weight, "tone": tone}) })
}

// ---- 示例库：元数据 + 源码文本（由 gen_samples.py 生成） ----

 type Sample struct {
     Name   string
     Group  string
     Tag    string
     Desc   string
     Source string
}

 var SampleIndex = Signal.New(0)

// 下面的 sample 列表由 gen_samples.py 从 samples/*.gx 生成。
// 构建前必须先通过 gox fmt -check、行数检查与 gox build -check 门禁，
// 全部通过才会写入。请勿手改。
func buildSamples() []Sample {
    return []Sample{
    // @@BEGIN-GENERATED-SAMPLES@@
        Sample{Name: "01 Hello GX", Group: "基础", Tag: "sig 裸读", Desc: "声明信号，JSX 里直接读，编译器自动补调用", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox/runtime/scene\"\n)\n\n// sig 声明 + 裸读（编译器自动补 ()）\nsig name = \"GX\"\nfunc view() *gxscene.Instance {\n    return <div>Hello, {name}!</div>\n}"},
        Sample{Name: "02 Counter", Group: "基础", Tag: "事件 / 样式", Desc: "事件传函数原值，样式收进 style 结构体", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox/runtime/scene\"\n)\n\n// 事件传函数原值 + sig 读写 + 样式进 style struct\nsig count = 0\nfunc inc() {\n    count = count + 1\n}\nfunc view() *gxscene.Instance {\n    return <button onClick={inc} style={{ background: \"#3b82f6\", padding: 10 }}>Count: {count}</button>\n}"},
        Sample{Name: "03 For 列表", Group: "基础", Tag: "For / getter", Desc: "keyed 列表，item 用 t() 读实时值", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox/runtime/scene\"\n)\n\n// For + item getter（t() 读值）+ 样式进 style struct\nsig todos = []string{\"a\", \"b\", \"c\"}\nfunc view() *gxscene.Instance {\n    return <div style={{ flexDirection: \"column\", gap: 6 }}>\n        <For each={todos}>{(t string) => <span>{t()}</span>}</For>\n    </div>\n}"},
        Sample{Name: "04 样式卡片", Group: "基础", Tag: "Style struct", Desc: "驼峰 key + hover 伪状态单词 key", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox/runtime/scene\"\n)\n\n// style={{...}} 强类型 struct + 驼峰 key + 伪状态单词 key\nfunc view() *gxscene.Instance {\n    return <div\n        style={{\n            background: \"#0b1020\",\n            padding: 20,\n            borderRadius: 12,\n            flexDirection: \"column\",\n            gap: 8,\n            hover: { background: \"#1e293b\" },\n        }}\n    >\n        <b style={{ color: \"#7dd3fc\", fontSize: 24 }}>Card</b>\n        <span style={{ color: \"#94a3b8\" }}>用 GX style struct 写的样式</span>\n    </div>\n}"},
        Sample{Name: "05 computed", Group: "响应式", Tag: "computed / batch", Desc: "派生信号 + 批量合并写入", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox/runtime/scene\"\n    Signal \"github.com/robertpanvip/gox/runtime/signal\"\n)\n\n// Signal.computed 派生 + Signal.batch 合并 + 事件传函数原值\nsig price = 10\nsig qty = 2\nvar total = Signal.computed(() => price * qty)\nfunc bump() {\n    Signal.batch(() => {\n        price = price + 1\n        qty = qty + 1\n    })\n}\nfunc view() *gxscene.Instance {\n    return <div style={{ flexDirection: \"column\", gap: 6 }}>\n        <span>Total: {total}</span>\n        <button onClick={bump}>加价加量</button>\n    </div>\n}"},
        Sample{Name: "06 条件分支", Group: "响应式", Tag: "if / else", Desc: "JSX 内条件编译成细粒度 Branch", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox/runtime/scene\"\n)\n\n// JSX 条件分支：if/else 编译成细粒度 Branch，条件变化只换分支子树\nsig on = true\nfunc toggle() { on = !on }\nfunc view() *gxscene.Instance {\n    return <div style={{ flexDirection: \"column\", gap: 8 }}>\n        <button onClick={toggle} style={{ background: \"#3b82f6\", padding: 8, borderRadius: 6 }}>\n            切换分支\n        </button>\n        <div style={{ padding: 16, borderRadius: 8 }}>\n            if on {\n                <b style={{ color: \"#4ec9b0\", fontSize: 18 }}>ON · 分支 A</b>\n            } else {\n                <b style={{ color: \"#f87171\", fontSize: 18 }}>OFF · 分支 B</b>\n            }\n        </div>\n    </div>\n}\n"},
        Sample{Name: "07 列表增删", Group: "响应式", Tag: "sig []T", Desc: "切片增删，For 自动增删对应节点", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox/runtime/scene\"\n)\n\n// sig 切片增删 + For 响应式列表 + item getter（t() 读值）\nsig items = []string{\"alpha\", \"beta\"}\nfunc add() { items = append(items, \"item\") }\nfunc clear() { items = []string{} }\nfunc view() *gxscene.Instance {\n    return <div style={{ flexDirection: \"column\", gap: 8 }}>\n        <div style={{ flexDirection: \"row\", gap: 8 }}>\n            <button onClick={add} style={{ padding: 8, borderRadius: 6 }}>添加</button>\n            <button onClick={clear} style={{ padding: 8, borderRadius: 6 }}>清空</button>\n        </div>\n        <For each={items}>\n            {(t string) => <div style={{ color: \"#9cdcfe\", padding: 4 }}>{t()}</div>}\n        </For>\n    </div>\n}\n"},
        Sample{Name: "08 枚举", Group: "语言特性", Tag: "enum", Desc: "类型 / 常量 / String / Parse，三种拼写", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox/runtime/scene\"\n)\n\n// enum 枚举：类型 + 常量 + String() + Parse<T>()，三种拼写指向同一符号\npub enum color string { red = \"#ef4444\", green = \"#22c55e\", blue = \"#3b82f6\" }\nsig idx = 0\nfunc next() { idx = (idx + 1) % 3 }\nfunc view() *gxscene.Instance {\n    cs := []color{colorRed, colorGreen, colorBlue}\n    return <div style={{ flexDirection: \"column\", gap: 8 }}>\n        <button onClick={next} style={{ padding: 8 }}>换色</button>\n        <div style={{ background: string(cs[idx]), padding: 20, borderRadius: 8, color: \"#fff\" }}>\n            {cs[idx]}\n        </div>\n    </div>\n}\n"},
        Sample{Name: "09 模板字符串", Group: "语言特性", Tag: "模板 / 默认值", Desc: "反引号插值 + 尾部参数默认值", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox/runtime/scene\"\n)\n\n// 模板字符串（反引号内插值）+ 参数默认值（尾部缺失参数自动补全）\nfunc greet(name string = \"GX\", mark string = \"!\") string {\n    return `你好, ${name}${mark}`\n}\nsig who = \"world\"\nfunc fullText() string { return greet(who) }\nfunc defaultText() string { return greet() }\nfunc change() { who = \"GX\" }\nfunc view() *gxscene.Instance {\n    return <div style={{ flexDirection: \"column\", gap: 8 }}>\n        <button onClick={change} style={{ padding: 8 }}>换名字</button>\n        <span style={{ color: \"#c3e88d\", fontSize: 16 }}>{fullText()}</span>\n        <span style={{ color: \"#858585\" }}>{defaultText()}</span>\n    </div>\n}\n"},
        Sample{Name: "10 对象解构", Group: "语言特性", Tag: "解构", Desc: "var { x, y } = p 逐字段取值", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox/runtime/scene\"\n)\n\n// 对象解构：{ x, y } := p 逐字段取值\ntype point struct { x int; y int }\nfunc coord() point { return point{x: 3, y: 4} }\nfunc view() *gxscene.Instance {\n    p := coord()\n    var { x, y } = p\n    return <div style={{ flexDirection: \"column\", gap: 6 }}>\n        <span style={{ color: \"#9cdcfe\" }}>x = {x}</span>\n        <span style={{ color: \"#c3e88d\" }}>y = {y}</span>\n        <span style={{ color: \"#858585\" }}>sum = {x + y}</span>\n    </div>\n}\n"},
        Sample{Name: "11 联合类型", Group: "语言特性", Tag: "union / is", Desc: "擦除为 any 的语法便利层 + 类型守卫", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox/runtime/scene\"\n    \"fmt\"\n)\n\n// 联合类型 int | string + is 守卫（擦除为 any 的语法便利层）\ntype value = int | string\nfunc show(v int | string) string {\n    if v is string { return fmt.sprint(\"str:\", v) }\n    return fmt.sprint(\"int:\", v)\n}\nfunc view() *gxscene.Instance {\n    return <div style={{ flexDirection: \"column\", gap: 6 }}>\n        <span style={{ color: \"#9cdcfe\" }}>{show(7)}</span>\n        <span style={{ color: \"#c3e88d\" }}>{show(\"gx\")}</span>\n    </div>\n}\n"},
        Sample{Name: "12 组件", Group: "语言特性", Tag: "props / sig 字段", Desc: "pub func 组件，props 的 sig 字段保鲜", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox/runtime/scene\"\n)\n\n// 组件：pub func 返回场景树，props 的 sig 字段保鲜（读到的是实时值）\ntype cardProps struct {\n    sig title string\n}\npub func card(p cardProps) *gxscene.Instance {\n    return <div style={{ background: \"#1e293b\", padding: 12, borderRadius: 8 }}>\n        <b style={{ color: \"#7dd3fc\" }}>{p.title}</b>\n    </div>\n}\nsig text = \"Hello\"\nfunc change() { text = \"Changed\" }\nfunc view() *gxscene.Instance {\n    return <div style={{ flexDirection: \"column\", gap: 8 }}>\n        <button onClick={change} style={{ padding: 8 }}>改标题</button>\n        <Card title={text}/>\n    </div>\n}\n"},
        Sample{Name: "13 事件签名", Group: "语言特性", Tag: "EventContext", Desc: "具名函数 / 箭头 / func 字面量 / 带 ctx", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox/runtime/scene\"\n)\n\n// 事件传函数原值：具名函数 / 箭头 / func 字面量 / 带 EventContext 签名\nsig last = \"none\"\nfunc named() { last = \"named\" }\nfunc onCtx(e gxscene.EventContext) { if e.Synthetic != nil { last = string(e.Synthetic.Type) } }\nfunc view() *gxscene.Instance {\n    return <div style={{ flexDirection: \"column\", gap: 6 }}>\n        <button onClick={named} style={{ padding: 8 }}>具名函数</button>\n        <button onClick={() => last = \"arrow\"} style={{ padding: 8 }}>箭头</button>\n        <button onClick={func() { last = \"literal\" }} style={{ padding: 8 }}>func 字面量</button>\n        <button onClick={onCtx} style={{ padding: 8 }}>带 ctx</button>\n        <span style={{ color: \"#c3e88d\" }}>last: {last}</span>\n    </div>\n}\n"},
        Sample{Name: "14 struct sig", Group: "语言特性", Tag: "字段响应式", Desc: "显式标注的字段才响应，构造后读实时值", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox/runtime/scene\"\n)\n\n// struct sig 字段：显式标注才响应，构造后读到的是实时值\ntype state struct {\n    sig count int\n}\nsig source = 0\nfunc bump() { source = source + 1 }\nfunc view() *gxscene.Instance {\n    s := state{count: source}\n    return <div style={{ flexDirection: \"column\", gap: 8 }}>\n        <button onClick={bump} style={{ padding: 8 }}>+1</button>\n        <span style={{ color: \"#9cdcfe\" }}>count: {s.count}</span>\n    </div>\n}\n"},
        Sample{Name: "15 集合链式", Group: "语言特性", Tag: "List[T]", Desc: "[]T 提升为 List，filter/map/reduce", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox/runtime/scene\"\n)\n\n// 集合链式：[]T 提升为 List[T]，filter/map/reduce 可换型\nsig nums = []int{1, 2, 3, 4, 5}\nfunc shuffle() { nums = []int{2, 4, 6, 8} }\nfunc view() *gxscene.Instance {\n    return <div style={{ flexDirection: \"column\", gap: 8 }}>\n        <button onClick={shuffle} style={{ padding: 8 }}>换数据</button>\n        <span>偶数个数: {len(nums.filter(func(n int) bool { return n % 2 == 0 }))}</span>\n    </div>\n}\n"},
    // @@END-GENERATED-SAMPLES@@
    }
}

var sampleStore []Sample = buildSamples()

 func Samples() []Sample { return sampleStore }

// loadSample 切换示例：高亮、源码、光标、焦点一次批量提交；预览区通过
// {demoPreview()} 的动态子节点插槽读到 sampleIndex 后自动重建并运行。
 func LoadSample(i int) {
    xs := Samples()
    if i < 0 { i = 0 }
    if i >= len(xs) { i = len(xs) - 1 }
    runInstance = nil
    runErr = ""
    Signal.Batch(func() {
        SampleIndex(i)
        RunningSig(false)
    })
    // 标签化：示例文件同样进 tab 条（磁盘模式点文件走 onProjectRowClick）。
    OpenTab(xs[i].Name + ".gx", "", xs[i].Source)
}

 func NextSample() { LoadSample(SampleIndex() + 1) }
 func PrevSample() { LoadSample(SampleIndex() - 1) }

// demoPreview 是预览槽：优先展示 Run 解释出的实例（runInstance），
// 否则回落到当前示例的预编译运行版。装配层在 Run / 切换示例时会调用
// app.Render 重建整棵树，因此这里返回的新实例会被正确挂载（不受
// OpDynChild 常驻语义影响——那是同一棵树内多次求值才需担心）。
func demoPreview() *gxscene.Instance {
    if runInstance != nil { return runInstance }
    return demos.View(SampleIndex())
}

// ---- Apple 设计系统接线（widgets token + 组件库）----
// 所有配色/阴影/圆角/线宽来自 widgets 的 Apple 明暗双主题 token，
// 主题切换后由装配层 app.Render 重建场景树即可整体换肤（与 showcase 同源）。
func RadiusPanelLocal() string { return widgets.RadiusPanel }
func appBg() string       { return widgets.UseToken("grouped")() }
func panelBg() string     { return widgets.UseToken("panel")() }
func canvasBg() string    { return widgets.UseToken("bg")() }
func borderColor() string { return widgets.UseToken("hairline")() }
func textColor() string   { return widgets.UseToken("text")() }
func mutedColor() string  { return widgets.UseToken("muted")() }
func accentColor() string { return widgets.UseToken("accent")() }

// 分组语义色走 Apple system color token。
func groupColor(g string) string {
    if g == "基础" { return widgets.UseToken("blue")() }
    if g == "响应式" { return widgets.UseToken("green")() }
    if g == "语言特性" { return widgets.UseToken("purple")() }
    return mutedColor()
}

// 阴影统一用 widgets 的 Apple HIG token（三端算法已对齐）。
func cardShadow() string { return widgets.ShadowCard }
func paneShadow() string { return widgets.ShadowFloat }

// 选中示例：Apple 强调色底 + 白字；未选中用控件底 + 主文字。
func sampleRowBg(i int) string {
    if i == SampleIndex() { return accentColor() }
    return widgets.UseToken("panelalt")()
}
func sampleRowFg(i int) string {
    if i == SampleIndex() { return widgets.UseToken("onaccent")() }
    return textColor()
}

func sampleRow(i int) *gxscene.Instance {
    xs := Samples()
    if i >= len(xs) { return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpClose,},},N:0,}) }
    s := xs[i]
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:3,},gxscene.Op{Code:gxscene.OpDynChild,Slot:4,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:5,}, func() any{ return gxscene.Style{Background:sampleRowBg(i),Padding:"8 10",BorderRadius:widgets.RadiusSmall,BorderWidth:widgets.HairlineWidth,Border:borderColor(),FlexDirection:"column",Gap:"3",Cursor:"pointer",Transition:"background .15s",} }, func() any{ return func() { LoadSample(i) } }, func() any{ return widgets.Text(gxscene.Props{"value": s.Name, "size": widgets.FontBody, "weight": "bold", "tone": sampleRowFg(i)}) }, func() any{ return gxscene.Style{Color:groupColor(s.Group),FontSize:widgets.FontFootnote,} }, func() any{ return s.Group + " · " + s.Tag })
}

func sampleList() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpFor,Slot:1,},gxscene.Op{Code:gxscene.OpClose,},},N:2,}, func() any{ return gxscene.Style{FlexDirection:"column",Gap:"4",Height:"500",Overflow:"auto",} }, gxscene.ForGetterOfSignal[int](func() []int { return Indices(len(Samples())) },nil,func(i func() int) *gxscene.Instance { return sampleRow(i()) }))
}

// ---- WebStorm 风格 Project 面板（GX + 内置 SVG 图标，非 emoji）----
//
// 把示例库的 group 视作虚拟目录、每个示例视作 .gx 文件，用 widgets.Icon 渲染
// runtime/icon 内置的 folder / doc / chevron 矢量图标。目录可展开折叠、行可
// 点击选中、文件点击后加载进编辑器；桌面与网页同源，全程零 emoji / 零位图。

 type ProjectNode struct {
     Label    string  // 文件名 / 目录名
     Path     string  // 稳定唯一键："dir:<group>" / "file:<index>" / 真实文件绝对路径
     IsDir    bool
     Sample   int     // 文件对应的示例索引；目录或真实文件为 -1
     Depth    int     // 缩进层级（1=分组目录，2=文件）
     Expanded bool    // 目录当前是否展开（仅 isDir 有效）
     Parent   string  // 所属目录 key（用于可见性判定；示例=分组名，真实=父目录绝对路径）
}

// 展开的 key 集合（响应式）。示例模式 key=分组名；真实模式 key=目录绝对路径。
// 默认全展开示例分组，首次打开即见完整项目树。
 var ProjectExpanded = Signal.New([]string{"基础", "响应式", "语言特性"})
// 当前选中的目录 path；文件选中由 sampleIndex 派生，始终与编辑器同步。
 var ProjectDirSelected = Signal.New("")
// 当前打开的真实文件路径（真实模式）；示例模式下为空。
 var ProjectFileSelected = Signal.New("")

// nodeExpandKey 返回节点的展开/折叠 key：示例模式用分组名（兼容既有语义），
// 真实模式用绝对路径（唯一，避免同名目录冲突）。
func nodeExpandKey(n ProjectNode) string {
    if projectRoot != "" { return n.Path }
    return n.Label
}
// 真实磁盘项目根；为空时用示例库虚拟树，非空时扫描该目录。
var projectRoot = ""
// 真实文件读取回调：由装配层注入，返回文件内容（GX 侧不直接碰 os）。
var projectFileReader func(string) string = nil

// SetProjectRoot 由装配层在启动期设置真实项目根并注入文件读取回调；
// 同时把扫描出的所有目录加入 projectExpanded，首次打开即见完整真实目录树。
 func SetProjectRoot(root string, reader func(string) string) {
    projectRoot = root
    projectFileReader = reader
    if root == "" {
        Signal.Batch(func() {
            ProjectExpanded(projectGroups())
            ProjectDirSelected("")
        })
        return
    }
    keys := []string{}
    nodes := scanDiskNodes(root)
    for i := 0; i < len(nodes); i = i + 1 {
        if nodes[i].IsDir { keys = append(keys, nodes[i].Path) }
    }
    Signal.Batch(func() {
        ProjectExpanded(keys)
        ProjectDirSelected("")
    })
}

// ProjectRoot 返回当前项目根（供装配层/测试查询）。
 func ProjectRoot() string { return projectRoot }

func containsStr(xs []string, s string) bool {
    for i := 0; i < len(xs); i = i + 1 {
        if xs[i] == s { return true }
    }
    return false
}

// sampleFile 返回示例在项目树中显示的文件名（与 ui/samples/*.gx 一致）。
func sampleFile(i int) string {
    names := []string{
        "01_hello.gx", "02_counter.gx", "03_for.gx", "04_style.gx", "05_computed.gx",
        "06_branch.gx", "07_list_ops.gx", "08_enum.gx", "09_template.gx", "10_destruct.gx",
        "11_union.gx", "12_component.gx", "13_eventctx.gx", "14_structsig.gx", "15_collections.gx",
    }
    if i >= 0 && i < len(names) { return names[i] }
    return "sample.gx"
}

// projectGroups 按示例首次出现顺序返回分组名（虚拟目录）。
func projectGroups() []string {
    out := []string{}
    xs := Samples()
    for i := 0; i < len(xs); i = i + 1 {
        g := xs[i].Group
        if !containsStr(out, g) { out = append(out, g) }
    }
    return out
}

// projectScrollTop Project 树容器的受控纵向滚动偏移（scroll-top），由
// locateCurrentFile 写入，把当前打开文件的行滚进视口。
var projectScrollTop = Signal.New(0)

// projectRowPitch 单个可见行的占位高度：内容 15（图标）+ 上下 padding 3×2
// + marginBottom 2，与 projectRow 的样式约定一致。
func projectRowPitch() int { return 25 }

// locateCurrentFile 定位当前打开的文件（WebStorm「Locate」语义）：
// 展开目标所在分组/父目录链，并把目标行滚动到树视口上部。
// 没有打开的文件时不动作。
func locateCurrentFile() {
    nodes := projectAllNodes()
    target := -1
    if projectRoot != "" {
        if ProjectFileSelected() == "" { return }
        for i := 0; i < len(nodes); i = i + 1 {
            if !nodes[i].IsDir && nodes[i].Path == ProjectFileSelected() { target = i }
        }
    } else {
        for i := 0; i < len(nodes); i = i + 1 {
            if nodes[i].Sample == SampleIndex() { target = i }
        }
    }
    if target < 0 { return }
    Signal.Batch(func() {
        // 展开目标的祖先目录：示例模式是单层分组；磁盘模式沿绝对路径逐级向上。
        keys := []string{}
        for i := 0; i < len(ProjectExpanded()); i = i + 1 { keys = append(keys, ProjectExpanded()[i]) }
        if projectRoot != "" {
            p := nodes[target].Parent
            for p != "" && p != projectRoot {
                if !containsStr(keys, p) { keys = append(keys, p) }
                p = filepath.Dir(p)
            }
            if !containsStr(keys, projectRoot) { keys = append(keys, projectRoot) }
        } else {
            g := nodes[target].Parent
            if !containsStr(keys, g) { keys = append(keys, g) }
        }
        ProjectExpanded(keys)
        // 目标行在新展开态下的可见序号（目录恒可见；文件看父目录是否展开）。
        idx := 0
        for i := 0; i < target; i = i + 1 {
            if nodes[i].IsDir || containsStr(keys, nodes[i].Parent) { idx = idx + 1 }
        }
        v := idx * projectRowPitch() - 50
        if v < 0 { v = 0 }
        projectScrollTop(v)
    })
}

func expandAllGroups() {
    if projectRoot != "" {
        keys := []string{}
        nodes := projectAllNodes()
        for i := 0; i < len(nodes); i = i + 1 {
            if nodes[i].IsDir { keys = append(keys, nodes[i].Path) }
        }
        Signal.Batch(func() { ProjectExpanded(keys )})
        return
    }
    Signal.Batch(func() { ProjectExpanded(projectGroups() )})
}
func collapseAllGroups() {
    Signal.Batch(func() { ProjectExpanded([]string{} )})
}
func toggleGroup(g string) {
    cur := ProjectExpanded()
    out := []string{}
    hit := false
    for i := 0; i < len(cur); i = i + 1 {
        if cur[i] == g { hit = true } else { out = append(out, cur[i]) }
    }
    if !hit { out = append(out, g) }
    Signal.Batch(func() { ProjectExpanded(out )})
}

// projectRows 把项目树展平为「当前可见」行（响应式：读 projectExpanded）。
// 供逻辑测试与需要可见集合的调用方使用；面板本身用 projectAllNodes 固定行 +
// 叶子可见性，避免结构型 <For> 变化在增量帧后端漏变更（见 projectPanel 注释）。
func projectRows() []ProjectNode {
    out := []ProjectNode{}
    all := projectAllNodes()
    for i := 0; i < len(all); i = i + 1 {
        n := all[i]
        if n.IsDir { n.Expanded = containsStr(ProjectExpanded(), nodeExpandKey(n)) }
        if projectNodeVisible(n) { out = append(out, n) }
    }
    return out
}

// projectAllNodes 返回完整且顺序稳定的行集合（目录 + 其下文件），不读任何 signal。
// 真实模式（projectRoot 非空）扫描磁盘目录；示例模式用内置示例库虚拟树。
func projectAllNodes() []ProjectNode {
    if projectRoot != "" { return scanDiskNodes(projectRoot) }
    out := []ProjectNode{}
    groups := projectGroups()
    xs := Samples()
    for gi := 0; gi < len(groups); gi = gi + 1 {
        g := groups[gi]
        out = append(out, ProjectNode{Label: g, Path: "dir:" + g, IsDir: true, Sample: -1, Depth: 1})
        for i := 0; i < len(xs); i = i + 1 {
            if xs[i].Group == g {
                out = append(out, ProjectNode{Label: sampleFile(i), Path: "file:" + strconv.Itoa(i), Sample: i, Depth: 2, Parent: g})
            }
        }
    }
    return out
}

// scanDiskNodes 递归扫描真实目录，生成顺序稳定的节点集合。
// 目录优先、按名排序；跳过隐藏文件与 node_modules/vendor/.git 等噪声目录；
// 深度限制防止符号链接环路与超大树拖慢首帧。
func scanDiskNodes(root string) []ProjectNode {
    out := []ProjectNode{}
    const maxDepth = 6
    var walk func(dir string, lvl int)
    walk = func(dir string, lvl int) {
        entries, err := os.ReadDir(dir)
        if err != nil { return }
        // os.ReadDir 已按文件名排序；目录优先。
        for pass := 0; pass < 2; pass = pass + 1 {
            for i := 0; i < len(entries); i = i + 1 {
                e := entries[i]
                name := e.Name()
                if (pass == 0) != e.IsDir() { continue }
                if len(name) > 0 && name[0] == '.' { continue }
                if name == "node_modules" || name == "vendor" || name == ".git" { continue }
                full := filepath.Join(dir, name)
                kindDir := e.IsDir()
                out = append(out, ProjectNode{Label: name, Path: full, IsDir: kindDir, Sample: -1, Depth: lvl, Parent: dir})
                if kindDir && lvl < maxDepth { walk(full, lvl+1) }
            }
        }
    }
    walk(root, 1)
    return out
}

func groupOfSample(i int) string {
    xs := Samples()
    if i >= 0 && i < len(xs) { return xs[i].Group }
    return ""
}

// projectNodeVisible 依据所属目录是否展开决定文件行是否可见（叶子响应式读取）。
func projectNodeVisible(n ProjectNode) bool {
    if !n.IsDir {
        if projectRoot != "" { return containsStr(ProjectExpanded(), n.Parent) }
        return containsStr(ProjectExpanded(), groupOfSample(n.Sample))
    }
    return true
}

// onProjectRowClick 目录切换展开并选中；文件加载进编辑器。
// 示例模式文件走 loadSample；真实模式文件读磁盘内容灌入编辑器。
func onProjectRowClick(n ProjectNode) {
    if n.IsDir {
        Signal.Batch(func() {
            ProjectDirSelected(n.Path)
            toggleGroup(nodeExpandKey(n))
        })
        return
    }
    Signal.Batch(func() {
        ProjectDirSelected("")
        if projectRoot != "" {
            content := ""
            if projectFileReader != nil {
                content = projectFileReader(n.Path)
            }
            ProjectFileSelected(n.Path)
            OpenTab(n.Label, n.Path, content)
        } else {
            ProjectFileSelected("")
            LoadSample(n.Sample)
        }
    })
}

func projectRowBg(n ProjectNode) string {
    if n.IsDir {
        if ProjectDirSelected() == n.Path { return widgets.UseToken("panelalt")() }
        return panelBg()
    }
    if projectRoot != "" {
        if ProjectFileSelected() == n.Path { return accentColor() }
        return panelBg()
    }
    if n.Sample == SampleIndex() { return accentColor() }
    return panelBg()
}
func projectRowFg(n ProjectNode) string {
    if !n.IsDir {
        if projectRoot != "" { return textColor() }
        if n.Sample == SampleIndex() { return widgets.UseToken("onaccent")() }
    }
    return textColor()
}

// projectChevron 目录显示展开箭头，文件用等宽占位保持名称对齐。
// 直接读 projectExpanded（叶子响应），切换箭头方向走叶子变更，增量帧可见。
// 两个箭头都静态建好，用 style 的 absolute+opacity 切换显隐——与行可见性同理，
// 走 dynAttr 叶子 effect，增量帧可见（dynChild 会被判为稳定子树而不再重算）。
func projectChevronPos(n ProjectNode, showOpen bool) string {
    if containsStr(ProjectExpanded(), nodeExpandKey(n)) == showOpen { return "relative" }
    return "absolute"
}
func projectChevronOpacity(n ProjectNode, showOpen bool) string {
    if containsStr(ProjectExpanded(), nodeExpandKey(n)) == showOpen { return "1" }
    return "0"
}

func projectChevronSlot(n ProjectNode, name string, showOpen bool) *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpClose,},},N:2,}, func() any{ return gxscene.Style{FlexDirection:"row",Position:projectChevronPos(n, showOpen),Top:"-9999",Opacity:projectChevronOpacity(n, showOpen),} }, func() any{ return widgets.Icon(gxscene.Props{"name": name, "size": 12, "color": mutedColor()}) })
}

func projectChevron(n ProjectNode) *gxscene.Instance {
    if !n.IsDir { return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpClose,},},N:1,}, func() any{ return gxscene.Style{Width:"12",} }) }
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpClose,},},N:3,}, func() any{ return gxscene.Style{FlexDirection:"row",Width:"12",Height:"14",} }, func() any{ return projectChevronSlot(n, "chevron-down", true) }, func() any{ return projectChevronSlot(n, "chevron-right", false) })
}

func projectRowIcon(n ProjectNode) *gxscene.Instance {
    if n.IsDir {
        return widgets.Icon(gxscene.Props{"name": "folder", "size": 15, "tone": "yellow"})
    }
    // 选中行背景是 accent 蓝，doc 图标同样用 onaccent 提亮，避免蓝图标配蓝底隐形。
    if projectRoot == "" && n.Sample == SampleIndex() {
        return widgets.Icon(gxscene.Props{"name": "doc", "size": 15, "tone": "onaccent"})
    }
    return widgets.Icon(gxscene.Props{"name": "doc", "size": 15, "tone": "blue"})
}

// 可见性用 style 叶子属性（position/opacity/padding/margin）表达，避免结构型
// <For> 增删节点在增量帧后端不产生 Change 记录而不重绘（桌面实测）。
// 注意 Skia 布局忽略 height=0（if s.height > 0 才 SetHeight），故用 absolute 脱离流。
func projectRowPadding(n ProjectNode) string {
    if !projectNodeVisible(n) { return "0" }
    return "3 8"
}
func projectRowMargin(n ProjectNode) string {
    if !projectNodeVisible(n) { return "0" }
    return "2"
}
func projectRowText(n ProjectNode) *gxscene.Instance {
    return widgets.Text(gxscene.Props{"value": n.Label, "size": widgets.FontBody, "tone": projectRowFg(n)})
}

// Skia 忽略 height=0，但 position:absolute 能让节点脱离父 flex 流（等价于隐藏）。
// 二者都走 style dynAttr（叶子 effect），能正确产生 Change 触发增量帧重绘。
func projectRowPosition(n ProjectNode) string {
    if !projectNodeVisible(n) { return "absolute" }
    return "relative"
}
func projectRowOpacity(n ProjectNode) string {
    if !projectNodeVisible(n) { return "0" }
    return "1"
}

func projectRow(n ProjectNode) *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpDynChild,Slot:4,},gxscene.Op{Code:gxscene.OpClose,},},N:5,}, func() any{ return gxscene.Style{Background:projectRowBg(n),Position:projectRowPosition(n),Top:"-9999",Opacity:projectRowOpacity(n),Padding:projectRowPadding(n),MarginBottom:projectRowMargin(n),MarginLeft:strconv.Itoa(n.Depth * 12),BorderRadius:widgets.RadiusSmall,FlexDirection:"row",AlignItems:"center",Gap:"6",Cursor:"pointer",Overflow:"hidden",Hover:&gxscene.Style{Background:widgets.UseToken("panelalt")(),},} }, func() any{ return func() { onProjectRowClick(n) } }, func() any{ return projectChevron(n) }, func() any{ return projectRowIcon(n) }, func() any{ return projectRowText(n) })
}

// projectPanel WebStorm 风格左侧项目面板：标题 + 展开/折叠全部 + 文件树。
func projectPanel() *gxscene.Instance {
    // 固定行集合：目录/文件节点一旦建好就不增删，展开折叠只改叶子属性，
    // 保证增量帧后端收到 Change 并正确重绘（见 projectRowHeight 注释）。
    nodes := projectAllNodes()
    cnt := 0
    for i := 0; i < len(nodes); i = i + 1 {
        if !nodes[i].IsDir { cnt = cnt + 1 }
    }
    // 外层固定壳：标题 + 工具按钮不随定位滚动；列表单独滚动（flex-grow 撑满
    // 剩余高度，minHeight:0 允许被压缩——否则内容高度会把列表撑出壳外）。
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpDynChild,Slot:4,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:5,},gxscene.Op{Code:gxscene.OpElem,Arg:"button",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:6,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:7,},gxscene.Op{Code:gxscene.OpDynChild,Slot:8,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"button",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:9,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:10,},gxscene.Op{Code:gxscene.OpDynChild,Slot:11,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"button",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:12,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:13,},gxscene.Op{Code:gxscene.OpDynChild,Slot:14,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:15,},gxscene.Op{Code:gxscene.OpFor,Slot:16,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:17,}, func() any{ return gxscene.Style{FlexDirection:"column",Gap:"0",Height:"500",} }, func() any{ return gxscene.Style{FlexDirection:"row",AlignItems:"center",Gap:"6",MarginBottom:"6",} }, func() any{ return widgets.Icon(gxscene.Props{"name": "folder", "size": 14, "color": accentColor()}) }, func() any{ return widgets.Text(gxscene.Props{"value": "PROJECT", "size": widgets.FontFootnote, "weight": "bold", "tone": "muted"}) }, func() any{ return widgets.Text(gxscene.Props{"value": "· " + strconv.Itoa(cnt) + " files", "size": widgets.FontFootnote, "tone": "muted"}) }, func() any{ return gxscene.Style{FlexDirection:"row",Gap:"4",Padding:"2 0",MarginBottom:"8",} }, func() any{ return expandAllGroups }, func() any{ return gxscene.Style{Padding:"3 8",BorderRadius:widgets.RadiusSmall,Cursor:"pointer",Background:widgets.UseToken("panelalt")(),BorderWidth:widgets.HairlineWidth,Border:borderColor(),} }, func() any{ return widgets.Icon(gxscene.Props{"name": "expand-all", "size": 12, "tone": "muted"}) }, func() any{ return collapseAllGroups }, func() any{ return gxscene.Style{Padding:"3 8",BorderRadius:widgets.RadiusSmall,Cursor:"pointer",Background:widgets.UseToken("panelalt")(),BorderWidth:widgets.HairlineWidth,Border:borderColor(),} }, func() any{ return widgets.Icon(gxscene.Props{"name": "collapse-all", "size": 12, "tone": "muted"}) }, func() any{ return locateCurrentFile }, func() any{ return gxscene.Style{Padding:"3 8",BorderRadius:widgets.RadiusSmall,Cursor:"pointer",Background:widgets.UseToken("panelalt")(),BorderWidth:widgets.HairlineWidth,Border:borderColor(),} }, func() any{ return widgets.Icon(gxscene.Props{"name": "locate", "size": 12, "tone": "muted"}) }, func() any{ return gxscene.Style{FlexGrow:"1",MinHeight:"0",Overflow:"auto",ScrollTop:strconv.Itoa(projectScrollTop()),} }, gxscene.ForGetterOfSignal[int](func() []int { return Indices(len(nodes)) },nil,func(i func() int) *gxscene.Instance { return projectRow(nodes[i()]) }))
}

// ---- WebStorm 风格 IDE 外壳（菜单栏 / 工具栏 / 编辑器标签 / 状态栏）----
// 与 playground 彻底分开：没有营销标题、示例库浏览、上一个/下一个大按钮与圆角卡片。
// 外壳全部贴边、细分隔线、紧凑控件，还原真实 JetBrains IDE 的观感。

// chromeBg 是 IDE 外壳（菜单栏/工具栏/标签栏/状态栏）底色。
// 不用 "grouped"（暗色下为纯黑，过于生硬），改用 panel 让外壳呈 JetBrains 风格的深灰。
func chromeBg() string { return widgets.UseToken("panel")() }

func ideNoop() {}

func menuItem(label string) *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpClose,},},N:2,}, func() any{ return gxscene.Style{Cursor:"pointer",Padding:"3 3",} }, func() any{ return widgets.Text(gxscene.Props{"value": label, "size": widgets.FontFootnote, "tone": "muted"}) })
}

// ---- 真下拉菜单（widgets.Popover 浮层底座；openMenu 受控，同时只开一个）----
// WebStorm 的菜单栏是可点下拉；这里把 File/View/Run 三项接成真菜单，
// 其余项保持静态文本（不造假交互）。菜单项只绑现有真实命令。

 var OpenMenu = Signal.New("")

// SetMenu 切换菜单：点同一个关闭，点另一个自然互斥（单一状态变量）。
 func SetMenu(name string) {
    if OpenMenu() == name { OpenMenu("" )} else { OpenMenu(name )}
}
 func MenuName() string { return OpenMenu() }

// NewFile 把编辑器重置为最小可运行骨架（File ▸ New File 的动作）。
 func NewFile() {
    runInstance = nil
    runErr = ""
    OpenTab("untitled.gx", "", "package main\n\nfunc main() {\n    println(\"hello, gox\")\n}\n")
}

// menuRow 菜单项：点击执行命令并收起菜单。
func menuRow(label string, fn func()) *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:0,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpClose,},},N:3,}, func() any{ return func() { fn(); OpenMenu("" )} }, func() any{ return gxscene.Style{FlexDirection:"row",AlignItems:"center",Height:"30",Padding:"0 14",Cursor:"pointer",Hover:&gxscene.Style{Background:widgets.UseToken("panelalt")(),},} }, func() any{ return widgets.Text(gxscene.Props{"value": label, "size": "12", "tone": "text"}) })
}

func menuBox(inner func() any) *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:0,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpClose,},},N:3,}, func() any{ return func(e gxscene.EventContext) { if e.Synthetic != nil { e.Synthetic.StopPropagation() } } }, func() any{ return gxscene.Style{FlexDirection:"column",Background:widgets.UseToken("elevated")(),BorderRadius:widgets.RadiusSmall,BorderWidth:widgets.HairlineWidth,Border:borderColor(),Padding:"4 0",} }, func() any{ return inner() })
}

// menuPopup trigger 与浮层内容组装成受控 Popover（外部点击由 scrim 关闭）。
func menuPopup(label string, name string, content func() *gxscene.Instance) *gxscene.Instance {
    return widgets.Popover(gxscene.Props{
        "open":         func() bool { return OpenMenu() == name },
        "onOpenChange": func(v bool) { if v { OpenMenu(name )} else { OpenMenu("" )} },
        "placement":    "bottom-start",
        "offset":       "2",
        "noScrim":      false,
        "width":        "200",
        "trigger":      func() any { return menuTrigger(label, name) },
        "content":      func() any { return content() },
    })
}

func menuTrigger(label string, name string) *gxscene.Instance {
    tone := "muted"
    if OpenMenu() == name { tone = "text" }
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:0,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpClose,},},N:3,}, func() any{ return func(e gxscene.EventContext) { if e.Synthetic != nil { e.Synthetic.StopPropagation() }; SetMenu(name) } }, func() any{ return gxscene.Style{Padding:"3 8",Cursor:"pointer",BorderRadius:widgets.RadiusSmall,Background:widgets.UseToken("panelalt")(),Hover:&gxscene.Style{Background:widgets.UseToken("panelalt")(),},} }, func() any{ return widgets.Text(gxscene.Props{"value": label, "size": widgets.FontFootnote, "tone": tone}) })
}

func editMenuContent() *gxscene.Instance {
    return menuBox(func() any {
        return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpClose,},},N:4,}, func() any{ return gxscene.Style{FlexDirection:"column",} }, func() any{ return menuRow("Undo", EditorUndo) }, func() any{ return menuRow("Redo", EditorRedo) }, func() any{ return menuRow("Select All", EditorSelectAll) })
    })
}

func fileMenuContent() *gxscene.Instance {
    return menuBox(func() any {
        return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpClose,},},N:4,}, func() any{ return gxscene.Style{FlexDirection:"column",} }, func() any{ return menuRow("New File", NewFile) }, func() any{ return menuRow("Next Example", NextSample) }, func() any{ return menuRow("Previous Example", PrevSample) })
    })
}

func viewMenuContent() *gxscene.Instance {
    return menuBox(func() any {
        return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpClose,},},N:3,}, func() any{ return gxscene.Style{FlexDirection:"column",} }, func() any{ return menuRow("Toggle Terminal Panel", ToggleTerminal) }, func() any{ return menuRow("Switch Theme", ToggleTheme) })
    })
}

func runMenuContent() *gxscene.Instance {
    return menuBox(func() any {
        return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpClose,},},N:3,}, func() any{ return gxscene.Style{FlexDirection:"column",} }, func() any{ return menuRow("Run Current Example", TriggerRun) }, func() any{ return menuRow("Clear Console", TerminalClear) })
    })
}

func menuBar() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpDynChild,Slot:4,},gxscene.Op{Code:gxscene.OpDynChild,Slot:5,},gxscene.Op{Code:gxscene.OpDynChild,Slot:6,},gxscene.Op{Code:gxscene.OpDynChild,Slot:7,},gxscene.Op{Code:gxscene.OpDynChild,Slot:8,},gxscene.Op{Code:gxscene.OpDynChild,Slot:9,},gxscene.Op{Code:gxscene.OpDynChild,Slot:10,},gxscene.Op{Code:gxscene.OpDynChild,Slot:11,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:12,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpDynChild,Slot:13,},gxscene.Op{Code:gxscene.OpClose,},},N:14,}, func() any{ return gxscene.Style{FlexDirection:"row",AlignItems:"center",Gap:"14",Height:"30",Padding:"0 12",Background:chromeBg(),BorderWidth:widgets.HairlineWidth,Border:borderColor(),} }, func() any{ return menuPopup("File", "File", fileMenuContent) }, func() any{ return menuPopup("Edit", "Edit", editMenuContent) }, func() any{ return menuPopup("View", "View", viewMenuContent) }, func() any{ return menuItem("Navigate") }, func() any{ return menuItem("Code") }, func() any{ return menuItem("Refactor") }, func() any{ return menuPopup("Run", "Run", runMenuContent) }, func() any{ return menuItem("Tools") }, func() any{ return menuItem("Git") }, func() any{ return menuItem("Window") }, func() any{ return menuItem("Help") }, func() any{ return gxscene.Style{FlexGrow:"1",} }, func() any{ return widgets.Text(gxscene.Props{"value": "GoX IDE", "size": widgets.FontFootnote, "tone": "muted"}) })
}

func iconButton(name string, fn func()) *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:0,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"testid",Slot:1,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:2,},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpClose,},},N:4,}, func() any{ return func() { fn() } }, func() any{ return "btn-" + name }, func() any{ return gxscene.Style{Width:"28",Height:"26",AlignItems:"center",JustifyContent:"center",BorderRadius:widgets.RadiusSmall,Cursor:"pointer",Hover:&gxscene.Style{Background:widgets.UseToken("panelalt")(),},} }, func() any{ return widgets.Icon(gxscene.Props{"name": name, "size": 16, "tone": "muted"}) })
}

func runButton() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:0,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpClose,},},N:4,}, func() any{ return func() { TriggerRun() } }, func() any{ return gxscene.Style{FlexDirection:"row",AlignItems:"center",Gap:"6",Height:"26",Padding:"0 12",BorderRadius:widgets.RadiusSmall,Background:accentColor(),Cursor:"pointer",} }, func() any{ return widgets.Icon(gxscene.Props{"name": "play", "size": 14, "color": widgets.UseToken("onaccent")()}) }, func() any{ return widgets.Text(gxscene.Props{"value": "Run", "size": widgets.FontFootnote, "weight": "bold", "tone": widgets.UseToken("onaccent")()}) })
}

func currentFileName() string {
    if projectRoot != "" && ProjectFileSelected() != "" { return filepath.Base(ProjectFileSelected()) }
    return sampleFile(SampleIndex())
}

func breadcrumbText() string {
    if projectRoot != "" {
        name := filepath.Base(projectRoot)
        if ProjectFileSelected() != "" {
            rel, err := filepath.Rel(projectRoot, ProjectFileSelected())
            if err == nil { return name + "  ›  " + filepath.ToSlash(rel) }
            return name + "  ›  " + filepath.Base(ProjectFileSelected())
        }
        return name
    }
    return "gox-idea  ›  " + groupOfSample(SampleIndex()) + "  ›  " + sampleFile(SampleIndex())
}

func ideToolbar() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:4,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpDynChild,Slot:5,},gxscene.Op{Code:gxscene.OpDynChild,Slot:6,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:7,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpDynChild,Slot:8,},gxscene.Op{Code:gxscene.OpDynChild,Slot:9,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:10,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpDynChild,Slot:11,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:12,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpDynChild,Slot:13,},gxscene.Op{Code:gxscene.OpDynChild,Slot:14,},gxscene.Op{Code:gxscene.OpClose,},},N:15,}, func() any{ return gxscene.Style{FlexDirection:"row",AlignItems:"center",Gap:"6",Height:"36",Padding:"0 10",Background:chromeBg(),BorderWidth:widgets.HairlineWidth,Border:borderColor(),} }, func() any{ return iconButton("folder", ToggleProjectPanel) }, func() any{ return iconButton("doc", NewFile) }, func() any{ return iconButton("search", ideNoop) }, func() any{ return gxscene.Style{Width:"1",Height:"18",Background:borderColor(),} }, func() any{ return iconButton("chevron-left", PrevSample) }, func() any{ return iconButton("chevron-right", NextSample) }, func() any{ return gxscene.Style{Width:"1",Height:"18",Background:borderColor(),} }, func() any{ return runButton() }, func() any{ return iconButton("refresh", TriggerRun) }, func() any{ return gxscene.Style{FlexGrow:"1",} }, func() any{ return widgets.Text(gxscene.Props{"value": breadcrumbText(), "size": widgets.FontFootnote, "tone": "muted"}) }, func() any{ return gxscene.Style{Width:"8",} }, func() any{ return iconButton("panel", ToggleTerminal) }, func() any{ return iconButton("moon", ToggleTheme) })
}

// editorTabBar 编辑器标签页：当前文件高亮 + 关闭图标，紧贴代码区顶部。
func editorTabBar() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpFor,Slot:1,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:2,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:3,}, func() any{ return gxscene.Style{FlexDirection:"row",AlignItems:"flex-end",Height:"32",Background:chromeBg(),BorderWidth:widgets.HairlineWidth,Border:borderColor(),} }, gxscene.ForGetterOfSignal[string](func() []string { return tabs() },nil,func(t func() string) *gxscene.Instance { return tabItem(t) }), func() any{ return gxscene.Style{FlexGrow:"1",} })
}

// tabItem 单个编辑器标签：激活高亮 + 点击切换 + 关闭。
func tabItem(name func() string) *gxscene.Instance {
    act := name() == activeTabName()
    tone := "muted"
    bg := chromeBg()
    if act {
        tone = "text"
        bg = canvasBg()
    }
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:0,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:4,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:5,},gxscene.Op{Code:gxscene.OpDynChild,Slot:6,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:7,}, func() any{ return func() { SwitchTab(name()) } }, func() any{ return gxscene.Style{FlexDirection:"row",AlignItems:"center",Gap:"8",Height:"31",Padding:"0 12",Background:bg,BorderWidth:widgets.HairlineWidth,Border:borderColor(),Cursor:"pointer",} }, func() any{ return widgets.Icon(gxscene.Props{"name": "doc", "size": 14, "tone": "blue"}) }, func() any{ return widgets.Text(gxscene.Props{"value": name(), "size": widgets.FontFootnote, "tone": tone}) }, func() any{ return func() { CloseTab(name()) } }, func() any{ return gxscene.Style{Width:"16",Height:"16",AlignItems:"center",JustifyContent:"center",Cursor:"pointer",BorderRadius:"8",Hover:&gxscene.Style{Background:widgets.UseToken("panelalt")(),},} }, func() any{ return widgets.Icon(gxscene.Props{"name": "close", "size": 12, "tone": "muted"}) })
}

// toolStripe 最左侧工具窗口竖栏（Project / Search / VCS / Settings），纯展示导航。
func stripeButton(name string, active bool) *gxscene.Instance {
    bg := ""
    tone := "muted"
    if active { bg = widgets.UseToken("panelalt")(); tone = "blue" }
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"testid",Slot:0,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpClose,},},N:3,}, func() any{ return "stripe-" + name }, func() any{ return gxscene.Style{Width:"34",Height:"34",AlignItems:"center",JustifyContent:"center",BorderRadius:widgets.RadiusSmall,Background:bg,Cursor:"pointer",Hover:&gxscene.Style{Background:widgets.UseToken("panelalt")(),},} }, func() any{ return widgets.Icon(gxscene.Props{"name": name, "size": 18, "tone": tone}) })
}

func toolStripe() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpDynChild,Slot:4,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:5,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpDynChild,Slot:6,},gxscene.Op{Code:gxscene.OpClose,},},N:7,}, func() any{ return gxscene.Style{FlexDirection:"column",AlignItems:"center",Gap:"4",Width:"40",Padding:"6 0",Background:chromeBg(),BorderWidth:widgets.HairlineWidth,Border:borderColor(),} }, func() any{ return stripeButton("folder", true) }, func() any{ return stripeButton("search", false) }, func() any{ return stripeButton("graph", false) }, func() any{ return stripeButton("cog", false) }, func() any{ return gxscene.Style{FlexGrow:"1",} }, func() any{ return stripeButton("gear", false) })
}

func statusBar() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:4,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpDynChild,Slot:5,},gxscene.Op{Code:gxscene.OpDynChild,Slot:6,},gxscene.Op{Code:gxscene.OpDynChild,Slot:7,},gxscene.Op{Code:gxscene.OpDynChild,Slot:8,},gxscene.Op{Code:gxscene.OpDynChild,Slot:9,},gxscene.Op{Code:gxscene.OpClose,},},N:10,}, func() any{ return gxscene.Style{FlexDirection:"row",AlignItems:"center",Gap:"16",Height:"26",Padding:"0 12",Background:chromeBg(),BorderWidth:widgets.HairlineWidth,Border:borderColor(),} }, func() any{ return widgets.Icon(gxscene.Props{"name": "bolt", "size": 13, "tone": "muted"}) }, func() any{ return widgets.Text(gxscene.Props{"value": "main", "size": widgets.FontFootnote, "tone": "muted"}) }, func() any{ return widgets.Text(gxscene.Props{"value": "GoX IDE 1.0", "size": widgets.FontFootnote, "tone": "muted"}) }, func() any{ return gxscene.Style{FlexGrow:"1",} }, func() any{ return widgets.Text(gxscene.Props{"value": EditorStatus(), "size": widgets.FontFootnote, "tone": "muted"}) }, func() any{ return widgets.Text(gxscene.Props{"value": "Spaces: 4", "size": widgets.FontFootnote, "tone": "muted"}) }, func() any{ return widgets.Text(gxscene.Props{"value": "UTF-8", "size": widgets.FontFootnote, "tone": "muted"}) }, func() any{ return widgets.Text(gxscene.Props{"value": "LF", "size": widgets.FontFootnote, "tone": "muted"}) }, func() any{ return widgets.Text(gxscene.Props{"value": "GX", "size": widgets.FontFootnote, "tone": "muted"}) })
}

// ---- 主界面：WebStorm 风格三区布局 ----
// [菜单栏 / 工具栏] → [工具竖栏 | Project 面板 | ⋮ | (代码区标签 / 编辑器 / ─ / Terminal)] → [状态栏]
// 左右分隔条 (gx-splitter-v) 调整 Project 宽度，水平分隔条 (gx-splitter-h) 调整 Terminal 高度。

// editorAndTerminal 是右侧「标签 / 代码 / 水平分隔条 / 终端」组合。
// 抽成函数是为了让项目面板显隐的两个分支各自给出完整的主区子树——
// 若把 if 直接放在 row flex 内，编译器生成的 #frag 节点会成为 row 的直接
// 子项，交叉轴对齐把它推到容器底部，导致垂直分隔条命中框跑到 y=834。
func editorAndTerminal() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:2,},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"gx-splitter-h",Key:"class",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:4,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpDynChild,Slot:5,},gxscene.Op{Code:gxscene.OpClose,},},N:6,}, func() any{ return gxscene.Style{FlexDirection:"column",Gap:"0",FlexGrow:"1",MinWidth:"0",} }, func() any{ return editorTabBar() }, func() any{ return gxscene.Style{FlexDirection:"column",Gap:"0",Height:editorAreaPx(),FlexShrink:"0",Overflow:"hidden",Background:canvasBg(),} }, func() any{ return Editor() }, func() any{ return gxscene.Style{Height:"6",FlexShrink:"0",Cursor:"row-resize",Background:splitActiveBg(),Hover:&gxscene.Style{Background:accentColor(),},} }, func() any{ return TerminalView() })
}

 func Playground() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:0,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpBranch,Slot:4,},gxscene.Op{Code:gxscene.OpDynChild,Slot:5,},gxscene.Op{Code:gxscene.OpClose,},},N:6,}, func() any{ return func(e gxscene.EventContext) { OpenMenu("" )} }, func() any{ return gxscene.Style{Background:canvasBg(),Color:textColor(),Padding:"0",FlexDirection:"column",Gap:"0",Width:"1400",Height:"860",Overflow:"hidden",} }, func() any{ return menuBar() }, func() any{ return ideToolbar() }, gxscene.BranchGetter(func() int{if projectPanelVisible() { return 0 }
return 1 }, func() *gxscene.Instance { return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"#frag",},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:2,},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"gx-splitter-v",Key:"class",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:4,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpDynChild,Slot:5,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:6,}, func() any{ return gxscene.Style{FlexDirection:"row",Gap:"0",FlexGrow:"1",Height:"768",} }, func() any{ return toolStripe() }, func() any{ return gxscene.Style{Background:panelBg(),Padding:"4 6",Width:projectWidthPx(),Height:"768",FlexShrink:"0",FlexDirection:"column",Gap:"4",BorderWidth:widgets.HairlineWidth,Border:borderColor(),Overflow:"hidden",} }, func() any{ return projectPanel() }, func() any{ return gxscene.Style{Width:"6",Height:"768",FlexShrink:"0",Cursor:"col-resize",Background:splitActiveBg(),Hover:&gxscene.Style{Background:accentColor(),},} }, func() any{ return editorAndTerminal() }) }, func() *gxscene.Instance { return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"#frag",},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:3,}, func() any{ return gxscene.Style{FlexDirection:"row",Gap:"0",FlexGrow:"1",Height:"768",} }, func() any{ return toolStripe() }, func() any{ return editorAndTerminal() }) }), func() any{ return statusBar() })
}
// ---- 编辑器命令转发（装配层 ui.Editor* API 保持兼容）----

 func EditorInsert(s string)              { editorState.Insert(s) }
 func EditorBackspace()                   { editorState.Backspace() }
 func EditorNewline()                     { editorState.Newline() }
 func EditorMove(dln int, dcol int)       { editorState.Move(dln, dcol) }
 func EditorMoveEx(dln int, dcol int, shift bool) { editorState.MoveEx(dln, dcol, shift) }
 func EditorMoveWord(dir int, shift bool) { editorState.MoveWord(dir, shift) }
 func EditorSetText(s string)             { editorState.SetText(s) }
 func EditorHome()                        { editorState.Home() }
 func EditorSetCursor(ln int, col int)    { editorState.SetCursor(ln, col) }
 func EditorClearSelection()              { editorState.ClearSelection() }
 func EditorSetSelect(ln int, col int)    { editorState.SetSelect(ln, col) }
 func EditorExtendTo(ln int, col int)     { editorState.ExtendTo(ln, col) }
 func EditorKeyHome(shift bool)           { editorState.KeyHome(shift) }
 func EditorKeyEnd(shift bool)            { editorState.KeyEnd(shift) }
 func EditorDeleteForward()               { editorState.DeleteForward() }
 func EditorSelectAll()                   { editorState.SelectAll() }
 func EditorSelectWord(ln int, col int)   { editorState.SelectWord(ln, col) }
 func EditorSelectLine(ln int)            { editorState.SelectLine(ln) }
 func EditorSelectedText() string         { return editorState.SelectedText() }
 func EditorDeleteSelection()             { editorState.DeleteSelection() }
 func EditorPaste(s string)               { editorState.Paste(s) }
 func EditorUndo()                        { editorState.Undo() }
 func EditorRedo()                        { editorState.Redo() }
 func EditorScrollLines(lines float64)    { editorState.ScrollLines(lines) }
 func EditorFocus()                       { editorState.Focus() }
 func EditorBlur()                        { editorState.Blur() }
 func EditorSettle()                      { editorState.Settle() }
 func EditorIdle()                        { editorState.Idle() }
 func SetCharWidth(w float64)             { editorState.SetCharWidth(w) }
 func SetWideWidth(w float64)             { editorState.SetWideWidth(w) }
 func EditorLine() int                    { return editorState.Line() }
 func EditorCol() int                     { return editorState.Col() }
 func EditorText() string                 { return editorState.Text() }
 func EditorStatus() string               { return editorState.Status() }
 func PickColumn(idx int, x float64) int  { return editorState.PickColumn(idx, x) }
 func EditorLineText(idx int, slot int) string { return editorState.LineText(idx, slot) }

// ---- GUI 端到端测试用的只读观测（无副作用，仅快照读取）----
// 项目树展开/折叠、点击文件打开标签都是纯响应面变化，进程外看不见；
// 暴露可见行数与展开目录数让 E2E 能硬断言，而非比对截图。
 func ProjectVisibleRows() int { return len(projectRows()) }
 func ProjectExpandedCount() int { return len(ProjectExpanded()) }
