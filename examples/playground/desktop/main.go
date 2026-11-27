// GX Playground —— 桌面端装配（skia 后端 + GLFW）。
//
// 这个 main.go 是纯装配样板：唯一业务代码是 ui.Playground()，来自
// 与网页端共用的 GX 源码（ui/app.gx + ui/editor.gx，编译产物 ui/*.go）。
// 键盘/鼠标输入由 glwindow.go 翻译成 GX 编辑命令，编辑逻辑零条件编译。
package main

import (
	"encoding/json"
	"os"
	"time"

	"github.com/robertpanvip/gox-playground/ui"
	"github.com/robertpanvip/gox-playground/ui/interp"
	skia "github.com/robertpanvip/gox-backend-skia"
	gxapp "github.com/robertpanvip/gox/runtime/app"
	"github.com/robertpanvip/gox/widgets"
)

func main() {
	// 窗口需容纳 Playground() 的 1360x760 界面，另留边距，否则内容被裁切。
	win := openGL()
	app, err := gxapp.OpenWith(gxapp.WindowConfig{
		Title:  "GX Playground (Desktop)",
		Width:  1400,
		Height: 860,
	}, win)
	if err != nil {
		panic(err)
	}
	defer app.Close()

	// P2 宿主接线（提案 §5.1/§6.1，须在 Render 前）：Loop 作为 UIExecutor 注入，
	// 帧请求、后台 Dispatch、微任务全部进入共享 Loop 队列，由 Run() 在
	// GLFW 线程 drain 执行；不启动第二个事件循环。
	app.SetUIExecutor(win)

	// GUI 端到端测试钩子：设置 GOX_PLAYGROUND_STATE_FILE 后，每 100ms 把 UI 状态
	// 快照写到指定文件，供脚本在真实鼠标/键盘操作后硬断言（点击定位、拖动选区、
	// 滚轮、主题、Run 等状态进程外不可见）。快照在 UI 线程生成，避免与编辑并发；
	// 未设置环境变量时完全不启用，正常用户零开销。
	if stateFile := os.Getenv("GOX_PLAYGROUND_STATE_FILE"); stateFile != "" {
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
	// 桌面 Skia 实际等宽族（Windows 上 Consolas 13px ≈ 7.15）不符，不注入会导致
	// 点击定位随列号累计偏移。与 Web 端 playground/web/main.go 同源。
	narrow, wide := skia.MeasureEditorAdvances(widgets.EditorCodeFont(), widgets.EditorCodeFontSize)
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

	// 注入实测字宽：让编辑器的列↔像素换算与 Skia 绘制端一致。
	// 必须在首帧 Render 前完成，否则 caret 与点击命中会用默认字宽而错位。
	injectEditorCharWidth()

	// 初始化第一个示例，让编辑器有内容（Render 前写 signal，首帧即正确）。
	ui.LoadSample(0)

	// 同一份 GX 场景树：桌面与网页唯一的差别只在后端装配。
	if err := app.Render(func() any { return ui.Playground() }); err != nil {
		panic(err)
	}
	if err := app.Run(); err != nil {
		panic(err)
	}
}
