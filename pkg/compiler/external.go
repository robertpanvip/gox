package compiler

import (
	"fmt"
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

// reportCaseMismatch 追加一条大小写诊断。diags 为 nil 时静默（单阶段/单文件路径）。
func reportCaseMismatch(diags *[]GXDiagnostic, p Pos, code, msg string) {
	if diags != nil {
		*diags = append(*diags, GXDiagnostic{Pos: p, Code: code, Message: msg})
	}
}

// caseFoldTopLevelExport 在 GX 导出表里按大小写不敏感查找 sel 对应的唯一导出名。
// 仅考虑顶层符号（key 不含 '.'），避免误匹配字段限定名。
// 返回唯一候选导出名；0 个或多个候选、或 sel 已是原名/导出名本身，返回 ""。
func caseFoldTopLevelExport(exports map[string]string, sel string) string {
	candidates := map[string]bool{}
	for k, v := range exports {
		if strings.Contains(k, ".") {
			continue
		}
		if strings.EqualFold(k, sel) || strings.EqualFold(v, sel) {
			candidates[v] = true
		}
	}
	if len(candidates) != 1 {
		return ""
	}
	return soleCaseCandidate(exports, sel, candidates, func(k string) (string, string, bool) {
		if strings.Contains(k, ".") {
			return "", "", false
		}
		return k, exports[k], true
	})
}

// caseFoldGXField 在 GX 字段导出表（"Type.field" → "Field"）里按大小写不敏感
// 查找 field 对应的唯一导出字段名；0/多候选或已是正确拼写返回 ""。
func caseFoldGXField(exports map[string]string, typeName, field string) string {
	candidates := map[string]bool{}
	for k, v := range exports {
		dot := strings.Index(k, ".")
		if dot < 0 {
			continue
		}
		typ, orig := k[:dot], k[dot+1:]
		if typ != typeName && typ != upperFirst(typeName) {
			continue
		}
		if strings.EqualFold(orig, field) || strings.EqualFold(v, field) {
			candidates[v] = true
		}
	}
	if len(candidates) != 1 {
		return ""
	}
	return soleCaseCandidate(exports, field, candidates, func(k string) (string, string, bool) {
		dot := strings.Index(k, ".")
		if dot < 0 {
			return "", "", false
		}
		typ, orig := k[:dot], k[dot+1:]
		if typ != typeName && typ != upperFirst(typeName) {
			return "", "", false
		}
		return orig, exports[k], true
	})
}

// soleCaseCandidate 在唯一候选时，排除书写本身已是原名或导出名的情况，
// 再返回该导出名；否则返回 ""。each 负责从表项 key 解出 (原名, 导出名, 是否参与)。
func soleCaseCandidate(exports map[string]string, written string, candidates map[string]bool, each func(k string) (string, string, bool)) string {
	var only string
	for v := range candidates {
		only = v
	}
	if written == only {
		return ""
	}
	for k := range exports {
		orig, exp, ok := each(k)
		if ok && written == orig && exp == only {
			return "" // 原名本身就是正确写法（还原路径已处理）
		}
	}
	return only
}

// transformCompositeFields 还原「外部包类型复合字面量」的字段名。
//
// 复合字面量的字段名：小写形式（value → Value）由这里补大写；全大写字段
// （URL）原样保留。transformExternal 只处理 selector 右侧（x.sel），不覆盖
// lib.Box{value: 9} 这种复合字面量的字段名——这里补上。
//
// 只识别 pkg.Type{...}（pkg 是 import 名，类型可带泛型实例化 [T]）形态；
// 本包类型 point{x:1}、map 字面量、匿名结构、嵌套函数体等一律不碰。
func transformCompositeFields(in []Token, scope *Scope, diags *[]GXDiagnostic) []Token {
	out := append([]Token{}, in...)
	for i := 0; i < len(out); i++ {
		if out[i].Kind != Ident {
			continue
		}
		info, ok := scope.ImportOf(out[i].Text)
		if !ok {
			continue
		}
		brace, typeName := externalTypeBrace(out, i)
		if brace < 0 {
			continue
		}
		if info.Origin == ImportGX {
			// GX 模块来源：字段名严格按被导入方 pub 导出表查 Type.field，
			// 不做 upperFirst 猜测。查不到但能大小写不敏感唯一匹配时给出诊断；
			// 仍保持书写原样（不改产物），诊断交给检查器/LSP 消费。
			promoteGXFields(out, brace, typeName, info.Exports, diags)
			continue
		}
		promoteLiteralFields(out, brace) // Go 来源维持现状
	}
	return out
}

// externalTypeBrace 给定 import 包名下标，若其后是 .Type{ 或 .Type[T]{，
// 返回该 '{' 的下标与类型名（书写方的写法），否则 (-1, "")。
func externalTypeBrace(toks []Token, pkgIdx int) (int, string) {
	j := nextSig(toks, pkgIdx+1)
	if j < 0 || toks[j].Text != "." {
		return -1, ""
	}
	j = nextSig(toks, j+1)
	if j < 0 || toks[j].Kind != Ident {
		return -1, ""
	}
	typeName := toks[j].Text
	// 可选泛型实例化 [T]
	n := nextSig(toks, j+1)
	if n >= 0 && toks[n].Text == "[" {
		close := matchBracket(toks, n)
		if close <= n {
			return -1, ""
		}
		j = close
	}
	j = nextSig(toks, j+1)
	if j < 0 || toks[j].Text != "{" {
		return -1, ""
	}
	return j, typeName
}

// promoteGXFields 对 GX 模块来源的复合字面量，按 Type.field 限定名查导出表还原字段名。
// 同时尝试书写方的类型名与其 upperFirst 形式（兼容小写/大写类型名引用），
// 以及字段原名与 upperFirst 形式；命中即用被导入方导出名，未命中保持原样。
func promoteGXFields(toks []Token, brace int, typeName string, exports map[string]string, diags *[]GXDiagnostic) {
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
		if depth != 0 || toks[i].Kind != Ident {
			continue
		}
		c := nextSig(toks, i+1)
		if c < 0 || c > close || toks[c].Text != ":" {
			continue
		}
		field := toks[i].Text
		// 查表顺序：书写类型名.字段原名 → 大写类型名.字段原名 → 各自的大写字段形式。
		keys := []string{
			typeName + "." + field,
			upperFirst(typeName) + "." + field,
			typeName + "." + upperFirst(field),
			upperFirst(typeName) + "." + upperFirst(field),
		}
		matched := false
		for _, k := range keys {
			if real, ok := exports[k]; ok {
				toks[i].Text = real
				matched = true
				break
			}
		}
		// 查表未命中：能做大小写不敏感唯一匹配时给出诊断（不改产物）。
		if !matched {
			if real := caseFoldGXField(exports, typeName, field); real != "" {
				reportCaseMismatch(diags, toks[i].Pos, "import-field-case",
					fmt.Sprintf("字段 %q 大小写错误：类型 %s 导出的是 %q", field, typeName, real))
			}
		}
	}
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
// diags 非 nil 时收集 GX 来源的大小写错误诊断（位置为书写标识符的原始 GX 坐标）。
func transformExternal(in []Token, scope *Scope, pubNames map[string]string, diags *[]GXDiagnostic) []Token {
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
		if k >= len(out) || out[k].Kind != Ident {
			continue
		}

		left, sel := out[i].Text, out[k].Text

		// 左侧是 import 包名：右侧是该包符号，按来源分支还原大小写。
		// 注意：GX 来源必须放在 isLowerStart 门槛之前——写错大小写（ADD/Add）
		// 本身就是大写开头，若先过门槛会被直接跳过，永远查不到诊断。
		if info, ok := scope.ImportOf(left); ok {
			if info.Origin == ImportGX {
				// GX 模块来源：严格查被导入方导出表，不做 upperFirst 猜测。
				if real, exists := info.Exports[sel]; exists {
					out[k].Text = real // 书写方用小写原名 → 还原为被导入方导出名
				} else if _, isExport := info.Exports[upperFirst(sel)]; isExport {
					// 书写方已用导出名（渐进迁移）：原样保留。
				} else if real := caseFoldTopLevelExport(info.Exports, sel); real != "" {
					// 写错大小写：不改产物，交给检查器/LSP 报诊断。
					reportCaseMismatch(diags, out[k].Pos, "import-case",
						fmt.Sprintf("符号 %q 大小写错误：包 %s 导出的是 %q", sel, info.Path, real))
				}
				// 真正拼写错误或被导入方私有符号：不在表中也无大小写候选，保持原样交 Go。
				continue
			}
			// ImportGo：维持机械 upperFirst（仅小写开头）。
			if isLowerStart(sel) {
				out[k].Text = upperFirst(sel)
			}
			continue
		}

		if !isLowerStart(sel) {
			continue
		}
		// 自己的 pub 字段/方法：用记录的导出名（自身优先）
		if real, ok := pubNames[sel]; ok {
			out[k].Text = real
			continue
		}
		// 自己的私有字段/方法（selector 右侧）：保持小写。
		// 只认 struct 字段/方法名——局部变量/参数名即使同名也不保护 selector（L-7）。
		if scope.IsPrivateField(sel) {
			continue
		}
		// 左侧是变量：外部结构体字段/方法，机械 upperFirst
		out[k].Text = upperFirst(sel)
	}
	return out
}
