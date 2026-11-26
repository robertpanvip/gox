package typecheck

// QuickFix：把诊断文本转成可执行修复。v1 覆盖两个最高频场景——
// undefined 包名 → 插入 import；imported and not used → 删除该 import 行。
// 行号为输入源码 1 基；补 import 的候选来自注入的可导入路径清单。

import (
	"regexp"
	"strconv"
	"strings"
)

// RawDiag 是修复规则消费的最小诊断形态。
type RawDiag struct {
	Line int // 1 基
	Msg  string
}

// QuickFix 一条修复：InsertLine>0 表示在该行行首前插入 InsertText；
// DeleteLine>0 表示删除该行。两者互斥。
type QuickFix struct {
	Title      string
	InsertLine int
	InsertText string
	DeleteLine int
}

var (
	reUndefined = regexp.MustCompile(`undefined: ([A-Za-z_][A-Za-z0-9_]*)`)
	reUnusedImp = regexp.MustCompile(`"([^"]+)" imported and not used`)
)

// QuickFixes 按诊断产出修复；pkgNames 为可导入路径（空则 undefined 类不产出）；Title 去重。
func QuickFixes(src string, diags []RawDiag, pkgNames []string) []QuickFix {
	lines := strings.Split(src, "\n")
	pkgLine, lastSingle, blockOpen := 0, 0, 0
	inBlock := false
	for i, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case pkgLine == 0 && strings.HasPrefix(t, "package "):
			pkgLine = i + 1
		case t == "import (":
			blockOpen = i + 1
			inBlock = true
		case inBlock && t == ")":
			inBlock = false
		case !inBlock && strings.HasPrefix(t, "import"):
			lastSingle = i + 1
		}
	}
	anchor := func(pkg string) (int, string) {
		if blockOpen > 0 {
			return blockOpen + 1, "\t" + strconv.Quote(pkg) + "\n"
		}
		if lastSingle > 0 {
			return lastSingle + 1, "import " + strconv.Quote(pkg) + "\n"
		}
		return pkgLine + 1, "import " + strconv.Quote(pkg) + "\n"
	}
	out := []QuickFix{}
	seen := map[string]bool{}
	for _, d := range diags {
		if m := reUnusedImp.FindStringSubmatch(d.Msg); m != nil {
			title := "remove unused import " + strconv.Quote(m[1])
			if !seen[title] && d.Line >= 1 && d.Line <= len(lines) {
				seen[title] = true
				out = append(out, QuickFix{Title: title, DeleteLine: d.Line})
			}
			continue
		}
		if m := reUndefined.FindStringSubmatch(d.Msg); m != nil {
			name := m[1]
			for _, p := range pkgNames {
				if p != name && !strings.HasSuffix(p, "/"+name) {
					continue
				}
				title := "add " + strconv.Quote(p) + " import"
				if seen[title] {
					continue
				}
				seen[title] = true
				ln, txt := anchor(p)
				out = append(out, QuickFix{Title: title, InsertLine: ln, InsertText: txt})
			}
		}
	}
	return out
}
