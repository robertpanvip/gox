package widgets

import (
	"strings"
	"testing"
)

// TestNewEditorStateLineCacheSynced 回归测试：构造期必须同步填充行文本快照。
//
// 背景 bug：NewEditorState 只写 doc（Rope）与 sourceCode 信号，lineTextCache
// 留空；而渲染（rich-text/gradient）与点击换算（PickColumn）读的是
// lineText → textCache 缓存——构造后、首次 setSource 前的窗口里两者失步：
// 代码区显示空白、点击换算 col 恒 0、空格插错位置（-path 磁盘模式首开即复现）。
func TestNewEditorStateLineCacheSynced(t *testing.T) {
	src := "    return <div style={{ flexDirection: \"column\", gap: 6 }}>\n// 中文注释行\n}"
	st := NewEditorState(src)

	lines := strings.Split(src, "\n")
	for i, want := range lines {
		if got := st.lineText(i); got != want {
			t.Fatalf("lineText(%d) 与 doc 失步:\n  got:  %q\n  want: %q", i, got, want)
		}
	}

	// 失步的实际症状：构造后（未 setSource）直接点击换算，col 必须正确。
	col := st.PickColumn(0, float64(len("    return <div style"))*st.charWidth())
	if want := len("    return <div style"); col != want {
		t.Fatalf("构造后点击换算列错误: got %d, want %d", col, want)
	}

	// ensureLineParsed 的防御校验：外部篡改快照后，lineText 必须自动纠正。
	if len(st.lineTextCache) > 1 {
		st.lineTextCache[1] = "被篡改的旧内容"
		if got := st.lineText(1); got != lines[1] {
			t.Fatalf("快照篡改后 lineText 未从 doc 纠正: got %q, want %q", got, lines[1])
		}
	}
}
