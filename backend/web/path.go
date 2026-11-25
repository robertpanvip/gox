package gxweb

// tokenizePath 把 SVG path 命令字母与数值拆成独立 token，兼容命令与数字粘连
// （"M4.375"）、逗号分隔、小数、指数与负号（"L-1.5e2"）。
func tokenizePath(d string) []string {
	tokens := make([]string, 0, len(d)/2)
	isCmd := func(r rune) bool {
		return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
	}
	isNumChar := func(r rune) bool {
		return (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '+' || r == 'e' || r == 'E'
	}
	runes := []rune(d)
	for i := 0; i < len(runes); {
		r := runes[i]
		if isCmd(r) {
			if r == 'e' || r == 'E' { // 数值里的指数符号（上一 token 是数字时）——简化处理为数字字符
				tokens = append(tokens, string(r))
				i++
				continue
			}
			tokens = append(tokens, string(r))
			i++
			continue
		}
		if isNumChar(r) {
			j := i
			for j < len(runes) && isNumChar(runes[j]) {
				j++
			}
			tokens = append(tokens, string(runes[i:j]))
			i = j
			continue
		}
		i++ // 逗号、空白等分隔符
	}
	return tokens
}
