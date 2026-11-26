package goxlsp

// 浏览器面导航增强：references 与 codeActions(QuickFix)。与桌面 internal/lsp
// 同源，复用同一持久类型图（s.timp）；本地坐标经 SourceMap 回投 .gx（0 基），
// 外部符号引用直接给出内嵌源坐标。QuickFix 作用于 .gx 文本本身（import 行两侧
// 语义一致），候选包名来自 embedstd 清单。

import (
	"github.com/robertpanvip/gox/internal/typecheck"
	"github.com/robertpanvip/gox/internal/typecheck/embedstd"
)

// ReferencesAt 返回光标（0 基行；col 指向光标前一字符的 1 基列）处符号的全部引用。
func (s *Session) ReferencesAt(line, col int) []Reference {
	off, ok := s.typedCursorPos(line, col)
	if !ok {
		return nil
	}
	refs, ok := s.timp.References(s.goSrc, off+1)
	if !ok {
		return nil
	}
	out := make([]Reference, 0, len(refs))
	for _, r := range refs {
		if r.Pkg != "" {
			out = append(out, Reference{Pkg: r.Pkg, File: r.File, Line: r.Line, Col: r.Col, Name: r.Name, IsDef: r.IsDef})
			continue
		}
		gx, ok := s.smap.GoToGx(r.Line, r.Col)
		if !ok {
			continue
		}
		out = append(out, Reference{Name: r.Name, Line: gx.Line - 1, Col: gx.Column - 1, IsDef: r.IsDef})
	}
	return out
}

// ReferencesJSON 返回引用 JSON（不适用时 null）。
func (s *Session) ReferencesJSON(line, col int) string { return marshal(s.ReferencesAt(line, col)) }

// CodeActions 基于当前诊断产出 QuickFix（补/删 import；0 基 .gx 文本坐标）。
func (s *Session) CodeActions() []CodeAction {
	if len(s.diags) == 0 {
		return nil
	}
	raw := make([]typecheck.RawDiag, 0, len(s.diags))
	for _, d := range s.diags {
		raw = append(raw, typecheck.RawDiag{Line: d.Range.Start.Line + 1, Msg: d.Message})
	}
	fixes := typecheck.QuickFixes(s.src, raw, embedstd.Packages())
	out := make([]CodeAction, 0, len(fixes))
	for _, f := range fixes {
		if f.DeleteLine > 0 {
			out = append(out, CodeAction{Title: f.Title, Edit: &TextEdit{Line: f.DeleteLine - 1, Delete: true}})
			continue
		}
		out = append(out, CodeAction{Title: f.Title, Edit: &TextEdit{Line: f.InsertLine - 1, Text: f.InsertText}})
	}
	return out
}

// CodeActionsJSON 返回 QuickFix 列表 JSON（无修复时 null）。
func (s *Session) CodeActionsJSON() string { return marshal(s.CodeActions()) }
