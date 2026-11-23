package gxscene

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Style 是「受限 CSS sheet」：一个 key 集合，key 必须是 StylePropNames 里的普通
// 属性，或以 "&:" 开头的受支持伪状态；伪状态的值必须也是 Style。除此之外一律
// 拒绝——这是「受限」的硬约束（区别于任意 key 都能塞的 Props）。
//
// Go 写法：
//
//	gxscene.Style{
//	    "bg": "#3b82f6", "color": "#fff", "padding": "11",
//	    "&:hover":  gxscene.Style{"bg": "#2563eb"},
//	    "&:active": gxscene.Style{"bg": "#1d4ed8", "transform": "scale(.98)"},
//	}
//
// JSON / GX 对象字面量同样用 "&:hover" 作为 key。伪状态只作用于本节点自身
// （& 语义），不支持后代选择器；不嵌套伪状态（子表里再写 &: 会被 Validate 拒绝
// 的第二层拦截——实际上子表允许但展平时忽略，见 Flatten 注释）。
type Style map[string]any

// 伪状态前缀。规范形式 "&:"；"$:" 是早期约定的别名，ParseStyle/Flatten/Props
// 会统一规整成 "&:"，保证后端只看到一种 key。
const (
	PseudoPrefix       = "&:"
	legacyPseudoPrefix = "$:"

	PseudoHover    = "&:hover"
	PseudoActive   = "&:active"
	PseudoFocus    = "&:focus"
	PseudoDisabled = "&:disabled"
)

// StylePropNames 是 Style 允许的普通属性白名单，覆盖两个后端 style 解析器已支持的
// 全部属性。想加新属性必须同时改这里 + 两后端解析，避免「host 收、后端忽略」的静默错值。
var StylePropNames = []string{
	"color", "bg", "background", "width", "height", "font-size", "weight",
	"padding", "border-radius", "border", "border-width", "opacity",
	"flex-direction", "flex-wrap", "justify-content", "align-items", "align-content",
	"gap", "row-gap", "column-gap",
	"position", "top", "right", "bottom", "left",
	"overflow", "scroll-top", "scroll-left",
	"transform", "transition", "box-shadow",
}

var stylePropSet = func() map[string]bool {
	m := make(map[string]bool, len(StylePropNames))
	for _, k := range StylePropNames {
		m[k] = true
	}
	return m
}()

var pseudoStates = map[string]bool{
	"hover": true, "active": true, "focus": true, "disabled": true,
}

// canonicalPseudo 把 "&:x" / "$:x" 统一成 "&:x"；非伪状态 key 原样返回。
func canonicalPseudo(k string) string {
	for _, p := range []string{PseudoPrefix, legacyPseudoPrefix} {
		if strings.HasPrefix(k, p) {
			return PseudoPrefix + strings.TrimPrefix(k, p)
		}
	}
	return k
}

func isPseudoKey(k string) bool {
	return strings.HasPrefix(k, PseudoPrefix) || strings.HasPrefix(k, legacyPseudoPrefix)
}

func isKnownPseudo(k string) bool {
	c := canonicalPseudo(k)
	name := strings.TrimPrefix(c, PseudoPrefix)
	return pseudoStates[name]
}

func asStyle(v any) (Style, bool) {
	switch x := v.(type) {
	case Style:
		return x, true
	case map[string]any:
		return Style(x), true
	default:
		return nil, false
	}
}

func isStyleScalar(v any) bool {
	switch v.(type) {
	case nil, string, bool, int, int64, int32, float64, float32:
		return true
	default:
		return false
	}
}

// Validate 校验样式表：普通 key 必须在白名单内；伪状态 key 必须是受支持的
// &:/$: 状态且值为 Style。返回首条错误，便于编译器/测试定位。
func (s Style) Validate() error {
	for k, v := range s {
		if isPseudoKey(k) {
			if !isKnownPseudo(k) {
				return fmt.Errorf("style: 未知伪状态 %q（支持 %s）", k, strings.Join(pseudoStateList(), ", "))
			}
			sub, ok := asStyle(v)
			if !ok {
				return fmt.Errorf("style: 伪状态 %q 的值必须是样式表", k)
			}
			if err := sub.Validate(); err != nil {
				return fmt.Errorf("style: %s: %w", k, err)
			}
			continue
		}
		if !stylePropSet[strings.ToLower(k)] {
			return fmt.Errorf("style: 未知属性 %q（不在受限 CSS sheet 白名单）", k)
		}
	}
	return nil
}

func pseudoStateList() []string {
	out := make([]string, 0, len(pseudoStates))
	for s := range pseudoStates {
		out = append(out, PseudoPrefix+s)
	}
	sort.Strings(out)
	return out
}

// encodeSubStyle 把子样式表编码成后端可解析的 JSON 对象串（只保留标量）。
// 伪状态 key 规整为 "&:"；子表里的嵌套伪状态忽略（不支持）。
func encodeSubStyle(sub Style) string {
	obj := make(map[string]any, len(sub))
	for k, v := range sub {
		if isPseudoKey(k) || !isStyleScalar(v) {
			continue
		}
		obj[canonicalPseudo(k)] = v
	}
	b, _ := json.Marshal(obj)
	return string(b)
}

// Props 把样式表展平成后端 props：普通属性直出；伪状态子表编码成
// props["&:hover"] = `{"bg":"#2563eb"}` 这样的紧凑串，交给后端按命中状态覆盖。
// 调用前应先 Validate（本方法不重复校验，保持热路径轻量）。
func (s Style) Props() Props {
	p := make(Props, len(s))
	for k, v := range s {
		if isPseudoKey(k) {
			if sub, ok := asStyle(v); ok && len(sub) > 0 {
				key := canonicalPseudo(k)
				encoded := encodeSubStyle(sub)
				if encoded != "{}" && encoded != "" {
					p[key] = encoded
				}
			}
			continue
		}
		p[k] = v
	}
	return p
}

// Flatten 按当前激活的伪状态把样式表合并成一份扁平 props（host/Skia headless
// 测试用，模拟后端的覆盖行为）。顺序：先普通属性，再按 state 列表依次覆盖，
// 后出现的伪状态优先级更高。子表里的嵌套伪状态忽略。
func (s Style) Flatten(states ...string) Props {
	out := Props{}
	for k, v := range s {
		if !isPseudoKey(k) {
			out[k] = v
		}
	}
	for _, st := range states {
		name := strings.TrimPrefix(strings.TrimPrefix(st, PseudoPrefix), legacyPseudoPrefix)
		// 原始 Style 可能用 &: 或 $: 存 key（未经 Props 规整），两种都查。
		sub, ok := asStyle(s[PseudoPrefix+name])
		if !ok {
			sub, ok = asStyle(s[legacyPseudoPrefix+name])
		}
		if ok {
			for sk, sv := range sub {
				if !isPseudoKey(sk) {
					out[sk] = sv
				}
			}
		}
	}
	return out
}

// ParseStyle 从 JSON 对象字符串解析样式表。"&:hover"/"$:hover" 这类 key 递归
// 解析为子 Style，其余 key 保留标量值。解析后自动 Validate。
func ParseStyle(src string) (Style, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(src), &raw); err != nil {
		return nil, fmt.Errorf("style: 非法 JSON: %w", err)
	}
	s := make(Style, len(raw))
	for k, r := range raw {
		if isPseudoKey(k) {
			var sub Style
			if err := json.Unmarshal(r, &sub); err != nil {
				return nil, fmt.Errorf("style: %q 的值必须是样式对象: %w", k, err)
			}
			s[canonicalPseudo(k)] = sub
			continue
		}
		var v any
		if err := json.Unmarshal(r, &v); err != nil {
			return nil, fmt.Errorf("style: 属性 %q 解析失败: %w", k, err)
		}
		s[k] = v
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return s, nil
}

// ApplyStyle 把 Style 展平后写入目标 Props（供 host 的 slotAttr 写入路径调用）。
// 只处理 key == "style" 的值：若 v 是 Style / map[string]any / JSON 字符串，
// 展平并合并进 dst；否则原样写回 key。返回是否发生了样式展开。
//
// 合并语义：Style 的普通属性与伪状态串直接覆盖 dst 同名 key——与手写属性共存时，
// style 表视为「整块样式声明」，优先级高于之前的同名属性。
func ApplyStyle(dst Props, key string, v any) bool {
	if key != "style" {
		return false
	}
	var st Style
	switch x := v.(type) {
	case Style:
		st = x
	case map[string]any:
		st = Style(x)
	case string:
		parsed, err := ParseStyle(x)
		if err != nil {
			dst[key] = v // 解析失败保留原值，不静默吞掉
			return false
		}
		st = parsed
	default:
		return false
	}
	for k, pv := range st.Props() {
		dst[k] = pv
	}
	delete(dst, key) // style 本身不是后端属性
	return true
}

// PseudoStateFromProps 从后端回传的 props 里读取某个伪状态子表（解码 Props/encodeSubStyle
// 的编码），供 Skia headless 测试强制状态用。找不到返回 ok=false。
func PseudoStateFromProps(p map[string]any, state string) (Style, bool) {
	key := PseudoPrefix + strings.TrimPrefix(strings.TrimPrefix(state, PseudoPrefix), legacyPseudoPrefix)
	v, ok := p[key]
	if !ok {
		return nil, false
	}
	var obj map[string]any
	switch x := v.(type) {
	case string:
		if err := json.Unmarshal([]byte(x), &obj); err != nil {
			return nil, false
		}
	case map[string]any:
		obj = x
	default:
		return nil, false
	}
	return Style(obj), true
}
