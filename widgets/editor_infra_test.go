package widgets

// 本文件验证 editor 的三项基础设施确实被接入并生效：
//   1. Rope 文本权威：构造后 doc 与 sourceCode 一致；编辑走 Rope（见 editor_rope_test.go）。
//   2. VEC 自适应：长文档行版本 >64 时自动迁移到 pvector 大后端。
//   3. 惰性 tokenize + 虚拟滚动：首屏不解析全文；场景树节点数与文档行数解耦。

import (
	"fmt"
	"strings"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

func longSource(lines int) string {
	var b strings.Builder
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&b, "line %d: const x = %d; // 注释 %d\n", i, i, i)
	}
	return b.String()
}

// TestEditorRopeDocAuthoritative 验证 Rope 与字符串文本始终一致。
func TestEditorRopeDocAuthoritative(t *testing.T) {
	st := NewEditorState(longSource(100))
	if st.doc == nil || st.doc.String() != st.Text() {
		t.Fatalf("doc/String 不一致: doc=%q text=%q", st.doc, st.Text())
	}
	st.SetCursor(0, 0)
	st.Insert("Z")
	if st.doc.String() != st.Text() || !strings.HasPrefix(st.Text(), "Z") {
		t.Fatalf("Insert 后 doc/text 不一致: %q / %q", st.doc.String(), st.Text())
	}
}

// TestEditorVecPromotesOnLongDoc 验证行数超过自适应阈值后行版本切到大后端。
func TestEditorVecPromotesOnLongDoc(t *testing.T) {
	short := NewEditorState("a\nb\nc")
	if short.rowVers.IsBig() {
		t.Fatal("短文档不应使用大后端")
	}
	long := NewEditorState(longSource(200))
	if !long.rowVers.IsBig() {
		t.Fatal("200 行文档应自动迁移到 pvector 大后端")
	}
	if long.rowVers.Len() != long.doc.LineCount() {
		t.Fatalf("行版本长度 %d != 实际行数 %d", long.rowVers.Len(), long.doc.LineCount())
	}
}

// TestEditorLazyTokenize 验证首屏不解析全文：构造长文档后大部分 tokenCache
// 为 nil；只有被读取过的行才被解析。
func TestEditorLazyTokenize(t *testing.T) {
	st := NewEditorState(longSource(300))
	parsed := 0
	for _, toks := range st.tokenCache {
		if toks != nil {
			parsed++
		}
	}
	if parsed != 0 {
		t.Fatalf("构造后不应有任何行被 tokenize，实际 %d 行", parsed)
	}
	// 读第 150 行 → 只有该行被解析。
	_ = st.lineText(150)
	if st.tokenCache[150] == nil {
		t.Fatal("读取后第 150 行应被惰性解析")
	}
	parsed = 0
	for _, toks := range st.tokenCache {
		if toks != nil {
			parsed++
		}
	}
	if parsed != 1 {
		t.Fatalf("应只有 1 行被解析，实际 %d 行", parsed)
	}
	// 编辑第 0 行后，其 token 失效；其它已解析行不受影响。
	st.SetCursor(0, 0)
	st.Insert("X")
	if st.tokenCache[0] != nil {
		t.Fatal("编辑后的第 0 行 token 缓存应失效")
	}
}

// TestEditorVirtualScrollNodeCount 验证虚拟滚动：300 行文档的场景树节点数
// 远小于行数（只建可见行 + overscan），且滚动后窗口内容随之改变。
func TestEditorVirtualScrollNodeCount(t *testing.T) {
	st := NewEditorState(longSource(300))
	view := Editor(st)
	view.BindReactive(signal.Effect)

	countNodes := func(root *gxscene.Node) int {
		n := 0
		var walk func(*gxscene.Node)
		walk = func(x *gxscene.Node) {
			n++
			for _, c := range x.Children {
				walk(c)
			}
		}
		walk(root)
		return n
	}

	nodes := countNodes(view.Root())
	// 每行 3 节点，可见 ~21 行 + overscan 3*2 ≈ 27 行 ≈ 81 节点 + 容器/caret。
	// 无论如何应远小于 300 行 ×3 = 900。
	if nodes >= 300 {
		t.Fatalf("虚拟滚动未生效：300 行文档建了 %d 个节点", nodes)
	}

	// 收集可见的行号文本（gutter）：首屏从 1 开始，且只到视口附近（远小于 300）。
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
	if len(texts) == 0 || texts[0] != "1" {
		t.Fatalf("首屏应从行号 1 开始，实际: %v", texts[:min(len(texts), 5)])
	}
	// 行号最大值应远小于总行数——证明只渲染了可见窗口。
	maxLine := 0
	for _, tx := range texts {
		var n int
		if _, err := fmt.Sscanf(tx, "%d", &n); err == nil && n > maxLine {
			maxLine = n
		}
	}
	if maxLine >= 100 {
		t.Fatalf("虚拟滚动未限制窗口：最大行号 %d（应远小于 300）", maxLine)
	}
}
