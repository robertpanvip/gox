package app

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// treeNode 是文件树的一个可见行。
type treeNode struct {
	Name  string
	Path  string
	Depth int
	Dir   bool
	Open  bool
}

// fileTree 维护一棵带折叠状态的文件树。
type fileTree struct {
	root      string
	collapsed map[string]bool // 被折叠的目录路径
}

func newFileTree(root string) *fileTree {
	return &fileTree{root: root, collapsed: map[string]bool{}}
}

// nodes 返回当前展开状态下的可见节点（扁平列表）。
func (t *fileTree) nodes() []treeNode {
	if t.root == "" {
		return nil
	}
	var out []treeNode
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		// 目录优先、再按名字排序。
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].IsDir() != entries[j].IsDir() {
				return entries[i].IsDir()
			}
			return entries[i].Name() < entries[j].Name()
		})
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
				continue
			}
			path := filepath.Join(dir, name)
			isDir := e.IsDir()
			open := isDir && !t.collapsed[path]
			out = append(out, treeNode{Name: name, Path: path, Depth: depth, Dir: isDir, Open: open})
			if isDir && open {
				walk(path, depth+1)
			}
		}
	}
	// 根节点本身也作为一行显示。
	out = append(out, treeNode{Name: filepath.Base(t.root), Path: t.root, Depth: 0, Dir: true, Open: !t.collapsed[t.root]})
	if !t.collapsed[t.root] {
		walk(t.root, 1)
	}
	return out
}

// toggle 展开/折叠指定目录。
func (t *fileTree) toggle(path string) {
	if t.collapsed[path] {
		delete(t.collapsed, path)
	} else {
		t.collapsed[path] = true
	}
}
