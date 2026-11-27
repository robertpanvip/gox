// gradcheck 是三端渐变统一的 Rust 后端可视化验收程序。
// 渲染：① 分段渐变文字（模拟编辑器代码高亮，红→绿→蓝按 ch 边界）
//
//	② 平滑渐变背景块（通用 linear-gradient，90deg 红→蓝）
//
// 在 xvfb 下运行并用截图采样像素，确认 Rust 后端与 Web/Go 桌面端语义一致。
package main

import (
	"fmt"
	"time"

	rb "github.com/robertpanvip/gox-backend-rust"
	gxapp "github.com/robertpanvip/gox-core/runtime/app"
	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
)

// gradientText 造一个 gradient-text 标记 + 渐变 bg 的 span，文本为子节点。
// 与编辑器 gx-code-line 的结构完全一致（见 examples/playground/ui/editor.gx）。
func gradientText(text, grad string) *gxscene.Instance {
	return gxscene.Render(gxscene.Template{
		Ops: []gxscene.Op{
			{Code: gxscene.OpElem, Arg: "span"},
			{Code: gxscene.OpAttr, Key: "white-space", Arg: "pre"},
			{Code: gxscene.OpAttr, Key: "font-size", Arg: "28"},
			{Code: gxscene.OpAttr, Key: "height", Arg: "44"},
			{Code: gxscene.OpAttr, Key: "color", Arg: "#ffffff"},
			{Code: gxscene.OpAttr, Key: "gradient-text", Arg: "1"},
			{Code: gxscene.OpAttr, Key: "bg", Arg: grad},
			{Code: gxscene.OpDynChild, Slot: 0},
			{Code: gxscene.OpClose},
		},
		N: 1,
	}, func() any { return text })
}

func main() {
	// 接入层装配：gx.use(Backend) → Use(接口值) + 逐窗 Render。
	handle := gxapp.Use(rb.NewBackend())
	build := func() any {
		return gxscene.Render(gxscene.Template{
			Ops: []gxscene.Op{
				{Code: gxscene.OpElem, Arg: "div"},
				{Code: gxscene.OpAttr, Key: "width", Arg: "620"},
				{Code: gxscene.OpAttr, Key: "height", Arg: "260"},
				{Code: gxscene.OpAttr, Key: "bg", Arg: "#0b1020"},
				{Code: gxscene.OpAttr, Key: "flex-direction", Arg: "column"},
				{Code: gxscene.OpAttr, Key: "padding", Arg: "20"},
				{Code: gxscene.OpAttr, Key: "gap", Arg: "14"},
				// ① 分段渐变文字：每字符 2ch，红[0,2) 绿[2,4) 蓝[4,6)
				{Code: gxscene.OpElem, Arg: "div"},
				{Code: gxscene.OpDynChild, Slot: 0},
				{Code: gxscene.OpClose},
				// ② 平滑渐变背景块：90deg 红→蓝，宽 300 高 60
				{Code: gxscene.OpElem, Arg: "div"},
				{Code: gxscene.OpAttr, Key: "width", Arg: "300"},
				{Code: gxscene.OpAttr, Key: "height", Arg: "60"},
				{Code: gxscene.OpAttr, Key: "bg", Arg: "linear-gradient(90deg, #ff0000 0%, #0000ff 100%)"},
				{Code: gxscene.OpClose},
				{Code: gxscene.OpClose},
			},
			N: 1,
		}, func() any {
			return gradientText("ABCDEF",
				"linear-gradient(90deg, #ff0000 0ch 2ch, #00ff00 2ch 4ch, #0000ff 4ch 6ch)")
		})
	}

	app, err := handle.Render(build, gxapp.WindowConfig{Title: "GradCheck", Width: 620, Height: 260})
	if err != nil {
		panic(err)
	}
	defer handle.Close()

	// 渲染稳定后自动退出，便于 xvfb 下截图。
	go func() {
		time.Sleep(2500 * time.Millisecond)
		fmt.Println("[gradcheck] closing")
		_ = app.Close()
	}()

	if err := handle.Run(); err != nil { // Host 唯一主循环（P4 目标态）：app.Run 已退化为 no-op
		panic(err)
	}
}
