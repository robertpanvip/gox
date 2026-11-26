package complete

import "testing"

type fake struct {
	name  string
	items []Candidate
}

func (f fake) Name() string              { return f.name }
func (f fake) Provide(Query) []Candidate { return f.items }

func TestEngineRankDedupe(t *testing.T) {
	e := NewEngineWith(5,
		fake{"a", []Candidate{{Label: "foo"}, {Label: "foo"}, {Label: "foobar"}}},
		fake{"b", []Candidate{{Label: "fool"}, {Label: "zzz"}}},
	)
	got := e.Complete(Query{Token: "foo"})
	if len(got) == 0 {
		t.Fatal("无候选")
	}
	if got[0].Label != "foo" {
		t.Errorf("首位应为精确匹配 foo，实际 %q", got[0].Label)
	}
	n := 0
	for _, c := range got {
		if c.Label == "foo" {
			n++
		}
		if c.Label == "zzz" {
			t.Error("不匹配项应过滤")
		}
	}
	if n != 1 {
		t.Errorf("foo 应去重为 1，实际 %d", n)
	}
}

func TestTokenBefore(t *testing.T) {
	cases := map[string]string{
		"    cou":     "cou",
		"x = cou":     "cou",
		"obj.field":   "field",
		"foo.bar.baz": "baz",
		"count + 1":   "1",
		"":            "",
	}
	for in, want := range cases {
		if got := TokenBefore(in); got != want {
			t.Errorf("TokenBefore(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMemberPrefix(t *testing.T) {
	if got := MemberPrefix("obj.field"); got != "obj" {
		t.Errorf("MemberPrefix = %q", got)
	}
	if got := MemberPrefix("foo.bar.baz"); got != "foo.bar" {
		t.Errorf("MemberPrefix = %q", got)
	}
	if got := MemberPrefix("field"); got != "" {
		t.Errorf("无点号应为空，得到 %q", got)
	}
}

func TestExtractSymbols(t *testing.T) {
	src := "package p\ntype Handler struct{}\nfunc Serve() {}\n// comment\nsig count = 0\nvar name = \"x\"\n"
	syms := extractSymbols(src)
	want := []string{"Handler", "Serve", "count", "name"}
	for _, w := range want {
		ok := false
		for _, s := range syms {
			if s == w {
				ok = true
			}
		}
		if !ok {
			t.Errorf("应抽取 %q，得到 %v", w, syms)
		}
	}
}

func TestKeywordProvider(t *testing.T) {
	p := NewKeywordProvider()
	if got := p.Provide(Query{Token: ""}); len(got) != 0 {
		t.Error("空 token 不应返回关键字")
	}
	got := p.Provide(Query{Token: "fu"})
	found := false
	for _, c := range got {
		if c.Label == "func" {
			found = true
		}
	}
	if !found {
		t.Error("应包含 func")
	}
}
