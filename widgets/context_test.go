package widgets

import (
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

func TestContextDefaultAndSet(t *testing.T) {
	ctx := NewContext("light")
	if got := ctx.Value(); got != "light" {
		t.Fatalf("默认值应为 light, got %q", got)
	}
	use := ctx.Use()
	if use() != "light" {
		t.Errorf("Use 应读默认值, got %q", use())
	}
	ctx.Set("dark")
	if got := use(); got != "dark" {
		t.Errorf("Set 后 Use 应读 dark, got %q", got)
	}
}

func TestContextSetTriggersSubscriber(t *testing.T) {
	ctx := NewContext(0)
	use := ctx.Use()
	seen := make([]int, 0, 3)
	// signal.Effect 同步执行：建立依赖并在每次写入后重跑。
	comp := signal.Effect(func() { seen = append(seen, use()) })
	defer comp.Dispose()
	ctx.Set(1)
	ctx.Set(2)
	if len(seen) == 0 || seen[len(seen)-1] != 2 {
		t.Errorf("Set 应驱动订阅者重算, seen=%v", seen)
	}
}

func TestContextProviderWritesValue(t *testing.T) {
	ctx := NewContext("default")
	prov := ctx.Provider(gxscene.Props{
		"value": "from-provider",
		"children": func() any {
			return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
				el("span"), gxscene.Op{Code: gxscene.OpChildText, Arg: "child"}, closeEl(),
			}})
		},
	})
	if ctx.Value() != "from-provider" {
		t.Errorf("Provider 构造时应写入 value, got %q", ctx.Value())
	}
	root := prov.Root()
	if findByGxRole(root, "context-provider") == nil {
		t.Error("缺少 context-provider 容器")
	}
	text := gxscene.TextBackend{}.Paint(root)
	if !containsStr(text, "child") {
		t.Errorf("子树未透传: %q", text)
	}
}

func TestContextProviderGetterInitialValue(t *testing.T) {
	ctx := NewContext("default")
	ctx.Provider(gxscene.Props{
		"value": func() any { return "from-getter" },
		"children": func() any {
			return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
				el("span"), gxscene.Op{Code: gxscene.OpChildText, Arg: "x"}, closeEl(),
			}})
		},
	})
	if ctx.Value() != "from-getter" {
		t.Errorf("getter 形式应取当前值写入, got %q", ctx.Value())
	}
}

// 主题切换的典型用法：组件 Use 插槽读 context，ctx.Set 驱动重绘。
func TestContextDrivesComponentSlot(t *testing.T) {
	ctx := NewContext("before")
	// 一个带动态文本插槽的节点，getter 读 context。
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		el("span"), dynChild(0), closeEl(),
	}, N: 1}, func() any { return ctx.Use()() })
	inst.BindReactive(signal.Effect)
	paint := func() string { return gxscene.TextBackend{}.Paint(inst.Root()) }
	if got := paint(); !containsStr(got, "before") {
		t.Fatalf("初始应渲染 before: %q", got)
	}
	ctx.Set("after")
	if got := paint(); !containsStr(got, "after") {
		t.Errorf("Set 后应重绘为 after: %q", got)
	}
}
