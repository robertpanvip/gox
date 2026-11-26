package typecheck

// 类型感知补全：对 `expr.` / `pkg.` 光标上下文枚举成员。
//
// go/types 无法解析未完成的点号表达式，因此把点号后（含已敲前缀）替换为占位
// 标识符再检查——持久 Importer 让 std 类型图不重建，每次补全只重解析用户文件。
// 随后在类型化 AST 中找到占位 SelectorExpr：包限定符经 Uses 取 *types.PkgName
// （PkgName 不实现 types.Type，不能走 Types 分支）；其余基底走变量对象类型或
// 表达式类型。成员枚举：包→作用域导出成员；结构体→字段+方法集；接口→方法集；
// 其余类型→方法集（按指针视图取值，覆盖指针接收者）。

import (
	"go/ast"
	"go/types"
	"strings"
)

// Candidate 是一条补全候选。Kind ∈ package|function|method|field|variable|constant|type。
type Candidate struct {
	Name      string
	Signature string
	Kind      string
	// Pkg 为符号归属的外部包路径（用户文件内符号为空），供 resolve 类能力回溯 doc。
	Pkg string
}

const completeDummy = "GxCompleteX_"

func isIdentChar(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b >= 0x80
}

// ByteOffset 把 src 的 1 基行列换成字节偏移；越界返回 -1。
func ByteOffset(src string, line, col int) int {
	if line < 1 || col < 1 {
		return -1
	}
	off := 0
	for i := 0; i < line-1; i++ {
		j := strings.IndexByte(src[off:], '\n')
		if j < 0 {
			return -1
		}
		off += j + 1
	}
	if off+col-1 > len(src) {
		return -1
	}
	return off + col - 1
}

// Complete 在 src 的 cursor（字节偏移，指向点号/已敲前缀末尾）做成员补全。
// 非点号上下文返回 nil（调用方回退符号表路径）。
func (in *Importer) Complete(src string, cursor int) []Candidate {
	if cursor <= 0 || cursor > len(src) {
		return nil
	}
	dot := cursor
	for dot > 0 && isIdentChar(src[dot-1]) {
		dot--
	}
	if dot == 0 || src[dot-1] != '.' {
		return nil
	}
	mods := src[:dot] + completeDummy + src[cursor:]
	_, info, file, ok := in.checkWithInfo(mods)
	if !ok || info == nil || file == nil {
		return nil
	}
	var sel *ast.SelectorExpr
	ast.Inspect(file, func(n ast.Node) bool {
		if sel != nil {
			return false
		}
		if sx, is := n.(*ast.SelectorExpr); is && sx.Sel != nil && sx.Sel.Name == completeDummy {
			sel = sx
		}
		return true
	})
	if sel == nil {
		return nil
	}
	// 包限定符：`strings.` 的 X 是 PkgName 标识符，只登记在 Uses。
	if id, isIdent := sel.X.(*ast.Ident); isIdent {
		if obj, uses := info.Uses[id]; uses {
			if pn, isPkg := obj.(*types.PkgName); isPkg {
				return pkgMembers(pn.Imported())
			}
			if obj != nil && obj.Type() != nil {
				return membersOf(obj.Type())
			}
		}
	}
	if tv, has := info.Types[sel.X]; has && tv.Type != nil {
		return membersOf(tv.Type)
	}
	return nil
}

func objKind(o types.Object) string {
	switch o.(type) {
	case *types.Func:
		return "function"
	case *types.Var:
		return "variable"
	case *types.Const:
		return "constant"
	case *types.TypeName:
		return "type"
	case *types.PkgName:
		return "package"
	}
	return "variable"
}

// candPkg 返回候选归属的外部包 import path；用户文件内符号返回空串。
func candPkg(obj types.Object) string {
	if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() == userPkgPath {
		return ""
	}
	return obj.Pkg().Path()
}

// pkgMembers 枚举包作用域导出成员（main 包外不可见名跳过）。
func pkgMembers(pkg *types.Package) []Candidate {
	out := []Candidate{}
	if pkg == nil {
		return out
	}
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if obj == nil {
			continue
		}
		if !visible(obj) {
			continue
		}
		sig := ""
		if obj.Type() != nil {
			sig = obj.Type().String()
		}
		out = append(out, Candidate{Name: name, Signature: sig, Kind: objKind(obj), Pkg: candPkg(obj)})
	}
	return out
}

// userPkgPath 与 checkWithInfo 中 conf.Check 的包路径一致；同包内部符号可见。
const userPkgPath = "input"

// visible 判定跨包补全泄漏：其它包的未导出成员不进候选。
func visible(obj types.Object) bool {
	if obj == nil {
		return false
	}
	if obj.Exported() {
		return true
	}
	pkg := obj.Pkg()
	return pkg == nil || pkg.Path() == userPkgPath
}

// membersOf 枚举类型可见成员（字段+方法集，去重；指针解引用；接口只列方法）。
func membersOf(t types.Type) []Candidate {
	out := []Candidate{}
	seen := map[string]bool{}
	add := func(name, sig, kind string, obj types.Object) {
		if name == "" || seen[name] || !visible(obj) {
			return
		}
		seen[name] = true
		out = append(out, Candidate{Name: name, Signature: sig, Kind: kind, Pkg: candPkg(obj)})
	}
	if ptr, is := t.(*types.Pointer); is {
		return membersOf(ptr.Elem())
	}
	if st, is := t.Underlying().(*types.Struct); is {
		for i := 0; i < st.NumFields(); i++ {
			f := st.Field(i)
			add(f.Name(), f.Type().String(), "field", f)
		}
	}
	if iface, is := t.Underlying().(*types.Interface); is {
		for i := 0; i < iface.NumMethods(); i++ {
			m := iface.Method(i)
			add(m.Name(), m.Type().String(), "method", m)
		}
		return out
	}
	ms := types.NewMethodSet(types.NewPointer(t))
	for i := 0; i < ms.Len(); i++ {
		obj := ms.At(i).Obj()
		sig := ""
		if obj.Type() != nil {
			sig = obj.Type().String()
		}
		add(obj.Name(), sig, "method", obj)
	}
	return out
}
