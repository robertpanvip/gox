package icon

import (
	"strings"
	"testing"

	"github.com/robertpanvip/gox/runtime/canvas"
)

func TestLookupBuiltinAndAlias(t *testing.T) {
	if _, ok := Lookup("house"); !ok {
		t.Fatal("house 应存在")
	}
	svg, ok := Lookup("home")
	if !ok {
		t.Fatal("home 别名应解析到 house")
	}
	if !strings.Contains(svg, "<svg") {
		t.Fatal("内置图标应为 SVG 文本")
	}
	if _, ok := Lookup("nonexistent-icon"); ok {
		t.Fatal("未知图标不应命中")
	}
}

func TestNamesSortedUnique(t *testing.T) {
	names := Names()
	if len(names) < 20 {
		t.Fatalf("内置图标过少: %d", len(names))
	}
	for i := 1; i < len(names); i++ {
		if names[i] <= names[i-1] {
			t.Fatalf("Names 未排序去重: %q %q", names[i-1], names[i])
		}
	}
}

func TestParseBuiltinIconWithColor(t *testing.T) {
	ops, err := IconOps("plus", "", ParseOptions{Size: 24, Color: "#0a84ff", StrokeWidth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) == 0 {
		t.Fatal("plus 应产出绘制指令")
	}
	// currentColor 应被替换为注入色。
	found := false
	for _, op := range ops {
		if op.Stroke == "#0a84ff" {
			found = true
		}
	}
	if !found {
		t.Fatalf("currentColor 未替换为注入色: %+v", ops)
	}
}

func TestParseViewBoxScale(t *testing.T) {
	// 12x12 viewBox 的内容放大到 24：坐标应翻倍。
	svg := `<svg viewBox="0 0 12 12"><path d="M1 1 L10 10" fill="none" stroke="currentColor" stroke-width="1"/></svg>`
	ops, err := Parse(svg, ParseOptions{Size: 24, Color: "#fff"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 {
		t.Fatalf("期望 1 条 path，got %d", len(ops))
	}
	if !strings.HasPrefix(ops[0].D, "M2 2") {
		t.Fatalf("viewBox 缩放错误（1,1 应→2,2）: %q", ops[0].D)
	}
	if !strings.Contains(ops[0].D, "L20 20") {
		t.Fatalf("viewBox 缩放终点错误（10,10 应→20,20）: %q", ops[0].D)
	}
}

func TestParseAllShapes(t *testing.T) {
	svg := `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor">` +
		`<path d="M2 2 L6 2"/>` +
		`<circle cx="12" cy="12" r="5"/>` +
		`<rect x="16" y="2" width="6" height="6" rx="1"/>` +
		`<line x1="2" y1="20" x2="8" y2="20"/>` +
		`<polygon points="12,16 16,22 8,22"/>` +
		`</svg>`
	ops, err := Parse(svg, ParseOptions{Size: 24, Color: "#fff"})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[canvas.OpKind]int{}
	for _, op := range ops {
		kinds[op.Kind]++
	}
	if kinds[canvas.OpPath] < 2 || kinds[canvas.OpCircle] != 1 ||
		kinds[canvas.OpRect] != 1 || kinds[canvas.OpLine] != 1 {
		t.Fatalf("图元覆盖不全: %+v (ops=%+v)", kinds, ops)
	}
}

func TestParseDataURI(t *testing.T) {
	// data:image/svg+xml,<url-encoded>
	svg := `<svg viewBox="0 0 24 24"><path d="M2 2 L22 22" fill="none" stroke="currentColor"/></svg>`
	uri := "data:image/svg+xml," + urlEncode(svg)
	got, err := Resolve("", uri)
	if err != nil {
		t.Fatal(err)
	}
	if got != svg {
		t.Fatalf("data URI 解码不符: %q", got)
	}
}

func TestParseRawSVG(t *testing.T) {
	svg := `<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="8" fill="currentColor"/></svg>`
	got, err := Resolve("", svg)
	if err != nil {
		t.Fatal(err)
	}
	if got != svg {
		t.Fatal("原始 SVG 应原样返回")
	}
	ops, err := Parse(got, ParseOptions{Size: 30, Color: "#f00"})
	if err != nil || len(ops) != 1 {
		t.Fatalf("解析失败: %v ops=%+v", err, ops)
	}
	// r=8, size=30 → scale=30/24=1.25 → r=10
	if ops[0].R < 9.9 || ops[0].R > 10.1 {
		t.Fatalf("circle 半径缩放错误: %f", ops[0].R)
	}
}

// 简单 URL 编码（仅处理 < > # " 空格等 SVG 常见字符）。
func urlEncode(s string) string {
	r := strings.NewReplacer("%", "%25", "<", "%3C", ">", "%3E", "#", "%23", `"`, "%22", " ", "%20", "\n", "%0A")
	return r.Replace(s)
}
