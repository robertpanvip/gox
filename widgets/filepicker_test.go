package widgets

import (
	"errors"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

type mockFileDialog struct {
	paths []string
	err   error
	got   FileOptions
	calls int
}

func (m *mockFileDialog) OpenFile(opts FileOptions) ([]string, error) {
	m.got = opts
	m.calls++
	return m.paths, m.err
}

func TestFilePickerStructure(t *testing.T) {
	fp := FilePicker(gxscene.Props{"label": "选择图片", "width": "200"})
	root := fp.Root()
	btn := findByGxRole(root, "filepicker")
	if btn == nil {
		t.Fatal("缺少 filepicker 按钮")
	}
	if btn.Handler("onClick") == nil {
		t.Error("缺少 onClick handler")
	}
	gotText := gxscene.TextBackend{}.Paint(root)
	if !containsStr(gotText, "选择图片") {
		t.Errorf("按钮文字缺失: %q", gotText)
	}
	if btn.Props["width"] != "200" {
		t.Errorf("width=%v want 200", btn.Props["width"])
	}
}

func TestFilePickerPick(t *testing.T) {
	m := &mockFileDialog{paths: []string{"/tmp/a.png", "/tmp/b.png"}}
	RegisterFileDialog(m)
	defer RegisterFileDialog(nil)

	var picked []string
	fp := FilePicker(gxscene.Props{
		"options": FileOptions{Multiple: true, Filters: []string{".png"}},
		"onPick":  BindPick(func(p []string) { picked = p }),
	})
	btn := findByGxRole(fp.Root(), "filepicker")
	btn.Dispatch("onClick", gxscene.EventContext{})

	if m.calls != 1 {
		t.Errorf("OpenFile 调用次数 = %d, want 1", m.calls)
	}
	if !m.got.Multiple || len(m.got.Filters) != 1 {
		t.Errorf("选项未透传: %+v", m.got)
	}
	if len(picked) != 2 || picked[0] != "/tmp/a.png" {
		t.Errorf("回传路径 = %v", picked)
	}
}

func TestFilePickerCancel(t *testing.T) {
	m := &mockFileDialog{err: errors.New("cancelled"), paths: nil}
	RegisterFileDialog(m)
	defer RegisterFileDialog(nil)

	picked, cancelled := false, false
	fp := FilePicker(gxscene.Props{
		"onPick":   BindPick(func([]string) { picked = true }),
		"onCancel": gxscene.Handler(func(gxscene.EventContext) { cancelled = true }),
	})
	btn := findByGxRole(fp.Root(), "filepicker")
	btn.Dispatch("onClick", gxscene.EventContext{})
	if picked {
		t.Error("取消时不应回传选中")
	}
	if !cancelled {
		t.Error("取消时应触发 onCancel")
	}
}

func TestFilePickerDisabled(t *testing.T) {
	m := &mockFileDialog{paths: []string{"/tmp/a"}}
	RegisterFileDialog(m)
	defer RegisterFileDialog(nil)

	called := false
	fp := FilePicker(gxscene.Props{
		"disabled": true,
		"onPick":   BindPick(func([]string) { called = true }),
	})
	btn := findByGxRole(fp.Root(), "filepicker")
	btn.Dispatch("onClick", gxscene.EventContext{})
	if m.calls != 0 || called {
		t.Error("disabled 时不应打开对话框")
	}
}

func TestFilePickerNoopDefault(t *testing.T) {
	RegisterFileDialog(nil) // 复位为 noop
	called := false
	fp := FilePicker(gxscene.Props{
		"onPick":   BindPick(func([]string) { called = true }),
		"onCancel": gxscene.Handler(func(gxscene.EventContext) {}),
	})
	btn := findByGxRole(fp.Root(), "filepicker")
	btn.Dispatch("onClick", gxscene.EventContext{}) // 不应 panic
	if called {
		t.Error("noop 实现不应回传选中")
	}
}
