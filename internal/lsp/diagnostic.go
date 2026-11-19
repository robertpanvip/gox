package lsp

import (
	"context"

	"github.com/robertpanvip/gox/internal/buildcheck"
	"github.com/robertpanvip/gox/pkg/compiler"
	"github.com/robertpanvip/gox/pkg/sourcemap"
)

// toDiagnostics 把 go build 诊断（Go 坐标，1 基）经 sourcemap 映射回 gx 坐标（0 基 LSP）。
func toDiagnostics(sm *sourcemap.SourceMap, diags []buildcheck.Diagnostic) []Diagnostic {
	out := make([]Diagnostic, 0, len(diags))
	for _, d := range diags {
		// Go 1 基 → sourcemap 查询 → gx 1 基 → LSP 0 基
		gxPos, ok := sm.GoToGx(d.Line, d.Col)
		line, col := gxPos.Line-1, gxPos.Column-1
		if !ok || line < 0 {
			line, col = d.Line-1, d.Col-1
		}
		if col < 0 {
			col = 0
		}
		p := Position{Line: line, Character: col}
		out = append(out, Diagnostic{
			Range:    Range{Start: p, End: p},
			Severity: 1,
			Source:   "gox",
			Message:  d.Message,
		})
	}
	return out
}

// compileAndCheck 转译 gx 源码并跑 buildcheck，返回映射后的诊断与产物。
func compileAndCheck(gxSrc string, checker *buildcheck.Checker) ([]Diagnostic, *compiler.Result, error) {
	res, err := compiler.Compile("input.gx", gxSrc)
	if err != nil {
		p := Position{Line: 0, Character: 0}
		return []Diagnostic{{
			Range: Range{Start: p, End: p}, Severity: 1, Source: "gox", Message: err.Error(),
		}}, nil, err
	}
	diags, err := checker.Check(context.Background(), res.GoSource)
	if err != nil {
		// 检查器失败（如 ctx 取消）不产诊断，但保留产物
		return nil, res, err
	}
	return toDiagnostics(res.SourceMap, diags), res, nil
}
