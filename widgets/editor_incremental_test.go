package widgets

// 本文件验证 editor 的"解析一点、渲染一点"增量路径：
//   1. 编辑只失效受影响行，未变化行缓存保持命中。
//   2. sourceCode 惰性：打字期间不序列化全文，Text() 首次读取才 O(N)，之后缓存命中。
//   3. 打字耗时与文档规模无关（O(1)），不再随全文增长。

import (
	"strings"
	"testing"
	"time"
)

// TestIncrementalEditOnlyInvalidatesDirtyLine 验证行内插入只让目标行的 token
// 缓存失效，其余已解析行保持命中——这正是"解析多少、失效多少、重渲染多少"。
func TestIncrementalEditOnlyInvalidatesDirtyLine(t *testing.T) {
	st := NewEditorState(longSource(100))
	// 先解析第 0、1、2 行（模拟它们进入可见窗口）。
	_ = st.lineText(0)
	_ = st.lineText(1)
	_ = st.lineText(2)
	if st.tokenCache[0] == nil || st.tokenCache[1] == nil || st.tokenCache[2] == nil {
		t.Fatal("前置条件：0/1/2 行应已解析")
	}

	// 在第 1 行行内插入一个字符：只有第 1 行应失效。
	st.SetCursor(1, 1)
	st.Insert("Z")
	if st.tokenCache[1] != nil {
		t.Fatal("被编辑的第 1 行 token 缓存应失效")
	}
	if st.tokenCache[0] == nil || st.tokenCache[2] == nil {
		t.Fatal("未变化的第 0/2 行缓存应保持命中（增量失效）")
	}
	// 惰性重解析：读第 1 行才恢复，且内容含新字符。
	got := st.lineText(1)
	if !strings.Contains(got, "Z") {
		t.Fatalf("重解析后第 1 行应含新字符，实际 %q", got)
	}
}

// TestTextIsLazy 验证打字期间不做全文序列化：连续 Insert 后 textDirty 为真，
// sourceCode 仍是旧值；Text() 首次读取才同步为最新全文，之后缓存命中不再序列化。
func TestTextIsLazy(t *testing.T) {
	st := NewEditorState(longSource(50))
	before := st.Text() // 建立基线
	st.SetCursor(0, 0)
	st.Insert("HEAD")
	if !st.textDirty {
		t.Fatal("编辑应标 textDirty")
	}
	// 编辑后、未读 Text() 前，sourceCode 仍应是旧全文（全文序列化被推迟）。
	if got := st.sourceCode(); got != before {
		t.Fatal("打字期间不应同步全文字符串（应惰性）")
	}
	after := st.Text()
	if !strings.HasPrefix(after, "HEAD") {
		t.Fatalf("Text() 应返回含最新编辑的全文，实际前缀 %q", after[:min(len(after), 20)])
	}
	if st.textDirty {
		t.Fatal("Text() 读取后应清除 textDirty")
	}
	// 再次读取走缓存，值不变。
	if st.Text() != after {
		t.Fatal("缓存的 Text() 应稳定")
	}
}

// TestTypingComplexityIndependentOfDocSize 验证打字耗时与文档规模无关。
// 100 行与 20000 行的单次 Insert 都在一个宽松阈值内（相比改造前 20000 行 O(N) 的
// ~1.28ms，这里应稳定在微秒级）。用总时长比较而非单次，降低抖动。
func TestTypingComplexityIndependentOfDocSize(t *testing.T) {
	const reps = 300
	measure := func(n int) time.Duration {
		st := NewEditorState(benchDoc(n))
		mid := n / 2
		start := time.Now()
		for i := 0; i < reps; i++ {
			st.SetCursor(mid, 1)
			st.Insert("x")
		}
		return time.Since(start) / reps
	}
	small := measure(100)
	large := measure(20000)
	// O(1) 时两者都在微秒级；即使有树深度差异，20000 行也不应比 100 行慢 20 倍以上。
	if large > small*20 {
		t.Fatalf("打字耗时随文档规模显著增长（疑似退化回 O(N)）：100 行=%v, 20000 行=%v", small, large)
	}
	if large > 200*time.Microsecond {
		t.Fatalf("20000 行单次打字耗时 %v 过高（应微秒级）", large)
	}
}
