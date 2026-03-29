package transformer

import (
	"fmt"
	"strings"

	"github.com/gox-lang/gox/ast"
)

// FxStateVar 状态变量
type FxStateVar struct {
	Name  string
	Type  string
	Value string
}

// FxDependency 依赖关系
type FxDependency struct {
	VarName     string   // 状态变量名
	UsedIn      []string // 在哪些组件中使用
	MutatedIn   []string // 在哪些事件处理器中被修改
}

// transformFxFunc 转换 FX 函数为 lit-html 风格的组件
func (t *Transformer) transformFxFunc(f *ast.FuncDecl) string {
	var sb strings.Builder
	
	componentName := f.Name
	if f.Visibility.Public {
		componentName = strings.Title(f.Name)
	} else {
		componentName = strings.ToLower(f.Name)
	}
	
	// 1. 收集状态变量（let 声明）
	stateVars := t.collectStateVars(f.Body)
	
	// 2. 分析 TSX 中的依赖
	dependencies := t.analyzeDependencies(f.Body, stateVars)
	
	// 3. 生成组件结构体
	sb.WriteString(t.generateFxComponentStruct(componentName, stateVars, dependencies))
	sb.WriteString("\n\n")
	
	// 4. 生成构造函数
	sb.WriteString(t.generateFxConstructor(f, componentName, stateVars, dependencies))
	
	return sb.String()
}

// transformTSXForFx 为 FX 组件转换 TSX 元素
func (t *Transformer) transformTSXForFx(tsx *ast.TSXElement, context string, stateVars []FxStateVar, deps []FxDependency) string {
	// 使用现有的 TSX 转换逻辑
	// 这里需要复用 transformer_expr.go 中的 transformExpr 方法
	// 但由于是私有方法，我们需要重新实现或导出
	
	// 简化版本：直接调用 transformExpr
	return t.transformExpr(tsx)
}

// collectStateVars 收集函数体中的状态变量（let 声明）
func (t *Transformer) collectStateVars(body *ast.BlockStmt) []FxStateVar {
	stateVars := make([]FxStateVar, 0)
	
	if body == nil {
		return stateVars
	}
	
	for _, stmt := range body.List {
		if varDecl, ok := stmt.(*ast.VarDecl); ok {
			// 收集 let 声明的变量
			varName := varDecl.Name
			varType := "interface{}"
			varValue := "nil"
			
			if varDecl.Type != nil {
				varType = t.transformType(varDecl.Type)
			}
			
			if varDecl.Value != nil {
				varValue = t.transformExpr(varDecl.Value)
			}
			
			stateVars = append(stateVars, FxStateVar{
				Name:  varName,
				Type:  varType,
				Value: varValue,
			})
		}
	}
	
	return stateVars
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
		
	case *ast.CallExpr:
		// 检查是否是事件处理器（如 onClick）
		if ident, ok := e.Fun.(*ast.Ident); ok {
			if ident.Name == "RequestUpdate" {
				// 找到了 RequestUpdate() 调用，需要分析前面的语句
				// 这里简化处理，假设所有状态变量都可能被修改
				for i := range *deps {
					(*deps)[i].MutatedIn = append((*deps)[i].MutatedIn, "event_handler")
				}
			}
		}
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

// generateFxConstructor 生成 FX 组件构造函数
func (t *Transformer) generateFxConstructor(f *ast.FuncDecl, componentName string, stateVars []FxStateVar, deps []FxDependency) string {
	var sb strings.Builder
	
	// 构造函数签名
	sb.WriteString(fmt.Sprintf("// New%s 创建 %s 组件\n", componentName, componentName))
	sb.WriteString(fmt.Sprintf("func New%s(", componentName))
	
	// 添加函数参数
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
	
	// 2. 生成 TSX 渲染代码
	if f.Body != nil {
		for _, stmt := range f.Body.List {
			if returnStmt, ok := stmt.(*ast.ReturnStmt); ok {
				if returnStmt.Result != nil {
					if tsx, ok := returnStmt.Result.(*ast.TSXElement); ok {
						// 生成 TSX 组件创建代码
						sb.WriteString(fmt.Sprintf("%s// 创建根组件\n", indentStr))
						sb.WriteString(fmt.Sprintf("%sc.rootComponent = %s\n", indentStr, t.transformTSXForFx(tsx, "c", stateVars, deps)))
						sb.WriteString("\n")
						
						// 生成动态部分
						sb.WriteString(fmt.Sprintf("%s// 创建动态部分\n", indentStr))
						sb.WriteString(fmt.Sprintf("%sc.dynamicParts = t.make([]gui.TemplatePart, 0)\n", indentStr))
						
						// 为每个依赖的状态变量创建更新函数
						for i, dep := range deps {
							if len(dep.UsedIn) > 0 {
								sb.WriteString(fmt.Sprintf("%sc.dynamicParts = append(c.dynamicParts, gui.NewTextPart(nil, func() string {\n", indentStr))
								sb.WriteString(fmt.Sprintf("%s    return fmt.Sprintf(\"%%v\", c.%s)\n", indentStr, strings.Title(dep.VarName)))
								sb.WriteString(fmt.Sprintf("%s}))\n", indentStr))
								_ = i // avoid unused variable warning
							}
						}
						sb.WriteString("\n")
					}
				}
			}
		}
	}
	
	// 3. 设置模板结果
	sb.WriteString(fmt.Sprintf("%sc.SetTemplateResult(&gui.TemplateResult{\n", indentStr))
	sb.WriteString(fmt.Sprintf("%s    StaticParts: []gui.Component{c.rootComponent},\n", indentStr))
	sb.WriteString(fmt.Sprintf("%s    DynamicParts: c.dynamicParts,\n", indentStr))
	sb.WriteString(fmt.Sprintf("%s})\n", indentStr))
	sb.WriteString("\n")
	
	// 4. 返回组件
	sb.WriteString(fmt.Sprintf("%sreturn c\n", indentStr))
	
	t.indent--
	sb.WriteString("}\n")
	
	return sb.String()
}
