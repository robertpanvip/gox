//go:build js && wasm

// web-widget-showcase：Apple macOS 风格组件库的 Web/wasm 展示。
//
// 与桌面 examples/widget-showcase 共享同一棵场景树（go X /showcase）：
// 组件排布、Apple token、预制矢量图标、明暗双主题完全一致，差异只在宿主
// 后端（Skia raster vs 浏览器 DOM）与尺寸策略（固定 pt vs 随窗口自适应）。
package main

import (
	"syscall/js"

	gxweb "github.com/robertpanvip/gox-backend-web"
	gxapp "github.com/robertpanvip/gox/runtime/app"
	"github.com/robertpanvip/gox/showcase"
	widgets "github.com/robertpanvip/gox/widgets"
)

func main() {
	// 默认暗色（与桌面 showcase 默认一致）；URL ?theme=light 可切浅色。
	theme := "dark"
	if href := js.Global().Get("location").Get("href").String(); len(href) >= 5 {
		for i := 0; i+5 <= len(href); i++ {
			if href[i:i+5] == "light" {
				theme = "light"
			}
		}
	}
	if theme == "light" {
		widgets.SetTheme(widgets.LightTheme)
	} else {
		widgets.SetTheme(widgets.DarkTheme)
	}

	win := gxweb.MustOpen("#app")
	app, err := gxapp.OpenWith(gxapp.WindowConfig{Title: "Widget Showcase · Apple macOS", Backend: "web"}, win)
	if err != nil {
		panic(err)
	}
	defer app.Close()
	// 用 Web 后端的 RAF UIExecutor 驱动帧投递（远程 P0/P3 决议：Web 复用浏览器事件循环）。
	app.SetUIExecutor(win)
	if err := app.Render(func() any { return showcase.Build(showcase.DefaultWidth, 1500) }); err != nil {
		panic(err)
	}
	if err := app.Run(); err != nil {
		panic(err)
	}

	// 主题切换按钮：挂到外部工具栏，重建场景树并设主题——验证明暗双主题在 Web 上同样对齐。
	registerThemeToggle(app)

	select {} // wasm main 返回即结束，保持事件回调可用
}

func registerThemeToggle(app *gxapp.App) {
	doc := js.Global().Get("document")
	bar := doc.Call("createElement", "div")
	bar.Call("setAttribute", "style",
		"position:fixed;top:12px;right:12px;z-index:9999;display:flex;gap:8px;font:13px -apple-system,system-ui,sans-serif;")
	doc.Get("body").Call("appendChild", bar)

	makeBtn := func(text string, t widgets.Theme) {
		b := doc.Call("createElement", "button")
		b.Call("setAttribute", "style",
			"height:28px;padding:0 12px;border-radius:6px;border:1px solid #ffffff22;background:#ffffff18;color:inherit;cursor:pointer;font:13px -apple-system,system-ui,sans-serif;")
		b.Set("textContent", text)
		b.Call("addEventListener", "click", js.FuncOf(func(this js.Value, args []js.Value) any {
			widgets.SetTheme(t)
			_ = app.Render(func() any { return showcase.Build(showcase.DefaultWidth, 1500) })
			_ = app.Draw()
			return nil
		}))
		bar.Call("appendChild", b)
	}
	makeBtn("浅色", widgets.LightTheme)
	makeBtn("深色", widgets.DarkTheme)
}
