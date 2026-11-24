package gxscene

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// Style 是「强类型受限 CSS sheet」：一个字段固定的结构体，而不是任意 key 的 map。
//
// 设计要点：
//   - 字段集合固定（见下方 struct）；字段名拼错在 Go 编译期就会报错，不再靠运行时
//     Validate 兜底。GX 的 style={{ fontSize: 12 }} 由编译器生成命名字段字面量。
//   - 标量类型统一为 StyleValue（string 别名）。CSS 值本质就是字符串：这样
//     opacity:0 不会被当成「未设置」（float64 零值会），width:"100%"/"auto" 也能
//     无损表达，后端 cssValue 本来就只接受字符串，零改动。
//   - 伪状态是命名字段 Hover/Focus/Active/Disabled *Style，编译期即可校验状态名；
//     子表经 Props 编码成 "&:hover" -> JSON 串，保持后端协议不变。
//
// Go 写法：
//
//	gxscene.Style{
//	    Bg: "#3b82f6", Color: "#fff", Padding: "11",
//	    Hover:  &gxscene.Style{Bg: "#2563eb"},
//	    Active: &gxscene.Style{Bg: "#1d4ed8", Transform: "scale(.98)"},
//	}
//
// GX 写法（驼峰 key，编译器映射到字段；数字字面量归一化成字符串）：
//
//	<div style={{ backgroundColor: lineGutterColor(idx), fontSize: 12,
//	              "&:hover": { bg: "#eee" } }}>
type StyleValue = string

// Style 是字段固定的强类型样式结构体。每个 tag 是后端约定的 kebab 属性名；
// 编译器按 kebab→PascalCase 生成字段名，二者由 TestStyleFieldsMatchNames 守护一致。
type Style struct {
	Color       StyleValue `gx:"color"`
	Bg          StyleValue `gx:"bg"`
	Background  StyleValue `gx:"background"`
	Width       StyleValue `gx:"width"`
	Height      StyleValue `gx:"height"`
	FontSize    StyleValue `gx:"font-size"`
	Weight      StyleValue `gx:"weight"`
	Padding     StyleValue `gx:"padding"`
	BorderRadius StyleValue `gx:"border-radius"`
	Border      StyleValue `gx:"border"`
	BorderWidth StyleValue `gx:"border-width"`
	Opacity     StyleValue `gx:"opacity"`
	FlexDirection  StyleValue `gx:"flex-direction"`
	FlexWrap       StyleValue `gx:"flex-wrap"`
	JustifyContent StyleValue `gx:"justify-content"`
	AlignItems     StyleValue `gx:"align-items"`
	AlignContent   StyleValue `gx:"align-content"`
	Gap            StyleValue `gx:"gap"`
	RowGap         StyleValue `gx:"row-gap"`
	ColumnGap      StyleValue `gx:"column-gap"`
	Position       StyleValue `gx:"position"`
	Top            StyleValue `gx:"top"`
	Right          StyleValue `gx:"right"`
	Bottom         StyleValue `gx:"bottom"`
	Left           StyleValue `gx:"left"`
	Overflow       StyleValue `gx:"overflow"`
	ScrollTop      StyleValue `gx:"scroll-top"`
	ScrollLeft     StyleValue `gx:"scroll-left"`
	Transform      StyleValue `gx:"transform"`
	Transition     StyleValue `gx:"transition"`
	BoxShadow      StyleValue `gx:"box-shadow"`
	WhiteSpace     StyleValue `gx:"white-space"`
	FlexGrow       StyleValue `gx:"flex-grow"`
	FlexShrink     StyleValue `gx:"flex-shrink"`
	MinWidth       StyleValue `gx:"min-width"`
	MaxWidth       StyleValue `gx:"max-width"`
	MinHeight      StyleValue `gx:"min-height"`
	MaxHeight      StyleValue `gx:"max-height"`
	Margin         StyleValue `gx:"margin"`
	MarginTop      StyleValue `gx:"margin-top"`
	MarginBottom   StyleValue `gx:"margin-bottom"`
	MarginLeft     StyleValue `gx:"margin-left"`
	MarginRight    StyleValue `gx:"margin-right"`
	TextAlign      StyleValue `gx:"text-align"`
	LineHeight     StyleValue `gx:"line-height"`
	LetterSpacing  StyleValue `gx:"letter-spacing"`
	ZIndex         StyleValue `gx:"z-index"`
	Cursor         StyleValue `gx:"cursor"`

	// 伪状态字段。nil 表示未设置；子表里的嵌套伪状态在展平时忽略。
	Hover    *Style `gx:"&:hover"`
	Focus    *Style `gx:"&:focus"`
	Active   *Style `gx:"&:active"`
	Disabled *Style `gx:"&:disabled"`
}

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

// StylePropNames 是 Style 允许的普通属性白名单（后端两解析器已支持的全部属性）。
// 由 styleFields 元信息生成，避免与 struct 字段不同步。
var StylePropNames = func() []string {
	fields, _ := styleMeta()
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f.pseudo == "" {
			out = append(out, f.key)
		}
	}
	return out
}()

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

func isStyleScalar(v any) bool {
	switch v.(type) {
	case nil, string, bool, int, int64, int32, float64, float32:
		return true
	default:
		return false
	}
}

// ---------- 字段元信息（反射一次，缓存复用；热路径只读缓存） ----------

type styleField struct {
	key    string // 后端 kebab key 或 "&:hover"
	name   string // Go 字段名
	index  int    // struct 字段索引
	pseudo string // 伪状态名（hover/...），空表示普通属性
}

var (
	styleMetaOnce sync.Once
	styleMetaList []styleField
	styleMetaByKey map[string]int
)

func styleMeta() ([]styleField, map[string]int) {
	styleMetaOnce.Do(func() {
		t := reflect.TypeOf(Style{})
		styleMetaList = make([]styleField, 0, t.NumField())
		styleMetaByKey = make(map[string]int, t.NumField())
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			key := f.Tag.Get("gx")
			if key == "" {
				continue
			}
			m := styleField{key: key, name: f.Name, index: i}
			if strings.HasPrefix(key, PseudoPrefix) {
				m.pseudo = strings.TrimPrefix(key, PseudoPrefix)
			}
			styleMetaByKey[key] = len(styleMetaList)
			styleMetaList = append(styleMetaList, m)
		}
	})
	return styleMetaList, styleMetaByKey
}

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
	return pseudoStates[strings.TrimPrefix(c, PseudoPrefix)]
}

func pseudoStateList() []string {
	out := make([]string, 0, len(pseudoStates))
	for s := range pseudoStates {
		out = append(out, PseudoPrefix+s)
	}
	sort.Strings(out)
	return out
}

// normalizeStylePseudo 把 "hover" / ":hover" / "&:hover" / "$:hover" 规整成 "hover"。
func normalizeStylePseudo(p string) string {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, PseudoPrefix) || strings.HasPrefix(p, legacyPseudoPrefix) {
		p = p[2:]
	}
	return strings.ToLower(strings.TrimPrefix(p, ":"))
}

// camelToKebab 把驼峰属性名转成后端 kebab 名（与编译器 styleKeyCamelToKebab 同规则）。
func camelToKebab(s string) string {
	if s == "" || strings.ContainsAny(s, "-:") {
		return s
	}
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('-')
			}
			b.WriteByte(byte(r - 'A' + 'a'))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ---------- 校验 ----------

// Validate 递归校验伪状态子表。普通属性因字段固定，编译期已保证合法；
// 从 JSON / map 动态构造时由 setStyleValue 拦截未知 key。
func (s Style) Validate() error {
	fields, _ := styleMeta()
	v := reflect.ValueOf(s)
	for _, f := range fields {
		if f.pseudo == "" {
			continue
		}
		sub, _ := v.Field(f.index).Interface().(*Style)
		if sub == nil {
			continue
		}
		if err := sub.Validate(); err != nil {
			return fmt.Errorf("style: %s: %w", f.key, err)
		}
	}
	return nil
}

// ---------- 展平 ----------

// forEachScalar 遍历所有非空的普通属性字段，供 Props / Flatten / encodeSubStyle 复用。
func (s Style) forEachScalar(fn func(key, val string)) {
	fields, _ := styleMeta()
	v := reflect.ValueOf(s)
	for _, f := range fields {
		if f.pseudo != "" {
			continue
		}
		sv, _ := v.Field(f.index).Interface().(StyleValue)
		if sv != "" {
			fn(f.key, sv)
		}
	}
}

func (s Style) pseudoPtr(name string) *Style {
	fields, byKey := styleMeta()
	i, ok := byKey[PseudoPrefix+name]
	if !ok {
		return nil
	}
	sub, _ := reflect.ValueOf(s).Field(fields[i].index).Interface().(*Style)
	return sub
}

// encodeSubStyle 把子样式表编码成后端可解析的紧凑 JSON 串（只保留普通属性，
// 忽略子表里的嵌套伪状态）。
func encodeSubStyle(sub Style) string {
	var parts []string
	sub.forEachScalar(func(k, v string) {
		b, _ := json.Marshal(v)
		kb, _ := json.Marshal(k)
		parts = append(parts, string(kb)+":"+string(b))
	})
	if len(parts) == 0 {
		return ""
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// Props 把样式表展平成后端 props：普通属性直出；伪状态子表编码成
// props["&:hover"] = `{"bg":"#eee"}` 这样的紧凑串。
func (s Style) Props() Props {
	fields, _ := styleMeta()
	v := reflect.ValueOf(s)
	p := make(Props, len(fields))
	for _, f := range fields {
		if f.pseudo != "" {
			sub, _ := v.Field(f.index).Interface().(*Style)
			if sub == nil {
				continue
			}
			if enc := encodeSubStyle(*sub); enc != "" {
				p[PseudoPrefix+f.pseudo] = enc
			}
			continue
		}
		sv, _ := v.Field(f.index).Interface().(StyleValue)
		if sv != "" {
			p[f.key] = sv
		}
	}
	return p
}

// Flatten 按当前激活的伪状态把样式表合并成一份扁平 props（host/Skia headless
// 用，模拟后端覆盖行为）。顺序：先普通属性，再按 states 依次覆盖。
func (s Style) Flatten(states ...string) Props {
	out := Props{}
	s.forEachScalar(func(k, v string) { out[k] = v })
	for _, st := range states {
		sub := s.pseudoPtr(normalizeStylePseudo(st))
		if sub == nil {
			continue
		}
		sub.forEachScalar(func(k, v string) { out[k] = v })
	}
	return out
}

// ---------- 动态构造（JSON / map） ----------

func scalarToStyleValue(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "", true
	case string:
		return x, true
	case bool:
		if x {
			return "true", true
		}
		return "false", true
	case float64:
		return fmt.Sprint(x), true
	case float32:
		return fmt.Sprint(x), true
	case int:
		return fmt.Sprint(x), true
	case int32:
		return fmt.Sprint(x), true
	case int64:
		return fmt.Sprint(x), true
	default:
		return "", false
	}
}

func styleFromAny(v any) (Style, error) {
	switch x := v.(type) {
	case Style:
		return x, nil
	case *Style:
		if x != nil {
			return *x, nil
		}
		return Style{}, nil
	case map[string]any:
		var s Style
		for k, val := range x {
			if err := setStyleValue(&s, k, val); err != nil {
				return Style{}, err
			}
		}
		return s, nil
	case string:
		return ParseStyle(x)
	default:
		return Style{}, fmt.Errorf("style: 值类型 %T 不是样式表", v)
	}
}

// setStyleValue 把动态构造的 key/value 写入 struct 字段；未知 key 报错。
func setStyleValue(s *Style, key string, val any) error {
	key = canonicalPseudo(key)
	fields, byKey := styleMeta()
	// 伪状态单词 key（hover/active/focus/disabled）映射到 "&:x" 字段。
	if _, isP := pseudoStates[strings.ToLower(key)]; isP && !isPseudoKey(key) {
		key = PseudoPrefix + strings.ToLower(key)
	}
	i, ok := byKey[key]
	if !ok {
		// 兼容驼峰 key（如 fontSize）。
		if kebab := camelToKebab(key); kebab != key {
			i, ok = byKey[kebab]
			key = kebab
		}
	}
	if !ok {
		return fmt.Errorf("style: 未知属性 %q（不在受限 CSS sheet）", key)
	}
	f := fields[i]
	dst := reflect.ValueOf(s).Elem().Field(f.index)
	if f.pseudo != "" {
		sub, err := styleFromAny(val)
		if err != nil {
			return fmt.Errorf("style: 伪状态 %q 的值必须是样式表: %w", key, err)
		}
		dst.Set(reflect.ValueOf(&sub))
		return nil
	}
	sv, ok := scalarToStyleValue(val)
	if !ok {
		return fmt.Errorf("style: 属性 %q 的值不是合法标量", key)
	}
	dst.SetString(sv)
	return nil
}

// StyleFromMap 把 map[string]any 严格转换为 Style（兼容既有的动态构造路径）。
func StyleFromMap(m map[string]any) (Style, error) {
	return styleFromAny(m)
}

// asStyle 把任意候选值解释为 Style，用于内部兼容。
func asStyle(v any) (Style, bool) {
	s, err := styleFromAny(v)
	return s, err == nil
}

// parseStyle 供编译器生成的 getter 使用（单返回值）。编译器已校验 JSON 合法，
// 这里解析失败时返回空 Style，不向生成代码暴露 error。
func parseStyle(src string) Style {
	s, _ := ParseStyle(src)
	return s
}

// ParseStyle 从 JSON 对象字符串解析样式表。"&:hover"/"$:hover" 递归解析为子表。
func ParseStyle(src string) (Style, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(src), &raw); err != nil {
		return Style{}, fmt.Errorf("style: 非法 JSON: %w", err)
	}
	var s Style
	for k, r := range raw {
		var v any
		if err := json.Unmarshal(r, &v); err != nil {
			return Style{}, fmt.Errorf("style: 属性 %q 解析失败: %w", k, err)
		}
		if err := setStyleValue(&s, k, v); err != nil {
			return Style{}, err
		}
	}
	return s, nil
}

// ApplyStyle 把 Style 展平后写入目标 Props（host 的 slotAttr 写入路径调用）。
func ApplyStyle(dst Props, key string, v any) bool {
	if key != "style" {
		return false
	}
	st, err := styleFromAny(v)
	if err != nil {
		dst[key] = v // 解析失败保留原值，不静默吞掉
		return false
	}
	for k, pv := range st.Props() {
		dst[k] = pv
	}
	delete(dst, key)
	return true
}

// PseudoStateFromProps 从后端回传的 props 里读取某个伪状态子表，供 Skia headless
// 测试强制状态用。找不到返回 ok=false。
func PseudoStateFromProps(p map[string]any, state string) (Style, bool) {
	key := PseudoPrefix + normalizeStylePseudo(state)
	v, ok := p[key]
	if !ok {
		return Style{}, false
	}
	st, err := styleFromAny(v)
	if err != nil {
		return Style{}, false
	}
	return st, true
}
