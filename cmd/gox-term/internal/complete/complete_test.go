package complete

import (
	"strings"
	"testing"
)

type fakeProvider struct {
	name  string
	items []Candidate
}

func (f fakeProvider) Name() string              { return f.name }
func (f fakeProvider) Provide(Query) []Candidate { return f.items }

func TestEngineScoreDedupeSort(t *testing.T) {
	providers := []Provider{
		fakeProvider{"a", []Candidate{
			{Label: "git", Kind: KindCommand},
			{Label: "git", Kind: KindCommand}, // 重复
			{Label: "gopher", Kind: KindCommand},
		}},
		fakeProvider{"b", []Candidate{
			{Label: "goto", Kind: KindKeyword},
			{Label: "zzz", Kind: KindSymbol}, // 不匹配
		}},
	}
	e := NewEngineWith(5, providers...)
	got := e.Complete(Query{Token: "git"})
	if len(got) == 0 {
		t.Fatal("期望有候选")
	}
	// 去重：不能出现两条 git
	n := 0
	for _, c := range got {
		if c.Label == "git" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("git 出现 %d 次，应为 1", n)
	}
	if got[0].Label != "git" {
		t.Errorf("首位应为精确匹配的 git，实际 %q", got[0].Label)
	}
	for _, c := range got {
		if c.Label == "zzz" {
			t.Error("不匹配的 zzz 不应出现")
		}
	}
}

func TestScore(t *testing.T) {
	c := Candidate{Label: "git status", Kind: KindCommand}
	if Score("", c) <= 0 {
		t.Error("空 token 也应给正分")
	}
	if Score("git", c) <= Score("gXX", c) {
		t.Error("前缀匹配应高于弱子序列匹配")
	}
	if Score("zzz", c) != 0 {
		t.Error("完全不匹配应为 0")
	}
}

func TestTokenBefore(t *testing.T) {
	cases := map[string]string{
		"git st":      "st",
		"git":         "git",
		"":            "",
		"ls -la foo":  "foo",
		"echo \"hi":   "hi",
		"go build ./": "./",
		"x; git":      "git",
	}
	for in, want := range cases {
		if got := TokenBefore(in); got != want {
			t.Errorf("TokenBefore(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestSubseqMatch(t *testing.T) {
	if !subseqMatch("gitstatus", "gst") {
		t.Error("gst 应为 gitstatus 的子序列")
	}
	if subseqMatch("git", "tg") {
		t.Error("tg 不是 git 的子序列")
	}
}

func TestKeywordProvider(t *testing.T) {
	p := NewKeywordProvider()
	if got := p.Provide(Query{Token: ""}); len(got) != 0 {
		t.Errorf("空查询不应返回关键字，得到 %d 条", len(got))
	}
	got := p.Provide(Query{Token: "git st"})
	if len(got) == 0 {
		t.Fatal("期望匹配 git status")
	}
	found := false
	for _, c := range got {
		if c.Label == "git status" {
			found = true
		}
	}
	if !found {
		t.Error("应包含 git status")
	}
}

func TestProjectProviderExtractSymbols(t *testing.T) {
	src := "package foo\n\ntype Handler struct{}\nfunc ServeHTTP() {}\n// comment\nsig count = 0\n"
	syms := extractSymbols(src)
	joined := []string{}
	for _, s := range syms {
		joined = append(joined, s.Label)
	}
	_ = strings.Join(joined, ",")
	for _, want := range []string{"Handler", "ServeHTTP", "count"} {
		ok := false
		for _, s := range syms {
			if s.Label == want {
				ok = true
			}
		}
		if !ok {
			t.Errorf("应抽取符号 %q，得到 %v", want, joined)
		}
	}
}

func TestContextSubCommands(t *testing.T) {
	e := NewEngine(".")
	got := e.Complete(Query{Token: "st", Prev: "git"})
	if len(got) == 0 {
		t.Fatal("git st 应有候选")
	}
	// 首位应是 git 子命令，而不是系统命令 stat/stty。
	if got[0].Label != "status" {
		t.Errorf("首位应为 status，实际 %q", got[0].Label)
	}
	for _, c := range got {
		if c.Kind == KindCommand {
			t.Errorf("git 子命令命中时不应混入系统命令: %q", c.Label)
		}
	}
	// 采纳文本应只含子命令，退格才不会错位。
	if got[0].Text != "status " {
		t.Errorf("采纳文本应为 'status '，实际 %q", got[0].Text)
	}
}

func TestBeforeTokens(t *testing.T) {
	prev, tok := BeforeTokens("git st")
	if prev != "git" || tok != "st" {
		t.Errorf("git st → (%q,%q)", prev, tok)
	}
	prev, tok = BeforeTokens("go test ./...")
	if prev != "test" || tok != "./..." {
		t.Errorf("go test ./... → (%q,%q)", prev, tok)
	}
	prev, tok = BeforeTokens("git")
	if prev != "" || tok != "git" {
		t.Errorf("git → (%q,%q)", prev, tok)
	}
}
