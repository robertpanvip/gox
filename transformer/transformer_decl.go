package transformer

import (
	"fmt"
	"strings"

	"github.com/gox-lang/gox/ast"
)

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
