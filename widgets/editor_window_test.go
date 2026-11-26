package widgets

import (
	"fmt"
	"strings"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// editorWindowLines 用与 app 场景一致的接线（叶子 + 结构两条路径）收集
// 编辑器虚拟窗口当前渲染的行号文本（gutter 数字）。
func editorWindowLines(t *testing.T, view *gxscene.Instance) []string {
	t.Helper()
	var texts []string
	var collect func(*gxscene.Node)
	collect = func(x *gxscene.Node) {
		if x.Type == gxscene.NodeText && x.Text != "" {
			texts = append(texts, x.Text)
		}
		for _, c := range x.Children {
			collect(c)
		}
	}
	collect(view.Root())
	return texts
}

// TestEditorSetSourceGrowsWindow 复现 wasm 实测缺陷：编辑器初始 11 行、
// 整篇替换成 60 行后，虚拟窗口必须扩展出新行（DOM 行节点数跟着变）。
// 之前的现象是窗口停在初始行数——"代码区残留旧内容"的根因之一。
func TestEditorSetSourceGrowsWindow(t *testing.T) {
	st := NewEditorState("package main\n\nfunc view() {}\n")
	view := Editor(st)
	// app 场景接线：叶子 + 结构互斥双路径（app.go BindLeafEffects + BindStructural）。
	view.BindLeafEffects(signal.Effect)
	view.BindStructural(signal.Effect, func() {})

	texts := editorWindowLines(t, view)
	if len(texts) == 0 || texts[0] != "1" {
		t.Fatalf("初始窗口应从行号 1 开始: %v", texts)
	}

	// 整篇替换成 60 行 → lineCount 60 > 视口 21 行 → 窗口应扩展。
	var sb strings.Builder
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	st.SetText(sb.String())

	texts = editorWindowLines(t, view)
	maxLine := 0
	for _, tx := range texts {
		var n int
		if _, err := fmt.Sscanf(tx, "%d", &n); err == nil && n > maxLine {
			maxLine = n
		}
	}
	if maxLine <= 11 {
		t.Fatalf("SetText 后窗口未扩展：最大行号 %d（应大于 11，60 行文档）", maxLine)
	}
}

// TestEditorScrollMovesWindowStructural 验证 app 场景接线（BindStructural）
// 下滚轮路径（ScrollLines → SetScrollTop）移动虚拟窗口。
func TestEditorScrollMovesWindowStructural(t *testing.T) {
	st := NewEditorState(longSource(300))
	view := Editor(st)
	view.BindLeafEffects(signal.Effect)
	view.BindStructural(signal.Effect, func() {})

	st.ScrollLines(30) // 下滚 30 行 → scrollTop 660px → 窗口应离开第 0 行

	texts := editorWindowLines(t, view)
	if len(texts) == 0 || texts[0] != "1" {
		// 首行号不再是 1 —— 窗口已滚动，符合预期
		t.Logf("滚动后首行号 = %s（窗口已移动）", texts[0])
		return
	}
	t.Fatal("ScrollLines(30) 后窗口仍从第 1 行开始——滚动未生效")
}
