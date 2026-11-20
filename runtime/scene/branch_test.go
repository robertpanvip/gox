package gxscene_test

import (
	"strings"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// 构造一个带条件分支插槽的模板：<div>{branch}</div>
func branchTpl() gxscene.Template {
	return gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpBranch, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
}

// 构造一个叶子文本模板：<span>{text}</span>
func spanTpl() gxscene.Template {
	return gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "span"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
}

func TestBranchRendersThenBranch(t *testing.T) {
	visible := signal.New(true)
	count := signal.New(10)

	// selector 读 visible；两个分支各建一个 span 子树，读 count。
	br := gxscene.NewBranch(
		func() int {
			if visible() {
				return 0
			}
			return 1
		},
		func() *gxscene.Instance {
			return gxscene.Render(spanTpl(), func() any { return count() })
		},
		func() *gxscene.Instance {
			return gxscene.Render(spanTpl(), func() any { return "Hidden" })
		},
	)

	view := gxscene.Render(branchTpl(), func() any { return br })
	view.BindReactive(signal.Effect)

	out := view.String()
	if !strings.Contains(out, "10") || strings.Contains(out, "Hidden") {
		t.Fatalf("初始应渲染 then 分支(10):\n%s", out)
	}
}

func TestBranchSelectorDependencySwitchesBranches(t *testing.T) {
	visible := signal.New(true)
	count := signal.New(10)

	thenBuilds, elseBuilds := 0, 0
	br := gxscene.NewBranch(
		func() int {
			if visible() {
				return 0
			}
			return 1
		},
		func() *gxscene.Instance {
			thenBuilds++
			return gxscene.Render(spanTpl(), func() any { return count() })
		},
		func() *gxscene.Instance {
			elseBuilds++
			return gxscene.Render(spanTpl(), func() any { return "Hidden" })
		},
	)

	view := gxscene.Render(branchTpl(), func() any { return br })
	view.BindReactive(signal.Effect)

	if !strings.Contains(view.String(), "10") {
		t.Fatalf("初始应渲染 then:\n%s", view.String())
	}

	// 写 visible：selector 依赖应触发分支切换到 else。
	visible(false)
	out := view.String()
	if !strings.Contains(out, "Hidden") || strings.Contains(out, "10") {
		t.Fatalf("visible=false 应切到 else 分支:\n%s", out)
	}
	if elseBuilds != 1 {
		t.Fatalf("else 分支应构建一次, got %d", elseBuilds)
	}

	visible(true)
	if !strings.Contains(view.String(), "10") {
		t.Fatalf("visible=true 应切回 then:\n%s", view.String())
	}
	if thenBuilds != 2 {
		t.Fatalf("then 分支应累计构建两次, got %d", thenBuilds)
	}
}

func TestBranchInnerSignalDoesNotSwitchBranch(t *testing.T) {
	visible := signal.New(true)
	count := signal.New(10)
	thenBuilds := 0

	br := gxscene.NewBranch(
		func() int {
			if visible() {
				return 0
			}
			return 1
		},
		func() *gxscene.Instance {
			thenBuilds++
			return gxscene.Render(spanTpl(), func() any { return count() })
		},
		func() *gxscene.Instance {
			return gxscene.Render(spanTpl(), func() any { return "Hidden" })
		},
	)

	view := gxscene.Render(branchTpl(), func() any { return br })
	view.BindReactive(signal.Effect)

	// 分支内部 signal 变化：只更新叶子文本，不重建分支、不重跑 selector。
	count(20)
	if !strings.Contains(view.String(), "20") {
		t.Fatalf("分支内 signal 应更新叶子:\n%s", view.String())
	}
	if thenBuilds != 1 {
		t.Fatalf("分支内变化不应重建分支, thenBuilds=%d", thenBuilds)
	}
}

func TestBranchDisposeCleansUpInnerEffects(t *testing.T) {
	visible := signal.New(true)
	count := signal.New(10)
	cleaned := 0

	br := gxscene.NewBranch(
		func() int {
			if visible() {
				return 0
			}
			return 1
		},
		func() *gxscene.Instance {
			// 分支内建一个 count 的 effect，注册 cleanup。
			signal.Effect(func() {
				_ = count()
				signal.OnCleanup(func() { cleaned++ })
			})
			return gxscene.Render(spanTpl(), func() any { return count() })
		},
		func() *gxscene.Instance {
			return gxscene.Render(spanTpl(), func() any { return "Hidden" })
		},
	)

	view := gxscene.Render(branchTpl(), func() any { return br })
	view.BindReactive(signal.Effect)

	if cleaned != 0 {
		t.Fatalf("初始不应有 cleanup, got %d", cleaned)
	}

	// 切到 else：旧分支 Owner 应被销毁，其中的 effect cleanup 执行。
	// 注意 count 也会让该 effect 重跑；这里只断言切分支后至少清理过。
	visible(false)
	if cleaned < 1 {
		t.Fatalf("切分支应清理旧分支 effect 的 cleanup, got %d", cleaned)
	}

	// 再写 count：旧分支的 effect 已销毁，不应再因 count 变化而 cleanup 增加。
	before := cleaned
	count(30)
	if cleaned > before {
		t.Fatalf("已销毁分支的 effect 不应再响应 count 变化: %d -> %d", before, cleaned)
	}
}

func TestBranchDisposeViaInstance(t *testing.T) {
	count := signal.New(1)
	cleaned := 0

	br := gxscene.NewBranch(
		func() int { return 0 },
		func() *gxscene.Instance {
			signal.Effect(func() {
				_ = count()
				signal.OnCleanup(func() { cleaned++ })
			})
			return gxscene.Render(spanTpl(), func() any { return count() })
		},
	)
	view := gxscene.Render(branchTpl(), func() any { return br })

	view.Dispose()
	if cleaned < 1 {
		t.Fatalf("Instance.Dispose 应级联销毁分支 effect, cleaned=%d", cleaned)
	}
	before := cleaned
	count(2)
	if cleaned > before {
		t.Fatalf("Dispose 后 effect 不应再响应: %d -> %d", before, cleaned)
	}
}
