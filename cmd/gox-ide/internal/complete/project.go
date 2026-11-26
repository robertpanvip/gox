package complete

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var srcExt = map[string]bool{
	".go": true, ".gx": true, ".js": true, ".ts": true, ".tsx": true,
	".jsx": true, ".py": true, ".rs": true, ".java": true, ".rb": true,
}

// ProjectProvider 扫描项目根目录下的源码文件抽取符号，带缓存。
type ProjectProvider struct {
	root  string
	mu    sync.Mutex
	cache []string
	stamp int64
}

func NewProjectProvider(root string) *ProjectProvider {
	if root == "" {
		root, _ = os.Getwd()
	}
	return &ProjectProvider{root: root}
}

func (p *ProjectProvider) Name() string { return "project" }

func (p *ProjectProvider) Provide(q Query) []Candidate {
	if q.Token == "" {
		return nil
	}
	syms := p.snapshot()
	tl := strings.ToLower(q.Token)
	out := make([]Candidate, 0, 32)
	for _, name := range syms {
		if strings.HasPrefix(strings.ToLower(name), tl) || strings.Contains(strings.ToLower(name), tl) {
			out = append(out, Candidate{Text: name, Label: name, Detail: "项目符号", Kind: KindSymbol})
		}
	}
	return out
}

func (p *ProjectProvider) snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	// 项目文件在编辑会话里变化不频繁，缓存 15 秒。
	if p.cache != nil && nowNano()-p.stamp < 15*1e9 {
		return p.cache
	}
	seen := map[string]bool{}
	_ = filepath.Walk(p.root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		base := info.Name()
		if strings.HasPrefix(base, ".") || !srcExt[strings.ToLower(filepath.Ext(base))] {
			return nil
		}
		if info.Size() > 1<<20 { // 跳过超大文件
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, s := range extractSymbols(string(data)) {
			seen[s] = true
		}
		return nil
	})
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	p.cache, p.stamp = out, nowNano()
	return out
}
