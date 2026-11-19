package compiler

import (
	"regexp"
	"strconv"
	"strings"
)

// external 转换：按「只首字母小写、其余驼峰照抄」的书写约定还原外部符号。
//
// 约定：Go 侧 NewRequest 在 gx 里写 newRequest；URL 写 uRL；Method 写 method。
// 还原因此退化为机械的 upperFirst——不读 export data、不查类型、无歧义。
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
