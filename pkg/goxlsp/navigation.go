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

// TypeDefinitionAt 光标表达式类型的声明位置（外部=内嵌源快览；本地=.gx 0 基回投）。
func (s *Session) TypeDefinitionAt(line, col int) *DefinitionResult {
	off, ok := s.typedCursorPos(line, col)
	if !ok {
		return nil
	}
	d, ok := s.timp.TypeDecl(s.goSrc, off+1)
	if !ok {
		return nil
	}
	if d.Pkg != "" {
		return &DefinitionResult{Kind: "std", Pkg: d.Pkg, File: d.File, Line: d.Line, Snippet: d.Snippet, Doc: d.Doc}
	}
	gx, ok := s.smap.GoToGx(d.Line, d.Col)
	if !ok {
		return nil
	}
	r := Range{
		Start: Position{Line: gx.Line - 1, Character: gx.Column - 1},
		End:   Position{Line: gx.Line - 1, Character: gx.Column - 1},
	}
	return &DefinitionResult{Kind: "local", Range: &r}
}

// SymbolRef 实现列表项：本地实现为 .gx 0 基坐标；外部实现带 pkg/file。
type SymbolRef struct {
	Name string `json:"name"`
	Pkg  string `json:"pkg,omitempty"`
	File string `json:"file,omitempty"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
}

// ImplementationsAt 光标接口（类型名或接口方法）→ 本源内实现类型列表。
func (s *Session) ImplementationsAt(line, col int) []SymbolRef {
	off, ok := s.typedCursorPos(line, col)
	if !ok {
		return nil
	}
	decls, ok := s.timp.Implementations(s.goSrc, off+1)
	if !ok {
		return nil
	}
	out := make([]SymbolRef, 0, len(decls))
	for _, d := range decls {
		if d.Pkg != "" {
			out = append(out, SymbolRef{Name: d.Name, Pkg: d.Pkg, File: d.File, Line: d.Line - 1, Col: d.Col - 1})
			continue
		}
		gx, ok := s.smap.GoToGx(d.Line, d.Col)
		if !ok {
			continue
		}
		out = append(out, SymbolRef{Name: d.Name, Line: gx.Line - 1, Col: gx.Column - 1})
	}
	return out
}

// TypeDefinitionJSON 返回 typeDefinition JSON（不适用 null）。
func (s *Session) TypeDefinitionJSON(line, col int) string {
	return marshal(s.TypeDefinitionAt(line, col))
}

// ImplementationsJSON 返回实现列表 JSON（不适用 null）。
func (s *Session) ImplementationsJSON(line, col int) string {
	return marshal(s.ImplementationsAt(line, col))
}
