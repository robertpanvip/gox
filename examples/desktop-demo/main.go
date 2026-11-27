// Gox 桌面 Demo —— PULSE 实时数据看板（预制控件版）
// 用 GX 编写，控件外观全部来自 skia 后端预制的 widgets 库，核心运行时零改动。
// 演示：signal 源 + computed 派生 + 预制控件库 + 真实键盘输入 + 事件写 signal + goroutine 数据流。
package main

import (
	"fmt"
	rb "github.com/robertpanvip/gox-backend-rust"
	gxapp "github.com/robertpanvip/gox-core/runtime/app"
	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
	"github.com/robertpanvip/gox-core/runtime/signal"
	widgets "github.com/robertpanvip/gox-core/widgets"
	"time"
)

// Brand 品牌标题（业务专属，不进通用控件库）。
func Brand(props gxscene.Props) *gxscene.Instance {
	return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{gxscene.Op{Code: gxscene.OpElem, Arg: "div"}, gxscene.Op{Code: gxscene.OpAttr, Arg: "column", Key: "flex-direction"}, gxscene.Op{Code: gxscene.OpAttr, Arg: "6", Key: "gap"}, gxscene.Op{Code: gxscene.OpElem, Arg: "div"}, gxscene.Op{Code: gxscene.OpAttr, Arg: "#eef3fb", Key: "color"}, gxscene.Op{Code: gxscene.OpAttr, Arg: "30", Key: "font-size"}, gxscene.Op{Code: gxscene.OpAttr, Arg: "bold", Key: "weight"}, gxscene.Op{Code: gxscene.OpChildText, Arg: "PULSE"}, gxscene.Op{Code: gxscene.OpClose}, gxscene.Op{Code: gxscene.OpElem, Arg: "div"}, gxscene.Op{Code: gxscene.OpAttr, Arg: "#6b7a94", Key: "color"}, gxscene.Op{Code: gxscene.OpAttr, Arg: "13", Key: "font-size"}, gxscene.Op{Code: gxscene.OpChildText, Arg: "Real-time signal dashboard · widgets"}, gxscene.Op{Code: gxscene.OpClose}, gxscene.Op{Code: gxscene.OpClose}}, N: 0})
}

func main() {
	var count = signal.New(42)
	var name = signal.New("operator")
	doubled := signal.Computed(func() int { return count() * 2 })
	loadPct := signal.Computed(func() int { return (count() % 50) * 2 })

	// 模拟实时数据流：每 1.4 秒 +1
	go func() {
		for {
			time.Sleep(1400 * time.Millisecond)
			count(count() + 1)
		}
	}()

	// V2 后端装配（阶段 4 切换）：Rust 渲染 + winit 窗口，替代原 GLFW+Skia glwindow。
	// V2 接入层装配：gx.use(Backend) → Use(接口值) + 逐窗 Render；后端按
	// gxapp.Backend 接口形状注入，核心零依赖具体后端。
	handle := gxapp.Use(rb.NewBackend())

	// 紧凑单行规避 GX 编译器跨行缩进边界；signal 写用 () 显式读。
	// 所有按钮/卡片/徽章/进度条/输入框均来自后端预制 widgets 库——JSX 只描述语义。
	build := func() any {
		return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{gxscene.Op{Code: gxscene.OpElem, Arg: "div"}, gxscene.Op{Code: gxscene.OpAttr, Arg: "#0b1020", Key: "bg"}, gxscene.Op{Code: gxscene.OpAttr, Arg: "640", Key: "height"}, gxscene.Op{Code: gxscene.OpAttr, Arg: "28", Key: "padding"}, gxscene.Op{Code: gxscene.OpAttr, Arg: "column", Key: "flex-direction"}, gxscene.Op{Code: gxscene.OpAttr, Arg: "18", Key: "gap"}, gxscene.Op{Code: gxscene.OpComp, Arg: "widgets.row", Slot: 0}, gxscene.Op{Code: gxscene.OpComp, Arg: "widgets.row", Slot: 1}, gxscene.Op{Code: gxscene.OpComp, Arg: "widgets.card", Slot: 2}, gxscene.Op{Code: gxscene.OpComp, Arg: "widgets.card", Slot: 3}, gxscene.Op{Code: gxscene.OpComp, Arg: "widgets.row", Slot: 4}, gxscene.Op{Code: gxscene.OpClose}}, N: 5}, func() any {
			return widgets.Row(gxscene.Props{"gap": "12", "align": "center", "justify": "space-between", "children": func() any {
				return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{gxscene.Op{Code: gxscene.OpElem, Arg: "#frag"}, gxscene.Op{Code: gxscene.OpComp, Arg: "Brand", Slot: 0}, gxscene.Op{Code: gxscene.OpComp, Arg: "widgets.badge", Slot: 1}, gxscene.Op{Code: gxscene.OpClose}}, N: 2}, func() any { return Brand(gxscene.Props{}) }, func() any { return widgets.Badge(gxscene.Props{"label": "LIVE", "tone": "green", "dot": true}) })
			}})
		}, func() any {
			return widgets.Row(gxscene.Props{"gap": "14", "children": func() any {
				return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{gxscene.Op{Code: gxscene.OpElem, Arg: "#frag"}, gxscene.Op{Code: gxscene.OpComp, Arg: "widgets.card", Slot: 0}, gxscene.Op{Code: gxscene.OpComp, Arg: "widgets.card", Slot: 1}, gxscene.Op{Code: gxscene.OpComp, Arg: "widgets.card", Slot: 2}, gxscene.Op{Code: gxscene.OpClose}}, N: 3}, func() any {
					return widgets.Card(gxscene.Props{"title": "REQUESTS", "tone": "cyan", "width": "196", "children": func() any {
						return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{gxscene.Op{Code: gxscene.OpElem, Arg: "#frag"}, gxscene.Op{Code: gxscene.OpElem, Arg: "b"}, gxscene.Op{Code: gxscene.OpAttr, Arg: "#2dd4bf", Key: "color"}, gxscene.Op{Code: gxscene.OpAttr, Arg: "34", Key: "font-size"}, gxscene.Op{Code: gxscene.OpAttr, Arg: "bold", Key: "weight"}, gxscene.Op{Code: gxscene.OpDynChild, Slot: 0}, gxscene.Op{Code: gxscene.OpClose}, gxscene.Op{Code: gxscene.OpClose}}, N: 1}, func() any { return fmt.Sprint(count()) })
					}})
				}, func() any {
					return widgets.Card(gxscene.Props{"title": "THROUGHPUT", "tone": "purple", "width": "196", "children": func() any {
						return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{gxscene.Op{Code: gxscene.OpElem, Arg: "#frag"}, gxscene.Op{Code: gxscene.OpElem, Arg: "b"}, gxscene.Op{Code: gxscene.OpAttr, Arg: "#a78bfa", Key: "color"}, gxscene.Op{Code: gxscene.OpAttr, Arg: "34", Key: "font-size"}, gxscene.Op{Code: gxscene.OpAttr, Arg: "bold", Key: "weight"}, gxscene.Op{Code: gxscene.OpDynChild, Slot: 0}, gxscene.Op{Code: gxscene.OpClose}, gxscene.Op{Code: gxscene.OpClose}}, N: 1}, func() any { return fmt.Sprint(doubled()) })
					}})
				}, func() any {
					return widgets.Card(gxscene.Props{"title": "UPTIME", "tone": "orange", "width": "196", "children": func() any {
						return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{gxscene.Op{Code: gxscene.OpElem, Arg: "#frag"}, gxscene.Op{Code: gxscene.OpElem, Arg: "b"}, gxscene.Op{Code: gxscene.OpAttr, Arg: "#fb923c", Key: "color"}, gxscene.Op{Code: gxscene.OpAttr, Arg: "34", Key: "font-size"}, gxscene.Op{Code: gxscene.OpAttr, Arg: "bold", Key: "weight"}, gxscene.Op{Code: gxscene.OpChildText, Arg: "99.98%"}, gxscene.Op{Code: gxscene.OpClose}, gxscene.Op{Code: gxscene.OpClose}}, N: 0})
					}})
				})
			}})
		}, func() any {
			return widgets.Card(gxscene.Props{"title": "SYSTEM LOAD", "tone": "info", "width": "624", "children": func() any {
				return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{gxscene.Op{Code: gxscene.OpElem, Arg: "#frag"}, gxscene.Op{Code: gxscene.OpComp, Arg: "widgets.progress", Slot: 0}, gxscene.Op{Code: gxscene.OpClose}}, N: 1}, func() any {
					return widgets.Progress(gxscene.Props{"value": func() any { return loadPct() }, "width": "560", "tone": "cyan", "label": func() any { return fmt.Sprint(loadPct()) + "%" }})
				})
			}})
		}, func() any {
			return widgets.Card(gxscene.Props{"title": "OPERATOR CONSOLE", "tone": "primary", "width": "624", "children": func() any {
				return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{gxscene.Op{Code: gxscene.OpElem, Arg: "#frag"}, gxscene.Op{Code: gxscene.OpComp, Arg: "widgets.input", Slot: 0}, gxscene.Op{Code: gxscene.OpElem, Arg: "span"}, gxscene.Op{Code: gxscene.OpAttr, Arg: "#8b9bb4", Key: "color"}, gxscene.Op{Code: gxscene.OpAttr, Arg: "13", Key: "font-size"}, gxscene.Op{Code: gxscene.OpChildText, Arg: "Hello, "}, gxscene.Op{Code: gxscene.OpDynChild, Slot: 1}, gxscene.Op{Code: gxscene.OpChildText, Arg: " — the text box is backed by a signal (type and press backspace)."}, gxscene.Op{Code: gxscene.OpClose}, gxscene.Op{Code: gxscene.OpClose}}, N: 2}, func() any {
					return widgets.Input(gxscene.Props{"id": "operator", "value": func() any { return name() }, "placeholder": "Type your name…", "width": "300", "onInput": func() any { return widgets.BindInput(func(s string) { name(s) }) }})
				}, func() any { return name() })
			}})
		}, func() any {
			return widgets.Row(gxscene.Props{"gap": "12", "children": func() any {
				return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{gxscene.Op{Code: gxscene.OpElem, Arg: "#frag"}, gxscene.Op{Code: gxscene.OpComp, Arg: "widgets.button", Slot: 0}, gxscene.Op{Code: gxscene.OpComp, Arg: "widgets.button", Slot: 1}, gxscene.Op{Code: gxscene.OpComp, Arg: "widgets.button", Slot: 2}, gxscene.Op{Code: gxscene.OpClose}}, N: 3}, func() any {
					return widgets.Button(gxscene.Props{"label": "+ BOOST", "variant": "primary", "onClick": func() any { return gxscene.Handler(func(ctx gxscene.EventContext) { count(count() + 10) }) }})
				}, func() any {
					return widgets.Button(gxscene.Props{"label": "- RESET", "variant": "secondary", "onClick": func() any { return gxscene.Handler(func(ctx gxscene.EventContext) { count(count() - 10) }) }})
				}, func() any {
					return widgets.Button(gxscene.Props{"label": "CLEAR", "variant": "danger", "onClick": func() any { return gxscene.Handler(func(ctx gxscene.EventContext) { name("") }) }})
				})
			}})
		})
	}

	app, err := handle.Render(build, gxapp.WindowConfig{Title: "PULSE Dashboard", Width: 760, Height: 640})
	if err != nil {
		panic(err)
	}
	defer handle.Close() // Handle.Close 级联：窗口 Close + 后端 Shutdown

	// rustbackend 专属扩展（BindApp 增量流 / SetDispatcher 输入映射）按能力
	// 经 WindowOf 取回窗口实例后调用——接入层接口保持后端无关。
	win := handle.WindowOf(app).(*rb.Window)

	// 增量动作流接线：Render 后建立基线，之后 signal 更新走 GRX3 delta。
	win.BindApp(app)

	// 输入接线：Rust 侧命中/IME 事件映射到预制 Input 的受控通道（装配层只翻译，
	// 编辑逻辑零条件编译——与 Web 端同一组 widgets 命令）。
	win.SetDispatcher(func(ev rb.InputEvent) {
		switch ev.Kind {
		case "ime_commit":
			for _, ch := range ev.Text {
				widgets.InputChar(focusedInput(app), ch)
			}
		case "ime_preedit":
			// 组合串预编辑：预制 Input 暂不渲染 preedit，只记录不消费。
		case "focus":
			widgets.Focus(findNode(app, ev.NodeID))
		case "backspace":
			widgets.InputBackspace(focusedInput(app))
		}
	})

	handle.Run() // Host 唯一主循环（P4 目标态）：app.Run 退化为 no-op，必须走 Handle.Run
}

// findNode 在当前帧场景树里按 NodeID 找节点（焦点事件定位用）。
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

// focusedInput 从当前帧取回聚焦的输入框节点。
func focusedInput(app *gxapp.App) *gxscene.Node {
	return widgets.FocusedInputIn(app.Instance().Root())
}
