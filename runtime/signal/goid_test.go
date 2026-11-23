package signal_test

import (
	"testing"

	"github.com/robertpanvip/gox/runtime/signal"
)

// TX.1：goid 实现可替换，替换后依赖追踪按新 id 隔离，核心逻辑不变。
func TestGoidProviderReplaceable(t *testing.T) {
	// 用一个恒定的假 id 替换：所有操作被视为同一 goroutine。
	old := signal.SetGoidProvider(func() uint64 { return 42 })
	defer signal.SetGoidProvider(old)

	if got := signal.Goid(); got != 42 {
		t.Fatalf("替换后 Goid 应为 42, got %d", got)
	}

	// 核心依赖追踪仍工作：effect 订阅 signal，写入触发重跑。
	s := signal.New(0)
	runs := 0
	signal.Effect(func() { runs++; _ = s() })
	base := runs
	s(1)
	if runs != base+1 {
		t.Fatalf("替换 goid 后 effect 应仍重跑一次, base=%d got=%d", base, runs)
	}

	// 恢复默认后 Goid 为正数（真实 goroutine id）。
	signal.SetGoidProvider(old)
	if signal.Goid() == 0 {
		t.Fatal("恢复默认后 Goid 应 > 0")
	}
}
