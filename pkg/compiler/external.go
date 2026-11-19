package compiler

import (
	"regexp"
	"strconv"
	"strings"
)

// external 转换：还原 selector 右侧的外部符号大小写。
//
// 约定：外部符号按 Go 真实拼写照抄即可，全大写缩写（URL、StatusOK）直接写
// 大写、原样保留；同时兼容「首字母小写」的旧写法（newRequest、uRL、method），
// 由 upperFirst 还原。大小写转换只作用于小写开头的标识符，全大写的不碰。
// 不读 export data、不查类型、无歧义。
//
// 作用域防误伤（Scope，在 pub 转换后收集）：
//   - selector 左侧是 import 包名 → 右侧是外部包符号 → upperFirst
//   - 右侧名字在自己 pubNames 表 → 用记录的导出名（自己的 pub 字段/方法）
//   - 右侧名字在自己 private 表 → 保持（自己的私有字段/变量/方法）
//   - 其余（左侧是变量 → 外部结构体字段/方法）→ upperFirst
//
// 裸标识符一律不碰（println/len/make 等内置与局部变量保持原样）；
// 自身 pub 符号的裸引用由 restoreRefs 负责。

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

// transformCompositeFields 还原「外部包类型复合字面量」的字段名。
//
// 复合字面量的字段名：小写形式（value → Value）由这里补大写；全大写字段
// （URL）原样保留。transformExternal 只处理 selector 右侧（x.sel），不覆盖
// lib.Box{value: 9} 这种复合字面量的字段名——这里补上。
//
// 只识别 pkg.Type{...}（pkg 是 import 名，类型可带泛型实例化 [T]）形态；
// 本包类型 point{x:1}、map 字面量、匿名结构、嵌套函数体等一律不碰。
func transformCompositeFields(in []Token, scope *Scope) []Token {
	out := append([]Token{}, in...)
	for i := 0; i < len(out); i++ {
		if out[i].Kind != Ident || !scope.HasImport(out[i].Text) {
			continue
		}
		brace := externalTypeBrace(out, i, scope)
		if brace < 0 {
			continue
		}
		promoteLiteralFields(out, brace)
	}
	return out
}

// externalTypeBrace 给定 import 包名下标，若其后是 .Type{ 或 .Type[T]{，
// 返回该 '{' 的下标，否则 -1。
func externalTypeBrace(toks []Token, pkgIdx int, scope *Scope) int {
	j := nextSig(toks, pkgIdx+1)
	if j < 0 || toks[j].Text != "." {
		return -1
	}
	j = nextSig(toks, j+1)
	if j < 0 || toks[j].Kind != Ident {
		return -1
	}
	// 可选泛型实例化 [T]
	n := nextSig(toks, j+1)
	if n >= 0 && toks[n].Text == "[" {
		close := matchBracket(toks, n)
		if close <= n {
			return -1
		}
		j = close
	}
	j = nextSig(toks, j+1)
	if j < 0 || toks[j].Text != "{" {
		return -1
	}
	return j
}

// promoteLiteralFields 把一个复合字面量顶层的字段名首字母大写。
// 只处理相对深度 0 的 `Ident :`；嵌套字面量由各自的识别单独处理。
func promoteLiteralFields(toks []Token, brace int) {
	close := matchDelim(toks, brace, "{", "}")
	if close <= brace {
		return
	}
	depth := 0
	for i := brace + 1; i < close; i++ {
		switch toks[i].Text {
		case "{", "(", "[":
			depth++
			continue
		case "}", ")", "]":
			depth--
			continue
		}
		if depth != 0 || toks[i].Kind != Ident || !isLowerStart(toks[i].Text) {
			continue
		}
		c := nextSig(toks, i+1)
		if c >= 0 && c <= close && toks[c].Text == ":" {
			toks[i].Text = upperFirst(toks[i].Text)
		}
	}
}

// transformExternal 还原 selector 右侧的外部符号。
// scope 提供私有符号保护；pubNames 提供自身导出符号映射（可能与外部同名，自身优先）。
func transformExternal(in []Token, scope *Scope, pubNames map[string]string) []Token {
	out := append([]Token{}, in...)
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

		left, sel := out[i].Text, out[k].Text

		// 左侧是 import 包名：右侧是外部包符号，机械 upperFirst
		if scope.HasImport(left) {
			out[k].Text = upperFirst(sel)
			continue
		}
		// 自己的 pub 字段/方法：用记录的导出名（自身优先）
		if real, ok := pubNames[sel]; ok {
			out[k].Text = real
			continue
		}
		// 自己的私有字段/变量/方法：保持小写
		if scope.IsPrivate(sel) {
			continue
		}
		// 左侧是变量：外部结构体字段/方法，机械 upperFirst
		out[k].Text = upperFirst(sel)
	}
	return out
}
