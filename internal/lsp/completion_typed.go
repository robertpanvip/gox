package lsp

// 桌面 LSP 的类型感知点号补全：与 goxlsp（wasm）共用 internal/typecheck 管线，
// 数据源用本机 GOROOT 磁盘虚拟 FS（无需 embed）。任何环节不适用都返回
// ok=false 回退原 symbolmap/关键字路径——零行为回归。

import (
	"path/filepath"
	"runtime"
	"strings"

	"github.com/robertpanvip/gox/internal/typecheck"
	"github.com/robertpanvip/gox/pkg/compiler"
)

// typedCompletion 在点号上下文返回成员候选（LSP 坐标 0 基）。
func (s *Server) typedCompletion(gxSrc string, line, col int) ([]CompletionItem, bool) {
	if col <= 0 {
		return nil, false
	}
	ls := strings.Split(gxSrc, "\n")
	if line < 0 || line >= len(ls) || col > len(ls[line]) || ls[line][col-1] != '.' {
		return nil, false
	}
	res, err := compiler.Compile("input.gx", gxSrc)
	if err != nil || res == nil || res.GoSource == "" || res.InvalidGo || res.SourceMap == nil {
		return nil, false
	}
	if s.timp == nil {
		gr := runtime.GOROOT()
		if gr == "" {
			return nil, false
		}
		s.timp = typecheck.NewImporter(typecheck.DiskLookup(filepath.Join(gr, "src"), runtime.GOOS, runtime.GOARCH))
	}
	gp, ok := res.SourceMap.GxToGo(line+1, col)
	if !ok {
		return nil, false
	}
	off := typecheck.ByteOffset(res.GoSource, gp.Line, gp.Column)
	if off < 0 || off >= len(res.GoSource) || res.GoSource[off] != '.' {
		return nil, false
	}
	cands := s.timp.Complete(res.GoSource, off+1)
	if len(cands) == 0 {
		return nil, false
	}
	items := make([]CompletionItem, 0, len(cands))
	for _, c := range cands {
		it := CompletionItem{Label: c.Name, Kind: completionKind(c.Kind), Detail: c.Signature}
		if c.Pkg != "" {
			it.Data = c.Pkg + "." + c.Name
		}
		items = append(items, it)
	}
	return items, true
}

// completionKind 内部 Kind → LSP CompletionItemKind。
func completionKind(k string) int {
	switch k {
	case "method":
		return 2
	case "function":
		return 3
	case "field":
		return 5
	case "package":
		return 9
	case "constant":
		return 21
	case "type":
		return 22
	}
	return 6
}
