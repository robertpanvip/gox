package compiler

import (
	"strconv"
	"strings"
)

// template.go — GX 模板字符串降级。
//
// GX 写法：`hello ${name}, score=${score}`（反引号内含 ${...} 插值）。
// 生成的 Go： "hello " + fmt.Sprint(name) + ", score=" + fmt.Sprint(score)
//
// 设计：
//   - lexer 把含 ${ 的反引号串标记为 Template token；不含的仍是 Go 原生 raw string；
//   - 静态段用 strconv.Quote 生成双引号字符串字面量（自动处理换行/引号转义）；
//   - 插值段递归交给 Lexer 重新词法分析，产出的 token 插入主流，因而仍能享受
//     外部符号还原 / 集合链式 / 联合擦除 / 嵌套模板等后续所有 transform；
//   - fmt 包名由 ImportManager 统一别名化（源码已 import fmt 则复用，否则注入）。
//
// 限制：暂不处理 `\${` 转义（把 \$ 视为字面反斜杠 + $），避免与 Go raw string
// 的语义打架；插值的花括号深度与字符串字面量会被正确跳过。

type tplPart struct {
	text   string
	isExpr bool
}

// transformTemplates 把 token 流里的 Template token 就地展开为字符串拼接 token。
//
// E2：ast 是 lex 后、原始流上构建的 TemplateAST。主流中的顶层模板 token 以
// Pos.Offset 命中节点（offset 稳定，token 下标因前面 transform 增删而漂移）；
// 命中时把节点的 TplBody 作为展开原文（语义与 token.Text 一致，但语义来源归一到 AST）。
// 递归展开内层模板时 ast 传 nil，内层模板复用父模板 Pos、无独立 offset，走 token 原文回退。
func transformTemplates(in []Token, b *Bindings, ast *TemplateAST) []Token {
	out := make([]Token, 0, len(in))
	for i, t := range in {
		if t.Kind != Template {
			out = append(out, t)
			continue
		}
		body := t.Text
		if node := ast.TemplateAt(t.Pos.Offset); node != nil {
			body = node.TplBody
		}
		out = append(out, expandTemplate(t, body, in, i, b)...)
	}
	return out
}

// expandTemplate 解析单个 Template token，返回展开后的 token 序列。
// body 是模板原文（主入口来自 AST 节点，递归来自 token 文本）；ctx/useIdx 为当前
// 模板在主流中的位置：对可静态判为 int 的整插值用 strconv.Itoa 替代 fmt.Sprint，
// 避免 fmt.Sprint 的反射装箱开销（实测约 2x）。递归展开嵌套模板时 ctx 传 nil。
func expandTemplate(t Token, body string, ctx []Token, useIdx int, b *Bindings) []Token {
	if len(body) >= 2 && body[0] == '`' && body[len(body)-1] == '`' {
		body = body[1 : len(body)-1]
	}
	parts := parseTemplateParts(body)

	// 每个"值段"的 token：静态段是字符串字面量，插值段是 fmt.Sprint(expr)。
	var segs [][]Token
	for _, part := range parts {
		if part.isExpr {
			inner := New(part.text).AllTokens()
			// 去掉 EOF，并递归展开嵌套模板。
			if len(inner) > 0 && inner[len(inner)-1].Kind == EOF {
				inner = inner[:len(inner)-1]
			}
			// 递归只展开嵌套模板；sig 改写留给外层在 Pos 统一重置后用正确
			// 的模板原始位置一次性完成（此处子串 lex 的 Pos 还是相对偏移）。
			inner = transformTemplates(inner, nil, nil)
			// 子 token 用模板起始位置，避免相对坐标污染 sourcemap。
			for i := range inner {
				inner[i].Pos = t.Pos
			}
			// 整数插值特化：能静态判为 int 时用 strconv.Itoa，避免 fmt.Sprint 的
			// 反射装箱（实测 2x 开销）。其余类型仍用 fmt.Sprint 保证通用。
			// 注意：必须在 sig 读改写之前判定——改写会把单 token 的 sig 名
			// 变成 name() 三 token，使 exprIsInt 失效、白白丢掉已有的 int 优化。
			pkgName, fnName := "fmt", "Sprint"
			if ctx != nil && exprIsInt(inner, ctx, useIdx, b) {
				pkgName, fnName = "strconv", "Itoa"
			}
			// sig 读改写补漏：transformSignals 先于 transformTemplates 执行，
			// 彼时 ${...} 还是整块 Template token，其内部 sig 引用未被识别。
			// 此处插值已展开为普通 token，按名字 + 模板位置补一次 name → name()，
			// 遮蔽/selector/字段键/已是调用等排除与主流改写保持一致。
			inner = rewriteSignalReadsInTemplateExpr(inner, b, t.Pos)
			seg := []Token{
				tokIdent(pkgName, t.Pos), tokOp(".", t.Pos),
				tokIdent(fnName, t.Pos), tokOp("(", t.Pos),
			}
			seg = append(seg, inner...)
			seg = append(seg, tokOp(")", t.Pos))
			segs = append(segs, seg)
		} else if part.text != "" {
			segs = append(segs, []Token{tokString(strconv.Quote(part.text), t.Pos)})
		}
	}

	// 用 + 连接所有值段；全静态时退化为单个字符串字面量。
	if len(segs) == 0 {
		return []Token{tokString(`""`, t.Pos)}
	}
	var out []Token
	for i, seg := range segs {
		if i > 0 {
			out = append(out, tokOp("+", t.Pos))
		}
		out = append(out, seg...)
	}
	return out
}

// exprIsInt 判断插值表达式 inner 是否可静态判定为 int。
// 支持：整数字面量、ctx 中已知为 int 的单个标识符、以及结果为 int 的简单字面量表达式。
func exprIsInt(inner []Token, ctx []Token, useIdx int, b *Bindings) bool {
	// 去掉 EOF
	toks := inner
	if len(toks) > 0 && toks[len(toks)-1].Kind == EOF {
		toks = toks[:len(toks)-1]
	}
	first := nextSig(toks, 0)
	if first < 0 {
		return false
	}
	last := prevSigIn(toks, len(toks)-1, 0)
	if last < 0 || last != first {
		return false // 多 token 表达式：保守不特化（除下方字面量表达式）
	}
	tk := toks[first]
	if tk.Kind == Number {
		return !strings.ContainsAny(tk.Text, ".eEpP")
	}
	if tk.Kind == Ident && !tk.is("true") && !tk.is("false") && !tk.is("nil") {
		// 在主流 ctx 里按名字查声明类型；useIdx 是模板位置。
		return lookupIdentType(b, ctx, tk.Text, useIdx) == "int"
	}
	return false
}

// parseTemplateParts 把模板文本切成静态段与 ${...} 插值段（交替，静态段可为空）。
func parseTemplateParts(body string) []tplPart {
	var parts []tplPart
	var lit strings.Builder
	for i := 0; i < len(body); {
		if body[i] == '$' && i+1 < len(body) && body[i+1] == '{' {
			parts = append(parts, tplPart{text: lit.String()})
			lit.Reset()
			exprStart := i + 2
			j := exprStart
			depth := 1
			for j < len(body) && depth > 0 {
				c := body[j]
				switch c {
				case '"', '\'', '`':
					q := c
					j++
					for j < len(body) {
						if q != '`' && body[j] == '\\' {
							j += 2
							continue
						}
						if body[j] == q {
							j++
							break
						}
						j++
					}
					continue
				case '{':
					depth++
				case '}':
					depth--
				}
				j++
			}
			if depth == 0 {
				parts = append(parts, tplPart{text: body[exprStart : j-1], isExpr: true})
				i = j
			} else {
				// 未闭合的 ${：当作静态文本，避免吞掉后续内容。
				lit.WriteString(body[i:])
				i = len(body)
			}
			continue
		}
		lit.WriteByte(body[i])
		i++
	}
	parts = append(parts, tplPart{text: lit.String()})
	return parts
}
