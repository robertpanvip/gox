//go:build js && wasm

// GX Playground —— 网页端装配（web 后端 + wasm）。
//
// 与桌面端共用同一份 GX 场景树（ui.Playground）。差别仅在：
//   - 后端：gxweb（把场景树渲染成真实 DOM）
//   - 输入：前端 TS 监听键盘，调本文件暴露的 Gox.key() → GX 编辑命令
//
// 编辑逻辑（editor.gx）两端完全相同，零条件编译。
package main

import (
	"syscall/js"

	gxweb "github.com/robertpanvip/gox-backend-web"
	gxapp "github.com/robertpanvip/gox/runtime/app"
	"github.com/robertpanvip/gox-playground/ui"
)

func main() {
	win := gxweb.MustOpen("#app")
	app, err := gxapp.OpenWith(gxapp.WindowConfig{Title: "GX Playground (Web)", Backend: "web"}, win)
	if err != nil {
		panic(err)
	}
	defer app.Close()

	ui.LoadSample(0)
	if err := app.Render(func() any { return ui.Playground() }); err != nil {
		panic(err)
	}
	if err := app.Run(); err != nil {
		panic(err)
	}

	// 暴露给严格 TS 前端的 API：键盘输入 → GX 编辑命令。
	g := js.Global().Get("Gox")
	if !g.Truthy() {
		g = js.ValueOf(map[string]any{})
		js.Global().Set("Gox", g)
	}
	g.Set("key", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		key := args[0].String()
		switch key {
		case "Backspace":
			ui.EditorBackspace()
		case "Enter":
			ui.EditorNewline()
		case "ArrowUp":
			ui.EditorMove(-1, 0)
		case "ArrowDown":
			ui.EditorMove(1, 0)
		case "ArrowLeft":
			ui.EditorMove(0, -1)
		case "ArrowRight":
			ui.EditorMove(0, 1)
		default:
			// 可打印字符（含中文）：整串插入。
			if len(key) > 0 {
				ui.EditorInsert(key)
			}
		}
		return nil
	}))
	g.Set("focus", js.FuncOf(func(this js.Value, args []js.Value) any {
		ui.EditorFocus()
		return nil
	}))
	g.Set("sample", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 {
			ui.EditorSetText(args[0].String())
		}
		return nil
	}))
	// wasm 里 main 返回即结束，用阻塞通道保持事件回调可用。
	select {}
}
