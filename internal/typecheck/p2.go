package typecheck

// typeDefinition / implementation / 跨文件重命名的核心层，全部建立在同一持久类型图上：
//   TypeDecl        光标符号的类型 → 解指针/别名 → 命名类型声明（universe 与匿名类型 false）；
//   Implementations 接口类型名或接口方法 → 被检源内满足该接口的命名类型（按行序）；
//   MultiRename     同包多文件一次 types 检查，产出目标符号在每个文件的全部出现（含声明处），
//                   局部遮蔽天然免疫—— occurrence 判定走 Uses/Defs 对象同一性，不是文本匹配。

import (
	"go/ast"
	"go/parser"
	"go/types"
	"path"
	"sort"
	"strings"
)

// declOf 把对象落位成 Decl（含快览字段）；与 Definition 行为一致但多带 Name。
func (in *Importer) declOf(obj types.Object) Decl {
	d := Decl{Name: obj.Name()}
	pos := obj.Pos()
	if !pos.IsValid() {
		return d
	}
	p := in.fset.Position(pos)
	if p.Filename == "input.go" || obj.Pkg() == nil || obj.Pkg().Path() == userPkgPath {
		d.Line, d.Col = p.Line, p.Column
		return d
	}
	d.Pkg, d.File = path.Dir(p.Filename), path.Base(p.Filename)
	d.Line, d.Col = p.Line, p.Column
	if files, ok := in.im.lookup(d.Pkg); ok {
		for _, fd := range files {
			if fd.Name == d.File {
				d.Snippet, d.Doc = snippetAndDoc(strings.Split(fd.Src, "\n"), p.Line)
			}
		}
	}
	return d
}

// TypeDecl 求光标处表达式类型的声明位置。指针解引用、别名展开；
// universe 类型（string/int/error）与匿名结构返回 false。
func (in *Importer) TypeDecl(src string, cursor int) (Decl, bool) {
	obj, ok := in.objectAt(src, cursor)
	if !ok {
		return Decl{}, false
	}
	t := obj.Type()
	if t == nil {
		return Decl{}, false
	}
	t = types.Unalias(t)
	for {
		ptr, is := t.(*types.Pointer)
		if !is {
			break
		}
		t = types.Unalias(ptr.Elem())
	}
	named, is := t.(*types.Named)
	if !is {
		return Decl{}, false
	}
	tobj := named.Obj()
	if tobj.Pkg() == nil {
		return Decl{}, false // universe：无源码可跳
	}
	return in.declOf(tobj), true
}

// ifaceOf 从对象提取接口类型（类型名或接口方法两种入口）。
func ifaceOf(obj types.Object) *types.Interface {
	switch o := obj.(type) {
	case *types.TypeName:
		if i, is := types.Unalias(o.Type()).Underlying().(*types.Interface); is {
			return i
		}
	case *types.Func:
		if sg, is := o.Type().(*types.Signature); is && sg.Recv() != nil {
			rt := types.Unalias(sg.Recv().Type())
			if n, is := rt.(*types.Named); is {
				if i, is := n.Underlying().(*types.Interface); is {
					return i
				}
			}
		}
	}
	return nil
}

// Implementations 返回被检源（用户文件）内满足光标接口的命名类型声明；
// 接口自身与其它接口不计入。跨包实现扫描在 roadmap（需全闭包解析），此处如实不承诺。
func (in *Importer) Implementations(src string, cursor int) ([]Decl, bool) {
	obj, ok := in.objectAt(src, cursor)
	if !ok {
		return nil, false
	}
	iface := ifaceOf(obj)
	if iface == nil {
		return nil, false
	}
	_, info, _, ok := in.checkWithInfo(src)
	if !ok {
		return nil, false
	}
	decls := []Decl{}
	for id, o := range info.Defs {
		tn, is := o.(*types.TypeName)
		if !is || id == nil || tn.Pkg() == nil || tn.Pkg().Path() != userPkgPath {
			continue
		}
		if _, self := types.Unalias(tn.Type()).Underlying().(*types.Interface); self {
			continue
		}
		if types.Implements(tn.Type(), iface) {
			decls = append(decls, in.declOf(tn))
		}
	}
	sort.SliceStable(decls, func(i, j int) bool { return decls[i].Line < decls[j].Line })
	return decls, true
}

// SrcOcc 是符号一处出现的 1 基行列与长度。
type SrcOcc struct {
	Line int
	Col  int
	Len  int
}

// MultiRename 同包多文件语境下定位光标符号的全部出现。
// files 为该包全部编译单元（Name 即返回 edits 的键）；targetFile + cursor 定位目标符号；
// 光标必须落在标识符上且符号声明在本包内，否则 false。
func (in *Importer) MultiRename(files []File, targetFile string, cursor int) (string, map[string][]SrcOcc, bool) {
	if cursor <= 0 {
		return "", nil, false
	}
	asts := map[string]*ast.File{}
	order := make([]*ast.File, 0, len(files))
	for _, fd := range files {
		if _, dup := asts[fd.Name]; dup {
			continue
		}
		f, err := parser.ParseFile(in.fset, fd.Name, fd.Src, parser.SkipObjectResolution)
		if err != nil || f == nil {
			return "", nil, false
		}
		asts[fd.Name] = f
		order = append(order, f)
	}
	tf, ok := asts[targetFile]
	if !ok {
		return "", nil, false
	}
	info := &types.Info{
		Types:        make(map[ast.Expr]types.TypeAndValue),
		Defs:         make(map[*ast.Ident]types.Object),
		Uses:         make(map[*ast.Ident]types.Object),
		Implicits:    make(map[ast.Node]types.Object),
		Selections:   make(map[*ast.SelectorExpr]*types.Selection),
		Scopes:       make(map[ast.Node]*types.Scope),
		FileVersions: make(map[*ast.File]string),
	}
	conf := types.Config{Importer: in.im, Error: func(error) {}}
	conf.Check(userPkgPath, in.fset, order, info)

	tgt := cursor - 1
	var obj types.Object
	bestStart := -1
	name := ""
	ast.Inspect(tf, func(n ast.Node) bool {
		id, is := n.(*ast.Ident)
		if !is {
			return true
		}
		sp := in.fset.Position(id.Pos())
		if sp.Filename != targetFile {
			return true
		}
		e := in.fset.Position(id.End()).Offset
		if sp.Offset <= tgt && tgt < e && sp.Offset > bestStart {
			o := info.Uses[id]
			if o == nil {
				o = info.Defs[id]
			}
			if o != nil {
				obj, bestStart, name = o, sp.Offset, o.Name()
			}
		}
		return true
	})
	if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != userPkgPath {
		return "", nil, false
	}
	edits := map[string][]SrcOcc{}
	for fname, f := range asts {
		ast.Inspect(f, func(n ast.Node) bool {
			id, is := n.(*ast.Ident)
			if !is {
				return true
			}
			if info.Uses[id] != obj && info.Defs[id] != obj {
				return true
			}
			p := in.fset.Position(id.Pos())
			edits[fname] = append(edits[fname], SrcOcc{Line: p.Line, Col: p.Column, Len: len(name)})
			return true
		})
	}
	return name, edits, true
}

// MultiRename 是一次性便捷入口（与 Check 用法一致）：files 即完整包语境，无需 lookup。
func MultiRename(files []File, targetFile string, cursor int) (string, map[string][]SrcOcc, bool) {
	return NewImporter(func(string) ([]File, bool) { return nil, false }).MultiRename(files, targetFile, cursor)
}

// TypeDecl 是一次性便捷入口（与 Check 用法一致）。
func TypeDecl(src string, cursor int, lookup Lookup) (Decl, bool) {
	return NewImporter(lookup).TypeDecl(src, cursor)
}

// Implementations 是一次性便捷入口。
func Implementations(src string, cursor int, lookup Lookup) ([]Decl, bool) {
	return NewImporter(lookup).Implementations(src, cursor)
}
