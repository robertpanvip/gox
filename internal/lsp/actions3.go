package lsp

// typeDefinition / implementation / 跨文件 rename：与浏览器共用 internal/typecheck。
// rename 采取"全有或全无"的保守接入——所有打开文档都能编译、目标符号确凿命中
// 且存在跨文件语境时才走强类型路径；任一前提不满足即回退原符号表单文件逻辑
// （含枚举成员等特殊形态），零行为回归。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/robertpanvip/gox/internal/typecheck"
	"github.com/robertpanvip/gox/pkg/compiler"
)

func (s *Server) typeDefinition(body []byte) (any, *RPCError) {
	var p TextDocumentPositionParams
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, &RPCError{Code: -32602, Message: err.Error()}
	}
	d, ok := s.docForURI(p.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	off, res, ok := navCompile(d.Text, p.Position.Line, p.Position.Character)
	if !ok {
		return nil, nil
	}
	in := s.navImporter()
	if in == nil {
		return nil, nil
	}
	dd, ok := in.TypeDecl(res.GoSource, off+1)
	if !ok {
		return nil, nil
	}
	if dd.Pkg != "" {
		abs := filepath.Join(runtime.GOROOT(), "src", filepath.FromSlash(dd.Pkg), dd.File)
		if _, err := os.Stat(abs); err != nil {
			return nil, nil
		}
		return Location{URI: "file://" + abs, Range: refRange(dd.Line-1, dd.Col-1, len(dd.Name))}, nil
	}
	gx, ok := res.SourceMap.GoToGx(dd.Line, dd.Col)
	if !ok {
		return nil, nil
	}
	return Location{URI: p.TextDocument.URI, Range: refRange(gx.Line-1, gx.Column-1, len(dd.Name))}, nil
}

func (s *Server) implementation(body []byte) (any, *RPCError) {
	var p TextDocumentPositionParams
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
	decls, ok := in.Implementations(res.GoSource, off+1)
	if !ok {
		return []Location{}, nil
	}
	out := make([]Location, 0, len(decls))
	for _, dd := range decls {
		if dd.Pkg != "" {
			abs := filepath.Join(runtime.GOROOT(), "src", filepath.FromSlash(dd.Pkg), dd.File)
			if _, err := os.Stat(abs); err != nil {
				continue
			}
			out = append(out, Location{URI: "file://" + abs, Range: refRange(dd.Line-1, dd.Col-1, len(dd.Name))})
			continue
		}
		gx, ok := res.SourceMap.GoToGx(dd.Line, dd.Col)
		if !ok {
			continue
		}
		out = append(out, Location{URI: p.TextDocument.URI, Range: refRange(gx.Line-1, gx.Column-1, len(dd.Name))})
	}
	return out, nil
}

// workspaceRename 多文件精确定位；前提不满足返回 false（回退旧路径）。
func (s *Server) workspaceRename(p RenameParams) (WorkspaceEdit, bool) {
	newName := strings.TrimSpace(p.NewName)
	if newName == "" || len(s.docs) < 2 {
		return WorkspaceEdit{}, false
	}
	in := s.navImporter()
	if in == nil {
		return WorkspaceEdit{}, false
	}
	d, ok := s.docForURI(p.TextDocument.URI)
	if !ok {
		return WorkspaceEdit{}, false
	}
	off, _, ok := navCursor(d.Text, p.Position.Line, p.Position.Character)
	if !ok {
		return WorkspaceEdit{}, false
	}
	files := make([]typecheck.File, 0, len(s.docs))
	resByURI := map[string]*compiler.Result{}
	for uri, dd := range s.docs {
		res, err := compiler.Compile("input.gx", dd.Text)
		if err != nil || res == nil || res.GoSource == "" || res.InvalidGo || res.SourceMap == nil {
			return WorkspaceEdit{}, false // 任一文档坏 → 整体回退旧路径
		}
		files = append(files, typecheck.File{Name: uri, Src: res.GoSource})
		resByURI[uri] = res
	}
	oldName, occs, ok := in.MultiRename(files, p.TextDocument.URI, off+1)
	if !ok || oldName == "" {
		return WorkspaceEdit{}, false
	}
	changes := map[string][]TextEdit{}
	for uri, list := range occs {
		res := resByURI[uri]
		for _, o := range list {
			gx, ok := res.SourceMap.GoToGx(o.Line, o.Col)
			if !ok {
				continue
			}
			changes[uri] = append(changes[uri], TextEdit{Range: refRange(gx.Line-1, gx.Column-1, o.Len), NewText: newName})
		}
	}
	if len(changes) == 0 {
		return WorkspaceEdit{}, false
	}
	return WorkspaceEdit{Changes: changes}, true
}
