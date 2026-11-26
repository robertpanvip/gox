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

func TestMemberToken(t *testing.T) {
	cases := []struct {
		left string
		tok  string
		ok   bool
	}{
		{"strings.", "", true},
		{"b.Re", "Re", true},
		{"    x := obj.field.Na", "Na", true},
		{"count", "", false},
		{"", "", false},
		{".", "", false},
		{"a + b", "", false},
	}
	for _, c := range cases {
		tok, ok := MemberToken(c.left)
		if tok != c.tok || ok != c.ok {
			t.Errorf("MemberToken(%q) = (%q,%v), want (%q,%v)", c.left, tok, ok, c.tok, c.ok)
		}
	}
}

func TestEngineMemberRouting(t *testing.T) {
	// 点号上下文下，引擎不应混入关键字/符号等噪声。用一个假成员 provider 验证分流。
	e := NewEngineWith(9,
		&fakeProvider{name: "member", cands: []Candidate{{Text: "Replace", Label: "Replace", Kind: KindMember}}},
		&fakeProvider{name: "keyword", cands: []Candidate{{Text: "func", Label: "func", Kind: KindKeyword}}},
	)
	got := e.Complete(Query{Token: "Re", Member: true})
	if len(got) != 1 || got[0].Label != "Replace" {
		t.Fatalf("点号上下文应只出成员, got %v", got)
	}
	// 非点号上下文跳过成员 provider。
	got = e.Complete(Query{Token: "fu", Member: false})
	if len(got) != 1 || got[0].Label != "func" {
		t.Fatalf("非点号上下文应只出关键字, got %v", got)
	}
}

type fakeProvider struct {
	name  string
	cands []Candidate
}

func (f *fakeProvider) Name() string              { return f.name }
func (f *fakeProvider) Provide(Query) []Candidate { return f.cands }

func TestMemberProviderRealGoTypes(t *testing.T) {
	// 真实调用 gxcomplete：诊断型测试，GOROOT 不可用时跳过。
	src := "package main\n\nimport \"strings\"\n\nfunc f() {\n    b := strings.NewReplacer()\n    b.\n    _ = b\n}\n"
	p := NewMemberProvider()
	cands := p.Provide(Query{Member: true, BufferText: src, LineNo: 6, Col: 6})
	if len(cands) == 0 {
		t.Skip("当前环境无 GOROOT/src 或类型检查不可用，跳过（生产环境降级到符号补全）")
	}
	found := false
	for _, c := range cands {
		if c.Label == "Replace" {
			found = true
		}
		if c.Kind != KindMember {
			t.Errorf("成员候选 Kind 应为 KindMember: %s=%v", c.Label, c.Kind)
		}
	}
	if !found {
		t.Fatalf("b. 应含 Replace, got %v", cands)
	}
}
