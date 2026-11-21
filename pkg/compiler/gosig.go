package compiler

// gosig.go — 通用的外部 Go 包函数签名查询。
//
// 目标：当对象字面量作为外部包的 selector 调用实参（如 gxapp.open({...})）
// 时，拿到该函数的参数类型（WindowConfig），把匿名 struct 补全为具名类型。
//
// 做法不是硬编码某个包的签名，而是真实读取外部包的 Go 源码 AST：
//   1. go list -f {{.Dir}} <importPath>  定位包目录（支持 module / GOPATH / 标准库）；
//   2. go/parser 解析该目录的 .go 文件（跳过 _test.go），提取函数的参数类型文本；
//   3. 结果按 (importPath, funcName) 缓存，避免 LSP 场景反复 fork go list。
//
// 查询失败（包不存在、无网络、解析错误）时静默返回 ""，调用方回退为匿名 struct——
// 即增强是「尽力而为」，不影响原有行为，符合零依赖/可预测的设计原则。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// funcSigCache 缓存 importPath → 函数名 → 参数类型文本列表。
// key 为 importPath；值为 pkgSigs。用 sync.Map 保证 LSP 并发安全。
var funcSigCache sync.Map // map[string]map[string][]string

// listDirCache 缓存 importPath → 包目录；失败结果（空串）不缓存。
var listDirCache sync.Map // map[string]string

// goListPkgDir 返回 importPath 对应包的源码目录。用 go list 以支持 module/GOPATH。
// toolchain 允许注入（测试用），默认 "go"。
func goListPkgDir(importPath, toolchain string) string {
	if importPath == "" {
		return ""
	}
	if v, ok := listDirCache.Load(importPath); ok {
		return v.(string)
	}
	if toolchain == "" {
		toolchain = "go"
	}
	out, err := exec.Command(toolchain, "list", "-f", "{{.Dir}}", importPath).Output()
	if err != nil {
		return ""
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return ""
	}
	listDirCache.Store(importPath, dir)
	return dir
}

// parseFuncSigs 解析包目录下所有非测试 .go 文件，构建 函数名 → 参数类型文本列表。
// 同名函数（不应存在）以首次出现的非测试声明为准。
func parseFuncSigs(pkgDir string) map[string][]string {
	if v, ok := funcSigCache.Load(pkgDir); ok {
		return v.(map[string][]string)
	}
	sigs := map[string][]string{}
	if pkgDir == "" {
		return sigs
	}
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, pkgDir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		// 部分包可能有 build tag 冲突等 ParseDir 错误：仍处理已成功解析的文件。
		if len(pkgs) == 0 {
			return sigs
		}
	}
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Type == nil || fd.Type.Params == nil {
					continue
				}
				var params []string
				for _, field := range fd.Type.Params.List {
					t := goExprText(field.Type)
					n := len(field.Names)
					if n == 0 {
						n = 1 // 未命名参数（如 func(string)）也占一个实参位
					}
					for i := 0; i < n; i++ {
						params = append(params, t)
					}
				}
				if _, exists := sigs[fd.Name.Name]; !exists {
					sigs[fd.Name.Name] = params
				}
			}
		}
	}
	funcSigCache.Store(pkgDir, sigs)
	return sigs
}

// goExprText 把 Go AST 表达式还原为类型文本（够用于补全期望类型）。
func goExprText(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		return "*" + goExprText(x.X)
	case *ast.SelectorExpr:
		return goExprText(x.X) + "." + x.Sel.Name
	case *ast.ArrayType:
		if x.Len == nil {
			return "[]" + goExprText(x.Elt)
		}
		return "[" + goExprText(x.Len) + "]" + goExprText(x.Elt)
	case *ast.MapType:
		return "map[" + goExprText(x.Key) + "]" + goExprText(x.Value)
	case *ast.InterfaceType:
		return "any"
	case *ast.Ellipsis:
		return "[]" + goExprText(x.Elt)
	case *ast.BasicLit:
		return x.Value
	default:
		return ""
	}
}

// externalFuncParamType 解析 selector 调用 pkg.fn(...)（GX 小写 fn）第 argIdx 个实参的
// 期望类型。步骤：pkg → importPath（从 import spec）→ 包目录（go list）→ 函数签名（AST）
// → 第 argIdx 参数类型 → 加 pkg 前缀（包内类型）或保留原包前缀（selector 类型）。
// 命中返回带包限定符的类型文本（如 gxapp.WindowConfig），否则 ""。
func externalFuncParamType(toks []Token, pkg, fn string, argIdx int) string {
	if pkg == "" || fn == "" || argIdx < 0 {
		return ""
	}
	// pkg 别名 → import path。
	var importPath string
	for _, s := range collectImportSpecs(toks) {
		if s.pkg == pkg {
			importPath = s.path
			break
		}
	}
	if importPath == "" {
		return ""
	}
	dir := goListPkgDir(importPath, "")
	sigs := parseFuncSigs(dir)
	goName := upperFirst(fn) // GX 小写 open → Go Open
	params, ok := sigs[goName]
	if !ok || argIdx >= len(params) {
		return ""
	}
	typ := params[argIdx]
	if typ == "" {
		return ""
	}
	return qualifyExternalType(typ, pkg)
}

// qualifyExternalType 把 AST 里的参数类型文本加上调用处的 pkg 别名前缀。
// 包内类型（WindowConfig）→ pkg.WindowConfig；已是 selector（gxscene.Instance）
// 或指针/切片含 selector 时按元素判断，避免误加前缀。
func qualifyExternalType(typ, pkg string) string {
	// *T → *qualify(T)
	if strings.HasPrefix(typ, "*") {
		return "*" + qualifyExternalType(strings.TrimPrefix(typ, "*"), pkg)
	}
	// []T / [N]T → 保持元素限定
	if strings.HasPrefix(typ, "[]") {
		return "[]" + qualifyExternalType(strings.TrimPrefix(typ, "[]"), pkg)
	}
	// 已带包限定符（如 gxscene.Instance）→ 原样保留。
	if strings.Contains(typ, ".") {
		return typ
	}
	// map[K]V 等复杂类型：原样返回（对象字面量场景一般不会命中）。
	if strings.HasPrefix(typ, "map[") {
		return typ
	}
	return pkg + "." + typ
}
