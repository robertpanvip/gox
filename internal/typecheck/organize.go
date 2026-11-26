package typecheck

// organizeImports：纯词法的 import 区整理（不依赖类型图，任何编辑态可用）。
// 规则：
//   1) 连续无空行的单行 import（≥2 条）合并为一个括号块；
//   2) 块内按空行分子组，每个子组内 std 在前、非 std 在后（两段间空一行），
//      组内按路径升序；空行隔开的子组不合并、不调序；
//   3) 块内含注释或复杂形态时保守跳过；`import "C"`（cgo）原样置于子组末尾；
//   4) 结果与原文一致时返回 false（幂等，不乱改）。

import (
	"sort"
	"strings"
)

// ImportEdit 区域替换：[StartLine, EndLine] 1 基闭区间，Text 含尾随换行。
type ImportEdit struct {
	StartLine int
	EndLine   int
	Text      string
}

type impEntry struct {
	alias string
	path  string
}

func (e impEntry) line() string {
	if e.alias != "" {
		return "\t" + e.alias + " \"" + e.path + "\""
	}
	return "\t\"" + e.path + "\""
}

// isStdImport 首段不含点视为标准库（gox 仓库路径同理归 std 组）。
func isStdImport(p string) bool {
	first := p
	if i := strings.Index(p, "/"); i >= 0 {
		first = p[:i]
	}
	return !strings.Contains(first, ".")
}

// parseImportEntry 解析一条 import 目标。kw=true 输入为整句 `import [alias] "path"`，
// kw=false 为块内条目 `[alias] "path"`。别名限标识符/下划线/点；尾部多余引号视为复杂形态。
func parseImportEntry(t string, kw bool) (impEntry, bool) {
	if kw {
		if !strings.HasPrefix(t, "import ") {
			return impEntry{}, false
		}
		t = strings.TrimSpace(t[len("import"):])
	} else if strings.HasPrefix(t, "import") {
		return impEntry{}, false
	}
	q1 := strings.IndexByte(t, '"')
	if q1 < 0 {
		return impEntry{}, false
	}
	rest := t[q1+1:]
	q2 := strings.IndexByte(rest, '"')
	if q2 < 0 {
		return impEntry{}, false
	}
	tail := strings.TrimSpace(rest[q2+1:])
	if tail != "" {
		return impEntry{}, false // 行尾有注释等杂质
	}
	path := rest[:q2]
	alias := strings.TrimSpace(t[:q1])
	if alias != "" && alias != "." && !validAlias(alias) {
		return impEntry{}, false
	}
	if path == "" {
		return impEntry{}, false
	}
	return impEntry{alias: alias, path: path}, true
}

func validAlias(a string) bool {
	for i := 0; i < len(a); i++ {
		c := a[i]
		ok := c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
		if !ok && i > 0 {
			ok = c >= '0' && c <= '9'
		}
		if !ok {
			return false
		}
	}
	return true
}

// sortSubGroup 子组内排序：std 段 + 非 std 段 + C 原样，返回分段文本。
func sortSubGroup(entries []impEntry) []string {
	var std, ext, cg []impEntry
	for _, e := range entries {
		switch {
		case e.path == "C":
			cg = append(cg, e)
		case isStdImport(e.path):
			std = append(std, e)
		default:
			ext = append(ext, e)
		}
	}
	key := func(e impEntry) string { return e.path + "\x00" + e.alias }
	sort.SliceStable(std, func(i, j int) bool { return key(std[i]) < key(std[j]) })
	sort.SliceStable(ext, func(i, j int) bool { return key(ext[i]) < key(ext[j]) })
	var parts []string
	for _, g := range [][]impEntry{std, ext, cg} {
		if len(g) == 0 {
			continue
		}
		var b []string
		for _, e := range g {
			b = append(b, e.line())
		}
		parts = append(parts, strings.Join(b, "\n"))
	}
	return parts
}

func flatLines(entries []impEntry) string {
	var b []string
	for _, e := range entries {
		b = append(b, e.line())
	}
	return strings.Join(b, "\n")
}

// renderBlock 括号块文本（尾随换行）。
func renderBlock(parts []string) string {
	var sb strings.Builder
	sb.WriteString("import (\n")
	for i, p := range parts {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(p)
		sb.WriteString("\n")
	}
	sb.WriteString(")\n")
	return sb.String()
}

// OrganizeImports 返回全部需要改动的 import 区；无需改动时返回 false（幂等）。
func OrganizeImports(src string) ([]ImportEdit, bool) {
	lines := strings.Split(src, "\n")
	type region struct {
		start, end int
		text       string
	}
	var regions []region
	i := 0
	for i < len(lines) {
		t := strings.TrimSpace(lines[i])
		if t == "import (" {
			j := i + 1
			var groups [][]impEntry
			var cur []impEntry
			closed, bad := false, false
			for j < len(lines) {
				s := strings.TrimSpace(lines[j])
				if s == ")" {
					closed = true
					break
				}
				if s == "" {
					if len(cur) > 0 {
						groups = append(groups, cur)
						cur = nil
					}
					j++
					continue
				}
				if strings.HasPrefix(s, "//") {
					bad = true
					break
				}
				e, ok := parseImportEntry(s, false)
				if !ok {
					bad = true
					break
				}
				cur = append(cur, e)
				j++
			}
			if closed && !bad {
				if len(cur) > 0 {
					groups = append(groups, cur)
				}
				var newParts []string
				changed := false
				for _, g := range groups {
					sub := sortSubGroup(g)
					if strings.Join(sub, "\n\n") != flatLines(g) {
						changed = true
					}
					newParts = append(newParts, sub...)
				}
				if changed {
					regions = append(regions, region{i, j, renderBlock(newParts)})
				}
				i = j + 1
				continue
			}
			i++
			continue
		}
		if strings.HasPrefix(t, "import ") && strings.Contains(t, "\"") {
			var run []impEntry
			start := i
			k := i
			for k < len(lines) {
				tk := strings.TrimSpace(lines[k])
				e, ok := parseImportEntry(tk, true)
				if !ok {
					break
				}
				run = append(run, e)
				k++
			}
			if len(run) >= 2 {
				regions = append(regions, region{start, k - 1, renderBlock(sortSubGroup(run))})
			}
			i = k
			continue
		}
		i++
	}
	if len(regions) == 0 {
		return nil, false
	}
	edits := make([]ImportEdit, 0, len(regions))
	for _, r := range regions {
		edits = append(edits, ImportEdit{StartLine: r.start + 1, EndLine: r.end + 1, Text: r.text})
	}
	return edits, true
}
