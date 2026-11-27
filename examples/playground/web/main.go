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
	"time"
	"fmt"
	"strings"
	"runtime"
	"syscall/js"

	gxweb "github.com/robertpanvip/gox-backend-web"
	"github.com/robertpanvip/gox-playground/ui"
	"github.com/robertpanvip/gox-playground/ui/interp"
	"github.com/robertpanvip/gox/pkg/goxlsp"
	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
	"strconv"
	"github.com/robertpanvip/gox/widgets"
)

// windowFirstLine 收集编辑器窗口首行的行号文本（诊断用）。
func windowFirstLine(v *gxscene.Instance) string {
	for _, c := range v.Root().Children {
		var texts []string
		var collect func(*gxscene.Node)
		collect = func(x *gxscene.Node) {
			if x.Type == gxscene.NodeText && x.Text != "" {
				texts = append(texts, x.Text)
			}
			for _, cc := range x.Children {
				collect(cc)
			}
		}
		collect(c)
		for _, tx := range texts {
			if _, err := strconv.Atoi(tx); err == nil {
				return tx
			}
		}
	}
	return "?"
}

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
	// 注意：web wasm 里不得 app.Close()——wasm 进程与页面同生命周期，
	// main 返回后事件循环（RAF/事件回调）继续驱动响应式；Close 会 teardownTree
	// 并让 closed 短路所有后续 Draw（表现为 UI 永久停在首帧，此前 playground
	// 全部交互失效的根因）。桌面端才有真正的进程退出语义。

	// Web 后端提供 RAF UIExecutor：同一帧内的多次状态变化合并为一次 DOM 提交，
	// 且事件回调不再被同步绘制阻塞。必须在 Render 之前设置。
	app.SetUIExecutor(win)

	// 统一帧调度（BACKEND_ARCHITECTURE_V2 阶段 1）：Signal 更新只标脏 + 预约
	// requestAnimationFrame，同帧合并为一次提交，无变化零 DOM 变更；动画 RAF
	// 续帧经同一时钟（与 postRAF 队列互补：时钟管帧时机，队列管任务）。
	app.SetFrameClock(win.FrameClock())

	// 输出双通道：解释器的 println / print / fmt.* 输出同时进浏览器
	// DevTools console（调试可查）与内置终端面板（页内可见）。
	console := js.Global().Get("console")
	interp.SetOutput(func(text string) {
		console.Call("log", strings.TrimRight(text, "\n"))
		ui.ConsolePrint(text)
	})

	// Run：解释当前编辑器源码生成场景树，替换预览后重渲染整棵树。
	// 源码在包级 sig 中，重渲染不会丢失编辑内容；app.Render 会统一绑定响应式。
	ui.SetRunHandler(func() {
		ui.ConsoleMark("Run")
		res, err := interp.Interpret(ui.EditorText())
		if err != nil {
			ui.ConsolePrint("运行错误: " + err.Error() + "\n")
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
	// debug 返回编辑器内部响应式状态快照（E2E 诊断内部值 vs UI 显示差异）。
	g.Set("debug", js.FuncOf(func(this js.Value, args []js.Value) any {
		return js.ValueOf(ui.Ed().DebugInfo())
	}))
	// frameState 返回最近一次 Present 的帧诊断（增量长度/全量标志/Change 摘要）。
	g.Set("frameState", js.FuncOf(func(this js.Value, args []js.Value) any {
		fs := win.ProbeFrame()
		fs["dump"] = app.DebugDumpStructural()
		fs["forResolves"] = gxscene.DebugForResolveCount()
		fs["window"] = widgets.DebugLastWindow()
		leaf, struct_, structEn, orphanID, orphanKind := gxscene.DebugSinkStats()
		fs["sinkLeaf"] = leaf
		fs["sinkStruct"] = struct_
		fs["sinkStructEnabled"] = structEn
		fs["orphanSinkID"] = orphanID
		fs["orphanKind"] = orphanKind
		fs["rootSinkID"] = app.DebugRootSinkID()
		return js.ValueOf(fs)
	}))
	// probe 诊断 wasm 下响应式各环节是否存活：
	//   effect   裸 signal 写 → effect 重跑？
	//   flush    写（有订阅者）→ AfterEffects 帧边界 hook 触发？
	//   appFlush 写编辑器 sig → app 的帧边界 hook 是否触发 + Draw 是否执行
	//            （对比 FrameVersion 前后变化）。
	g.Set("probe", js.FuncOf(func(this js.Value, args []js.Value) any {
		res := map[string]any{}
		// js.FuncOf 回调 panic 会终止整个 wasm runtime——顶层必须兜底捕获。
		defer func() {
			if r := recover(); r != nil {
				buf := make([]byte, 64<<10)
				n := runtime.Stack(buf, false)
				res["probePanic"] = fmt.Sprint(r)
				res["probePanicStack"] = string(buf[:n])
			}
		}()
		func() {
			defer func() {
				if r := recover(); r != nil {
					res["effect"] = "panic"
				}
			}()
			ran := false
			sig := signal.New(0)
			_ = signal.Effect(func() { _ = sig(); ran = true })
			ran = false
			sig(1)
			res["effect"] = ran
		}()
		func() {
			defer func() {
				if r := recover(); r != nil {
					res["lockedBatch"] = "panic:" + fmt.Sprint(r)
				}
			}()
			// LockedGetter（编译器 sig 字段档5 的信号源）Batch 内写入 → effect 重跑？
			// 与 EditorState.lineCount 的真实写路径同构。
			v := 0
			lg := signal.LockedGetter(func() int { return v }, func(x int) { v = x })
			ran := false
			h := signal.Effect(func() { _ = lg(); ran = true })
			ran = false
			signal.Batch(func() { lg(1) })
			h.Dispose()
			res["lockedBatch"] = ran
		}()
		func() {
			defer func() {
				if r := recover(); r != nil {
					res["miniScene"] = "panic:" + fmt.Sprint(r)
				}
			}()
			// scene 层最小复现：直连 signal.Effect 的编辑器 + SetText 扩窗。
			// 与 widgets 包 native 测试完全同构——用于区分「signal/scene 层在
			// js/wasm 环境失效」vs「app 包装层问题」。
			st := widgets.NewEditorState("package main\n\nfunc view() {}\n")
			v := widgets.Editor(st)
			v.BindLeafEffects(signal.Effect)
			v.BindStructural(signal.Effect, func() {})
			var sb strings.Builder
			for i := 0; i < 60; i++ {
				fmt.Fprintf(&sb, "line %d\n", i)
			}
			st.SetText(sb.String())
			maxLine := 0
			var collect func(*gxscene.Node)
			collect = func(x *gxscene.Node) {
				if n, err := strconv.Atoi(strings.TrimSpace(x.Text)); err == nil && x.Type == gxscene.NodeText && n > maxLine {
					maxLine = n
				}
				for _, c := range x.Children {
					collect(c)
				}
			}
			collect(v.Root())
			res["miniScene"] = maxLine // native 预期 > 11（窗口扩展成功）
		}()
		func() {
			defer func() {
				if r := recover(); r != nil {
					res["miniWrap"] = "panic:" + fmt.Sprint(r)
				}
			}()
			// 复刻 app 接线形态：包装 effect（requestDraw 计数）+ 双路径绑定。
			// 与 miniScene 的差异只有「wrap 包一层」——用于定位 app 层断点。
			st := widgets.NewEditorState("package main\n\nfunc view() {}\n")
			v := widgets.Editor(st)
			draws := 0
			wrap := func(body func()) signal.Computation {
				return signal.Effect(func() { body(); draws++ })
			}
			v.BindLeafEffects(wrap)
			v.BindStructural(wrap, func() { draws++ })
			var sb strings.Builder
			for i := 0; i < 60; i++ {
				fmt.Fprintf(&sb, "line %d\n", i)
			}
			before := draws
			st.SetText(sb.String())
			maxLine := 0
			var collect func(*gxscene.Node)
			collect = func(x *gxscene.Node) {
				if n, err := strconv.Atoi(strings.TrimSpace(x.Text)); err == nil && x.Type == gxscene.NodeText && n > maxLine {
					maxLine = n
				}
				for _, c := range x.Children {
					collect(c)
				}
			}
			collect(v.Root())
			res["miniWrap"] = maxLine
			res["miniWrapDraws"] = draws - before
		}()
		func() {
			defer func() {
				if r := recover(); r != nil {
					res["miniScroll"] = "panic:" + fmt.Sprint(r)
				}
			}()
			// 滚动链最小复现：ScrollLines(30) → scrollTop 写入 → For 窗口移动。
			st := widgets.NewEditorState("")
			// 300 行长文档
			var sb strings.Builder
			for i := 0; i < 300; i++ {
				fmt.Fprintf(&sb, "// row %d\n", i)
			}
			st.SetText(sb.String())
			v := widgets.Editor(st)
			v.BindLeafEffects(signal.Effect)
			v.BindStructural(signal.Effect, func() {})
			first := windowFirstLine(v)
			st.ScrollLines(30)
			after := windowFirstLine(v)
			res["miniScroll"] = "before=" + first + " after=" + after
		}()
		func() {
			defer func() {
				if r := recover(); r != nil {
					res["flush"] = "panic"
				}
			}()
			fired := false
			un := signal.AfterEffects(func() { fired = true })
			sig := signal.New(0)
			_ = signal.Effect(func() { _ = sig() })
			sig(1)
			un()
			res["flush"] = fired
		}()
		func() {
			defer func() {
				if r := recover(); r != nil {
					res["appFlush"] = "panic:" + fmt.Sprint(r)
				}
			}()
			before := app.FrameVersion()
			ui.EditorSetCursor(5, 0)
			time.Sleep(400 * time.Millisecond) // 等 RAF 下一帧执行
			after := app.FrameVersion()
			res["rafBefore"] = int64(before) // uint64 不被 js.ValueOf 支持
			res["rafAfter"] = int64(after)
			res["rafDelta"] = int64(after - before)
		}()
		func() {
			defer func() {
				if r := recover(); r != nil {
					res["post"] = "panic:" + fmt.Sprint(r)
				}
			}()
			// win.Post 是否真的到达浏览器 RAF：投一个置位任务，等 400ms 看标志。
			ran := false
			if err := win.Post(func() { ran = true }); err != nil {
				res["post"] = "err:" + err.Error()
				return
			}
			time.Sleep(400 * time.Millisecond)
			res["post"] = ran
			res["rafState"] = win.ProbeRAF()
			res["frameState"] = win.ProbeFrame()
		}()
		func() {
			defer func() {
				if r := recover(); r != nil {
					res["directDraw"] = "panic:" + fmt.Sprint(r)
				}
			}()
			// 绕过派发器同步 Draw：返回本次真实错误 + FrameVersion 增量。
			// statusBefore/After 读 Go 侧状态栏文本——区分「effect 没重跑（场景树
			// 未更新）」与「Present 增量提交失败（场景树更新了但 DOM 没变）」。
			res["statusBefore"] = ui.EditorStatus()
			before := app.FrameVersion()
			ui.EditorSetCursor(7, 0)
			res["statusAfterSet"] = ui.EditorStatus()
			if err := app.DrawNow(); err != nil {
				res["directDraw"] = "err:" + err.Error()
				res["directDrawDelta"] = int64(app.FrameVersion() - before)
				return
			}
			res["directDraw"] = "ok"
			res["directDrawDelta"] = int64(app.FrameVersion() - before)
			// Update() 强制重算叶子 getter 并写回节点（不依赖 signal 通知），
			// 返回实际写入的节点数：>0 说明场景树 getter 层正常、只是「effect
			// 重跑通知链」没把变化推进树；=0 说明连脏标记都没建立。
			res["updateCount"] = app.Update()
			if err := app.DrawNow(); err != nil {
				res["postUpdateDraw"] = "err:" + err.Error()
				return
			}
			res["postUpdateDraw"] = "ok"
		}()
		func() {
			defer func() {
				if r := recover(); r != nil {
					res["setTimeout"] = "panic:" + fmt.Sprint(r)
				}
			}()
			// 最小闭环：浏览器 setTimeout(fn, 30) 的 Go 回调是否执行。
			// RAF 与 setTimeout 同为「浏览器注册、浏览器回调」的异步路径。
			done := make(chan bool, 1)
			fn := js.FuncOf(func(this js.Value, args []js.Value) any {
				done <- true
				return nil
			})
			defer fn.Release()
			js.Global().Call("setTimeout", fn, 30)
			select {
			case <-done:
				res["setTimeout"] = true
			case <-time.After(800 * time.Millisecond):
				res["setTimeout"] = false
			}
		}()
		return js.ValueOf(res)
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
	g.Set("lspSignature", js.FuncOf(func(this js.Value, args []js.Value) any {
		line, col := 0, 0
		if len(args) >= 2 {
			line, col = args[0].Int(), args[1].Int()
		}
		return lsp.SignatureJSON(line, col)
	}))
	g.Set("lspHover", js.FuncOf(func(this js.Value, args []js.Value) any {
		line, col := 0, 0
		if len(args) >= 2 {
			line, col = args[0].Int(), args[1].Int()
		}
		return lsp.HoverJSON(line, col)
	}))
	g.Set("stdSource", js.FuncOf(func(this js.Value, args []js.Value) any {
		pkg, file := "", ""
		if len(args) >= 2 {
			pkg, file = args[0].String(), args[1].String()
		}
		return lsp.StdSourceJSON(pkg, file)
	}))
	g.Set("lspReferences", js.FuncOf(func(this js.Value, args []js.Value) any {
		line, col := 0, 0
		if len(args) >= 2 {
			line, col = args[0].Int(), args[1].Int()
		}
		return lsp.ReferencesJSON(line, col)
	}))
	g.Set("lspCodeActions", js.FuncOf(func(this js.Value, args []js.Value) any {
		return lsp.CodeActionsJSON()
	}))
	g.Set("lspTypeDefinition", js.FuncOf(func(this js.Value, args []js.Value) any {
		line, col := 0, 0
		if len(args) >= 2 {
			line, col = args[0].Int(), args[1].Int()
		}
		return lsp.TypeDefinitionJSON(line, col)
	}))
	g.Set("lspImplementation", js.FuncOf(func(this js.Value, args []js.Value) any {
		line, col := 0, 0
		if len(args) >= 2 {
			line, col = args[0].Int(), args[1].Int()
		}
		return lsp.ImplementationsJSON(line, col)
	}))
	g.Set("lspSemanticTokens", js.FuncOf(func(this js.Value, args []js.Value) any {
		return lsp.SemanticTokensJSON()
	}))
	g.Set("lspInlayHints", js.FuncOf(func(this js.Value, args []js.Value) any {
		return lsp.ParamHintsJSON()
	}))
	g.Set("lspResolveDoc", js.FuncOf(func(this js.Value, args []js.Value) any {
		pkg, name := "", ""
		if len(args) >= 2 {
			pkg, name = args[0].String(), args[1].String()
		}
		return lsp.ResolveDocJSON(pkg, name)
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
