// Package complete 提供 gox-ide 的代码补全引擎（独立于 gox-term 子模块）。
//
// 设计要点：
//   - 纯标准库，不把 skia/cgo 拖进终端程序；
//   - 可插拔 Provider：关键字/片段、当前 buffer 符号、项目符号、可选 LSP；
//   - 引擎统一打分、去重、排序、限流；LSP 异步预热，Provide 永不阻塞 UI。
package complete

import (
	"sort"
	"strings"
)

// Kind 是候选类别。
type Kind uint8

const (
	KindKeyword Kind = iota
	KindSymbol
	KindBuffer
	KindLSP
)

func (k Kind) Mark() string {
	switch k {
	case KindKeyword:
		return "◆"
	case KindSymbol:
		return "ƒ"
	case KindBuffer:
		return "•"
	case KindLSP:
		return "λ"
	}
	return "·"
}

// Candidate 是一条补全候选。Text 为采纳时插入的 token（不含已输入部分）。
type Candidate struct {
	Text   string
	Label  string
	Detail string
	Kind   Kind
	Score  int
}

// Query 是一次补全请求。
type Query struct {
	Token      string // 光标前当前标识符
	Line       string // 当前行（光标前）
	BufferText string // 当前文档全文（供 buffer 符号）
	CWD        string // 项目目录
}

// Provider 是一个补全来源。
type Provider interface {
	Name() string
	Provide(q Query) []Candidate
}

// Engine 聚合多个 Provider。
type Engine struct {
	providers []Provider
	limit     int
}

// NewEngine 用默认来源构建引擎。cwd 为项目根目录。
func NewEngine(cwd string) *Engine {
	return &Engine{
		providers: []Provider{
			NewKeywordProvider(),
			NewBufferProvider(),
			NewProjectProvider(cwd),
			NewLSPProvider(cwd),
		},
		limit: 9,
	}
}

// NewEngineWith 用给定来源构建（测试用）。
func NewEngineWith(limit int, providers ...Provider) *Engine {
	return &Engine{providers: providers, limit: limit}
}

// Complete 返回排序后的候选。
func (e *Engine) Complete(q Query) []Candidate {
	all := make([]Candidate, 0, 64)
	for _, p := range e.providers {
		all = append(all, p.Provide(q)...)
	}
	type ranked struct {
		c     Candidate
		score int
	}
	seen := map[string]bool{}
	out := make([]ranked, 0, len(all))
	for _, c := range all {
		score := Score(q.Token, c)
		if score <= 0 {
			continue
		}
		key := c.Kind.String() + "\x00" + c.Label
		if seen[key] {
			continue
		}
		seen[key] = true
		c.Score = score
		out = append(out, ranked{c, score})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].c.Label < out[j].c.Label
	})
	limit := e.limit
	if limit <= 0 {
		limit = 9
	}
	if len(out) > limit {
		out = out[:limit]
	}
	res := make([]Candidate, len(out))
	for i, r := range out {
		res[i] = r.c
	}
	return res
}

func (k Kind) String() string {
	switch k {
	case KindKeyword:
		return "keyword"
	case KindSymbol:
		return "symbol"
	case KindBuffer:
		return "buffer"
	case KindLSP:
		return "lsp"
	}
	return "?"
}

// Score 计算相关度，0 表示不匹配。
func Score(token string, c Candidate) int {
	if token == "" {
		return 1 + c.Score
	}
	tl, ll := strings.ToLower(token), strings.ToLower(c.Label)
	var s int
	switch {
	case ll == tl:
		s = 1000
	case strings.HasPrefix(ll, tl):
		s = 800 - minInt(len(ll), 200)
	case strings.Contains(ll, tl):
		s = 400 - strings.Index(ll, tl)*10 - minInt(len(ll)/4, 60)
	case subseqMatch(ll, tl):
		s = 150
	default:
		return 0
	}
	if c.Score > 0 {
		s += c.Score
	}
	return s
}

func subseqMatch(label, token string) bool {
	if token == "" {
		return false
	}
	j := 0
	for i := 0; i < len(label) && j < len(token); i++ {
		if label[i] == token[j] {
			j++
		}
	}
	return j == len(token)
}

// TokenBefore 从光标左侧文本取出当前标识符（支持 . 成员访问时取点号后的部分）。
func TokenBefore(left string) string {
	s := strings.TrimRight(left, " \t")
	if i := strings.LastIndexAny(s, " \t;|&=<>()'\"[]{}+-*/%,!:"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// MemberPrefix 返回点号左侧的表达式（用于"装了 gopls，输入 obj. 弹成员"）。
func MemberPrefix(left string) string {
	s := strings.TrimRight(left, " \t")
	i := strings.LastIndex(s, ".")
	if i < 0 {
		return ""
	}
	start := i - 1
	for start >= 0 && (isIdent(s[start]) || s[start] == '.') {
		start--
	}
	return s[start+1 : i]
}

func isIdent(r byte) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r >= 0x80
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
