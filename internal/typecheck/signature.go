package typecheck

// 参数签名提示（signature help）：光标位于某调用的实参区内时，给出该调用的真实
// 签名。与成员补全同源同套路——go/types 不接受半截调用，probe 在光标处注入占位
// 实参、丢弃光标之后的内容，并按未闭合括号栈倒序补齐闭合符，把"正在输入"还原成
// 可解析程序；再用持久 Importer 读取被调的 *types.Signature。内建函数（len、
// append 等）在 go/types 中没有公开的签名访问器，按"无提示"降级。任何环节不成
// 立都返回 ok=false，由宿主隐藏提示——零行为变化。

import (
	"go/ast"
	"go/types"
	"strings"
)

// CallSig 是一次调用的签名快照（宿主层再映射为 LSP SignatureHelp）。
type CallSig struct {
	Label  string   // 例：Sprintf(format string, a ...any) string
	Params [][2]int // 各参数在 Label 内的字节区间（LSP ParameterInformation.label）
	Active int      // 光标所在参数序号（已按可变参数钳位）
}

const sigDummy = "GxSigX_"

type bracket struct {
	off int
	ch  byte
}

func closerOf(o byte) byte {
	switch o {
	case '(':
		return ')'
	case '[':
		return ']'
	case '{':
		return '}'
	}
	return 0
}

// skipLiteral 跨过字符串/字符/原始字面量与注释，返回下一个扫描位置；
// 无法收尾（未闭合字面量）返回 -1。
func skipLiteral(s string, i int) int {
	switch s[i] {
	case '\'', '"', '`':
		q := s[i]
		i++
		for i < len(s) {
			if q != '`' && s[i] == '\\' {
				i += 2
				continue
			}
			if s[i] == q {
				return i + 1
			}
			i++
		}
		return -1
	case '/':
		if i+1 >= len(s) {
			return i + 1
		}
		if s[i+1] == '/' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			return i
		}
		if s[i+1] == '*' {
			i += 2
			for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			return i + 2
		}
	}
	return i + 1
}

// unclosedStack 扫描 s 返回未闭合括号栈（类型感知配对）；配错或未闭合字面量
// 返回 ok=false（奇异输入，调用方降级）。
func unclosedStack(s string) ([]bracket, bool) {
	var st []bracket
	for i := 0; i < len(s); {
		switch c := s[i]; c {
		case '\'', '"', '`', '/':
			ni := skipLiteral(s, i)
			if ni < 0 {
				return nil, false
			}
			i = ni
		case '(', '[', '{':
			st = append(st, bracket{i, c})
			i++
		case ')', ']', '}':
			if len(st) == 0 || closerOf(st[len(st)-1].ch) != c {
				return nil, false
			}
			st = st[:len(st)-1]
			i++
		default:
			i++
		}
	}
	return st, true
}

// countTopCommas 统计一段实参文本里深度 0 的顶层逗号数。
func countTopCommas(s string) int {
	depth, n := 0, 0
	for i := 0; i < len(s); {
		switch c := s[i]; c {
		case '\'', '"', '`', '/':
			ni := skipLiteral(s, i)
			if ni < 0 {
				return n
			}
			i = ni
		case '(', '[', '{':
			depth++
			i++
		case ')', ']', '}':
			depth--
			i++
		case ',':
			if depth == 0 {
				n++
			}
			i++
		default:
			i++
		}
	}
	return n
}

// unwrapSig 逐层剥出可调用签名（指针解引用、命名类型取底层）。
func unwrapSig(t types.Type) *types.Signature {
	for {
		switch x := t.(type) {
		case *types.Signature:
			return x
		case *types.Pointer:
			t = x.Elem()
		case *types.Named:
			s, is := x.Underlying().(*types.Signature)
			if !is {
				return nil
			}
			return s
		default:
			return nil
		}
	}
}

func calleeName(fn ast.Expr) string {
	switch f := fn.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	case *ast.IndexExpr:
		return calleeName(f.X)
	case *ast.IndexListExpr:
		return calleeName(f.X)
	case *ast.ParenExpr:
		return calleeName(f.X)
	}
	return "func"
}

// Signature 在 cursor（字节偏移，视为最后输入字符之后）求所在调用签名。
// 触发不依赖具体字符（宿主层的 '(' / ',' 触发词约定在接线处执行），本层只认
// "光标在未闭合实参区内"这一事实，对空格、续行同样鲁棒。
func (in *Importer) Signature(src string, cursor int) (CallSig, bool) {
	if cursor <= 0 || cursor > len(src) {
		return CallSig{}, false
	}
	st, ok := unclosedStack(src[:cursor])
	if !ok {
		return CallSig{}, false
	}
	callOpen := -1
	for i := len(st) - 1; i >= 0; i-- {
		if st[i].ch == '(' {
			callOpen = st[i].off
			break
		}
	}
	if callOpen < 0 {
		return CallSig{}, false
	}

	// 组装 probe：截断后缀；按光标前最后一个实参字符决定是否补逗号分隔。
	k := cursor
	for k > 0 && (src[k-1] == ' ' || src[k-1] == '\t') {
		k--
	}
	delim := ","
	if k > 0 {
		if c := src[k-1]; c == '(' || c == ',' || isIdentChar(c) || c == ')' || c == ']' || c == '}' || c == '"' || c == '\'' || c == '`' {
			delim = ""
		}
		if k > 0 && isIdentChar(src[k-1]) {
			delim = "," // 正在输入的标识符后接占位符必须分隔
		}
	}
	probe := src[:cursor] + delim + sigDummy
	for i := len(st) - 1; i >= 0; i-- {
		probe += string(closerOf(st[i].ch))
	}

	_, info, file, ok := in.checkWithInfo(probe)
	if !ok || info == nil || file == nil {
		return CallSig{}, false
	}
	var best *ast.CallExpr
	ast.Inspect(file, func(n ast.Node) bool {
		if c, is := n.(*ast.CallExpr); is && in.fset.Position(c.Lparen).Offset == callOpen {
			best = c
		}
		return true
	})
	if best == nil {
		return CallSig{}, false
	}
	sg := unwrapSig(info.TypeOf(best.Fun))
	if sg == nil {
		return CallSig{}, false
	}
	q := func(p *types.Package) string {
		if p == nil || p.Path() == userPkgPath {
			return ""
		}
		return p.Name()
	}

	label := calleeName(best.Fun) + "("
	var params [][2]int
	pl := sg.Params()
	for i := 0; i < pl.Len(); i++ {
		if i > 0 {
			label += ", "
		}
		p := pl.At(i)
		txt := types.TypeString(p.Type(), q)
		if sg.Variadic() && i == pl.Len()-1 {
			if sl, is := p.Type().(*types.Slice); is {
				txt = "..." + types.TypeString(sl.Elem(), q)
			}
		}
		if p.Name() != "" {
			txt = p.Name() + " " + txt
		}
		s0 := len(label)
		label += txt
		params = append(params, [2]int{s0, len(label)})
	}
	label += ")"
	switch rl := sg.Results(); rl.Len() {
	case 1:
		r := rl.At(0)
		if r.Name() == "" {
			label += " " + types.TypeString(r.Type(), q)
		} else {
			label += " (" + r.Name() + " " + types.TypeString(r.Type(), q) + ")"
		}
	case 0:
	default:
		parts := []string{}
		for i := 0; i < rl.Len(); i++ {
			r := rl.At(i)
			txt := types.TypeString(r.Type(), q)
			if r.Name() != "" {
				txt = r.Name() + " " + txt
			}
			parts = append(parts, txt)
		}
		label += " (" + strings.Join(parts, ", ") + ")"
	}

	active := countTopCommas(src[callOpen+1 : cursor])
	if len(params) > 0 && active > len(params)-1 {
		active = len(params) - 1
	}
	return CallSig{Label: label, Params: params, Active: active}, true
}

// Signature 是一次性便捷入口（与 Check 用法一致）。
func Signature(src string, cursor int, lookup Lookup) (CallSig, bool) {
	return NewImporter(lookup).Signature(src, cursor)
}
