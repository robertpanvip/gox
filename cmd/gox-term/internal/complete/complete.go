// Package complete 提供 gox-term 的旁路补全引擎。
//
// 设计要点：
//   - 补全不接管 shell 的行编辑。按键照常进 PTY，shell 负责回显与历史；
//     引擎只根据「光标左侧文本」给出候选，采纳时由上层回发退格+候选文本。
//   - 每个来源是一个 Provider，可插拔。引擎统一做打分、去重、排序、限流。
//   - 纯标准库，不拖入 gox 主仓库的 cgo/skia 依赖。
package complete

import (
	"sort"
	"strings"
	"sync"
	"unicode"
)

// Kind 是候选来源类别，用于图标与配色。
type Kind uint8

const (
	KindCommand Kind = iota // PATH 里的可执行命令
	KindSymbol              // 代码符号（LSP 或项目扫描）
	KindKeyword             // 关键字与片段
	KindFile                // 文件与目录
)

func (k Kind) String() string {
	switch k {
	case KindCommand:
		return "command"
	case KindSymbol:
		return "symbol"
	case KindKeyword:
		return "keyword"
	case KindFile:
		return "file"
	}
	return "?"
}

// Mark 返回候选项左侧的类别标记（单字符，终端友好）。
func (k Kind) Mark() string {
	switch k {
	case KindCommand:
		return "$"
	case KindSymbol:
		return "ƒ"
	case KindKeyword:
		return "◆"
	case KindFile:
		return "▸"
	}
	return "·"
}

// Candidate 是一条补全候选。
type Candidate struct {
	Text   string // 采纳时插入的文本（命令通常带尾随空格）
	Label  string // 显示的主文本
	Detail string // 右侧说明（来源/签名）
	Kind   Kind
	Score  int // 由引擎计算，Provider 可给基础分
}

// Query 是一次补全请求。
type Query struct {
	Token string // 光标前当前单词（补全主键）
	Prev  string // Token 前面的那个单词（如 "git st" 中的 "git"，用于子命令补全）
	Line  string // 当前命令行整行（供上下文判断）
	CWD   string // 当前工作目录
}

// Provider 是一个补全来源。
type Provider interface {
	Name() string
	Provide(q Query) []Candidate
}

// Engine 聚合多个 Provider，统一打分、去重、排序、限流。
type Engine struct {
	mu        sync.Mutex
	providers []Provider
	limit     int
}

// NewEngine 用默认的四类来源构建引擎。cwd 用于项目符号与文件扫描。
func NewEngine(cwd string) *Engine {
	return &Engine{
		providers: []Provider{
			NewPathProvider(),
			NewKeywordProvider(),
			NewProjectProvider(cwd),
			NewLSPSymbolProvider(cwd),
		},
		limit: 9,
	}
}

// NewEngineWith 用给定来源构建引擎（便于测试与定制）。
func NewEngineWith(limit int, providers ...Provider) *Engine {
	return &Engine{providers: providers, limit: limit}
}

// SetLimit 调整返回的候选上限。
func (e *Engine) SetLimit(n int) {
	if n > 0 {
		e.limit = n
	}
}

// Complete 返回按相关度排序的候选列表。
func (e *Engine) Complete(q Query) []Candidate {
	all := make([]Candidate, 0, 64)
	for _, p := range e.providers {
		all = append(all, p.Provide(q)...)
	}
	// 上下文子命令（git st → status）命中时，屏蔽同前缀的全局命令噪声。
	if _, ok := subcommands[strings.ToLower(q.Prev)]; ok {
		hasSubHit := false
		for _, c := range all {
			if c.Kind == KindKeyword && c.Score >= 300 {
				hasSubHit = true
				break
			}
		}
		if hasSubHit {
			filtered := all[:0]
			for _, c := range all {
				if c.Kind != KindCommand {
					filtered = append(filtered, c)
				}
			}
			all = filtered
		}
	}

	type ranked struct {
		c     Candidate
		score int
	}
	seen := make(map[string]bool, len(all))
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
		out = append(out, ranked{c: c, score: score})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		if out[i].c.Kind != out[j].c.Kind {
			return out[i].c.Kind < out[j].c.Kind
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

// Score 计算候选相对查询的相关度，0 表示不匹配。
func Score(token string, c Candidate) int {
	label := c.Label
	if token == "" {
		return 1 + c.Score
	}
	tl, ll := strings.ToLower(token), strings.ToLower(label)
	var s int
	switch {
	case ll == tl:
		s = 1000
	case strings.HasPrefix(ll, tl):
		s = 800 - minInt(len(ll), 200) // 命中越精确、候选越短越靠前
	case strings.Contains(ll, tl):
		s = 400 - strings.Index(ll, tl)*10 - minInt(len(ll)/4, 60)
	case subseqMatch(ll, tl):
		s = 150
	default:
		return 0
	}
	// 类别权重：命令行场景优先命令，其次符号。
	switch c.Kind {
	case KindCommand:
		s += 30
	case KindKeyword:
		s += 12
	case KindSymbol:
		s += 8
	}
	s += popularity(ll)
	if c.Score > 0 {
		s += c.Score
	}
	return s
}

// subseqMatch 判断 token 是否为 label 的子序列（首字母缩写式匹配，如 "gst" → "git status"）。
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

// popularity 给高频命令一点加成，让最常见的结果排在前面。
func popularity(label string) int {
	switch label {
	case "cd", "ls", "pwd", "git", "go", "vim", "cat", "grep", "find", "make", "clear", "echo":
		return 60
	case "git status", "git add", "git commit", "git push", "go build", "go test", "go run":
		return 40
	}
	return 0
}

func isSep(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune(";|&=<>()'\"[]{}", r)
}

// TokenBefore 从光标左侧文本里取出当前单词。shell 常见分隔符都会切断单词。
func TokenBefore(left string) string {
	_, token := BeforeTokens(left)
	return token
}

// BeforeTokens 返回 (前一个单词, 当前单词)。例如 "git st" → ("git", "st")，
// 用于 git/go/docker 等子命令的上下文补全。
func BeforeTokens(left string) (string, string) {
	s := strings.TrimRight(left, " \t")
	i := strings.LastIndexFunc(s, isSep)
	token := s
	if i >= 0 {
		token = s[i+1:]
	}
	rest := ""
	if i > 0 {
		rest = strings.TrimRight(s[:i], " \t")
	}
	j := strings.LastIndexFunc(rest, isSep)
	prev := rest
	if j >= 0 {
		prev = rest[j+1:]
	}
	return prev, token
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
