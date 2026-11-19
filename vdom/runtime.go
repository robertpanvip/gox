// Package gxvdom 是 GX JSX 生成的 createElement 调用的运行时。
//
// 它只负责「构建 + 渲染」：CreateElement 返回一个轻量 Element 树，
// Render 把它序列化为 HTML 字符串。不包含 diff、调度或状态管理。
package gxvdom

import (
	"fmt"
	"html"
	"sort"
	"strings"
)

// Element 是一个虚拟节点。Props 为 nil 表示无属性。
type Element struct {
	Tag      string
	Props    map[string]any
	Children []any
}

// CreateElement 由 JSX 转译结果调用。签名刻意保持简单：
//
//	CreateElement(tag string, props map[string]any, children ...any) *Element
func CreateElement(tag string, props map[string]any, children ...any) *Element {
	return &Element{Tag: tag, Props: props, Children: children}
}

var voidTags = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true,
	"img": true, "input": true, "link": true, "meta": true, "param": true,
	"source": true, "track": true, "wbr": true,
}

// Render 把 Element 树渲染为 HTML 字符串。
func Render(node any) string {
	var b strings.Builder
	renderTo(&b, node)
	return b.String()
}

func renderTo(b *strings.Builder, node any) {
	switch n := node.(type) {
	case nil:
		return
	case *Element:
		if n == nil {
			return
		}
		b.WriteString("<")
		b.WriteString(n.Tag)
		for _, key := range sortedKeys(n.Props) {
			v := n.Props[key]
			if v == nil || v == false {
				continue
			}
			if key == "children" || (strings.HasPrefix(key, "on") && isFunc(v)) {
				continue
			}
			b.WriteString(" ")
			b.WriteString(key)
			if v == true {
				continue
			}
			b.WriteString(`="`)
			b.WriteString(html.EscapeString(scalarString(v)))
			b.WriteString(`"`)
		}
		b.WriteString(">")
		if voidTags[n.Tag] {
			return
		}
		for _, c := range n.Children {
			renderTo(b, c)
		}
		b.WriteString("</")
		b.WriteString(n.Tag)
		b.WriteString(">")
	case string:
		b.WriteString(html.EscapeString(n))
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64, bool:
		b.WriteString(html.EscapeString(scalarString(n)))
	case []any:
		for _, c := range n {
			renderTo(b, c)
		}
	default:
		b.WriteString(html.EscapeString(fmt.Sprint(n)))
	}
}

func isFunc(v any) bool {
	if v == nil {
		return false
	}
	switch v.(type) {
	case func(), func(any), func(...any):
		return true
	}
	return false
}

func scalarString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(x)
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
