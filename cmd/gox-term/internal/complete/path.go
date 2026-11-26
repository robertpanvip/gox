package complete

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// PathProvider 扫描 $PATH 下的可执行文件，实现「装了全局工具，输首字母就弹」。
// 结果按目录缓存一段时间，避免每次按键都遍历磁盘。
type PathProvider struct {
	mu      sync.Mutex
	names   []string
	stamp   int64
	lastDir string
	ttl     int64 // 纳秒
}

func NewPathProvider() *PathProvider {
	return &PathProvider{ttl: int64(20 * 1e9)} // 20s
}

func (p *PathProvider) Name() string { return "path" }

func (p *PathProvider) Provide(q Query) []Candidate {
	names := p.snapshot()
	// 单词为空时给一批常见命令；否则子串筛选，交给引擎打分。
	out := make([]Candidate, 0, 32)
	for _, name := range names {
		if q.Token == "" || strings.Contains(name, q.Token) || subseqMatch(name, strings.ToLower(q.Token)) {
			out = append(out, Candidate{
				Text:   name + " ",
				Label:  name,
				Detail: "命令",
				Kind:   KindCommand,
			})
		}
	}
	return out
}

func (p *PathProvider) snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	dir := os.Getenv("PATH")
	now := nowNano()
	if p.names != nil && dir == p.lastDir && now-p.stamp < p.ttl {
		return p.names
	}
	seen := map[string]bool{}
	for _, d := range filepath.SplitList(dir) {
		if d == "" {
			continue
		}
		ents, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			info, err := e.Info()
			if err != nil || info.Mode()&0o111 == 0 {
				continue // 只要可执行文件
			}
			seen[e.Name()] = true
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	p.names, p.lastDir, p.stamp = names, dir, now
	return names
}
