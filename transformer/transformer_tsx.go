package transformer

import (
	"fmt"
	"strings"

	"github.com/gox-lang/gox/ast"
)

// TransformerTSX TSX 椋庢牸鐨勮浆鎹㈠櫒锛圱emplateResult锛?
type TransformerTSX struct {
	imports map[string]bool
}

// NewTSX 鍒涘缓 TSX 椋庢牸鐨勮浆鎹㈠櫒
func NewTSX() *TransformerTSX {
	return &TransformerTSX{
		imports: make(map[string]bool),
	}
}

// Transform 杞崲绋嬪簭
func (t *TransformerTSX) Transform(prog *ast.Program) string {
	var sb strings.Builder

	// 鍐欏叆 package
	if prog.Package != nil {
		sb.WriteString(fmt.Sprintf("package %s\n\n", prog.Package.Name))
	}

	// 鏀堕泦瀵煎叆
	t.collectImports(prog)

	// 杈撳嚭瀵煎叆
	sb.WriteString("import (\n")
	sb.WriteString("\t\"github.com/gox-lang/gox/gui\"\n")
	for path := range t.imports {
		sb.WriteString(fmt.Sprintf("\t%q\n", path))
	}
	sb.WriteString(")\n\n")

	// 杞崲 FX 鍑芥暟锛堝凡搴熷純锛屼繚鐣欏嚱鏁扮鍚嶄互鍏煎鏃т唬鐮侊級
	// FX 鍔熻兘宸茬Щ闄わ紝涓嶅啀澶勭悊
	for _, decl := range prog.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			// 涓嶅啀妫€鏌?IsFx锛屾墍鏈夊嚱鏁伴兘鎸夋櫘閫氬嚱鏁板鐞?
			_ = fn
		}
	}

	return sb.String()
}

func (t *TransformerTSX) collectImports(prog *ast.Program) {
	for _, decl := range prog.Decls {
		if imp, ok := decl.(*ast.ImportDecl); ok {
			t.imports[imp.Path] = true
		}
	}
}

// TransformFxFunc 杞崲 FX 鍑芥暟涓?lit-html 椋庢牸锛堝叕寮€鏂规硶锛?
func (t *TransformerTSX) TransformFxFunc(f *ast.FuncDecl) string {
	return t.transformFxFunc(f)
}

// TransformFunc 杞崲鏅€氬嚱鏁颁负 lit-html 椋庢牸锛堟敮鎸?sig 鍏抽敭瀛楋級
func (t *TransformerTSX) TransformFunc(f *ast.FuncDecl) string {
	return t.transformFuncWithSig(f)
}

func (t *TransformerTSX) transformFuncWithSig(f *ast.FuncDecl) string {
	var sb strings.Builder

	componentName := strings.Title(f.Name)

	// 鐢熸垚鍑芥暟绛惧悕
	sb.WriteString(fmt.Sprintf("// %s 缁勪欢鍑芥暟\n", componentName))
	sb.WriteString(fmt.Sprintf("func %s() {\n", componentName))

	// 杞崲鍑芥暟浣擄紙鍖呮嫭 sig 澹版槑锛?
	if f.Body != nil {
		for _, stmt := range f.Body.List {
			sb.WriteString(t.transformStmt(stmt))
		}
	}

	sb.WriteString("}\n")

	return sb.String()
}

func (t *TransformerTSX) transformFxFunc(f *ast.FuncDecl) string {
	var sb strings.Builder

	componentName := strings.Title(f.Name)

	// 鏀堕泦鐘舵€佸彉閲?
	stateVars := t.collectStateVars(f.Body)

	// 鐢熸垚鍑芥暟绛惧悕
	sb.WriteString(fmt.Sprintf("// %s 缁勪欢鍑芥暟\n", componentName))
	sb.WriteString(fmt.Sprintf("func %s() func() gui.TemplateResult {\n", componentName))

	// 鐘舵€佸彉閲忎綔涓洪棴鍖呭彉閲?
	for _, sv := range stateVars {
		sb.WriteString(fmt.Sprintf("\t%s := %s\n", sv.Name, sv.Value))
	}

	// 杩斿洖缁勪欢鍑芥暟
	sb.WriteString("\treturn func() gui.TemplateResult {\n")

	// 杞崲 TSX
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

func (t *TransformerTSX) collectStateVars(block *ast.BlockStmt) []StateVar {
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

func (t *TransformerTSX) findReturnStmt(block *ast.BlockStmt) *ast.ReturnStmt {
	for _, stmt := range block.List {
		if ret, ok := stmt.(*ast.ReturnStmt); ok {
			return ret
		}
	}
	return nil
}

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
		return e.Name
	default:
		return "nil"
	}
}

func (t *TransformerTSX) transformTSX(tsx *ast.TSXElement, stateVars []StateVar) string {
	var sb strings.Builder

	// 鎻愬彇鍔ㄦ€佸€?
	dynamicValues := t.extractDynamicValues(tsx)

	// StaticCode
	sb.WriteString(fmt.Sprintf("\t\treturn gui.TemplateResult{\n"))
	sb.WriteString(fmt.Sprintf("\t\t\tStaticCode: `<%s>`,\n", tsx.TagName))

	// Dynamic 鏁扮粍
	if len(dynamicValues) > 0 {
		sb.WriteString(fmt.Sprintf("\t\t\tDynamic: []interface{}{%s},\n", strings.Join(dynamicValues, ", ")))
	} else {
		sb.WriteString("\t\t\tDynamic: []interface{}{},\n")
	}

	// Factory 鍑芥暟
	sb.WriteString("\t\t\tFactory: func() (gui.Component, []gui.Part) {\n")

	// 鍒涘缓 Parts
	partCount := len(dynamicValues)
	for i := 0; i < partCount; i++ {
		sb.WriteString(fmt.Sprintf("\t\t\t\tcomment%d := gui.NewComment(\"dynamic-%d\")\n", i, i))
		sb.WriteString(fmt.Sprintf("\t\t\t\tpart%d := gui.NewTextPart(comment%d)\n", i, i))
	}

	// 鍒涘缓缁勪欢
	sb.WriteString(t.createComponent(tsx, partCount))

	// 杩斿洖
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

	// 妫€鏌ュ睘鎬?
	for _, attr := range tsx.Attributes {
		if attr.Value != nil {
			// 妫€鏌ユā鏉垮瓧绗︿覆
			if tmpl, ok := attr.Value.(*ast.TemplateString); ok {
				for _, expr := range tmpl.Exprs {
					values = append(values, t.transformExpr(expr))
				}
			} else {
				// 妫€鏌ユ槸鍚︽槸鏍囪瘑绗︽垨鍏朵粬琛ㄨ揪寮忥紙闈炲瓧绗︿覆瀛楅潰閲忥級
				if _, isStringLit := attr.Value.(*ast.StringLit); !isStringLit {
					values = append(values, t.transformExpr(attr.Value))
				}
			}
		}
	}

	// 妫€鏌ュ瓙鑺傜偣
	for _, child := range tsx.Children {
		// 妫€鏌ユā鏉垮瓧绗︿覆
		if tmpl, ok := child.(*ast.TemplateString); ok {
			for _, expr := range tmpl.Exprs {
				values = append(values, t.transformExpr(expr))
			}
		} else if child != nil {
			// 妫€鏌ユ槸鍚︽槸鏍囪瘑绗︽垨鍏朵粬琛ㄨ揪寮忥紙闈炲瓧绗︿覆瀛楅潰閲忥級
			if _, isStringLit := child.(*ast.StringLit); !isStringLit {
				values = append(values, t.transformExpr(child))
			}
		}
	}

	return values
}

func (t *TransformerTSX) createComponent(tsx *ast.TSXElement, partCount int) string {
	componentName := strings.Title(tsx.TagName)

	// 绠€鍗曞疄鐜帮細鍒涘缓缁勪欢
	return fmt.Sprintf("\t\t\t\troot := gui.New%s(gui.%sProps{})\n", componentName, componentName)
}

// transformStmt 杞崲璇彞锛堟敮鎸?sig 鍜?TSX锛?
func (t *TransformerTSX) transformStmt(stmt ast.Stmt) string {
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
		// 鍏朵粬璇彞浣跨敤鏅€氳浆鎹?
		sb.WriteString(fmt.Sprintf("\t// TODO: transform statement %T\n", stmt))
	}
	
	return sb.String()
}

// StateVar 鐘舵€佸彉閲?
type StateVar struct {
	Name  string
	Value string
}

