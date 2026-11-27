package main

import (
	"fmt"
	"os"

	skia "github.com/robertpanvip/gox-backend-skia"
	gxapp "github.com/robertpanvip/gox/runtime/app"
	"github.com/robertpanvip/gox-idea/ui"
)

// 离屏渲染整窗帧并导出 RGBA8888 raw（外部用 ImageMagick 转 PNG）。
// 与真实窗口共用同一 painter/布局/字体链（渲染管线的第二观测面）。
func main() {
	w := skia.NewWindow()
	app, err := gxapp.OpenWith(gxapp.WindowConfig{Title: "shot", Width: 1400, Height: 860}, w)
	if err != nil {
		panic(err)
	}
	defer app.Close()
	ui.LoadSample(0) // 与桌面入口 main.go 同序：首帧前打开基线示例（标签/树才有内容）
	if err := app.Render(func() any { return ui.Playground() }); err != nil {
		panic(err)
	}
	pm := w.Pixels()
	if len(pm) != 1400*860*4 {
		panic(fmt.Sprintf("unexpected pixel buffer size %d", len(pm)))
	}
	if err := os.WriteFile(os.Args[1], pm, 0o644); err != nil {
		panic(err)
	}
	fmt.Println("DUMP_OK", len(pm))
}
