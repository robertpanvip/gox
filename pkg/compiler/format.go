package compiler

import "strings"

// Format 格式化 GX 源码。
//
// GX 源码 = Go 语法 + 内嵌 JSX 表达式。Go 部分原样保留（编译产物由 gofmt
// 保证规范，源码中的 Go 片段交由开发者/编辑器按 Go 习惯书写）；JSX 区域
// 用与编译器同一个 jsxParser 解析后按统一规则重排：
//
//   - 属性列表过长时逐行排列，闭合 '>' 独占一行；
//   - 子元素逐行缩进，短元素（仅一个文本/插值子节点且宽度允许）保持单行；
//   - if/else 条件分支按 Go 风格缩进；
//   - lambda 内的 JSX（For 的 `{(t) => <span>…</span>}`）递归格式化；
//   - 多行纯空白文本（排版缩进）丢弃，非空文本折叠规则与 normalizeText 一致。
//
// 格式化只改排版，不改语义：属性/插值的表达式原文原样保留，文本折叠与
// 编译器共用 normalizeText 语义。
func Format(src string) string {
	toks := New(src).AllTokens()
	var b strings.Builder
	b.Grow(len(src))

	// lineIndent 是当前行的前导空白宽度；JSX 内部的换行以它为基准缩进。
	// 不用 JSX token 自身的列号，避免 `return <div>` 把子节点推到列 12 之后。
	lineIndent := 0
	atLineStart := true
	for _, t := range toks {
		if t.Kind == EOF {
			break
		}
		if t.Kind == JSX {
			n := newJSXParser(t.Text, NoPos).parseElement()
			if n == nil {
				b.WriteString(t.Text)
			} else {
				writeJSXNode(&b, n, lineIndent)
			}
			atLineStart = false
			continue
		}
		b.WriteString(t.Text)
		switch t.Kind {
		case Newline:
			atLineStart = true
			lineIndent = 0
		case Space:
			if atLineStart {
				lineIndent += len(t.Text)
			}
		default:
			atLineStart = false
		}
	}
	return b.String()
}

const (
	jsxFormatWidth = 100 // 单行最大宽度（含前导缩进）
	jsxFormatStep  = 4   // 每级缩进空格
)

func jsxSpaces(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}

// writeJSXNode 打印一个元素。调用方保证当前输出位置已位于该元素的起始列
// （行首缩进已写，或 `return ` 已写），因此第一行不额外补缩进；内部换行以
// indent 为基准。
func writeJSXNode(b *strings.Builder, n *jsxNode, indent int) {
	attrs := make([]string, len(n.attrs))
	for i, a := range n.attrs {
		attrs[i] = jsxFormatAttr(a, indent+jsxFormatStep)
	}
	openOne := "<" + n.name
	for _, a := range attrs {
		openOne += " " + a
	}
	openOne += ">"

	// 空元素（含 <br/>）：自闭合，属性过长则逐行。
	if len(n.children) == 0 {
		jsxWriteOpenTag(b, n.name, attrs, indent, true, openOne)
		return
	}

	// 短元素：仅文本/插值子节点、无换行、总宽允许 → 单行。
	single := true
	width := indent + len(openOne) + len("</"+n.name+">")
	for _, c := range n.children {
		if c.kind == childElement || c.kind == childCond || strings.ContainsAny(c.text, "\n\r") {
			single = false
			break
		}
		width += len(jsxFormatChildOneLine(c))
	}
	if width > jsxFormatWidth {
		single = false
	}
	if single {
		b.WriteString(openOne)
		for _, c := range n.children {
			b.WriteString(jsxFormatChildOneLine(c))
		}
		b.WriteString("</" + n.name + ">")
		return
	}

	jsxWriteOpenTag(b, n.name, attrs, indent, false, openOne)
	for i := 0; i < len(n.children); {
		c := n.children[i]
		// 排版缩进产生的纯空白文本节点丢弃，避免留下只有空格的空行。
		if c.kind == childText && jsxFormatText(c.text) == "" {
			i++
			continue
		}
		b.WriteString("\n")
		if c.kind == childText || c.kind == childExpr {
			// 连续的文本/插值合并为一行（如 `Hello, {name}!`），
			// 既不改变语义，又避免每个词独占一行。
			j, line := i, ""
			for j < len(n.children) {
				nc := n.children[j]
				if nc.kind != childText && nc.kind != childExpr {
					break
				}
				piece := jsxFormatChildOneLine(nc)
				if strings.ContainsAny(piece, "\n\r") ||
					line != "" && indent+jsxFormatStep+len(line)+len(piece) > jsxFormatWidth {
					break
				}
				line += piece
				j++
			}
			if j > i {
				b.WriteString(jsxSpaces(indent + jsxFormatStep))
				b.WriteString(line)
				i = j
				continue
			}
			// j == i：首个子节点自身就无法并入一行（单行结果含换行，或
			// 超过 jsxFormatWidth）。此时 line 为空且 j 未推进；若直接
			// i = j 会原地打转，反复解析同一个插值表达式直到耗尽内存。
			// 落到下面走 jsxWriteChild 分支，由它输出该子节点并推进 i。
		}
		jsxWriteChild(b, c, indent+jsxFormatStep)
		i++
	}
	b.WriteString("\n")
	b.WriteString(jsxSpaces(indent))
	b.WriteString("</" + n.name + ">")
}

// jsxWriteOpenTag 打印开始标签。selfClosing 为 true 时输出 `/>`。
func jsxWriteOpenTag(b *strings.Builder, name string, attrs []string, indent int, selfClosing bool, openOne string) {
	tail := ">"
	if selfClosing {
		tail = "/>"
	}
	if len(attrs) == 0 || indent+len(openOne) <= jsxFormatWidth {
		// openOne 以 '>' 结尾，替换成 tail。
		b.WriteString(openOne[:len(openOne)-1])
		b.WriteString(tail)
		return
	}
	b.WriteString("<" + name)
	for _, a := range attrs {
		b.WriteString("\n")
		b.WriteString(jsxSpaces(indent + jsxFormatStep))
		b.WriteString(a)
	}
	b.WriteString("\n")
	b.WriteString(jsxSpaces(indent))
	b.WriteString(tail)
}

func jsxFormatAttr(a jsxAttr, indent int) string {
	switch a.kind {
	case attrBool:
		return a.name
	case attrString:
		return a.name + "=" + a.value
	case attrExpr:
		v := jsxFormatExpr(a.value, indent)
		return a.name + "={" + jsxReindentExpr(v, indent) + "}"
	}
	return a.name
}

// jsxReindentExpr 把多行插值表达式（典型是 style={{...}} 对象字面量）的内部
// 缩进按属性的新列号重排：首行保持，后续行以 propIndent 为基准，保留相对层级。
// 单行表达式原样返回。这样属性被拆到新行后，对象内部不会沿用旧的缩进列。
func jsxReindentExpr(expr string, propIndent int) string {
	if !strings.ContainsAny(expr, "\n\r") {
		return expr
	}
	lines := strings.Split(strings.ReplaceAll(expr, "\r\n", "\n"), "\n")
	const big = 1 << 30
	minIndent := big
	for i := 1; i < len(lines); i++ {
		trimmed := strings.TrimLeft(lines[i], " \t")
		if trimmed == "" {
			continue
		}
		lead := len(lines[i]) - len(trimmed)
		if lead < minIndent {
			minIndent = lead
		}
	}
	if minIndent == big {
		minIndent = 0
	}
	var b strings.Builder
	b.WriteString(lines[0])
	for i := 1; i < len(lines); i++ {
		trimmed := strings.TrimLeft(lines[i], " \t")
		b.WriteString("\n")
		if trimmed == "" {
			continue
		}
		lead := len(lines[i]) - len(trimmed)
		rel := lead - minIndent
		if rel < 0 {
			rel = 0
		}
		b.WriteString(jsxSpaces(propIndent + rel))
		b.WriteString(trimmed)
	}
	return b.String()
}

func jsxFormatChildOneLine(c jsxChild) string {
	switch c.kind {
	case childText:
		return jsxFormatText(c.text)
	case childExpr:
		return "{" + jsxFormatExpr(c.text, 0) + "}"
	}
	return ""
}

func jsxWriteChild(b *strings.Builder, c jsxChild, indent int) {
	b.WriteString(jsxSpaces(indent))
	switch c.kind {
	case childElement:
		writeJSXNode(b, c.node, indent)
	case childExpr:
		b.WriteString("{")
		b.WriteString(jsxFormatExpr(c.text, indent))
		b.WriteString("}")
	case childText:
		b.WriteString(jsxFormatText(c.text))
	case childCond:
		jsxWriteCond(b, c.cond, indent)
	}
}

// jsxWriteCond 按 Go 风格打印 if / else if / else。首行缩进由调用方写好，
// 后续分支与首行 `if` 对齐（`} else {`）。
func jsxWriteCond(b *strings.Builder, c *jsxCond, indent int) {
	if c == nil {
		return
	}
	for i, br := range c.branches {
		if i > 0 {
			b.WriteString(" ")
		}
		switch {
		case i == 0:
			b.WriteString("if " + br.test + " {")
		case br.test == "":
			b.WriteString("else {")
		default:
			b.WriteString("else if " + br.test + " {")
		}
		for _, ch := range br.children {
			b.WriteString("\n")
			jsxWriteChild(b, ch, indent+jsxFormatStep)
		}
		b.WriteString("\n")
		b.WriteString(jsxSpaces(indent))
		b.WriteString("}")
	}
}

// jsxFormatText 复刻 normalizeText 的折叠语义：多行纯空白丢弃，多行文本只
// 去首尾空白（内部换行与缩进原样保留，避免改变编译产物）。
func jsxFormatText(s string) string {
	if strings.ContainsAny(s, "\n\r") {
		if strings.TrimSpace(s) == "" {
			return ""
		}
		return strings.TrimSpace(s)
	}
	return s
}

// jsxFormatExpr 格式化插值表达式：整体是一个 JSX、或 lambda 的 `=>` 后是
// 一个 JSX 时递归排版；其余表达式原样保留（避免改动 Go 代码语义）。
func jsxFormatExpr(expr string, indent int) string {
	s := strings.TrimSpace(expr)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "<") {
		if n, ok := jsxSingleExpr(s); ok {
			var b strings.Builder
			writeJSXNode(&b, n, indent)
			return b.String()
		}
	}
	if i := strings.Index(s, "=>"); i >= 0 {
		rest := strings.TrimSpace(s[i+2:])
		if strings.HasPrefix(rest, "<") {
			if n, ok := jsxSingleExpr(rest); ok {
				var b strings.Builder
				b.WriteString(strings.TrimSpace(s[:i]))
				b.WriteString(" => ")
				writeJSXNode(&b, n, indent)
				return b.String()
			}
		}
	}
	return expr
}

// jsxSingleExpr 判断 s 是否是「恰好一个 JSX 元素」，是则返回解析树。
func jsxSingleExpr(s string) (*jsxNode, bool) {
	toks := New(s).AllTokens()
	if len(toks) == 2 && toks[0].Kind == JSX {
		return newJSXParser(toks[0].Text, NoPos).parseElement(), true
	}
	return nil, false
}
