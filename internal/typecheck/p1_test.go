package typecheck

import (
	"strings"
	"testing"
)

// 三件套核心层红测试：Tokens/EncodeTokens/Legend、DocOf、ParamHints。

func TestTokensCategories(t *testing.T) {
	src := "package main\n\nimport \"fmt\"\n\n// hi\nfunc add(a int) int { return a + 12 }\nvar s = \"x\"\n"
	toks, ok := Tokens(src)
	if !ok || len(toks) < 12 {
		t.Fatalf("token 数量异常: n=%d ok=%v", len(toks), ok)
	}
	by := map[string]int{}
	for _, tk := range toks {
		by[tk.Type]++
		if tk.Line < 1 || tk.Col < 1 || tk.Len <= 0 {
			t.Fatalf("token 坐标异常: %+v", tk)
		}
	}
	for _, want := range []string{"keyword", "string", "comment", "function", "number", "variable", "operator", "namespace"} {
		if by[want] == 0 {
			t.Fatalf("缺少类别 %s: %v", want, by)
		}
	}
	enc := EncodeTokens(toks)
	if len(enc) == 0 || len(enc)%5 != 0 {
		t.Fatalf("编码长度 %d 非 5 倍数", len(enc))
	}
	lg := Legend()
	found := false
	for _, n := range lg {
		if n == "keyword" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Legend 缺 keyword: %v", lg)
	}
}

func TestDocOfFake(t *testing.T) {
	doc, file, line, ok := DocOf("fmt", "Sprintf", fmtLookup())
	if !ok {
		t.Fatal("DocOf 应命中")
	}
	if file != "print.go" || line != 4 || !strings.Contains(doc, "formats according") {
		t.Fatalf("doc=%q file=%q line=%d", doc, file, line)
	}
	if _, _, _, ok := DocOf("fmt", "Nope", fmtLookup()); ok {
		t.Fatal("不存在符号应 false")
	}
}

func TestParamHintsRules(t *testing.T) {
	lookup := func(path string) ([]File, bool) {
		switch path {
		case "p":
			return []File{{Name: "p.go", Src: "package p\n\nfunc Add(a int, b string) int { return a }\n"}}, true
		case "q":
			return []File{{Name: "q.go", Src: "package q\n\nfunc F(a int, rest ...string) {}\n"}}, true
		}
		return nil, false
	}
	src := "package main\n\nimport (\n\t\"p\"\n\t\"q\"\n)\n\nfunc main() {\n\tvar a int\n\t_ = p.Add(a, \"s\")\n\tq.F(1, \"r\")\n}\n"
	hints, ok := NewImporter(lookup).ParamHints(src)
	if !ok {
		t.Fatal("ParamHints 应成功")
	}
	got := map[string]int{}
	for _, h := range hints {
		got[h.Label]++
		if h.Line <= 0 || h.Col <= 0 {
			t.Fatalf("hint 坐标异常: %+v", h)
		}
	}
	// 规则：同名标识符实参跳过（a）；字面量补提示（"s"→b:、1→a:）；可变参数不提示。
	if got["b:"] != 1 || got["a:"] != 1 || len(hints) != 2 {
		t.Fatalf("hints=%+v", hints)
	}
}
