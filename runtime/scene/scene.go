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
	// OpDynComp 动态组件插槽（Solid <Dynamic> 语义）：Slot=索引；getter 返回一个
	// *Instance。与 OpDynChild（children 插槽，实例一劳永逸保稳，Issue #1）不同：
	// 该插槽每次求值 getter，按返回实例的场景模板身份比较——身份变化则替换子树、
	// 身份不变则复用旧子树只做细粒度更新。是"确实想按条件切换组件"的显式出口。
	OpDynComp
	// OpRef 模板引用插槽（命令式逃生口）：Slot=索引；getter 返回一个回调
	// func(*Element)（或其 func(any) 兼容形态）。建树时该节点 ID 分配完毕后回调
	// 一次 *Element 句柄，Dispose 时回调 nil 表示卸载。ref 是一次性挂载/卸载语义，
	// 不进响应式重新绑定——signal 变化不会重跑 ref。
	OpRef
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

// NodeID 是节点的稳定身份，一个 App/Instance 生命周期内唯一且不复用。
//
// 设计（Host 架构评审 P1）：身份**不由图结构位置派生**——插入兄弟节点不会改变
// 其他节点的 ID。节点首次创建时分配一个单调递增的整数，逻辑节点存活（同 key 的
// 列表项、未切换的分支、同一动态 part）就保留该 ID；只有真正重建（新实例）才分配
// 新 ID。这解决了字符串结构路径 ID 的两大问题：插入导致身份漂移、fmt.Sprint(key)
// 让整数 1 与字符串 "1" 碰撞。
//
// 0 是保留的「无节点/空锚点」值（对应旧协议的空字符串），后端据此表示末尾插入。
// 分配器是全局的：不同 Instance / 列表 / 分支各自分配的 ID 天然不撞车。
type NodeID uint64

// NoNode 表示无节点（InsertBefore 的末尾锚点、nil 节点的安全 ID）。
const NoNode NodeID = 0

// nodeIDSeq 全局单调分配器。不随节点销毁复用——评审要求「ID 不能因节点销毁立即
// 复用」，避免后端缓存里旧节点的延迟事件误命中新节点。
var nodeIDSeq atomic.Uint64

// nextNodeID 分配一个全局唯一的节点身份。
func nextNodeID() NodeID { return NodeID(nodeIDSeq.Add(1)) }

// String 返回 NodeID 的十进制字符串形式，供对外协议（Change / 二进制 / JSON）编码。
// 协议层用字符串承载 ID 只是传输格式，不改变「内部是整数稳定身份」的事实。
func (id NodeID) String() string {
	if id == NoNode {
		return ""
	}
	return strconv.FormatUint(uint64(id), 10)
}

// parseNodeID 是 String 的逆操作；空串与非法值归一为 NoNode。
func parseNodeID(s string) NodeID {
	if s == "" {
		return NoNode
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return NoNode
	}
	return NodeID(v)
}

// Node 是常驻场景树的节点。Kind 为标签名（元素）或 "#text"/"#root"/"#frag"（内部）。
// Type 是给后端分派用的协议类型；Handlers 存原样透传的事件处理器（key 为 JSX 事件名原样）。
type Node struct {
	Type     NodeType
	Kind     string
	Props    map[string]any
	Handlers map[string]any // 事件处理器：key=JSX 原样事件名（如 "onClick"），value=handler 原值，前端不解释
	Children []*Node
	Text     string // 仅 NodeText 节点使用

	// ID 是 host 分配的稳定整数身份（见 NodeID）。后端可用它做命中缓存、焦点/hover
	// 跟踪、布局缓存键；不解释其语义，可完全忽略。0（NoNode）表示尚未分配。
	ID NodeID

	// 布局结果等后端私有状态**不放在 Node 上**（Host 评审 P1）：Node 是 host 管理的
	// 场景节点描述，不应变成"什么都能塞"的公共对象。后端用 map[NodeID]any 自行维护
	// 布局缓存——整数 ID 是天然高效的 map key，节点销毁时后端按其 ID 清理即可。
	// 旧的 Layout any 挂载点已移除，因全仓无后端实际使用，移除不破坏既有实现。

	parent *Node
	dirty  bool
}

// slotKind 区分动态子节点与动态属性。
type slotKind uint8

const (
	slotChild slotKind = iota
	slotAttr
	slotComp    // 复合组件：getter 返回一个 *Instance，挂载为子树，Update 递归驱动
	slotBranch  // 动态子树：getter 返回 subtreeController（*Branch 条件分支 / *For keyed 列表）
	slotEvent   // 事件绑定：getter 返回 handler 原值，写入节点的 Handlers（不进 Props）
	slotDynamic // 动态组件（OpDynComp / <Dynamic>）：getter 返回 *Instance，按模板身份切换子树
	slotRef     // 模板引用：getter 返回 func(*Element) 回调，建树/销毁各触发一次，不进响应式重绑
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
	evals int               // 响应式 effect 驱动该插槽 getter 的求值次数（T4.5 可观测指标）
	wired bool              // 是否已建立响应式 effect（T4.5 可观测指标）
	node  *Node             // clone 后回填的实际节点
	child *Instance         // slotComp 时挂载的子组件实例
	ctrl  subtreeController // slotBranch/OpFor 时挂载的动态子树控制器
	owner *Instance         // 所属实例，供挂载子实例时共享变更 sink

	refElem *Element // slotRef 建树时创建的句柄，Dispose 时标记销毁并回调 nil
	refCb   any      // slotRef 的回调原值 func(*Element)，Dispose 时回填 nil
}

// PerfStats 是场景树的可观测性能指标（T4.5）。所有计数在响应式 effect 内累加，
// effect 串行执行（见 signal.scheduleEffect），普通 int 无数据竞争。
type PerfStats struct {
	GetterEvals int // 各插槽 getter 被响应式 effect 求值的总次数（含写相同值）
	Writes      int // 累计实际写入节点的次数（equalValue 裁剪后）
	Updates     int // 命令式 Update() 调用次数
	Slots       int // 插槽总数
	WiredSlots  int // 已建立响应式 effect 的插槽数
}

// Perf 返回当前实例的性能快照（只读，无副作用）。子树各自记在自己的 Instance 上。
func (i *Instance) Perf() PerfStats {
	var s PerfStats
	s.Writes, s.Updates, s.Slots = i.Writes, i.Updates, len(i.slots)
	for _, sl := range i.slots {
		s.GetterEvals += sl.evals
		if sl.wired {
			s.WiredSlots++
		}
	}
	return s
}

// noteEval 记一次插槽 getter 求值（响应式路径调用）。
func (i *Instance) noteEval(slot int) {
	if slot >= 0 && slot < len(i.slots) {
		i.slots[slot].evals++
	}
}

// stableChildComp 返回该插槽当前作为「稳定组件子树」承载的实例，没有则返回 nil。
//
// slotComp（OpComp 组件插槽）恒为稳定子树；slotChild（OpDynChild / children 插槽）
// 在 getter 首次产出 *Instance 后也升级为稳定子树。原因：children getter 每次求值
// 都会 Render 出一个新 *Instance（编译器产物形如 `func() any { return Render(...MyInput...) }`），
// 若每次 Update/SetPart 都重新求值并 embedValue 整棵替换，子树内的组件会被反复重建、
// identity 与局部状态（输入框焦点 token、展开态等）全部丢失（Issue #1）。
//
// 统一为「实例常驻、递归 Update、不重建」语义，与 slotComp 一致，即 SolidJS「组件不重建、
// 结构未变时实例保稳」。slotBranch 虽然也用 child 字段，但生命周期由 ctrl 管理，不在此列。
//
// 例外 slotDynamic（OpDynComp / Solid <Dynamic>）：它是「确实想按条件切换组件」的显式出口，
// 不纳入保稳——每次求值 getter、按模板身份决定复用或替换（见 syncDynamicComp）。
// 缺省的 children 插槽（slotChild）保持保稳，故 Issue #1 不受影响。
func (s *slot) stableChildComp() *Instance {
	if s.child == nil {
		return nil
	}
	if s.kind == slotComp || s.kind == slotChild {
		return s.child
	}
	return nil
}

// dynamicCompKey 返回动态组件插槽当前承载实例的「场景模板身份」。
// 用模板结构（templateKey）而非实例指针作身份：同一组件每次 Render 会 new 出
// 不同 *Instance，但模板相同即视为同一组件（复用旧子树、只 Update 保局部状态）；
// 模板不同才视为切换组件。返回 "" 表示当前无实例或无法判定。
func (s *slot) dynamicCompKey() string {
	if s.child == nil {
		return ""
	}
	return templateKey(s.child.tpl)
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
	// 骨架节点不分配身份（ID=NoNode）：模板是共享原型，真实 ID 由 clone 时分配。
	root := &Node{Kind: "#root", Type: NodeFragment}
	stack := []*Node{root}
	slots := make([]*slot, t.N)
	childOfStack := func(parent *Node) int {
		idx := len(parent.Children)
		parent.Children = append(parent.Children, &Node{})
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
		case OpRef:
			// ref 挂在栈顶元素节点上：不新增节点、不进 Props/Handlers，仅记录目标
			// 路径。建树时该节点已由 clone 分配 ID，applyAll 里回调 func(*Element)。
			if op.Slot >= 0 && op.Slot < t.N {
				slots[op.Slot] = &slot{kind: slotRef, path: childPath(cur)}
			}
		case OpComp, OpDynComp:
			// 组件在场景树里是一个逻辑包装节点（Kind=组件名，Type=NodeFragment 透明透传）。
			// 它的子节点在 applyAll 时由子组件实例的根 children 嫁接而来；编译期只有空壳。
			// OpDynComp 同构，但插槽标记为 slotDynamic（按模板身份切换，见 stableChildComp）。
			idx := childOfStack(cur)
			n := cur.Children[idx]
			n.Kind = op.Arg
			n.Type = NodeFragment
			n.Props = map[string]any{}
			n.parent = cur
			if op.Slot >= 0 && op.Slot < t.N {
				kind := slotComp
				if op.Code == OpDynComp {
					kind = slotDynamic
				}
				slots[op.Slot] = &slot{kind: kind, path: append(childPath(cur), idx)}
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

// clone 复制静态骨架为常驻场景树：Props/Children 深拷贝避免实例间污染，并为每个
// 节点分配一个全新的稳定整数身份（NodeID）。
//
// 与旧的字符串结构路径方案（"r/0/1"→"iN/0/1"）相比：整数身份绑定到节点对象本身，
// 不再从「结构位置」派生。因此即使在相邻位置插入兄弟节点，其他节点 ID 也不变；
// 后端按 ID 缓存的 rect/焦点/滚动状态不会因结构微调而集体失效（Host 评审 P1）。
// 同一骨架被多次 clone 时每次分配新 ID，两棵树不串台。
func clone(n *Node) *Node {
	c := &Node{Kind: n.Kind, Type: n.Type, Text: n.Text, ID: nextNodeID(), dirty: true}
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
	comps   []Computation // BindReactive 建立的响应式计算句柄，Dispose 时统一清理
	Writes  int           // 累计实际写入节点的次数（细粒度更新的可观测指标）
	Updates int           // Update 调用次数

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

	root := clone(ct.root)
	inst := &Instance{tpl: tpl, root: root, slots: make([]*slot, len(ct.slots)), changes: newChangeSink()}
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

// subtreeElems 把任意类型的子树切片规整为 []any，供子节点槽位统一按子树嫁接。
//
// Issue #3：编译器生成的 children 是 []any，但 Go 侧手写组件/命令式构造常写
// []*Instance / []*Node 等类型化切片。Go 中它们与 []any 是不同类型，type switch
// 的 case []any 命不中，会掉进 default 被 valueText 序列化成文本（静默错误）。
// 用反射识别「元素为 *Instance / *Node 的切片」，逐元素规整后交给 embedAnyChild：
// 元素本身是子树则嫁接，其余退化为文本；nil/非子树切片返回 ok=false，保持原语义。
// []any 直接在调用处走快路径，不经过反射。
func subtreeElems(v any) ([]any, bool) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || rv.Kind() != reflect.Slice {
		return nil, false
	}
	n := rv.Len()
	out := make([]any, 0, n)
	for i := 0; i < n; i++ {
		e := rv.Index(i).Interface()
		// 只认元素本身是子树或子树的 []any；非子树切片（[]string/[]int/...）
		// 不当作子树列表，避免把 `{[]string{"a","b"}}` 这类标量切片误嫁接。
		switch e.(type) {
		case *Instance, *Node, []any:
		default:
			if _, ok := subtreeElems(e); !ok {
				return nil, false
			}
		}
		out = append(out, e)
	}
	return out, true
}

// embedValue 把一个动态值写入子节点槽位。结构性值（*Instance/*Node/子树切片）按节点
// 嫁接，标量退化为文本节点——这让 `{childJSX}` 和 `{普通值}` 走同一套 slot。
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
		// 编译器生成的 children 快路径，免反射。
		s.node.Kind = "#frag"
		s.node.Type = NodeFragment
		for _, e := range x {
			embedAnyChild(s.owner, s.node, e)
		}
	default:
		// Issue #3：类型化的子树切片（[]*Instance / []*Node / ...）。
		if elems, ok := subtreeElems(v); ok {
			s.node.Kind = "#frag"
			s.node.Type = NodeFragment
			for _, e := range elems {
				embedAnyChild(s.owner, s.node, e)
			}
			return
		}
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
		// Issue #3：类型化的子树切片递归展开，与 []any 同语义。
		if elems, ok := subtreeElems(v); ok {
			for _, e := range elems {
				embedAnyChild(owner, dst, e)
			}
			return
		}
		// 动态文本节点分配独立稳定 ID（Host 评审 P1：动态文本对应固定 part 身份）。
		n := &Node{Kind: "#text", Type: NodeText, Text: valueText(v), parent: dst, ID: nextNodeID()}
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

// syncDynamicComp 是 slotDynamic（<Dynamic>）的核心：每次求值 getter，按场景模板
// 身份决定复用还是替换子组件实例。
//
//   - getter 返回 *Instance：其模板身份（templateKey）与当前 child 相同 → 视为同一
//     组件，丢弃这次新构造的实例（它未被 adopt/接线），保留旧实例并递归 Update；
//     身份不同 → Dispose 旧子树、嫁接新子树、adoptSinkDeep、按需递归接线，完成组件切换。
//   - getter 返回非 *Instance（标量/切片）→ 退化走 embedValue，与普通 child 插槽一致。
//
// 用模板身份而非指针判等，是因为 children getter 每次求值都 new 一个 *Instance；
// 同一组件的新构造应复用（保局部状态、props 由实例内响应式 getter 保鲜），不同
// 组件才替换——这正是 Solid <Dynamic> 的语义，且不触碰普通 children 插槽的
// Issue #1 保稳路径。
//
// 返回 true 表示本次写入了节点（组件切换或标量变化），供 Update/SetPart 计数。
func (i *Instance) syncDynamicComp(s *slot, v any, ctx *bindCtx) bool {
	if inst, ok := v.(*Instance); ok && inst != nil {
		newKey := templateKey(inst.tpl)
		if s.child != nil && s.child != inst && s.dynamicCompKey() == newKey {
			// 同一组件模板的新构造：复用旧实例，丢弃新构造。props 由旧实例内
			// 的响应式 getter 保鲜；这里只递归驱动旧子树细粒度更新。
			s.last = v
			s.child.Update()
			return false
		}
		if s.child == inst {
			// getter 直接返回同一实例：只递归 Update。
			s.last = v
			s.child.Update()
			return false
		}
		// 组件身份变化（或首次挂载）：替换。
		old := s.child
		if old != nil {
			old.Dispose() // 停订阅 + 清 comps，级联其稳定子树
		}
		oldChildren := childrenSnapshot(s.node)
		s.last = v
		i.mountDynamicComp(s, inst, ctx)
		if i.changes != nil {
			diffChildren(i.changes, s.node, oldChildren, s.node.Children)
		}
		return true
	}
	// 非 *Instance：退化走普通 child 值语义。
	old := childrenSnapshot(s.node)
	wasText := s.node.Kind == "#text"
	s.last = v
	embedValue(s, v)
	i.recordChildTransition(s.node, old, wasText)
	return true
}

// mountDynamicComp 把一个动态组件实例嫁接到 slotDynamic 包装节点下，
// 并共享变更 sink、按需递归接线（app 场景由 bindCtx 提供 effect/onDirty）。
func (i *Instance) mountDynamicComp(s *slot, inst *Instance, ctx *bindCtx) {
	mountComp(s, inst)
	if inst == nil {
		return
	}
	adoptSinkDeep(inst, i.changes)
	if ctx != nil {
		ctx.withSilent(func() {
			inst.bindLeaves(ctx.effect)
			inst.bindStructure(ctx)
		})
	} else {
		inst.BindReactive(Reactive().Effect)
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

// callRef 把 ref getter 的结果当作命令式回调，在节点 ID 已分配后调用一次。
// 主签名 func(*Element)；兼容 func(any)（GX 侧未注解参数时的通用形态）。
// 返回保存下来供 Dispose 回填 nil 的回调原值（无有效回调时返回 nil）。
func (i *Instance) callRef(s *slot, v any) any {
	if s.node == nil || v == nil {
		return nil
	}
	elem := newElement(s.node.ID, i.changes)
	switch fn := v.(type) {
	case func(*Element):
		if elem != nil {
			fn(elem)
		}
		s.refElem = elem
		return v
	case func(any):
		if elem != nil {
			fn(elem)
		}
		s.refElem = elem
		return v
	}
	return nil
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
		case slotDynamic:
			// 首帧只挂载、接线延后到 BindReactive/BindStructural（那里会带 ctx）。
			// 这里不建 effect（由调用方统一接线），故直接挂载而不走 syncDynamicComp。
			if inst, ok := v.(*Instance); ok && inst != nil {
				mountComp(s, inst)
				adoptSinkDeep(inst, i.changes)
			} else {
				embedValue(s, v)
			}
		case slotBranch:
			attachController(i, s, v)
		case slotRef:
			// 一次性挂载语义：建树时 ID 已分配，回调一次 *Element 句柄。
			s.refCb = i.callRef(s, v)
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
		// ref 是一次性挂载/卸载语义，不进响应式重新绑定：Update 不重求值 ref getter。
		if s.kind == slotRef {
			continue
		}
		// 稳定组件子树：slotComp 组件插槽，或 slotChild（children 插槽）首次求值产出
		// 的 *Instance。getter 只在首次渲染调用一次，之后递归驱动子组件自身的细粒度
		// 更新；不重新求值 getter、不重建子树——组件实例与局部状态保稳（Issue #1）。
		if ch := s.stableChildComp(); ch != nil {
			w := ch.Update()
			written += w
			i.Writes += w
			continue
		}
		// Bug 03：动态组件插槽（<Dynamic>）不做稳定锁定——每次求值 getter，
		// 按模板身份复用或替换子组件（syncDynamicComp）。
		if s.kind == slotDynamic {
			var v any
			if s.get != nil {
				v = s.get()
			}
			if i.syncDynamicComp(s, v, i.bctx) {
				written++
				i.Writes++
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
	// Bug 03：动态组件插槽按模板身份切换，不走下面的 equalValue 快照裁剪
	// （同一个 *Instance 指针幂等由 syncDynamicComp 内部处理）。
	if s.kind == slotDynamic {
		if i.syncDynamicComp(s, v, i.bctx) {
			s.node.dirty = true
			i.Writes++
		}
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
		// ref 不建响应式 effect：它是一次性生命周期回调，signal 变化不重跑。
		if sl.kind == slotRef {
			continue
		}
		// 稳定组件子树：slotComp 或承载 *Instance 的 slotChild。组件实例常驻，
		// 递归进子树接线（叶子 + 内部的结构插槽都接）；不为它建 SetPart(getter())
		// effect——getter 每次产出新 *Instance，重求值会重建子树、丢失局部状态（Issue #1）。
		if ch := sl.stableChildComp(); ch != nil {
			ch.BindReactive(effect)
			continue
		}
		switch sl.kind {
		case slotDynamic:
			// <Dynamic>：给当前子树递归接叶子；为 getter 建 effect 驱动组件切换。
			// SetPart→syncDynamicComp 按模板身份决定复用/替换，替换时新子树由
			// mountDynamicComp 递归接线（无 ctx 退化路径）。
			if sl.child != nil {
				sl.child.BindReactive(effect)
			}
			if sl.get != nil {
				slot := s
				getter := sl.get
				i.slots[slot].wired = true
				if c := effect(func() {
					i.noteEval(slot)
					i.SetPart(slot, getter())
				}); c != nil {
					i.comps = append(i.comps, c)
				}
			}
		case slotBranch:
			// 纯 scene 场景下 BindReactive 是唯一入口，For/Branch 靠这里的 effect
			// 调 Resolve 驱动增删/切换。app 场景走 BindStructural——两条路径互斥，
			// 由调用方择一，避免动态子树的 slotBranch 被双订阅。
			if sl.child != nil {
				sl.child.BindReactive(effect)
			}
			if sl.get != nil {
				slot := s
				getter := sl.get
				i.slots[slot].wired = true
				if c := effect(func() {
					i.noteEval(slot)
					i.SetPart(slot, getter())
				}); c != nil {
					i.comps = append(i.comps, c)
				}
			}
		default:
			if sl.get == nil {
				continue
			}
			slot := s
			getter := sl.get
			i.slots[slot].wired = true
			if c := effect(func() {
				i.noteEval(slot)
				i.SetPart(slot, getter())
			}); c != nil {
				i.comps = append(i.comps, c)
			}
		}
	}
}

// BindLeafEffects 是 bindLeaves 的导出入口：为整棵稳定树（含组件子树）的叶子
// 插槽建立每插槽 effect，不碰结构插槽。
//
// app 场景用它替代「一个聚合 effect 调 Update 遍历全树」：每个叶子 getter 只在
// 依赖它的 signal 变化时重求值并 SetPart，值未变则零写入；所有 effect 跑完后由
// signal 的 AfterEffects 帧边界统一触发一次窗口重绘（T1.1 插槽级 + T1.2 帧合并）。
// 结构插槽（条件/列表）由 BindStructural 负责，二者职责互斥，避免双订阅。
func (i *Instance) BindLeafEffects(effect func(func()) Computation) {
	i.bindLeaves(effect)
}

// bindLeaves 只给叶子插槽（slotChild/slotAttr/slotEvent）接线，不碰结构插槽；
// slotComp 递归进子组件。供「已有 bindCtx 负责结构接线」的场景使用——动态子树
// 的叶子需要自己的细粒度 effect（不在父级聚合 Update 的递归路径上），而结构由
// bindRec(ctx) 统一处理，二者分工避免 selector 被双订阅。
func (i *Instance) bindLeaves(effect func(func()) Computation) {
	if i == nil || effect == nil {
		return
	}
	for s, sl := range i.slots {
		// ref 不建响应式 effect：一次性生命周期回调，signal 变化不重跑。
		if sl.kind == slotRef {
			continue
		}
		// 稳定组件子树（slotComp / 承载 *Instance 的 slotChild）：递归进子树接叶子，
		// 不为其建 SetPart(getter()) effect（getter 重求值会重建子树，Issue #1）。
		if ch := sl.stableChildComp(); ch != nil {
			ch.bindLeaves(effect)
			continue
		}
		// slotDynamic（<Dynamic>）的 getter effect 由 bindStructure 建立（组件切换
		// 是结构变化，需触发 onDirty 重绘）；这里只递归给当前子树接叶子。
		if sl.kind == slotDynamic {
			if sl.child != nil {
				sl.child.bindLeaves(effect)
			}
			continue
		}
		if sl.kind == slotBranch || sl.get == nil {
			continue
		}
		slot := s
		getter := sl.get
		i.slots[slot].wired = true
		if c := effect(func() {
			i.noteEval(slot) // T4.5：记一次 getter 求值（响应式路径）
			i.SetPart(slot, getter())
		}); c != nil {
			i.comps = append(i.comps, c)
		}
	}
}

// bindCtx 是结构插槽的统一接线上下文：effect 用于建立订阅，onDirty 在结构
// 变化后回调（app 层据此重绘）。它沿实例树与动态子树向下传递，保证任意深度的
// 条件分支 / 列表都用同一套接线，不会在某一层漏掉 effect 或丢失重绘回调。
//
// silent 是共享的静音开关：接线期、以及 For/Branch 动态构建子树期间置位，
// 此时各 effect 的首次回调不触发 onDirty（避免首帧或批量新增时重复重绘）。
// 所有动态子树共享同一个 ctx 指针，故值字段即可全局生效。
type bindCtx struct {
	effect  func(func()) Computation
	onDirty func()
	silent  bool
}

// withSilent 在静音状态下执行 fn，结束后恢复原状态（支持嵌套）。
func (c *bindCtx) withSilent(fn func()) {
	if c == nil {
		fn()
		return
	}
	old := c.silent
	c.silent = true
	defer func() { c.silent = old }()
	fn()
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
	ctx := &bindCtx{effect: effect, onDirty: dirty}
	// 接线期整体静音：各 effect 首次立即执行会读 selector 建子树并调 onDirty，
	// 但 Render 已画首帧，这些首次回调不该各自触发重绘（N 个结构插槽 = N 帧）。
	ctx.silent = true
	i.bindStructure(ctx)
	ctx.silent = false
}

// bindStructure 递归为所有层级的 slotBranch 接线。
func (i *Instance) bindStructure(ctx *bindCtx) {
	if i == nil || ctx == nil {
		return
	}
	i.bctx = ctx
	for s, sl := range i.slots {
		slot := s
		// 稳定组件子树（slotComp / 承载 *Instance 的 slotChild）：递归进子树接上
		// 内部的条件分支/列表，保证组件内结构插槽也登记依赖（Issue #1 保稳子树同步）。
		if ch := sl.stableChildComp(); ch != nil {
			ch.bindStructure(ctx)
			continue
		}
		switch sl.kind {
		case slotDynamic:
			// <Dynamic> 组件切换是结构变化：当前子树递归接结构插槽，并为 getter
			// 建 effect（SetPart→syncDynamicComp 按模板身份切换）+ 变化后 onDirty 重绘。
			if sl.child != nil {
				sl.child.bindStructure(ctx)
			}
			if sl.get == nil {
				continue
			}
			getter := sl.get
			i.slots[slot].wired = true
			if c := ctx.effect(func() {
				i.noteEval(slot)
				i.SetPart(slot, getter())
				if ctx.onDirty != nil && !ctx.silent {
					ctx.onDirty()
				}
			}); c != nil {
				i.comps = append(i.comps, c)
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
			i.slots[slot].wired = true
			if c := ctx.effect(func() {
				i.noteEval(slot)
				i.SetPart(slot, getter())
				if ctx.onDirty != nil && !ctx.silent {
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
		if ch := sl.stableChildComp(); ch != nil {
			ch.Dispose()
		}
		if sl.kind == slotDynamic && sl.child != nil {
			sl.child.Dispose()
		}
		if sl.kind == slotBranch && sl.ctrl != nil {
			sl.ctrl.Dispose()
		}
		if sl.kind == slotRef {
			// 卸载语义：先标记句柄失效，再回调 nil（与建树时的一次性回调对称）。
			// 支持 func(*Element) 与 func(any) 两种签名。
			if sl.refElem != nil {
				sl.refElem.disposeRef()
			}
			switch fn := sl.refCb.(type) {
			case func(*Element):
				fn(nil)
			case func(any):
				fn(nil)
			}
			sl.refCb = nil
			sl.refElem = nil
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
