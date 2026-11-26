package lsp

// 桌面扩展能力：references / documentHighlight / codeAction(QuickFix) /
// workspace/symbol。与浏览器（pkg/goxlsp）同源，复用 internal/typecheck 的
// 持久类型图与 GOROOT 磁盘数据；磁盘不存在的路径如实跳过、不伪造跳转；
// quickfix 依据客户端回传的诊断文本执行（补/删 import）。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/robertpanvip/gox/internal/typecheck"
)

// ReferenceParams textDocument/references 入参。
type ReferenceParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      struct {
		IncludeDeclaration bool `json:"includeDeclaration"`
	} `json:"context"`
}

// DocumentHighlight 同文件符号高亮（Kind: 1 text, 2 read, 3 write/decl）。
type DocumentHighlight struct {
	Range Range `json:"range"`
	Kind  int   `json:"kind,omitempty"`
}

// CodeAction QuickFix 动作（LSP 子集；TextEdit/WorkspaceEdit 复用协议既有类型）。
type CodeAction struct {
	Title string         `json:"title"`
	Kind  string         `json:"kind,omitempty"`
	Edit  *WorkspaceEdit `json:"edit,omitempty"`
}

// CodeActionParams textDocument/codeAction 入参。
type CodeActionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
	Context      struct {
		Diagnostics []Diagnostic `json:"diagnostics"`
	} `json:"context"`
}

// WorkspaceSymbolParams workspace/symbol 入参。
type WorkspaceSymbolParams struct {
	Query string `json:"query"`
}

// WorkspaceSymbol 全局符号项（v1 覆盖已打开文档）。
type WorkspaceSymbol struct {
	Name     string   `json:"name"`
	Kind     int      `json:"kind"`
	Location Location `json:"location"`
}

func refRange(line, character, span int) Range {
	return Range{
		Start: Position{Line: line, Character: character},
		End:   Position{Line: line, Character: character + span},
	}
}

func (s *Server) references(body []byte) (any, *RPCError) {
	var p ReferenceParams
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, &RPCError{Code: -32602, Message: err.Error()}
	}
	d, ok := s.docForURI(p.TextDocument.URI)
	if !ok {
		return []Location{}, nil
	}
	off, res, ok := navCompile(d.Text, p.Position.Line, p.Position.Character)
	if !ok {
		return []Location{}, nil
	}
	in := s.navImporter()
	if in == nil {
		return []Location{}, nil
	}
	refs, ok := in.References(res.GoSource, off+1)
	if !ok {
		return []Location{}, nil
	}
	out := make([]Location, 0, len(refs))
	for _, r := range refs {
		if r.Pkg != "" {
			abs := filepath.Join(runtime.GOROOT(), "src", filepath.FromSlash(r.Pkg), r.File)
			if _, err := os.Stat(abs); err != nil {
				continue
			}
			out = append(out, Location{URI: "file://" + abs, Range: refRange(r.Line-1, r.Col-1, len(r.Name))})
			continue
		}
		gx, ok := res.SourceMap.GoToGx(r.Line, r.Col)
		if !ok {
			continue
		}
		out = append(out, Location{URI: p.TextDocument.URI, Range: refRange(gx.Line-1, gx.Column-1, len(r.Name))})
	}
	return out, nil
}

func (s *Server) documentHighlight(body []byte) (any, *RPCError) {
	var p TextDocumentPositionParams
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, &RPCError{Code: -32602, Message: err.Error()}
	}
	d, ok := s.docForURI(p.TextDocument.URI)
	if !ok {
		return []DocumentHighlight{}, nil
	}
	off, res, ok := navCompile(d.Text, p.Position.Line, p.Position.Character)
	if !ok {
		return []DocumentHighlight{}, nil
	}
	in := s.navImporter()
	if in == nil {
		return []DocumentHighlight{}, nil
	}
	refs, ok := in.References(res.GoSource, off+1)
	if !ok {
		return []DocumentHighlight{}, nil
	}
	out := make([]DocumentHighlight, 0, len(refs))
	for _, r := range refs {
		if r.Pkg != "" {
			continue
		}
		gx, ok := res.SourceMap.GoToGx(r.Line, r.Col)
		if !ok {
			continue
		}
		kind := 2
		if r.IsDef {
			kind = 3
		}
		out = append(out, DocumentHighlight{Range: refRange(gx.Line-1, gx.Column-1, len(r.Name)), Kind: kind})
	}
	return out, nil
}

func (s *Server) codeAction(body []byte) (any, *RPCError) {
	var p CodeActionParams
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, &RPCError{Code: -32602, Message: err.Error()}
	}
	d, ok := s.docForURI(p.TextDocument.URI)
	if !ok {
		return []CodeAction{}, nil
	}
	raw := make([]typecheck.RawDiag, 0, len(p.Context.Diagnostics))
	for _, dg := range p.Context.Diagnostics {
		raw = append(raw, typecheck.RawDiag{Line: dg.Range.Start.Line + 1, Msg: dg.Message})
	}
	fixes := typecheck.QuickFixes(d.Text, raw, diskPkgNames())
	out := make([]CodeAction, 0, len(fixes))
	for _, f := range fixes {
		edits := []TextEdit{}
		if f.DeleteLine > 0 {
			edits = append(edits, TextEdit{
				Range:   Range{Start: Position{Line: f.DeleteLine - 1}, End: Position{Line: f.DeleteLine}},
				NewText: "",
			})
		} else {
			q := Position{Line: f.InsertLine - 1}
			edits = append(edits, TextEdit{Range: Range{Start: q, End: q}, NewText: f.InsertText})
		}
		out = append(out, CodeAction{
			Title: f.Title,
			Kind:  "quickfix",
			Edit:  &WorkspaceEdit{Changes: map[string][]TextEdit{p.TextDocument.URI: edits}},
		})
	}
	// organizeImports：纯词法整理动作，与诊断无关、常驻提供（有变更才出现）
	if oedits, ok := typecheck.OrganizeImports(d.Text); ok {
		oes := make([]TextEdit, 0, len(oedits))
		for _, oe := range oedits {
			oes = append(oes, TextEdit{
				Range:   Range{Start: Position{Line: oe.StartLine - 1}, End: Position{Line: oe.EndLine}},
				NewText: oe.Text,
			})
		}
		out = append(out, CodeAction{
			Title: "organize imports",
			Kind:  "source.organizeImports",
			Edit:  &WorkspaceEdit{Changes: map[string][]TextEdit{p.TextDocument.URI: oes}},
		})
	}
	return out, nil
}

func (s *Server) workspaceSymbol(body []byte) (any, *RPCError) {
	var p WorkspaceSymbolParams
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, &RPCError{Code: -32602, Message: err.Error()}
	}
	q := strings.ToLower(p.Query)
	out := []WorkspaceSymbol{}
	for uri, d := range s.docs {
		for _, sym := range s.symbolMap(d).Symbols {
			if q != "" && !strings.Contains(strings.ToLower(sym.Name), q) {
				continue
			}
			out = append(out, WorkspaceSymbol{
				Name:     sym.Name,
				Kind:     sym.Kind,
				Location: Location{URI: uri, Range: refRange(sym.Line, sym.Col, len(sym.Name))},
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Location.URI < out[j].Location.URI
	})
	if len(out) > 100 {
		out = out[:100]
	}
	return out, nil
}
