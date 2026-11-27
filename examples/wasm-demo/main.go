//go:build js && wasm

// wasm-demo 演示「GX 应用 + Web 后端」一起编译成 WebAssembly 在浏览器运行。
//
// 这个 main.go 就是 GX 编译器产出 Go 代码 + 装配代码的形态：view() 里的
// gxscene.Render(Template{...}) 由 GX 编译器从 JSX 生成；其余是固定装配样板。
// 想用 JSX 写界面时，把 view() 换成 .gx 里编译出的同名函数即可，装配层零改动。
package main

import (
	gxweb "github.com/robertpanvip/gox-backend-web"
	gxapp "github.com/robertpanvip/gox-core/runtime/app"
	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
	"github.com/robertpanvip/gox-core/runtime/signal"
)

// view 是一棵计数器场景树：点击按钮 → signal+1 → 精确更新文本 → Web 后端增量重绘 DOM。
// 结构上等价于 GX：
//
//	<div style={{bg:"#0f172a", color:"#e2e8f0", padding:"24", borderRadius:"14", gap:"12", flexDirection:"column"}}>
//	  <b style={{fontSize:"28"}}>{count}</b>
//	  <button onClick={increment} style={{bg:"#3b82f6", padding:"10", borderRadius:"8"}}>+</button>
//	</div>
func view() any {
	count := signal.New(0)
	increment := func(ctx gxscene.EventContext) { count(count() + 1) }

	return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpAttr, Key: "bg", Arg: "#0f172a"},
		{Code: gxscene.OpAttr, Key: "color", Arg: "#e2e8f0"},
		{Code: gxscene.OpAttr, Key: "padding", Arg: "24"},
		{Code: gxscene.OpAttr, Key: "border-radius", Arg: "14"},
		{Code: gxscene.OpAttr, Key: "flex-direction", Arg: "column"},
		{Code: gxscene.OpAttr, Key: "gap", Arg: "12"},
		{Code: gxscene.OpElem, Arg: "b"},
		{Code: gxscene.OpAttr, Key: "font-size", Arg: "28"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
		{Code: gxscene.OpElem, Arg: "button"},
		{Code: gxscene.OpAttr, Key: "bg", Arg: "#3b82f6"},
		{Code: gxscene.OpAttr, Key: "padding", Arg: "10"},
		{Code: gxscene.OpAttr, Key: "border-radius", Arg: "8"},
		{Code: gxscene.OpEvent, Key: "onClick", Slot: 1},
		{Code: gxscene.OpChildText, Arg: "点我 +1"},
		{Code: gxscene.OpClose},
		{Code: gxscene.OpClose},
	}, N: 2},
		func() any { return count() },                         // slot 0: 动态文本
		func() any { return gxscene.Handler(increment) },      // slot 1: 事件 handler
	)
}

func main() {
	// 接入层装配：gx.use(Backend) → Use(接口值) + 逐窗 Render（与桌面后端同一路径）。
	handle := gxapp.Use(gxweb.NewBackend())
	app, err := handle.Render(view,
		gxapp.WindowConfig{Title: "gox wasm demo", Width: 1200, Height: 800})
	if err != nil {
		panic(err)
	}
	defer handle.Close()
	// Web 后端无自己的事件循环：Run 立即返回，页面靠 DOM 事件持续驱动。
	if err := app.Run(); err != nil {
		panic(err)
	}
	// 阻止 main 返回——wasm 里 main 返回即结束，用阻塞通道保持事件回调可用。
	select {}
}
