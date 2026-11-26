// Package syntax 提供终端 IDE 的轻量语法分词。
//
// 算法移植自 widgets/editor.go 的 highlight（GX 编辑器同源），保持
// "拼回去严格等于原文" 的契约；token.Color 存语义名，由 theme 解析成色值。
// 这里刻意不依赖 widgets/runtime，避免把 skia/cgo 拖进终端程序。
//
// 语义色（比早期版本更细）：
//
//	keyword 关键字        fn 函数名（定义/调用/方法）
//	type    类型/枚举名    param 参数
//	bracket 括号 ()[]{}    punct 其它标点
//	str 字符串  comment 注释  number 数字  tag/attr 标签/属性  text 正文
package syntax

// Token 是一个带语义色的词素。Color 存语义名，由 theme 解析成色值。
type Token struct {
	Text  string
	Color string
}

// Highlight 把一行源码切成语义 token；拼接 Text 严格还原输入。
//
// 由于只做单行分词，跨行结构（多行函数签名）无法完整推断，但通过
// "标识符后紧跟 ("、"func/enum/type 后的名字"、括号深度内的首个标识符"
// 等局部启发式，可区分函数、类型/枚举、参数与括号。
func Highlight(line string) []Token {
	out := make([]Token, 0, len(line)/2+1)
	i := 0
	inTag := false
	tagNamePending := false

	// 行内状态机。
	expectFn := false        // 刚读到 func/fn/def，下一个标识符是函数名
	expectType := false      // 刚读到 type/enum/struct/interface，下一个标识符是类型名
	afterDot := false        // 刚读过 '.'，下一个标识符是成员（方法/字段）
	bracketDepth := 0        // 圆括号/方括号嵌套深度（>0 处于参数列表）
	afterCommaInArgs := true // 刚进入参数列表或逗号后，下一个标识符可能是参数名

	for i < len(line) {
		if inTag {
			switch {
			case line[i] == '>':
				out = append(out, Token{">", "punct"})
				i++
				inTag = false
			case line[i] == '/' && i+1 < len(line) && line[i+1] == '>':
				out = append(out, Token{"/>", "punct"})
				i += 2
				inTag = false
			case line[i] == '"':
				j := scanQuoted(line, i)
				out = append(out, Token{line[i:j], "str"})
				i = j
			case line[i] == '{':
				close := matchBrace(line, i)
				out = append(out, Token{"{", "bracket"})
				if close < len(line) {
					for _, tk := range Highlight(line[i+1 : close]) {
						out = append(out, tk)
					}
					out = append(out, Token{"}", "bracket"})
					i = close + 1
				} else {
					i++
				}
			case isAlpha(line[i]):
				j := scanWord(line, i, true)
				c := "attr"
				if tagNamePending {
					c = "tag"
					tagNamePending = false
				}
				out = append(out, Token{line[i:j], c})
				i = j
			default:
				c := "punct"
				if line[i] == ' ' || line[i] == '\t' {
					c = "text"
				}
				out = append(out, Token{line[i : i+1], c})
				i++
			}
			continue
		}

		switch {
		case line[i] == '<' && i+1 < len(line) && (isAlpha(line[i+1]) || line[i+1] == '/'):
			out = append(out, Token{"<", "punct"})
			i++
			inTag = true
			tagNamePending = true
			if i < len(line) && line[i] == '/' {
				out = append(out, Token{"/", "punct"})
				i++
			}

		case i+1 < len(line) && line[i] == '/' && line[i+1] == '/':
			out = append(out, Token{line[i:], "comment"})
			i = len(line)

		case line[i] == '"':
			j := scanQuoted(line, i)
			out = append(out, Token{line[i:j], "str"})
			i = j

		case line[i] >= '0' && line[i] <= '9':
			j := i + 1
			for j < len(line) && isAlnum(line[j]) {
				j++
			}
			out = append(out, Token{line[i:j], "number"})
			i = j

		case isAlpha(line[i]):
			j := scanWord(line, i, false)
			w := line[i:j]
			called := nextNonSpaceIs(line, j, '(')

			c := "text"
			switch {
			case expectFn:
				c = "fn"
				expectFn = false
				afterCommaInArgs = true // 函数名后的 ( 进入参数列表
			case expectType:
				c = "type"
				expectType = false
			case isKeyword(w):
				c = "keyword"
				switch w {
				case "func", "fn", "def":
					expectFn = true
				case "type", "enum", "struct", "interface", "class":
					expectType = true
				}
			case isBuiltinType(w):
				c = "type"
			case afterDot && called:
				c = "fn" // obj.Method( → 方法调用
			case afterDot:
				c = "attr" // obj.Field → 字段
			case bracketDepth > 0 && afterCommaInArgs && !called:
				c = "param" // 参数列表里的参数名/实参位置
				afterCommaInArgs = false
			case called:
				c = "fn" // 普通函数调用
			case isUpperStart(w):
				c = "type" // 大写开头的标识符按类型惯例着色
			}
			out = append(out, Token{w, c})
			i = j
			afterDot = false

		default: // 标点与空白
			b := line[i]
			switch b {
			case '(', ')', '[', ']':
				out = append(out, Token{line[i : i+1], "bracket"})
				if b == '(' || b == '[' {
					bracketDepth++
					// 函数调用/声明的第一个 ( 进入"期待参数名"状态。
					if len(out) >= 2 {
						prev := out[len(out)-2]
						if prev.Color == "fn" || prev.Color == "keyword" {
							afterCommaInArgs = true
						}
					}
				} else if bracketDepth > 0 {
					bracketDepth--
				}
			case '{', '}':
				out = append(out, Token{line[i : i+1], "bracket"})
			case ',':
				out = append(out, Token{",", "punct"})
				if bracketDepth > 0 {
					afterCommaInArgs = true
				}
			case '.':
				out = append(out, Token{".", "punct"})
				afterDot = true
			default:
				c := "punct"
				if b == ' ' || b == '\t' {
					c = "text"
				} else {
					afterDot = false
				}
				out = append(out, Token{line[i : i+1], c})
			}
			i++
		}
	}
	return out
}

// scanQuoted 扫描一个双引号字符串（含转义），返回值结束后的下标。
func scanQuoted(line string, i int) int {
	j := i + 1
	for j < len(line) && line[j] != '"' {
		if line[j] == '\\' && j+1 < len(line) {
			j++
		}
		j++
	}
	if j < len(line) {
		j++
	}
	return j
}

// scanWord 扫描一个标识符。inTag 时额外纳入 '-'，兼容 data-foo 这类属性名。
func scanWord(line string, i int, inTag bool) int {
	j := i + 1
	for j < len(line) && isAlnum(line[j]) {
		j++
	}
	if inTag {
		for j+1 < len(line) && line[j] == '-' && isAlnum(line[j+1]) {
			j++
			for j < len(line) && isAlnum(line[j]) {
				j++
			}
		}
	}
	return j
}

// nextNonSpaceIs 判断 end 之后（跳过空白）是否是字节 b。
func nextNonSpaceIs(line string, end int, b byte) bool {
	j := end
	for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
		j++
	}
	return j < len(line) && line[j] == b
}

func isAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_' || b >= 0x80
}
func isAlnum(b byte) bool { return isAlpha(b) || (b >= '0' && b <= '9') }

func isUpperStart(w string) bool { return w != "" && w[0] >= 'A' && w[0] <= 'Z' }

func isBuiltinType(w string) bool {
	switch w {
	case "string", "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"byte", "rune", "float32", "float64", "complex64", "complex128",
		"bool", "any", "error":
		return true
	}
	return false
}

func isKeyword(w string) bool {
	switch w {
	case "return", "if", "else", "for", "range", "var", "sig", "let",
		"pub", "import", "package", "go", "defer", "class", "def", "fn",
		"export", "default", "new", "this", "try", "catch", "await",
		"nil", "true", "false", "func", "type", "enum", "struct", "interface",
		"const", "switch", "case", "break", "continue":
		return true
	}
	return false
}

func matchBrace(line string, start int) int {
	depth, i := 0, start
	for i < len(line) {
		if line[i] == '"' {
			i++
			for i < len(line) && line[i] != '"' {
				if line[i] == '\\' && i+1 < len(line) {
					i++
				}
				i++
			}
		} else if line[i] == '{' {
			depth++
		} else if line[i] == '}' {
			depth--
			if depth == 0 {
				return i
			}
		}
		i++
	}
	return len(line)
}
