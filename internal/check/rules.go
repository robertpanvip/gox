package check

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"github.com/robertpanvip/gox/pkg/compiler"
)

// =============================================================================
// 表达式类型推断
// =============================================================================

// exprType 递归推断表达式的 GX 类型文本；无法判定时返回空串（放过，避免误报）。
// 与旧 literalType 不同，它支持：括号、类型断言、标识符（查 side table 收窄后类型）、
// 函数调用返回、一元/二元运算结果。
func (v *visitor) exprType(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.BasicLit:
		switch x.Kind {
		case token.INT:
			return "int"
		case token.STRING:
			return "string"
		case token.FLOAT:
			return "float64"
		case token.CHAR:
			return "rune"
		}
	case *ast.Ident:
		switch x.Name {
		case "true", "false":
			return "bool"
		case "nil":
			return "nil"
		}
		// 变量：查 side table（含收窄），单类型才返回；联合/未知放过。
		line, col, _ := v.gxPos(x.Pos())
		t, ok := v.c.an.TypeAtGx(x.Name, line, col)
		if ok && !t.IsUnion() && t.Name != "unknown" && t.Name != "any" && t.Name != "never" {
			return t.Name
		}
	case *ast.UnaryExpr:
		switch x.Op {
		case token.NOT:
			return "bool"
		case token.SUB, token.ADD:
			return v.exprType(x.X) // -1 / +1 保持原类型
		}
	case *ast.ParenExpr:
		return v.exprType(x.X)
	case *ast.TypeAssertExpr:
		if x.Type != nil {
			return typeExprName(x.Type)
		}
	case *ast.CallExpr:
		// 已知函数的单返回值类型
		if id, ok := x.Fun.(*ast.Ident); ok {
			if sig, ok := v.c.an.FuncSig(id.Name); ok && len(sig.Results) == 1 {
				r := sig.Results[0]
				if !r.IsUnion() && r.Name != "unknown" {
					return r.Name
				}
			}
		}
	case *ast.BinaryExpr:
		if isCompareOp(x.Op) {
			return "bool"
		}
		lt, rt := v.exprType(x.X), v.exprType(x.Y)
		if lt != "" && lt == rt {
			return lt
		}
	case *ast.StarExpr:
		// 指针解引用：类型文本前带 * 的参数无法简单推断，放过
		_ = x
	}
	return ""
}

// typeExprName 把 AST 类型表达式还原为 GX 类型文本。
func typeExprName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		if p, ok := x.X.(*ast.Ident); ok {
			return p.Name + "." + x.Sel.Name
		}
	case *ast.StarExpr:
		if n := typeExprName(x.X); n != "" {
			return "*" + n
		}
	case *ast.InterfaceType:
		if x.Methods == nil || len(x.Methods.List) == 0 {
			return "any"
		}
	case *ast.ArrayType:
		if x.Len == nil {
			return "[]" + typeExprName(x.Elt)
		}
	}
	return ""
}

func isCompareOp(op token.Token) bool {
	switch op {
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ, token.LAND, token.LOR:
		return true
	}
	return false
}

// typeUnionReport 生成"类型不在联合成员内"的诊断消息。
// what 描述目标（如"变量 x"），union 为声明的联合类型。
func typeUnionReport(what, typeText string, union compiler.Type) string {
	return fmt.Sprintf("类型 %s 不能赋给%s（联合类型 %s）", typeText, what, union.String())
}

// =============================================================================
// 规则 1：联合赋值 / 初始化兼容性（union-assign）
// =============================================================================

func (v *visitor) checkValueSpec(vs *ast.ValueSpec) {
	if len(vs.Values) == 0 {
		return
	}
	for i, name := range vs.Names {
		if i >= len(vs.Values) {
			break
		}
		line, col, _ := v.gxPos(name.Pos())
		lhs, ok := v.c.an.TypeAtGx(name.Name, line, col)
		if !ok || !lhs.IsUnion() {
			continue
		}
		rhs := v.exprType(vs.Values[i])
		if rhs == "" || lhs.Has(rhs) {
			continue
		}
		v.report(vs.Values[i].Pos(), "union-assign",
			typeUnionReport(fmt.Sprintf("变量 %s", name.Name), rhs, lhs))
	}
}

func (v *visitor) checkAssign(as *ast.AssignStmt) {
	if as.Tok != token.ASSIGN || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return
	}
	id, ok := as.Lhs[0].(*ast.Ident)
	if !ok {
		return
	}
	line, col, _ := v.gxPos(id.Pos())
	lhs, ok := v.c.an.TypeAtGx(id.Name, line, col)
	if !ok || !lhs.IsUnion() {
		return
	}
	rhs := v.exprType(as.Rhs[0])
	if rhs == "" || lhs.Has(rhs) {
		return
	}
	v.report(as.Rhs[0].Pos(), "union-assign",
		typeUnionReport(fmt.Sprintf("变量 %s", id.Name), rhs, lhs))
}

// =============================================================================
// 规则 2：函数调用实参（union-call-arg）
// =============================================================================

func (v *visitor) checkCall(c *ast.CallExpr) {
	var name string
	switch f := c.Fun.(type) {
	case *ast.Ident:
		name = f.Name
	case *ast.SelectorExpr:
		name = f.Sel.Name // 跨包/方法可能重名，尽力而为
	default:
		return
	}
	sig, ok := v.c.an.FuncSig(name)
	if !ok || len(sig.Params) == 0 {
		return
	}
	if len(c.Args) != len(sig.Params) {
		return
	}
	for i, p := range sig.Params {
		if !p.IsUnion() {
			continue
		}
		at := v.exprType(c.Args[i])
		if at == "" || p.Has(at) {
			continue
		}
		v.report(c.Args[i].Pos(), "union-call-arg",
			typeUnionReport(fmt.Sprintf("函数 %s 第 %d 个参数", name, i+1), at, p))
	}
}

// =============================================================================
// 规则 3：函数返回（union-return）
// =============================================================================

func (v *visitor) checkReturn(r *ast.ReturnStmt) {
	line, col, _ := v.gxPos(r.Pos())
	sig, ok := v.c.an.FuncSigAtGx(line, col)
	if !ok || len(sig.Results) == 0 {
		return
	}
	if len(r.Results) != len(sig.Results) {
		return // 多值返回直传等场景放过
	}
	for i, e := range r.Results {
		ret := sig.Results[i]
		if !ret.IsUnion() {
			continue
		}
		at := v.exprType(e)
		if at == "" || ret.Has(at) {
			continue
		}
		v.report(e.Pos(), "union-return",
			typeUnionReport(fmt.Sprintf("函数 %s 返回值", sig.Name), at, ret))
	}
}

// =============================================================================
// 规则 4：未收窄联合参与运算（union-operand）
// =============================================================================

func (v *visitor) checkBinary(b *ast.BinaryExpr) {
	// 只查算术/位运算；比较与逻辑运算放过（any == nil 等合法场景避免误报）
	switch b.Op {
	case token.ADD, token.SUB, token.MUL, token.QUO, token.REM,
		token.SHL, token.SHR, token.AND, token.OR, token.XOR, token.AND_NOT:
	default:
		return
	}
	reported := map[string]bool{}
	for _, operand := range []ast.Expr{b.X, b.Y} {
		id, ok := operand.(*ast.Ident)
		if !ok || reported[id.Name] {
			continue
		}
		line, col, _ := v.gxPos(id.Pos())
		t, ok := v.c.an.TypeAtGx(id.Name, line, col)
		if !ok || !t.IsUnion() {
			continue
		}
		reported[id.Name] = true
		v.report(id.Pos(), "union-operand", fmt.Sprintf(
			"联合类型变量 %s（%s）未收窄，不能参与 %s 运算；请先用 is 守卫收窄",
			id.Name, t.String(), b.Op))
	}
}

// =============================================================================
// 规则 5：type switch 穷尽性（non-exhaustive）
// =============================================================================

func (v *visitor) checkTypeSwitch(ts *ast.TypeSwitchStmt) {
	// 取被 switch 的变量：switch v := x.(type) / switch x.(type)
	var subject ast.Expr
	switch a := ts.Assign.(type) {
	case *ast.AssignStmt:
		if len(a.Rhs) == 1 {
			if ta, ok := a.Rhs[0].(*ast.TypeAssertExpr); ok && ta.Type == nil {
				subject = ta.X
			}
		}
	case *ast.ExprStmt:
		if ta, ok := a.X.(*ast.TypeAssertExpr); ok && ta.Type == nil {
			subject = ta.X
		}
	}
	id, ok := subject.(*ast.Ident)
	if !ok {
		return
	}
	line, col, _ := v.gxPos(id.Pos())
	t, ok := v.c.an.TypeAtGx(id.Name, line, col)
	if !ok || !t.IsUnion() {
		return
	}
	covered := map[string]bool{}
	hasDefault := false
	for _, stmt := range ts.Body.List {
		cc, ok := stmt.(*ast.CaseClause)
		if !ok {
			continue
		}
		if cc.List == nil { // default
			hasDefault = true
			continue
		}
		for _, e := range cc.List {
			if n := typeExprName(e); n != "" {
				covered[n] = true
			}
		}
	}
	if hasDefault {
		return // 显式 default 视为已处理
	}
	var missing []string
	for _, m := range t.Members {
		if !covered[m.String()] {
			missing = append(missing, m.String())
		}
	}
	if len(missing) > 0 {
		v.report(ts.Pos(), "non-exhaustive", fmt.Sprintf(
			"type switch 未覆盖 %s 的联合成员：%s",
			id.Name, strings.Join(missing, "|")))
	}
}
