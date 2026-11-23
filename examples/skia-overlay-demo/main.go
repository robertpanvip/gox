// Skia 后端 Popover 浮层验证 demo：默认打开一个下拉菜单，首帧截图即可验证
// 浮层定位、层级与遮罩命中，与 Rust 后端 overlaydemo 场景一致。
package main

import (
	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	widgets "github.com/robertpanvip/gox-backend-skia/widgets"
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
	app, err := gxapp.OpenWith(gxapp.WindowConfig{Title: "Skia Overlay Demo", Width: 640, Height: 480}, openGL())
	if err != nil {
		panic(err)
	}
	defer app.Close()

	app.Render(func() any {
		return widgets.Col(gxscene.Props{
			"width": "640", "height": "480", "padding": "24", "gap": "16",
			"bg": widgets.ThemeBg, "align": "center",
			"children": func() any {
				return []any{
					textNode("Popover / Select 浮层验证 (Skia)", widgets.ThemeAccent, "20"),
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
	})
	app.Run()
}
