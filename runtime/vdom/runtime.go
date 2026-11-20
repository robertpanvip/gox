// Package gxvdom 是 GX JSX 生成的 createElement 调用的运行时。
//
// 它负责「构建 + 渲染 + 差异计算」：CreateElement 返回一个轻量 VNode 树，
// Render 把它序列化为 HTML 字符串，Diff 计算两棵树之间的结构化补丁。
// 不含调度或状态管理——补丁如何应用由调用方决定。
package gxvdom

import (
	"fmt"
	"html"
	"reflect"
	"sort"
	"strings"
)

// VNode 是一个虚拟节点。Props 为 nil 表示无属性。
//
// 一个节点要么是「原生元素节点」（Tag 非空、Component 为 nil），
// 要么是「函数组件节点」（Component 为函数、Tag 为空）。两者由
// CreateElement 的第一个参数区分：string 视为标签，其余（函数）视为组件。
type VNode struct {
	Tag       string
	Component any // 函数组件；非 nil 时本节点在 Render/Diff 阶段展开
	Props     map[string]any
	Children  []any

	// 组件展开缓存：惰性执行，但一个组件节点只执行一次。
	// 避免 Diff 展开一次、Render 又展开一次导致有副作用/不纯组件行为不一致。
	resolved    any
	resolveDone bool
}

// Element 是 VNode 的兼容别名（早期版本使用 Element 这一名称）。
type Element = VNode

// CreateElement 由 JSX 转译结果调用。
//
// 第一个参数是 string 时创建原生元素节点；是函数时创建函数组件节点：
//
//	CreateElement("div", props, children...)   // 原生 <div>
//	CreateElement(Foo, props, children...)     // 函数组件 <Foo/>
//
// 函数组件不在创建时执行，而在 Render/Diff 阶段惰性展开：CreateElement
// 只产出「描述」，这与 React createElement 的语义一致，也让 Diff 能比较
// 展开后的具体节点。
func CreateElement(tagOrComponent any, props map[string]any, children ...any) *Element {
	n := &Element{Props: props, Children: children}
	if tag, ok := tagOrComponent.(string); ok {
		n.Tag = tag
	} else {
		n.Component = tagOrComponent
	}
	return n
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
		if n.Component != nil {
			// 函数组件惰性展开：调用组件函数得到节点树，再渲染其结果。
			renderTo(b, invokeComponent(n))
			return
		}
		b.WriteString("<")
		b.WriteString(n.Tag)
		for _, key := range sortedKeys(n.Props) {
			v := n.Props[key]
			if v == nil || v == false {
				continue
			}
			if key == "children" || key == "key" || (strings.HasPrefix(key, "on") && isFunc(v)) {
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

// invokeComponent 调用一个函数组件节点，返回它产出的节点树。
// 约定组件签名为 func(map[string]any) any（或返回 *Element 等具体类型）；
// props 原样传入，children 以 "children" 键附加。用反射以兼容不同返回类型。
// 若组件不是可调用对象或调用失败，返回 nil（渲染为空），不 panic。
func invokeComponent(n *Element) any {
	if n.resolveDone {
		return n.resolved
	}
	v := reflect.ValueOf(n.Component)
	if !v.IsValid() || v.Kind() != reflect.Func {
		return nil
	}
	t := v.Type()
	if t.NumIn() != 1 || t.In(0).Kind() != reflect.Map {
		return nil
	}
	props := n.Props
	if props == nil {
		props = map[string]any{}
	} else {
		// 复制一份，避免把 children 写回调用方的 props
		cp := make(map[string]any, len(props)+1)
		for k, val := range props {
			cp[k] = val
		}
		props = cp
	}
	if len(n.Children) > 0 {
		props["children"] = n.Children
	}
	// 用 map[string]any 形态构造参数；组件参数若为兼容的 map 类型可直接传入。
	arg := reflect.ValueOf(props)
	if !arg.Type().AssignableTo(t.In(0)) {
		if arg.Type().ConvertibleTo(t.In(0)) {
			arg = arg.Convert(t.In(0))
		} else {
			return nil
		}
	}
	res := v.Call([]reflect.Value{arg})
	if len(res) == 0 {
		return nil
	}
	out := res[0].Interface()
	n.resolved = out
	n.resolveDone = true
	return out
}

// ExpandComponent 显式展开一个组件节点，返回其产出的节点树；非组件节点返回 nil。
// 供调用方在需要时主动解析（结果会被缓存，与 Render/Diff 共享）。
func ExpandComponent(n *Element) any {
	if n == nil || n.Component == nil {
		return nil
	}
	return invokeComponent(n)
}

func isFunc(v any) bool {
	if v == nil {
		return false
	}
	return reflect.ValueOf(v).Kind() == reflect.Func
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
	OpSetText    PatchOp = "set-text"    // 文本节点内容变化
	OpReplace    PatchOp = "replace"     // 节点类型不同，整体替换
	OpSetProp    PatchOp = "set-prop"    // 属性新增/修改
	OpRemoveProp PatchOp = "remove-prop" // 属性删除
	OpAppend     PatchOp = "append"      // 新增子节点
	OpRemove     PatchOp = "remove"      // 删除多余子节点
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
	// 两侧都为空：无变化，不产生补丁（修复 Diff(nil,nil) 生成无意义 Replace）。
	if old == nil && new == nil {
		return
	}
	// 函数组件节点先惰性展开成具体节点树，再参与比较；
	// 这样 Diff 始终在「原生元素 + 文本」层面工作。
	if e, ok := old.(*VNode); ok && e != nil && e.Component != nil {
		old = invokeComponent(e)
	}
	if e, ok := new.(*VNode); ok && e != nil && e.Component != nil {
		new = invokeComponent(e)
	}

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

	// 子节点差异
	diffChildren(oe.Children, ne.Children, path, out)
}

// nodeKey 提取元素子节点的 key（Props["key"] 的字符串形式）。
// 非元素节点或没有 key 时返回 false。
func nodeKey(v any) (string, bool) {
	e, ok := v.(*VNode)
	if !ok || e == nil || e.Props == nil {
		return "", false
	}
	k, ok := e.Props["key"]
	if !ok {
		return "", false
	}
	s, ok := k.(string)
	return s, ok && s != ""
}

// diffChildren 比较子节点列表。若两侧所有元素子节点都带 key，则按 key 对齐
// （支持插入/删除/重排，避免位置错位产生大量错误补丁）；否则按位置线性比较。
func diffChildren(oldKids, newKids []any, path []int, out *[]Patch) {
	// 判断是否可启用 key 对齐：两侧非空、且每个元素子节点都有 key。
	allKeyed := len(oldKids) > 0 && len(newKids) > 0
	if allKeyed {
		for _, c := range oldKids {
			if _, ok := nodeKey(c); !ok {
				allKeyed = false
				break
			}
		}
	}
	if allKeyed {
		for _, c := range newKids {
			if _, ok := nodeKey(c); !ok {
				allKeyed = false
				break
			}
		}
	}

	if allKeyed {
		// 建 old 的 key → (节点, 原位置) 索引
		oldByKey := map[string]any{}
		for _, c := range oldKids {
			k, _ := nodeKey(c)
			oldByKey[k] = c
		}
		used := map[string]bool{}
		// 以 new 顺序为准：命中的对位 diff（路径用 new 索引），未命中的 append
		for i, nc := range newKids {
			k, _ := nodeKey(nc)
			if oc, ok := oldByKey[k]; ok {
				used[k] = true
				diffNode(oc, nc, append(clonePath(path), i), out)
			} else {
				*out = append(*out, Patch{Op: OpAppend, Path: clonePath(path), Value: nc})
			}
		}
		// old 中未被使用的 key → remove（按原位置倒序，路径稳定）
		for i := len(oldKids) - 1; i >= 0; i-- {
			k, _ := nodeKey(oldKids[i])
			if !used[k] {
				*out = append(*out, Patch{Op: OpRemove, Path: append(clonePath(path), i)})
			}
		}
		return
	}

	// 无 key：按位置线性比较（保持向后兼容）
	n := len(oldKids)
	if len(newKids) < n {
		n = len(newKids)
	}
	for i := 0; i < n; i++ {
		diffNode(oldKids[i], newKids[i], append(clonePath(path), i), out)
	}
	for i := n; i < len(newKids); i++ {
		*out = append(*out, Patch{Op: OpAppend, Path: clonePath(path), Value: newKids[i]})
	}
	for i := n; i < len(oldKids); i++ {
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
	// 严格按动态类型比较：true(bool) 与 "true"(string) 渲染结果不同
	// （<button disabled> vs <button disabled="true">），不能只比字符串。
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ta, tb := reflect.TypeOf(a), reflect.TypeOf(b)
	if ta != tb {
		return false
	}
	return reflect.DeepEqual(a, b)
}

func clonePath(p []int) []int {
	if p == nil {
		return nil
	}
	out := make([]int, len(p))
	copy(out, p)
	return out
}
