package compiler

import (
	"strings"
	"testing"
)

// sigToks 返回非空白/非注释/非 EOF 的 token，便于断言词法主干。
func sigToks(src string) []Token {
	all := New(src).AllTokens()
	out := make([]Token, 0, len(all))
	for _, t := range all {
		if t.Kind == Space || t.Kind == Newline || t.Kind == Comment || t.Kind == EOF {
			continue
		}
		out = append(out, t)
	}
	return out
}

func tokSignature(toks []Token) string {
	var b strings.Builder
	for _, t := range toks {
		b.WriteString(t.Kind.String())
		b.WriteString(":")
		b.WriteString(t.Text)
		b.WriteString("|")
	}
	return b.String()
}

func TestLexMultiCharOperators(t *testing.T) {
	src := "a <<= b &^= c ... d := e == f != g <= h >= i && j || k <- l ++ m -- n += o"
	got := sigToks(src)
	var ops []string
	for _, tok := range got {
		if tok.Kind == Operator {
			ops = append(ops, tok.Text)
		}
	}
	want := []string{"<<=", "&^=", "...", ":=", "==", "!=", "<=", ">=", "&&", "||", "<-", "++", "--", "+="}
	if len(ops) != len(want) {
		t.Fatalf("运算符数量 = %d, want %d; got %v", len(ops), len(want), ops)
	}
	for i := range want {
		if ops[i] != want[i] {
			t.Errorf("ops[%d] = %q, want %q", i, ops[i], want[i])
		}
	}
}

func TestLexNumbers(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"123", "123"},
		{"0", "0"},
		{"1.5", "1.5"},
		{".5", ".5"},
		{"1e10", "1e10"},
		{"1E-3", "1E-3"},
		{"0x1p2", "0x1p2"},
		{"0xFF", "0xFF"},
		{"1_000", "1_000"},
	}
	for _, c := range cases {
		toks := sigToks(c.in)
		if len(toks) != 1 || toks[0].Kind != Number || toks[0].Text != c.want {
			t.Errorf("lex %q = %s, want number %q", c.in, tokSignature(toks), c.want)
		}
	}
}

func TestLexStringsAndComments(t *testing.T) {
	src := "\"hello\\nworld\" `raw\nstring` 'x' // line\n/* block */"
	toks := sigToks(src)
	kinds := make([]Kind, 0, len(toks))
	texts := make([]string, 0, len(toks))
	for _, tok := range toks {
		kinds = append(kinds, tok.Kind)
		texts = append(texts, tok.Text)
	}
	// 主干应为: String, String(raw), Rune —— 注释被 sigToks 过滤
	wantKinds := []Kind{String, String, Rune}
	if len(kinds) != len(wantKinds) {
		t.Fatalf("kinds = %v texts = %q, want %v", kinds, texts, wantKinds)
	}
	for i := range wantKinds {
		if kinds[i] != wantKinds[i] {
			t.Errorf("kind[%d] = %s, want %s", i, kinds[i], wantKinds[i])
		}
	}
	// 原始字符串应保留换行
	if !strings.Contains(texts[1], "\n") {
		t.Errorf("raw string 应保留换行, got %q", texts[1])
	}
}

func TestLexCRLFAsOneNewline(t *testing.T) {
	src := "a\r\nb\rc\nd"
	all := New(src).AllTokens()
	nls := 0
	for _, tok := range all {
		if tok.Kind == Newline {
			nls++
			if tok.Text != "\n" {
				t.Errorf("Newline 文本应归一为 \\n, got %q", tok.Text)
			}
		}
	}
	if nls != 3 {
		t.Errorf("换行数 = %d, want 3 (CRLF/CR/LF 各一个)", nls)
	}
}

func TestLexUnicodeIdent(t *testing.T) {
	src := "变量 := 42"
	toks := sigToks(src)
	if len(toks) == 0 || toks[0].Kind != Ident || toks[0].Text != "变量" {
		t.Fatalf("Unicode 标识符识别失败: %s", tokSignature(toks))
	}
}

func TestLexPositionLineCol(t *testing.T) {
	src := "package p\npub func f() {}\n"
	all := New(src).AllTokens()
	var f Token
	for _, tok := range all {
		if tok.is("func") {
			f = tok
			break
		}
	}
	if f.Pos.Line != 2 {
		t.Errorf("func 行号 = %d, want 2", f.Pos.Line)
	}
	// "pub func" 中 func 从第 5 列起（p=1,u=2,b=3,空格=4,f=5）
	if f.Pos.Col != 5 {
		t.Errorf("func 列号 = %d, want 5", f.Pos.Col)
	}
}

func TestLexJSXVsLessThan(t *testing.T) {
	// 比较运算符：'<' 前的 token 可作为表达式结尾 → 不应识别为 JSX
	cmpCases := []string{
		"if a < b { }",
		"x := a < 5",
		"return a < b",
		"foo(a < b)",
	}
	for _, src := range cmpCases {
		toks := sigToks(src)
		for _, tok := range toks {
			if tok.Kind == JSX {
				t.Errorf("%q 不应产生 JSX token: %s", src, tokSignature(toks))
			}
		}
	}
	// JSX：'<' 紧跟字母且处于表达式起点 → 应识别为 JSX
	jsxCases := []string{
		"x := <div>hi</div>",
		"return <span/>",
		"foo(<br/>)",
	}
	for _, src := range jsxCases {
		toks := sigToks(src)
		found := false
		for _, tok := range toks {
			if tok.Kind == JSX {
				found = true
			}
		}
		if !found {
			t.Errorf("%q 应产生 JSX token: %s", src, tokSignature(toks))
		}
	}
}

func TestLexEOFAlwaysPresent(t *testing.T) {
	all := New("").AllTokens()
	if len(all) != 1 || all[0].Kind != EOF {
		t.Fatalf("空源码应只有 EOF, got %v", all)
	}
	all = New("a").AllTokens()
	if all[len(all)-1].Kind != EOF {
		t.Error("token 流末尾应有 EOF")
	}
}
