package transformer

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gox-lang/gox/ast"
	"github.com/gox-lang/gox/token"
)

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
	case token.INC:
		return "++"
	case token.DEC:
		return "--"
	case token.NULL_COALESCE:
		return "??"
	case token.SAFE_DOT:
		return "?."
	default:
		return ""
	}
}

// transformStyleObject transforms a style object literal into &Style{...}
func (t *Transformer) transformStyleObject(tmpl *ast.TemplateString) string {
	// Parse the object fields from the template string parts
	fields := make([]string, 0)
	
	// The template string contains the object fields
	// Each field is like: display: "flex", flexDirection: "column", etc.
	for i, part := range tmpl.Parts {
		// Skip empty parts and braces
		part = strings.TrimSpace(part)
		if part == "" || part == "{" || part == "}" || part == "," {
			continue
		}
		
		// Parse field: value pairs
		// Format: fieldName: value
		parts := strings.SplitN(part, ":", 2)
		if len(parts) != 2 {
			continue
		}
		
		fieldName := strings.TrimSpace(parts[0])
		fieldValue := strings.TrimSpace(parts[1])
		
		// Remove trailing comma from value
		fieldValue = strings.TrimSuffix(fieldValue, ",")
		fieldValue = strings.TrimSpace(fieldValue)
		
		// Convert camelCase to PascalCase for Go struct field
		goFieldName := strings.Title(fieldName)
		
		// Transform the value (could be string, number, etc.)
		goValue := t.transformStyleValue(fieldValue)
		
		fields = append(fields, fmt.Sprintf("%s: %s", goFieldName, goValue))
		
		_ = i // avoid unused variable warning
	}
	
	return fmt.Sprintf("&gui.Style{%s}", strings.Join(fields, ", "))
}

// transformStyleValue transforms a style value to Go syntax
func (t *Transformer) transformStyleValue(value string) string {
	value = strings.TrimSpace(value)
	
	// If it's a string literal, keep it as-is
	if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
		return value
	}
	
	// If it's a number, keep it as-is
	if _, err := strconv.Atoi(value); err == nil {
		return value
	}
	if _, err := strconv.ParseFloat(value, 32); err == nil {
		return value
	}
	
	// Otherwise, return as-is (might be a constant or expression)
	return value
}

// mapTSXTagsToComponent maps lowercase TSX tags to Go component names
func (t *Transformer) mapTSXTagsToComponent(tagName string) string {
	// Map common HTML tags to Go components
	switch tagName {
	case "div":
		return "Div"
	case "label":
		return "Label"
	case "button":
		return "Button"
	case "span":
		return "Span"
	case "input":
		return "Input"
	case "img":
		return "Image"
	case "a":
		return "Link"
	default:
		// If not in the map, use Title case (for custom components)
		return strings.Title(tagName)
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
		// Add fmt import when using fmt.Sprintf
		t.addImport("fmt", "")
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
		// Map lowercase HTML tags to Go component names
		componentName := t.mapTSXTagsToComponent(e.TagName)
		propsTypeName := fmt.Sprintf("%sProps", componentName)
		
		// Check if there's a "style" attribute with object literal
		var styleValue string
		propsFields := make([]string, 0)
		for _, attr := range e.Attributes {
			if attr.Name == "style" {
				// Check if it's an object literal {{...}}
				if tmpl, ok := attr.Value.(*ast.TemplateString); ok && len(tmpl.Exprs) == 0 {
					// Parse the object literal inside {{...}}
					// The parts should contain the object fields
					styleValue = t.transformStyleObject(tmpl)
				} else {
					// Not an object literal, use as-is
					styleValue = t.transformExpr(attr.Value)
				}
			} else {
				fieldName := strings.Title(attr.Name)
				fieldValue := t.transformExpr(attr.Value)
				propsFields = append(propsFields, fmt.Sprintf("%s: %s", fieldName, fieldValue))
			}
		}
		
		// Generate props struct or use style
		propsStr := ""
		if styleValue != "" {
			// Use style directly as first parameter
			propsStr = styleValue
			// If there are other props, we need to merge them
			if len(propsFields) > 0 {
				// For now, just use style and ignore other props
				// TODO: Support merging style with other props
			}
		} else if len(propsFields) > 0 {
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
