package lsp

// workspace/willRenameFiles + workspace/didRenameFiles：gox 文件包语义独立、
// 互不引用文件名，重命名无需跨文件编辑——价值在服务器状态正确跟随：文档表迁移、
// 符号缓存标脏、按新 URI 换发诊断（旧 URI 清空），gox-ide 改名即所见即所得。
// willRename 冲突校验：目标 URI 已被其他打开文档占用时报错，不静默覆盖。

import (
	"bufio"
	"encoding/json"
)

func (s *Server) willRenameFiles(body []byte) (any, *RPCError) {
	var p WorkspaceRenameFilesParams
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, &RPCError{Code: -32602, Message: err.Error()}
	}
	for _, f := range p.Files {
		if f.OldURI == f.NewURI {
			continue
		}
		if _, busy := s.docs[f.NewURI]; busy {
			return nil, &RPCError{Code: -32602, Message: "rename target already open: " + f.NewURI}
		}
	}
	return WorkspaceEdit{Changes: map[string][]TextEdit{}}, nil
}

func (s *Server) didRenameFiles(body []byte, w *bufio.Writer) {
	var p WorkspaceRenameFilesParams
	if json.Unmarshal(body, &p) != nil {
		return
	}
	for _, f := range p.Files {
		if f.OldURI == "" || f.NewURI == "" || f.OldURI == f.NewURI {
			continue
		}
		d := s.docs[f.OldURI]
		if d == nil {
			continue
		}
		delete(s.docs, f.OldURI)
		d.SymbolsDirty = true
		s.docs[f.NewURI] = d
		_ = writeMessage(w, NotificationMessage{
			JSONRPC: "2.0", Method: "textDocument/publishDiagnostics",
			Params: mustJSON(PublishDiagnosticsParams{URI: f.OldURI, Diagnostics: []Diagnostic{}}),
		})
		s.publishDiagnostics(f.NewURI, w)
	}
}
