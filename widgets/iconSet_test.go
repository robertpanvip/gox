package widgets

import (
	"testing"

	"github.com/robertpanvip/gox/runtime/canvas"
	gxicon "github.com/robertpanvip/gox/runtime/icon"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// namedIconCases 覆盖各分区的代表性具名组件，验证 GX 源码常量 → 矢量指令链路。
var namedIconCases = []struct {
	name string
	fn   func(gxscene.Props) *gxscene.Instance
}{
	{"house", IconHouse},
	{"chevron-right", IconChevronRight},
	{"plus", IconPlus},
	{"search", IconSearch},
	{"play", IconPlay},
	{"heart", IconHeart},
	{"bell", IconBell},
	{"warning", IconWarning},
	{"moon", IconMoon},
	{"home(alias)", IconHome},
	{"user(alias)", IconUser},
	{"settings(alias)", IconSettings},
}

func TestNamedIcon_AllProduceVectorCanvas(t *testing.T) {
	for _, tc := range namedIconCases {
		t.Run(tc.name, func(t *testing.T) {
			inst := tc.fn(gxscene.Props{"size": 24, "color": "#ff0000"})
			n := findKind(inst.Root(), "canvas")
			if n == nil {
				t.Fatal("应产出 Kind=canvas 节点")
			}
			if n.Props["width"] != "24" || n.Props["height"] != "24" {
				t.Fatalf("尺寸未透传: %v", n.Props)
			}
			raw, ok := n.Props[canvas.PropKey].(string)
			if !ok || raw == "" || raw == "[]" {
				t.Fatalf("应含矢量指令: %v", n.Props[canvas.PropKey])
			}
			ops := canvas.Decode(raw)
			if len(ops) == 0 {
				t.Fatal("解码后应有绘制指令")
			}
			// 具名组件固定 color=#ff0000，应注入每条指令的 stroke 或 fill。
			found := false
			for _, op := range ops {
				if op.Stroke == "#ff0000" || op.Fill == "#ff0000" {
					found = true
				}
			}
			if !found {
				t.Fatalf("color 未注入: %+v", ops)
			}
		})
	}
}

func TestNamedIcon_ToneColor(t *testing.T) {
	// 不传 color，tone=red 应取苹果红（通用 Icon 解析逻辑复用）。
	inst := IconHeart(gxscene.Props{"tone": "red"})
	n := findKind(inst.Root(), "canvas")
	ops := canvas.Decode(n.Props[canvas.PropKey].(string))
	found := false
	for _, op := range ops {
		if op.Stroke == AppleRed || op.Fill == AppleRed {
			found = true
		}
	}
	if !found {
		t.Fatalf("tone=red 未注入苹果红: %+v", ops)
	}
}

func TestNamedIcon_StrokeWidthScale(t *testing.T) {
	// stroke-width=3 随 size=20 缩放：scale=20/24 → 2.5
	inst := IconPlus(gxscene.Props{"size": 20, "stroke-width": 3, "color": "#fff"})
	n := findKind(inst.Root(), "canvas")
	ops := canvas.Decode(n.Props[canvas.PropKey].(string))
	if len(ops) == 0 {
		t.Fatal("应有指令")
	}
	if ops[0].StrokeWidth < 2.4 || ops[0].StrokeWidth > 2.6 {
		t.Fatalf("stroke-width 缩放错误: %f", ops[0].StrokeWidth)
	}
}

func TestNamedIcon_AliasEqualsCanonical(t *testing.T) {
	// IconHome 应与 IconHouse 产出完全相同的矢量指令（同一 SVG 常量）。
	a := canvas.Decode(findKind(IconHome(gxscene.Props{"size": 20, "color": "#fff"}).Root(), "canvas").Props[canvas.PropKey].(string))
	b := canvas.Decode(findKind(IconHouse(gxscene.Props{"size": 20, "color": "#fff"}).Root(), "canvas").Props[canvas.PropKey].(string))
	if len(a) != len(b) {
		t.Fatalf("别名与规范名指令数不同: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("别名与规范名指令不一致 @%d: %+v vs %+v", i, a[i], b[i])
		}
	}
}

func TestNamedIcon_SVGMatchBuiltin(t *testing.T) {
	// GX 源码常量应与 runtime/icon 内置表逐字节一致（防止组件库与运行时漂移）。
	cases := map[string]string{
		"house": svgHouse, "search": svgSearch, "heart": svgHeart,
		"gear": svgGear, "warning": svgWarning, "moon": svgMoon,
	}
	for name, svg := range cases {
		builtin, ok := gxicon.Lookup(name)
		if !ok {
			t.Fatalf("内置表缺少 %q", name)
		}
		if builtin != svg {
			t.Fatalf("内置 SVG %q 与组件库常量不一致", name)
		}
	}
}
