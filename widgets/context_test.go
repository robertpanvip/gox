package widgets

import (
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// leafText 造一个「读 context 的叶子组件」：组件函数在建树期调用 use() 取 getter，
// 放进动态文本插槽。它被 Provider 的 children getter 调用，故处于该 Provider 的
// Owner 栈内，能沿链 Lookup 到值。返回的 getter 供测试在 Build 后读绘制文本。
func leafText(use func() string) *gxscene.Instance {
	return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		el("span"), dynChild(0), closeEl(),
	}, N: 1}, func() any { return use() })
}

func paint(inst *gxscene.Instance) string {
	return (gxscene.TextBackend{}).Paint(inst.Root())
}

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
	comp := signal.Effect(func() { seen = append(seen, use()) })
	defer comp.Dispose()
	ctx.Set(1)
	ctx.Set(2)
	if len(seen) == 0 || seen[len(seen)-1] != 2 {
		t.Errorf("Set 应驱动订阅者重算, seen=%v", seen)
	}
}

// Provider 只影响自己的子树：子树内读 to-provider，子树外（全局默认）仍是 default。
func TestContextProviderScopesSubtree(t *testing.T) {
	ctx := NewContext("default")
	var inside *gxscene.Instance
	prov := ctx.Provider(gxscene.Props{
		"value": "to-provider",
		"children": func() any {
			inside = leafText(ctx.Use())
			return inside
		},
	})
	if got := paint(inside); !containsStr(got, "to-provider") {
		t.Errorf("子树内应读 Provider 值, got %q", got)
	}
	if ctx.Value() != "default" {
		t.Errorf("Provider 不应写全局默认（子树隔离）, got %q", ctx.Value())
	}
	if findByGxRole(prov.Root(), "context-provider") == nil {
		t.Error("缺少 context-provider 容器")
	}
}

// 嵌套 Provider 就近覆盖：内层子树读 inner，外层子树（内层之外）读 outer。
func TestContextNestedProviderNearestWins(t *testing.T) {
	ctx := NewContext("default")
	var innerLeaf, outerLeaf *gxscene.Instance
	ctx.Provider(gxscene.Props{
		"value": "outer",
		"children": func() any {
			// 外层子树里先放一个读外层值的叶子。
			outerLeaf = leafText(ctx.Use())
			// 再嵌一个内层 Provider，覆盖 value。
			inner := ctx.Provider(gxscene.Props{
				"value": "inner",
				"children": func() any {
					innerLeaf = leafText(ctx.Use())
					return innerLeaf
				},
			})
			return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
				el("div"), dynChild(0), dynChild(1), closeEl(),
			}, N: 2}, func() any { return outerLeaf }, func() any { return inner })
		},
	})
	if got := paint(innerLeaf); !containsStr(got, "inner") {
		t.Errorf("内层子树应读 inner, got %q", got)
	}
	if got := paint(outerLeaf); !containsStr(got, "outer") {
		t.Errorf("外层子树应读 outer（未被内层覆盖）, got %q", got)
	}
}

// 兄弟 Provider 互相隔离：各自子树读各自值，互不串台。
func TestContextSiblingProvidersIsolated(t *testing.T) {
	ictx := NewContext(0)
	var ia, ib *gxscene.Instance
	ictx.Provider(gxscene.Props{
		"value": 11,
		"children": func() any { ia = intLeaf(ictx.Use()); return ia },
	})
	ictx.Provider(gxscene.Props{
		"value": 22,
		"children": func() any { ib = intLeaf(ictx.Use()); return ib },
	})
	if got := paint(ia); !containsStr(got, "11") {
		t.Errorf("兄弟 A 应读 11, got %q", got)
	}
	if got := paint(ib); !containsStr(got, "22") {
		t.Errorf("兄弟 B 应读 22, got %q", got)
	}
}

func intLeaf(use func() int) *gxscene.Instance {
	return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		el("span"), dynChild(0), closeEl(),
	}, N: 1}, func() any { return use() })
}

// value 是 getter 时，上游 signal 变化只让消费者子树重绘（响应式更新）。
func TestContextProviderValueGetterReactive(t *testing.T) {
	src := signal.New("A")
	ctx := NewContext("default")
	var leaf *gxscene.Instance
	ctx.Provider(gxscene.Props{
		"value": func() any { return src() },
		"children": func() any {
			use := ctx.Use()
			leaf = gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
				el("span"), dynChild(0), closeEl(),
			}, N: 1}, func() any { return use() })
			leaf.BindReactive(signal.Effect)
			return leaf
		},
	})
	if got := paint(leaf); !containsStr(got, "A") {
		t.Fatalf("初始应读 A, got %q", got)
	}
	src("B")
	if got := paint(leaf); !containsStr(got, "B") {
		t.Errorf("上游变化后应读 B, got %q", got)
	}
}

// 典型用法：组件 Use 插槽读 context，Set 写默认值驱动重绘（全局单例用法兼容）。
func TestContextDrivesComponentSlot(t *testing.T) {
	ctx := NewContext("before")
	inst := leafText(ctx.Use())
	inst.BindReactive(signal.Effect)
	if got := paint(inst); !containsStr(got, "before") {
		t.Fatalf("初始应渲染 before: %q", got)
	}
	ctx.Set("after")
	if got := paint(inst); !containsStr(got, "after") {
		t.Errorf("Set 后应重绘为 after: %q", got)
	}
}

// Provider 构造不写全局：children getter 里 Use 才捕获子树值（旧契约已废弃）。
func TestContextProviderGetterDoesNotWriteGlobal(t *testing.T) {
	ctx := NewContext("default")
	ctx.Provider(gxscene.Props{
		"value": func() any { return "from-getter" },
		"children": func() any {
			use := ctx.Use()
			if use() != "from-getter" {
				t.Errorf("子树内 getter 形式应读 from-getter, got %q", use())
			}
			return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
				el("span"), gxscene.Op{Code: gxscene.OpChildText, Arg: "x"}, closeEl(),
			}})
		},
	})
	if ctx.Value() != "default" {
		t.Errorf("Provider 不应写全局默认, got %q", ctx.Value())
	}
}
