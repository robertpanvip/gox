// Package gxscene 是 GX JSX「路线三」的运行时：编译期 opcode + 常驻场景树。
//
// 与 gxvdom（createElement：每次重建整棵树 + Diff）不同，这里把 JSX 拆成两部分：
//
//   - 静态结构：编译期展开成一串 opcode（Op 序列），只解析一次并缓存成骨架；
//   - 动态插槽：每个 {expr} / attr={expr} 是一个 slot，绑定一个 func() any getter。
//
// Render 时 clone 缓存的静态骨架（廉价、无解析），把 getter 绑到 slot 上得到一棵
// 常驻场景树（Instance）。之后 signal / 外部状态变化只需调 Instance.Update()：
// 它重算每个 slot，只把「值真的变了」的写回对应节点，零 Diff、零整树重建。
//
// 这正是 lit-html「静态模板只解析一次 + 动态 part 原地更新」的思想，但终点不是
// DOM，而是一棵可移植的场景树；绘制由 Backend 抽象，Skia / wgpu 后端可后续接入。
// 依赖追踪也刻意留白：slot getter 是普通闭包，任何响应式系统（如 signal）都能
// 用「读取时订阅、变化时调 Update」的方式驱动它——渲染与响应式核心解耦。
package gxscene

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// OpCode 是编译期生成的 opcode 类型。
type OpCode uint8

const (
	OpElem      OpCode = iota // 打开元素：Arg=标签名；压栈
	OpClose                   // 关闭栈顶元素
	OpAttr                    // 静态属性：Key=属性名, Arg=属性值
	OpChildText               // 静态文本子节点：Arg=文本
	OpDynChild                // 动态子节点插槽：Slot=索引
	OpDynAttr                 // 动态属性插槽：Key=属性名, Slot=索引
	OpComp                    // 复合组件插槽：Arg=组件名, Slot=索引；getter 返回一个 *Instance
)

// Op 是一条 opcode 指令。Arg/Key/Slot 的含义由 Code 决定。
type Op struct {
	Code OpCode
	Arg  string
	Key  string
	Slot int
}

// Template 是编译期产出的模板：opcode 序列 + 动态插槽数量。
// 它只描述「结构」，不含任何运行期值，因此可被多个 Instance 共享。
type Template struct {
	Ops []Op
	N   int // 动态插槽总数
}

// Node 是常驻场景树的节点。Kind 为标签名（元素）或 "#text"（文本）。
type Node struct {
	Kind     string
	Props    map[string]any
	Children []*Node
	Text     string // 仅 #text 节点使用
	parent   *Node
	dirty    bool
}

// slotKind 区分动态子节点与动态属性。
type slotKind uint8

const (
	slotChild slotKind = iota
	slotAttr
	slotComp // 复合组件：getter 返回一个 *Instance，挂载为子树，Update 递归驱动
)

// slot 描述一个动态插槽在场景树中的位置与绑定。
type slot struct {
	kind  slotKind
	path  []int  // 从根到目标节点的子节点索引路径
	attr  string // slotAttr 时的属性名
	get   func() any
	last  any
	node  *Node     // clone 后回填的实际节点
	child *Instance // slotComp 时挂载的子组件实例
}

// compiledTemplate 是解析 opcode 一次得到的静态骨架（只解析一次，全局缓存）。
type compiledTemplate struct {
	root  *Node   // 静态骨架；动态位置留空
	slots []*slot // 每个插槽的目标路径（get/node 尚未绑定）
}

var (
	cacheMu sync.RWMutex
	cache   = map[string]*compiledTemplate{}
)

// templateKey 把 opcode 结构规范化成缓存键：只含结构与静态文本，不含 slot 的运行值。
// 等价于 lit 用 TemplateStringsArray 作 WeakMap key——同一份源码模板命中同一骨架。
func templateKey(t Template) string {
	var b strings.Builder
	for _, op := range t.Ops {
		fmt.Fprintf(&b, "%d\x00%s\x00%s\x00%d\x1f", op.Code, op.Arg, op.Key, op.Slot)
	}
	return b.String()
}

// compile 把 opcode 解释成一棵静态骨架树，并记录每个 slot 的目标路径。
func compile(t Template) *compiledTemplate {
	root := &Node{Kind: "#root"}
	stack := []*Node{root}
	slots := make([]*slot, t.N)
	childOfStack := func(parent *Node) int {
		parent.Children = append(parent.Children, &Node{})
		return len(parent.Children) - 1
	}
	for _, op := range t.Ops {
		cur := stack[len(stack)-1]
		switch op.Code {
		case OpElem:
			idx := childOfStack(cur)
			n := cur.Children[idx]
			n.Kind = op.Arg
			n.Props = map[string]any{}
			n.parent = cur
			stack = append(stack, n)
		case OpClose:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case OpAttr:
			if cur.Props == nil {
				cur.Props = map[string]any{}
			}
			cur.Props[op.Key] = op.Arg
		case OpChildText:
			idx := childOfStack(cur)
			n := cur.Children[idx]
			n.Kind = "#text"
			n.Text = op.Arg
			n.parent = cur
		case OpDynChild:
			idx := childOfStack(cur)
			n := cur.Children[idx]
			n.Kind = "#text"
			n.parent = cur
			if op.Slot >= 0 && op.Slot < t.N {
				slots[op.Slot] = &slot{kind: slotChild, path: append(childPath(cur), idx)}
			}
		case OpDynAttr:
			if cur.Props == nil {
				cur.Props = map[string]any{}
			}
			cur.Props[op.Key] = nil
			if op.Slot >= 0 && op.Slot < t.N {
				slots[op.Slot] = &slot{kind: slotAttr, path: childPath(cur), attr: op.Key}
			}
		case OpComp:
			// 组件在场景树里是一个逻辑包装节点（Kind=组件名）。它的子节点在
			// applyAll 时由子组件实例的根 children 嫁接而来；编译期只有空壳。
			idx := childOfStack(cur)
			n := cur.Children[idx]
			n.Kind = op.Arg
			n.Props = map[string]any{}
			n.parent = cur
			if op.Slot >= 0 && op.Slot < t.N {
				slots[op.Slot] = &slot{kind: slotComp, path: append(childPath(cur), idx)}
			}
		}
	}
	// 补全未被引用的插槽，避免 nil 解引用（编译期应保证 slot 连续，这里做防御）。
	for i := range slots {
		if slots[i] == nil {
			slots[i] = &slot{kind: slotChild, path: []int{}}
		}
	}
	return &compiledTemplate{root: root, slots: slots}
}

// childPath 返回从根到节点的子节点索引路径。
func childPath(n *Node) []int {
	var path []int
	for n != nil && n.parent != nil {
		p := n.parent
		for i, c := range p.Children {
			if c == n {
				path = append(path, i)
				break
			}
		}
		n = p
	}
	// 反转为 根→叶
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

// clone 复制静态骨架：结构共享常量，Props/Children 深拷贝，避免实例间相互污染。
func clone(n *Node) *Node {
	c := &Node{Kind: n.Kind, Text: n.Text, dirty: true}
	if n.Props != nil {
		c.Props = make(map[string]any, len(n.Props))
		for k, v := range n.Props {
			c.Props[k] = v
		}
	}
	if len(n.Children) > 0 {
		c.Children = make([]*Node, len(n.Children))
		for i, child := range n.Children {
			c.Children[i] = clone(child)
			c.Children[i].parent = c
		}
	}
	return c
}

// atPath 按路径定位 clone 后的节点。
func atPath(root *Node, path []int) *Node {
	n := root
	for _, i := range path {
		if i < 0 || i >= len(n.Children) {
			return n
		}
		n = n.Children[i]
	}
	return n
}

// Instance 是一棵常驻场景树 + 一组已绑定的动态插槽。
type Instance struct {
	tpl     Template
	root    *Node
	slots   []*slot
	Writes  int // 累计实际写入节点的次数（细粒度更新的可观测指标）
	Updates int // Update 调用次数
}

// Render 取（或编译）模板骨架，clone 成常驻树并绑定 getters。
// getters 的数量须等于模板的插槽数 N；缺失的 getter 以 nil 处理（渲染为 <nil> 的零值）。
func Render(tpl Template, getters ...func() any) *Instance {
	key := templateKey(tpl)
	cacheMu.RLock()
	ct := cache[key]
	cacheMu.RUnlock()
	if ct == nil {
		ct = compile(tpl)
		cacheMu.Lock()
		if existing := cache[key]; existing != nil {
			ct = existing
		} else {
			cache[key] = ct
		}
		cacheMu.Unlock()
	}

	root := clone(ct.root)
	inst := &Instance{tpl: tpl, root: root, slots: make([]*slot, len(ct.slots))}
	for i, proto := range ct.slots {
		s := &slot{
			kind: proto.kind,
			path: proto.path,
			attr: proto.attr,
			node: atPath(root, proto.path),
		}
		if i < len(getters) {
			s.get = getters[i]
		}
		inst.slots[i] = s
	}
	inst.applyAll()
	return inst
}

// valueText 把动态值渲染成文本节点内容。
func valueText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	default:
		return fmt.Sprint(x)
	}
}

// Props 是传给复合组件的属性包。动态属性（attr={expr}）以 func() any 形式存放，
// 组件函数只执行一次，因此需用 getter 读取才能在 Update 时看到最新值。
// 静态属性以字面值存放。
type Props map[string]any

// Prop 读一个属性并规整为 getter：动态属性直接返回其 getter，
// 静态属性/缺失属性返回一个恒值/空值 getter。组件体里统一写 `Prop(p,"x")()`。
func Prop(p Props, key string) func() any {
	if p == nil {
		return func() any { return nil }
	}
	if v, ok := p[key]; ok {
		if g, ok := v.(func() any); ok {
			return g
		}
		return func() any { return v }
	}
	return func() any { return nil }
}

// Get 是 Prop 的泛型版本：把属性 getter 的结果断言为 T，便于类型安全地读取。
func Get[T any](p Props, key string) func() T {
	g := Prop(p, key)
	return func() T {
		v := g()
		if t, ok := v.(T); ok {
			return t
		}
		var zero T
		return zero
	}
}

// Child 读取组件的 children（由编译器放入 "children" 属性，值为一个 *Instance getter）。
func Child(p Props) func() any { return Prop(p, "children") }

// graftNode 把 src 挂到 dst 下；#root/#frag 这类透明节点会把自己的 children 摊平，
// 不引入额外标签层。
func graftNode(dst *Node, src *Node) {
	if src == nil {
		return
	}
	if src.Kind == "#root" || src.Kind == "#frag" {
		for _, c := range src.Children {
			graftNode(dst, c)
		}
		return
	}
	src.parent = dst
	dst.Children = append(dst.Children, src)
}

// embedValue 把一个动态值写入子节点槽位。结构性值（*Instance/*Node/切片）按节点嫁接，
// 标量退化为文本节点——这让 `{childJSX}` 和 `{普通值}` 走同一套 slot。
func embedValue(s *slot, v any) {
	s.node.Children = nil
	s.child = nil
	switch x := v.(type) {
	case *Instance:
		s.node.Kind = "#frag"
		s.child = x
		graftNode(s.node, x.root)
	case *Node:
		s.node.Kind = "#frag"
		graftNode(s.node, x)
	case []any:
		s.node.Kind = "#frag"
		for _, e := range x {
			embedAnyChild(s.node, e)
		}
	default:
		s.node.Kind = "#text"
		s.node.Text = valueText(v)
	}
}

func embedAnyChild(dst *Node, v any) {
	switch x := v.(type) {
	case *Instance:
		graftNode(dst, x.root)
	case *Node:
		graftNode(dst, x)
	case []any:
		for _, e := range x {
			embedAnyChild(dst, e)
		}
	default:
		n := &Node{Kind: "#text", Text: valueText(v), parent: dst}
		dst.Children = append(dst.Children, n)
	}
}

// mountComp 把一个组件实例的根 children 嫁接到槽位包装节点下。
// 组件函数只在首次 applyAll 时通过 getter 调用一次；之后靠递归 Update 保鲜。
func mountComp(s *slot, v any) {
	if s.node == nil {
		return
	}
	s.node.Children = nil
	s.child = nil
	inst, ok := v.(*Instance)
	if !ok || inst == nil {
		return
	}
	s.child = inst
	for _, c := range inst.root.Children {
		c.parent = s.node
		s.node.Children = append(s.node.Children, c)
	}
}

// applyAll 首次填充所有插槽（不计入 Writes——那是「更新」指标）。
func (i *Instance) applyAll() {
	for _, s := range i.slots {
		var v any
		if s.get != nil {
			v = s.get()
		}
		s.last = v
		switch s.kind {
		case slotChild:
			embedValue(s, v)
		case slotAttr:
			s.node.Props[s.attr] = v
		case slotComp:
			mountComp(s, v)
		}
	}
}

// Update 重算所有插槽，只把变化的写回节点。返回本次写入的节点数。
// 这是细粒度更新的核心：值未变的插槽不产生任何写入。
func (i *Instance) Update() int {
	i.Updates++
	written := 0
	for _, s := range i.slots {
		// 复合组件：getter 只在首次渲染调用一次（组件实例常驻），
		// 之后递归驱动子组件自身的细粒度更新；不重新 mount、不重建子树。
		if s.kind == slotComp {
			if s.child != nil {
				w := s.child.Update()
				written += w
				i.Writes += w
			}
			continue
		}
		var v any
		if s.get != nil {
			v = s.get()
		}
		// 动态子节点里内嵌的组件/JSX：getter 每次会产出新实例，这里按结构回填，
		// 但不重复计数子树写入（子树自身 Update 时会计入）。
		if s.kind == slotChild {
			if _, ok := v.(*Instance); ok {
				s.last = v
				embedValue(s, v)
				continue
			}
		}
		if equalValue(s.last, v) {
			continue
		}
		s.last = v
		switch s.kind {
		case slotChild:
			embedValue(s, v)
		case slotAttr:
			s.node.Props[s.attr] = v
		}
		s.node.dirty = true
		written++
		i.Writes++
	}
	return written
}

// SetGetter 在运行期替换某个插槽的 getter（用于动态改绑）。
func (i *Instance) SetGetter(slot int, get func() any) {
	if slot >= 0 && slot < len(i.slots) {
		i.slots[slot].get = get
	}
}

// Root 返回场景树根（供 Backend 绘制 / 测试检查）。
func (i *Instance) Root() *Node { return i.root }

// TPL 返回绑定的模板（供高级用法：把同一模板渲染到不同 backend / 数据源）。
func (i *Instance) TPL() Template { return i.tpl }

// Getters 返回绑定的 getter 列表（按 slot 顺序）。
func (i *Instance) Getters() []func() any {
	out := make([]func() any, len(i.slots))
	for j, s := range i.slots {
		out[j] = s.get
	}
	return out
}

// String 用 TextBackend 把常驻场景树渲染成文本，便于 fmt.Println 直接查看。
func (i *Instance) String() string { return (TextBackend{}).Paint(i.root) }

// equalValue 按类型严格比较，避免 true(bool) 与 "true"(string) 混淆。
func equalValue(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return fmt.Sprintf("%T:%v", a, a) == fmt.Sprintf("%T:%v", b, b)
}

// SetPart 是 Update 的单插槽版本：直接写入一个值，绕过 getter（命令式更新）。
func (i *Instance) SetPart(slot int, v any) {
	if slot < 0 || slot >= len(i.slots) {
		return
	}
	s := i.slots[slot]
	if equalValue(s.last, v) {
		return
	}
	s.last = v
	switch s.kind {
	case slotChild:
		embedValue(s, v)
	case slotAttr:
		s.node.Props[s.attr] = v
	case slotComp:
		mountComp(s, v)
	}
	s.node.dirty = true
	i.Writes++
}

// ---------- Backend：绘制抽象，为 Skia / wgpu 留接口 ----------

// Backend 把常驻场景树绘制到具体输出。第一版提供 TextBackend 便于测试；
// Skia / wgpu 后端可另行实现，无需改动编译器或场景树。
type Backend interface {
	Paint(root *Node) string
}

// TextBackend 把场景树渲染成缩进的类 HTML 文本（确定性输出，便于 golden / 断言）。
type TextBackend struct{}

// Paint 实现 Backend。
func (TextBackend) Paint(root *Node) string {
	var b strings.Builder
	for _, c := range root.Children {
		paintNode(&b, c, 0)
	}
	return b.String()
}

func paintNode(b *strings.Builder, n *Node, depth int) {
	indent := strings.Repeat("  ", depth)
	if n.Kind == "#text" {
		if n.Text != "" {
			b.WriteString(indent + strconv.Quote(n.Text) + "\n")
		}
		return
	}
	if n.Kind == "#frag" {
		// 透明包装：只绘制子节点，不产生标签层。
		for _, c := range n.Children {
			paintNode(b, c, depth)
		}
		return
	}
	b.WriteString(indent + "<" + n.Kind)
	for _, k := range sortedKeys(n.Props) {
		v := n.Props[k]
		if v == nil {
			continue
		}
		fmt.Fprintf(b, " %s=%s", k, strconv.Quote(valueText(v)))
	}
	b.WriteString(">\n")
	for _, c := range n.Children {
		paintNode(b, c, depth+1)
	}
	b.WriteString(indent + "</" + n.Kind + ">\n")
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

// RenderText 便捷函数：渲染模板到文本（用给定 getters）。
func RenderText(tpl Template, getters ...func() any) string {
	return (TextBackend{}).Paint(Render(tpl, getters...).Root())
}
