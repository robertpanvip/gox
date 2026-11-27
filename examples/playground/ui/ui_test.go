package ui

import (
	"strings"
	"testing"

	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
	"github.com/robertpanvip/gox-core/runtime/signal"
)

// 验证 GX 编译产物里的纯编辑逻辑（插入/退格/换行/中文）。
func TestEditorLogic(t *testing.T) {
	s, c := InsertText("ab\ncd", 1, 1, "X")
	if s != "ab\ncXd" || c != 2 {
		t.Fatalf("insert: got %q col=%d", s, c)
	}

	// 中文插入（多字节）：列按 rune 计
	s, c = InsertText("你好", 0, 1, "世")
	if s != "你世好" || c != 2 {
		t.Fatalf("cjk insert: got %q col=%d", s, c)
	}

	// 行首退格合并上一行
	s, ln, col := BackspaceAt("ab\ncd", 1, 0)
	if s != "abcd" || ln != 0 || col != 2 {
		t.Fatalf("backspace merge: got %q ln=%d col=%d", s, ln, col)
	}

	// 行中断行
	s, ln, col = NewlineAt("abcd", 0, 2)
	if s != "ab\ncd" || ln != 1 || col != 0 {
		t.Fatalf("newline: got %q ln=%d col=%d", s, ln, col)
	}
}

// 验证语法高亮 token 拼回去等于原文，且关键字/字符串/注释着色正确。
func TestHighlight(t *testing.T) {
	line := `func main() { return "hi" } // note`
	toks := Highlight(line)
	var sb strings.Builder
	for _, tk := range toks {
		sb.WriteString(tk.Text)
	}
	if sb.String() != line {
		t.Fatalf("tokens 拼回不等原文: %q", sb.String())
	}
	// 语法色已主题化：Highlight 返回语义 token 名，具体色值由当前主题解析。
	colors := map[string]string{}
	for _, tk := range toks {
		colors[tk.Text] = tk.Color
	}
	if colors["func"] != "keyword" {
		t.Errorf("func 关键字语义色错误: %q", colors["func"])
	}
	if colors[`"hi"`] != "str" {
		t.Errorf("字符串语义色错误: %q", colors[`"hi"`])
	}
	if colors["// note"] != "comment" {
		t.Errorf("注释语义色错误: %q", colors["// note"])
	}
}

// 验证 JSX 着色：标签名、属性名、属性值、{插值} 各自着色，且 token 严格拼回原文。
func TestHighlightJSX(t *testing.T) {
	line := `<div flex-direction="column"><For each={todos()}>{(t string) => <span>{t()}</span>}</For></div>`
	toks := Highlight(line)
	var sb strings.Builder
	for _, tk := range toks {
		sb.WriteString(tk.Text)
	}
	if sb.String() != line {
		t.Fatalf("JSX tokens 拼回不等原文:\n got %q\nwant %q", sb.String(), line)
	}

	// 按位置取颜色（同名 token 可能出现多次，逐个断言其颜色集合）。
	has := func(text, color string) bool {
		for _, tk := range toks {
			if tk.Text == text && tk.Color == color {
				return true
			}
		}
		return false
	}
	for _, c := range []struct{ text, color string }{
		{"div", "tag"},            // 标签名
		{"flex-direction", "attr"}, // kebab-case 属性名
		{`"column"`, "str"},       // 属性值字符串
		{"each", "attr"},           // 插值外的属性名
		{"todos", "text"},          // 插值内的 Go 标识符
		{"string", "keyword"},      // 插值内的 Go 类型关键字
		{"For", "tag"},             // 闭合标签名
	} {
		if !has(c.text, c.color) {
			t.Errorf("缺少 token %q 颜色 %q；实际: %+v", c.text, c.color, toks)
		}
	}
}

// 验证 GX JSX 编译产物能构建出完整场景树（自举核心：GX 写的 UI 是真实场景树）。
func TestPlaygroundSceneTree(t *testing.T) {
	inst := Playground()
	if inst == nil {
		t.Fatal("Playground 返回 nil")
	}
	root := inst.Root()
	if root == nil {
		t.Fatal("Root 为 nil")
	}
	txt := (gxscene.TextBackend{}).Paint(root)
	// 标题、示例名、编辑器里的行号与高亮 token 都应出现在文本投影里。
	// 注意：编辑器源码被切成逐 token 的 span，"sig count" 不会连成连续串，
	// 故只断言标题、示例名与确实连续的输入片段。
	for _, want := range []string{"GX Playground", "用 GX 写 GX", "01 Hello GX", "示例", "Hello"} {
		if !strings.Contains(txt, want) {
			t.Errorf("场景树缺少 %q\n---\n%s\n---", want, txt)
		}
	}
}

// 验证示例切换写信号并驱动编辑器源码更新。
func TestLoadSample(t *testing.T) {
	EditorSetText("placeholder")
	LoadSample(1)
	if !strings.Contains(EditorText(), "sig count") {
		t.Fatalf("切到示例 1 后源码未更新: %q", EditorText())
	}
	NextSample()
	if !strings.Contains(EditorText(), "For each") {
		t.Fatalf("下一个示例源码未更新: %q", EditorText())
	}
	PrevSample()
	if !strings.Contains(EditorText(), "sig count") {
		t.Fatalf("上一个示例源码未更新: %q", EditorText())
	}
}

// 复现：点击示例切换后行数应随之增删（Counter 是 8 行，Hello 是 5 行）。
// 需要像真实 app 那样 BindReactive，否则 getter 不重算。
func TestEditorLineCountOnSampleSwitch(t *testing.T) {
	// 用 signal.Effect 作为绑定器，与 gxapp 实际接线一致。
	LoadSample(0) // Hello GX: 5 行
	inst := Playground()
	inst.BindStructural(signal.Effect)
	inst.BindLeafEffects(signal.Effect)
	_ = (gxscene.TextBackend{}).Paint(inst.Root()) // 首帧 5 行已建

	LoadSample(1) // Counter: 8 行
	txt8 := (gxscene.TextBackend{}).Paint(inst.Root())

	// 代码是逐 token 的 span，连续串不会出现；改数行号 gutter 节点（完整文本）。
	// 5 行示例行号为 1..5，8 行示例行号为 1..8，故 "6"/"7"/"8" 这些行号应新出现。
	for _, gutter := range []string{`"6"`, `"7"`, `"8"`} {
		if !strings.Contains(txt8, gutter) {
			t.Errorf("切到 8 行 Counter 后应出现行号 gutter %s\n---8行---\n%s", gutter, txt8)
		}
	}
}

// 回归：行级 token 缓存优化的核心不变量——插入字符只 bump 变化行的版本，
// 其它行版本保持不变（否则 384 个 token getter 会全量重跑，按键延迟回升到 ~88ms）。
func TestSetSourceOnlyBumpsChangedLines(t *testing.T) {
	EditorSetText("line0\nsig count = 0\nline2\nline3")
	before := []int{rowVersion(0), rowVersion(1), rowVersion(2), rowVersion(3)}

	// 只在第 1 行末尾插入字符，其余行文本不变。
	setSource("line0\nsig count = 01\nline2\nline3")
	after := []int{rowVersion(0), rowVersion(1), rowVersion(2), rowVersion(3)}

	if after[1] <= before[1] {
		t.Errorf("变化行(1)的版本应递增: before=%d after=%d", before[1], after[1])
	}
	for _, i := range []int{0, 2, 3} {
		if after[i] != before[i] {
			t.Errorf("未变化行(%d)的版本不应改变: before=%d after=%d", i, before[i], after[i])
		}
	}
	// tokenCache 应反映新文本
	if got := EditorLineText(1, 2); got == "" {
		t.Errorf("变化行的 token 缓存应有内容, got %q", got)
	}
}
