package widgets

import (
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

func TestTextAndHeading(t *testing.T) {
	txt := gxscene.TextBackend{}.Paint(Text(gxscene.Props{
		"value": func() any { return "hello" },
		"tone":  "success",
		"size":  "16",
	}).Root())
	if !containsStr(txt, "hello") {
		t.Errorf("Text 应渲染文本: %q", txt)
	}
	h := gxscene.TextBackend{}.Paint(Heading(gxscene.Props{
		"value": func() any { return "标题" },
		"level": 1,
	}).Root())
	if !containsStr(h, "标题") {
		t.Errorf("Heading 应渲染标题: %q", h)
	}
	// level=1 字号 26
	if got := findAttrDeep(Heading(gxscene.Props{"value": func() any { return "X" }, "level": 1}).Root(), "font-size"); got != "26" {
		t.Errorf("h1 字号 = %v, want 26", got)
	}
}

func TestAvatar(t *testing.T) {
	a := Avatar(gxscene.Props{"name": func() any { return "Alice" }, "tone": "purple"})
	node := findByRole(a.Root(), "avatar")
	if node == nil {
		t.Fatal("未生成 avatar 节点")
	}
	if got := node.Props["bg"]; got != tones["purple"] {
		t.Errorf("头像底色 = %v, want %v", got, tones["purple"])
	}
	txt := gxscene.TextBackend{}.Paint(a.Root())
	if !containsStr(txt, "A") {
		t.Errorf("头像应显示首字母 A: %q", txt)
	}
}

func TestStat(t *testing.T) {
	inst := Stat(gxscene.Props{
		"label": func() any { return "访问量" },
		"value": func() any { return "12,345" },
		"delta": func() any { return "+8%" },
		"tone":  "green",
	})
	txt := gxscene.TextBackend{}.Paint(inst.Root())
	for _, s := range []string{"访问量", "12,345", "+8%"} {
		if !containsStr(txt, s) {
			t.Errorf("Stat 应包含 %q: %q", s, txt)
		}
	}
	if findByRole(inst.Root(), "stat") == nil {
		t.Error("缺少 stat 角色节点")
	}
}

func TestStatNoDelta(t *testing.T) {
	inst := Stat(gxscene.Props{
		"label": func() any { return "L" },
		"value": func() any { return "V" },
	})
	// 无 delta 时 N=2，不应报错且文本完整。
	txt := gxscene.TextBackend{}.Paint(inst.Root())
	if !containsStr(txt, "L") || !containsStr(txt, "V") {
		t.Errorf("无 delta 的 Stat 文本 = %q", txt)
	}
}

func TestTag(t *testing.T) {
	tag := Tag(gxscene.Props{
		"label":   func() any { return "已发布" },
		"tone":    "green",
		"outline": func() any { return true },
	})
	n := findByRole(tag.Root(), "tag")
	if n == nil {
		t.Fatal("未生成 tag 节点")
	}
	if got := n.Props["color"]; got != tones["green"] {
		t.Errorf("描边 Tag 文字色 = %v, want %v", got, tones["green"])
	}
}

func TestAlert(t *testing.T) {
	inst := Alert(gxscene.Props{
		"title":   func() any { return "注意" },
		"message": func() any { return "这是一条警告" },
		"tone":    "orange",
	})
	txt := gxscene.TextBackend{}.Paint(inst.Root())
	if !containsStr(txt, "注意") || !containsStr(txt, "这是一条警告") {
		t.Errorf("Alert 文本 = %q", txt)
	}
	if findByRole(inst.Root(), "alert") == nil {
		t.Error("缺少 alert 角色节点")
	}
}

func TestEmptyStateAndSkeleton(t *testing.T) {
	e := EmptyState(gxscene.Props{
		"title": func() any { return "暂无数据" },
		"desc":  func() any { return "稍后再来" },
	})
	if findByRole(e.Root(), "empty") == nil {
		t.Error("缺少 empty 角色节点")
	}
	s := Skeleton(gxscene.Props{"width": "120", "height": "20"})
	n := findByRole(s.Root(), "skeleton")
	if n == nil {
		t.Fatal("未生成 skeleton 节点")
	}
	if n.Props["width"] != "120" || n.Props["height"] != "20" {
		t.Errorf("skeleton 尺寸错误: %+v", n.Props)
	}
}

func TestSpinner(t *testing.T) {
	if findByRole(Spinner(gxscene.Props{"tone": "info"}).Root(), "spinner") == nil {
		t.Error("缺少 spinner 角色节点")
	}
}

// findAttrDeep 深度优先查找第一个指定属性。
func findAttrDeep(n *gxscene.Node, key string) string {
	if n == nil {
		return ""
	}
	if v, ok := n.Props[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	for _, c := range n.Children {
		if got := findAttrDeep(c, key); got != "" {
			return got
		}
	}
	return ""
}
