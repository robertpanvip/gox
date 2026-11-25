package signal_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/robertpanvip/gox/runtime/signal"
)

// 线程守卫的核心契约：
//   - 默认构建 ThreadGuardEnabled=false，钩子不触发（零开销、零误报）
//   - gxthreadcheck 构建下，后台 goroutine 触碰结构变更点会被抓到并上报
//
// 用构建隔离：上面的断言在两种构建下都成立；后台违规测试只在 check 构建下
// 期待上报。测试结束都注销钩子，避免污染其它测试。

func TestThreadGuardToggle(t *testing.T) {
	un := signal.SetThreadViolationHook(func(signal.ThreadViolation) {})
	defer un()

	if got := signal.ThreadGuardEnabled(); got {
		// check 构建：守卫启用
		t.Log("gxthreadcheck 构建：线程守卫启用")
	} else {
		// 默认构建：守卫关闭，绑定/钩子都是空操作
		signal.BindUIThread()
		t.Log("默认构建：线程守卫关闭（零开销）")
	}
}

// TestThreadGuardCatchesBackgroundWrite 只在 gxthreadcheck 构建下断言上报。
// 用文件名后缀无法做构建约束（同包），所以在运行时按 ThreadGuardEnabled 分支：
// 默认构建跳过断言，check 构建完整验证抓栈与去重。
func TestThreadGuardCatchesBackgroundWrite(t *testing.T) {
	if !signal.ThreadGuardEnabled() {
		t.Skip("默认构建未启用线程守卫")
	}

	// 把当前测试 goroutine 绑定为 UI 线程
	signal.BindUIThread()

	var mu sync.Mutex
	var got []signal.ThreadViolation
	un := signal.SetThreadViolationHook(func(v signal.ThreadViolation) {
		mu.Lock()
		got = append(got, v)
		mu.Unlock()
	})
	defer un()

	// 构造一个有订阅者的信号：写它会走 collectDirty（守卫检查点）
	src := signal.New(0)
	done := make(chan struct{})
	signal.Effect(func() { _ = src() })

	// 后台 goroutine 写信号——违反单线程契约
	go func() {
		for i := 0; i < 2000; i++ {
			src(i)
		}
		close(done)
	}()
	<-done

	// 给去重/回调留一点时间
	deadline := time.Now().Add(time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) == 0 {
		t.Fatal("gxthreadcheck 构建下后台写信号应被守卫上报")
	}
	v := got[0]
	if v.Op == "" || v.Goid == 0 || v.UIGoid == 0 {
		t.Fatalf("违规记录字段不完整: %+v", v)
	}
	if !strings.Contains(v.Stack, "TestThreadGuardCatchesBackgroundWrite") {
		t.Fatalf("违规栈应含调用方: %s", v.Stack)
	}
	// 去重：同一 (goid, op) 多次违规只报一次（后台写了 2000 次，每个检查点各 1 条）
	seen := map[string]bool{}
	for _, item := range got {
		k := item.Op // 本测试里 goid 都相同，按 op 校验唯一性
		if seen[k] {
			t.Fatalf("同一 goroutine 的检查点 %s 被重复上报", k)
		}
		seen[k] = true
	}
	if !seen["collectDirty"] {
		t.Fatalf("写信号的 collectDirty 检查点应被上报, got ops=%v", seen)
	}
}
