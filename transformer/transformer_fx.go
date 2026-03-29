package transformer

import (
	"fmt"
	"strings"

	"github.com/gox-lang/gox/ast"
)

// FxStateVar 状态变量
type FxStateVar struct {
	Name      string
	Type      string
	Value     string
	IsState   bool  // 是否是内部状态（let 声明）
	IsProp    bool  // 是否是 props（从参数来）
	StateType string  // State 的内部类型，如 "int", "string"
}

// FxDependency 依赖关系
type FxDependency struct {
	VarName     string   // 状态变量名
	UsedIn      []string // 在哪些组件中使用
	MutatedIn   []string // 在哪些事件处理器中被修改
	NeedsUpdate bool     // 是否需要自动触发更新
	IsProp      bool     // 是否是 props
}

// transformFxFunc 转换 FX 函数为 lit-html 风格的组件
func (t *Transformer) transformFxFunc(f *ast.FuncDecl) string {
	var sb strings.Builder
	
	// 组件名总是首字母大写（导出的组件）
	componentName := strings.Title(f.Name)
	
	// 1. 收集状态变量（let 声明）
	stateVars := t.collectStateVars(f.Body)
	
	// 2. 分析 TSX 中的依赖
	dependencies := t.analyzeDependencies(f.Body, stateVars)
	
	// 3. 生成组件结构体
	sb.WriteString(t.generateFxComponentStruct(componentName, stateVars, dependencies))
	sb.WriteString("\n\n")
	
	// 4. 生成构造函数（带状态修改检测）
	sb.WriteString(t.generateFxConstructorWithMutationCheck(f, componentName, stateVars, dependencies))
	
	return sb.String()
}

// transformTSXWithMutationCheck 为 FX 组件转换 TSX 元素（带状态修改检测）
func (t *Transformer) transformTSXWithMutationCheck(tsx *ast.TSXElement, context string, stateVars []FxStateVar) string {
	// 使用现有的 TSX 转换逻辑，但需要特殊处理事件处理器
	// 当检测到事件处理器中修改了状态变量时，自动插入 c.RequestUpdate()
	
	// 1. 首先检查 TSX 中的事件处理器
	t.checkTSXForMutations(tsx, stateVars)
	
	// 2. 转换 TSX 为普通 Go 代码（带状态变量信息）
	goCode := t.transformExprWithStateCheck(tsx, stateVars)
	
	return goCode
}

// transformExprWithStateCheck 转换表达式（带状态检查，用于 FX 组件）
func (t *Transformer) transformExprWithStateCheck(expr ast.Expr, stateVars []FxStateVar) string {
	switch e := expr.(type) {
	case *ast.TSXElement:
		return t.transformTSXElementWithStateCheck(e, stateVars)
	default:
		// 其他表达式使用普通转换
		return t.transformExpr(expr)
	}
}

// transformTSXElementWithStateCheck 转换 TSX 元素（带状态检查）
func (t *Transformer) transformTSXElementWithStateCheck(e *ast.TSXElement, stateVars []FxStateVar) string {
	// 复用现有的 transformExpr 逻辑，但特殊处理事件处理器
	componentName := t.mapTSXTagsToComponent(e.TagName)
	propsTypeName := fmt.Sprintf("%sProps", componentName)
	
	// 检查是否有 "style" 属性
	var styleValue string
	propsFields := make([]string, 0)
	
	for _, attr := range e.Attributes {
		if attr.Name == "style" {
			if tmpl, ok := attr.Value.(*ast.TemplateString); ok {
				styleValue = t.transformStyleObject(tmpl)
			}
		} else {
			// 特殊处理事件处理器
			if strings.HasPrefix(attr.Name, "on") {
				// 事件处理器不添加到 propsFields 中，会在后面通过 setter 方法处理
				continue
			} else {
				// 普通属性，使用状态感知转换
				fieldName := strings.Title(attr.Name)
				fieldValue := t.transformExprWithStatePrefix(attr.Value, stateVars, "c.")
				propsFields = append(propsFields, fmt.Sprintf("%s: %s", fieldName, fieldValue))
			}
		}
	}
	
	// 构建 props
	propsStr := ""
	guiPrefix := "gui."  // Props 类型和构造函数都在 gui 包中
	if styleValue != "" {
		// 有 style 属性，使用 style 作为第一个参数
		propsStr = styleValue
		// 如果还有其他 props，需要创建一个包含 style 和其他字段的 props 结构
		if len(propsFields) > 0 {
			// 将 style 转换为字段添加到 props 结构中
			// 对于 Div 组件，需要创建 DivProps{Style: &gui.Style{...}, OnClick: ...}
			allFields := append([]string{fmt.Sprintf("Style: %s", styleValue)}, propsFields...)
			propsStr = fmt.Sprintf("%s%s{%s}", guiPrefix, propsTypeName, strings.Join(allFields, ", "))
		}
	} else if len(propsFields) > 0 {
		propsStr = fmt.Sprintf("%s%s{%s}", guiPrefix, propsTypeName, strings.Join(propsFields, ", "))
	} else {
		propsStr = fmt.Sprintf("%s{}", guiPrefix+propsTypeName)
	}
	
	// 处理子元素（递归使用状态感知转换）
	childrenStr := ""
	if len(e.Children) > 0 {
		children := make([]string, 0)
		for _, child := range e.Children {
			if childTSX, ok := child.(*ast.TSXElement); ok {
				children = append(children, t.transformTSXElementWithStateCheck(childTSX, stateVars))
			} else {
				children = append(children, t.transformExpr(child))
			}
		}
		childrenStr = ", " + strings.Join(children, ", ")
	}
	
	// 生成构造函数调用
	constructorName := fmt.Sprintf("gui.New%s", componentName)
	result := fmt.Sprintf("%s(%s%s)", constructorName, propsStr, childrenStr)
	
	// 如果有事件处理器，需要生成额外的代码来设置它们
	// 对于 Button 组件，使用 SetOnClick 方法
	for _, attr := range e.Attributes {
		if strings.HasPrefix(attr.Name, "on") {
			if funcLit, ok := attr.Value.(*ast.FunctionLiteral); ok {
				// 生成 SetOnClick 调用（方法名总是包含 On）
				eventHandlerName := strings.TrimPrefix(attr.Name, "on")
				setterName := fmt.Sprintf("SetOn%s", eventHandlerName)
				handlerCode := t.transformEventHandler(funcLit, stateVars)
				result = fmt.Sprintf("func() *gui.%s { b := %s; b.%s(%s); return b }()", componentName, result, setterName, handlerCode)
			}
		}
	}
	
	return result
}

// transformEventHandler 转换事件处理器（带 c. 前缀和 RequestUpdate()）
func (t *Transformer) transformEventHandler(funcLit *ast.FunctionLiteral, stateVars []FxStateVar) string {
	var sb strings.Builder
	
	sb.WriteString("func() {\n")
	
	// 转换函数体，为状态变量添加 c. 前缀
	if funcLit.Body != nil {
		for _, stmt := range funcLit.Body.List {
			sb.WriteString(t.transformStmtWithStatePrefix(stmt, stateVars, "c."))
		}
	}
	
	// 在末尾添加 RequestUpdate()
	sb.WriteString("    c.RequestUpdate()\n")
	
	sb.WriteString("}")
	
	return sb.String()
}

// transformStmtWithStatePrefix 转换语句，为状态变量添加前缀
func (t *Transformer) transformStmtWithStatePrefix(stmt ast.Stmt, stateVars []FxStateVar, prefix string) string {
	var sb strings.Builder
	
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		// count = 1, count += 1 等
		if ident, ok := s.LHS.(*ast.Ident); ok {
			if containsStateVar(stateVars, ident.Name) {
				// 是状态变量，添加前缀（使用大写字段名）
				sb.WriteString(fmt.Sprintf("    %s%s = %s\n", prefix, strings.Title(ident.Name), t.transformExprWithStatePrefix(s.RHS, stateVars, prefix)))
			} else {
				// 不是状态变量，正常转换
				sb.WriteString(fmt.Sprintf("    %s = %s\n", s.LHS, t.transformExpr(s.RHS)))
			}
		}
		
	case *ast.ExprStmt:
		// count++ 等
		if unary, ok := s.X.(*ast.UnaryExpr); ok {
			if ident, ok := unary.X.(*ast.Ident); ok {
				if containsStateVar(stateVars, ident.Name) {
					// 是状态变量，添加前缀
					op := t.mapOp(unary.Op)
					// 使用大写的字段名
					fieldName := strings.Title(ident.Name)
					if unary.Post {
						sb.WriteString(fmt.Sprintf("    %s%s%s\n", prefix, fieldName, op))
					} else {
						sb.WriteString(fmt.Sprintf("    %s%s%s\n", prefix, op, fieldName))
					}
				} else {
					// 不是状态变量，使用状态前缀转换
					sb.WriteString(fmt.Sprintf("    %s\n", t.transformExprWithStatePrefix(s.X, stateVars, prefix)))
				}
			} else {
				sb.WriteString(fmt.Sprintf("    %s\n", t.transformExprWithStatePrefix(s.X, stateVars, prefix)))
			}
		} else {
			sb.WriteString(fmt.Sprintf("    %s\n", t.transformExprWithStatePrefix(s.X, stateVars, prefix)))
		}
		
	case *ast.BlockStmt:
		// 递归处理块中的语句
		for _, innerStmt := range s.List {
			sb.WriteString(t.transformStmtWithStatePrefix(innerStmt, stateVars, prefix))
		}
		
	case *ast.IfStmt:
		// if 语句
		sb.WriteString(fmt.Sprintf("    if %s {\n", t.transformExprWithStatePrefix(s.Cond, stateVars, prefix)))
		if s.Body != nil {
			for _, innerStmt := range s.Body.List {
				sb.WriteString(t.transformStmtWithStatePrefix(innerStmt, stateVars, prefix))
			}
		}
		if s.Else != nil {
			sb.WriteString("    } else {\n")
			if elseBody, ok := s.Else.(*ast.BlockStmt); ok {
				for _, innerStmt := range elseBody.List {
					sb.WriteString(t.transformStmtWithStatePrefix(innerStmt, stateVars, prefix))
				}
			}
			sb.WriteString("    }\n")
		} else {
			sb.WriteString("    }\n")
		}
		
	default:
		// 其他语句正常转换
		sb.WriteString("    " + t.transformStmt(stmt, false) + "\n")
	}
	
	return sb.String()
}

// transformExprWithStatePrefix 转换表达式，为状态变量添加前缀
func (t *Transformer) transformExprWithStatePrefix(expr ast.Expr, stateVars []FxStateVar, prefix string) string {
	switch e := expr.(type) {
	case *ast.Ident:
		if containsStateVar(stateVars, e.Name) {
			// 使用大写的字段名
			return prefix + strings.Title(e.Name)
		}
		return e.Name
		
	case *ast.BinaryExpr:
		left := t.transformExprWithStatePrefix(e.X, stateVars, prefix)
		right := t.transformExprWithStatePrefix(e.Y, stateVars, prefix)
		return fmt.Sprintf("%s %s %s", left, t.mapOp(e.Op), right)
		
	case *ast.UnaryExpr:
		x := t.transformExprWithStatePrefix(e.X, stateVars, prefix)
		op := t.mapOp(e.Op)
		if e.Post {
			return x + op
		}
		return op + x
		
	case *ast.CallExpr:
		// 函数调用，递归处理参数
		args := make([]string, len(e.Args))
		for i, arg := range e.Args {
			args[i] = t.transformExprWithStatePrefix(arg, stateVars, prefix)
		}
		funcName := t.transformExprWithStatePrefix(e.Fun, stateVars, prefix)
		return fmt.Sprintf("%s(%s)", funcName, strings.Join(args, ", "))
		
	case *ast.TemplateString:
		// 模板字符串：`Hello ${name}!`
		// Parts 是字符串部分，Exprs 是 ${} 中的表达式
		// 需要递归处理 Exprs 中的表达式
		
		// 创建新的表达式列表，对每个表达式添加状态前缀
		newExprs := make([]ast.Expr, len(e.Exprs))
		for i, exprPart := range e.Exprs {
			newExprs[i] = t.createStateAwareExpr(exprPart, stateVars, prefix)
		}
		
		// 使用普通 transformExpr 来生成最终的 Go 代码（fmt.Sprintf）
		// 但使用我们处理过的 Exprs
		newTemplate := &ast.TemplateString{Parts: e.Parts, Exprs: newExprs, P: e.P}
		return t.transformExpr(newTemplate)
		
	default:
		return t.transformExpr(expr)
	}
}

// createStateAwareExpr 创建状态感知的表达式（递归处理）
func (t *Transformer) createStateAwareExpr(expr ast.Expr, stateVars []FxStateVar, prefix string) ast.Expr {
	switch e := expr.(type) {
	case *ast.Ident:
		if containsStateVar(stateVars, e.Name) {
			// 是状态变量，返回带前缀的新标识符
			return &ast.Ident{Name: prefix + e.Name}
		}
		return e
		
	case *ast.BinaryExpr:
		// 递归处理左右操作数
		return &ast.BinaryExpr{
			X:  t.createStateAwareExpr(e.X, stateVars, prefix),
			Op: e.Op,
			Y:  t.createStateAwareExpr(e.Y, stateVars, prefix),
		}
		
	case *ast.UnaryExpr:
		// 递归处理操作数
		return &ast.UnaryExpr{
			Op:   e.Op,
			X:    t.createStateAwareExpr(e.X, stateVars, prefix),
			Post: e.Post,
		}
		
	default:
		// 其他表达式保持不变
		return expr
	}
}

// checkTSXForMutations 检查 TSX 中的事件处理器是否修改了状态
func (t *Transformer) checkTSXForMutations(tsx *ast.TSXElement, stateVars []FxStateVar) {
	for _, attr := range tsx.Attributes {
		// 检查事件处理器属性（onClick, onChange 等）
		if strings.HasPrefix(attr.Name, "on") {
			if funcLit, ok := attr.Value.(*ast.FunctionLiteral); ok {
				// 检查这个回调函数是否修改了状态
				if t.hasStateMutation(funcLit.Body, stateVars) {
					// 标记这个回调需要插入 RequestUpdate()
					// TODO: 在转换时处理
				}
			}
		}
	}
	
	// 递归检查子元素
	for _, child := range tsx.Children {
		if childTSX, ok := child.(*ast.TSXElement); ok {
			t.checkTSXForMutations(childTSX, stateVars)
		}
	}
}

// hasStateMutation 检查函数体中是否修改了状态变量
func (t *Transformer) hasStateMutation(body *ast.BlockStmt, stateVars []FxStateVar) bool {
	if body == nil {
		return false
	}
	
	for _, stmt := range body.List {
		if t.stmtMutatesState(stmt, stateVars) {
			return true
		}
	}
	
	return false
}

// stmtMutatesState 检查语句是否修改了状态变量
func (t *Transformer) stmtMutatesState(stmt ast.Stmt, stateVars []FxStateVar) bool {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		// 检查赋值语句的左边：count = 1, count += 1 等
		if ident, ok := s.LHS.(*ast.Ident); ok {
			return containsStateVar(stateVars, ident.Name)
		}
		
	case *ast.ExprStmt:
		// 检查表达式语句：count++
		if unary, ok := s.X.(*ast.UnaryExpr); ok {
			// 检查是否是后置自增或自减（Post = true 表示后置运算符）
			if unary.Post {
				if ident, ok := unary.X.(*ast.Ident); ok {
					return containsStateVar(stateVars, ident.Name)
				}
			}
		}
		
	case *ast.BlockStmt:
		// 递归检查块中的语句
		for _, innerStmt := range s.List {
			if t.stmtMutatesState(innerStmt, stateVars) {
				return true
			}
		}
		
	case *ast.IfStmt:
		// 检查 if 块
		if s.Body != nil && t.stmtMutatesState(s.Body, stateVars) {
			return true
		}
		// 检查 else 块
		if s.Else != nil {
			if elseBody, ok := s.Else.(*ast.BlockStmt); ok {
				if t.stmtMutatesState(elseBody, stateVars) {
					return true
				}
			}
		}
		
	case *ast.ForStmt:
		// 检查 for 循环体
		if s.Body != nil && t.stmtMutatesState(s.Body, stateVars) {
			return true
		}
	}
	
	return false
}

// containsStateVar 检查变量名是否在状态变量列表中
func containsStateVar(stateVars []FxStateVar, name string) bool {
	for _, sv := range stateVars {
		if sv.Name == name {
			return true
		}
	}
	return false
}

// collectStateVars 收集函数体中的状态变量（let 声明）
func (t *Transformer) collectStateVars(body *ast.BlockStmt) []FxStateVar {
	stateVars := make([]FxStateVar, 0)
	
	if body == nil {
		return stateVars
	}
	
	for _, stmt := range body.List {
		if varDecl, ok := stmt.(*ast.VarDecl); ok {
			varName := varDecl.Name
			varType := "interface{}"
			varValue := "nil"
			isState := true  // let 声明的都是状态
			isProp := false  // 不是 props
			
			if varDecl.Type != nil {
				varType = t.transformType(varDecl.Type)
			} else if varDecl.Value != nil {
				// 根据初始值推断类型
				varType = t.inferTypeFromExpr(varDecl.Value)
			}
			
			if varDecl.Value != nil {
				varValue = t.transformExpr(varDecl.Value)
			}
			
			stateVars = append(stateVars, FxStateVar{
				Name:      varName,
				Type:      varType,
				Value:     varValue,
				IsState:   isState,
				IsProp:    isProp,
				StateType: "",
			})
		}
	}
	
	return stateVars
}

// inferTypeFromExpr 根据表达式推断类型
func (t *Transformer) inferTypeFromExpr(expr ast.Expr) string {
	switch expr.(type) {
	case *ast.IntLit:
		return "int"
	case *ast.FloatLit:
		return "float64"
	case *ast.StringLit:
		return "string"
	case *ast.BoolLit:
		return "bool"
	default:
		return "interface{}"
	}
}

// analyzeDependencies 分析 TSX 中的变量依赖
func (t *Transformer) analyzeDependencies(body *ast.BlockStmt, stateVars []FxStateVar) []FxDependency {
	dependencies := make([]FxDependency, 0)
	
	// 为每个状态变量创建依赖记录
	for _, sv := range stateVars {
		dependencies = append(dependencies, FxDependency{
			VarName:   sv.Name,
			UsedIn:    make([]string, 0),
			MutatedIn: make([]string, 0),
			NeedsUpdate: false,
		})
	}
	
	if body == nil {
		return dependencies
	}
	
	// 遍历语句查找 return TSX
	for _, stmt := range body.List {
		if returnStmt, ok := stmt.(*ast.ReturnStmt); ok {
			if returnStmt.Result != nil {
				t.analyzeTSXForDependencies(returnStmt.Result, stateVars, &dependencies)
			}
		}
	}
	
	// 标记需要自动更新的变量
	for i := range dependencies {
		if len(dependencies[i].MutatedIn) > 0 {
			dependencies[i].NeedsUpdate = true
		}
	}
	
	return dependencies
}

// analyzeTSXForDependencies 分析 TSX 元素中的依赖
func (t *Transformer) analyzeTSXForDependencies(expr ast.Expr, stateVars []FxStateVar, deps *[]FxDependency) {
	switch e := expr.(type) {
	case *ast.TSXElement:
		// 分析属性和子元素
		for _, attr := range e.Attributes {
			if attr.Value != nil {
				t.analyzeExprForDependencies(attr.Value, stateVars, deps)
			}
		}
		for _, child := range e.Children {
			t.analyzeTSXForDependencies(child, stateVars, deps)
		}
	}
}

// analyzeExprForDependencies 分析表达式中的变量依赖
func (t *Transformer) analyzeExprForDependencies(expr ast.Expr, stateVars []FxStateVar, deps *[]FxDependency) {
	switch e := expr.(type) {
	case *ast.TemplateString:
		// 分析模板字符串中的表达式
		for _, subExpr := range e.Exprs {
			t.analyzeExprForDependencies(subExpr, stateVars, deps)
		}
		
	case *ast.FunctionLiteral:
		// 箭头函数，检查函数体中是否修改了状态变量
		if e.Body != nil {
			for _, stmt := range e.Body.List {
				t.checkMutationInStmt(stmt, stateVars, deps)
			}
		}
		
	case *ast.CallExpr:
		// 分析参数
		for _, arg := range e.Args {
			t.analyzeExprForDependencies(arg, stateVars, deps)
		}
		
	case *ast.Ident:
		// 检查是否是状态变量
		for i, sv := range stateVars {
			if e.Name == sv.Name {
				(*deps)[i].UsedIn = append((*deps)[i].UsedIn, "expression")
			}
		}
		
	case *ast.BinaryExpr:
		t.analyzeExprForDependencies(e.X, stateVars, deps)
		t.analyzeExprForDependencies(e.Y, stateVars, deps)
		
	case *ast.UnaryExpr:
		t.analyzeExprForDependencies(e.X, stateVars, deps)
	}
}

// checkMutationInStmt 检查语句中是否修改了状态变量
func (t *Transformer) checkMutationInStmt(stmt ast.Stmt, stateVars []FxStateVar, deps *[]FxDependency) {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		// 检查赋值语句的左边
		if ident, ok := s.LHS.(*ast.Ident); ok {
			for i, sv := range stateVars {
				if ident.Name == sv.Name {
					(*deps)[i].MutatedIn = append((*deps)[i].MutatedIn, "assignment")
				}
			}
		}
		
	case *ast.ExprStmt:
		// 检查表达式语句
		t.analyzeExprForDependencies(s.X, stateVars, deps)
		
	case *ast.VarDecl:
		// 变量声明
		if s.Value != nil {
			t.analyzeExprForDependencies(s.Value, stateVars, deps)
		}
	}
}

// generateFxComponentStruct 生成 FX 组件结构体
func (t *Transformer) generateFxComponentStruct(name string, stateVars []FxStateVar, deps []FxDependency) string {
	var sb strings.Builder
	
	sb.WriteString(fmt.Sprintf("// %s FX 组件（lit-html 风格）\n", name))
	sb.WriteString(fmt.Sprintf("type %s struct {\n", name))
	sb.WriteString("    gui.BaseFxComponent\n")
	sb.WriteString("    \n")
	
	// 添加状态变量字段
	sb.WriteString("    // 状态变量\n")
	for _, sv := range stateVars {
		sb.WriteString(fmt.Sprintf("    %s %s\n", strings.Title(sv.Name), sv.Type))
	}
	
	sb.WriteString("    \n")
	sb.WriteString("    // 静态组件（创建一次）\n")
	sb.WriteString("    rootComponent gui.Component\n")
	sb.WriteString("    \n")
	sb.WriteString("    // 动态部分（可更新）\n")
	sb.WriteString("    dynamicParts []gui.TemplatePart\n")
	sb.WriteString("}")
	
	return sb.String()
}

// generateFxConstructorWithMutationCheck 生成 FX 组件构造函数（带状态修改检测）
func (t *Transformer) generateFxConstructorWithMutationCheck(f *ast.FuncDecl, componentName string, stateVars []FxStateVar, deps []FxDependency) string {
	var sb strings.Builder
	
	// 构造函数签名
	sb.WriteString(fmt.Sprintf("// New%s 创建 %s 组件\n", componentName, componentName))
	sb.WriteString(fmt.Sprintf("func New%s(", componentName))
	
	// 添加函数参数（props）
	params := make([]string, 0)
	for _, param := range f.Params {
		paramName := param.Name
		paramType := t.transformType(param.Type)
		params = append(params, fmt.Sprintf("%s %s", paramName, paramType))
	}
	
	if len(params) > 0 {
		sb.WriteString(strings.Join(params, ", "))
	}
	
	sb.WriteString(") *")
	sb.WriteString(componentName)
	sb.WriteString(" {\n")
	
	t.indent++
	indentStr := strings.Repeat("    ", t.indent)
	
	// 1. 创建组件实例
	sb.WriteString(fmt.Sprintf("%sc := &%s{\n", indentStr, componentName))
	t.indent++
	innerIndent := strings.Repeat("    ", t.indent)
	
	// 初始化状态变量
	for _, sv := range stateVars {
		sb.WriteString(fmt.Sprintf("%s%s: %s,\n", innerIndent, strings.Title(sv.Name), sv.Value))
	}
	
	t.indent--
	sb.WriteString(fmt.Sprintf("%s}\n", indentStr))
	sb.WriteString("\n")
	
	// 2. 生成 TSX 渲染代码（带状态修改检测）
	if f.Body != nil {
		for _, stmt := range f.Body.List {
			if returnStmt, ok := stmt.(*ast.ReturnStmt); ok {
				if returnStmt.Result != nil {
					if tsx, ok := returnStmt.Result.(*ast.TSXElement); ok {
						// 生成 TSX 组件创建代码（带状态修改检测）
						sb.WriteString(fmt.Sprintf("%s// 创建根组件\n", indentStr))
						sb.WriteString(fmt.Sprintf("%sc.rootComponent = %s\n", indentStr, t.transformTSXWithMutationCheck(tsx, "c", stateVars)))
						sb.WriteString("\n")
					}
				}
			}
		}
	}
	
	// 3. 创建动态部分
	sb.WriteString(fmt.Sprintf("%s// 创建动态部分\n", indentStr))
	sb.WriteString(fmt.Sprintf("%sc.dynamicParts = make([]gui.TemplatePart, 0)\n", indentStr))
	
	for _, dep := range deps {
		if len(dep.UsedIn) > 0 {
			sb.WriteString(fmt.Sprintf("%sc.dynamicParts = append(c.dynamicParts, gui.NewTextPart(nil, func() string {\n", indentStr))
			sb.WriteString(fmt.Sprintf("%s    return fmt.Sprintf(\"%%v\", c.%s)\n", indentStr, strings.Title(dep.VarName)))
			sb.WriteString(fmt.Sprintf("%s}))\n", indentStr))
		}
	}
	sb.WriteString("\n")
	
	// 4. 设置模板结果
	sb.WriteString(fmt.Sprintf("%sc.SetTemplateResult(&gui.TemplateResult{\n", indentStr))
	sb.WriteString(fmt.Sprintf("%s    StaticParts: []gui.Component{c.rootComponent},\n", indentStr))
	sb.WriteString(fmt.Sprintf("%s    DynamicParts: c.dynamicParts,\n", indentStr))
	sb.WriteString(fmt.Sprintf("%s})\n", indentStr))
	sb.WriteString("\n")
	
	// 5. 返回组件
	sb.WriteString(fmt.Sprintf("%sreturn c\n", indentStr))
	
	t.indent--
	sb.WriteString("}\n")
	
	return sb.String()
}
