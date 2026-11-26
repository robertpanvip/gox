package complete

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ProjectProvider 扫描当前工作目录：文件名 + 源码符号（func/type/struct/def/class…）。
// 让「打开一个项目，敲前缀就能补全项目里的符号和文件」开箱即用。
type ProjectProvider struct {
	root  string
	mu    sync.Mutex
	cache []Candidate
	stamp int64
	ttl   int64
}

var srcExt = map[string]bool{
	".go": true, ".gx": true, ".js": true, ".ts": true, ".tsx": true,
	".jsx": true, ".py": true, ".rs": true, ".java": true, ".c": true,
	".cpp": true, ".h": true, ".rb": true, ".php": true, ".cs": true,
}

func NewProjectProvider(root string) *ProjectProvider {
	if root == "" {
		root, _ = os.Getwd()
	}
	return &ProjectProvider{root: root, ttl: int64(8 * 1e9)}
}

func (p *ProjectProvider) Name() string { return "project" }

func (p *ProjectProvider) Provide(q Query) []Candidate {
	cwd := q.CWD
	if cwd == "" {
		cwd = p.root
	}
	pool := p.snapshot(cwd)
	if q.Token == "" {
		return nil
	}
	tl := strings.ToLower(q.Token)
	out := make([]Candidate, 0, 32)
	for _, c := range pool {
		if strings.HasPrefix(strings.ToLower(c.Label), tl) || strings.Contains(strings.ToLower(c.Label), tl) {
			out = append(out, c)
		}
	}
	return out
}

func (p *ProjectProvider) snapshot(cwd string) []Candidate {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := nowNano()
	if p.cache != nil && cwd == p.root && now-p.stamp < p.ttl {
		return p.cache
	}

	var files, syms []Candidate
	ents, err := os.ReadDir(cwd)
	if err != nil {
		p.root, p.stamp, p.cache = cwd, now, nil
		return nil
	}
	for _, e := range ents {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if e.IsDir() {
			files = append(files, Candidate{Text: name + "/", Label: name + "/", Detail: "目录", Kind: KindFile})
			continue
		}
		files = append(files, Candidate{Text: name, Label: name, Detail: "文件", Kind: KindFile})
		if !srcExt[strings.ToLower(filepath.Ext(name))] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cwd, name))
		if err != nil || len(data) > 2<<20 {
			continue
		}
		syms = append(syms, extractSymbols(string(data))...)
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Label < files[j].Label })
	seen := map[string]bool{}
	uniqSyms := syms[:0]
	for _, s := range syms {
		if !seen[s.Label] {
			seen[s.Label] = true
			uniqSyms = append(uniqSyms, s)
		}
	}
	sort.Slice(uniqSyms, func(i, j int) bool { return uniqSyms[i].Label < uniqSyms[j].Label })

	all := make([]Candidate, 0, len(files)+len(uniqSyms))
	all = append(all, uniqSyms...)
	all = append(all, files...)
	p.root, p.stamp, p.cache = cwd, now, all
	return all
}

// extractSymbols 用轻量词法扫描抽取声明符号，够补全用，不求编译级精确。
func extractSymbols(src string) []Candidate {
	var out []Candidate
	lines := strings.Split(src, "\n")
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if len(t) > 200 {
			continue
		}
		for _, k := range []string{"func ", "type ", "struct ", "interface ", "class ", "def ", "sig ", "fn ", "pub fn ", "const ", "var "} {
			if !strings.HasPrefix(t, k) {
				continue
			}
			name := symbolAfter(t[len(k):])
			if name == "" {
				continue
			}
			out = append(out, Candidate{Text: name, Label: name, Detail: "符号", Kind: KindSymbol})
			break
		}
	}
	return out
}

func symbolAfter(s string) string {
	end := 0
	for end < len(s) {
		c := s[end]
		if c == '(' || c == '{' || c == ' ' || c == '\t' || c == '=' || c == ':' || c == '[' {
			break
		}
		end++
	}
	return s[:end]
}
