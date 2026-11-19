package compiler

import (
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// 本文件实现「外部符号还原」：不依赖类型推导，直接读 Go 编译产物的导出符号表。
//
// 全局约定：GX 里一律写小写（自己的用 pub 声明、外部的转译器还原大小写）。
// 两级索引：
//   - byPkg[包名/别名][小写名] = 真实名  —— pkg.foo 精确还原，零歧义
//   - global[小写名] = 真实名            —— 变量.字段 还原，仅唯一映射才收录
//
// 歧义（同一小写名对应多个真实名）直接丢弃该条，回退到「用户写真实大小写」。
// 失败降级：某个包读不到 export data（如第三方包没 go build 过）就跳过，不影响其它包。

type importSpec struct {
	alias string // 显式别名，可能为空
	path  string // import 路径（未加引号）
	pkg   string // 实际包名（别名或由路径推断）
}

// collectImportSpecs 解析 import 声明，拿到别名、路径与实际包名。
func collectImportSpecs(toks []Token) []importSpec {
	var specs []importSpec
	add := func(alias, lit string) {
		realPath, err := strconv.Unquote(lit)
		if err != nil {
			realPath = strings.Trim(lit, "\"`")
		}
		pkg := alias
		if pkg == "" {
			pkg = pkgNameFromPath(lit)
		}
		if realPath == "" || pkg == "_" || pkg == "." {
			return
		}
		specs = append(specs, importSpec{alias: alias, path: realPath, pkg: pkg})
	}
	for i := 0; i < len(toks); i++ {
		if !toks[i].is("import") {
			continue
		}
		j := i + 1
		for j < len(toks) {
			k := toks[j].Kind
			if k == Space || k == Newline || k == Comment {
				j++
				continue
			}
			break
		}
		if j < len(toks) && toks[j].Text == "(" {
			j++
			alias := ""
			for j < len(toks) && toks[j].Text != ")" {
				t := toks[j]
				switch {
				case t.Kind == Newline:
					j++
					continue
				case t.Kind == Ident && alias == "":
					alias = t.Text
				case t.Kind == String:
					add(alias, t.Text)
					alias = ""
				}
				j++
			}
			continue
		}
		alias := ""
		for j < len(toks) && toks[j].Kind != Newline && !toks[j].is("import") {
			t := toks[j]
			if t.Kind == Ident && alias == "" {
				alias = t.Text
			}
			if t.Kind == String {
				add(alias, t.Text)
				break
			}
			j++
		}
	}
	return specs
}

// ExtSymbols 是外部符号索引。
type ExtSymbols struct {
	byPkg  map[string]map[string]string // 包名 -> 小写名 -> 真实名
	global map[string]string            // 小写名 -> 真实名（唯一）
}

// importPackage 加载包的导出信息。
// 快路径：importer.Default()（标准库开箱即用）。
// 兜底：go list -export 拿到 export data 文件，再用 ForCompiler+lookup 读取
// （覆盖本地 module 包与已 go build 的第三方包，实现「一律写小写」）。
func importPackage(path string) (*types.Package, error) {
	if pkg, err := importer.Default().Import(path); err == nil {
		return pkg, nil
	}
	// 兜底：go list -export
	out, err := exec.Command("go", "list", "-export", "-f", "{{.Export}}", path).Output()
	if err != nil {
		return nil, err
	}
	exportFile := strings.TrimSpace(string(out))
	if exportFile == "" {
		return nil, strconv.ErrSyntax
	}
	lookup := func(p string) (io.ReadCloser, error) {
		if p == path || p == exportFile {
			return os.Open(exportFile)
		}
		// import 路径可能带版本/子路径，直接复用已拿到的 export data
		return os.Open(exportFile)
	}
	imp := importer.ForCompiler(token.NewFileSet(), "gc", lookup)
	return imp.Import(path)
}

// loadExtSymbols 读取所有 import 包的导出符号（包级 + 方法 + 结构体字段）。
func loadExtSymbols(specs []importSpec) *ExtSymbols {
	e := &ExtSymbols{
		byPkg:  map[string]map[string]string{},
		global: map[string]string{},
	}
	// global 候选：小写名 -> 真实名集合；多于一个则判为歧义，丢弃。
	cand := map[string]map[string]bool{}
	addGlobal := func(real string) {
		if real == "" || !isExported(real) {
			return
		}
		lo := strings.ToLower(real)
		if cand[lo] == nil {
			cand[lo] = map[string]bool{}
		}
		cand[lo][real] = true
	}

	for _, sp := range specs {
		pkg, err := importPackage(sp.path)
		if err != nil {
			continue // 降级：读不到就跳过该包
		}
		table := e.byPkg[sp.pkg]
		if table == nil {
			table = map[string]string{}
			e.byPkg[sp.pkg] = table
		}
		index := func(real string) {
			if real == "" || !isExported(real) {
				return
			}
			lo := strings.ToLower(real)
			if _, exists := table[lo]; !exists {
				table[lo] = real
			}
			addGlobal(real)
		}

		scope := pkg.Scope()
		seenType := map[string]bool{}
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			if obj == nil || !obj.Exported() {
				continue
			}
			index(name) // 包级符号：NewRequest、Println……
			collectTypeSymbols(obj.Type(), seenType, index)
		}
	}

	for lo, reals := range cand {
		if len(reals) == 1 {
			for real := range reals {
				e.global[lo] = real
				break
			}
		}
	}
	return e
}

// collectTypeSymbols 递归收集命名类型的方法与其结构体字段（含具名字段的底层类型）。
func collectTypeSymbols(t types.Type, seen map[string]bool, index func(string)) {
	switch x := t.(type) {
	case *types.Pointer:
		collectTypeSymbols(x.Elem(), seen, index)
	case *types.Slice:
		collectTypeSymbols(x.Elem(), seen, index)
	case *types.Array:
		collectTypeSymbols(x.Elem(), seen, index)
	case *types.Map:
		collectTypeSymbols(x.Key(), seen, index)
		collectTypeSymbols(x.Elem(), seen, index)
	case *types.Chan:
		collectTypeSymbols(x.Elem(), seen, index)
	case *types.Named:
		obj := x.Obj()
		key := ""
		if obj != nil && obj.Pkg() != nil {
			key = obj.Pkg().Path() + "." + obj.Name()
		} else {
			key = obj.Name()
		}
		if seen[key] {
			return
		}
		seen[key] = true
		// 命名类型的方法（List 的 Map/Filter/Reduce，或 http.Request 的方法）
		for i := 0; i < x.NumMethods(); i++ {
			index(x.Method(i).Name())
		}
		// 结构体字段
		collectTypeSymbols(x.Underlying(), seen, index)
	case *types.Struct:
		for i := 0; i < x.NumFields(); i++ {
			f := x.Field(i)
			if f.Exported() {
				index(f.Name())
			}
			if f.Embedded() || isComposite(f.Type()) {
				collectTypeSymbols(f.Type(), seen, index)
			}
		}
	case *types.Interface:
		if iface, ok := x.Underlying().(*types.Interface); ok {
			for i := 0; i < iface.NumMethods(); i++ {
				index(iface.Method(i).Name())
			}
		}
	}
}

func isComposite(t types.Type) bool {
	switch t.(type) {
	case *types.Pointer, *types.Slice, *types.Array, *types.Map, *types.Chan, *types.Named, *types.Struct:
		return true
	}
	return false
}

func isExported(name string) bool {
	if name == "" {
		return false
	}
	r := []rune(name)
	return r[0] >= 'A' && r[0] <= 'Z' || r[0] >= 0x80
}

// restore 把 selector 右侧的小写名还原为真实大小写。
// left 是 selector 左侧文本（包名或变量名）。返回 (还原后的名字, 是否命中)。
func (e *ExtSymbols) restore(left, sel string) (string, bool) {
	if !isLowerStart(sel) {
		return sel, false // 已大写或首字母非字母：不动
	}
	lo := strings.ToLower(sel)
	if table, ok := e.byPkg[left]; ok {
		if real, hit := table[lo]; hit {
			return real, true
		}
		// 左边是包名但没精确命中：不退回 global，避免包内私有名被误改
		return sel, false
	}
	if real, ok := e.global[lo]; ok {
		return real, true
	}
	return sel, false
}
