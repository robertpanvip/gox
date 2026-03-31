package transformer

import (
	"fmt"
	"strings"

	"github.com/gox-lang/gox/ast"
	"github.com/gox-lang/gox/token"
)

// TransformerTSX TSX 风格的转换器（TemplateResult）
type TransformerTSX struct {
	imports      map[string]bool
	transformer  *Transformer  // 引用主转换器，用于调用 transformExpr
	sigVars      map[string]bool  // Signal 变量追踪
}

// NewTSX 创建 TSX 风格的转换器
func NewTSX() *TransformerTSX {
	return &TransformerTSX{
		imports: make(map[string]bool),
		sigVars: make(map[string]bool),
	}
}

// SetTransformer 设置主转换器引用
func (t *TransformerTSX) SetTransformer(tr *Transformer) {
	t.transformer = tr
}

// Transform 转换程序
func (t *TransformerTSX) Transform(prog *ast.Program) string {
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

	return sb.String()
}

func (t *TransformerTSX) collectImports(prog *ast.Program) {
	for _, decl := range prog.Decls {
		if imp, ok := decl.(*ast.ImportDecl); ok {
			t.imports[imp.Path] = true
		}
	}
}

// TransformFunc 转换普通函数为 lit-html 风格（支持 sig 关键字）
func (t *TransformerTSX) TransformFunc(f *ast.FuncDecl) string {
	return t.transformFuncWithSig(f)
}

func (t *TransformerTSX) transformFuncWithSig(f *ast.FuncDecl) string {
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

func (t *TransformerTSX) transformTSX(tsx *ast.TSXElement, stateVars []StateVar) string {
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

func (t *TransformerTSX) extractDynamicValues(tsx *ast.TSXElement) []string {
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

func (t *TransformerTSX) createComponent(tsx *ast.TSXElement, partCount int) string {
	componentName := strings.Title(tsx.TagName)
	
	// 构建 Props 字段
	propsFields := make([]string, 0)
	
	// 添加事件处理器
	for _, attr := range tsx.Attributes {
		if strings.HasPrefix(strings.ToLower(attr.Name), "on") {
			// 事件处理器：onClick => OnClick: func() { ... }
			eventName := strings.Title(attr.Name)
			if fnLit, ok := attr.Value.(*ast.FunctionLiteral); ok {
				// 使用主转换器的 transformExpr 转换箭头函数
				// 先同步 sigVars 到主转换器
				if t.transformer != nil {
					// 同步 sigVars
					for sigVar := range t.sigVars {
						t.transformer.sigVars[sigVar] = true
					}
					fnCode := t.transformer.transformExpr(fnLit)
					propsFields = append(propsFields, fmt.Sprintf("%s: %s", eventName, fnCode))
				} else {
					// 如果没有主转换器，暂时记为 nil
					propsFields = append(propsFields, fmt.Sprintf("%s: nil", eventName))
				}
			}
		}
	}
	
	propsStr := "{}"
	if len(propsFields) > 0 {
		propsStr = fmt.Sprintf("{%s}", strings.Join(propsFields, ", "))
	}
	
	return fmt.Sprintf("\t\t\t\troot := gui.New%s(gui.%sProps%s)\n", componentName, componentName, propsStr)
}

// transformStmt 转换语句（支持 sig 和 TSX）
func (t *TransformerTSX) transformStmt(stmt ast.Stmt) string {
	var sb strings.Builder
	
	switch s := stmt.(type) {
	case *ast.SigDecl:
		// sig count = 0  ->  count := gox.New(0)
		// 记录 Signal 变量
		t.sigVars[s.Name] = true
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

// transformExpr 转换表达式（支持 Signal 变量）
func (t *TransformerTSX) transformExpr(expr ast.Expr) string {
	if expr == nil {
		return ""
	}

	switch e := expr.(type) {
	case *ast.IntLit:
		return fmt.Sprintf("%d", e.Value)
	case *ast.StringLit:
		return fmt.Sprintf("%q", e.Value)
	case *ast.Ident:
		// 检查是否是 Signal 变量，如果是则添加.Get()
		if t.sigVars[e.Name] {
			return fmt.Sprintf("%s.Get()", e.Name)
		}
		return e.Name
	case *ast.BinaryExpr:
		// 特殊处理赋值表达式：检查是否是 Signal 变量赋值
		if e.Op == token.ASSIGN {
			if ident, ok := e.X.(*ast.Ident); ok {
				if t.sigVars[ident.Name] {
					// count = count + 1  ->  count.Set(count.Get() + 1)
					return fmt.Sprintf("%s.Set(%s)", ident.Name, t.transformExpr(e.Y))
				}
			}
		}
		x := t.transformExpr(e.X)
		y := t.transformExpr(e.Y)
		op := t.mapOp(e.Op)
		return x + " " + op + " " + y
	default:
		return "nil"
	}
}

// mapOp 映射操作符
func (t *TransformerTSX) mapOp(op token.TokenKind) string {
	// 根据 token.go 中的定义映射
	switch op {
	case token.ASSIGN:
		return "="
	case token.PLUS:
		return "+"
	case token.MINUS:
		return "-"
	case token.STAR:
		return "*"
	case token.SLASH:
		return "/"
	case token.PERCENT:
		return "%"
	case token.LESS:
		return "<"
	case token.GREATER:
		return ">"
	case token.LESS_EQUAL:
		return "<="
	case token.GREATER_EQUAL:
		return ">="
	case token.EQUAL:
		return "=="
	case token.NOT_EQUAL:
		return "!="
	}
	return ""
}

// StateVar 状态变量
type StateVar struct {
	Name  string
	Value string
}
