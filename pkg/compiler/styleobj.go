package compiler

import (
	"strconv"
	"strings"
	"unicode"
)

// styleobj.go — JSX `style={{...}}` 的强类型结构体编译。
//
// GX 写法（key 用驼峰，值是普通 GX 表达式，可静态可动态；伪状态用单词 key）：
//
//	<div style={{
//	    background: lineGutterColor(idx),
//	    color: "#858585",
//	    fontSize: 12,
//	    textAlign: "right",
//	    hover: { bg: "#eee" },
//	}}>
//
// 编译成 gxscene.Style 结构体字面量（字段固定，编译期校验；标量统一 StyleValue）：
//
//	gxscene.Style{
//	    Background: lineGutterColor(idx),
//	    Color: "#858585",
//	    FontSize: "12",
//	    TextAlign: "right",
//	    Hover: &gxscene.Style{Bg: "#eee"},
//	}
//
// 设计要点：
//   - 普通属性 key 按 kebab→PascalCase 映射到固定字段；不在白名单的 key 报诊断，
//     不再生成 map[string]any 让错字段静默通过。
//   - 伪状态用单词 key（hover/active/focus/disabled），映射到 Hover/Active/...
//     字段；源码不出现 "&:" 符号，不做任何前缀字符串解析，避免转译歧义。
//   - 数字字面量加引号成 StyleValue 字符串（与 CSS 值语义一致）。
//   - 带引号 key 的 JSON 写法同样支持，但伪状态 key 必须是单词（hover，不是 &:hover）。

// stylePseudoFields 把伪状态单词 key 映射到 struct 字段名。
var stylePseudoFields = map[string]string{
	"hover": "Hover", "active": "Active", "focus": "Focus", "disabled": "Disabled",
}

// stylePropFields 是普通属性 kebab key → struct 字段名，与 runtime/scene/style.go
// 的 gx tag / PascalCase 一一对应。是字段名的唯一真相表。
var stylePropFields = map[string]string{
	"color": "Color", "bg": "Bg", "background": "Background",
	"width": "Width", "height": "Height", "font-size": "FontSize", "weight": "Weight",
	"padding": "Padding", "border-radius": "BorderRadius", "border": "Border",
	"border-width": "BorderWidth", "opacity": "Opacity",
	"flex-direction": "FlexDirection", "flex-wrap": "FlexWrap",
	"justify-content": "JustifyContent", "align-items": "AlignItems",
	"align-content": "AlignContent", "gap": "Gap", "row-gap": "RowGap",
	"column-gap": "ColumnGap", "position": "Position", "top": "Top",
	"right": "Right", "bottom": "Bottom", "left": "Left", "overflow": "Overflow",
	"scroll-top": "ScrollTop", "scroll-left": "ScrollLeft", "transform": "Transform",
	"transition": "Transition", "box-shadow": "BoxShadow", "white-space": "WhiteSpace",
	"flex-grow": "FlexGrow", "flex-shrink": "FlexShrink",
	"min-width": "MinWidth", "max-width": "MaxWidth",
	"min-height": "MinHeight", "max-height": "MaxHeight",
	"margin": "Margin", "margin-top": "MarginTop", "margin-bottom": "MarginBottom",
	"margin-left": "MarginLeft", "margin-right": "MarginRight",
	"text-align": "TextAlign", "line-height": "LineHeight",
	"letter-spacing": "LetterSpacing", "z-index": "ZIndex", "cursor": "Cursor",
}

// styleKeyToField 把源码 key 解析成 (字段名, 是否伪状态)。
// 普通 key：camel→kebab 后查表；伪状态只认单词 key（hover/active/focus/disabled）。
// 不再解析 "&:" / "$:" / ":" 前缀——源码不写 & 符号，避免字符串转译歧义。
func styleKeyToField(raw string) (field string, pseudo bool, ok bool) {
	key := raw
	// 兼容引号形式（带引号 key 在 styleObjToTokens 里已去引号，这里仍兜底）。
	if u, err := strconv.Unquote(key); err == nil {
		key = u
	}
	key = strings.TrimSpace(key)
	// 伪状态：只认单词 key，无前缀转译。
	if f, isP := stylePseudoFields[strings.ToLower(key)]; isP {
		return f, true, true
	}
	// 普通属性：camel → kebab → 查字段表。
	kebab := styleKeyCamelToKebab(key)
	if f, exists := stylePropFields[kebab]; exists {
		return f, false, true
	}
	return "", false, false
}

// styleKeyCamelToKebab 把驼峰属性名转成后端约定的短横线名。
//   - fontSize → font-size, textAlign → text-align, borderRadius → border-radius
//   - 已是短横线的 key 原样保留；含 ":" 的 key（如 "&:hover"）原样返回后查表失败，
//     由调用方报 style-unknown-prop——即源码不写 & 前缀。
func styleKeyCamelToKebab(s string) string {
	if s == "" {
		return s
	}
	if strings.ContainsAny(s, "-:") {
		return s
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
// "{ fontSize: 12, bg: \"#fff\", hover: { bg: \"#eee\" } }"）翻译成
// gxscene.Style{...} 结构体字面量 token 序列。
//
// 返回 (tokens, true) 表示解析成功；ok=false 表示文本不是可识别的对象字面量，
// 调用方应回退到既有诊断路径。未知 key 通过 ctx.report 报诊断并整体放弃（避免
// 半翻译产物）。
func styleObjToTokens(raw string, pos Pos, ctx *jsxSceneCtx) ([]Token, bool) {
	t := strings.TrimSpace(raw)
	if len(t) < 2 || t[0] != '{' || t[len(t)-1] != '}' {
		return nil, false
	}
	inner := t[1 : len(t)-1]
	toks := New(inner).AllTokens()
	if len(toks) > 0 && toks[len(toks)-1].Kind == EOF {
		toks = toks[:len(toks)-1]
	}
	segs := splitTopLevelSegs(toks, 0, len(toks))

	type field struct {
		name  string
		val   []Token
		ptr   bool // 伪状态字段用 &gxscene.Style{...}
	}
	var fields []field
	allOK := true
	for _, seg := range segs {
		 // 跳过尾部逗号产生的空段。
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
			allOK = false
			break
		}
		keyIdx := nextSigIn(toks, seg[0], colon)
		if keyIdx < 0 {
			allOK = false
			break
		}
		var keyRaw string
		switch toks[keyIdx].Kind {
		case Ident, String:
			keyRaw = toks[keyIdx].Text
		default:
			allOK = false
			break
		}
		fname, pseudo, ok := styleKeyToField(keyRaw)
		if !ok {
			// 未知属性：报明确诊断（列出正确写法提示），整体放弃。
			ctx.report("style-unknown-prop", toks[keyIdx].Pos,
				"style 未知属性 "+keyRaw+"：请用固定字段的驼峰名（如 fontSize / backgroundColor）或伪状态单词（hover/active/focus/disabled）")
			allOK = false
			break
		}
		valFrom := nextSigIn(toks, colon+1, seg[1])
		if valFrom < 0 || valFrom >= seg[1] {
			allOK = false
			break
		}
		valText := exprText(toks, valFrom, seg[1])
		if valText == "" {
			allOK = false
			break
		}
		var valToks []Token
		if pseudo {
			// 伪状态值必须是对象字面量 → 递归 gxscene.Style{...}，字段取地址。
			if isObjectLitText(valText) {
				if sub, ok := styleObjToTokens(valText, pos, ctx); ok {
					valToks = []Token{styleTokOp("&", pos)}
					valToks = append(valToks, sub...)
				}
			}
			if len(valToks) == 0 {
				ctx.report("style-pseudo-value", toks[keyIdx].Pos,
					"伪状态 "+keyRaw+" 的值必须是样式对象（如 hover: { bg: \"#eee\" }）")
				allOK = false
				break
			}
		} else {
			valToks = styleScalarTokens(valText, pos, ctx)
		}
		if len(valToks) == 0 {
			allOK = false
			break
		}
		fields = append(fields, field{name: fname, val: valToks, ptr: pseudo})
	}
	if !allOK {
		return nil, false
	}

	out := []Token{styleTokIdent("gxscene", pos), styleTokOp(".", pos),
		styleTokIdent("Style", pos), styleTokOp("{", pos)}
	for _, f := range fields {
		out = append(out, styleTokIdent(f.name, pos), styleTokOp(":", pos))
		out = append(out, f.val...)
		out = append(out, styleTokOp(",", pos))
	}
	out = append(out, styleTokOp("}", pos))
	return out, true
}

// styleScalarTokens 把单个普通属性的值表达式文本翻译成 token：
//   - 纯数字字面量 → 字符串字面量（StyleValue = string）
//   - 其余 → lexExprScene（补 sig 读 ()、For item 参数、嵌套 JSX）
func styleScalarTokens(valText string, pos Pos, ctx *jsxSceneCtx) []Token {
	v := strings.TrimSpace(valText)
	if isNumericLiteral(v) {
		return []Token{styleTokString(strconv.Quote(v), pos)}
	}
	return lexExprScene(valText, pos, ctx)
}

// isNumericLiteral 判断文本是否为 Go 数字字面量（含负号、小数、指数）。
func isNumericLiteral(s string) bool {
	if s == "" {
		return false
	}
	i := 0
	if s[0] == '+' || s[0] == '-' {
		i = 1
	}
	if i >= len(s) {
		return false
	}
	digits := 0
	for ; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			digits++
		} else if c != '.' && c != 'e' && c != 'E' && c != '+' && c != '-' {
			return false
		}
	}
	return digits > 0
}

// ---------- token 构造 helper（与 tokIdent/tokOp 等价，独立命名避免误用 IsGen 语义） ----------

func styleTokIdent(s string, p Pos) Token { return Token{Kind: Ident, Text: s, Pos: p, IsGen: true} }
func styleTokOp(s string, p Pos) Token    { return Token{Kind: Operator, Text: s, Pos: p, IsGen: true} }
func styleTokString(s string, p Pos) Token {
	return Token{Kind: String, Text: s, Pos: p, IsGen: true}
}
