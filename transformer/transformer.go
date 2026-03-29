package transformer

import (
	"fmt"
	"strings"

	"github.com/gox-lang/gox/ast"
)

type Transformer struct {
	indent      int
	extendFuncs map[string][]*ast.FuncDecl
	imports     map[string]string           // package path -> source type ("go", "gox", or "")
	funcTypes   map[string][]*ast.FuncParam // Store function parameter types: "FuncName" -> [params]
	fxFuncs     []*ast.FuncDecl             // FX functions to process
}

func New() *Transformer {
	t := &Transformer{
		indent:      0,
		extendFuncs: make(map[string][]*ast.FuncDecl),
		imports:     make(map[string]string),
		funcTypes:   make(map[string][]*ast.FuncParam),
		fxFuncs:     make([]*ast.FuncDecl, 0),
	}
	// Always add fmt package as it's used for string formatting
	t.addImport("fmt", "")
	return t
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
			content = "" // Clear content to avoid double-adding
			break
		}

		// Escape % in the content before ${
		format += strings.ReplaceAll(content[:idx], "%", "%%")
		content = content[idx+2:]

		endIdx := strings.Index(content, "}")
		if endIdx == -1 {
			format += "${" + content
			content = "" // Clear content to avoid double-adding
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

	// Second pass: transform declarations
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
			if d.IsFx {
				// Collect FX functions for later processing
				t.fxFuncs = append(t.fxFuncs, d)
			} else {
				sb.WriteString(t.transformFunc(d))
				sb.WriteString("\n\n")
			}
		case *ast.VarDecl:
			// Skip global var declarations, they will be in init()
			// Only output var declarations inside functions
		case *ast.ConstDecl:
			sb.WriteString(t.transformConstDecl(d))
			sb.WriteString("\n")
		case *ast.ExtendDecl:
			if d.Type != nil {
				// Get type name for extend declaration (supports array types like int[])
				typeName := t.getTypeName(d.Type)
				t.extendFuncs[typeName] = d.Methods
			}
		}
	}
	
	// Process FX functions after all other declarations
	for _, fxFunc := range t.fxFuncs {
		sb.WriteString(t.transformFxFunc(fxFunc))
		sb.WriteString("\n\n")
	}

	// Transform global variable declarations and statements in init()
	hasGlobalVars := false
	for _, decl := range prog.Decls {
		if _, ok := decl.(*ast.VarDecl); ok {
			hasGlobalVars = true
			break
		}
	}
	if hasGlobalVars || len(prog.Stmts) > 0 {
		sb.WriteString("\n// Global initialization\n")
		sb.WriteString("func init() {\n")
		t.indent++
		// Transform global variable declarations
		for _, decl := range prog.Decls {
			if v, ok := decl.(*ast.VarDecl); ok {
				sb.WriteString(t.transformVarDecl(v))
				sb.WriteString("\n")
			}
		}
		// Transform global statements
		for _, stmt := range prog.Stmts {
			sb.WriteString(t.transformStmt(stmt, false))
		}
		t.indent--
		sb.WriteString("}\n\n")
	}

	// Output dynamically added imports (not in source code)
	// These are imports added by the transformer itself (e.g., "fmt")
	if len(t.imports) > 0 {
		// Check which imports were not already output
		for path := range t.imports {
			// Only output if it's an auto-added import (source type is empty)
			if t.imports[path] == "" {
				sb.WriteString(fmt.Sprintf("import %q\n", path))
			}
		}
	}

	for typeName, methods := range t.extendFuncs {
		for _, method := range methods {
			genName := typeName + strings.Title(method.Name)
			method.Name = genName
			// Convert type identifier back to actual type for self parameter
			// ArrayInt -> []int, ArrayString -> []string, etc.
			actualType := t.identifierToType(typeName)
			sb.WriteString(t.transformExtendFunc(method, actualType))
			sb.WriteString("\n\n")
		}
	}

	return sb.String()
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
