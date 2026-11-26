package complete

import "strings"

// BufferProvider 从当前文档抽取符号（函数/类型/变量/sig），开箱即用。
type BufferProvider struct{}

func NewBufferProvider() *BufferProvider { return &BufferProvider{} }
func (p *BufferProvider) Name() string   { return "buffer" }

func (p *BufferProvider) Provide(q Query) []Candidate {
	if q.Token == "" || q.BufferText == "" {
		return nil
	}
	tl := strings.ToLower(q.Token)
	out := make([]Candidate, 0, 24)
	for _, name := range extractSymbols(q.BufferText) {
		if strings.HasPrefix(strings.ToLower(name), tl) || strings.Contains(strings.ToLower(name), tl) {
			out = append(out, Candidate{Text: name, Label: name, Detail: "本文件", Kind: KindBuffer})
		}
	}
	return out
}
