// PULSE Dashboard —— 用 GX 预制组件库 + Rust 后端渲染的端到端验证。
// 验证：预制控件跨后端复用（同一套 widgets 同款跑在 Skia 与 Rust 后端）、
// 首帧全量快照 + signal 更新走增量动作（SetText/SetAttr）+ IME 输入回传。
package main

import (
	"fmt"
	"time"

	rb "github.com/robertpanvip/gox-backend-rust"
	gxapp "github.com/robertpanvip/gox/runtime/app"
	"github.com/robertpanvip/gox/runtime/canvas"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
	widgets "github.com/robertpanvip/gox/widgets"
)

// textNode 用 Template 造一个带样式的文本组件（host 没有独立 Text 构造函数）。
// value 可以是静态字符串或 func() any 的动态 getter。
func textNode(value any, color, size string) *gxscene.Instance {
	return gxscene.Render(gxscene.Template{
		Ops: []gxscene.Op{
			{Code: gxscene.OpElem, Arg: "span"},
			{Code: gxscene.OpAttr, Key: "color", Arg: color},
			{Code: gxscene.OpAttr, Key: "font-size", Arg: size},
			{Code: gxscene.OpDynChild, Slot: 0},
			{Code: gxscene.OpClose},
		},
		N: 1,
	}, func() any {
		// value 可能是动态 getter（signal 派生的 func() any），需调用取值。
		if g, ok := value.(func() any); ok {
			return g()
		}
		return value
	})
}

func main() {
	fmt.Println("[demo] Rust backend version:", rb.Version())

	var count = signal.New(42)
	var name = signal.New("operator")
	var load = signal.New(68)

	// 每 1.2 秒自增，触发增量动作（SetText + SetAttr）
	go func() {
		for {
			time.Sleep(1200 * time.Millisecond)
			count(count() + 1)
			load((load() + 7) % 101)
		}
	}()

	// 接入层装配：gx.use(Backend) → Use(接口值) + 逐窗 Render。核心 gxapp
	// 不 import 本后端，依赖方向保持单向（后端实现 gxapp.Backend 接口形状）。
	handle := gxapp.Use(rb.NewBackend())
	// 注册 Rust 后端的文件对话框能力：FilePicker 组件点击时由它弹出原生对话框。
	rb.RegisterFileDialog()
	app, err := handle.Render(func() any { return gxscene.Render(gxscene.Template{}) },
		gxapp.WindowConfig{Title: "PULSE Dashboard", Width: 760, Height: 560})
	if err != nil {
		panic(err)
	}
	defer handle.Close()

	// 全部外观来自预制组件库；业务只描述语义。Rust 后端理解同一套属性
	// （含 align-items/justify-content 与样式继承），无需改一行组件代码。
	app.Render(func() any {
		return widgets.Col(gxscene.Props{
			"width": "760", "gap": "14", "padding": "20",
			"align": "center", // 交叉轴居中 → 卡片行/输入框/文字全部水平居中
			"children": func() any {
				return []any{
					// 标题
					textNode("PULSE · 脉冲看板", "#e2e8f0", "26"),
					// 三张卡片（预制 Card）
					widgets.Row(gxscene.Props{
						"gap": "14",
						"children": func() any {
							return []any{
								widgets.Card(gxscene.Props{
									"title": "请求量", "tone": "cyan", "width": "208",
									"children": func() any {
										return textNode(func() any { return fmt.Sprint(count()) }, "#2dd4bf", "28")
									},
								}),
								widgets.Card(gxscene.Props{
									"title": "吞吐量", "tone": "purple", "width": "208",
									"children": func() any {
										return textNode(func() any { return fmt.Sprint(count() * 2) }, "#a78bfa", "28")
									},
								}),
								widgets.Card(gxscene.Props{
									"title": "运行时长", "tone": "orange", "width": "208",
									"children": func() any {
										return textNode("99.98%", "#fb923c", "28")
									},
								}),
							}
						},
					}),
					// 进度条（预制 Progress，signal 驱动）
					widgets.Progress(gxscene.Props{
						"value": func() int { return load() }, "tone": "cyan",
						"width": "360", "label": "系统负载",
					}),
					// 徽章行（预制 Badge）
					widgets.Row(gxscene.Props{
						"gap": "10",
						"children": func() any {
							return []any{
								widgets.Badge(gxscene.Props{"label": "LIVE", "tone": "green", "dot": true}),
								widgets.Badge(gxscene.Props{"label": "RUST BACKEND", "tone": "cyan"}),
								widgets.Badge(gxscene.Props{"label": "GRX2 增量", "tone": "purple"}),
							}
						},
					}),
					// 分隔线（预制 Divider）
					widgets.Divider(gxscene.Props{"width": "360"}),
					// canvas 原语：用预制 Canvas 组件画一张折线图（Rust 端 tiny-skia / Skia 端原生绘制）
					widgets.Canvas(gxscene.Props{
						"width": "360", "height": "70", "bg": "#0b1020", "border-radius": "8",
						"ops": func() []canvas.Op {
							return []canvas.Op{
								{Kind: canvas.OpLine, X1: 0, Y1: 60, X2: 360, Y2: 60, Stroke: "#334155", StrokeWidth: 1},
								{Kind: canvas.OpLine, X1: 0, Y1: 30, X2: 360, Y2: 30, Stroke: "#334155", StrokeWidth: 1},
								{Kind: canvas.OpPath, D: "M0 55 L60 40 L120 48 L180 20 L240 35 L300 12 L360 25", Stroke: "#2dd4bf", StrokeWidth: 2},
								{Kind: canvas.OpCircle, CX: 300, CY: 12, R: 4, Fill: "#fb923c"},
							}
						},
					}),
					// 交互组件验证（预制 Slider / Switch / Segmented）
					widgets.Slider(gxscene.Props{"value": func() float64 { return float64(load()) }, "min": 0, "max": 100, "width": "360",
						"onChange": widgets.BindNumber(func(v float64) { load(int(v)) })}),
					widgets.Switch(gxscene.Props{"label": "启用实时刷新", "checked": true}),
					widgets.Segmented(gxscene.Props{"options": []string{"CPU", "MEM", "NET"}, "value": "CPU"}),
					// 输入框（预制 Input，稳定 id + 受控 value）
					widgets.Input(gxscene.Props{
						"id": "demo-input", "value": name,
						"width": "360", "placeholder": "点击后输入…",
						"onInput": widgets.BindInput(func(s string) { name(s) }),
					}),
					// 新增组件验证：Tooltip / TimePicker / FilePicker / Table 均走同一套 GX 场景树。
					widgets.Tooltip(gxscene.Props{
						"trigger": func() any { return widgets.Button(gxscene.Props{"label": "悬停我", "variant": "secondary"}) },
						"content": func() any { return textNode("这是 Tooltip 提示", "#ffffff", "12") },
						"placement": "top",
					}),
					widgets.TimePicker(gxscene.Props{
						"value": func() any { return "09:30" },
						"onChange": widgets.BindValue(func(string) {}),
					}),
					widgets.FilePicker(gxscene.Props{
						"label":  "选择文件…",
						"onPick": widgets.BindPick(func([]string) {}),
					}),
					widgets.Table(gxscene.Props{
						"columns": []widgets.TableColumn{
							{Key: "k", Title: "键", Width: "120", Sortable: true},
							{Key: "v", Title: "值", Width: "120"},
						},
						"rows": func() any { return []any{[]string{"A", "1"}, []string{"B", "2"}} },
						"row-height": 32, "viewport-height": 72,
					}),
					// 底部说明
					textNode("IME: 点击输入框后，中文组合串/上屏字经回调写回 signal", "#2dd4bf", "13"),
				}
			},
		})
	})

	// rustbackend 专属扩展（BindApp 增量流 / SetDispatcher 输入映射）按能力
	// 经 WindowOf 取回窗口实例后调用——接入层接口保持后端无关。
	win := handle.WindowOf(app).(*rb.Window)

	win.BindApp(app)

	win.SetDispatcher(func(ev rb.InputEvent) {
		switch ev.Kind {
		case "ime_commit":
			// 走预制 Input 的受控通道：把字符派发到当前焦点输入框的 onInput。
			for _, ch := range ev.Text {
				widgets.InputChar(focusedInput(app), ch)
				break
			}
			fmt.Printf("[demo] ime_commit %q -> name=%q\n", ev.Text, name())
		case "ime_preedit":
			fmt.Printf("[demo] ime_preedit %q\n", ev.Text)
		case "click":
			fmt.Printf("[demo] click node=%s event=%s\n", ev.NodeID, ev.Event)
		case "focus":
			// 命中输入框 → 设焦点（预制 Input 的边框/光标由 signal 自动重绘）
			widgets.Focus(findNode(app, ev.NodeID))
			fmt.Printf("[demo] focus node=%s\n", ev.NodeID)
		case "backspace":
			widgets.InputBackspace(focusedInput(app))
		}
	})

	if err := app.Run(); err != nil {
		panic(err)
	}
}

// findNode 在应用当前帧场景树里按 nodeID 找节点。
func findNode(app *gxapp.App, id string) *gxscene.Node {
	return findNodeIn(app.Instance().Root(), id)
}

func findNodeIn(n *gxscene.Node, id string) *gxscene.Node {
	if n == nil {
		return nil
	}
	if n.ID.String() == id {
		return n
	}
	for _, c := range n.Children {
		if hit := findNodeIn(c, id); hit != nil {
			return hit
		}
	}
	return nil
}

// focusedInput 从当前帧取回聚焦的输入框节点（按稳定 id 定位）。
func focusedInput(app *gxapp.App) *gxscene.Node {
	return widgets.FocusedInputIn(app.Instance().Root())
}
