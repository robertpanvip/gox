//go:build js && wasm

package main

import (
	"fmt"
	"syscall/js"
	"time"

	"github.com/robertpanvip/gox-core/runtime/loop"
)

// Web 宿主驱动：gxweb.Window 只提供 Post（RAF），用 WakeExecutor 补出 PostMicro，
// 在 wake 里经 win.Post 投递下一帧 drain——不改 Web 后端现有渲染管线。
//
// 真实项目里 win 来自 gxweb.MustOpen("#app")；这里用 syscall/js 直接驱动，
// 演示 UIFetch 在浏览器/WASM 下通过 WakeExecutor 走通完整链路。
func main() {
	wakePosted := false
	exec := loop.NewWakeExecutor(func() {
		// 真实场景：_ = win.Post(func(){ exec.DrainTurn() })
		wakePosted = true
	})
	defer exec.Stop()

	// 用一个必然失败的 URL 演示 reject 通道（浏览器 fetch 会立即拒绝，
	// 不依赖外网可达性/CORS）。成功路径用法与桌面完全相同（见 logic.go + README）。
	LoadUser(exec, "http://127.0.0.1:0/unreachable",
		func(u User) {
			fmt.Println("[Web UI] 加载成功:", u.Name)
		},
		func(err error) {
			fmt.Println("[Web UI] 加载失败（预期）:", err)
			js.Global().Get("console").Call("log", "UIFetch web reject OK:", err.Error())
		},
	)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, micro := exec.HasPending()
		if micro > 0 {
			exec.DrainTurn()
		}
		time.Sleep(time.Millisecond)
	}
	if !wakePosted {
		fmt.Println("fetch-demo wasm: 警告 — wake 未触发")
	}
	fmt.Println("fetch-demo wasm: OK")
}
