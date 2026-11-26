package typecheck

// 引用查找：声明点 + 用户文件内全部使用点，与补全/签名/跳转共用持久类型图。
// 外部符号的声明点落在虚拟 FS 源文件（Pkg/File 定位），使用点始终在用户文件。

import (
	"go/ast"
	"go/types"
	"path"
	"sort"
)

// Ref 是一次引用：Line/Col 1 基；本地（含全部使用点）Pkg 为空；外部声明点带 Pkg/File。
type Ref struct {
	Pkg   string
	File  string
	Line  int
	Col   int
	Name  string
	IsDef bool
}

// References 求光标（字节偏移，指向最后输入字符之后）处符号的全部引用。非标识符返回 false。
func (in *Importer) References(src string, cursor int) ([]Ref, bool) {
	_, info, file, ok := in.checkWithInfo(src)
	if !ok || info == nil || file == nil {
		return nil, false
	}
	tgt := cursor - 1
	var obj types.Object
	bestStart := -1
	ast.Inspect(file, func(n ast.Node) bool {
		id, is := n.(*ast.Ident)
		if !is {
			return true
		}
		s := in.fset.Position(id.Pos()).Offset
		e := in.fset.Position(id.End()).Offset
		if s <= tgt && tgt < e && s > bestStart {
			o := info.Uses[id]
			if o == nil {
				o = info.Defs[id]
			}
			if o != nil {
				obj, bestStart = o, s
			}
		}
		return true
	})
	if obj == nil || obj.Pkg() == nil {
		return nil, false
	}
	out := make([]Ref, 0, 8)
	if pos := obj.Pos(); pos.IsValid() {
		p := in.fset.Position(pos)
		r := Ref{Name: obj.Name(), Line: p.Line, Col: p.Column, IsDef: true}
		if p.Filename != "input.go" {
			r.Pkg = path.Dir(p.Filename)
			r.File = path.Base(p.Filename)
		}
		out = append(out, r)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		id, is := n.(*ast.Ident)
		if !is {
			return true
		}
		if info.Uses[id] != obj && info.Defs[id] != obj {
			return true
		}
		p := in.fset.Position(id.Pos())
		if p.Filename != "input.go" {
			return true
		}
		for _, e := range out {
			if e.Line == p.Line && e.Col == p.Column {
				return true
			}
		}
		out = append(out, Ref{Name: obj.Name(), Line: p.Line, Col: p.Column})
		return true
	})
	if len(out) == 0 {
		return nil, false
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsDef != out[j].IsDef {
			return out[i].IsDef
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Col < out[j].Col
	})
	return out, true
}

// References 是一次性便捷入口（与 Check 用法一致）。
func References(src string, cursor int, lookup Lookup) ([]Ref, bool) {
	return NewImporter(lookup).References(src, cursor)
}
