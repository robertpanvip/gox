// Package gxscene 是 GX JSX「路线三」的运行时：编译期 opcode + 常驻场景树。
//
// 与「每次重建整棵树 + Diff」的传统虚拟 DOM 方案不同，这里把 JSX 拆成两部分：
//
//   - 静态结构：编译期展开成一串 opcode（Op 序列），只解析一次并缓存成骨架；
//   - 动态插槽：每个 {expr} / attr={expr} 是一个 slot，绑定一个 func() any getter。
//
// Render 时 clone 缓存的静态骨架（廉价、无解析），把 getter 绑到 slot 上得到一棵
// 常驻场景树（Instance）。之后 signal / 外部状态变化只需调 Instance.Update()：
// 它重算每个 slot，只把「值真的变了」的写回对应节点，零 Diff、零整树重建。
//
// 这正是 lit-html「静态模板只解析一次 + 动态 part 原地更新」的思想，但终点不是
// DOM，而是一棵可移植的场景树；host 不定义绘制后端协议——真实 GUI 后端（Skia /
// wgpu / GLFW）实现 gxapp.Window，直接吃 *Node 树自行布局/绘制，host 不碰命中/绘制。
// 依赖追踪也刻意留白：slot getter 是普通闭包，任何响应式系统（如 signal）都能
// 用「读取时订阅、变化时调 Update」的方式驱动它——渲染与响应式核心解耦。
package gxscene

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/robertpanvip/gox/runtime/signal"
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
	OpBranch                  // 条件分支插槽：Slot=索引；getter 返回一个动态子树控制器（*Branch）
	OpFor                     // Keyed 列表插槽：Slot=索引；getter 返回一个动态子树控制器（*For）
	OpEvent                   // 事件绑定插槽：Key=事件名（JSX 原样，如 "onClick"）, Slot=索引；getter 返回 handler
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

// NodeType 是暴露给渲染后端的协议级节点类型。前端只区分这两种形状，
// 具体标签（div/span/button…）由 Kind 承载；后端按 Type 分派渲染逻辑。
// 内部透明节点（#root/#frag）保留 Kind 标记，Type 为 NodeFragment，后端透传其 children。
type NodeType uint8

const (
	NodeText     NodeType = iota // 文本节点：读 Text 画文字
	NodeDiv                      // 元素节点（HTML 原生元素）：读 Kind 作标签、Props 作属性、递归 Children
	NodeFragment                 // 内部透明包装（#root/#frag，含条件分支/keyed 列表）：不画，透传 children
)

// Event 是事件名类型。前端不解释事件语义，仅把 JSX 里的 on* 属性名原样作为 key 透传。
type Event string

// 标准事件表：仅作为「前端认识的已知事件」供 IDE 提示/文档使用，**不是白名单**。
// 任何 on* 属性（含后端自定义的 onFoo）都会无条件透传进 Node.Handlers，不校验、不拦截。
//
// 常量值用驼峰（与 NormalizeEventName 的产物一致），使业务可写 ev.Type == EvMouseEnter
// 直接比较。需要兼容后端传入的小写写法时用 Event.Is（大小写不敏感）。
const (
	EvClick      Event = "click"
	EvMouseEnter Event = "mouseEnter"
	EvMouseLeave Event = "mouseLeave"
	EvMouseMove  Event = "mouseMove"
	EvMouseDown  Event = "mouseDown"
	EvMouseUp    Event = "mouseUp"
	EvKeyDown    Event = "keyDown"
	EvKeyUp      Event = "keyUp"
	EvInput      Event = "input"
	EvChange     Event = "change"
	EvFocus      Event = "focus"
	EvBlur       Event = "blur"
)

// StandardEvents 是标准事件表的切片形式，供 LSP/文档枚举提示。表外事件同样可透传。
var StandardEvents = []Event{
	EvClick, EvMouseEnter, EvMouseLeave, EvMouseMove, EvMouseDown, EvMouseUp,
	EvKeyDown, EvKeyUp, EvInput, EvChange, EvFocus, EvBlur,
}

// EventContext 是后端派发事件时构造的上下文。Raw 由后端填入平台原生事件对象，
// 前端完全不碰——事件语义归后端。
//
// Synthetic 是可选的统一合成事件（见 event.go）：后端若已把原生事件映射为
// SyntheticEvent 就填进来，业务 handler 可从此拿到跨后端一致的字段。为 nil 时
// 完全退回旧的裸透传行为——这是 additive 扩展，老后端不需要任何改动。
type EventContext struct {
	Raw       any
	Synthetic *SyntheticEvent
}

// Handler 是协议推荐的事件处理器签名。注意 Node.Handlers 存的是 any（handler 原值），
// 后端可自行断言为 Handler 或自己约定的任意签名——前端不强制签名，不阻拦自定义事件。
type Handler func(ctx EventContext)

// Node 是常驻场景树的节点。Kind 为标签名（元素）或 "#text"/"#root"/"#frag"（内部）。
// Type 是给后端分派用的协议类型；Handlers 存原样透传的事件处理器（key 为 JSX 事件名原样）。
type Node struct {
	Type     NodeType
	Kind     string
	Props    map[string]any
	Handlers map[string]any // 事件处理器：key=JSX 原样事件名（如 "onClick"），value=handler 原值，前端不解释
	Children []*Node
	Text     string // 仅 NodeText 节点使用

	// ID 是 host 生成、跨帧稳定的逻辑节点 id，供后端做命中缓存、焦点/hover 跟踪等。
	// host 只按结构位置/key 派生它，不解释其语义；后端可完全忽略。
	// 规则：静态节点=结构路径（如 "r/0/1"）；keyed 列表项=".../[key]"；动态文本=".../tN"。
	ID string

	// Layout 是给后端存放自己布局结果（rect/尺寸等）的空挂载点。
	// host 不解释、不填充、不计算——命中/布局/绘制全部归后端，这里只提供存放位置，
	// 避免每个后端被迫维护 map[*Node]any 旁路结构。后端不采用时为 nil。
	Layout any

	parent *Node
	dirty  bool
}

// slotKind 区分动态子节点与动态属性。
type slotKind uint8

const (
	slotChild slotKind = iota
	slotAttr
	slotComp   // 复合组件：getter 返回一个 *Instance，挂载为子树，Update 递归驱动
	slotBranch // 动态子树：getter 返回 subtreeController（*Branch 条件分支 / *For keyed 列表）
	slotEvent  // 事件绑定：getter 返回 handler 原值，写入节点的 Handlers（不进 Props）
)

// subtreeController 是动态子树控制器的公共契约：Branch（条件分支）与 For（keyed 列表）
// 都实现它。Resolve 在当前 effect 上下文求值并重建子树，Dispose 级联清理所有元素 Owner。
type subtreeController interface {
	Resolve() *Instance
	Dispose()
	// adoptSink 把所属实例树的变更 sink 传给控制器，供其动态构建的子实例共享。
	adoptSink(*changeSink)
	// adoptBind 把统一接线上下文传给控制器，供其 Resolve 构建的新子树递归接线。
	adoptBind(*bindCtx)
}

// slot 描述一个动态插槽在场景树中的位置与绑定。
type slot struct {
	kind  slotKind
	path  []int  // 从根到目标节点的子节点索引路径
	attr  string // slotAttr 时的属性名
	get   func() any
	last  any
	node  *Node             // clone 后回填的实际节点
	child *Instance         // slotComp 时挂载的子组件实例
	ctrl  subtreeController // slotBranch/OpFor 时挂载的动态子树控制器
	owner *Instance         // 所属实例，供挂载子实例时共享变更 sink
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
	root := &Node{Kind: "#root", Type: NodeFragment, ID: "r"}
	stack := []*Node{root}
	slots := make([]*slot, t.N)
	childOfStack := func(parent *Node) int {
		idx := len(parent.Children)
		parent.Children = append(parent.Children, &Node{ID: parent.ID + "/" + strconv.Itoa(idx)})
		return idx
	}
	for _, op := range t.Ops {
		cur := stack[len(stack)-1]
		switch op.Code {
		case OpElem:
			idx := childOfStack(cur)
			n := cur.Children[idx]
			n.Kind = op.Arg
			n.Type = NodeDiv
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
			n.Type = NodeText
			n.Text = op.Arg
			n.parent = cur
		case OpDynChild:
			idx := childOfStack(cur)
			n := cur.Children[idx]
			n.Kind = "#text"
			n.Type = NodeText
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
		case OpEvent:
			// 事件走独立的 Handlers 映射，不进 Props——后端绘制属性时不会看到 on*，
			// 派发事件时也只查 Handlers。任何事件名原样透传，不设白名单。
			if cur.Handlers == nil {
				cur.Handlers = map[string]any{}
			}
			cur.Handlers[op.Key] = nil
			if op.Slot >= 0 && op.Slot < t.N {
				slots[op.Slot] = &slot{kind: slotEvent, path: childPath(cur), attr: op.Key}
			}
		case OpComp:
			// 组件在场景树里是一个逻辑包装节点（Kind=组件名，Type=NodeFragment 透明透传）。
			// 它的子节点在 applyAll 时由子组件实例的根 children 嫁接而来；编译期只有空壳。
			idx := childOfStack(cur)
			n := cur.Children[idx]
			n.Kind = op.Arg
			n.Type = NodeFragment
			n.Props = map[string]any{}
			n.parent = cur
			if op.Slot >= 0 && op.Slot < t.N {
				slots[op.Slot] = &slot{kind: slotComp, path: append(childPath(cur), idx)}
			}
		case OpBranch, OpFor:
			// 条件分支 / keyed 列表都是透明包装节点（Kind=#frag 语义），
			// 其子节点由控制器在应用时嫁接而来；编译期只有空壳。
			idx := childOfStack(cur)
			n := cur.Children[idx]
			n.Kind = "#frag"
			n.Type = NodeFragment
			n.Props = map[string]any{}
			n.parent = cur
			if op.Slot >= 0 && op.Slot < t.N {
				slots[op.Slot] = &slot{kind: slotBranch, path: append(childPath(cur), idx)}
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

// 全局实例序号：每次 Render / 动态子树构建分配一个唯一短前缀，保证跨实例、跨列表
// 的 Node.ID 全局唯一。同一实例生命周期内前缀固定，故其节点 ID 跨帧稳定。
var instanceSeq atomic.Uint64

func nextIDBase() string {
	return "i" + strconv.FormatUint(instanceSeq.Add(1), 36)
}

// clone 复制静态骨架：Props/Children 深拷贝避免实例间污染，并把骨架里的相对
// ID（根 "r"、子 "r/0"...）重写为以 base 开头的实例专属 ID。这样两棵由同一
// 缓存骨架 clone 出的树不会共享 ID，后端按 ID 做缓存/焦点跟踪不再串台。
func clone(n *Node, base string) *Node {
	// 骨架根节点 ID 是 "r"，其后代是 "r/0/1" 形式；把前导 "r" 换成 base。
	id := base + strings.TrimPrefix(n.ID, "r")
	c := &Node{Kind: n.Kind, Type: n.Type, Text: n.Text, ID: id, dirty: true}
	if n.Props != nil {
		c.Props = make(map[string]any, len(n.Props))
		for k, v := range n.Props {
			c.Props[k] = v
		}
	}
	if n.Handlers != nil {
		c.Handlers = make(map[string]any, len(n.Handlers))
		for k, v := range n.Handlers {
			c.Handlers[k] = v
		}
	}
	if len(n.Children) > 0 {
		c.Children = make([]*Node, len(n.Children))
		for i, child := range n.Children {
			c.Children[i] = clone(child, base)
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
	comps   []Computation // BindReactive 建立的响应式计算句柄，Dispose 时统一清理
	Writes  int           // 累计实际写入节点的次数（细粒度更新的可观测指标）
	Updates int           // Update 调用次数
	idBase  string        // 本实例专属 ID 前缀； graft 子树时据此重编号，保证全局唯一

	changes *changeSink // 增量变更缓冲；根实例创建，子实例共享同一 sink
	bctx    *bindCtx    // 统一接线上下文；动态子树构建后据此递归接线
}

// Computation 是响应式计算的可销毁句柄。用类型别名统一到 signal 包的定义，
// 使 signal.Effect（func(func()) signal.Computation）能直接作为 BindReactive
// 的实参——Go 函数类型要求返回类型 identical，别名保证二者是同一类型。
type Computation = signal.Computation

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

	base := nextIDBase()
	root := clone(ct.root, base)
	inst := &Instance{tpl: tpl, root: root, slots: make([]*slot, len(ct.slots)), changes: newChangeSink(), idBase: base}
	for i, proto := range ct.slots {
		s := &slot{
			kind:  proto.kind,
			path:  proto.path,
			attr:  proto.attr,
			node:  atPath(root, proto.path),
			owner: inst,
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
		s.node.Type = NodeFragment
		s.child = x
		s.owner.adoptSink(x)
		graftNode(s.node, x.root)
	case *Node:
		s.node.Kind = "#frag"
		s.node.Type = NodeFragment
		graftNode(s.node, x)
	case []any:
		s.node.Kind = "#frag"
		s.node.Type = NodeFragment
		for _, e := range x {
			embedAnyChild(s.owner, s.node, e)
		}
	default:
		s.node.Kind = "#text"
		s.node.Type = NodeText
		s.node.Text = valueText(v)
	}
}

func embedAnyChild(owner *Instance, dst *Node, v any) {
	switch x := v.(type) {
	case *Instance:
		owner.adoptSink(x)
		graftNode(dst, x.root)
	case *Node:
		graftNode(dst, x)
	case []any:
		for _, e := range x {
			embedAnyChild(owner, dst, e)
		}
	default:
		n := &Node{Kind: "#text", Type: NodeText, Text: valueText(v), parent: dst,
			ID: dst.ID + "/t" + strconv.Itoa(len(dst.Children))}
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
	s.owner.adoptSink(inst)
	for _, c := range inst.root.Children {
		c.parent = s.node
		s.node.Children = append(s.node.Children, c)
	}
}

// mountBranch 把条件分支/列表当前命中的子树嫁接到槽位包装节点下。
// 分支切换由 Branch.Resolve / For.Resolve 负责销毁旧 Owner 并构建/复用子树；
// 这里做场景树嫁接，并把"哪些子节点增删/移动"记录成细粒度指令。
func mountBranch(s *slot, sub *Instance) {
	if s.node == nil {
		return
	}
	old := childrenSnapshot(s.node)
	s.node.Children = nil
	s.child = nil
	if sub != nil {
		s.child = sub
		s.owner.adoptSink(sub)
		for _, c := range sub.root.Children {
			c.parent = s.node
			s.node.Children = append(s.node.Children, c)
		}
	}
	s.node.Kind = "#frag"
	// 首渲染时 sink 尚未 enable，diff 内部 add 会 no-op，无需特殊分支。
	if s.owner != nil {
		diffChildren(s.owner.changes, s.node, old, s.node.Children)
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
		case slotEvent:
			// handler 原样存入 Handlers——前端不解释、不校验签名、不进 Props。
			if s.node.Handlers == nil {
				s.node.Handlers = map[string]any{}
			}
			s.node.Handlers[s.attr] = v
		case slotComp:
			mountComp(s, v)
		case slotBranch:
			attachController(i, s, v)
		}
	}
}

// attachController 把 getter 产出的动态子树控制器（*Branch / *For）绑定到 slot，
// 并首次求值、嫁接子树。getter 每次返回同一稳定对象（工厂闭包捕获），因此绑定一次即可。
func attachController(i *Instance, s *slot, v any) {
	ctrl, ok := v.(subtreeController)
	if !ok || ctrl == nil {
		return
	}
	s.ctrl = ctrl
	ctrl.adoptSink(i.changes)
	mountBranch(s, ctrl.Resolve())
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
		// 条件分支：切换由 BindReactive 建立的独立 Effect 驱动（读 selector 登记
		// 依赖并在变化时重建）。这里只递归驱动当前命中子树的细粒度更新，
		// 不重新求值 selector——避免与独立 Effect 形成双重依赖追踪。
		if s.kind == slotBranch {
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
				old := childrenSnapshot(s.node)
				wasText := s.node.Kind == "#text"
				s.last = v
				embedValue(s, v)
				i.recordChildTransition(s.node, old, wasText)
				continue
			}
		}
		if equalValue(s.last, v) {
			continue
		}
		s.last = v
		switch s.kind {
		case slotChild:
			old := childrenSnapshot(s.node)
			wasText := s.node.Kind == "#text"
			embedValue(s, v)
			i.recordChildTransition(s.node, old, wasText)
		case slotAttr:
			s.node.Props[s.attr] = v
			i.record(ChangeSetAttr, s.node, s.attr, v)
		case slotEvent:
			// 事件 handler 换绑（SetGetter/命令式改绑）时更新槽位。
			if s.node.Handlers == nil {
				s.node.Handlers = map[string]any{}
			}
			s.node.Handlers[s.attr] = v
			i.record(ChangeSetHandler, s.node, s.attr, v)
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

// Root 返回场景树根（供 Window.Draw 绘制 / 测试检查）。
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

// Handler 返回节点上绑定的某个事件处理器（原值），未绑定返回 nil。
// 后端命中测试后据此取 handler 并按自己约定的签名调用——前端不解释事件语义。
func (n *Node) Handler(name string) any {
	if n == nil || n.Handlers == nil {
		return nil
	}
	return n.Handlers[name]
}

// Dispatch 是便利派发方法：若 name 对应的 handler 是协议推荐的 Handler 签名，
// 则构造 EventContext 调用之并返回 true；否则返回 false，由后端按自定义签名自行处理。
// 这不影响自定义事件——handler 原值始终可通过 n.Handler(name) 取得。
// 只在命中节点本地派发，不冒泡；需要冒泡见 DispatchBubble。
func (n *Node) Dispatch(name string, ctx EventContext) bool {
	return dispatchLocal(n, name, ctx)
}

// dispatchLocal 是单节点派发的内部实现，供 Dispatch 与冒泡/捕获共用。
// 冒泡/捕获时在调用前会更新 ctx.Synthetic.CurrentTarget。
func dispatchLocal(n *Node, name string, ctx EventContext) bool {
	if n == nil {
		return false
	}
	h := n.Handler(name)
	if h == nil {
		return false
	}
	if fn, ok := h.(Handler); ok {
		fn(ctx)
		return true
	}
	return false
}

// String 用 TextBackend 把常驻场景树渲染成文本，便于 fmt.Println 直接查看。
func (i *Instance) String() string { return (TextBackend{}).Paint(i.root) }

// equalValue 判断两个插槽值是否相等。类型严格（true(bool) != "true"(string)），
// 但避免热路径每次 Sprintf 分配：可比较类型直接用 ==（含所有标量、指针、接口），
// 不可比较类型（切片/map/函数）用 reflect.DeepEqual。
func equalValue(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ta, tb := reflect.TypeOf(a), reflect.TypeOf(b)
	if ta != tb {
		return false // 类型不同即不等，无需比较值
	}
	if ta.Comparable() {
		return a == b
	}
	return reflect.DeepEqual(a, b)
}

// SetPart 是 Update 的单插槽版本：直接写入一个值，绕过 getter（命令式更新）。
func (i *Instance) SetPart(slot int, v any) {
	if slot < 0 || slot >= len(i.slots) {
		return
	}
	s := i.slots[slot]
	// 动态子树控制器（条件分支 / keyed 列表）：v 是 subtreeController（getter
	// 每次返回同一稳定对象）。Resolve 在当前 effect 上下文读依赖并重建/复用子树。
	if s.kind == slotBranch {
		ctrl, ok := v.(subtreeController)
		if !ok || ctrl == nil {
			return
		}
		s.ctrl = ctrl
		// mountBranch 内部已对嫁接前后做逐节点 diff（含列表增删/重排）；
		// 这里不再额外发 ReplaceChildren，否则后端会先收到整块重建再收到细粒度补丁。
		mountBranch(s, ctrl.Resolve())
		i.Writes++
		return
	}
	if equalValue(s.last, v) {
		return
	}
	s.last = v
	switch s.kind {
	case slotChild:
		old := childrenSnapshot(s.node)
		wasText := s.node.Kind == "#text"
		embedValue(s, v)
		i.recordChildTransition(s.node, old, wasText)
	case slotAttr:
		s.node.Props[s.attr] = v
		i.record(ChangeSetAttr, s.node, s.attr, v)
	case slotComp:
		mountComp(s, v)
		i.record(ChangeReplaceChildren, s.node, "", nil)
	}
	s.node.dirty = true
	i.Writes++
}

// BindReactive 把整棵场景树的**叶子插槽**接到外部响应式系统的 effect 原语上，
// 每个 getter 一个精确 effect（不用 Update）。结构插槽由 BindStructural 负责。
//
// effect 形如 signal.Effect：传入的函数立即执行一次，并在它读到的响应式数据源
// 变化时自动重跑。BindReactive 为每个叶子插槽建立一个这样的 effect：
//
//   - 普通插槽（slotChild / slotAttr）：effect 体读 getter 后直接 SetPart，
//     因此某个 signal 变化只精确命中依赖它的那个插槽——不遍历全树、不重建子树；
//   - 复合组件插槽（slotComp）：组件实例常驻，递归对子组件做同样接线，
//     不重新调用组件函数、不重新挂载子树。
//
// 于是 signal 从根经 Props getter 一路传到叶子、在叶子的动态插槽里被读取，
// signal 写入即可自动更新，无需手调 Instance.Update()。signal 写相同值虽会
// 触发 effect 重跑，但 SetPart 的 equalValue 裁剪保证不产生多余写入。
//
// scene 核心不依赖任何具体响应式库：effect 由调用方注入（如 signal.Effect），
// 渲染与响应式解耦——这也是 scene 后端从一开始就留白的那层。
func (i *Instance) BindReactive(effect func(func()) Computation) {
	if i == nil || effect == nil {
		return
	}
	for s, sl := range i.slots {
		if sl.kind == slotComp {
			if sl.child != nil {
				sl.child.BindReactive(effect)
			}
			continue
		}
		// 结构插槽也接线：纯 scene 场景下 BindReactive 是唯一入口，
		// For/Branch 靠这里的 effect 调 Resolve 驱动增删/切换。不注入 bctx——
		// 让控制器继续用裸 BindReactive 给动态子树接线（onDirty 非必需）。
		if sl.kind == slotBranch {
			if sl.child != nil {
				sl.child.BindReactive(effect)
			}
			if sl.get == nil {
				continue
			}
			slot := s
			getter := sl.get
			if c := effect(func() { i.SetPart(slot, getter()) }); c != nil {
				i.comps = append(i.comps, c)
			}
			continue
		}
		if sl.get == nil {
			continue
		}
		slot := s
		getter := sl.get
		if c := effect(func() { i.SetPart(slot, getter()) }); c != nil {
			i.comps = append(i.comps, c)
		}
	}
}

// bindCtx 是结构插槽的统一接线上下文：effect 用于建立订阅，onDirty 在结构
// 变化后回调（app 层据此重绘）。它沿实例树与动态子树向下传递，保证任意深度的
// 条件分支 / 列表都用同一套接线，不会在某一层漏掉 effect 或丢失重绘回调。
type bindCtx struct {
	effect  func(func()) Computation
	onDirty func()
}

// BindStructural 为整棵实例树（含组件子组件与所有命中子树）的**结构插槽**
// （slotBranch：条件分支 / keyed 列表）接线，每个插槽一个 effect：读 selector/each
// → Resolve 重建/复用子树 → 子树继承同一 ctx → 变化后触发 onDirty。
//
// 叶子插槽不在这里——它们由 BindReactive 的细粒度 effect 或 app 层的聚合 Update
// 负责，二者职责分离。关键修复：递归 slotComp 与嵌套子树，使组件内的 if、分支
// 内嵌的分支都能登记依赖并触发重绘（旧实现两个 P0 的根因即此处不递归）。
//
// effect 由调用方注入；onDirty 可为 nil（纯 scene 场景无需重绘）。ctx 同时注入
// Branch/For 控制器，供其在 Resolve 构建新子树后递归接线。
func (i *Instance) BindStructural(effect func(func()) Computation, onDirty ...func()) {
	if i == nil || effect == nil {
		return
	}
	var dirty func()
	if len(onDirty) > 0 {
		dirty = onDirty[0]
	}
	i.bindStructure(&bindCtx{effect: effect, onDirty: dirty})
}

// bindStructure 递归为所有层级的 slotBranch 接线。
func (i *Instance) bindStructure(ctx *bindCtx) {
	if i == nil || ctx == nil {
		return
	}
	i.bctx = ctx
	for s, sl := range i.slots {
		slot := s
		switch sl.kind {
		case slotComp:
			// 组件实例常驻：递归进子组件，接上组件内部的条件分支/列表。
			if sl.child != nil {
				sl.child.bindStructure(ctx)
			}
		case slotBranch:
			if sl.ctrl != nil {
				// ctx 交给控制器，供其后续 Resolve 构建的新子树递归接线。
				sl.ctrl.adoptBind(ctx)
				if sl.child != nil {
					sl.child.bindStructure(ctx)
				}
			}
			if sl.get == nil {
				continue
			}
			getter := sl.get
			if c := ctx.effect(func() {
				i.SetPart(slot, getter())
				if ctx.onDirty != nil {
					ctx.onDirty()
				}
			}); c != nil {
				i.comps = append(i.comps, c)
			}
		}
	}
}

// bindRec 用已有 ctx 给一棵动态子树接结构插槽。供 Branch/For 在 Resolve 构建
// 新子树后调用——新子树内的嵌套分支需要继承同一 ctx（含 onDirty 重绘回调）。
func (i *Instance) bindRec(ctx *bindCtx) {
	i.bindStructure(ctx)
}

// Dispose 销毁该实例通过 BindReactive 建立的所有响应式计算，停止订阅并清理。
// 幂等：多次调用只清理一次。复合子组件的计算由其自身实例负责。
func (i *Instance) Dispose() {
	for _, c := range i.comps {
		c.Dispose()
	}
	i.comps = nil
	for _, sl := range i.slots {
		if sl.kind == slotComp && sl.child != nil {
			sl.child.Dispose()
		}
		if sl.kind == slotBranch && sl.ctrl != nil {
			sl.ctrl.Dispose()
		}
	}
}

// ---------- 文本渲染工具（非后端协议） ----------
//
// 注意：host 不定义「绘制后端协议」。真实 GUI 后端（Skia / wgpu / GLFW）实现的是
// gxapp.Window，直接吃 *Node 树自行布局/绘制。这里只有 TextBackend 一个具体工具，
// 用于把场景树渲染成确定性文本（golden / 断言 / HeadlessWindow 输出），它不是
// 需要后端去实现的接口。曾有一个 Backend interface（Paint(root) string）已被移除：
// 返回字符串只适合文本，对真实 GUI 后端是误导。

// TextBackend 把场景树渲染成缩进的类 HTML 文本（确定性输出，便于 golden / 断言）。
// 它是具体渲染工具，不是后端需要实现的协议接口。
type TextBackend struct{}

// Paint 把场景树渲染成文本字符串。
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
