package transformer

import (
	"fmt"
	"strings"

	"github.com/gox-lang/gox/ast"
	"github.com/gox-lang/gox/token"
)

type Transformer struct {
	indent     int
	extendFuncs map[string][]*ast.FuncDecl
	imports   map[string]string  // package path -> source type ("go", "gox", or "")
	funcTypes map[string][]*ast.FuncParam  // Store function parameter types: "FuncName" -> [params]
}

func New() *Transformer {
	return &Transformer{
		indent:     0,
		extendFuncs: make(map[string][]*ast.FuncDecl),
		imports:   make(map[string]string),
		funcTypes: make(map[string][]*ast.FuncParam),
	}
}

func (t *Transformer) addImport(path, sourceType string) {
	t.imports[path] = sourceType
}

// parseTemplateString parses a string like "Hello, ${name}!" and returns format string and expressions
func (t *Transformer) parseTemplateString(s string) (format string, exprs []string) {
	format = ""
	exprs = make([]string, 0)
	
	content := s
	for {
		idx := strings.Index(content, "${")
		if idx == -1 {
			format += content
			break
		}
		
		// Escape % in the content before ${
		format += strings.ReplaceAll(content[:idx], "%", "%%")
		content = content[idx+2:]
		
		endIdx := strings.Index(content, "}")
		if endIdx == -1 {
			format += "${" + content
			break
		}
		
		exprStr := strings.TrimSpace(content[:endIdx])
		content = content[endIdx+1:]
		
		format += "%v"
		exprs = append(exprs, exprStr)
	}
	
	// Escape % in remaining content
	format += strings.ReplaceAll(content, "%", "%%")
	return
}

func (t *Transformer) Transform(prog *ast.Program) string {
var sb strings.Builder

// Write package clause first
if prog.Package != nil {
sb.WriteString(fmt.Sprintf("package %s\n\n", prog.Package.Name))
}

// First pass: collect function type information
for _, decl := range prog.Decls {
if fn, ok := decl.(*ast.FuncDecl); ok {
key := fn.Name
t.funcTypes[key] = fn.Params
}
}

// Second pass: transform
for _, decl := range prog.Decls {
switch d := decl.(type) {
case *ast.ImportDecl:
			sb.WriteString(fmt.Sprintf("import %s\n", d.Path))
			t.addImport(strings.Trim(d.Path, `"`), d.SourceType)
		case *ast.StructDecl:
			sb.WriteString(t.transformStruct(d))
			sb.WriteString("\n\n")
		case *ast.InterfaceDecl:
			sb.WriteString(t.transformInterface(d))
			sb.WriteString("\n\n")
		case *ast.FuncDecl:
			sb.WriteString(t.transformFunc(d))
			sb.WriteString("\n\n")
		case *ast.VarDecl:
			sb.WriteString(t.transformVarDecl(d))
			sb.WriteString("\n")
		case *ast.ConstDecl:
			sb.WriteString(t.transformConstDecl(d))
			sb.WriteString("\n")
		case *ast.ExtendDecl:
			if d.Type != nil {
				t.extendFuncs[d.Type.Name] = d.Methods
			}
		}
	}

	// Note: imports are already added in the first pass
	_ = t.imports // suppress unused field warning

	for typeName, methods := range t.extendFuncs {
		for _, method := range methods {
			genName := typeName + strings.Title(method.Name)
			method.Name = genName
			sb.WriteString(t.transformExtendFunc(method, typeName))
			sb.WriteString("\n\n")
		}
	}

	return sb.String()
}

func (t *Transformer) transformVarDecl(v *ast.VarDecl) string {
	var sb strings.Builder
	name := v.Name
	if v.Visibility.Public {
		name = strings.Title(name)
	} else if !v.Visibility.Private {
		name = strings.ToLower(name)
	}

	if v.Value != nil {
		sb.WriteString(fmt.Sprintf("%s := %s", name, t.transformExpr(v.Value)))
	} else {
		typ := "interface{}"
		if v.Type != nil {
			typ = t.transformType(v.Type)
		}
		sb.WriteString(fmt.Sprintf("var %s %s", name, typ))
	}
	return sb.String()
}

func (t *Transformer) transformConstDecl(c *ast.ConstDecl) string {
	var sb strings.Builder
	name := c.Name
	if c.Visibility.Public {
		name = strings.Title(name)
	} else if !c.Visibility.Private {
		name = strings.ToLower(name)
	}

	if c.Value != nil {
		if c.Type != nil {
			sb.WriteString(fmt.Sprintf("const %s %s = %s", name, t.transformType(c.Type), t.transformExpr(c.Value)))
		} else {
			sb.WriteString(fmt.Sprintf("const %s = %s", name, t.transformExpr(c.Value)))
		}
	}
	return sb.String()
}

func (t *Transformer) transformStruct(s *ast.StructDecl) string {
	var sb strings.Builder

	name := s.Name
	if s.Visibility.Public {
		name = strings.Title(name)
	} else {
		name = strings.ToLower(name)
	}

	// Add type parameters [T any, U any, ...]
	typeParams := ""
	if len(s.TypeParams) > 0 {
		var params []string
		for _, tp := range s.TypeParams {
			constraint := "any"
			if tp.Constraint != nil {
				constraint = t.transformType(tp.Constraint)
			}
			params = append(params, fmt.Sprintf("%s %s", tp.Name, constraint))
		}
		typeParams = "[" + strings.Join(params, ", ") + "]"
	}

	sb.WriteString(fmt.Sprintf("type %s%s struct {\n", name, typeParams))

	// Add embedded structs (mixed) first
	for _, mixed := range s.Mixed {
		mixedName := mixed.Name
		if mixedName == strings.ToLower(mixedName) {
			mixedName = strings.Title(mixedName)
		}
		sb.WriteString(fmt.Sprintf("    %s\n", mixedName))
	}

	// Add fields
	for _, field := range s.Fields {
		fieldName := field.Name
		if field.Visibility.Public {
			fieldName = strings.Title(fieldName)
		} else {
			fieldName = strings.ToLower(fieldName)
		}

		fieldType := t.transformType(field.Type)
		sb.WriteString(fmt.Sprintf("    %s %s\n", fieldName, fieldType))
	}

	sb.WriteString("}")
	return sb.String()
}

func (t *Transformer) transformInterface(i *ast.InterfaceDecl) string {
	var sb strings.Builder

	name := i.Name
	if i.Visibility.Public {
		name = strings.Title(name)
	} else {
		name = strings.ToLower(name)
	}

	sb.WriteString(fmt.Sprintf("type %s interface {\n", name))

	for _, method := range i.Methods {
		methodName := method.Name
		if method.Visibility.Public {
			methodName = strings.Title(methodName)
		} else {
			methodName = strings.ToLower(methodName)
		}

		// Build method signature
		params := make([]string, 0)
		for _, param := range method.Params {
			params = append(params, t.transformType(param.Type))
		}

		retType := ""
		if method.ReturnType != nil {
			retType = t.transformType(method.ReturnType)
		}

		if retType != "" {
			sb.WriteString(fmt.Sprintf("    %s(%s) %s\n", methodName, strings.Join(params, ", "), retType))
		} else {
			sb.WriteString(fmt.Sprintf("    %s(%s)\n", methodName, strings.Join(params, ", ")))
		}
	}

	sb.WriteString("}")
	return sb.String()
}

func (t *Transformer) transformFunc(f *ast.FuncDecl) string {
	var sb strings.Builder

	name := f.Name
	if f.Visibility.Public {
		// Special case: Main function in package main should be lowercase
		if f.Name == "Main" {
			name = "main"
		} else {
			name = strings.Title(name)
		}
	} else {
		name = strings.ToLower(name)
	}

	// Handle receiver (struct method)
	if f.Receiver != nil {
		// Go-style method with receiver: func (r ReceiverType) MethodName(...)
		sb.WriteString(fmt.Sprintf("func (%s %s) %s", 
			f.Receiver.Name, 
			t.transformType(f.Receiver.Type), 
			name))
	} else {
		// Regular function
		sb.WriteString(fmt.Sprintf("func %s", name))
	}

	// Add type parameters
	if len(f.TypeParams) > 0 {
		sb.WriteString("[")
		for i, tp := range f.TypeParams {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(tp.Name)
			if tp.Constraint != nil {
				constraint := t.transformType(tp.Constraint)
				sb.WriteString(fmt.Sprintf(" %s", constraint))
			} else {
				sb.WriteString(" any")
			}
		}
		sb.WriteString("]")
	}

	sb.WriteString("(")

	for i, param := range f.Params {
		if i > 0 {
			sb.WriteString(", ")
		}
		paramName := param.Name
		if !f.Visibility.Public && !f.Visibility.Private {
			paramName = strings.ToLower(paramName)
		}
		sb.WriteString(fmt.Sprintf("%s %s", paramName, t.transformType(param.Type)))
	}

	sb.WriteString(")")

	if f.ReturnType != nil {
		retType := t.transformType(f.ReturnType)
		if f.Throws {
			sb.WriteString(fmt.Sprintf(" (%s, error)", retType))
		} else {
			sb.WriteString(fmt.Sprintf(" %s", retType))
		}
	} else if f.Throws {
		sb.WriteString(" (error)")
	}

	sb.WriteString(" {\n")

	t.indent++
	if f.Body != nil {
		for _, stmt := range f.Body.List {
			sb.WriteString(t.transformStmt(stmt, f.Throws))
		}
	}
	t.indent--

	sb.WriteString("}")
	return sb.String()
}

func (t *Transformer) transformExtendFunc(f *ast.FuncDecl, typeName string) string {
	var sb strings.Builder

	name := typeName + strings.Title(f.Name)

	sb.WriteString(fmt.Sprintf("func %s(self %s", name, typeName))

	for i, param := range f.Params {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(fmt.Sprintf("%s %s", param.Name, t.transformType(param.Type)))
	}

	retType := ""
	if f.ReturnType != nil {
		retType = t.transformType(f.ReturnType)
	}

	if retType != "" {
		sb.WriteString(fmt.Sprintf(") %s {\n", retType))
	} else {
		sb.WriteString(") {\n")
	}

	t.indent++
	if f.Body != nil {
		for _, stmt := range f.Body.List {
			sb.WriteString(t.transformStmt(stmt, false))
		}
	}
	t.indent--

	sb.WriteString("}")
	return sb.String()
}

func (t *Transformer) transformStmt(stmt ast.Stmt, isFuncThrows bool) string {
	var sb strings.Builder
	indentStr := strings.Repeat("    ", t.indent)

	switch s := stmt.(type) {
	case *ast.ExprStmt:
		sb.WriteString(indentStr)
		sb.WriteString(t.transformExpr(s.X))
		sb.WriteString("\n")

	case *ast.VarDecl:
		// Keep variable name as-is (Go convention is camelCase for local variables)
		name := s.Name

		if s.Value != nil {
			// Use := for variable declaration with value
			valueStr := t.transformExpr(s.Value)
			sb.WriteString(fmt.Sprintf("%s%s := %s\n", indentStr, name, valueStr))
		} else {
			// Use var for variable declaration without value
			typ := "interface{}"
			if s.Type != nil {
				typ = t.transformType(s.Type)
			}
			sb.WriteString(fmt.Sprintf("%svar %s %s\n", indentStr, name, typ))
		}

	case *ast.ConstDecl:
		sb.WriteString(indentStr)
		name := s.Name
		if s.Visibility.Public {
			name = strings.Title(name)
		} else if !s.Visibility.Private {
			name = strings.ToLower(name)
		}

		if s.Value != nil {
			if s.Type != nil {
				sb.WriteString(fmt.Sprintf("const %s %s = %s\n", name, t.transformType(s.Type), t.transformExpr(s.Value)))
			} else {
				sb.WriteString(fmt.Sprintf("const %s = %s\n", name, t.transformExpr(s.Value)))
			}
		}

	case *ast.AssignStmt:
		sb.WriteString(indentStr)
		sb.WriteString(t.transformExpr(s.LHS))
		sb.WriteString(" = ")
		sb.WriteString(t.transformExpr(s.RHS))
		sb.WriteString("\n")

	case *ast.ReturnStmt:
		sb.WriteString(indentStr)
		sb.WriteString("return")
		if s.Result != nil {
			if isFuncThrows {
				if call, ok := s.Result.(*ast.CallExpr); ok {
					call.HasThrows = true
				}
			}
			sb.WriteString(" ")
			sb.WriteString(t.transformExpr(s.Result))
			if isFuncThrows {
				sb.WriteString(", nil")
			}
		}
		sb.WriteString("\n")

	case *ast.IfStmt:
		sb.WriteString(indentStr)
		sb.WriteString("if ")
		sb.WriteString(t.transformExpr(s.Cond))
		sb.WriteString(" {\n")
		t.indent++
		for _, stmt := range s.Body.List {
			sb.WriteString(t.transformStmt(stmt, isFuncThrows))
		}
		t.indent--
		sb.WriteString(indentStr)
		sb.WriteString("}")

		if s.Else != nil {
			if ifStmt, ok := s.Else.(*ast.IfStmt); ok {
				sb.WriteString(" else ")
				sb.WriteString(t.transformStmt(ifStmt, isFuncThrows))
			} else {
				sb.WriteString(" else {\n")
				t.indent++
				elseStmts := s.Else.(*ast.BlockStmt).List
				for _, stmt := range elseStmts {
					sb.WriteString(t.transformStmt(stmt, isFuncThrows))
				}
				t.indent--
				sb.WriteString(indentStr)
				sb.WriteString("}")
			}
		}
		sb.WriteString("\n")

	case *ast.ForStmt:
		sb.WriteString(indentStr)
		sb.WriteString("for ")
		if s.Cond != nil {
			sb.WriteString(t.transformExpr(s.Cond))
		}
		sb.WriteString(" {\n")
		t.indent++
		for _, stmt := range s.Body.List {
			sb.WriteString(t.transformStmt(stmt, isFuncThrows))
		}
		t.indent--
		sb.WriteString(indentStr)
		sb.WriteString("}\n")

	case *ast.WhileStmt:
		sb.WriteString(indentStr)
		sb.WriteString("for ")
		if s.Cond != nil {
			sb.WriteString(t.transformExpr(s.Cond))
		}
		sb.WriteString(" {\n")
		t.indent++
		for _, stmt := range s.Body.List {
			sb.WriteString(t.transformStmt(stmt, isFuncThrows))
		}
		t.indent--
		sb.WriteString(indentStr)
		sb.WriteString("}\n")

	case *ast.BreakStmt:
		sb.WriteString(indentStr)
		sb.WriteString("break\n")

	case *ast.ContinueStmt:
		sb.WriteString(indentStr)
		sb.WriteString("continue\n")

	case *ast.SwitchStmt:
		sb.WriteString(indentStr)
		sb.WriteString("switch ")
		if s.Cond != nil {
			sb.WriteString(t.transformExpr(s.Cond))
		}
		sb.WriteString(" {\n")
		t.indent++
		for _, c := range s.Cases {
			sb.WriteString(indentStr)
			if c.Cond != nil {
				sb.WriteString("case ")
				sb.WriteString(t.transformExpr(c.Cond))
				sb.WriteString(":\n")
			} else {
				sb.WriteString("default:\n")
			}
			t.indent++
			for _, stmt := range c.Body.List {
				sb.WriteString(t.transformStmt(stmt, isFuncThrows))
			}
			t.indent--
		}
		t.indent--
		sb.WriteString(indentStr)
		sb.WriteString("}\n")

	case *ast.WhenStmt:
		sb.WriteString(indentStr)
		sb.WriteString("switch ")
		if s.Cond != nil {
			sb.WriteString(t.transformExpr(s.Cond))
		}
		sb.WriteString(" {\n")
		t.indent++
		for _, c := range s.Cases {
			sb.WriteString(indentStr)
			sb.WriteString("case ")
			sb.WriteString(t.transformExpr(c.Cond))
			sb.WriteString(":\n")
			t.indent++
			for _, stmt := range c.Body.List {
				sb.WriteString(t.transformStmt(stmt, isFuncThrows))
			}
			t.indent--
		}
		t.indent--
		sb.WriteString(indentStr)
		sb.WriteString("}\n")

	case *ast.TryStmt:
		if s.CatchBlock != nil {
			sb.WriteString(indentStr)
			sb.WriteString("{\n")
			t.indent++
			sb.WriteString(indentStr)
			sb.WriteString("res, err := func() {\n")
			t.indent++
			for _, stmt := range s.TryBlock.List {
				sb.WriteString(t.transformStmt(stmt, false))
			}
			t.indent--
			sb.WriteString(indentStr)
			sb.WriteString("}()\n")
			sb.WriteString(indentStr)
			sb.WriteString("if err != nil {\n")
			t.indent++
			for _, stmt := range s.CatchBlock.List {
				sb.WriteString(t.transformStmt(stmt, false))
			}
			t.indent--
			sb.WriteString(indentStr)
			sb.WriteString("}\n")
			t.indent--
			sb.WriteString(indentStr)
			sb.WriteString("}\n")
		} else {
			for _, stmt := range s.TryBlock.List {
				sb.WriteString(t.transformStmt(stmt, isFuncThrows))
			}
		}

	case *ast.BlockStmt:
		sb.WriteString(indentStr)
		sb.WriteString("{\n")
		t.indent++
		for _, stmt := range s.List {
			sb.WriteString(t.transformStmt(stmt, isFuncThrows))
		}
		t.indent--
		sb.WriteString(indentStr)
		sb.WriteString("}\n")
	}

	return sb.String()
}

func (t *Transformer) transformType(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.BaseType:
		return e.Name
	case *ast.Ident:
		return e.Name
	case *ast.ArrayType:
		return "[]" + t.transformType(e.Element)
	case *ast.NullableType:
		return "*" + t.transformType(e.Element)
	case *ast.PointerType:
		return "*" + t.transformType(e.Base)
	case *ast.FuncType:
		params := make([]string, 0)
		for _, p := range e.Params {
			params = append(params, t.transformType(p.Type))
		}
		ret := ""
		if e.ReturnType != nil {
			ret = t.transformType(e.ReturnType)
		}
		return "func(" + strings.Join(params, ", ") + ")" + ret
	case *ast.ParenExpr:
		return "(" + t.transformType(e.X) + ")"
	case *ast.IndexExpr:
		// Handle generic types like Container[T]
		obj := t.transformType(e.X)
		index := t.transformType(e.Index)
		return obj + "[" + index + "]"
	case *ast.MemberExpr:
		// Handle pkg.Type syntax for struct literals
		obj := t.transformType(e.X)
		return obj + "." + e.Name
	default:
		return "interface{}"
	}
}

func (t *Transformer) transformExpr(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name

	case *ast.IntLit:
		return fmt.Sprintf("%d", e.Value)

	case *ast.FloatLit:
		return fmt.Sprintf("%v", e.Value)

	case *ast.StringLit:
		return fmt.Sprintf(`"%s"`, e.Value)

	case *ast.TemplateString:
		// Convert template string to fmt.Sprintf
		if len(e.Exprs) == 0 {
			return fmt.Sprintf(`"%s"`, strings.Join(e.Parts, ""))
		}
		
		format := ""
		args := make([]string, 0)
		for i, part := range e.Parts {
			if i < len(e.Exprs) {
				format += strings.ReplaceAll(part, "%", "%%") // Escape %
				format += "%v"
				args = append(args, t.transformExpr(e.Exprs[i]))
			} else {
				// Last part (after all expressions)
				format += strings.ReplaceAll(part, "%", "%%")
			}
		}
		return fmt.Sprintf(`fmt.Sprintf("%s", %s)`, format, strings.Join(args, ", "))

	case *ast.BoolLit:
		if e.Value {
			return "true"
		}
		return "false"

	case *ast.NilLit:
		return "nil"

	case *ast.CallExpr:
		// Get function parameter types for type inference
		fnName := ""
		if ident, ok := e.Fun.(*ast.Ident); ok {
			fnName = ident.Name
		}
		funcParams := t.funcTypes[fnName]
		
		args := make([]string, 0)
		for i, arg := range e.Args {
			// Check if this is an anonymous struct literal that needs type inference
			if sl, ok := arg.(*ast.StructLit); ok && sl.Type == nil && i < len(funcParams) {
				// Infer type from function parameter
				sl.Type = funcParams[i].Type
				args = append(args, t.transformExpr(sl))
			} else {
				args = append(args, t.transformExpr(arg))
			}
		}
		fn := t.transformExpr(e.Fun)
		
		// Handle print/println with template strings
		if fn == "print" || fn == "println" {
			// Check if any argument is a template string or string with ${...}
			hasTemplate := false
			for _, arg := range e.Args {
				if _, ok := arg.(*ast.TemplateString); ok {
					hasTemplate = true
					break
				}
				// Also check StringLit for ${...} pattern
				if sl, ok := arg.(*ast.StringLit); ok {
					if strings.Contains(sl.Value, "${") && strings.Contains(sl.Value, "}") {
						hasTemplate = true
						break
					}
				}
			}
			
			if hasTemplate {
				// Convert to fmt.Sprint/Sprintln with fmt.Sprintf for template strings
				fmtFunc := "fmt.Sprint"
				if fn == "println" {
					fmtFunc = "fmt.Sprintln"
				}
				
				finalArgs := make([]string, 0)
				for _, arg := range e.Args {
					if ts, ok := arg.(*ast.TemplateString); ok {
						// Transform template string to fmt.Sprintf
						if len(ts.Exprs) == 0 {
							finalArgs = append(finalArgs, fmt.Sprintf(`"%s"`, strings.Join(ts.Parts, "")))
						} else {
							format := ""
							tArgs := make([]string, 0)
							for i, part := range ts.Parts {
								if i < len(ts.Exprs) {
									format += strings.ReplaceAll(part, "%", "%%")
									format += "%v"
									tArgs = append(tArgs, t.transformExpr(ts.Exprs[i]))
								} else {
									// Last part (after all expressions)
									format += strings.ReplaceAll(part, "%", "%%")
								}
							}
							finalArgs = append(finalArgs, fmt.Sprintf(`fmt.Sprintf("%s", %s)`, format, strings.Join(tArgs, ", ")))
						}
					} else if sl, ok := arg.(*ast.StringLit); ok {
						// Check if StringLit contains ${...} pattern
						if strings.Contains(sl.Value, "${") && strings.Contains(sl.Value, "}") {
							// Parse and convert to fmt.Sprintf
							format, exprs := t.parseTemplateString(sl.Value)
							if len(exprs) == 0 {
								finalArgs = append(finalArgs, fmt.Sprintf(`"%s"`, sl.Value))
							} else {
								finalArgs = append(finalArgs, fmt.Sprintf(`fmt.Sprintf("%s", %s)`, format, strings.Join(exprs, ", ")))
							}
						} else {
							finalArgs = append(finalArgs, fmt.Sprintf(`"%s"`, sl.Value))
						}
					} else {
						finalArgs = append(finalArgs, t.transformExpr(arg))
					}
				}
				
				// Add fmt import
				t.addImport("fmt", "go")
				
				return fmtFunc + "(" + strings.Join(finalArgs, ", ") + ")"
			}
		}
		
		result := fn + "(" + strings.Join(args, ", ") + ")"

		if e.HasThrows || t.isFuncThrows(e.Fun) {
			parts := strings.SplitN(result, "(", 2)
			if len(parts) == 2 {
				return parts[0] + "Ret, err := " + parts[0] + "(" + parts[1]
			}
			return result
		}
		return result

	case *ast.FunctionLiteral:
		params := make([]string, 0)
		paramTypes := make([]string, 0)
		for _, p := range e.Params {
			params = append(params, p.Name)
			paramTypes = append(paramTypes, t.transformType(p.Type))
		}

		retType := ""
		if e.ReturnType != nil {
			retType = t.transformType(e.ReturnType)
		}

		if e.IsArrow && e.Body != nil && len(e.Body.List) == 1 {
			if retStmt, ok := e.Body.List[0].(*ast.ReturnStmt); ok {
				body := t.transformExpr(retStmt.Result)
				if retType != "" {
					return fmt.Sprintf("func(%s) %s { return %s }",
						formatFuncParams(params, paramTypes), retType, body)
				}
				return fmt.Sprintf("func(%s) { return %s }",
					formatFuncParams(params, paramTypes), body)
			}
		}

		t.indent++
		bodyStr := ""
		for _, stmt := range e.Body.List {
			bodyStr += t.transformStmt(stmt, false)
		}
		t.indent--

		if retType != "" {
			return fmt.Sprintf("func(%s) %s {\n%s}",
				formatFuncParams(params, paramTypes), retType, bodyStr)
		}
		return fmt.Sprintf("func(%s) {\n%s}",
			formatFuncParams(params, paramTypes), bodyStr)

	case *ast.MemberExpr:
		obj := t.transformExpr(e.X)
		if e.HasSafe {
			return fmt.Sprintf("safeDot(%s, \"%s\")", obj, e.Name)
		}
		
		// Check if this is a Go package call
		// If the object (package) is imported as "go", capitalize the function name
		sourceType, ok := t.imports[obj]
		if ok && sourceType == "go" {
			// Go package - capitalize function name
			return obj + "." + strings.Title(e.Name)
		}
		
		// For struct fields and methods, apply visibility transformation
		// Capitalize first letter for public access
		transformedName := strings.Title(e.Name)
		return obj + "." + transformedName

	case *ast.IndexExpr:
		obj := t.transformExpr(e.X)
		index := t.transformExpr(e.Index)
		return obj + "[" + index + "]"

	case *ast.BinaryExpr:
		x := t.transformExpr(e.X)
		y := t.transformExpr(e.Y)
		op := t.mapOp(e.Op)
		return x + " " + op + " " + y

	case *ast.UnaryExpr:
		x := t.transformExpr(e.X)
		op := t.mapOp(e.Op)
		if e.Post {
			return x + op
		}
		return op + x

	case *ast.NilCoalesceExpr:
		x := t.transformExpr(e.X)
		y := t.transformExpr(e.Y)
		return fmt.Sprintf("nilCoalesce(%s, %s)", x, y)

	case *ast.ArrayLit:
		elts := make([]string, 0)
		for _, elt := range e.Elements {
			elts = append(elts, t.transformExpr(elt))
		}
		return "[]interface{}{" + strings.Join(elts, ", ") + "}"
	
	case *ast.StructLit:
		typeName := ""
		if e.Type != nil {
			typeName = t.transformType(e.Type)
		}
		fields := make([]string, 0)
		for _, field := range e.Fields {
			if field.Name != "" {
				// Named field
				fieldName := field.Name
				// Capitalize field name if it starts with lowercase
				if len(fieldName) > 0 && fieldName[0] >= 'a' && fieldName[0] <= 'z' {
					fieldName = strings.Title(fieldName)
				}
				
				// Check if field value is a struct literal that needs type inference
				fieldValue := field.Value
				if sl, ok := fieldValue.(*ast.StructLit); ok && sl.Type == nil && e.Type != nil {
					// Infer nested struct type from field name
					// Try to find field type from the parent struct type
					if _, ok := e.Type.(*ast.Ident); ok {
						// For simple struct types, we can infer the nested struct type
						// This is a simplified inference - in real implementation, we would need
						// to look up the struct definition to get the field type
						sl.Type = &ast.Ident{Name: strings.Title(field.Name)}
						fieldValue = sl
					}
				}
				
				fields = append(fields, fmt.Sprintf("%s: %s", fieldName, t.transformExpr(fieldValue)))
			} else {
				// Positional field
				fields = append(fields, t.transformExpr(field.Value))
			}
		}
		if typeName != "" {
			return fmt.Sprintf("%s{%s}", typeName, strings.Join(fields, ", "))
		} else {
			return fmt.Sprintf("{%s}", strings.Join(fields, ", "))
		}
	
	case *ast.TSXElement:
		// Transform TSX to function call with props: Component(ComponentProps{Field1: val1, Field2: val2})
		componentName := strings.Title(e.TagName)
		propsTypeName := fmt.Sprintf("%sProps", componentName)
		
		// Build props struct fields
		propsFields := make([]string, 0)
		for _, attr := range e.Attributes {
			fieldName := strings.Title(attr.Name)
			fieldValue := t.transformExpr(attr.Value)
			propsFields = append(propsFields, fmt.Sprintf("%s: %s", fieldName, fieldValue))
		}
		
		// Generate props struct
		propsStr := ""
		if len(propsFields) > 0 {
			propsStr = fmt.Sprintf("%s{%s}", propsTypeName, strings.Join(propsFields, ", "))
		} else {
			propsStr = fmt.Sprintf("%s{}", propsTypeName)
		}
		
		// Transform children
		childrenStr := ""
		if len(e.Children) > 0 {
			children := make([]string, 0)
			for _, child := range e.Children {
				children = append(children, t.transformExpr(child))
			}
			childrenStr = ", " + strings.Join(children, ", ")
		}
		
		// Generate constructor call
		constructorName := fmt.Sprintf("gui.New%s", componentName)
		return fmt.Sprintf("%s(%s%s)", constructorName, propsStr, childrenStr)
	
	case *ast.CompositeLit:
		elts := make([]string, 0)
		for _, elt := range e.Elts {
			elts = append(elts, t.transformExpr(elt))
		}
		typ := ""
		if e.Type != nil {
			typ = t.transformType(e.Type)
		}
		if typ != "" && !strings.Contains(typ, "[]") && !strings.Contains(typ, "{}") {
			return typ + "{" + strings.Join(elts, ", ") + "}"
		}
		return "[]" + typ + "{" + strings.Join(elts, ", ") + "}"

	case *ast.ParenExpr:
		return "(" + t.transformExpr(e.X) + ")"

	case *ast.TryExpr:
		x := t.transformExpr(e.X)
		if call, ok := e.X.(*ast.CallExpr); ok {
			call.HasThrows = true
			return t.transformExpr(call)
		}
		return x

	default:
		return ""
	}
}

func (t *Transformer) mapOp(op token.TokenKind) string {
	switch op {
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
	case token.AMP:
		return "&"
	case token.LOGICAL_AND:
		return "&&"
	case token.PIPE:
		return "|"
	case token.LOGICAL_OR:
		return "||"
	case token.CARET:
		return "^"
	case token.TILDE:
		return "~"
	case token.BANG:
		return "!"
	case token.EQUAL:
		return "=="
	case token.NOT_EQUAL:
		return "!="
	case token.LESS:
		return "<"
	case token.GREATER:
		return ">"
	case token.LESS_EQUAL:
		return "<="
	case token.GREATER_EQUAL:
		return ">="
	case token.ASSIGN:
		return "="
	case token.NULL_COALESCE:
		return "??"
	case token.SAFE_DOT:
		return "?."
	default:
		return ""
	}
}

func (t *Transformer) isFuncThrows(expr ast.Expr) bool {
	return false
}

func formatFuncParams(names, types []string) string {
	if len(names) == 0 {
		return ""
	}
	result := ""
	for i := range names {
		if i > 0 {
			result += ", "
		}
		result += names[i] + " " + types[i]
	}
	return result
}
