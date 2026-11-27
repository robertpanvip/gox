// Popover 浮层验证 demo（V2 rustbackend）：默认打开一个下拉菜单，首帧截图即可
// 验证浮层定位、层级与遮罩命中，与 backend/rust/go/cmd/overlaydemo 场景一致。
//
// V2 接入层装配：gx.use(Backend) → Use(接口值) + Handle.Render / Handle.Run
// （Host 唯一主循环）。GX_AUTOCLOSE_MS=N 支持脚本化截图验收。
package main

import (
	"fmt"
	"os"
	"time"

	rb "github.com/robertpanvip/gox-backend-rust"
	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	widgets "github.com/robertpanvip/gox/widgets"
)

func textNode(value, color, size string) *gxscene.Instance {
	return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "span"},
		{Code: gxscene.OpAttr, Key: "color", Arg: color},
		{Code: gxscene.OpAttr, Key: "font-size", Arg: size},
		{Code: gxscene.OpChildText, Arg: value},
		{Code: gxscene.OpClose},
	}})
}

func menuItem(label string) *gxscene.Instance {
	return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpAttr, Key: "padding", Arg: "8"},
		{Code: gxscene.OpAttr, Key: "font-size", Arg: "15"},
		{Code: gxscene.OpAttr, Key: "color", Arg: widgets.ThemeText},
		{Code: gxscene.OpChildText, Arg: label},
		{Code: gxscene.OpClose},
	}})
}

func main() {
	state := widgets.NewPopoverState(true)
	handle := gxapp.Use(rb.NewBackend())
	app, err := handle.Render(func() any {
		return widgets.Col(gxscene.Props{
			"width": "640", "height": "480", "padding": "24", "gap": "16",
			"bg": widgets.ThemeBg, "align": "center",
			"children": func() any {
				return []any{
					textNode("Popover / Select 浮层验证 (Rust)", widgets.ThemeAccent, "20"),
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
	}, gxapp.WindowConfig{Title: "Overlay Demo (Rust)", Width: 640, Height: 480})
	if err != nil {
		panic(err)
	}
	defer handle.Close() // Handle.Close 级联：窗口 Close + 后端 Shutdown

	win := handle.WindowOf(app).(*rb.Window)
	win.BindApp(app) // GRX3 增量流：Render 后建基线，signal 更新走 delta

	// GX_AUTOCLOSE_MS：到时自动请求关窗（脚本化截图验收）。
	if v := os.Getenv("GX_AUTOCLOSE_MS"); v != "" {
		var ms int
		if cnt, perr := fmt.Sscanf(v, "%d", &ms); perr == nil && cnt > 0 && ms > 0 {
			time.AfterFunc(time.Duration(ms)*time.Millisecond, win.RequestClose)
		}
	}

	handle.Run() // Host 唯一主循环（P4 目标态）
}
