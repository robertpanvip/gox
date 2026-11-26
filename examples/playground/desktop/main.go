// GX Playground —— 桌面端装配（skia 后端 + GLFW）。
//
// 这个 main.go 是纯装配样板：唯一业务代码是 ui.Playground()，来自
// 与网页端共用的 GX 源码（ui/app.gx + ui/editor.gx，编译产物 ui/*.go）。
// 键盘/鼠标输入由 glwindow.go 翻译成 GX 编辑命令，编辑逻辑零条件编译。
package main

import (
	"github.com/robertpanvip/gox-playground/ui"
	"github.com/robertpanvip/gox-playground/ui/interp"
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
