package complete

import "strings"

type kw struct{ text, label, detail string }

var keywords = []kw{
	// GX 语言
	{"sig ", "sig", "信号声明"}, {"func ", "func", "函数"},
	{"view ", "view", "视图"}, {"canvas ", "canvas", "画布"},
	{"return ", "return", "返回"}, {"for ", "for", "循环"},
	{"range ", "range", "范围"}, {"if ", "if", "条件"},
	{"else ", "else", "否则"}, {"import ", "import", "导入"},
	{"type ", "type", "类型"}, {"struct ", "struct", "结构体"},
	{"interface ", "interface", "接口"}, {"on ", "on", "事件绑定"},
	{"computed ", "computed", "计算属性"}, {"style ", "style", "样式"},
	// Go
	{"package ", "package", "包声明"}, {"var ", "var", "变量"},
	{"const ", "const", "常量"}, {"go ", "go", "协程"},
	{"defer ", "defer", "延迟"}, {"select ", "select", "选择"},
	{"chan ", "chan", "通道"}, {"map", "map", "映射"},
	// 片段
	{"for i in range(10) {\n    \n}", "for-range", "循环片段"},
	{"func main() {\n    \n}", "func-main", "主函数片段"},
	{"if true {\n    \n}", "if-block", "条件片段"},
}

type KeywordProvider struct{ items []Candidate }

func NewKeywordProvider() *KeywordProvider {
	p := &KeywordProvider{items: make([]Candidate, 0, len(keywords))}
	for _, k := range keywords {
		p.items = append(p.items, Candidate{Text: k.text, Label: k.label, Detail: k.detail, Kind: KindKeyword})
	}
	return p
}

func (p *KeywordProvider) Name() string { return "keyword" }

func (p *KeywordProvider) Provide(q Query) []Candidate {
	if q.Token == "" {
		return nil
	}
	tl := strings.ToLower(q.Token)
	out := make([]Candidate, 0, 16)
	for _, c := range p.items {
		if strings.HasPrefix(strings.ToLower(c.Label), tl) || strings.Contains(strings.ToLower(c.Label), tl) {
			out = append(out, c)
		}
	}
	return out
}
