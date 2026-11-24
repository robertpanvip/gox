package widgets

import (
	"testing"

	"github.com/robertpanvip/gox/runtime/canvas"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// hasVectorIcon 报告节点子树里是否存在带矢量绘制指令的 canvas 节点。
func hasVectorIcon(n *gxscene.Node) bool {
	if n == nil {
		return false
	}
	if n.Kind == "canvas" {
		if raw, ok := n.Props[canvas.PropKey].(string); ok && raw != "" && raw != "[]" {
			return true
		}
	}
	for _, c := range n.Children {
		if hasVectorIcon(c) {
			return true
		}
	}
	return false
}

func TestAlertHasSemanticVectorIcon(t *testing.T) {
	for _, tone := range []string{"info", "success", "warning", "danger"} {
		inst := Alert(gxscene.Props{
			"title": "提示", "message": "正文", "tone": tone, "width": "300",
		})
		if !hasVectorIcon(inst.Root()) {
			t.Errorf("tone=%q 的 Alert 应含语义矢量图标", tone)
		}
	}
}

func TestEmptyStateUsesVectorIcon(t *testing.T) {
	// 传内置图标名 → 矢量 canvas；不再依赖 "◻" 之类的字体字符。
	inst := EmptyState(gxscene.Props{
		"icon": "folder", "title": "空", "description": "无内容",
	})
	if !hasVectorIcon(inst.Root()) {
		t.Error("EmptyState 应渲染矢量图标，而非文字占位")
	}
	// 不传 icon 时用默认 folder，同样有矢量图标。
	def := EmptyState(gxscene.Props{"title": "空"})
	if !hasVectorIcon(def.Root()) {
		t.Error("EmptyState 默认图标应是矢量")
	}
}

func TestInputOptionalIcon(t *testing.T) {
	// 无图标：不崩溃、无 canvas。
	plain := Input(gxscene.Props{"value": func() any { return "" }})
	if hasVectorIcon(plain.Root()) {
		t.Error("未传 icon 的 Input 不应有图标")
	}
	// 有图标：搜索矢量节点。
	withIcon := Input(gxscene.Props{"value": func() any { return "" }, "icon": "search"})
	if !hasVectorIcon(withIcon.Root()) {
		t.Error("带 icon 的 Input 应渲染前置矢量图标")
	}
	// 有图标时输入结构仍保留 input 节点。
	if findKind(withIcon.Root(), "input") == nil {
		t.Error("带图标后 Input 结构应保留 input 节点")
	}
}

func TestButtonOptionalIcon(t *testing.T) {
	plain := Button(gxscene.Props{"label": "Go", "variant": "primary"})
	if hasVectorIcon(plain.Root()) {
		t.Error("未传 icon 的 Button 不应有图标")
	}
	withIcon := Button(gxscene.Props{"label": "Go", "variant": "primary", "icon": "plus"})
	if !hasVectorIcon(withIcon.Root()) {
		t.Error("带 icon 的 Button 应渲染矢量图标")
	}
	// -- 危险/次要按钮同样支持图标，不崩溃。
	for _, v := range []string{"secondary", "danger", "ghost"} {
		b := Button(gxscene.Props{"label": "Go", "variant": v, "icon": "trash"})
		if !hasVectorIcon(b.Root()) {
			t.Errorf("variant=%q 的带图标 Button 应渲染矢量图标", v)
		}
	}
}

func TestSelectUsesVectorIcons(t *testing.T) {
	state := NewPopoverState(false)
	opts := []Option{{Label: "A", Value: "a"}, {Label: "B", Value: "b"}}
	// 未选中：至少下拉箭头是矢量。
	inst := Select(gxscene.Props{
		"value": func() any { return "" }, "options": opts,
		"state": func() any { return state }, "placeholder": "选择",
	})
	if !hasVectorIcon(inst.Root()) {
		t.Error("Select 触发器的下拉箭头应是矢量图标")
	}
	// 选中态：菜单里出现 check 矢量（展开后扫描整棵树）。
	state.Open()
	sel := Select(gxscene.Props{
		"value": func() any { return "a" }, "options": opts,
		"state": func() any { return state },
	})
	if !hasVectorIcon(sel.Root()) {
		t.Error("Select 选中项应含矢量 check 图标")
	}
}
