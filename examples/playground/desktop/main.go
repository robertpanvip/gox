// GX Playground —— 桌面端装配（skia 后端 + GLFW）。
//
// 这个 main.go 是纯装配样板：唯一业务代码是 ui.Playground()，来自
// 与网页端共用的 GX 源码（ui/app.gx + ui/editor.gx，编译产物 ui/*.go）。
// 键盘/鼠标输入由 glwindow.go 翻译成 GX 编辑命令，编辑逻辑零条件编译。
package main

import (
	gxapp "github.com/robertpanvip/gox/runtime/app"
	"github.com/robertpanvip/gox-playground/ui"
)

func main() {
	app, err := gxapp.OpenWith(gxapp.WindowConfig{
		Title:  "GX Playground (Desktop)",
		Width:  960,
		Height: 680,
	}, openGL())
	if err != nil {
		panic(err)
	}
	defer app.Close()

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
