package lsp

// 桌面 LSP 参数签名提示：与 goxlsp（wasm）共用 internal/typecheck 管线与持久
// Importer（s.timp），数据源为本机 GOROOT 磁盘视图。触发字符不符 / 编译映射
// 失败 / 类型层缺席一律返回空 SignatureHelp（客户端隐藏提示），零行为回归。

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/robertpanvip/gox/internal/typecheck"
	"github.com/robertpanvip/gox/pkg/compiler"
)

func (s *Server) signatureHelp(body []byte) (any, *RPCError) {
	var p TextDocumentPositionParams
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, &RPCError{Code: -32602, Message: err.Error()}
	}
	d, ok := s.docForURI(p.TextDocument.URI)
	if !ok {
		return SignatureHelp{}, nil
	}
	if h, ok := s.typedSignature(d.Text, p.Position.Line, p.Position.Character); ok {
		return h, nil
	}
	return SignatureHelp{}, nil
}

// typedSignature 在 .gx 坐标（0 基 line/character）解析所在调用签名。
func (s *Server) typedSignature(gxSrc string, line, character int) (SignatureHelp, bool) {
	if character <= 0 {
		return SignatureHelp{}, false
	}
	ls := strings.Split(gxSrc, "\n")
	if line < 0 || line >= len(ls) || character > len(ls[line]) {
		return SignatureHelp{}, false
	}
	k := character
	for k > 0 && (ls[line][k-1] == ' ' || ls[line][k-1] == '\t') {
		k--
	}
	if k == 0 {
		return SignatureHelp{}, false
	}
	if ch := ls[line][k-1]; ch != '(' && ch != ',' {
		return SignatureHelp{}, false
	}
	res, err := compiler.Compile("input.gx", gxSrc)
	if err != nil || res == nil || res.GoSource == "" || res.InvalidGo || res.SourceMap == nil {
		return SignatureHelp{}, false
	}
	if s.timp == nil {
		gr := runtime.GOROOT()
		if gr == "" {
			return SignatureHelp{}, false
		}
		s.timp = typecheck.NewImporter(typecheck.DiskLookup(filepath.Join(gr, "src"), runtime.GOOS, runtime.GOARCH))
	}
	gp, ok := res.SourceMap.GxToGo(line+1, k)
	if !ok {
		return SignatureHelp{}, false
	}
	off := typecheck.ByteOffset(res.GoSource, gp.Line, gp.Column)
	if off < 0 || off >= len(res.GoSource) {
		return SignatureHelp{}, false
	}
	if ch := res.GoSource[off]; ch != '(' && ch != ',' {
		return SignatureHelp{}, false
	}
	cs, ok := s.timp.Signature(res.GoSource, off+1)
	if !ok {
		return SignatureHelp{}, false
	}
	si := SignatureInformation{Label: cs.Label}
	for _, rg := range cs.Params {
		si.Parameters = append(si.Parameters, ParameterInformation{Label: rg})
	}
	return SignatureHelp{Signatures: []SignatureInformation{si}, ActiveParameter: cs.Active}, true
}
