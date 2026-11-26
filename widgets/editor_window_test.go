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

// TestEditorSetTextResetsScroll 复现桌面端缺陷：上一个长文档滚动到中部后
// 切换到短文档，残留的 scrollTop 会把短文档顶部滚掉（行号从 4 开始、顶部留白）。
// SetText 与 Home 必须把滚动偏移归零。
func TestEditorSetTextResetsScroll(t *testing.T) {
	st := NewEditorState(longSource(300))
	st.ScrollLines(30) // 先滚到中部
	if got := st.scrollTopVal; got <= 0 {
		t.Fatalf("前置条件失败：滚动后 scrollTop 应 > 0, got %v", got)
	}

	// 切到 17 行短文档（内容 374px < 视口 470px，本不该滚动）。
	st.SetText(shortSource(17))
	if got := st.scrollTopVal; got != 0 {
		t.Fatalf("SetText 后 scrollTop 应归零, got %v", got)
	}
}

// TestEditorScrollTopClampsToContent 验证 SetScrollTop 上界夹紧：
// 短文档（内容放得下）时任何正偏移都被夹到 0。
func TestEditorScrollTopClampsToContent(t *testing.T) {
	st := NewEditorState(shortSource(5)) // 5 行 × 22 = 110px << 470px 视口
	st.SetScrollTop(600)
	if got := st.scrollTopVal; got != 0 {
		t.Fatalf("内容放得下时 scrollTop 应夹到 0, got %v", got)
	}

	// 长文档允许滚动，但不得超过 内容高 - 视口高。
	st.SetText(longSource(100))
	st.SetScrollTop(1e6)
	maxScroll := float64(st.lineCount()*editorRowHeight - st.ViewportHeight())
	if maxScroll <= 0 {
		t.Fatalf("前置条件失败：长文档 maxScroll 应 > 0, got %v", maxScroll)
	}
	if got := st.scrollTopVal; got != maxScroll {
		t.Fatalf("长文档 scrollTop 应夹到 maxScroll=%v, got %v", maxScroll, got)
	}
}

func shortSource(n int) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	return sb.String()
}
