package transformer

import (
	"fmt"
	"strings"

	"github.com/gox-lang/gox/ast"
)

func (t *Transformer) getTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.BaseType:
		return e.Name
	case *ast.ArrayType:
		// For array types, generate a valid identifier: []int -> ArrayInt
		elementName := t.getTypeName(e.Element)
		return "Array" + strings.Title(elementName)
	default:
		return ""
	}
}

func (t *Transformer) identifierToType(identifier string) string {
	// Convert type identifier back to actual type
	// ArrayInt -> []int, ArrayString -> []string, etc.
	if strings.HasPrefix(identifier, "Array") {
		elementType := strings.ToLower(identifier[5:])
		return "[]" + elementType
	}
	return identifier
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
