package compiler

import (
	"regexp"
	"strconv"
	"strings"
)

// external 转换：把 selector 右侧的小写符号还原为 Go 侧真实大小写。
// 精确还原依赖 extsym（go/importer 读导出符号表），不靠首字母猜测：
//   - pkg.println → pkg.Println（包级，查该包符号表）
//   - req.method  → req.Method（字段/方法，查全局唯一表）
// 只作用于 X.sel 右侧；裸标识符不碰；已大写不动；歧义回退。

var versionSuffix = regexp.MustCompile(`^(.+)\.v[0-9]+$`)

func pkgNameFromPath(lit string) string {
	s, err := strconv.Unquote(lit)
	if err != nil {
		s = strings.Trim(lit, "\"`")
	}
	base := s
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	if m := versionSuffix.FindStringSubmatch(base); m != nil {
		base = m[1]
	}
	return base
}

// isLowerStart 判断标识符是否小写开头（rune 级，避免 UTF-8 截断）。
func isLowerStart(s string) bool {
	if s == "" {
		return false
	}
	return lowerFirst(s) == s && upperFirst(s) != s
}

func transformExternal(in []Token) []Token {
	specs := collectImportSpecs(in)
	if len(specs) == 0 {
		return in
	}
	ext := loadExtSymbols(specs)
	out := append([]Token{}, in...)

	importNames := map[string]bool{}
	for _, sp := range specs {
		importNames[sp.pkg] = true
	}

	for i := 0; i < len(out); i++ {
		if out[i].Kind != Ident {
			continue
		}
		// 定位 selector：out[i] . out[k]
		j := i + 1
		for j < len(out) && (out[j].Kind == Space || out[j].Kind == Comment) {
			j++
		}
		if j >= len(out) || out[j].Text != "." {
			continue
		}
		k := j + 1
		for k < len(out) && (out[k].Kind == Space || out[k].Kind == Comment) {
			k++
		}
		if k >= len(out) || out[k].Kind != Ident || !isLowerStart(out[k].Text) {
			continue
		}

		left := out[i].Text
		if importNames[left] {
			// 左侧是 import 包名：精确查该包符号表
			if real, ok := ext.restore(left, out[k].Text); ok {
				out[k].Text = real
			}
			continue
		}
		// 左侧是变量：查全局唯一表（字段/方法）
		if real, ok := ext.restore("", out[k].Text); ok {
			out[k].Text = real
		}
	}
	return out
}
