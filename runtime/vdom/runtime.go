// Package gxvdom 是 GX JSX 生成的 createElement 调用的运行时。
//
// 它负责「构建 + 渲染 + 差异计算」：CreateElement 返回一个轻量 VNode 树，
// Render 把它序列化为 HTML 字符串，Diff 计算两棵树之间的结构化补丁。
// 不含调度或状态管理——补丁如何应用由调用方决定。
package gxvdom

import (
	"fmt"
	"html"
	"sort"
	"strings"
)

// VNode 是一个虚拟节点。Props 为 nil 表示无属性。
type VNode struct {
	Tag      string
	Props    map[string]any
	Children []any
}

// Element 是 VNode 的兼容别名（早期版本使用 Element 这一名称）。
type Element = VNode

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

// ---------- Diff：计算两棵 VNode 树之间的结构化补丁 ----------
//
// Diff 只产出「做什么」的描述，不接触真实 DOM，也不做调度。调用方（例如
// 浏览器绑定或服务端补丁应用器）按 Patch 序列自行应用。对比策略是按位置
// 的线性比较：结构简单、可预测，足够覆盖 JSX 产出的静态树场景。

// PatchOp 是补丁类型。
type PatchOp string

const (
	OpSetText    PatchOp = "set-text"     // 文本节点内容变化
	OpReplace    PatchOp = "replace"      // 节点类型不同，整体替换
	OpSetProp    PatchOp = "set-prop"     // 属性新增/修改
	OpRemoveProp PatchOp = "remove-prop"  // 属性删除
	OpAppend     PatchOp = "append"       // 新增子节点
	OpRemove     PatchOp = "remove"       // 删除多余子节点
)

// Patch 是一条补丁。Path 是自根节点的子节点索引路径，如 []int{0, 1}。
type Patch struct {
	Op    PatchOp
	Path  []int
	Key   string // SetProp/RemoveProp 的属性名
	Value any    // SetText/SetProp/Append/Replace 的新值
}

// Diff 计算 old 树到 new 树所需的最小补丁序列（按路径顺序）。
func Diff(old, new any) []Patch {
	var patches []Patch
	diffNode(old, new, nil, &patches)
	return patches
}

func diffNode(old, new any, path []int, out *[]Patch) {
	// 文本 / 标量节点
	os, oText := scalar(old)
	ns, nText := scalar(new)
	if oText && nText {
		// 两侧都是文本：内容不同才发补丁
		if os != ns {
			*out = append(*out, Patch{Op: OpSetText, Path: clonePath(path), Value: ns})
		}
		return
	}
	// 只有一侧是文本：类型变化，整体替换
	if oText || nText {
		*out = append(*out, Patch{Op: OpReplace, Path: clonePath(path), Value: new})
		return
	}

	oe, oElem := old.(*VNode)
	ne, nElem := new.(*VNode)
	if !oElem || !nElem {
		// 类型不一致：整体替换
		*out = append(*out, Patch{Op: OpReplace, Path: clonePath(path), Value: new})
		return
	}
	if oe.Tag != ne.Tag {
		*out = append(*out, Patch{Op: OpReplace, Path: clonePath(path), Value: new})
		return
	}

	// 属性差异
	for _, k := range sortedKeys(ne.Props) {
		if ov, ok := oe.Props[k]; !ok || !equalValue(ov, ne.Props[k]) {
			*out = append(*out, Patch{Op: OpSetProp, Path: clonePath(path), Key: k, Value: ne.Props[k]})
		}
	}
	for _, k := range sortedKeys(oe.Props) {
		if _, ok := ne.Props[k]; !ok {
			*out = append(*out, Patch{Op: OpRemoveProp, Path: clonePath(path), Key: k})
		}
	}

	// 子节点差异：按位置线性比较
	n := len(oe.Children)
	if len(ne.Children) < n {
		n = len(ne.Children)
	}
	for i := 0; i < n; i++ {
		diffNode(oe.Children[i], ne.Children[i], append(clonePath(path), i), out)
	}
	for i := n; i < len(ne.Children); i++ {
		*out = append(*out, Patch{Op: OpAppend, Path: clonePath(path), Value: ne.Children[i]})
	}
	for i := n; i < len(oe.Children); i++ {
		*out = append(*out, Patch{Op: OpRemove, Path: append(clonePath(path), i)})
	}
}

// scalar 判断节点是否为文本/标量，并返回其字符串表示。
func scalar(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "", false
	case string:
		return x, true
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, float32, float64, bool:
		return scalarString(x), true
	default:
		return "", false
	}
}

func equalValue(a, b any) bool {
	as, aOK := scalar(a)
	bs, bOK := scalar(b)
	if aOK && bOK {
		return as == bs
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func clonePath(p []int) []int {
	if p == nil {
		return nil
	}
	out := make([]int, len(p))
	copy(out, p)
	return out
}
