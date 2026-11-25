// Package syntax 提供终端 IDE 的轻量语法分词。
//
// 算法移植自 widgets/editor.go 的 highlight（GX 编辑器同源），保持
// "拼回去严格等于原文" 的契约；token.Color 存语义名，由 theme 解析成色值。
// 这里刻意不依赖 widgets/runtime，避免把 skia/cgo 拖进终端程序。
package syntax

// Token 是一个带语义色的词素。Color ∈ keyword/str/comment/number/tag/attr/punct/text。
type Token struct {
	Text  string
	Color string
}

// Highlight 把一行源码切成语义 token；拼接 Text 严格还原输入。
func Highlight(line string) []Token {
	out := make([]Token, 0, len(line)/2+1)
	i := 0
	inTag := false
	tagNamePending := false
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
				out = append(out, Token{line[i:j], "str"})
				i = j
			case line[i] == '{':
				close := matchBrace(line, i)
				out = append(out, Token{"{", "punct"})
				if close < len(line) {
					for _, tk := range Highlight(line[i+1 : close]) {
						out = append(out, tk)
					}
					out = append(out, Token{"}", "punct"})
					i = close + 1
				} else {
					i++
				}
			case isAlpha(line[i]):
				j := i + 1
				for j < len(line) && isAlnum(line[j]) {
					j++
				}
				for j+1 < len(line) && line[j] == '-' && isAlnum(line[j+1]) {
					j++
					for j < len(line) && isAlnum(line[j]) {
						j++
					}
				}
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
			out = append(out, Token{line[i:j], "str"})
			i = j
		case isAlpha(line[i]):
			j := i + 1
			for j < len(line) && isAlnum(line[j]) {
				j++
			}
			w := line[i:j]
			c := "text"
			if isKeyword(w) {
				c = "keyword"
			}
			out = append(out, Token{w, c})
			i = j
		case line[i] >= '0' && line[i] <= '9':
			j := i + 1
			for j < len(line) && isAlnum(line[j]) {
				j++
			}
			out = append(out, Token{line[i:j], "number"})
			i = j
		default:
			c := "punct"
			if line[i] == ' ' || line[i] == '\t' {
				c = "text"
			}
			out = append(out, Token{line[i : i+1], c})
			i++
		}
	}
	return out
}

func isAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_' || b >= 0x80
}
func isAlnum(b byte) bool { return isAlpha(b) || (b >= '0' && b <= '9') }

func isKeyword(w string) bool {
	switch w {
	case "func", "return", "if", "else", "for", "range", "var", "sig", "let",
		"pub", "type", "struct", "enum", "import", "package", "go", "defer",
		"nil", "true", "false", "any", "string", "int", "bool", "interface",
		"const", "switch", "case", "break", "continue", "class", "def", "fn",
		"export", "default", "new", "this", "try", "catch":
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
