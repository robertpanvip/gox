//go:build js && wasm

// GX Playground —— 网页端装配（web 后端 + wasm）。
//
// 与桌面端共用同一份 GX 场景树（ui.Playground）。差别仅在：
//   - 后端：gxweb（把场景树渲染成真实 DOM）
//   - 输入：前端 TS 监听键盘，调本文件暴露的 Gox API → GX 编辑命令
//   - LSP：goxlsp 会话编译进同一个 wasm，无服务端，诊断/补全/hover 全在本地
//
// 编辑逻辑（editor.gx）两端完全相同，零条件编译。
package main

import (
	"syscall/js"

	gxweb "github.com/robertpanvip/gox-backend-web"
	"github.com/robertpanvip/gox-playground/ui"
	"github.com/robertpanvip/gox-playground/ui/interp"
	"github.com/robertpanvip/gox/pkg/goxlsp"
	gxapp "github.com/robertpanvip/gox/runtime/app"
	"github.com/robertpanvip/gox/widgets"
)

// lsp 是浏览器内的语言服务会话。只跑编译器 + GX 语义规则，不启动 go build，
// 因此 file:// 双击打开、完全离线也能诊断/补全。
var lsp = goxlsp.New()

func main() {
	// 初始主题读 URL ?theme=light（便于分享浅色链接与视觉回归截图）。
	// 默认跟随 Apple 深色；显式 light 切浅色。body 属性同步视口底色。
	if js.Global().Get("location").Get("search").String() == "?theme=light" {
		widgets.SetTheme(widgets.LightTheme)
		js.Global().Get("document").Get("body").Call("setAttribute", "data-theme", "light")
		ui.SetLightTheme(true)
	}

	win := gxweb.MustOpen("#app")
	app, err := gxapp.OpenWith(gxapp.WindowConfig{Title: "GX Playground (Web)", Backend: "web"}, win)
	if err != nil {
		panic(err)
	}
	defer app.Close()

	// Web 后端提供 RAF UIExecutor：同一帧内的多次状态变化合并为一次 DOM 提交，
	// 且事件回调不再被同步绘制阻塞。必须在 Render 之前设置。
	app.SetUIExecutor(win)

	// Run：解释当前编辑器源码生成场景树，替换预览后重渲染整棵树。
	// 源码在包级 sig 中，重渲染不会丢失编辑内容；app.Render 会统一绑定响应式。
	ui.SetRunHandler(func() {
		res, err := interp.Interpret(ui.EditorText())
		if err != nil {
			ui.SetRunResult(nil, err.Error())
		} else {
			ui.SetRunResult(res.Instance, "")
		}
		if rerr := app.Render(func() any { return ui.Playground() }); rerr != nil {
			ui.SetRunResult(nil, "重渲染失败: "+rerr.Error())
		}
	})

	// 主题切换：Apple 明暗双主题，同步 body 底色避免宽视口露出异色边。
	ui.SetThemeHandler(func(light bool) {
		if light {
			widgets.SetTheme(widgets.LightTheme)
			js.Global().Get("document").Get("body").Call("setAttribute", "data-theme", "light")
		} else {
			widgets.SetTheme(widgets.DarkTheme)
			js.Global().Get("document").Get("body").Call("setAttribute", "data-theme", "dark")
		}
		if rerr := app.Render(func() any { return ui.Playground() }); rerr != nil {
			panic(rerr)
		}
	})

	ui.LoadSample(0)
	lsp.SetSource(ui.EditorText())
	if err := app.Render(func() any { return ui.Playground() }); err != nil {
		panic(err)
	}
	if err := app.Run(); err != nil {
		panic(err)
	}

	// 暴露给严格 TS 前端的 API。所有返回值都是 JSON 字符串或纯字符串，
	// 避免 syscall/js 逐字段转换，也让 TS 侧保持零 any。
	g := js.Global().Get("Gox")
	if !g.Truthy() {
		g = js.ValueOf(map[string]any{})
		js.Global().Set("Gox", g)
	}
	// refreshLSP 在每次源码变化后同步 LSP 会话，保证诊断/补全看到最新文本。
	refreshLSP := func() { lsp.SetSource(ui.EditorText()) }

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
			if len(key) > 0 {
				ui.EditorInsert(key)
			}
		}
		refreshLSP()
		return nil
	}))
	// undo / redo 由前端 Ctrl+Z / Ctrl+Shift+Z 触发。wasm 侧恢复源码后同步 LSP，
	// 保证撤销后的诊断/补全立即反映回退后的文本。
	g.Set("undo", js.FuncOf(func(this js.Value, args []js.Value) any {
		ui.EditorUndo()
		refreshLSP()
		return nil
	}))
	g.Set("redo", js.FuncOf(func(this js.Value, args []js.Value) any {
		ui.EditorRedo()
		refreshLSP()
		return nil
	}))
	// 虚拟滚动：前端 wheel 事件把 deltaY 换算成行数传入，移动可见窗口。
	g.Set("scrollLines", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 {
			ui.EditorScrollLines(args[0].Float())
		}
		return nil
	}))
	// run 解释当前源码并运行（供 Ctrl+Enter 快捷键；工具栏 Run 按钮走 GX 节点）。
	g.Set("run", js.FuncOf(func(this js.Value, args []js.Value) any {
		ui.TriggerRun()
		return nil
	}))
	g.Set("focus", js.FuncOf(func(this js.Value, args []js.Value) any {
		ui.EditorFocus()
		return nil
	}))
	g.Set("sample", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 {
			ui.EditorSetText(args[0].String())
			refreshLSP()
		}
		return nil
	}))
	// cursor 返回编辑器光标（0 基行/列），供 TS 侧定位补全与 hover。
	g.Set("cursor", js.FuncOf(func(this js.Value, args []js.Value) any {
		return js.ValueOf(map[string]any{
			"line": ui.EditorLine(),
			"col":  ui.EditorCol(),
		})
	}))
	// pickColumn 是点击落点换算：给定行号与"相对代码行左缘的 x 像素"，
	// 用与 caret 渲染完全相同的 runeCells × charWidth 算出列（Ace 风格同源度量）。
	// 前端不再自己 canvas 测宽算列，点哪列就落哪列。
	g.Set("pickColumn", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) >= 2 {
			return ui.PickColumn(args[0].Int(), args[1].Float())
		}
		return 0
	}))
	// setCursor 把光标定位到指定行/列（点击代码行时调用，列按字符计）。
	g.Set("setCursor", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) >= 2 {
			ui.EditorSetCursor(args[0].Int(), args[1].Int())
		}
		return nil
	}))
	// ---- 选区 API（引擎在 ui/editor.gx「选区引擎」一节，桌面端可直接复用同一组）----
	g.Set("selectWord", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) >= 2 {
			ui.EditorSelectWord(args[0].Int(), args[1].Int())
		}
		return nil
	}))
	g.Set("selectLine", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) >= 1 {
			ui.EditorSelectLine(args[0].Int())
		}
		return nil
	}))
	g.Set("setSelect", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) >= 2 {
			ui.EditorSetSelect(args[0].Int(), args[1].Int())
		}
		return nil
	}))
	g.Set("extendTo", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) >= 2 {
			ui.EditorExtendTo(args[0].Int(), args[1].Int())
		}
		return nil
	}))
	g.Set("selectAll", js.FuncOf(func(this js.Value, args []js.Value) any {
		ui.EditorSelectAll()
		return nil
	}))
	g.Set("clearSelection", js.FuncOf(func(this js.Value, args []js.Value) any {
		ui.EditorClearSelection()
		return nil
	}))
	g.Set("selectedText", js.FuncOf(func(this js.Value, args []js.Value) any {
		return ui.EditorSelectedText()
	}))
	g.Set("deleteSelection", js.FuncOf(func(this js.Value, args []js.Value) any {
		ui.EditorDeleteSelection()
		return nil
	}))
	g.Set("paste", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) >= 1 {
			ui.EditorPaste(args[0].String())
			refreshLSP()
		}
		return nil
	}))
	// setCharWidth 注入实测的等宽字体单格宽度（canvas.measureText("0")），
	// 供点击换算与 caret 定位共用；未注入时用 13px monospace 默认值。
	g.Set("setCharWidth", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) >= 1 {
			ui.SetCharWidth(args[0].Float())
		}
		return nil
	}))
	// setWideWidth 注入全角/CJK 字符的实测像素宽度（canvas.measureText("中")），
	// 与 setCharWidth 一起构成点击换算与 caret 定位共用的字体度量。
	g.Set("setWideWidth", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) >= 1 {
			ui.SetWideWidth(args[0].Float())
		}
		return nil
	}))
	// settle / idle 控制 caret 闪烁：交互后常亮，静置后恢复闪烁（Ace 的 restartTimer）。
	g.Set("settle", js.FuncOf(func(this js.Value, args []js.Value) any {
		ui.EditorSettle()
		return nil
	}))
	g.Set("idle", js.FuncOf(func(this js.Value, args []js.Value) any {
		ui.EditorIdle()
		return nil
	}))
	// insert 把补全项文本插入光标处（补全浮层点击后调用）。
	g.Set("insert", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 {
			ui.EditorInsert(args[0].String())
			refreshLSP()
		}
		return nil
	}))
	// setSource 整体替换源码（格式化后回写）。
	g.Set("setSource", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 {
			ui.EditorSetText(args[0].String())
			refreshLSP()
		}
		return nil
	}))

	// ---- 内建 LSP（无服务端） ----
	g.Set("lspDiagnostics", js.FuncOf(func(this js.Value, args []js.Value) any {
		return lsp.DiagnosticsJSON()
	}))
	g.Set("lspComplete", js.FuncOf(func(this js.Value, args []js.Value) any {
		line, col := 0, 0
		if len(args) >= 2 {
			line, col = args[0].Int(), args[1].Int()
		}
		return lsp.CompleteJSON(line, col)
	}))
	g.Set("lspHover", js.FuncOf(func(this js.Value, args []js.Value) any {
		line, col := 0, 0
		if len(args) >= 2 {
			line, col = args[0].Int(), args[1].Int()
		}
		return lsp.HoverJSON(line, col)
	}))
	g.Set("lspDefinition", js.FuncOf(func(this js.Value, args []js.Value) any {
		line, col := 0, 0
		if len(args) >= 2 {
			line, col = args[0].Int(), args[1].Int()
		}
		return lsp.DefinitionJSON(line, col)
	}))
	g.Set("lspSymbols", js.FuncOf(func(this js.Value, args []js.Value) any {
		return lsp.DocumentSymbolsJSON()
	}))
	g.Set("lspFormat", js.FuncOf(func(this js.Value, args []js.Value) any {
		return lsp.Format()
	}))

	// wasm 里 main 返回即结束，用阻塞通道保持事件回调可用。
	select {}
}
