package widgets

import (
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// syncEffect 是测试用的同步 effect：立即执行一次且不依赖调度器，
// 保证 BindReactive 后 Branch 当场解析（signal.Effect 是异步调度的）。
func syncEffect(f func()) signal.Computation {
	f()
	return nil
}

func modalBody() *gxscene.Instance {
	return Text(gxscene.Props{"value": func() any { return "确定要删除吗？" }})
}

func modalActions() *gxscene.Instance {
	return Row(gxscene.Props{"gap": Space2, "children": func() any {
		return []any{
			Button(gxscene.Props{"label": "取消"}),
			Button(gxscene.Props{"label": "删除", "variant": "danger"}),
		}
	}})
}

func fireHandler(t *testing.T, n *gxscene.Node, ev string) {
	t.Helper()
	h := n.Handler(ev)
	if h == nil {
		t.Fatalf("节点 %s(gx-role=%s) 未绑定 %s", n.Kind, n.Props["gx-role"], ev)
	}
	fn, ok := h.(gxscene.Handler)
	if !ok {
		t.Fatalf("%s handler 类型错误: %T", ev, h)
	}
	fn(gxscene.EventContext{Synthetic: &gxscene.SyntheticEvent{Type: gxscene.EvClick, NativeName: ev}})
}

func TestModalClosedNoOverlay(t *testing.T) {
	st := NewModalState(false)
	m := Modal(gxscene.Props{
		"state":   st,
		"title":   func() any { return "删除确认" },
		"content": func() any { return modalBody() },
	})
	root := m.Root()
	if findByGxRole(root, "modal") != nil || findByGxRole(root, "scrim") != nil {
		t.Error("初始关闭不应有 modal/scrim 子树")
	}
}

func TestModalOpenStructure(t *testing.T) {
	st := NewModalState(true)
	m := Modal(gxscene.Props{
		"state":        st,
		"title":        func() any { return "删除确认" },
		"content":      func() any { return modalBody() },
		"actions":      func() any { return modalActions() },
		"width":        "480",
		"maskClosable": true,
	})
	m.BindReactive(syncEffect)
	root := m.Root()

	panel := findByGxRole(root, "modal")
	if panel == nil {
		t.Fatal("打开应有 gx-role=modal 面板")
	}
	if panel.Props["width"] != "480" {
		t.Errorf("宽度 = %v, want 480", panel.Props["width"])
	}
	if panel.Props["border-radius"] != RadiusModal {
		t.Errorf("圆角 = %v, want %v", panel.Props["border-radius"], RadiusModal)
	}
	if panel.Props["box-shadow"] != ShadowModal {
		t.Errorf("缺少 Apple 阴影: %v", panel.Props["box-shadow"])
	}
	// 标题
	if findKind(panel, "span") == nil {
		t.Error("面板缺少标题文本")
	}
	// 关闭按钮
	if findByGxRole(panel, "modal-close") == nil {
		t.Error("缺少关闭按钮")
	}
	// 遮罩 + 内容
	scrim := findByGxRole(root, "scrim")
	if scrim == nil {
		t.Fatal("缺少 scrim 遮罩")
	}
	if scrim.Props["bg"] != AppleScrim {
		t.Errorf("遮罩色 = %v, want %v", scrim.Props["bg"], AppleScrim)
	}
	txt := gxscene.TextBackend{}.Paint(root)
	if !containsStr(txt, "删除确认") || !containsStr(txt, "确定要删除吗") {
		t.Errorf("缺少标题/内容文本: %q", txt)
	}

	// 点遮罩关闭（maskClosable=true）
	fireHandler(t, scrim, "onClick")
	if st.IsOpen() {
		t.Error("maskClosable=true 时点遮罩应关闭")
	}
}

func TestModalMaskNotClosableByDefault(t *testing.T) {
	st := NewModalState(true)
	m := Modal(gxscene.Props{
		"state":   st,
		"content": func() any { return modalBody() },
	})
	m.BindReactive(syncEffect)
	scrim := findByGxRole(m.Root(), "scrim")
	if scrim == nil {
		t.Fatal("缺少 scrim")
	}
	fireHandler(t, scrim, "onClick")
	if !st.IsOpen() {
		t.Error("maskClosable 默认 false，点遮罩不应关闭")
	}
}

func TestModalCloseButtonAndESC(t *testing.T) {
	st := NewModalState(true)
	m := Modal(gxscene.Props{
		"state":   st,
		"title":   func() any { return "提示" },
		"content": func() any { return modalBody() },
	})
	m.BindReactive(syncEffect)
	closeBtn := findByGxRole(m.Root(), "modal-close")
	if closeBtn == nil {
		t.Fatal("缺少关闭按钮")
	}
	fireHandler(t, closeBtn, "onClick")
	if st.IsOpen() {
		t.Error("点关闭按钮应关闭")
	}

	st.Open()
	if !ModalHandleKey(st, "Escape") {
		t.Error("ESC 应被消费")
	}
	if st.IsOpen() {
		t.Error("ESC 应关闭模态框")
	}
	st.Open()
	if ModalHandleKey(st, "Enter") {
		t.Error("非 ESC 键不应消费")
	}
	if !st.IsOpen() {
		t.Error("非 ESC 键不应关闭")
	}
}

func TestPortalStructure(t *testing.T) {
	// 受控 open=false：无 portal 子树
	closed := Portal(gxscene.Props{
		"open":     func() any { return false },
		"children": func() any { return modalBody() },
	})
	closed.BindReactive(syncEffect)
	if findByGxRole(closed.Root(), "portal") != nil {
		t.Error("open=false 不应有 portal 子树")
	}

	// 无 open：默认常开
	p := Portal(gxscene.Props{
		"children": func() any { return modalBody() },
	})
	p.BindReactive(syncEffect)
	portal := findByGxRole(p.Root(), "portal")
	if portal == nil {
		t.Fatal("默认应有 portal 子树")
	}
	if !containsStr(gxscene.TextBackend{}.Paint(portal), "确定要删除吗") {
		t.Error("portal 未包含子树内容")
	}
}
