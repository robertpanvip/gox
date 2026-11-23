package gxscene

import (
	"sort"
	"strconv"
	"strings"
)

// 本文件实现 ref 句柄的 el.getComputedStyle()：DOM 风格的只读样式快照。
//
// 设计要点（与 Element 其余命令式方法一致）：
//   - 返回 *ComputedStyle 而不是裸 props map：业务用 getPropertyValue("bg") 读值，
//     不接触 Node/Props 内部结构，也不接触 NodeID；
//   - 数据源有两级：后端可选能力注入的真实计算样式（含默认值/继承/动画插值，
//     由 gxapp 从实现了 ComputedStyler 的窗口注入）优先；未注入时回退到场景节点
//     Props 里已展平的声明样式（纯 scene / headless 零改动）；
//   - 快照语义：每次调用生成一份字符串快照，不随后续属性更新自动变化（与
//     BoundingClientRect 一致）；需要最新值再调一次即可。
//
// GX 侧按小写书写（el.getComputedStyle() / style.getPropertyValue("bg")），
// 由编译器的 transformExternal 还原为首字母大写的方法。

// ComputedStyle 是元素某一时刻的计算样式快照（DOM CSSStyleDeclaration 的只读子集）。
// 键是受限 CSS sheet 的属性名（见 StylePropNames），值是字符串化的计算值。
//
// 它刻意是纯值类型：不含 NodeID 或节点指针，业务拿到的只是一份不可变快照。
type ComputedStyle struct {
	vals map[string]string
}

// newComputedStyle 由属性表构造快照。调用方负责传入已归一化的键值表；
// 这里复制一份，避免外部后续改动影响快照。
func newComputedStyle(vals map[string]string) *ComputedStyle {
	out := make(map[string]string, len(vals))
	for k, v := range vals {
		out[k] = v
	}
	return &ComputedStyle{vals: out}
}

// emptyComputedStyle 返回一份空快照（nil/dead 句柄的统一返回值）。
func emptyComputedStyle() *ComputedStyle {
	return &ComputedStyle{vals: map[string]string{}}
}

// GetPropertyValue 返回属性名对应的计算值字符串；属性不存在时返回空串。
// 属性名大小写不敏感、忽略首尾空白（Web getPropertyValue 语义）。
//
// GX 小写写法：style.getPropertyValue("bg")
func (c *ComputedStyle) GetPropertyValue(name string) string {
	if c == nil {
		return ""
	}
	return c.vals[strings.ToLower(strings.TrimSpace(name))]
}

// Has 报告该属性是否在快照中有值（区分"未设置"与"值为空串"）。
func (c *ComputedStyle) Has(name string) bool {
	if c == nil {
		return false
	}
	_, ok := c.vals[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

// Length 返回快照中的属性个数。
func (c *ComputedStyle) Length() int {
	if c == nil {
		return 0
	}
	return len(c.vals)
}

// PropertyNames 返回快照中所有属性名（小写），按字典序排列，便于稳定遍历与断言。
func (c *ComputedStyle) PropertyNames() []string {
	if c == nil {
		return nil
	}
	names := make([]string, 0, len(c.vals))
	for k := range c.vals {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// normalizePseudo 把 getComputedStyle(el, pseudo) 的伪状态写法规整成状态名：
// "" / ":hover" / "&:hover" / "$:active" → "" / "hover" / "hover" / "active"。
// 与 Style 的伪状态 key 约定一致（见 style.go）。
func normalizePseudo(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	for _, prefix := range []string{PseudoPrefix, legacyPseudoPrefix} {
		if strings.HasPrefix(p, prefix) {
			p = strings.TrimPrefix(p, prefix)
			break
		}
	}
	p = strings.TrimPrefix(p, ":")
	return strings.ToLower(p)
}

// GetComputedStyle 返回该节点当前的计算样式快照。
//
// pseudo 是可选的伪状态名（Web getComputedStyle(el, pseudoElt) 的对应物）：
// "hover" / ":hover" / "&:hover" 等价，缺省为空表示基础样式。传入伪状态时，
// 快照会在基础属性之上叠加该伪状态子表的同名覆盖（子表取值经过与 Style.Flatten
// 相同的合语义），让业务能读到 hover/active 下的实际颜色等。
//
// 数据源优先级：
//  1. gxapp 注入的 ComputedStyler（后端可提供含默认值/动画插值的权威值）；
//  2. 回退到节点 Props 里已展平的声明样式——headless / 纯 scene 零改动。
//
// nil / 已销毁句柄返回空快照，不返回 nil（调用方可安全地链式取值）。
//
// GX 小写写法：el.getComputedStyle() / el.getComputedStyle("hover")
func (e *Element) GetComputedStyle(pseudo ...string) *ComputedStyle {
	if e == nil || e.dead || e.node == nil {
		return emptyComputedStyle()
	}
	state := ""
	if len(pseudo) > 0 {
		state = normalizePseudo(pseudo[0])
	}

	// ① 后端权威计算样式（可选能力）：后端返回 nil 表示放弃，继续走回退。
	if c := e.caps(); c.computedStyle != nil {
		if vals := c.computedStyle(e.id, state); vals != nil {
			return newComputedStyle(vals)
		}
	}

	// ② 回退：从节点 Props 里读已展平的样式。Props 可能含非样式键（id/class/事件），
	// 这里按 StylePropNames 白名单过滤，只暴露受限 CSS sheet 的属性。
	vals := make(map[string]string)
	for _, key := range StylePropNames {
		if v, ok := e.node.Props[key]; ok {
			if s, ok := styleValueString(v); ok {
				vals[key] = s
			}
		}
	}
	// 伪状态子表叠加（与 Style.Flatten 同语义：子表同名 key 覆盖基础）。
	if state != "" {
		if sub, ok := PseudoStateFromProps(map[string]any(e.node.Props), state); ok {
			for key, v := range sub {
				key = strings.ToLower(key)
				if !stylePropSet[key] || isPseudoKey(key) {
					continue
				}
				if s, ok := styleValueString(v); ok {
					vals[key] = s
				}
			}
		}
	}
	return newComputedStyle(vals)
}

// styleValueString 把 Props 里的标量样式值字符串化。与 Skia 的 propString 同源
// 语义（字符串原样、常见标量用 fmt.Sprint），但只接受 isStyleScalar 允许的标量，
// 复杂值（slice/map/函数）一律忽略——避免把结构误当成样式值暴露。
func styleValueString(v any) (string, bool) {
	if !isStyleScalar(v) {
		return "", false
	}
	switch x := v.(type) {
	case nil:
		return "", true
	case string:
		return x, true
	case bool:
		return strconv.FormatBool(x), true
	case int:
		return strconv.Itoa(x), true
	case int32:
		return strconv.FormatInt(int64(x), 10), true
	case int64:
		return strconv.FormatInt(x, 10), true
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32), true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	default:
		return "", false
	}
}
