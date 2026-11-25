//go:build !js

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/robertpanvip/gox/runtime/loop"
)

// 桌面 / Rust 宿主共用这份驱动：两者都在 Go 侧提供 *loop.Queue 作为 Executor。
// Rust 是纯渲染后端（C ABI 收场景树），业务逻辑与网络都在 Go 宿主侧，故驱动完全相同。
func main() {
	// 本地 mock API（真实项目里换成后端地址）。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"Pan","bio":"GX UI Engineer"}`))
	}))
	defer srv.Close()

	rt := loop.New(loop.Config{})
	defer rt.Stop()

	done := make(chan struct{})
	LoadUser(rt, srv.URL,
		func(u User) {
			fmt.Printf("[UI] 用户加载成功: %s — %s\n", u.Name, u.Bio)
			close(done)
		},
		func(err error) {
			fmt.Printf("[UI] 加载失败: %v\n", err)
			close(done)
		},
	)

	// 宿主 UI 轮次：桌面 Run() 循环 / Rust winit 事件循环每帧 drain。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			fmt.Println("fetch-demo: OK")
			return
		default:
		}
		rt.DrainTurn()
		time.Sleep(time.Millisecond)
	}
	fmt.Println("fetch-demo: 超时")
}
