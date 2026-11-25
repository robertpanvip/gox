package gxscene

import (
	"encoding/json"
	"html"
	"strconv"
	"strings"
)

// RichSpan 是富文本的一个着色分段。Text 是该段的原文，Color 是 CSS 颜色（如 #c586c0）。
// Color 为空表示沿用父节点继承色。
//
// Selected 标记该段是否处于文本选区中（三端统一的 selection 协议）：
//   - 为 true 时，后端绘制该段时用节点的 &::selection 子表覆盖背景与前景色；
//   - 为 false 时按普通语法高亮色绘制。
// 选区与语法高亮正交：一段文字既有语法色，也可被选中；选中态只改配色不改分词。
type RichSpan struct {
	Text     string
	Color    string
	Selected bool
}

// PropSelection 是选区子表在节点 Props 里的协议键（双冒号 = 伪元素）。
// 值为紧凑 JSON，如 `{"bg":"#b4d5fe","color":"#fff"}`。
const PropSelection = "&::selection"

// PropSelectionRange 是选区字符区间的标量协议键，值为 "start:end"（UTF-8 rune
// 索引，半开区间 [start,end)），挂在文本节点上。它让不接收结构化富文本的
// 后端（Rust/scene 标量协议）也能实现字符级选区着色，是三端统一的标量通道。
// 缺省 / 非法 / start>=end 表示无选区。
const PropSelectionRange = "selection-range"

// SelectionStyle 是选区配色的解码结果；空串字段表示未指定，沿用默认。
type SelectionStyle struct {
	Bg    string
	Color string
}

// SelectionRange 是选区字符区间 [Start,End)，单位 rune。空选区用 Start>=End 表示。
type SelectionRange struct {
	Start int
	End   int
}

// Valid 报告区间是否非空。
func (r SelectionRange) Valid() bool { return r.End > r.Start }

// SelectionRangeFromProps 从文本节点 Props 读取 selection-range，解析 rune 区间。
// 接受 "start:end" / "start,end" 两种分隔；非法值返回空选区。
func SelectionRangeFromProps(props map[string]any) SelectionRange {
	if props == nil {
		return SelectionRange{}
	}
	raw, _ := props[PropSelectionRange].(string)
	if raw == "" {
		return SelectionRange{}
	}
	raw = strings.TrimSpace(raw)
	sep := strings.IndexAny(raw, ":,")
	if sep < 0 {
		return SelectionRange{}
	}
	start, err1 := strconv.Atoi(strings.TrimSpace(raw[:sep]))
	end, err2 := strconv.Atoi(strings.TrimSpace(raw[sep+1:]))
	if err1 != nil || err2 != nil || start < 0 || end <= start {
		return SelectionRange{}
	}
	return SelectionRange{Start: start, End: end}
}

// RichText 是一行/一段富文本的有序分段序列，拼起来等于该行完整文本。
// 它作为动态属性值挂在文本节点上，key 固定为 PropRichText。
//
// 设计动机：语法高亮若把每个 token 渲染成一个独立的 span 节点，一行代码就要
// 24 个 DOM 节点 × 2 个 getter（text/color），整篇替换时数百个 effect 全量重跑。
// 富文本把一行的所有分段打包成"一个值"——文本节点自身 Text 存纯文本供布局测量，
// rich-text 属性携带分段着色数据。这样一行的着色更新 = 1 个属性插槽 + 1 条 Change，
// effect 数与节点数都从 O(行×token) 降到 O(行)。
type RichText []RichSpan

// PropRichText 是富文本数据在节点 Props / Change.Key 中的固定属性名。
// 后端识别该 key 走富文本绘制通道（web: innerHTML；skia: 分段着色）。
const PropRichText = "rich-text"

// Plain 返回富文本拼接后的纯文本（供布局测量 / 无障碍 / 回退绘制）。
func (r RichText) Plain() string {
	var b strings.Builder
	for _, s := range r {
		b.WriteString(s.Text)
	}
	return b.String()
}

// HTML 把富文本序列化为带内联 color 样式的 HTML 片段，供 web 后端一次 innerHTML 写入。
// 等价于 HTMLWithSelection(SelectionStyle{})——无选区配色时不产出选区背景。
func (r RichText) HTML() string {
	return r.HTMLWithSelection(SelectionStyle{})
}

// HTMLWithSelection 在 HTML 基础上叠加选区着色：Selected 段用 sel.Bg 作背景、
// sel.Color 作前景（缺省时沿用段自身语法色）。三端统一由 &::selection 子表提供配色，
// Web 端借此把程序化选区（键盘 Shift 选择）也画成与 Skia/Rust 一致的色块。
//
// 合并策略：相邻「同 color + 同 selected」的段共用一个 span，避免细粒度分词让
// innerHTML 膨胀。文本一律 HTML 转义，防止源码里的 < > & 破坏结构或注入。
func (r RichText) HTMLWithSelection(sel SelectionStyle) string {
	var b strings.Builder
	for i := 0; i < len(r); {
		s := r[i]
		if s.Text == "" {
			i++
			continue
		}
		color := s.Color
		if s.Selected && sel.Color != "" {
			color = sel.Color
		}
		bg := ""
		if s.Selected {
			bg = sel.Bg
		}
		if color == "" && bg == "" {
			b.WriteString(html.EscapeString(s.Text))
			i++
			continue
		}
		b.WriteString(`<span style="`)
		if bg != "" {
			b.WriteString(`background-color:`)
			b.WriteString(html.EscapeString(bg))
			b.WriteString(`;`)
		}
		if color != "" {
			b.WriteString(`color:`)
			b.WriteString(html.EscapeString(color))
		}
		b.WriteString(`">`)
		// 连续同 (color, selected) 分段共用一个 span。
		for i < len(r) && r[i].Color == s.Color && r[i].Selected == s.Selected {
			if r[i].Text != "" {
				b.WriteString(html.EscapeString(r[i].Text))
			}
			i++
		}
		b.WriteString(`</span>`)
	}
	return b.String()
}

// SelectionFromProps 从节点 Props 读取 &::selection 子表，解码成 SelectionStyle。
// 子表是紧凑 JSON 串（由 Style.Props 编码）；非法或缺失返回空配色。
func SelectionFromProps(props map[string]any) SelectionStyle {
	if props == nil {
		return SelectionStyle{}
	}
	raw, ok := props[PropSelection].(string)
	if !ok || raw == "" {
		return SelectionStyle{}
	}
	var m map[string]string
	if json.Unmarshal([]byte(raw), &m) != nil {
		return SelectionStyle{}
	}
	return SelectionStyle{Bg: m["bg"], Color: m["color"]}
}

// RichTextFromProps 从节点属性里取出富文本（若存在且类型正确）。
func RichTextFromProps(props map[string]any) (RichText, bool) {
	if props == nil {
		return nil, false
	}
	v, ok := props[PropRichText]
	if !ok {
		return nil, false
	}
	rt, ok := v.(RichText)
	return rt, ok
}
