package lsp

// textDocument/diagnostic（3.17 拉取模型）：与 publishDiagnostics 复用同一
// compileAndCheck 管线，客户端主动拉取时现算现返；工作区级拉取未实现，
// 能力声明如实置 false。

import "encoding/json"

func (s *Server) pullDiagnostic(body []byte) (any, *RPCError) {
	var p DiagnosticParams
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, &RPCError{Code: -32602, Message: err.Error()}
	}
	d, ok := s.docForURI(p.TextDocument.URI)
	if !ok {
		return DiagnosticReport{Kind: "full", Items: []Diagnostic{}}, nil
	}
	diags, _, err := compileAndCheck(d.Text, s.checker)
	if err != nil && len(diags) == 0 {
		diags = []Diagnostic{{
			Range: Range{Start: Position{}, End: Position{}}, Severity: 1, Source: "gox", Message: err.Error(),
		}}
	}
	return DiagnosticReport{Kind: "full", Items: diags}, nil
}
