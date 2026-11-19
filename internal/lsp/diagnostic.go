package lsp

import (
	"context"

	"github.com/robertpanvip/gox/internal/buildcheck"
	"github.com/robertpanvip/gox/internal/check"
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

// toGxDiagnostics 把 GX 语义层诊断（已是 GX 坐标，1 基）转成 LSP 诊断（0 基）。
// 这些诊断由 AST + side table 规则与 token 层分析产出，无需 sourcemap 回投。
func toGxDiagnostics(ds []check.Diagnostic) []Diagnostic {
	out := make([]Diagnostic, 0, len(ds))
	for _, d := range ds {
		line, col := d.Line-1, d.Col-1
		if line < 0 {
			line = 0
		}
		if col < 0 {
			col = 0
		}
		p := Position{Line: line, Character: col}
		code := d.Code
		out = append(out, Diagnostic{
			Range: Range{Start: p, End: p},
			Severity: 1, Source: "gox", Code: &code,
			Message: d.Code + ": " + d.Message,
		})
	}
	return out
}

// compileAndCheck 转译 gx 源码，跑 buildcheck（go build 兜底）与 GX 语义层检查，
// 两路诊断合并后返回（均为 GX 坐标，0 基）与产物。
func compileAndCheck(gxSrc string, checker *buildcheck.Checker) ([]Diagnostic, *compiler.Result, error) {
	res, err := compiler.Compile("input.gx", gxSrc)
	if err != nil {
		p := Position{Line: 0, Character: 0}
		return []Diagnostic{{
			Range: Range{Start: p, End: p}, Severity: 1, Source: "gox", Message: err.Error(),
		}}, nil, err
	}
	// 第一路：go build 诊断（Go 坐标 → sourcemap 回投）
	var diags []Diagnostic
	goDiags, err := checker.Check(context.Background(), res.GoSource)
	if err == nil {
		diags = append(diags, toDiagnostics(res.SourceMap, goDiags)...)
	}
	// 第二路：GX 语义层诊断（AST 规则 + token 层分析，已是 GX 坐标）
	if res.Analysis != nil {
		c := check.New(res.SourceMap, res.Analysis)
		gxDiags, _ := c.CheckAll(res.GoSource)
		diags = append(diags, toGxDiagnostics(gxDiags)...)
	}
	return diags, res, err
}
