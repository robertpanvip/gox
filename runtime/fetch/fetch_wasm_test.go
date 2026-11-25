//go:build js && wasm

package fetch

import (
	"testing"
	"time"

	"github.com/robertpanvip/gox/runtime/loop"
)

// TestUIFetchWASMRejectPath 验证 js/wasm 下 fetch 链路完整：
// 后台 goroutine 发请求 → 错误经 Executor 投回 UI → Catch 在 drain 后执行。
// 用非法 URL 触发立即失败（Go 的 net/http 在 js/wasm 下由 roundtrip_js.go 走
// 浏览器/node 的 fetch），不依赖网络可达性或 CORS。
func TestUIFetchWASMRejectPath(t *testing.T) {
	exec := loop.NewWakeExecutor(nil)
	defer exec.Stop()

	var caught error
	loop.Catch(
		UIFetch(exec, Request{URL: "http://127.0.0.1:0/unreachable"}),
		func(err error) (*Response, error) {
			caught = err
			return nil, nil
		},
	)

	// 宿主轮次：等后台 settle 产生微任务，再 drain。
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, micro := exec.HasPending()
		if micro > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	exec.DrainTurn()

	if caught == nil {
		t.Fatal("Catch 应捕获到错误（wasm 下 fetch 链路未走通）")
	}
	if caught.Error() == "" {
		t.Fatal("错误信息不应为空")
	}
	t.Logf("wasm fetch reject 通道正常: %v", caught)
}
