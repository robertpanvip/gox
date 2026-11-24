package compiler

import (
	"strconv"
	"strings"
	"unicode"
)

// styleobj.go — JSX `style={{...}}` 的 react-native 风格对象字面量编译。
//
// GX 写法（key 用驼峰，值是普通 GX 表达式，可静态可动态）：
//
//	<div style={{
//	    background: lineGutterColor(idx),
//	    color: "#858585",
//	    fontSize: 12,
//	    textAlign: "right",
//	    "&:hover": { bg: "#eee" },
//	}}>
//
// 编译成 gxscene.Style 复合字面量（runtime 的 writeAttr 会展平为后端 props，
// 并按 key 做细粒度 diff）：
//
//	gxscene.Style{
//	    "background": lineGutterColor(idx),
//	    "color": "#858585",
//	    "font-size": 12,
//	    "text-align": "right",
//	    "&:hover": gxscene.Style{"bg": "#eee"},
//	}
//
// 与既有的「带引号 key 的 JSON」路径（走 gxscene.parseStyle 静态解析）互补：
// 本路径支持无引号驼峰 key + 动态表达式值；JSON 路径保持兼容不动。

// styleKeyCamelToKebab 把驼峰属性名转成后端约定的短横线名。
//   - fontSize → font-size, textAlign → text-align, borderRadius → border-radius
//   - 已是短横线 / 含 "&:" "$:" 的 key 原样保留（伪状态、显式短横线）
//   - 全小写单词（color/width/background/bg…）原样保留
func styleKeyCamelToKebab(s string) string {
	if s == "" {
		return s
	}
	if strings.ContainsAny(s, "-:") {
		return s // 短横线或伪状态 key，保持原样
	}
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// styleObjToTokens 把 style 属性的对象字面量文本（含两端花括号，如
// "{ fontSize: 12, bg: \"#fff\" }"）翻译成 gxscene.Style{...} token 序列。
//
// 返回 (tokens, true) 表示解析成功；ok=false 表示文本不是可识别的「驼峰 key
// 对象字面量」，调用方应回退到既有 JSON / 诊断路径。
//
// pos 用于生成 token 的位置；ctx 传给 lexExprScene 以补 sig 读 () / For item。
func styleObjToTokens(raw string, pos Pos, ctx *jsxSceneCtx) ([]Token, bool) {
	t := strings.TrimSpace(raw)
	if len(t) < 2 || t[0] != '{' || t[len(t)-1] != '}' {
		return nil, false
	}
	inner := t[1 : len(t)-1]
	if strings.TrimSpace(inner) == "" {
		return []Token{styleTokIdent("gxscene", pos), styleTokOp(".", pos),
			styleTokIdent("Style", pos), styleTokOp("{", pos), styleTokOp("}", pos)}, true
	}
	toks := New(inner).AllTokens()
	// 去掉末尾 EOF，避免它被当作有效 token 进入分段/取值判断。
	if len(toks) > 0 && toks[len(toks)-1].Kind == EOF {
		toks = toks[:len(toks)-1]
	}
	segs := splitTopLevelSegs(toks, 0, len(toks))
	if len(segs) == 0 {
		return nil, false
	}

	// 先解析所有字段；若任一段不是 key: value 形态，整体放弃（避免半翻译）。
	type field struct {
		key  string
		val  []Token
		from int // 段起点，用于定位诊断（当前不报，保留）
	}
	var fields []field
	for _, seg := range segs {
		// 跳过尾部逗号产生的空段（只有空白 / EOF）。
		firstSig := -1
		for k := seg[0]; k < seg[1] && k < len(toks); k++ {
			if toks[k].Kind == Space || toks[k].Kind == Newline || toks[k].Kind == Comment || toks[k].Kind == EOF {
				continue
			}
			firstSig = k
			break
		}
		if firstSig < 0 {
			continue
		}
		colon := -1
		depth := 0
		for k := seg[0]; k < seg[1]; k++ {
			switch toks[k].Text {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				if depth > 0 {
					depth--
				}
			case ":":
				if depth == 0 {
					colon = k
					break
				}
			}
		}
		if colon < 0 {
			return nil, false
		}
		keyIdx := nextSigIn(toks, seg[0], colon)
		if keyIdx < 0 {
			return nil, false
		}
		var key string
		switch toks[keyIdx].Kind {
		case Ident:
			key = styleKeyCamelToKebab(toks[keyIdx].Text)
		case String:
			// 带引号 key（伪状态 "&:hover" 或显式短横线）：去引号后原样保留。
			if u, err := strconv.Unquote(toks[keyIdx].Text); err == nil {
				key = u
			} else {
				key = strings.Trim(toks[keyIdx].Text, "\"'`")
			}
		default:
			return nil, false
		}
		if key == "" {
			return nil, false
		}
		valFrom := nextSigIn(toks, colon+1, seg[1])
		valTo := seg[1]
		if valFrom < 0 || valFrom >= valTo {
			return nil, false
		}
		valText := exprText(toks, valFrom, valTo)
		if valText == "" {
			return nil, false
		}
		valToks := styleValueTokens(valText, pos, ctx)
		if len(valToks) == 0 {
			return nil, false
		}
		fields = append(fields, field{key: key, val: valToks, from: seg[0]})
	}

	out := []Token{styleTokIdent("gxscene", pos), styleTokOp(".", pos),
		styleTokIdent("Style", pos), styleTokOp("{", pos)}
	for _, f := range fields {
		out = append(out, styleTokString(strconv.Quote(f.key), pos), styleTokOp(":", pos))
		out = append(out, f.val...)
		out = append(out, styleTokOp(",", pos))
	}
	out = append(out, styleTokOp("}", pos))
	return out, true
}

// styleValueTokens 把单个 style 字段的值表达式文本翻译成 token：
//   - 嵌套对象字面量 → 递归 gxscene.Style{...}（伪状态子表）
//   - 其余 → lexExprScene（补 sig 读 ()、For item 参数、嵌套 JSX）
func styleValueTokens(valText string, pos Pos, ctx *jsxSceneCtx) []Token {
	if isObjectLitText(valText) {
		if toks, ok := styleObjToTokens(valText, pos, ctx); ok {
			return toks
		}
	}
	return lexExprScene(valText, pos, ctx)
}

// ---------- token 构造 helper（与 tokIdent/tokOp 等价，独立命名避免误用 IsGen 语义） ----------

func styleTokIdent(s string, p Pos) Token { return Token{Kind: Ident, Text: s, Pos: p, IsGen: true} }
func styleTokOp(s string, p Pos) Token    { return Token{Kind: Operator, Text: s, Pos: p, IsGen: true} }
func styleTokString(s string, p Pos) Token {
	return Token{Kind: String, Text: s, Pos: p, IsGen: true}
}
