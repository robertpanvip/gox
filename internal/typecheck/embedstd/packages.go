package embedstd

// 可导入路径清单：供 import 前缀补全。std 与 gox 闭包各按目录树收集——
// 目录内含 .go 文件即为一个可导入包；internal/testdata 段整支跳过。
// 数据未生成时返回 nil（调用方回退原补全路径，零行为变化）。

import (
	"io/fs"
	"sort"
	"strings"
	"sync"
)

var (
	pkgsOnce sync.Once
	pkgsList []string
)

// Packages 返回排序后的全部可导入路径。
func Packages() []string {
	if Version() == "" {
		return nil
	}
	pkgsOnce.Do(func() {
		out := []string{}
		collect := func(root, prefix string) {
			_ = fs.WalkDir(srcs, root, func(p string, d fs.DirEntry, err error) error {
				if err != nil || !d.IsDir() {
					return nil
				}
				rel := strings.TrimPrefix(p, root)
				rel = strings.TrimPrefix(rel, "/")
				segs := rel
				if segs != "" && containsSeg(segs, "internal") {
					return fs.SkipDir
				}
				if segs != "" && containsSeg(segs, "testdata") {
					return fs.SkipDir
				}
				if rel == "." || rel == "" {
					return nil
				}
				if hasGoFile(p) {
					out = append(out, prefix+rel)
				}
				return nil
			})
		}
		collect("_srcs/std", "")
		collect("_srcs/gox", "github.com/robertpanvip/gox/")
		sort.Strings(out)
		pkgsList = out
	})
	return pkgsList
}

func containsSeg(rel, seg string) bool {
	for _, s := range strings.Split(rel, "/") {
		if s == seg {
			return true
		}
	}
	return false
}

func hasGoFile(dir string) bool {
	entries, err := fs.ReadDir(srcs, dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			return true
		}
	}
	return false
}
