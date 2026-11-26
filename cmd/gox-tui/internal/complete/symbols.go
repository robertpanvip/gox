package complete

import "strings"

// extractSymbols 从源码文本抽取声明符号，够补全用。
func extractSymbols(src string) []string {
	var out []string
	seen := map[string]bool{}
	for _, ln := range strings.Split(src, "\n") {
		t := strings.TrimSpace(ln)
		if len(t) > 200 || strings.HasPrefix(t, "//") {
			continue
		}
		for _, k := range []string{"func ", "type ", "struct ", "interface ", "class ", "def ", "sig ", "fn ", "pub fn ", "var ", "const "} {
			if !strings.HasPrefix(t, k) {
				continue
			}
			name := symbolAfter(t[len(k):])
			if name != "" && !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
			break
		}
	}
	return out
}

func symbolAfter(s string) string {
	end := 0
	for end < len(s) {
		c := s[end]
		if c == '(' || c == '{' || c == ' ' || c == '\t' || c == '=' || c == ':' || c == '[' {
			break
		}
		end++
	}
	return s[:end]
}
