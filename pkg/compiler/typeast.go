package compiler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// typeast.go — 用 Go 官方 parser 作为复杂类型边界的交叉验证层。
//
// 背景（代码审查第二阶段 #9）：手写 token 扫描器 typeEnd 已覆盖 func/struct/
// interface/chan 等复杂类型，但「自己维护一套类型解析」始终有遗漏风险。
// 这里不把整个 GX 文件交给 go/parser（GX 含 union/JSX 等非 Go 语法，无法整体解析），
// 而是对 typeEnd 识别出的、即将提升为 list.List[T] 的「类型片段 T」用
// parser.ParseExpr 验证它确是一个合法 Go 类型表达式。
//
// 效果：若 typeEnd 切错边界（例如把 func 截断成 list.List[func](int)），
// AST 校验会失败，提升阶段据此放弃该次提升——宁可不提升，也不生成非法 Go。

// exprText 把 token 区间拼回源码文本（保留空白，供 parser 解析）。
func exprText(toks []Token, start, end int) string {
	var b strings.Builder
	for i := start; i < end && i < len(toks); i++ {
		b.WriteString(toks[i].Text)
	}
	return strings.TrimSpace(b.String())
}

// isValidGoTypeExpr 用 go/parser 判断文本是否为合法 Go 类型表达式。
//
// 不能用 parser.ParseExpr：它会把 int|string 当作合法的按位或「表达式」接受，
// 而那不是 Go 类型。这里把片段放进合成声明 `var _ <expr>` 的类型位置，
// parser 就会按类型语法校验，union 等非类型写法会被拒绝。
func isValidGoTypeExpr(text string) bool {
	if text == "" {
		return false
	}
	src := "package p\nvar _ " + text + "\n"
	_, err := parser.ParseFile(token.NewFileSet(), "", src, 0)
	return err == nil
}

// isSliceGoTypeExpr 判断文本是否为 Go 切片类型表达式（*ast.ArrayType 且 Len==nil）。
func isSliceGoTypeExpr(text string) bool {
	e, err := parser.ParseExpr(text)
	if err != nil {
		return false
	}
	at, ok := e.(*ast.ArrayType)
	return ok && at.Len == nil
}

// verifyTypeExprAt 校验 toks[start:end) 是否为合法 Go 类型表达式。
// 这是 promoteAllSlices 在提升前调用的守卫。
func verifyTypeExprAt(toks []Token, start, end int) bool {
	return isValidGoTypeExpr(exprText(toks, start, end))
}
