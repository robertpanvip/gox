package widgets

import (
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// 本文件用同步 effect 接线（signal.Effect 异步，写后不保证当场刷新）。
var _ = syncEffect

func fieldControl() *gxscene.Instance {
	return Input(gxscene.Props{"value": func() any { return "" }, "width": "200"})
}

func TestFormStructure(t *testing.T) {
	form := Form(gxscene.Props{
		"title":    "登录",
		"onSubmit": BindSubmit(func() {}),
		"children": func() any { return nil },
	})
	root := form.Root()
	node := findByGxRole(root, "form")
	if node == nil {
		t.Fatal("缺少 gx-role=form 容器")
	}
	if findByGxRole(root, "form-title") == nil {
		t.Error("缺少表单标题")
	}
	btn := findByGxRole(root, "form-submit")
	if btn == nil || btn.Handler("onClick") == nil {
		t.Error("缺少带 onClick 的提交按钮")
	}
}

func TestFormNoSubmitButtonWithoutHandler(t *testing.T) {
	form := Form(gxscene.Props{"children": func() any { return nil }})
	if findByGxRole(form.Root(), "form-submit") != nil {
		t.Error("未传 onSubmit 时不应渲染提交按钮")
	}
}

func TestFormErrorsSummary(t *testing.T) {
	errs := signal.New(map[string]string{"email": "格式错误", "name": "必填"})
	form := Form(gxscene.Props{
		"errors":   func() any { return errs() },
		"children": func() any { return nil },
	})
	form.BindReactive(signal.Effect)
	root := form.Root()
	sum := findByGxRole(root, "form-errors")
	if sum == nil {
		t.Fatal("错误摘要未渲染")
	}
	text := gxscene.TextBackend{}.Paint(root)
	if !containsStr(text, "email") || !containsStr(text, "格式错误") {
		t.Errorf("摘要应含字段错误: %q", text)
	}
	// 错误清空 → 同步 effect 驱动摘要文本变空
	errs(map[string]string{})
	got := gxscene.TextBackend{}.Paint(form.Root())
	if containsStr(got, "格式错误") {
		t.Errorf("错误清空后不应仍有错误文本: %q", got)
	}
}

func TestFormItem(t *testing.T) {
	err := signal.New("邮箱必填")
	item := FormItem(gxscene.Props{
		"label":    "邮箱",
		"name":     "email",
		"required": true,
		"hint":     "我们不会公开",
		"error":    func() any { return err() },
		"children": func() any { return fieldControl() },
	})
	item.BindReactive(signal.Effect)
	root := item.Root()
	if findByGxRole(root, "form-item") == nil {
		t.Fatal("缺少 form-item")
	}
	if findByGxRole(root, "form-label") == nil {
		t.Error("缺少 label")
	}
	if findByGxRole(root, "form-hint") == nil {
		t.Error("缺少 hint")
	}
	if findByGxRole(root, "form-field-error") == nil {
		t.Error("应显示字段错误")
	}
	if findKind(root, "input") == nil {
		t.Error("控件子树未挂载")
	}
	text := gxscene.TextBackend{}.Paint(root)
	if !containsStr(text, "*") {
		t.Error("required 应显示必填星号")
	}
	// 错误清空
	err("")
	got2 := gxscene.TextBackend{}.Paint(item.Root())
	if containsStr(got2, "邮箱必填") {
		t.Errorf("错误清空后不应仍有错误文本: %q", got2)
	}
}

func TestBindSubmit(t *testing.T) {
	called := false
	h := BindSubmit(func() { called = true })
	h(gxscene.EventContext{})
	if !called {
		t.Error("BindSubmit 回调未触发")
	}
}
