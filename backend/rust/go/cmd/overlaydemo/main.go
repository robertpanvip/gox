// overlaydemo 验证 Popover 浮层在 Rust 后端的端到端渲染：默认打开一个下拉菜单，
// 便于无交互环境首帧截图验证浮层定位与层级。
package main

import (
	"fmt"

	rb "github.com/robertpanvip/gox-backend-rust"
	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	widgets "github.com/robertpanvip/gox/widgets"
)

func textNode(value string, color, size string) *gxscene.Instance {
	return gxscene.Render(gxscene.Template{
		Ops: []gxscene.Op{
			{Code: gxscene.OpElem, Arg: "span"},
			{Code: gxscene.OpAttr, Key: "color", Arg: color},
			{Code: gxscene.OpAttr, Key: "font-size", Arg: size},
			{Code: gxscene.OpChildText, Arg: value},
			{Code: gxscene.OpClose},
		},
	})
}

func menuItem(label string) *gxscene.Instance {
	return gxscene.Render(gxscene.Template{
		Ops: []gxscene.Op{
			{Code: gxscene.OpElem, Arg: "div"},
			{Code: gxscene.OpAttr, Key: "padding", Arg: "8"},
			{Code: gxscene.OpAttr, Key: "font-size", Arg: "15"},
			{Code: gxscene.OpAttr, Key: "color", Arg: widgets.ThemeText},
			{Code: gxscene.OpChildText, Arg: label},
			{Code: gxscene.OpClose},
		},
	})
}

func main() {
	fmt.Println("[overlaydemo] Rust backend:", rb.Version())

	// 默认打开，方便首帧截图验证浮层。
	state := widgets.NewPopoverState(true)

	// 接入层装配：gx.use(Backend) → Use(接口值) + 逐窗 Render。
	handle := gxapp.Use(rb.NewBackend())
	app, err := handle.Render(func() any {
		return widgets.Col(gxscene.Props{
			"width": "640", "height": "480", "padding": "24", "gap": "16",
			"bg": widgets.ThemeBg, "align": "center",
			"children": func() any {
				return []any{
					textNode("Popover / Select 浮层验证", widgets.ThemeAccent, "20"),
					// 触发按钮 + 浮层
					widgets.Popover(gxscene.Props{
						"state":     state,
						"placement": "bottom-start",
						"offset":    "8",
						"width":     "180",
						"trigger": func() any {
							return widgets.Button(gxscene.Props{"label": "选择项 ▾", "variant": "primary"})
						},
						"content": func() any {
							return widgets.Col(gxscene.Props{
								"gap": "2",
								"children": func() any {
									return []any{
										menuItem("选项一 · Alpha"),
										menuItem("选项二 · Beta"),
										menuItem("选项三 · Gamma"),
									}
								},
							})
						},
					}),
					textNode("上方内容层，用于验证浮层盖在其之上", widgets.ThemeMuted, "13"),
				}
			},
		})
	}, gxapp.WindowConfig{Title: "Overlay Demo", Width: 640, Height: 480})
	if err != nil {
		panic(err)
	}
	defer handle.Close()

	// rustbackend 专属扩展经 WindowOf 取回窗口实例后调用——接入层接口保持后端无关。
	win := handle.WindowOf(app).(*rb.Window)

	win.BindApp(app)

	// 事件回传：命中节点后派发到场景树，触发 Popover scrim 的 onClick 关闭浮层。
	win.SetDispatcher(func(ev rb.InputEvent) {
		fmt.Printf("[overlaydemo] event kind=%s node=%s event=%s\n", ev.Kind, ev.NodeID, ev.Event)
		if ev.Kind != "click" {
			return
		}
		n := findNode(app.Instance().Root(), ev.NodeID)
		fmt.Printf("[overlaydemo] findNode -> %v\n", n != nil)
		if n == nil {
			var dump func(*gxscene.Node, int)
			dump = func(nd *gxscene.Node, d int) {
				if d > 6 {
					return
				}
				fmt.Printf("[overlaydemo]   tree id=%s kind=%s role=%v\n", nd.ID, nd.Kind, nd.Props["gx-role"])
				for _, c := range nd.Children {
					dump(c, d+1)
				}
			}
			dump(app.Instance().Root(), 0)
		}
		if n != nil {
			n.DispatchBubble("onClick", gxscene.EventContext{
				Synthetic: &gxscene.SyntheticEvent{
					Type: gxscene.EvClick, NativeName: "onClick",
					Target: n, CurrentTarget: n,
					X: float64(ev.X), Y: float64(ev.Y),
				},
			})
		}
	})

	if err := handle.Run(); err != nil { // Host 唯一主循环（P4 目标态）：app.Run 已退化为 no-op
		panic(err)
	}
}

func findNode(n *gxscene.Node, id string) *gxscene.Node {
	if n == nil {
		return nil
	}
	if n.ID.String() == id {
		return n
	}
	for _, c := range n.Children {
		if hit := findNode(c, id); hit != nil {
			return hit
		}
	}
	return nil
}
