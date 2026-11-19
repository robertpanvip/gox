package check

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"github.com/robertpanvip/gox/pkg/compiler"
)

// =============================================================================
// 表达式类型推断（值类型 + 无类型常量标记）
// =============================================================================

// valueType 是一个表达式的类型推断结果。
// Text 是类型文本；无类型常量（Go untyped constant）用内部标记 int-lit /
// float-lit / rune-lit / string-lit / bool-lit，配合 Untyped=true，
// 交给 compiler.Type.Accepts 按 Go 可赋值规则判定。
// Text 为空表示无法判定（放过，避免误报）。
type valueType struct {
	Text    string
	Untyped bool
}

func known(v valueType) bool { return v.Text != "" }

// exprType 递归推断表达式类型。env 为当前值环境（标识符 → 推断类型）。
func (v *visitor) exprType(e ast.Expr, env map[string]valueType) valueType {
	switch x := e.(type) {
	case *ast.BasicLit:
		switch x.Kind {
		case token.INT:
			return valueType{"int-lit", true}
		case token.FLOAT:
			return valueType{"float-lit", true}
		case token.STRING:
			return valueType{"string-lit", true}
		case token.CHAR:
			return valueType{"rune-lit", true}
		case token.IMAG:
			return valueType{"complex-lit", true}
		}
	case *ast.Ident:
		switch x.Name {
		case "true", "false":
			return valueType{"bool-lit", true}
		case "nil":
			return valueType{"nil", true}
		case "iota":
			return valueType{"int-lit", true}
		}
		// 值环境只用于无类型常量传播（const c = true），避免具类型值
		// 因扁平 env 泄漏（is 块内 shadow）而误判。
		if t, ok := env[x.Name]; ok && t.Untyped {
			return t
		}
		// 具类型值一律以 side table + 坐标为准（含 shadow 区间判定）。
		line, col, _ := v.gxPos(x.Pos())
		if t, ok := v.c.an.TypeAtGx(x.Name, line, col); ok && !t.IsUnion() {
			switch t.Name {
			case "unknown", "any", "never":
			default:
				return valueType{t.Name, false}
			}
		}
	case *ast.UnaryExpr:
		switch x.Op {
		case token.NOT:
			return valueType{"bool", false} // !x 结果具类型 bool
		case token.SUB:
			inner := v.exprType(x.X, env)
			// -<整数字面量> 标记为负整数字面量，不能赋给无符号类型
			if inner.Text == "int-lit" || inner.Text == "rune-lit" {
				return valueType{"neg-int-lit", true}
			}
			return inner
		case token.ADD, token.XOR:
			// +x / ^x 保持操作数类型族（无类型常量仍无类型）
			return v.exprType(x.X, env)
		case token.AND:
			// &x 是指针，无法简单推断类型文本
			return valueType{}
		}
	case *ast.ParenExpr:
		return v.exprType(x.X, env)
	case *ast.TypeAssertExpr:
		if x.Type != nil {
			return valueType{typeExprName(x.Type), false}
		}
	case *ast.CallExpr:
		// 类型转换 T(x)：T 是类型名且已知函数不是普通调用时，结果为 T
		if id, ok := x.Fun.(*ast.Ident); ok {
			if isBuiltinType(id.Name) && len(x.Args) == 1 {
				return valueType{id.Name, false}
			}
			if sig, ok := v.c.an.FuncSig(id.Name); ok && len(sig.Results) == 1 {
				r := sig.Results[0]
				if !r.IsUnion() && r.Name != "unknown" {
					return valueType{r.Name, false}
				}
			}
		}
	case *ast.BinaryExpr:
		if isCompareOp(x.Op) {
			return valueType{"bool", false}
		}
		lt, rt := v.exprType(x.X, env), v.exprType(x.Y, env)
		if !known(lt) || !known(rt) {
			return valueType{}
		}
		// 无类型常量参与运算：结果仍无类型，取更高优先级的操作数类型族。
		if lt.Untyped && rt.Untyped {
			return valueType{dominantLiteral(lt.Text, rt.Text), true}
		}
		if lt.Untyped && !rt.Untyped {
			return valueType{rt.Text, false}
		}
		if !lt.Untyped && rt.Untyped {
			return valueType{lt.Text, false}
		}
		if lt.Text == rt.Text {
			return valueType{lt.Text, false}
		}
	case *ast.IndexExpr:
		// 索引结果类型无法从文本简单推断（slice/array/map 元素），放过
	case *ast.StarExpr:
		// 指针解引用类型无法从文本推断，放过
	}
	return valueType{}
}

// dominantLiteral 返回两个无类型字面量运算后的类型族（优先级：complex > float > rune > int）。
func dominantLiteral(a, b string) string {
	rank := func(s string) int {
		switch s {
		case "complex-lit":
			return 5
		case "float-lit":
			return 4
		case "rune-lit":
			return 3
		case "int-lit", "neg-int-lit":
			return 2
		case "string-lit":
			return 1
		}
		return 0
	}
	if rank(a) >= rank(b) {
		return a
	}
	return b
}

func isBuiltinType(s string) bool {
	switch s {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"byte", "rune", "float32", "float64", "complex64", "complex128",
		"string", "bool", "any", "error":
		return true
	}
	return false
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

// =============================================================================
// 规则 1：联合赋值 / 初始化兼容性（union-assign）
// =============================================================================

func (v *visitor) checkValueSpec(vs *ast.ValueSpec) {
	// 先推断每个 RHS 的类型，供后续赋值检查与值环境记录。
	rhs := make([]valueType, len(vs.Values))
	for i, e := range vs.Values {
		rhs[i] = v.exprType(e, v.env)
	}

	// 逐名字检查：声明类型（联合）从 side table 按名字坐标取回（降级后 AST 是 any）。
	for i, name := range vs.Names {
		line, col, _ := v.gxPos(name.Pos())
		declared, hasDecl := v.c.an.TypeAtGx(name.Name, line, col)
		if i < len(rhs) && hasDecl && declared.IsUnion() {
			v.checkOneAssign(name.Pos(), name.Name, declared, rhs[i], vs.Values[i])
		}
		// 记录值环境：显式联合类型优先，否则用 RHS 推断（const 字面量保留 untyped）。
		if hasDecl && declared.IsUnion() {
			v.env[name.Name] = valueType{declared.String(), false}
		} else if i < len(rhs) && known(rhs[i]) {
			v.env[name.Name] = rhs[i]
		}
	}
}

func (v *visitor) checkAssign(as *ast.AssignStmt) {
	rhs := make([]valueType, len(as.Rhs))
	for i, e := range as.Rhs {
		rhs[i] = v.exprType(e, v.env)
	}

	// 逐项检查（支持多值赋值）。:=/= 都检查：= 时左侧联合来自 side table。
	for i, lhsExpr := range as.Lhs {
		if i >= len(rhs) {
			break
		}
		id, ok := lhsExpr.(*ast.Ident)
		if !ok {
			continue
		}
		line, col, _ := v.gxPos(id.Pos())
		t, ok := v.c.an.TypeAtGx(id.Name, line, col)
		if ok && t.IsUnion() {
			v.checkOneAssign(id.Pos(), id.Name, t, rhs[i], as.Rhs[i])
		}
		// 更新值环境：:= 新值类型；= 若推断不到则保留声明类型。
		if known(rhs[i]) {
			v.env[id.Name] = rhs[i]
		}
	}
}

// checkOneAssign 检查单个赋值是否符合联合类型，不符则报 union-assign。
func (v *visitor) checkOneAssign(pos token.Pos, name string, union compiler.Type, val valueType, expr ast.Expr) {
	if !known(val) || union.Accepts(val.Text, val.Untyped) {
		return
	}
	v.report(pos, "union-assign", fmt.Sprintf(
		"类型 %s 不能赋给变量 %s（联合类型 %s）", literalDisplay(val), name, union.String()))
}

func literalDisplay(v valueType) string {
	switch v.Text {
	case "int-lit":
		return "整数(int)"
	case "neg-int-lit":
		return "负整数"
	case "float-lit":
		return "浮点数(float64)"
	case "rune-lit":
		return "rune"
	case "string-lit":
		return "字符串(string)"
	case "bool-lit":
		return "bool"
	}
	return v.Text
}

// =============================================================================
// 规则 2：函数调用实参（union-call-arg）
// =============================================================================

func (v *visitor) checkCall(c *ast.CallExpr) {
	// 只检查本包直接调用 f(...)。selector 调用（跨包 pkg.f / 方法 obj.f）
	// 无法可靠解析到本包的联合签名——按名字匹配会误判同名函数，故跳过，
	// 类型安全交给 go build 与函数入口的运行时守卫。
	f, ok := c.Fun.(*ast.Ident)
	if !ok {
		return
	}
	name := f.Name
	sig, ok := v.c.an.FuncSig(name)
	if !ok || len(sig.Params) == 0 || len(c.Args) != len(sig.Params) {
		return
	}
	for i, p := range sig.Params {
		if !p.IsUnion() {
			continue
		}
		at := v.exprType(c.Args[i], v.env)
		if !known(at) || p.Accepts(at.Text, at.Untyped) {
			continue
		}
		v.report(c.Args[i].Pos(), "union-call-arg", fmt.Sprintf(
			"类型 %s 不能作为函数 %s 第 %d 个参数（联合类型 %s）",
			literalDisplay(at), name, i+1, p.String()))
	}
}

// =============================================================================
// 规则 3：函数返回（union-return）
// =============================================================================

func (v *visitor) checkReturn(r *ast.ReturnStmt) {
	line, col, _ := v.gxPos(r.Pos())
	sig, ok := v.c.an.FuncSigAtGx(line, col)
	if !ok || len(sig.Results) == 0 || len(r.Results) != len(sig.Results) {
		return
	}
	for i, e := range r.Results {
		ret := sig.Results[i]
		if !ret.IsUnion() {
			continue
		}
		at := v.exprType(e, v.env)
		if !known(at) || ret.Accepts(at.Text, at.Untyped) {
			continue
		}
		v.report(e.Pos(), "union-return", fmt.Sprintf(
			"类型 %s 不能作为函数 %s 返回值（联合类型 %s）",
			literalDisplay(at), sig.Name, ret.String()))
	}
}

// =============================================================================
// 规则 4：联合参与运算（union-operand）—— 一元 + 二元
// =============================================================================

// checkUnary 检查一元运算的操作数是否为未收窄联合。
func (v *visitor) checkUnary(u *ast.UnaryExpr) {
	switch u.Op {
	case token.ADD, token.SUB, token.XOR: // 数值运算要求操作数可运算
	default:
		return
	}
	v.checkOperand(u.X, u.Op.String())
}

func (v *visitor) checkBinary(b *ast.BinaryExpr) {
	if isCompareOp(b.Op) {
		return // 比较/逻辑运算放过（any == nil 等合法）
	}
	v.checkOperand(b.X, b.Op.String())
	v.checkOperand(b.Y, b.Op.String())
}

// checkOperand 检查一个操作数：解包括号后，若是标识符且为未收窄联合，则报。
// 关键：只认「desugar 真正注入了 shadow」的收窄——即 if 守卫块内。
// else 分支的分析层收窄没有对应 shadow，仍视为未收窄，避免 check 通过但 build 失败。
func (v *visitor) checkOperand(e ast.Expr, op string) {
	// 解包括号
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			break
		}
		e = p.X
	}
	id, ok := e.(*ast.Ident)
	if !ok {
		return
	}
	line, col, _ := v.gxPos(id.Pos())
	// TypeAtGx 内部用 TypeAtShadowed：只认真正注入了 shadow 的收窄区间。
	// then 块内、以及 else 排除法收窄到单一类型的块内返回具体类型（放过）；
	// else 剩余仍是联合（无法注入单断言）时返回联合（报），
	// 与 desugar 生成代码完全一致——避免 check 通过但 go build 失败。
	t, ok := v.c.an.TypeAtGx(id.Name, line, col)
	if !ok || !t.IsUnion() {
		return
	}
	v.report(id.Pos(), "union-operand", fmt.Sprintf(
		"联合类型变量 %s（%s）未收窄，不能参与 %s 运算；请先用 is 守卫收窄",
		id.Name, t.String(), op))
}

// =============================================================================
// 规则 5：type switch 穷尽性（non-exhaustive）
// =============================================================================

func (v *visitor) checkTypeSwitch(ts *ast.TypeSwitchStmt) {
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
		if cc.List == nil {
			hasDefault = true
			continue
		}
		for _, e := range cc.List {
			n := typeExprName(e)
			if n == "" {
				continue
			}
			// case 是联合别名（type MyU = int|string → 擦除为 any）：
			// 在 type switch 中 case MyU 等价 case any，覆盖全部成员。
			if at, ok := v.c.an.NamedType(n); ok && at.IsUnion() {
				hasDefault = true
				continue
			}
			covered[n] = true
		}
	}
	if hasDefault {
		return
	}
	var missing []string
	for _, m := range t.Members {
		if !covered[m.String()] {
			missing = append(missing, m.String())
		}
	}
	if len(missing) > 0 {
		v.report(ts.Pos(), "non-exhaustive", fmt.Sprintf(
			"type switch 未覆盖 %s 的联合成员：%s", id.Name, strings.Join(missing, "|")))
	}
}
