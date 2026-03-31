package transformer

import (
	"fmt"
	"strings"

	"github.com/gox-lang/gox/ast"
)

// TransformerLitHTML lit-html 风格的转换器
type TransformerLitHTML struct {
	imports map[string]bool
}

// NewLitHTML 创建 lit-html 风格的转换器
func NewLitHTML() *TransformerLitHTML {
	return &TransformerLitHTML{
		imports: make(map[string]bool),
	}
}

// Transform 转换程序
func (t *TransformerLitHTML) Transform(prog *ast.Program) string {
	var sb strings.Builder

	// 写入 package
	if prog.Package != nil {
		sb.WriteString(fmt.Sprintf("package %s\n\n", prog.Package.Name))
	}

	// 收集导入
	t.collectImports(prog)

	// 输出导入
	sb.WriteString("import (\n")
	sb.WriteString("\t\"github.com/gox-lang/gox/gui\"\n")
	for path := range t.imports {
		sb.WriteString(fmt.Sprintf("\t%q\n", path))
	}
	sb.WriteString(")\n\n")

	// 转换 FX 函数（已废弃，保留函数签名以兼容旧代码）
	// FX 功能已移除，不再处理
	for _, decl := range prog.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			// 不再检查 IsFx，所有函数都按普通函数处理
			_ = fn
		}
	}

	return sb.String()
}

func (t *TransformerLitHTML) collectImports(prog *ast.Program) {
	for _, decl := range prog.Decls {
		if imp, ok := decl.(*ast.ImportDecl); ok {
			t.imports[imp.Path] = true
		}
	}
}

// TransformFxFunc 转换 FX 函数为 lit-html 风格（公开方法）
func (t *TransformerLitHTML) TransformFxFunc(f *ast.FuncDecl) string {
	return t.transformFxFunc(f)
}

// TransformFunc 转换普通函数为 lit-html 风格（支持 sig 关键字）
func (t *TransformerLitHTML) TransformFunc(f *ast.FuncDecl) string {
	return t.transformFuncWithSig(f)
}

func (t *TransformerLitHTML) transformFuncWithSig(f *ast.FuncDecl) string {
	var sb strings.Builder

	componentName := strings.Title(f.Name)

	// 生成函数签名
	sb.WriteString(fmt.Sprintf("// %s 组件函数\n", componentName))
	sb.WriteString(fmt.Sprintf("func %s() {\n", componentName))

	// 转换函数体（包括 sig 声明）
	if f.Body != nil {
		for _, stmt := range f.Body.List {
			sb.WriteString(t.transformStmt(stmt))
		}
	}

	sb.WriteString("}\n")

	return sb.String()
}

func (t *TransformerLitHTML) transformFxFunc(f *ast.FuncDecl) string {
	var sb strings.Builder

	componentName := strings.Title(f.Name)

	// 收集状态变量
	stateVars := t.collectStateVars(f.Body)

	// 生成函数签名
	sb.WriteString(fmt.Sprintf("// %s 组件函数\n", componentName))
	sb.WriteString(fmt.Sprintf("func %s() func() gui.TemplateResult {\n", componentName))

	// 状态变量作为闭包变量
	for _, sv := range stateVars {
		sb.WriteString(fmt.Sprintf("\t%s := %s\n", sv.Name, sv.Value))
	}

	// 返回组件函数
	sb.WriteString("\treturn func() gui.TemplateResult {\n")

	// 转换 TSX
	if f.Body != nil {
		returnStmt := t.findReturnStmt(f.Body)
		if returnStmt != nil {
			if tsx, ok := returnStmt.Result.(*ast.TSXElement); ok {
				sb.WriteString(t.transformTSX(tsx, stateVars))
			}
		}
	}

	sb.WriteString("\t}\n")
	sb.WriteString("}\n")

	return sb.String()
}

func (t *TransformerLitHTML) collectStateVars(block *ast.BlockStmt) []StateVar {
	vars := make([]StateVar, 0)

	if block == nil {
		return vars
	}

	for _, stmt := range block.List {
		if varDecl, ok := stmt.(*ast.VarDecl); ok {
			vars = append(vars, StateVar{
				Name:  varDecl.Name,
				Value: t.transformExpr(varDecl.Value),
			})
		}
	}

	return vars
}

func (t *TransformerLitHTML) findReturnStmt(block *ast.BlockStmt) *ast.ReturnStmt {
	for _, stmt := range block.List {
		if ret, ok := stmt.(*ast.ReturnStmt); ok {
			return ret
		}
	}
	return nil
}

func (t *TransformerLitHTML) transformExpr(expr ast.Expr) string {
	if expr == nil {
		return ""
	}

	switch e := expr.(type) {
	case *ast.IntLit:
		return fmt.Sprintf("%d", e.Value)
	case *ast.StringLit:
		return fmt.Sprintf("%q", e.Value)
	case *ast.Ident:
		return e.Name
	default:
		return "nil"
	}
}

func (t *TransformerLitHTML) transformTSX(tsx *ast.TSXElement, stateVars []StateVar) string {
	var sb strings.Builder

	// 提取动态值
	dynamicValues := t.extractDynamicValues(tsx)

	// StaticCode
	sb.WriteString(fmt.Sprintf("\t\treturn gui.TemplateResult{\n"))
	sb.WriteString(fmt.Sprintf("\t\t\tStaticCode: `<%s>`,\n", tsx.TagName))

	// Dynamic 数组
	if len(dynamicValues) > 0 {
		sb.WriteString(fmt.Sprintf("\t\t\tDynamic: []interface{}{%s},\n", strings.Join(dynamicValues, ", ")))
	} else {
		sb.WriteString("\t\t\tDynamic: []interface{}{},\n")
	}

	// Factory 函数
	sb.WriteString("\t\t\tFactory: func() (gui.Component, []gui.Part) {\n")

	// 创建 Parts
	partCount := len(dynamicValues)
	for i := 0; i < partCount; i++ {
		sb.WriteString(fmt.Sprintf("\t\t\t\tcomment%d := gui.NewComment(\"dynamic-%d\")\n", i, i))
		sb.WriteString(fmt.Sprintf("\t\t\t\tpart%d := gui.NewTextPart(comment%d)\n", i, i))
	}

	// 创建组件
	sb.WriteString(t.createComponent(tsx, partCount))

	// 返回
	if partCount > 0 {
		parts := make([]string, partCount)
		for i := 0; i < partCount; i++ {
			parts[i] = fmt.Sprintf("part%d", i)
		}
		sb.WriteString(fmt.Sprintf("\t\t\t\treturn root, []gui.Part{%s}\n", strings.Join(parts, ", ")))
	} else {
		sb.WriteString("\t\t\t\treturn root, []gui.Part{}\n")
	}

	sb.WriteString("\t\t\t},\n")
	sb.WriteString("\t\t}\n")

	return sb.String()
}

func (t *TransformerLitHTML) extractDynamicValues(tsx *ast.TSXElement) []string {
	values := make([]string, 0)

	// 检查属性
	for _, attr := range tsx.Attributes {
		if attr.Value != nil {
			// 检查模板字符串
			if tmpl, ok := attr.Value.(*ast.TemplateString); ok {
				for _, expr := range tmpl.Exprs {
					values = append(values, t.transformExpr(expr))
				}
			} else {
				// 检查是否是标识符或其他表达式（非字符串字面量）
				if _, isStringLit := attr.Value.(*ast.StringLit); !isStringLit {
					values = append(values, t.transformExpr(attr.Value))
				}
			}
		}
	}

	// 检查子节点
	for _, child := range tsx.Children {
		// 检查模板字符串
		if tmpl, ok := child.(*ast.TemplateString); ok {
			for _, expr := range tmpl.Exprs {
				values = append(values, t.transformExpr(expr))
			}
		} else if child != nil {
			// 检查是否是标识符或其他表达式（非字符串字面量）
			if _, isStringLit := child.(*ast.StringLit); !isStringLit {
				values = append(values, t.transformExpr(child))
			}
		}
	}

	return values
}

func (t *TransformerLitHTML) createComponent(tsx *ast.TSXElement, partCount int) string {
	componentName := strings.Title(tsx.TagName)

	// 简单实现：创建组件
	return fmt.Sprintf("\t\t\t\troot := gui.New%s(gui.%sProps{})\n", componentName, componentName)
}

// transformStmt 转换语句（支持 sig 和 TSX）
func (t *TransformerLitHTML) transformStmt(stmt ast.Stmt) string {
	var sb strings.Builder
	
	switch s := stmt.(type) {
	case *ast.SigDecl:
		// sig count = 0  ->  count := gox.New(0)
		sb.WriteString(fmt.Sprintf("\t%s := gox.New(%s)\n", s.Name, t.transformExpr(s.Value)))
	case *ast.ReturnStmt:
		if s.Result != nil {
			if tsx, ok := s.Result.(*ast.TSXElement); ok {
				sb.WriteString(t.transformTSX(tsx, nil))
			} else {
				sb.WriteString(fmt.Sprintf("\treturn %s\n", t.transformExpr(s.Result)))
			}
		}
	default:
		// 其他语句使用普通转换
		sb.WriteString(fmt.Sprintf("\t// TODO: transform statement %T\n", stmt))
	}
	
	return sb.String()
}

// StateVar 状态变量
type StateVar struct {
	Name  string
	Value string
}
