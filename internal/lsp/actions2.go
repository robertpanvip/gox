package lsp

// semanticTokens / inlayHints / completionItem/resolve：语义 token 走语法级分类
// （typecheck.Tokens，对 .gx 文本直接扫描，无需类型图）；参数提示走持久类型图
// （与签名/跳转同一 Importer）；resolve 依据候选 Data 从本机 GOROOT 取 doc。
// 数据链路与既有能力同源，任何环节不适用返回空结果，零行为回归。

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/robertpanvip/gox/internal/typecheck"
	"github.com/robertpanvip/gox/pkg/compiler"
)

func (s *Server) semanticTokens(body []byte) (any, *RPCError) {
	var p SemanticTokensParams
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, &RPCError{Code: -32602, Message: err.Error()}
	}
	d, ok := s.docForURI(p.TextDocument.URI)
	if !ok {
		return SemanticTokensResult{}, nil
	}
	toks, ok := typecheck.Tokens(d.Text)
	if !ok {
		return SemanticTokensResult{}, nil
	}
	return SemanticTokensResult{Data: typecheck.EncodeTokens(toks)}, nil
}

func (s *Server) inlayHints(body []byte) (any, *RPCError) {
	var p InlayHintParams
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, &RPCError{Code: -32602, Message: err.Error()}
	}
	d, ok := s.docForURI(p.TextDocument.URI)
	if !ok {
		return []InlayHint{}, nil
	}
	res, err := compiler.Compile("input.gx", d.Text)
	if err != nil || res == nil || res.GoSource == "" || res.InvalidGo || res.SourceMap == nil {
		return []InlayHint{}, nil
	}
	in := s.navImporter()
	if in == nil {
		return []InlayHint{}, nil
	}
	hs, ok := in.ParamHints(res.GoSource)
	if !ok {
		return []InlayHint{}, nil
	}
	out := make([]InlayHint, 0, len(hs))
	for _, h := range hs {
		gx, ok := res.SourceMap.GoToGx(h.Line, h.Col)
		if !ok {
			continue
		}
		out = append(out, InlayHint{
			Position: Position{Line: gx.Line - 1, Character: gx.Column - 1},
			Label:    h.Label,
			Kind:     2,
		})
	}
	return out, nil
}

func (s *Server) completionResolve(body []byte) (any, *RPCError) {
	var it CompletionItem
	if err := json.Unmarshal(body, &it); err != nil {
		return nil, &RPCError{Code: -32602, Message: err.Error()}
	}
	k := strings.LastIndex(it.Data, ".")
	if k <= 0 || k+1 >= len(it.Data) {
		return it, nil
	}
	pkg, name := it.Data[:k], it.Data[k+1:]
	gr := runtime.GOROOT()
	if gr == "" {
		return it, nil
	}
	lk := typecheck.DiskLookup(filepath.Join(gr, "src"), runtime.GOOS, runtime.GOARCH)
	doc, file, line, ok := typecheck.DocOf(pkg, name, lk)
	if ok {
		if doc != "" {
			it.Documentation = doc
		} else {
			it.Documentation = pkg + "/" + file + ":" + strconv.Itoa(line)
		}
	}
	return it, nil
}
