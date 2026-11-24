package gxscene

import (
	"html"
	"strings"
)

// RichSpan 是富文本的一个着色分段。Text 是该段的原文，Color 是 CSS 颜色（如 #c586c0）。
// Color 为空表示沿用父节点继承色。
type RichSpan struct {
	Text  string
	Color string
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
// 文本经过 HTML 转义，避免源码里的 < > & 破坏结构或注入。
// Color 为空的分段不写 style，沿用父元素颜色。
func (r RichText) HTML() string {
	var b strings.Builder
	for _, s := range r {
		if s.Text == "" {
			continue
		}
		esc := html.EscapeString(s.Text)
		if s.Color == "" {
			b.WriteString(esc)
			continue
		}
		b.WriteString(`<span style="color:`)
		b.WriteString(html.EscapeString(s.Color))
		b.WriteString(`">`)
		b.WriteString(esc)
		b.WriteString(`</span>`)
	}
	return b.String()
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
