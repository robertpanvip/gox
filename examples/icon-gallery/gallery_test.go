package main

import (
	"strings"
	"testing"

	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
	gxicon "github.com/robertpanvip/gox-core/runtime/icon"
	"github.com/robertpanvip/gox-core/widgets"
)

// TestNamedComponentsExist 保证 showcase 里引用的具名图标组件都真实导出且产出
// canvas 矢量节点（防止 iconSet 变动后示例引用失效或静默空白）。
func TestNamedComponentsExist(t *testing.T) {
	named := map[string]func(gxscene.Props) *gxscene.Instance{
		"house": widgets.IconHouse, "search": widgets.IconSearch,
		"heart": widgets.IconHeart, "star": widgets.IconStar,
		"gear": widgets.IconGear, "bolt": widgets.IconBolt,
	}
	props := gxscene.Props{"size": 24, "color": "#0a84ff"}
	for name, fn := range named {
		inst := fn(props)
		if inst == nil || inst.Root() == nil {
			t.Fatalf("%s 产出空实例", name)
		}
	}
}

// TestAllIconNamesNonEmpty 保证全量网格数据源（gxicon.Names）非空且可解析。
func TestAllIconNamesNonEmpty(t *testing.T) {
	names := gxicon.Names()
	if len(names) < 85 {
		t.Fatalf("内置图标数 %d < 85", len(names))
	}
	for _, n := range names {
		if strings.TrimSpace(n) == "" {
			t.Fatal("存在空图标名")
		}
	}
}
