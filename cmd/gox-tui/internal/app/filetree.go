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

// glyphFor 返回节点图标（emoji）与主题里的样式 key。
// 目录用 📂/📁 表达展开/折叠，文件按扩展名/用途分类， IDEA 风格一眼可辨。
// 注意：emoji 通常是双宽字符（lipgloss.Width==2），view.go 的 truncateWidth
// 已按显示宽度截断，缩进也按列计算，故不影响文件树对齐与点击命中。
func glyphFor(n treeNode) (glyph, styleKey string) {
	if n.Dir {
		if n.Open {
			return "📂", "folder"
		}
		return "📁", "folder"
	}
	name := strings.ToLower(n.Name)
	ext := filepath.Ext(name)
	switch {
	case strings.HasSuffix(name, "_test.go"):
		return "🧪", "test"
	case ext == ".go":
		return "🐹", "go"
	case ext == ".gx":
		return "⚡", "gx"
	case ext == ".md" || ext == ".markdown":
		return "📝", "md"
	case ext == ".json" || ext == ".yaml" || ext == ".yml" || ext == ".toml" ||
		ext == ".ini" || ext == ".xml" || ext == ".conf" || ext == ".mod" ||
		ext == ".sum" || ext == ".lock":
		return "📋", "conf"
	case ext == ".sh" || ext == ".bash" || ext == ".zsh" || ext == ".bat":
		return "🔧", "script"
	default:
		return "📄", "file"
	}
}

// nodeAt 返回屏幕 y 对应的文件树节点下标（见 viewTree 布局：header 在 y=3，node[i] 在 y=4+i）。
func (m *Model) nodeAt(screenY int) (int, bool) {
	const headerY = 4 // 上边框(1) + 标题栏(1) + body padding(1) + header(1)
	idx := screenY - headerY
	if idx < 0 {
		return 0, false
	}
	nodes := m.tree.nodes()
	if idx >= len(nodes) {
		return 0, false
	}
	// 视口高度限制：viewTree 最多渲染 h-1 个节点。
	h := m.height - 4
	if idx >= h-1 {
		return 0, false
	}
	return idx, true
}

// inTreeX 判断屏幕 x 是否落在文件树可点击列（左边框1 + padding1 起，到 treeW）。
func (m *Model) inTreeX(screenX int) bool {
	return screenX >= 2 && screenX <= m.treeWidth()+1
}
