package gxscene

import "strings"

// ---------------- 统一合成事件（additive 扩展，不改既有透传契约） ----------------
//
// 这一层解决的是「业务 handler 被绑死在具体后端」的问题：
// EventContext.Raw 是平台原生事件，形状由后端决定；同一份 GX 组件换后端就得改
// raw.(xxxEvent) 断言。SyntheticEvent 由 host 定义统一字段，后端把原生事件映射进来，
// handler 只读这个对象即可跨后端移植。
//
// 设计约束（与 backend-integration.md 的黄金规则一致）：
//   - additive：EventContext 只是多一个可选字段，Raw 保留，老后端零改动；
//   - host 只定义形状与规范化逻辑，命中/派发/布局仍归后端；
//   - 不设白名单：任何 on* 事件名都能合成，NormalizeEventName 对表外事件同样工作。

// Modifier 是修饰键位掩码，供各后端把平台修饰键统一映射到位。
type Modifier uint8

const (
	ModShift Modifier = 1 << iota
	ModCtrl
	ModAlt
	ModMeta
)

// Has 报告 m 是否包含指定的修饰键组合（全部置位）。
func (m Modifier) Has(flag Modifier) bool { return m&flag == flag }

// SyntheticEvent 是跨后端一致的合成事件对象。后端在命中并派发时构造它，
// 把平台原生事件映射到统一字段；不能映射的字段留零值，原生对象放 Raw 不丢。
type SyntheticEvent struct {
	Type          Event          // 规范化事件类型，如 EvClick（由事件名推导）
	NativeName    string         // 原 JSX 事件名，如 "onClick"
	Target        *Node          // 命中节点（派发起点，冒泡全程不变）
	CurrentTarget *Node          // 当前正在处理该事件的节点（冒泡/捕获中变化）
	X, Y          float64        // 位置（后端按自己的坐标系填，host 不解释单位）
	Button        int            // 鼠标按键序号
	Key           string         // 按键名（规范化由后端决定）
	Mods          Modifier       // 修饰键位掩码
	Raw           any            // 平台原生事件对象，原样保留
	Data          map[string]any // 后端自定义扩展位，host 不解释

	propagationStopped bool
	defaultPrevented   bool
}

// StopPropagation 终止冒泡/捕获：DispatchBubble / DispatchCapture 在此节点处理后停止。
func (e *SyntheticEvent) StopPropagation() {
	if e != nil {
		e.propagationStopped = true
	}
}

// PreventDefault 标记默认动作应被阻止；具体是否执行由后端读取后决定。
func (e *SyntheticEvent) PreventDefault() {
	if e != nil {
		e.defaultPrevented = true
	}
}

// IsPropagationStopped 报告是否已调用 StopPropagation。
func (e *SyntheticEvent) IsPropagationStopped() bool {
	return e != nil && e.propagationStopped
}

// IsDefaultPrevented 报告是否已调用 PreventDefault。
func (e *SyntheticEvent) IsDefaultPrevented() bool {
	return e != nil && e.defaultPrevented
}

// NormalizeEventName 把 JSX 事件名规范化为 Event 类型：
// 去掉 "on" 前缀并整体小写（"onClick"→"click"，"onMouseEnter"→"mouseenter"，
// "onFoo"→"foo"）。整体小写是为了和标准事件表常量对齐——业务可写
// ev.Type == gxscene.EvMouseEnter。表外自定义事件同样适用——它不是白名单。
func NormalizeEventName(name string) Event {
	s := name
	if len(s) > 2 && strings.HasPrefix(s, "on") && s[2] >= 'A' && s[2] <= 'Z' {
		s = s[2:]
	}
	return Event(strings.ToLower(s))
}

// NewSyntheticEvent 构造合成事件，自动填好 Type / NativeName / Target / CurrentTarget，
// 其余字段后端按需补齐。raw 原样保存在 Raw。
func NewSyntheticEvent(nativeName string, target *Node, raw any) *SyntheticEvent {
	return &SyntheticEvent{
		Type:          NormalizeEventName(nativeName),
		NativeName:    nativeName,
		Target:        target,
		CurrentTarget: target,
		Raw:           raw,
	}
}

// ---------------- 冒泡 / 捕获派发（additive 便利方法，不改命中归属） ----------------
//
// 命中测试与「先冒泡还是先捕获」的调度仍归后端；这里只提供两个沿既有 parent 链
// 走的方法，后端可选调用。两者都只在节点绑定了协议签名的 handler 时触发，
// 自定义签名的 handler 原值始终可经 n.Handler(name) 取得，不被拦截。

// ancestorChain 返回从根到 n（含 n）的节点链。
func ancestorChain(n *Node) []*Node {
	var chain []*Node
	for cur := n; cur != nil; cur = cur.parent {
		chain = append(chain, cur)
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

// ensureSynthetic 保证 ctx 带一个合成事件；Target 固定为命中节点 target，
// 已由后端填好的 Synthetic 原样沿用（不覆盖后端映射的字段）。
func ensureSynthetic(ctx *EventContext, nativeName string, target *Node) {
	if ctx.Synthetic == nil {
		ctx.Synthetic = NewSyntheticEvent(nativeName, target, ctx.Raw)
	} else {
		if ctx.Synthetic.Target == nil {
			ctx.Synthetic.Target = target
		}
		if ctx.Synthetic.NativeName == "" {
			ctx.Synthetic.NativeName = nativeName
		}
		if ctx.Synthetic.Type == "" {
			ctx.Synthetic.Type = NormalizeEventName(nativeName)
		}
	}
}

// DispatchBubble 从命中节点 n 沿 parent 链向根冒泡派发 name：先 n，再父、祖……
// 每步把 ctx.Synthetic.CurrentTarget 更新为当前节点后调用其 handler；任一 handler
// 调 StopPropagation 即停止。返回是否有任一 handler 被触发。
func (n *Node) DispatchBubble(name string, ctx EventContext) bool {
	if n == nil {
		return false
	}
	ensureSynthetic(&ctx, name, n)
	fired := false
	for cur := n; cur != nil; cur = cur.parent {
		ctx.Synthetic.CurrentTarget = cur
		if dispatchLocal(cur, name, ctx) {
			fired = true
		}
		if ctx.Synthetic.IsPropagationStopped() {
			break
		}
	}
	return fired
}

// DispatchCapture 从根向命中节点 n 自顶向下捕获派发 name：先根，再……最后 n。
// 与 DispatchBubble 共用 StopPropagation 语义；CurrentTarget 每步更新。
func (n *Node) DispatchCapture(name string, ctx EventContext) bool {
	if n == nil {
		return false
	}
	ensureSynthetic(&ctx, name, n)
	fired := false
	for _, cur := range ancestorChain(n) {
		ctx.Synthetic.CurrentTarget = cur
		if dispatchLocal(cur, name, ctx) {
			fired = true
		}
		if ctx.Synthetic.IsPropagationStopped() {
			break
		}
	}
	return fired
}
