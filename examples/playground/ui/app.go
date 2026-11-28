// app.gx —— 用 GX 语言编写的 playground 主界面。
//
// 与 editor.gx 一起构成"用 GX 写 GX playground"的自举案例：
// 所有 UI（工具栏 / 示例列表 / 编辑器 / 实时预览 / 状态栏）都用 GX JSX 表达，
// 转译成标准 Go 场景树后，桌面（skia+GLFW）与网页（web+wasm）共用。
package ui

import (
    "strconv"
    "strings"
    demos "github.com/robertpanvip/gox-playground/ui/demos"
    Signal "github.com/robertpanvip/gox-core/runtime/signal"
    gxscene "github.com/robertpanvip/gox-core/runtime/scene"
    widgets "github.com/robertpanvip/gox-core/widgets"
)

// ---- 编辑器实例（widgets.Editor 公共组件）----
// 从 ui/editor.gx 抽离到 widgets 后，playground 持有一个 EditorState 句柄，
// 通过下列转发函数把装配层（desktop/web）的 ui.Editor* 调用委托给实例方法。
// 这样装配层零改动，同时编辑器本身成为可多实例/多 App 隔离的公共组件。
var editorState = widgets.NewEditorState("")

// Ed 暴露编辑器实例句柄（供需要直接访问 state 的装配层使用）。
 func Ed() *widgets.EditorState { return editorState }

// Editor 构造编辑器视图（替代原 ui.Editor()）。
func Editor() *gxscene.Instance { return widgets.Editor(editorState) }

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
        Signal.Batch(func() { RunningSig(true )})
        runHandler()
    }
}

// SetRunResult 由装配层在解释完成后调用；inst=nil 表示回落到预编译 demo。
 func SetRunResult(inst *gxscene.Instance, err string) {
    runInstance = inst
    runErr = err
    runStamp = runStamp + 1
    Signal.Batch(func() { RunningSig(false )})
}

// ---- 输出终端（Console）----
// 用户源码里的 println / print / fmt.println / fmt.print / fmt.printf 输出
// 显示在这里。状态源是 widgets 内置 Terminal 组件（sig 驱动刷新）；
// 装配层经 interp.SetOutput 把解释器的输出接到 ConsolePrint，web 端同时
// 转发到浏览器 DevTools console。
var consoleState = widgets.NewTerminal()

 func ConsolePrint(text string) {
    consoleState.Print(text)
}

 func ConsoleMark(label string) {
    consoleState.RunMark(label)
}

 func ConsoleClear() {
    consoleState.Clear()
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

// ---- 视口（响应式布局，四端统一）----
// 各端装配层在窗口尺寸建立/变化时注入逻辑视口（CSS px）：desktop=GLFW 窗口
// 尺寸，web=浏览器视口，Android=物理像素/DPR。Playground 按断点自适应：
// <720 切竖屏紧凑布局（含虚拟键盘），<1024 收缩预览列，其余保持桌面三列。
// 注入后装配层需调用 gxApp.Render 重建整树（与主题切换同机制）。
var vpW = 1360
var vpH = 760

 func SetViewport(w int, h int) {
    if w > 0 && h > 0 {
        vpW = w
        vpH = h
        // 紧凑布局（<720）联动编辑器视口高度：扣除固定预算，下限 120。
        if w < 720 {
            eh := h - 600
            if eh < 120 { eh = 120 }
            editorState.SetViewportHeight(eh)
        }
    }
}
 func ViewportW() int { return vpW }
 func ViewportH() int { return vpH }

// ---- 渲染诊断角标（Android 真机排查用，桌面/Web 默认空不显示）----
// 装配层注入当前渲染路径（v2-wgpu / gles-blit）、DPR、物理/逻辑尺寸。
// 只在文本非空时显示，桌面/Web 不注入即零视觉影响。
var diagText = ""
 func SetDiagText(s string) { diagText = s }
 func DiagText() string      { return diagText }

// ---- 虚拟键盘（触屏端）----
// Android 等无物理键盘的平台由装配层置 true，紧凑布局底部渲染 KeyboardBar。
// 按键点击走与桌面键盘回调同一组 Editor* 命令——编辑逻辑零平台差异。
var KeyboardEnabled = false
 func SetKeyboardEnabled(v bool) { KeyboardEnabled = v }

 var VkShift = Signal.New(false)
 var VkSymbol = Signal.New(false)

func toggleVKShift() {
    Signal.Batch(func() {
        VkShift(!VkShift())
    })
}
func setVKSymbol(v bool) {
    if VkSymbol() != v {
        Signal.Batch(func() {
            VkSymbol(v)
            VkShift(false)
        })
    }
}

// vkInsert 把按键字符插入编辑器：Shift 决定大小写，插入后自动复位 Shift。
func vkInsert(ch string) {
    if VkShift() && len(ch) == 1 {
        ch = strings.ToUpper(ch)
    }
    EditorInsert(ch)
    if VkShift() {
        Signal.Batch(func() {
            VkShift(false)
        })
    }
}

// ---- 测试桥 ----
// TestSnapshot 供 E2E 装配层读取的单行 JSON 状态快照（示例/光标/滚动/运行态）。
// 文本截断到 200 个 rune 并走 strconv.Quote（保证合法 JSON 转义）。
func TestSnapshot() string {
    rs := []rune(EditorText())
    if len(rs) > 200 { rs = rs[:200] }
    return "{\"sample\":" + strconv.Itoa(SampleIndex()) +
        ",\"count\":" + strconv.Itoa(len(Samples())) +
        ",\"line\":" + strconv.Itoa(EditorLine()) +
        ",\"col\":" + strconv.Itoa(EditorCol()) +
        ",\"scroll\":" + strconv.Itoa(int(editorState.ScrollTop())) +
        ",\"runStamp\":" + strconv.Itoa(runStamp) +
        ",\"runErr\":" + strconv.Quote(runErr) +
        ",\"text\":" + strconv.Quote(string(rs)) + "}"
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
        Sample{Name: "01 Hello GX", Group: "基础", Tag: "sig 裸读", Desc: "声明信号，JSX 里直接读，编译器自动补调用", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox-core/runtime/scene\"\n)\n\n// sig 声明 + 裸读（编译器自动补 ()）\nsig name = \"GX\"\nfunc view() *gxscene.Instance {\n    return <div>Hello, {name}!</div>\n}"},
        Sample{Name: "02 Counter", Group: "基础", Tag: "事件 / 样式", Desc: "事件传函数原值，样式收进 style 结构体", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox-core/runtime/scene\"\n)\n\n// 事件传函数原值 + sig 读写 + 样式进 style struct\nsig count = 0\nfunc inc() {\n    count = count + 1\n}\nfunc view() *gxscene.Instance {\n    return <button onClick={inc} style={{ background: \"#3b82f6\", padding: 10 }}>Count: {count}</button>\n}"},
        Sample{Name: "03 For 列表", Group: "基础", Tag: "For / getter", Desc: "keyed 列表，item 用 t() 读实时值", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox-core/runtime/scene\"\n)\n\n// For + item getter（t() 读值）+ 样式进 style struct\nsig todos = []string{\"a\", \"b\", \"c\"}\nfunc view() *gxscene.Instance {\n    return <div style={{ flexDirection: \"column\", gap: 6 }}>\n        <For each={todos}>{(t string) => <span>{t()}</span>}</For>\n    </div>\n}"},
        Sample{Name: "04 样式卡片", Group: "基础", Tag: "Style struct", Desc: "驼峰 key + hover 伪状态单词 key", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox-core/runtime/scene\"\n)\n\n// style={{...}} 强类型 struct + 驼峰 key + 伪状态单词 key\nfunc view() *gxscene.Instance {\n    return <div\n        style={{\n            background: \"#0b1020\",\n            padding: 20,\n            borderRadius: 12,\n            flexDirection: \"column\",\n            gap: 8,\n            hover: { background: \"#1e293b\" },\n        }}\n    >\n        <b style={{ color: \"#7dd3fc\", fontSize: 24 }}>Card</b>\n        <span style={{ color: \"#94a3b8\" }}>用 GX style struct 写的样式</span>\n    </div>\n}"},
        Sample{Name: "05 computed", Group: "响应式", Tag: "computed / batch", Desc: "派生信号 + 批量合并写入", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox-core/runtime/scene\"\n    Signal \"github.com/robertpanvip/gox-core/runtime/signal\"\n)\n\n// Signal.computed 派生 + Signal.batch 合并 + 事件传函数原值\nsig price = 10\nsig qty = 2\nvar total = Signal.computed(() => price * qty)\nfunc bump() {\n    Signal.batch(() => {\n        price = price + 1\n        qty = qty + 1\n    })\n}\nfunc view() *gxscene.Instance {\n    return <div style={{ flexDirection: \"column\", gap: 6 }}>\n        <span>Total: {total}</span>\n        <button onClick={bump}>加价加量</button>\n    </div>\n}"},
        Sample{Name: "06 条件分支", Group: "响应式", Tag: "if / else", Desc: "JSX 内条件编译成细粒度 Branch", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox-core/runtime/scene\"\n)\n\n// JSX 条件分支：if/else 编译成细粒度 Branch，条件变化只换分支子树\nsig on = true\nfunc toggle() { on = !on }\nfunc view() *gxscene.Instance {\n    return <div style={{ flexDirection: \"column\", gap: 8 }}>\n        <button onClick={toggle} style={{ background: \"#3b82f6\", padding: 8, borderRadius: 6 }}>\n            切换分支\n        </button>\n        <div style={{ padding: 16, borderRadius: 8 }}>\n            if on {\n                <b style={{ color: \"#4ec9b0\", fontSize: 18 }}>ON · 分支 A</b>\n            } else {\n                <b style={{ color: \"#f87171\", fontSize: 18 }}>OFF · 分支 B</b>\n            }\n        </div>\n    </div>\n}\n"},
        Sample{Name: "07 列表增删", Group: "响应式", Tag: "sig []T", Desc: "切片增删，For 自动增删对应节点", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox-core/runtime/scene\"\n)\n\n// sig 切片增删 + For 响应式列表 + item getter（t() 读值）\nsig items = []string{\"alpha\", \"beta\"}\nfunc add() { items = append(items, \"item\") }\nfunc clear() { items = []string{} }\nfunc view() *gxscene.Instance {\n    return <div style={{ flexDirection: \"column\", gap: 8 }}>\n        <div style={{ flexDirection: \"row\", gap: 8 }}>\n            <button onClick={add} style={{ padding: 8, borderRadius: 6 }}>添加</button>\n            <button onClick={clear} style={{ padding: 8, borderRadius: 6 }}>清空</button>\n        </div>\n        <For each={items}>\n            {(t string) => <div style={{ color: \"#9cdcfe\", padding: 4 }}>{t()}</div>}\n        </For>\n    </div>\n}\n"},
        Sample{Name: "08 枚举", Group: "语言特性", Tag: "enum", Desc: "类型 / 常量 / String / Parse，三种拼写", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox-core/runtime/scene\"\n)\n\n// enum 枚举：类型 + 常量 + String() + Parse<T>()，三种拼写指向同一符号\npub enum color string { red = \"#ef4444\", green = \"#22c55e\", blue = \"#3b82f6\" }\nsig idx = 0\nfunc next() { idx = (idx + 1) % 3 }\nfunc view() *gxscene.Instance {\n    cs := []color{colorRed, colorGreen, colorBlue}\n    return <div style={{ flexDirection: \"column\", gap: 8 }}>\n        <button onClick={next} style={{ padding: 8 }}>换色</button>\n        <div style={{ background: string(cs[idx]), padding: 20, borderRadius: 8, color: \"#fff\" }}>\n            {cs[idx]}\n        </div>\n    </div>\n}\n"},
        Sample{Name: "09 模板字符串", Group: "语言特性", Tag: "模板 / 默认值", Desc: "反引号插值 + 尾部参数默认值", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox-core/runtime/scene\"\n)\n\n// 模板字符串（反引号内插值）+ 参数默认值（尾部缺失参数自动补全）\nfunc greet(name string = \"GX\", mark string = \"!\") string {\n    return `你好, ${name}${mark}`\n}\nsig who = \"world\"\nfunc fullText() string { return greet(who) }\nfunc defaultText() string { return greet() }\nfunc change() { who = \"GX\" }\nfunc view() *gxscene.Instance {\n    return <div style={{ flexDirection: \"column\", gap: 8 }}>\n        <button onClick={change} style={{ padding: 8 }}>换名字</button>\n        <span style={{ color: \"#c3e88d\", fontSize: 16 }}>{fullText()}</span>\n        <span style={{ color: \"#858585\" }}>{defaultText()}</span>\n    </div>\n}\n"},
        Sample{Name: "10 对象解构", Group: "语言特性", Tag: "解构", Desc: "var { x, y } = p 逐字段取值", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox-core/runtime/scene\"\n)\n\n// 对象解构：{ x, y } := p 逐字段取值\ntype point struct { x int; y int }\nfunc coord() point { return point{x: 3, y: 4} }\nfunc view() *gxscene.Instance {\n    p := coord()\n    var { x, y } = p\n    return <div style={{ flexDirection: \"column\", gap: 6 }}>\n        <span style={{ color: \"#9cdcfe\" }}>x = {x}</span>\n        <span style={{ color: \"#c3e88d\" }}>y = {y}</span>\n        <span style={{ color: \"#858585\" }}>sum = {x + y}</span>\n    </div>\n}\n"},
        Sample{Name: "11 联合类型", Group: "语言特性", Tag: "union / is", Desc: "擦除为 any 的语法便利层 + 类型守卫", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox-core/runtime/scene\"\n    \"fmt\"\n)\n\n// 联合类型 int | string + is 守卫（擦除为 any 的语法便利层）\ntype value = int | string\nfunc show(v int | string) string {\n    if v is string { return fmt.sprint(\"str:\", v) }\n    return fmt.sprint(\"int:\", v)\n}\nfunc view() *gxscene.Instance {\n    return <div style={{ flexDirection: \"column\", gap: 6 }}>\n        <span style={{ color: \"#9cdcfe\" }}>{show(7)}</span>\n        <span style={{ color: \"#c3e88d\" }}>{show(\"gx\")}</span>\n    </div>\n}\n"},
        Sample{Name: "12 组件", Group: "语言特性", Tag: "props / sig 字段", Desc: "pub func 组件，props 的 sig 字段保鲜", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox-core/runtime/scene\"\n)\n\n// 组件：pub func 返回场景树，props 的 sig 字段保鲜（读到的是实时值）\ntype cardProps struct {\n    sig title string\n}\npub func card(p cardProps) *gxscene.Instance {\n    return <div style={{ background: \"#1e293b\", padding: 12, borderRadius: 8 }}>\n        <b style={{ color: \"#7dd3fc\" }}>{p.title}</b>\n    </div>\n}\nsig text = \"Hello\"\nfunc change() { text = \"Changed\" }\nfunc view() *gxscene.Instance {\n    return <div style={{ flexDirection: \"column\", gap: 8 }}>\n        <button onClick={change} style={{ padding: 8 }}>改标题</button>\n        <Card title={text}/>\n    </div>\n}\n"},
        Sample{Name: "13 事件签名", Group: "语言特性", Tag: "EventContext", Desc: "具名函数 / 箭头 / func 字面量 / 带 ctx", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox-core/runtime/scene\"\n)\n\n// 事件传函数原值：具名函数 / 箭头 / func 字面量 / 带 EventContext 签名\nsig last = \"none\"\nfunc named() { last = \"named\" }\nfunc onCtx(e gxscene.EventContext) { if e.Synthetic != nil { last = string(e.Synthetic.Type) } }\nfunc view() *gxscene.Instance {\n    return <div style={{ flexDirection: \"column\", gap: 6 }}>\n        <button onClick={named} style={{ padding: 8 }}>具名函数</button>\n        <button onClick={() => last = \"arrow\"} style={{ padding: 8 }}>箭头</button>\n        <button onClick={func() { last = \"literal\" }} style={{ padding: 8 }}>func 字面量</button>\n        <button onClick={onCtx} style={{ padding: 8 }}>带 ctx</button>\n        <span style={{ color: \"#c3e88d\" }}>last: {last}</span>\n    </div>\n}\n"},
        Sample{Name: "14 struct sig", Group: "语言特性", Tag: "字段响应式", Desc: "显式标注的字段才响应，构造后读实时值", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox-core/runtime/scene\"\n)\n\n// struct sig 字段：显式标注才响应，构造后读到的是实时值\ntype state struct {\n    sig count int\n}\nsig source = 0\nfunc bump() { source = source + 1 }\nfunc view() *gxscene.Instance {\n    s := state{count: source}\n    return <div style={{ flexDirection: \"column\", gap: 8 }}>\n        <button onClick={bump} style={{ padding: 8 }}>+1</button>\n        <span style={{ color: \"#9cdcfe\" }}>count: {s.count}</span>\n    </div>\n}\n"},
        Sample{Name: "15 集合链式", Group: "语言特性", Tag: "List[T]", Desc: "[]T 提升为 List，filter/map/reduce", Source: "package main\n\nimport (\n    gxscene \"github.com/robertpanvip/gox-core/runtime/scene\"\n)\n\n// 集合链式：[]T 提升为 List[T]，filter/map/reduce 可换型\nsig nums = []int{1, 2, 3, 4, 5}\nfunc shuffle() { nums = []int{2, 4, 6, 8} }\nfunc view() *gxscene.Instance {\n    return <div style={{ flexDirection: \"column\", gap: 8 }}>\n        <button onClick={shuffle} style={{ padding: 8 }}>换数据</button>\n        <span>偶数个数: {len(nums.filter(func(n int) bool { return n % 2 == 0 }))}</span>\n    </div>\n}\n"},
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
        EditorSetText(xs[i].Source)
        EditorHome()
        EditorFocus()
        RunningSig(false)
    })
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
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpAttr,Arg:"gx-sample-row",Key:"class",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"data-index",Slot:1,},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:2,},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:4,},gxscene.Op{Code:gxscene.OpDynChild,Slot:5,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:6,}, func() any{ return gxscene.Style{Background:sampleRowBg(i),Padding:"8 10",BorderRadius:widgets.RadiusSmall,BorderWidth:widgets.HairlineWidth,Border:borderColor(),FlexDirection:"column",Gap:"3",Cursor:"pointer",Transition:"background .15s",} }, func() any{ return strconv.Itoa(i) }, func() any{ return func() { LoadSample(i) } }, func() any{ return widgets.Text(gxscene.Props{"value": s.Name, "size": widgets.FontBody, "weight": "bold", "tone": sampleRowFg(i)}) }, func() any{ return gxscene.Style{Color:groupColor(s.Group),FontSize:widgets.FontFootnote,} }, func() any{ return s.Group + " · " + s.Tag })
}

func sampleList() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpFor,Slot:1,},gxscene.Op{Code:gxscene.OpClose,},},N:2,}, func() any{ return gxscene.Style{FlexDirection:"column",Gap:"4",Height:"500",Overflow:"auto",} }, gxscene.ForGetterOfSignal[int](func() []int { return Indices(len(Samples())) },nil,func(i func() int) *gxscene.Instance { return sampleRow(i()) }))
}

// ---- 工具栏 ----

func toolbarButton(label string, icon string, variant string, fn func()) *gxscene.Instance {
    return widgets.Button(gxscene.Props{
        "label": label,
        "icon": icon,
        "variant": variant,
        "width": "96",
        "onClick": gxscene.Handler(func(e gxscene.EventContext) { fn() }),
    })
}

// toolButton 带定位标记的工具按钮（class/data-action 供 E2E 测试桥导出坐标）。
func toolButton(label string, icon string, variant string, action string, fn func()) *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"gx-tool-btn",Key:"class",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"data-action",Slot:0,},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpClose,},},N:2,}, func() any{ return action }, func() any{ return toolbarButton(label, icon, variant, fn) })
}

func toolbar() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpDynChild,Slot:4,},gxscene.Op{Code:gxscene.OpDynChild,Slot:5,},gxscene.Op{Code:gxscene.OpClose,},},N:6,}, func() any{ return gxscene.Style{FlexDirection:"row",Gap:"8",AlignItems:"center",} }, func() any{ return toolButton("上一个", "chevron-left", "secondary", "prev", PrevSample) }, func() any{ return toolButton("下一个", "chevron-right", "secondary", "next", NextSample) }, func() any{ return toolButton("运行", "play", "primary", "run", TriggerRun) }, func() any{ return toolButton("主题", "moon", "secondary", "theme", ToggleTheme) }, func() any{ return widgets.Text(gxscene.Props{"value": EditorStatus(), "size": widgets.FontFootnote, "tone": "muted"}) })
}

// ---- 主界面（响应式，四端统一）----
// 断点：>=1024 桌面三列（示例 / 编辑器 / 预览）；720-1023 收缩两列；
// <720 竖屏紧凑单列（示例 / 工具栏 / 编辑器 / 预览 / 虚拟键盘）。
 func Playground() *gxscene.Instance {
    if ViewportW() < 720 {
        return PlaygroundCompact()
    }
    return PlaygroundFull()
}

// playgroundFull 桌面/平板布局：原三列结构，根尺寸由视口驱动。
 func PlaygroundFull() *gxscene.Instance {
    n := len(Samples())
    leftW := 240
    if ViewportW() < 1024 { leftW = 190 }
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:4,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:5,},gxscene.Op{Code:gxscene.OpDynChild,Slot:6,},gxscene.Op{Code:gxscene.OpDynChild,Slot:7,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:8,},gxscene.Op{Code:gxscene.OpDynChild,Slot:9,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:10,},gxscene.Op{Code:gxscene.OpDynChild,Slot:11,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpDynChild,Slot:12,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:13,},gxscene.Op{Code:gxscene.OpChildText,Arg:"⌘↵ 运行 · ⌘Z 撤销 · ⌃空格 补全 · ⇧⌥F 格式化 · 悬停看符号",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:14,}, func() any{ return gxscene.Style{Background:canvasBg(),Color:textColor(),Padding:"0",FlexDirection:"column",Gap:"0",Width:strconv.Itoa(ViewportW()),Height:strconv.Itoa(ViewportH()),BorderRadius:RadiusPanelLocal(),BorderWidth:widgets.HairlineWidth,Border:borderColor(),Overflow:"hidden",BoxShadow:paneShadow(),} }, func() any{ return gxscene.Style{Background:panelBg(),Padding:"16 20",FlexDirection:"column",Gap:"4",} }, func() any{ return widgets.Heading(gxscene.Props{"value": "GX Playground", "level": 2, "tone": "text"}) }, func() any{ return widgets.Text(gxscene.Props{
                "value": "用 GX 写 GX · 桌面与网页同源 · " + strconv.Itoa(n) + " 个示例 · 内建 LSP · Run 解释当前源码",
                "size": widgets.FontCallout, "tone": "muted",
            }) }, func() any{ return gxscene.Style{FlexDirection:"row",Gap:"12",Padding:"16",FlexGrow:"1",Height:strconv.Itoa(ViewportH() - 120),} }, func() any{ return gxscene.Style{Background:panelBg(),Padding:"12",BorderRadius:widgets.RadiusCard,Width:strconv.Itoa(leftW),FlexDirection:"column",Gap:"8",BorderWidth:widgets.HairlineWidth,Border:borderColor(),BoxShadow:cardShadow(),} }, func() any{ return widgets.Text(gxscene.Props{"value": "示例  /  " + strconv.Itoa(n), "size": widgets.FontFootnote, "weight": "bold", "tone": "muted"}) }, func() any{ return sampleList() }, func() any{ return gxscene.Style{FlexDirection:"column",Gap:"8",FlexGrow:"1",} }, func() any{ return toolbar() }, func() any{ return gxscene.Style{Background:panelBg(),Padding:"12",BorderRadius:widgets.RadiusCard,BorderWidth:widgets.HairlineWidth,Border:borderColor(),BoxShadow:cardShadow(),} }, func() any{ return Editor() }, func() any{ return previewPane() }, func() any{ return gxscene.Style{Color:mutedColor(),FontSize:widgets.FontFootnote,Padding:"12 16",} })
}

// previewPane 实时预览列：宽 <1024 时让位给编辑器（预览折叠）。
func previewPane() *gxscene.Instance {
    if ViewportW() < 1024 { return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpClose,},},N:0,}) }
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:1,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:2,},gxscene.Op{Code:gxscene.OpChildText,Arg:"⬤ 实时预览",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:4,},gxscene.Op{Code:gxscene.OpDynChild,Slot:5,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:6,},gxscene.Op{Code:gxscene.OpDynChild,Slot:7,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:8,},gxscene.Op{Code:gxscene.OpDynChild,Slot:9,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:10,}, func() any{ return gxscene.Style{Background:panelBg(),Padding:"12",BorderRadius:widgets.RadiusCard,Width:"400",FlexDirection:"column",Gap:"8",BorderWidth:widgets.HairlineWidth,Border:borderColor(),BoxShadow:cardShadow(),} }, func() any{ return gxscene.Style{FlexDirection:"column",Gap:"3",} }, func() any{ return gxscene.Style{Color:accentColor(),FontSize:widgets.FontBody,Weight:"bold",} }, func() any{ return widgets.Text(gxscene.Props{
                "value": Samples()[SampleIndex()].Name + " · " + Samples()[SampleIndex()].Desc,
                "size": widgets.FontFootnote, "tone": "muted",
            }) }, func() any{ return gxscene.Style{Background:appBg(),Padding:"12",BorderRadius:widgets.RadiusSmall,MinHeight:"400",Overflow:"auto",BorderWidth:widgets.HairlineWidth,Border:borderColor(),} }, func() any{ return demoPreview() }, func() any{ return gxscene.Style{Color:RunInfoColor(),FontSize:widgets.FontFootnote,} }, func() any{ return RunInfo() }, func() any{ return gxscene.Style{FlexDirection:"column",Gap:"4",} }, func() any{ return widgets.Terminal(gxscene.Props{"state": consoleState, "height": "140", "title": "Console · println 输出"}) })
}

// playgroundCompact 竖屏单列：示例列表折叠为固定高度区块，预览与 console
// 收进编辑器下方，底部挂虚拟键盘（KeyboardEnabled 时）。
 func PlaygroundCompact() *gxscene.Instance {
    n := len(Samples())
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:3,},gxscene.Op{Code:gxscene.OpDynChild,Slot:4,},gxscene.Op{Code:gxscene.OpDynChild,Slot:5,},gxscene.Op{Code:gxscene.OpDynChild,Slot:6,},gxscene.Op{Code:gxscene.OpDynChild,Slot:7,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:8,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:9,},gxscene.Op{Code:gxscene.OpDynChild,Slot:10,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:11,},gxscene.Op{Code:gxscene.OpAttr,Arg:"gx-sample-list",Key:"class",},gxscene.Op{Code:gxscene.OpFor,Slot:12,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:13,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:14,},gxscene.Op{Code:gxscene.OpDynChild,Slot:15,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:16,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:17,},gxscene.Op{Code:gxscene.OpDynChild,Slot:18,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:19,},gxscene.Op{Code:gxscene.OpDynChild,Slot:20,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpDynChild,Slot:21,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpDynChild,Slot:22,},gxscene.Op{Code:gxscene.OpClose,},},N:23,}, func() any{ return gxscene.Style{Background:canvasBg(),Color:textColor(),Padding:"0",FlexDirection:"column",Gap:"0",Width:strconv.Itoa(ViewportW()),Height:strconv.Itoa(ViewportH()),Overflow:"hidden",} }, func() any{ return gxscene.Style{Background:panelBg(),Padding:"8 12",FlexDirection:"column",Gap:"2",} }, func() any{ return widgets.Heading(gxscene.Props{"value": "GX Playground", "level": 3, "tone": "text"}) }, func() any{ return gxscene.Style{FlexDirection:"row",Gap:"6",Padding:"4 12",AlignItems:"center",} }, func() any{ return toolButton("‹ 上一个", "chevron-left", "secondary", "prev", PrevSample) }, func() any{ return toolButton("下一个 ›", "chevron-right", "secondary", "next", NextSample) }, func() any{ return toolButton("运行", "play", "primary", "run", TriggerRun) }, func() any{ return toolButton("主题", "moon", "secondary", "theme", ToggleTheme) }, func() any{ return gxscene.Style{FlexDirection:"column",Gap:"6",Padding:"4 12",Height:"132",} }, func() any{ return gxscene.Style{Background:panelBg(),Padding:"8",BorderRadius:widgets.RadiusCard,FlexDirection:"column",Gap:"6",BorderWidth:widgets.HairlineWidth,Border:borderColor(),BoxShadow:cardShadow(),Height:"124",Overflow:"hidden",} }, func() any{ return widgets.Text(gxscene.Props{"value": "示例  /  " + strconv.Itoa(n), "size": widgets.FontFootnote, "weight": "bold", "tone": "muted"}) }, func() any{ return gxscene.Style{FlexDirection:"column",Gap:"4",FlexGrow:"1",Overflow:"auto",} }, gxscene.ForGetterOfSignal[int](func() []int { return Indices(len(Samples())) },nil,func(i func() int) *gxscene.Instance { return sampleRow(i()) }), func() any{ return gxscene.Style{FlexDirection:"column",Gap:"6",Padding:"0 12",FlexGrow:"1",} }, func() any{ return gxscene.Style{Background:panelBg(),Padding:"8",BorderRadius:widgets.RadiusCard,BorderWidth:widgets.HairlineWidth,Border:borderColor(),BoxShadow:cardShadow(),FlexGrow:"1",Overflow:"hidden",} }, func() any{ return Editor() }, func() any{ return gxscene.Style{FlexDirection:"column",Gap:"4",Padding:"6 12",} }, func() any{ return gxscene.Style{Color:RunInfoColor(),FontSize:widgets.FontFootnote,} }, func() any{ return RunInfo() }, func() any{ return gxscene.Style{Color:widgets.ThemeFaint,FontSize:"11",FontFamily:"monospace",} }, func() any{ return DiagText() }, func() any{ return widgets.Terminal(gxscene.Props{"state": consoleState, "height": "64", "title": "Console"}) }, func() any{ return KeyboardBar() })
}

// ---- 虚拟键盘（GX 自绘，触屏端与桌面键盘命令同源）----

func vkRows() [][]string {
    if VkSymbol() {
        return [][]string{
            {"1", "2", "3", "4", "5", "6", "7", "8", "9", "0"},
            {"-", "/", ":", ";", "(", ")", "$", "&", "@", "\""},
            {"+", "=", "<", ">", "{", "}", "[", "]", ".", ",", "_"},
        }
    }
    return [][]string{
        {"q", "w", "e", "r", "t", "y", "u", "i", "o", "p"},
        {"a", "s", "d", "f", "g", "h", "j", "k", "l"},
        {"z", "x", "c", "v", "b", "n", "m"},
    }
}

func vkCharLabel(ch string) string {
    if VkSymbol() { return ch }
    if VkShift() { return strings.ToUpper(ch) }
    return ch
}

func vkSymLabel() string {
    if VkSymbol() { return "ABC" }
    return "123"
}

// vkDispatch 按键语义统一分发：key 既是 data-key 标记也是字符键的输入值。
// 避免在 For 回调里嵌套箭头函数——转译器对嵌套闭包实参支持有限。
func vkDispatch(key string) {
    if key == "shift" { toggleVKShift(); return }
    if key == "sym" { setVKSymbol(!VkSymbol()); return }
    if key == "backspace" { EditorBackspace(); return }
    if key == "enter" { EditorNewline(); return }
    if key == "up" { EditorMoveEx(-1, 0, false); return }
    if key == "down" { EditorMoveEx(1, 0, false); return }
    if key == "left" { EditorMoveEx(0, -1, false); return }
    if key == "right" { EditorMoveEx(0, 1, false); return }
    if key == "undo" { EditorUndo(); return }
    if key == "redo" { EditorRedo(); return }
    if key == "selectall" { EditorSelectAll(); return }
    if key == "home" { EditorKeyHome(false); return }
    if key == "end" { EditorKeyEnd(false); return }
    vkInsert(key)
}

// vkKey 渲染单个按键。class/data-key 供 E2E 测试桥从命中框导出坐标；
// 点击行为统一走 vkDispatch（按 data-key 分发）。
func vkKey(label string, key string, w int) *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpAttr,Arg:"gx-vk-key",Key:"class",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"data-key",Slot:1,},gxscene.Op{Code:gxscene.OpEvent,Key:"onClick",Slot:2,},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpClose,},},N:4,}, func() any{ return gxscene.Style{Background:panelBg(),Width:strconv.Itoa(w),Height:"30",BorderRadius:widgets.RadiusSmall,BorderWidth:widgets.HairlineWidth,Border:borderColor(),FlexDirection:"row",AlignItems:"center",JustifyContent:"center",Cursor:"pointer",} }, func() any{ return key }, func() any{ return gxscene.Handler(func(e gxscene.EventContext) { vkDispatch(key) }) }, func() any{ return widgets.Text(gxscene.Props{"value": label, "size": widgets.FontFootnote, "tone": "text"}) })
}

// vkRowCharRow 渲染一行字符键（For 内不做嵌套闭包实参）。
func vkRowCharRow(chars []string, keyW int) *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpAttr,Arg:"gx-vk-row",Key:"class",},gxscene.Op{Code:gxscene.OpFor,Slot:1,},gxscene.Op{Code:gxscene.OpClose,},},N:2,}, func() any{ return gxscene.Style{FlexDirection:"row",Gap:"4",JustifyContent:"center",} }, gxscene.ForGetterOfSignal[string](func() []string { return chars },nil,func(c func() string) *gxscene.Instance { return vkKey(vkCharLabel(c()), c(), keyW) }))
}

 func KeyboardBar() *gxscene.Instance {
    if !KeyboardEnabled { return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpClose,},},N:0,}) }
    keyW := (ViewportW() - 24 - 9 * 4) / 10
    row1 := vkRows()[0]
    row2 := vkRows()[1]
    row3 := vkRows()[2]
    fnW := keyW * 2 + 4
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:0,},gxscene.Op{Code:gxscene.OpAttr,Arg:"gx-vk-bar",Key:"class",},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:3,},gxscene.Op{Code:gxscene.OpAttr,Arg:"gx-vk-row",Key:"class",},gxscene.Op{Code:gxscene.OpDynChild,Slot:4,},gxscene.Op{Code:gxscene.OpFor,Slot:5,},gxscene.Op{Code:gxscene.OpDynChild,Slot:6,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:7,},gxscene.Op{Code:gxscene.OpAttr,Arg:"gx-vk-row",Key:"class",},gxscene.Op{Code:gxscene.OpDynChild,Slot:8,},gxscene.Op{Code:gxscene.OpDynChild,Slot:9,},gxscene.Op{Code:gxscene.OpDynChild,Slot:10,},gxscene.Op{Code:gxscene.OpDynChild,Slot:11,},gxscene.Op{Code:gxscene.OpDynChild,Slot:12,},gxscene.Op{Code:gxscene.OpDynChild,Slot:13,},gxscene.Op{Code:gxscene.OpDynChild,Slot:14,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:15,},gxscene.Op{Code:gxscene.OpAttr,Arg:"gx-vk-row",Key:"class",},gxscene.Op{Code:gxscene.OpDynChild,Slot:16,},gxscene.Op{Code:gxscene.OpDynChild,Slot:17,},gxscene.Op{Code:gxscene.OpDynChild,Slot:18,},gxscene.Op{Code:gxscene.OpDynChild,Slot:19,},gxscene.Op{Code:gxscene.OpDynChild,Slot:20,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:21,}, func() any{ return gxscene.Style{Background:appBg(),Padding:"6 12",FlexDirection:"column",Gap:"4",} }, func() any{ return vkRowCharRow(row1, keyW) }, func() any{ return vkRowCharRow(row2, keyW) }, func() any{ return gxscene.Style{FlexDirection:"row",Gap:"4",JustifyContent:"center",} }, func() any{ return vkKey("⇧", "shift", fnW) }, gxscene.ForGetterOfSignal[string](func() []string { return row3 },nil,func(c func() string) *gxscene.Instance { return vkKey(vkCharLabel(c()), c(), keyW) }), func() any{ return vkKey("⌫", "backspace", fnW) }, func() any{ return gxscene.Style{FlexDirection:"row",Gap:"4",JustifyContent:"center",} }, func() any{ return vkKey(vkSymLabel(), "sym", fnW) }, func() any{ return vkKey("↑", "up", keyW) }, func() any{ return vkKey("↓", "down", keyW) }, func() any{ return vkKey("←", "left", keyW) }, func() any{ return vkKey("→", "right", keyW) }, func() any{ return vkKey("␣", "space", fnW + keyW) }, func() any{ return vkKey("⏎", "enter", fnW) }, func() any{ return gxscene.Style{FlexDirection:"row",Gap:"4",JustifyContent:"center",} }, func() any{ return vkKey("撤销", "undo", fnW) }, func() any{ return vkKey("重做", "redo", fnW) }, func() any{ return vkKey("全选", "selectall", fnW) }, func() any{ return vkKey("Home", "home", fnW) }, func() any{ return vkKey("End", "end", fnW) })
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

// ---- GUI E2E 只读观测钩子（仅测试快照使用，正常用户零开销）----
 func EditorScrollTop() float64  { return editorState.ScrollTop() }
 func EditorHasSelection() bool  { return editorState.HasSelection() }
 func ConsoleTextSnapshot() string { return consoleState.Text() }
