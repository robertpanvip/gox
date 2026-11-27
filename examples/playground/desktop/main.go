// GX Playground —— 桌面端装配（V2 rustbackend）。
//
// 这个 main.go 是纯装配样板：唯一业务代码是 ui.Playground()，来自
// 与网页端共用的 GX 源码（ui/app.gx + ui/editor.gx，编译产物 ui/*.go）。
// 键盘/鼠标输入由 input_translate.go 翻译成 GX 编辑命令，编辑逻辑零条件编译。
package main

import (
	"encoding/json"
	"os"
	"time"

	rb "github.com/robertpanvip/gox-backend-rust"
	"github.com/robertpanvip/gox-playground/ui"
	"github.com/robertpanvip/gox-playground/ui/interp"
	gxapp "github.com/robertpanvip/gox-core/runtime/app"
	"github.com/robertpanvip/gox-core/widgets"
)

func main() {
	// V2 接入层装配：gx.use(Backend) → Use(接口值) + 逐窗 Render。
	handle := gxapp.Use(rb.NewBackend())
	app, err := handle.Render(func() any { return ui.Playground() },
		gxapp.WindowConfig{Title: "GX Playground (Desktop)", Width: 1400, Height: 860})
	if err != nil {
		panic(err)
	}
	defer handle.Close() // Handle.Close 级联：窗口 Close + 后端 Shutdown

	// 统一帧调度（BACKEND_ARCHITECTURE_V2 阶段 1）：Signal 更新只标脏 + 唤醒
	// 事件泵，同轮次合并为一帧；无变化零绘制；动画 RAF 续帧经同一时钟。
	app.SetFrameClock(handle.Host().WindowOf(app).(*rb.Window).FrameClock())

	// rustbackend 专属扩展：SetDispatcher 接输入翻译层（V2 输入面：key 携带
	// 键名与 mods、pointer 分 down/move/up、wheel 仅编辑器滚动回传）。
	bridge := &inputBridge{app: app}
	bridge.win = handle.WindowOf(app).(*rb.Window)
	bridge.win.SetDispatcher(bridge.dispatch)

	// GUI 端到端测试钩子：设置 GOX_PLAYGROUND_STATE_FILE 后，每 100ms 把 UI 状态
	// 快照写到指定文件，供脚本在真实鼠标/键盘操作后硬断言（点击定位、拖动选区、
	// 滚轮、主题、Run 等状态进程外不可见）。快照在 UI 线程生成，避免与编辑并发；
	// 未设置环境变量时完全不启用，正常用户零开销。
	if stateFile := os.Getenv("GOX_PLAYGROUND_STATE_FILE"); stateFile != "" {
		win := bridge.win
		go func() {
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for range ticker.C {
				path := stateFile
				if err := win.Post(func() {
					data := ui.DebugStateJSON()
					var snap map[string]any
					if json.Unmarshal(data, &snap) == nil {
						ww, wh := win.DebugWindowSize()
						snap["window_w"] = ww
						snap["window_h"] = wh
						snap["boxes"] = win.DebugAllBoxes()
						if merged, err := json.Marshal(snap); err == nil {
							data = merged
						}
					}
					// 原子替换，避免脚本读到半截 JSON。
					tmp := path + ".tmp"
					if os.WriteFile(tmp, data, 0644) == nil {
						_ = os.Rename(tmp, path)
					}
				}); err != nil {
					return
				}
			}
		}()
	}

	// 字符宽度实测注入：编辑器点击换算/光标渲染依赖字符宽度，默认值 7.83 与
	// 桌面实际等宽族不符，不注入会导致点击定位随列号累计偏移。V2 后端测量走
	// glyphon 真实 shaping（与绘制端字形同源），语义与 skia 路径对齐。
	narrow, wide := rb.MeasureEditorAdvances(widgets.EditorCodeFont(), widgets.EditorCodeFontSize)
	ui.SetCharWidth(narrow)
	ui.SetWideWidth(wide)

	// 桌面端 Run 与网页端同源：解释当前编辑器源码生成场景树，替换预览后
	// 重渲染整棵树。源码在包级 sig 中，重建不丢编辑内容。
	// 解释器输出接内置终端面板（页内 Console 显示 println 等）。
	interp.SetOutput(ui.ConsolePrint)

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

	// 主题切换：Apple 明暗双主题。切主题后重建整棵树换肤（token 立即求值）。
	ui.SetThemeHandler(func(light bool) {
		if light {
			widgets.SetTheme(widgets.LightTheme)
		} else {
			widgets.SetTheme(widgets.DarkTheme)
		}
		if rerr := app.Render(func() any { return ui.Playground() }); rerr != nil {
			panic(rerr)
		}
	})

	// 注入实测字宽：让编辑器的列↔像素换算与绘制端一致。
	// 必须在首帧 Render 前完成，否则 caret 与点击命中会用默认字宽而错位。
	injectEditorCharWidth()

	// 初始化第一个示例，让编辑器有内容（Render 前写 signal，首帧即正确）。
	ui.LoadSample(0)

	// 同一份 GX 场景树：桌面与网页唯一的差别只在后端装配。
	// P4 目标态：app.Run 退化为 no-op，主循环统一走 Handle.Run（Host 轮询）。
	if err := handle.Run(); err != nil {
		panic(err)
	}
}
