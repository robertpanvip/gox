package rope

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// ---------- 基础构造与查询 ----------

func TestEmptyAndBasics(t *testing.T) {
	var z Rope // 零值
	if z.String() != "" || z.RuneLen() != 0 || z.ByteLen() != 0 || z.LineCount() != 1 {
		t.Fatalf("零值异常: %q len=%d bytes=%d lines=%d", z.String(), z.RuneLen(), z.ByteLen(), z.LineCount())
	}
	r := Of("hello")
	if r.String() != "hello" || r.RuneLen() != 5 || r.ByteLen() != 5 || r.LineCount() != 1 {
		t.Fatalf("基础查询异常: %q len=%d bytes=%d lines=%d", r.String(), r.RuneLen(), r.ByteLen(), r.LineCount())
	}
	if _, ok := r.RuneAt(-1); ok {
		t.Error("RuneAt(-1) 应失败")
	}
	if _, ok := r.RuneAt(5); ok {
		t.Error("RuneAt(越界) 应失败")
	}
	if ch, ok := r.RuneAt(1); !ok || ch != 'e' {
		t.Errorf("RuneAt(1) = %q,%v", ch, ok)
	}
}

func TestLineCountAndLines(t *testing.T) {
	cases := []struct {
		s     string
		lines int
		want  []string
	}{
		{"", 1, []string{""}},
		{"a", 1, []string{"a"}},
		{"a\nb", 2, []string{"a", "b"}},
		{"a\n", 2, []string{"a", ""}},
		{"\n", 2, []string{"", ""}},
		{"a\nb\nc", 3, []string{"a", "b", "c"}},
	}
	for _, c := range cases {
		r := Of(c.s)
		if got := r.LineCount(); got != c.lines {
			t.Errorf("LineCount(%q) = %d, want %d", c.s, got, c.lines)
		}
		got := r.Lines()
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("Lines(%q) = %q, want %q", c.s, got, c.want)
		}
	}
}

// ---------- 行列换算 ----------

func TestOffsetLineColRoundTrip(t *testing.T) {
	s := "func main() {\n    x := 1\n    println(x)\n}\n"
	r := Of(s)
	runes := []rune(s)
	for off := 0; off <= len(runes); off++ {
		line, col := r.OffsetToLineCol(off)
		back := r.LineColToOffset(line, col)
		if back != off {
			t.Fatalf("往返失败: off=%d -> (%d,%d) -> %d", off, line, col, back)
		}
	}
}

func TestLineStart(t *testing.T) {
	s := "aa\nbbb\nc"
	r := Of(s)
	// rune 偏移：aa=0,1; \n=2; bbb=3,4,5; \n=6; c=7
	want := []int{0, 3, 7}
	for line, w := range want {
		if got := r.LineStart(line); got != w {
			t.Errorf("LineStart(%d) = %d, want %d", line, got, w)
		}
	}
	if got := r.LineStart(99); got != r.RuneLen() {
		t.Errorf("LineStart(越界) = %d, want %d", got, r.RuneLen())
	}
}

func TestLineColClamping(t *testing.T) {
	r := Of("ab\ncd")
	// 列越界夹到行尾
	if got := r.LineColToOffset(0, 99); got != 2 {
		t.Errorf("列越界 = %d, want 2", got)
	}
	// 行越界夹到最后一行
	if got := r.LineColToOffset(99, 0); got != 3 {
		t.Errorf("行越界 = %d, want 3", got)
	}
	// 负行负列夹到 0
	if got := r.LineColToOffset(-5, -5); got != 0 {
		t.Errorf("负值 = %d, want 0", got)
	}
}

// ---------- UTF-8 / CJK ----------

func TestUTF8AndCJK(t *testing.T) {
	s := "中文abc\n你好"
	r := Of(s)
	if r.ByteLen() != len(s) {
		t.Errorf("ByteLen = %d, want %d", r.ByteLen(), len(s))
	}
	if r.RuneLen() != 8 { // 中文(2)+abc(3)+\n(1)+你好(2)
		t.Errorf("RuneLen = %d, want 8", r.RuneLen())
	}
	if r.LineCount() != 2 {
		t.Errorf("LineCount = %d, want 2", r.LineCount())
	}
	if got := r.Line(1); got != "你好" {
		t.Errorf("Line(1) = %q", got)
	}
	var b strings.Builder
	for i := 0; i < r.RuneLen(); i++ {
		ch, _ := r.RuneAt(i)
		b.WriteRune(ch)
	}
	if b.String() != s {
		t.Errorf("逐 rune 重建 = %q", b.String())
	}
	// 在 CJK 中间插入，不能切开 UTF-8
	r2 := r.Insert(1, "X")
	if !strings.Contains(r2.String(), "中X文") {
		t.Errorf("CJK 插入异常: %q", r2.String())
	}
}

// ---------- 编辑操作 ----------

func TestInsertDelete(t *testing.T) {
	r := Of("hello world")
	r = r.Insert(5, ",")
	if r.String() != "hello, world" {
		t.Fatalf("Insert = %q", r.String())
	}
	r = r.Delete(5, 1)
	if r.String() != "hello world" {
		t.Fatalf("Delete = %q", r.String())
	}
	// 头尾插入
	if got := Of("b").Insert(0, "a").Insert(2, "c").String(); got != "abc" {
		t.Errorf("头尾插入 = %q", got)
	}
	// 删除整段
	if got := Of("abc").Delete(0, 3).String(); got != "" {
		t.Errorf("删空 = %q", got)
	}
	// 越界夹紧
	if got := Of("ab").Insert(999, "X").String(); got != "abX" {
		t.Errorf("越界插入 = %q", got)
	}
	if got := Of("ab").Delete(999, 5).String(); got != "ab" {
		t.Errorf("越界删除 = %q", got)
	}
}

func TestInsertCrossesLeafBoundary(t *testing.T) {
	// 构造超过一个叶子（leafMaxRunes=128）的文本，验证跨叶子插删。
	s := strings.Repeat("a", leafMaxRunes*3+7)
	r := Of(s)
	if r.String() != s {
		t.Fatal("初始大文本不一致")
	}
	r = r.Insert(leafMaxRunes+2, "XYZ")
	want := s[:leafMaxRunes+2] + "XYZ" + s[leafMaxRunes+2:]
	if r.String() != want {
		t.Fatalf("跨叶子插入失败")
	}
	r = r.Delete(leafMaxRunes, 10)
	want = want[:leafMaxRunes] + want[leafMaxRunes+10:]
	if r.String() != want {
		t.Fatalf("跨叶子删除失败")
	}
}

// ---------- 持久性 / 结构共享 ----------

func TestPersistentImmutable(t *testing.T) {
	base := Of("abcdef")
	edited := base.Insert(3, "XX")
	deleted := base.Delete(1, 2)
	if base.String() != "abcdef" {
		t.Errorf("Insert 污染了原值: %q", base.String())
	}
	if edited.String() != "abcXXdef" {
		t.Errorf("edited = %q", edited.String())
	}
	if deleted.String() != "adef" {
		t.Errorf("deleted = %q", deleted.String())
	}
	// 未触碰的旧子树被复用（结构共享）：对同一基址做两次独立编辑，
	// 它们的根不同，但共享前缀应指向同一节点对象。
	a := Of(strings.Repeat("L", 300) + "MID" + strings.Repeat("R", 300))
	x := a.Insert(301, "X")
	y := a.Insert(301, "Y")
	if x.root == y.root {
		t.Error("两次独立编辑不应产生相同根")
	}
	if x.String() == y.String() {
		t.Error("不同插入内容结果却相同")
	}
}

// ---------- 平衡性 ----------

func checkAVL(t *testing.T, n *node) int {
	t.Helper()
	if n == nil {
		return 0
	}
	if n.left == nil && n.right == nil {
		if n.height != 1 {
			t.Errorf("叶子 height=%d", n.height)
		}
		return 1
	}
	hl, hr := checkAVL(t, n.left), checkAVL(t, n.right)
	if hl-hr > 1 || hr-hl > 1 {
		t.Errorf("失衡节点: hl=%d hr=%d runes=%d", hl, hr, n.runes)
	}
	want := hl
	if hr > want {
		want = hr
	}
	if n.height != want+1 {
		t.Errorf("height 缓存错误: got=%d want=%d", n.height, want+1)
	}
	if n.left != nil && n.right != nil {
		if n.runes != n.left.runes+n.right.runes || n.lines != n.left.lines+n.right.lines {
			t.Errorf("聚合错误 at runes=%d", n.runes)
		}
	}
	return n.height
}

func TestAVLBalanced(t *testing.T) {
	check := func(n *node) int { return checkAVL(t, n) }
	r := Of("")
	for i := 0; i < 500; i++ {
		pos := rand.Intn(r.RuneLen() + 1)
		r = r.Insert(pos, string(rune('a'+rand.Intn(26))))
		if i%50 == 0 {
			check(r.root)
		}
	}
	check(r.root)
	if h := height(r.root); h > 12 {
		t.Errorf("500 rune 文本树高 %d 过高", h)
	}
}

// ---------- 随机差分测试（对照 string 模型） ----------

func TestRandomAgainstStringModel(t *testing.T) {
	rnd := rand.New(rand.NewSource(42))
	alphabet := []rune("abcXYZ中文\n ")
	var r *Rope = Of("seed\ntext")
	model := []rune("seed\ntext")

	for step := 0; step < 3000; step++ {
		if rnd.Intn(2) == 0 {
			// 插入
			off := rnd.Intn(len(model) + 1)
			n := 1 + rnd.Intn(5)
			var ins []rune
			for i := 0; i < n; i++ {
				ins = append(ins, alphabet[rnd.Intn(len(alphabet))])
			}
			r = r.Insert(off, string(ins))
			tail := append([]rune{}, model[off:]...)
			model = append(model[:off], ins...)
			model = append(model, tail...)
		} else {
			// 删除
			if len(model) == 0 {
				continue
			}
			off := rnd.Intn(len(model))
			max := len(model) - off
			n := 1 + rnd.Intn(max)
			r = r.Delete(off, n)
			model = append(model[:off], model[off+n:]...)
		}

		got := r.String()
		want := string(model)
		if got != want {
			t.Fatalf("step %d 文本不一致:\n got=%q\nwant=%q", step, got, want)
		}
		if r.RuneLen() != len(model) {
			t.Fatalf("step %d RuneLen=%d want=%d", step, r.RuneLen(), len(model))
		}
		if r.ByteLen() != len(want) {
			t.Fatalf("step %d ByteLen=%d want=%d", step, r.ByteLen(), len(want))
		}
		if r.LineCount() != strings.Count(want, "\n")+1 {
			t.Fatalf("step %d LineCount=%d", step, r.LineCount())
		}
		// 每 200 步校验一次行列换算与行文本
		if step%200 == 0 {
			checkLineCols(t, r, model)
		}
	}
}

func checkLineCols(t *testing.T, r *Rope, model []rune) {
	t.Helper()
	s := string(model)
	wantLines := strings.Split(s, "\n")
	if r.LineCount() != len(wantLines) {
		t.Fatalf("行数不符: got=%d want=%d", r.LineCount(), len(wantLines))
	}
	for i := 0; i < len(wantLines); i++ {
		if got := r.Line(i); got != wantLines[i] {
			t.Fatalf("Line(%d)=%q want=%q (full=%q)", i, got, wantLines[i], s)
		}
	}
	for off := 0; off <= len(model); off++ {
		line, col := r.OffsetToLineCol(off)
		back := r.LineColToOffset(line, col)
		if back != off {
			t.Fatalf("往返失败 off=%d (%d,%d)->%d full=%q", off, line, col, back, s)
		}
	}
}

// ---------- 基准 ----------

// 公平对比：都在同一份大文本上"连续插入"，而非每轮重建输入。
// Rope 的 Of 构造成本 O(N) 是一次性的，string 基线则每次插入都是 O(N) 拷贝。
func BenchmarkRopeInsert(b *testing.B) {
	r := Of(strings.Repeat("// line of code\n", 2000))
	mid := r.RuneLen() / 2
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r = r.Insert(mid, "x")
		mid++
	}
}

func BenchmarkStringInsert(b *testing.B) {
	s := strings.Repeat("// line of code\n", 2000)
	mid := len(s) / 2
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s = s[:mid] + "x" + s[mid:]
		mid++
	}
}

func BenchmarkRopeLineCol(b *testing.B) {
	s := strings.Repeat("// line of code\n", 2000)
	r := Of(s)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.OffsetToLineCol(i % r.RuneLen())
	}
}

func ExampleRope() {
	r := Of("hello\nworld")
	r = r.Insert(5, ",")
	line, col := r.OffsetToLineCol(3)
	fmt.Printf("%s | %d:%d\n", r.Line(1), line, col)
	// Output: world | 0:3
}
