// Package goxlsp 提供浏览器 / 单文件 HTML 场景下的无服务端 GX 语言服务门面。
//
// 与 internal/lsp 的 stdio 语言服务器共享语义（符号表规则、补全触发条件），
// 但刻意不走 buildcheck（它依赖 go build 外部进程，在 js/wasm 里不可用），
// 也不 import internal/lsp（避免把 os/exec 链拖进 wasm）。诊断只取编译器错误
// + GX 语义层规则，补全 / hover / 跳转 / 文档符号全部基于源码符号表，
// 纯 Go 实现，可直接编译进 playground 的 wasm。
package goxlsp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/robertpanvip/gox/internal/check"
	"github.com/robertpanvip/gox/internal/typecheck"
	"github.com/robertpanvip/gox/internal/typecheck/embedstd"
	"github.com/robertpanvip/gox/pkg/compiler"
	"github.com/robertpanvip/gox/pkg/sourcemap"
)

// Session 持有单份源码的 LSP 状态。非并发安全，始终在 UI 线程（wasm 主线程）访问。
type Session struct {
	src   string
	syms  *SymbolMap
	diags []Diagnostic
	// timp 持久类型检查会话（embedstd 就绪时惰性创建，std 依赖图只建一次）
	timp *typecheck.Importer
	// 最近一次编译的 Go 产物与映射，供类型感知补全做 gx→go 坐标换算
	goSrc string
	smap  *sourcemap.SourceMap
}

// New 创建空会话。
func New() *Session { return &Session{diags: []Diagnostic{}} }

// SetSource 更新源码并失效缓存。调用后诊断立即重算，符号表惰性重建。
func (s *Session) SetSource(src string) {
	s.src = src
	s.syms = nil
	s.diagnostics()
}

// Source 返回当前源码。
func (s *Session) Source() string { return s.src }

func (s *Session) symbolMap() *SymbolMap {
	if s.syms == nil {
		s.syms = BuildSymbolMap(s.src)
	}
	return s.syms
}

func (s *Session) lines() []string {
	return strings.Split(strings.ReplaceAll(s.src, "\r\n", "\n"), "\n")
}

// diagnostics 跑编译器 + GX 语义检查，产出 0 基 LSP 诊断。
// 语法层：go/parser 纯 Go，可在 wasm 内运行，parse 诊断已逐条回投 .gx 坐标；
// 类型层：完整 go/types/go build 兜底需要外部 Go 工具链，js/wasm 不可用——
// 浏览器内不提供该层，桌面 gox-lsp / CLI `gox check -check` 负责。
func (s *Session) diagnostics() {
	s.diags = make([]Diagnostic, 0)
	if strings.TrimSpace(s.src) == "" {
		return
	}
	res, err := compiler.Compile("input.gx", s.src)
	s.goSrc, s.smap = "", nil
	if err != nil {
		s.diags = append(s.diags, Diagnostic{
			Range:    Range{Start: Position{}, End: Position{}},
			Severity: 1, Source: "gox", Message: err.Error(),
		})
		return
	}
	if res == nil || res.Analysis == nil {
		return
	}
	c := check.New(res.SourceMap, res.Analysis)
	gxDiags, _ := c.CheckAll(res.GoSource)
	for _, d := range gxDiags {
		line, col := d.Line-1, d.Col-1
		if line < 0 {
			line = 0
		}
		if col < 0 {
			col = 0
		}
		p := Position{Line: line, Character: col}
		code := d.Code
		s.diags = append(s.diags, Diagnostic{
			Range:    Range{Start: p, End: p},
			Severity: 1, Source: "gox", Code: &code,
			Message: d.Code + ": " + d.Message,
		})
	}
	// 类型层（embedstd 就绪时）：go/types 完整检查生成代码，坐标经 sourcemap
	// 回投 .gx。未生成 embed 数据时整段跳过，行为与接入前逐字节一致。
	if res.GoSource != "" && !res.InvalidGo {
		s.goSrc, s.smap = res.GoSource, res.SourceMap
		if lk := embedstd.Lookup(); lk != nil {
			if s.timp == nil {
				s.timp = typecheck.NewImporter(lk) // std 类型图跨编辑持久，见 typecheck.Importer
			}
			tds, terr := s.timp.Check(res.GoSource)
			if terr == nil {
				tcode := "type"
				for _, d := range tds {
					line, col := d.Line-1, d.Col-1
					if res.SourceMap != nil {
						if gp, ok := res.SourceMap.GoToGx(d.Line, d.Col); ok {
							line, col = gp.Line-1, gp.Column-1
						}
					}
					if line < 0 {
						line = 0
					}
					if col < 0 {
						col = 0
					}
					tp := Position{Line: line, Character: col}
					s.diags = append(s.diags, Diagnostic{
						Range:    Range{Start: tp, End: tp},
						Severity: 1, Source: "gox", Code: &tcode,
						Message: "type: " + d.Message,
					})
				}
			}
		}
	}
}

// Diagnostics 返回当前诊断（拷贝，避免调用方改动内部状态）。
func (s *Session) Diagnostics() []Diagnostic {
	out := make([]Diagnostic, len(s.diags))
	copy(out, s.diags)
	return out
}

// HoverAt 返回光标处的符号说明；无符号返回 nil。
func (s *Session) HoverAt(line, col int) *Hover {
	sym := s.symbolMap().At(s.lines(), line, col)
	if sym == nil {
		return nil
	}
	value := fmt.Sprintf("**%s** — %s\n\n`%s`", sym.Name, sym.Doc, sym.Detail)
	return &Hover{Contents: struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	}{Kind: "markdown", Value: value}}
}

// DefinitionAt 返回光标处符号的定义位置；无符号返回 nil。
func (s *Session) DefinitionAt(line, col int) *Location {
	sym := s.symbolMap().At(s.lines(), line, col)
	if sym == nil {
		return nil
	}
	return &Location{
		Range: Range{
			Start: Position{Line: sym.Line, Character: sym.Col},
			End:   Position{Line: sym.Line, Character: sym.Col + len(sym.Name)},
		},
	}
}

// CompleteAt 返回光标处的补全候选。位于 `<` 之后时只列 JSX 组件，
// 与 internal/lsp 的补全规则一致。
func (s *Session) CompleteAt(line, col int) CompletionList {
	sm := s.symbolMap()
	items := make([]CompletionItem, 0)
	seen := map[string]bool{}
	// 类型感知分支：光标紧跟点号 → 只列成员（不掺全局符号/关键字，避免噪音）
	if list, ok := s.typedCompletion(line, col); ok {
		return list
	}
	add := func(sym Symbol) {
		if seen[sym.Name] {
			return
		}
		seen[sym.Name] = true
		items = append(items, CompletionItem{
			Label: sym.Name, Kind: sym.Kind, Detail: sym.Detail,
		})
	}
	if inJSXTagOpen(s.src, line, col) {
		for _, sym := range sm.Symbols {
			if isComponentName(sym.Name) && isComponentKind(sym.Kind) {
				add(sym)
			}
		}
		return CompletionList{IsIncomplete: false, Items: items}
	}
	for _, sym := range sm.Symbols {
		add(sym)
	}
	for _, kw := range []string{"func", "pub", "type", "enum", "var", "const",
		"import", "return", "if", "else", "for", "sig", "map", "filter", "reduce"} {
		items = append(items, CompletionItem{Label: kw, Kind: 14, Detail: "keyword"})
	}
	return CompletionList{IsIncomplete: false, Items: items}
}

// typedCompletion 在 (line,col) 处于点号上下文且类型层就绪时返回成员补全。
// ok=false 表示不适用（非点号 / 无类型层 / 坐标映射失败），调用方回退符号表。
func (s *Session) typedCompletion(line, col int) (CompletionList, bool) {
	if s.timp == nil || s.goSrc == "" || s.smap == nil {
		return CompletionList{}, false
	}
	ls := s.lines()
	if line < 0 || line >= len(ls) || col <= 0 || col > len(ls[line]) || ls[line][col-1] != '.' {
		return CompletionList{}, false
	}
	gp, ok := s.smap.GxToGo(line+1, col) // 点号位置（1 基）
	if !ok {
		return CompletionList{}, false
	}
	off := goByteOffset(s.goSrc, gp.Line, gp.Column)
	if off < 0 || off >= len(s.goSrc) || s.goSrc[off] != '.' {
		return CompletionList{}, false
	}
	cands := s.timp.Complete(s.goSrc, off+1)
	list := CompletionList{IsIncomplete: false, Items: make([]CompletionItem, 0, len(cands))}
	for _, c := range cands {
		list.Items = append(list.Items, CompletionItem{Label: c.Name, Kind: lspKind(c.Kind), Detail: c.Signature})
	}
	return list, true
}

// goByteOffset 把生成源码的 1 基行列换成字节偏移。
func goByteOffset(src string, line, col int) int {
	if line < 1 || col < 1 {
		return -1
	}
	off := 0
	for i := 0; i < line-1; i++ {
		j := strings.IndexByte(src[off:], '\n')
		if j < 0 {
			return -1
		}
		off += j + 1
	}
	return off + col - 1
}

// lspKind 内部 Kind → LSP CompletionItemKind。
func lspKind(k string) int {
	switch k {
	case "method":
		return 2
	case "function":
		return 3
	case "field":
		return 5
	case "variable":
		return 6
	case "package":
		return 9
	case "constant":
		return 21
	case "type":
		return 22
	}
	return 6
}

// DocumentSymbols 返回全部符号（供大纲/跳转列表使用）。
func (s *Session) DocumentSymbols() []DocumentSymbol {
	sm := s.symbolMap()
	out := make([]DocumentSymbol, 0, len(sm.Symbols))
	for _, sym := range sm.Symbols {
		r := Range{
			Start: Position{Line: sym.Line, Character: sym.Col},
			End:   Position{Line: sym.Line, Character: sym.Col + len(sym.Name)},
		}
		out = append(out, DocumentSymbol{
			Name: sym.Name, Kind: sym.Kind, Range: r, SelectionRange: r, Detail: sym.Detail,
		})
	}
	return out
}

// Format 返回 gox fmt 格式化后的源码；无变化返回原串。
func (s *Session) Format() string { return compiler.Format(s.src) }

// ---- JSON 门面：syscall/js 侧只需要传字符串，避免逐字段转换 ----

func marshal(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

// DiagnosticsJSON 返回诊断数组 JSON。
func (s *Session) DiagnosticsJSON() string { return marshal(s.Diagnostics()) }

// CompleteJSON 返回补全列表 JSON。
func (s *Session) CompleteJSON(line, col int) string { return marshal(s.CompleteAt(line, col)) }

// HoverJSON 返回 hover JSON（无符号时为 "null"）。
func (s *Session) HoverJSON(line, col int) string { return marshal(s.HoverAt(line, col)) }

// DefinitionJSON 返回定义位置 JSON（无符号时为 "null"）。
func (s *Session) DefinitionJSON(line, col int) string { return marshal(s.DefinitionAt(line, col)) }

// DocumentSymbolsJSON 返回文档符号 JSON。
func (s *Session) DocumentSymbolsJSON() string { return marshal(s.DocumentSymbols()) }

// inJSXTagOpen 判断光标是否紧跟在 `<` 后且标签名未闭合。
func inJSXTagOpen(src string, line, col int) bool {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	if line < 0 || line >= len(lines) {
		return false
	}
	s := lines[line]
	if col > len(s) {
		col = len(s)
	}
	for i := col - 1; i >= 0; i-- {
		switch s[i] {
		case '<':
			return true
		case '>', ' ', '\t':
			return false
		}
	}
	return false
}

func isComponentName(name string) bool {
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}

func isComponentKind(kind int) bool { return kind == 12 || kind == 5 || kind == 10 }

// SignatureAt 在光标最近非空白为 '(' 或 ',' 且类型层就绪时，解析所在调用的
// 签名帮助；其余情形返回 nil（客户端隐藏提示，行为零变化）。坐标 LSP 约定 0 基。
func (s *Session) SignatureAt(line, col int) *SignatureHelp {
	if s.timp == nil || s.goSrc == "" || s.smap == nil {
		return nil
	}
	ls := s.lines()
	if line < 0 || line >= len(ls) || col <= 0 || col > len(ls[line]) {
		return nil
	}
	k := col
	for k > 0 && (ls[line][k-1] == ' ' || ls[line][k-1] == '\t') {
		k--
	}
	if k == 0 {
		return nil
	}
	if ch := ls[line][k-1]; ch != '(' && ch != ',' {
		return nil
	}
	gp, ok := s.smap.GxToGo(line+1, k)
	if !ok {
		return nil
	}
	off := goByteOffset(s.goSrc, gp.Line, gp.Column)
	if off < 0 || off >= len(s.goSrc) {
		return nil
	}
	if ch := s.goSrc[off]; ch != '(' && ch != ',' {
		return nil
	}
	cs, ok := s.timp.Signature(s.goSrc, off+1)
	if !ok {
		return nil
	}
	si := SignatureInformation{Label: cs.Label}
	for _, rg := range cs.Params {
		si.Parameters = append(si.Parameters, ParameterInformation{Label: rg})
	}
	return &SignatureHelp{Signatures: []SignatureInformation{si}, ActiveParameter: cs.Active}
}

// SignatureJSON 返回签名帮助 JSON；不适用时为 null（客户端按空处理）。
func (s *Session) SignatureJSON(line, col int) string { return marshal(s.SignatureAt(line, col)) }
