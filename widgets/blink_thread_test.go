package widgets

// 回归测试：blinkOn 的后台翻转必须经 UI Runtime 投递，绝不在后台线程直接
// 读写响应式 signal。直接读写会重建依赖图（clearDeps/track 改 map），与 UI
// 线程并发写触发 fatal error: concurrent map writes（已在极快打字场景复现）。

import (
	"sync"
	"testing"

	Signal "github.com/robertpanvip/gox/runtime/signal"
)

// TestToggleBlinkRoutesThroughUIRuntime 验证 toggleBlink 只投递不就地执行：
// 注入捕获型 poster 后，调用方线程上的 blinkOn 不应变化，闭包只有在 poster
// 主动执行时才翻转。这固化「后台线程零 signal 访问」的线程契约。
func TestToggleBlinkRoutesThroughUIRuntime(t *testing.T) {
	var mu sync.Mutex
	var queued []func()
	old := Signal.SetUIPost(func(fn func()) bool {
		if fn == nil {
			return true
		}
		mu.Lock()
		queued = append(queued, fn)
		mu.Unlock()
		return true
	})
	defer Signal.SetUIPost(old)

	before := blinkOn()
	toggleBlink()

	mu.Lock()
	n := len(queued)
	fn := queued[0]
	mu.Unlock()
	if n == 0 {
		t.Fatal("toggleBlink 未向 UI Runtime 投递闭包")
	}
	if got := blinkOn(); got != before {
		t.Fatalf("投递前 blinkOn 不该变化: before=%v got=%v", before, got)
	}
	fn() // 模拟 UI Runtime 执行投递的闭包
	if got := blinkOn(); got == before {
		t.Fatal("UI Runtime 执行投递闭包后 blinkOn 应翻转")
	}
}

// TestToggleBlinkNoopWithoutUIRuntime 验证 UI Runtime 未就绪时安全跳过：
// 既不 panic，也不在调用方（后台）线程就地翻转 signal。
func TestToggleBlinkNoopWithoutUIRuntime(t *testing.T) {
	old := Signal.SetUIPost(nil)
	defer Signal.SetUIPost(old)

	before := blinkOn()
	toggleBlink() // 未注入投递器：闭包不执行
	if got := blinkOn(); got != before {
		t.Fatalf("UI Runtime 未就绪不应就地翻转: before=%v got=%v", before, got)
	}
}
