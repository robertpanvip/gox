// Package gxweb 是 gox 的浏览器（WebAssembly）后端。
//
// 它和 backend/skia、backend/rust/go 一样是一个**独立 Go module**，实现 gxapp 的
// Backend Contract：gxapp.Window + gxapp.FramePresenter，以及可选的 Focuser /
// Measurer / ComputedStyler。核心 runtime 不 import 它；装配层（main）通过
// OpenWith / gxapp.Register 注入。
//
// # 渲染模型
//
// Web 后端没有自己的光栅引擎——浏览器 DOM 就是渲染面。Present 收到 gxapp 帧后：
//
//   - 全量帧（FullSync）或首帧：遍历 frame.Root 重建一棵 DOM 镜像；
//   - 增量帧：按 frame.Changes 逐条打补丁（setAttr / setText / 结构移动）。
//
// 场景节点与 DOM 元素的映射由 NodeID 维护（gxscene 分配的稳定整数身份），
// 事件回投也靠它：浏览器 onclick → 找到 NodeID → gxscene.Node.DispatchBubble。
//
// # 平台分层（build tag）
//
//   - dom_js.go   （js && wasm）：syscall/js 驱动，真实操作 document；
//   - dom_mem.go  （!js || !wasm）：内存 DOM 驱动，供 native 单测 / 快照 / headless。
//
// 两者实现同一个 Driver 接口，因此后端核心逻辑（window.go / reconcile.go）在
// native 下可完整单测，wasm 下只替换驱动层即可。`go build ./...` 在原生平台
// 只编 mem 驱动，不触碰 syscall/js。
//
// # 用法（浏览器）
//
//	// main.go 用 //go:build js && wasm
//	app, _ := gxapp.OpenWith(gxapp.WindowConfig{Title:"Demo"}, gxweb.MustOpen("#root"))
//	app.Render(view)
//	app.Run() // Web 后端立即返回；事件循环由浏览器驱动
//
// 编译：
//
//	GOOS=js GOARCH=wasm go build -o app.wasm .
package gxweb
