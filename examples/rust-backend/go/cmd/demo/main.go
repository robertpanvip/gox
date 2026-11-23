// PULSE Dashboard —— 用 GX 预制组件库 + Rust 后端渲染的端到端验证。
// 验证：预制控件跨后端复用（同一套 widgets 同款跑在 Skia 与 Rust 后端）、
// 首帧全量快照 + signal 更新走增量动作（SetText/SetAttr）+ IME 输入回传。
package main

import (
	"fmt"
	"time"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
	rb "github.com/robertpanvip/gox/examples/rust-backend/go"
	widgets "github.com/robertpanvip/gox-backend-skia/widgets"
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

	win := rb.NewWindow()
	app, err := gxapp.OpenWith(gxapp.WindowConfig{
		Title: "PULSE Dashboard", Width: 760, Height: 560,
	}, win)
	if err != nil {
		panic(err)
	}
	defer app.Close()

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
					// 输入框（预制 Input，稳定 id + 受控 value）
					widgets.Input(gxscene.Props{
						"id": "demo-input", "value": name,
						"width": "360", "placeholder": "点击后输入…",
						"onInput": widgets.BindInput(func(s string) { name(s) }),
					}),
					// 底部说明
					textNode("IME: 点击输入框后，中文组合串/上屏字经回调写回 signal", "#2dd4bf", "13"),
				}
			},
		})
	})

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
