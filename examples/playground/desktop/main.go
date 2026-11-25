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
)

func main() {
	// 窗口需容纳 Playground() 的 1180x720 界面，另留边距，否则内容被裁切。
	app, err := gxapp.OpenWith(gxapp.WindowConfig{
		Title:  "GX Playground (Desktop)",
		Width:  1220,
		Height: 820,
	}, openGL())
	if err != nil {
		panic(err)
	}
	defer app.Close()

	// 桌面端 Run 与网页端同源：解释当前编辑器源码生成场景树，替换预览后
	// 重渲染整棵树。源码在包级 sig 中，重建不丢编辑内容。
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
