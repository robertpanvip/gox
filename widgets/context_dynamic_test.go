package widgets

import (
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// 分支切换重建子树时，builder 里的 Use() 必须仍读到所属 Provider 的值，
// 而不是回落到全局默认——验证 withObserver 恢复 Owner + Branch 捕获 scope。
func TestContextInBranchSwitchKeepsProvider(t *testing.T) {
	ctx := NewContext("default")
	sel := signal.New(0)
	var root *gxscene.Instance

	ctx.Provider(gxscene.Props{
		"value": "scoped",
		"children": func() any {
			// 两个分支的 builder 都在 Provider 的 Owner 栈内被 NewBranch 捕获。
			branch := gxscene.BranchGetter(
				func() int { return sel() },
				func() *gxscene.Instance { return leafText(ctx.Use()) },
				func() *gxscene.Instance { return leafText(ctx.Use()) },
			)
			root = gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
				el("div"), {Code: gxscene.OpBranch, Slot: 0}, closeEl(),
			}, N: 1}, branch)
			root.BindReactive(signal.Effect)
			return root
		},
	})

	if got := paint(root); !containsStr(got, "scoped") {
		t.Fatalf("分支初始应读 scoped, got %q", got)
	}
	// 切到分支 1：触发 Resolve 重建子树，builder 里的 Use() 需沿恢复的 Owner 链命中。
	sel(1)
	if got := paint(root); !containsStr(got, "scoped") {
		t.Errorf("分支切换后仍应读 scoped（不回落 default）, got %q", got)
	}
}

// keyed 列表重建列表项时，render 里的 Use() 必须仍读到 Provider 的值。
func TestContextInForKeepsProvider(t *testing.T) {
	ctx := NewContext("default")
	items := signal.New([]any{"a", "b"})
	var root *gxscene.Instance

	ctx.Provider(gxscene.Props{
		"value": "scoped",
		"children": func() any {
			forGetter := gxscene.ForGetter(
				func() []any { return items() },
				nil,
				func(any) *gxscene.Instance { return leafText(ctx.Use()) },
			)
			root = gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
				el("div"), {Code: gxscene.OpFor, Slot: 0}, closeEl(),
			}, N: 1}, forGetter)
			root.BindReactive(signal.Effect)
			return root
		},
	})

	if got := paint(root); !containsStr(got, "scoped") {
		t.Fatalf("列表初始应读 scoped, got %q", got)
	}
	// 增删项触发部分列表项重建，render 里的 Use() 需沿恢复的 Owner 链命中。
	items([]any{"a", "b", "c"})
	if got := paint(root); !containsStr(got, "scoped") {
		t.Errorf("列表增删项后仍应读 scoped, got %q", got)
	}
}
