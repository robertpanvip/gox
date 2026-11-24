package gxweb

import (
	"errors"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// ElemID 是 DOM 元素的后端句柄。js 驱动下是 js.Value 的索引，mem 驱动下是
// 内存元素地址；后端核心只把它当不透明整数，不解释其含义。0 表示无效。
type ElemID uint64

const noElem ElemID = 0

// Driver 是 Web 后端与「DOM 实现」之间的最小契约。syscall/js 驱动与内存驱动
// 都实现它，让后端核心（reconcile / 事件 / 能力）与平台解耦、可在 native 单测。
//
// 方法语义刻意贴近真实 DOM：Create/Remove/InsertBefore/SetAttr/SetText，
// 因此 js 驱动几乎是直译，mem 驱动则维护一棵镜像树供断言。
type Driver interface {
	// Create 按场景 Kind 创建元素。"#text" 创建文本节点，其余创建对应 HTML 标签；
	// "#root"/"#frag" 这类透明节点由后端核心处理，不会下传到 Create。
	Create(kind string, id gxscene.NodeID) ElemID
	// Mount 把元素挂到宿主容器（js 为 querySelector 选中的节点/body，mem 为内存根）。
	Mount(e ElemID)
	// Destroy 移除并释放元素及其子树。
	Destroy(e ElemID)
	// InsertBefore 把 child 插入 parent，位于 before 之前；before==0 表示追加到末尾。
	InsertBefore(parent, child, before ElemID)
	// SetAttr 设置属性/样式。key 是 GX 样式名（bg/font-size/...）或普通 DOM 属性名，
	// 由驱动负责映射到 CSS。
	SetAttr(e ElemID, key, value string)
	// RemoveAttr 移除属性；value 为空串时驱动需把该 key 恢复默认（回退）。
	RemoveAttr(e ElemID, key string)
	// SetText 设置文本节点的文本内容。
	SetText(e ElemID, text string)
	// Listen 为元素注册事件监听。event 是规范化事件名（click/input/...，无 on 前缀）。
	// 回调 fired 由浏览器/测试触发，携带合成事件字段；后端据此构造 SyntheticEvent 回投。
	Listen(e ElemID, event string, cb func(fired FiredEvent))
	// Focus 让元素获得焦点。
	Focus(e ElemID)
	// Rect 读取元素最近一次布局矩形，ok=false 表示无布局（未挂载/隐藏）。
	Rect(e ElemID) (x, y, w, h float32, ok bool)
	// ComputedStyle 读取元素的计算样式（含默认/inherit/伪状态由 DOM 决定）。
	// state 为空读基础态，否则为 "hover"/"focus"/"active"/"disabled"。
	ComputedStyle(e ElemID, state string) map[string]string
	// CanvasOps 把 canvas 绘制指令（JSON）交给驱动解释。mem 驱动记录指令供断言；
	// js 驱动用 2D context 真实绘制。
	CanvasOps(e ElemID, opsJSON string)
}

// FiredEvent 是浏览器事件回投到 Go 的统一载荷。字段与 gxscene.SyntheticEvent 对齐，
// 驱动只负责把平台原生事件映射进来，后端核心据此构造合成事件。
type FiredEvent struct {
	Name   string // 规范化事件名（click/input/keydown/...）
	NodeID gxscene.NodeID
	X, Y   float64
	Button int
	Key    string
	Mods   uint8 // gxscene.Modifier 位掩码
	Value  string // input/textarea 的当前值
	Raw    any   // 平台原生对象，原样放进 SyntheticEvent.Raw
}

var (
	errNotOpen    = errors.New("gxweb: 窗口未打开")
	errClosed     = errors.New("gxweb: 窗口已关闭")
	errNoSuchNode = errors.New("gxweb: 事件目标节点已不存在")
)
