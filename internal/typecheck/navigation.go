package typecheck

// 声明跳转与类型化 hover：与补全/签名共享同一套持久 Importer 类型图。
// 光标下标识符 → types.Object → 声明位置；std 符号的位置落在内嵌/磁盘虚拟 FS 的
// 源文件上（Filename 形如 "fmt/print.go"），顺带取回声明片段与文档注释供速览；
// 用户文件内的符号回投归宿主（.gx 映射）负责。内建函数没有源码，优雅缺席。

import (
	"go/ast"
	"go/types"
	"path"
	"strconv"
	"strings"
)

// Decl 是符号声明位置与快览。Pkg 为空串表示声明在用户当前文件
// （Line/Col 为生成 Go 代码 1 基坐标，宿主经 SourceMap 回投 .gx）。
type Decl struct {
	Pkg     string
	File    string // 包内文件名，如 print.go
	Line    int    // 1 基
	Col     int    // 1 基
	Snippet string // 声明签名行（到首个 '{' 或 4 行为止），可能为空
	Doc     string // 紧贴声明上方的 // 注释，可能为空

	// Name 为符号名（typeDefinition/implementation 展示与跨度计算用）。
	Name string
}

// objectAt 返回光标（字节偏移，指向最后输入字符之后）所覆盖标识符的类型对象。
// 多个候选取最内层（起点最大者）。
func (in *Importer) objectAt(src string, cursor int) (types.Object, bool) {
	if cursor <= 0 || cursor > len(src) {
		return nil, false
	}
	_, info, file, ok := in.checkWithInfo(src)
	if !ok || info == nil || file == nil {
		return nil, false
	}
	tgt := cursor - 1
	var bestObj types.Object
	var bestStart int = -1
	ast.Inspect(file, func(n ast.Node) bool {
		id, is := n.(*ast.Ident)
		if !is {
			return true
		}
		s := in.fset.Position(id.Pos()).Offset
		e := in.fset.Position(id.End()).Offset
		if s <= tgt && tgt < e && s > bestStart {
			obj := info.Uses[id]
			if obj == nil {
				obj = info.Defs[id]
			}
			if obj != nil {
				bestObj, bestStart = obj, s
			}
		}
		return true
	})
	return bestObj, bestObj != nil
}

// Definition 求光标处符号的声明位置。内建函数/无效位置返回 false。
func (in *Importer) Definition(src string, cursor int) (Decl, bool) {
	obj, ok := in.objectAt(src, cursor)
	if !ok || obj.Pkg() == nil {
		return Decl{}, false
	}
	pos := obj.Pos()
	if !pos.IsValid() {
		return Decl{}, false
	}
	p := in.fset.Position(pos)
	if p.Filename == "input.go" {
		return Decl{Line: p.Line, Col: p.Column}, true
	}
	d := Decl{
		Pkg:  path.Dir(p.Filename),
		File: path.Base(p.Filename),
	}
	if obj.Pkg().Path() == userPkgPath {
		d.Line, d.Col = p.Line, p.Column
		d.Pkg = ""
		return d, true
	}
	d.Line, d.Col = p.Line, p.Column
	if files, ok := in.im.lookup(d.Pkg); ok {
		for _, fd := range files {
			if fd.Name != d.File {
				continue
			}
			d.Snippet, d.Doc = snippetAndDoc(strings.Split(fd.Src, "\n"), p.Line)
		}
	}
	return d, true
}

// snippetAndDoc 从声明行（1 基）取签名片段与上方紧邻注释。
func snippetAndDoc(lines []string, line int) (snippet, doc string) {
	i := line - 1
	if i < 0 || i >= len(lines) {
		return "", ""
	}
	var docLines []string
	for j := i - 1; j >= 0; j-- {
		t := strings.TrimSpace(lines[j])
		if strings.HasPrefix(t, "//") {
			docLines = append([]string{strings.TrimSpace(strings.TrimPrefix(t, "//"))}, docLines...)
			continue
		}
		break
	}
	end := i
	for end+1 < len(lines) && !strings.Contains(lines[end], "{") && end-i < 3 {
		end++
	}
	return strings.Join(lines[i:end+1], "\n"), strings.Join(docLines, "\n")
}

// TypeInfo 返回光标处对象的类型文本与声明位置提示（hover 用）。
// 用户文件内 where 为空串；内建函数类型串取 Builtin 签名表。
func (in *Importer) TypeInfo(src string, cursor int) (sig, where string, ok bool) {
	obj, found := in.objectAt(src, cursor)
	if !found {
		return "", "", false
	}
	q := func(p *types.Package) string {
		if p == nil || p.Path() == userPkgPath {
			return ""
		}
		return p.Name()
	}
	if obj.Pkg() == nil {
		return "", "", false // 内建函数在 go/types 无私开签名访问器，按无提示降级
	}
	t := obj.Type()
	if t == nil {
		return "", "", false
	}
	sig = types.TypeString(t, q)
	if obj.Pkg().Path() != userPkgPath {
		if pos := obj.Pos(); pos.IsValid() {
			p := in.fset.Position(pos)
			where = p.Filename + ":" + strconv.Itoa(p.Line)
		}
	}
	return sig, where, true
}

// StdSource 返回依赖包某个源文件的全文（只读视图数据）。数据源不可达返回 false。
func (in *Importer) StdSource(pkg string, file string) (string, bool) {
	files, ok := in.im.lookup(pkg)
	if !ok {
		return "", false
	}
	for _, fd := range files {
		if fd.Name == file {
			return fd.Src, true
		}
	}
	return "", false
}

// 一次性便捷入口（与 Check 用法一致）。
func Definition(src string, cursor int, lookup Lookup) (Decl, bool) {
	return NewImporter(lookup).Definition(src, cursor)
}

func TypeInfo(src string, cursor int, lookup Lookup) (string, string, bool) {
	return NewImporter(lookup).TypeInfo(src, cursor)
}
