//go:build !js || !wasm

package gxweb

import (
	"strings"
	"sync"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// memDriver 是内存 DOM 驱动，用于 native 单测、快照断言与无浏览器的 headless 场景。
// 它维护一棵与浏览器 DOM 同构的镜像树，字段命名刻意对齐真实 DOM 便于对照。
type memDriver struct {
	mu      sync.Mutex
	next    ElemID
	elements map[ElemID]*memElem
	root    ElemID
	events  []memFired // 已触发事件记录（观测面）
}

type memElem struct {
	id       ElemID
	nodeID   gxscene.NodeID
	kind     string
	text     string
	attrs    map[string]string
	styles   map[string]string
	canvas   []string // canvas 指令 JSON 历史
	richHTML string   // 最近一次 innerHTML 写入的富文本片段（测试断言用）
	parent   ElemID
	children []ElemID
	listeners map[string]func(FiredEvent)
	focused  bool
	rect     memRect
}

type memRect struct{ x, y, w, h float32; ok bool }

type memFired struct {
	Elem  ElemID
	Event FiredEvent
}

func newMemDriver() *memDriver {
	d := &memDriver{elements: map[ElemID]*memElem{}}
	d.root = d.Create("#root", 0)
	return d
}

func (d *memDriver) Create(kind string, id gxscene.NodeID) ElemID {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.next++
	e := &memElem{
		id: d.next, nodeID: id, kind: kind,
		attrs: map[string]string{}, styles: map[string]string{},
		listeners: map[string]func(FiredEvent){},
	}
	d.elements[e.id] = e
	return e.id
}

func (d *memDriver) Mount(e ElemID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	el, root := d.elements[e], d.elements[d.root]
	if el == nil || root == nil || e == d.root {
		return
	}
	if el.parent != 0 {
		if old := d.elements[el.parent]; old != nil {
			old.children = removeID(old.children, e)
		}
	}
	root.children = append(root.children, e)
	el.parent = d.root
}

func (d *memDriver) Destroy(e ElemID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.destroyLocked(e)
}

func (d *memDriver) destroyLocked(e ElemID) {
	el := d.elements[e]
	if el == nil {
		return
	}
	for _, c := range el.children {
		d.destroyLocked(c)
	}
	if p := d.elements[el.parent]; p != nil {
		p.children = removeID(p.children, e)
	}
	delete(d.elements, e)
}

func (d *memDriver) InsertBefore(parent, child, before ElemID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p, c := d.elements[parent], d.elements[child]
	if p == nil || c == nil {
		return
	}
	if c.parent != 0 {
		if old := d.elements[c.parent]; old != nil {
			old.children = removeID(old.children, child)
		}
	}
	idx := len(p.children)
	if before != 0 {
		for i, id := range p.children {
			if id == before {
				idx = i
				break
			}
		}
	}
	p.children = append(p.children, 0)
	copy(p.children[idx+1:], p.children[idx:])
	p.children[idx] = child
	c.parent = parent
}

func (d *memDriver) SetAttr(e ElemID, key, value string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	el := d.elements[e]
	if el == nil {
		return
	}
	// ::selection 伪元素子表按 Web 端等价语义记录成两个自定义属性，
	// 让 native 测试能断言与浏览器一致的落地形态（见 js 驱动 SetAttr）。
	if isSelectionKey(key) {
		bg, fg := selectionVars(value)
		if bg == "" {
			delete(el.styles, selectionVarBg)
		} else {
			el.styles[selectionVarBg] = cssValue("bg", bg)
		}
		if fg == "" {
			delete(el.styles, selectionVarFg)
		} else {
			el.styles[selectionVarFg] = cssValue("color", fg)
		}
		return
	}
	// animation 与浏览器同构：记录翻译后的原生 CSS animation 值，空/非法值删除。
	if strings.EqualFold(key, "animation") {
		css := animationCSS(value)
		if css == "" {
			delete(el.styles, "animation")
		} else {
			el.styles["animation"] = css
		}
		return
	}
	if isStyleKey(key) {
		if value == "" {
			delete(el.styles, key)
		} else {
			el.styles[key] = value
		}
		return
	}
	if value == "" {
		delete(el.attrs, key)
	} else {
		el.attrs[key] = value
	}
}

func (d *memDriver) RemoveAttr(e ElemID, key string) {
	d.SetAttr(e, key, "")
}

func (d *memDriver) SetText(e ElemID, text string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if el := d.elements[e]; el != nil {
		el.text = text
	}
}

func (d *memDriver) SetRichText(e ElemID, richHTML string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if el := d.elements[e]; el != nil {
		el.richHTML = richHTML
	}
}

func (d *memDriver) Listen(e ElemID, event string, cb func(FiredEvent)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if el := d.elements[e]; el != nil {
		el.listeners[event] = cb
	}
}

func (d *memDriver) Focus(e ElemID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, el := range d.elements {
		el.focused = false
	}
	if el := d.elements[e]; el != nil {
		el.focused = true
	}
}

func (d *memDriver) Rect(e ElemID) (x, y, w, h float32, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if el := d.elements[e]; el != nil {
		return el.rect.x, el.rect.y, el.rect.w, el.rect.h, el.rect.ok
	}
	return 0, 0, 0, 0, false
}

func (d *memDriver) ComputedStyle(e ElemID, state string) map[string]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	el := d.elements[e]
	if el == nil {
		return map[string]string{}
	}
	out := map[string]string{}
	for k, v := range el.styles {
		out[k] = v
	}
	// mem 驱动不做真实伪状态计算：把 "&:hover" 这类子表在显式请求时叠加进去，
	// 便于 native 单测验证伪状态读取路径。
	if state != "" {
		sub := "&:" + strings.TrimPrefix(state, "&:")
		for k, v := range el.styles {
			if strings.HasPrefix(k, sub+".") {
				out[strings.TrimPrefix(k, sub+".")] = v
			}
		}
	}
	return out
}

func (d *memDriver) CanvasOps(e ElemID, opsJSON string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if el := d.elements[e]; el != nil {
		el.canvas = append(el.canvas, opsJSON)
	}
}

// ---- 观测面（测试断言用） ----

// Fire 模拟浏览器触发一次事件：定位元素 → 调用其监听 → 记录。
func (d *memDriver) Fire(e ElemID, ev FiredEvent) bool {
	d.mu.Lock()
	el := d.elements[e]
	if el == nil {
		d.mu.Unlock()
		return false
	}
	cb := el.listeners[ev.Name]
	ev.NodeID = el.nodeID
	d.events = append(d.events, memFired{Elem: e, Event: ev})
	d.mu.Unlock()
	if cb != nil {
		cb(ev)
		return true
	}
	return false
}

// Element 返回元素快照副本（不可变观测面）。
func (d *memDriver) Element(e ElemID) *memElem {
	d.mu.Lock()
	defer d.mu.Unlock()
	el := d.elements[e]
	if el == nil {
		return nil
	}
	cp := *el
	cp.attrs = map[string]string{}
	for k, v := range el.attrs { cp.attrs[k] = v }
	cp.styles = map[string]string{}
	for k, v := range el.styles { cp.styles[k] = v }
	cp.children = append([]ElemID(nil), el.children...)
	cp.canvas = append([]string(nil), el.canvas...)
	cp.listeners = nil
	return &cp
}

// Root 返回内存 DOM 根句柄。
func (d *memDriver) Root() ElemID { return d.root }

// SetRect 注入布局矩形（测试 Measurer 用）。
func (d *memDriver) SetRect(e ElemID, x, y, w, h float32) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if el := d.elements[e]; el != nil {
		el.rect = memRect{x, y, w, h, true}
	}
}

// FiredEvents 返回已触发事件记录副本。
func (d *memDriver) FiredEvents() []memFired {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]memFired(nil), d.events...)
}

func removeID(ids []ElemID, id ElemID) []ElemID {
	for i, v := range ids {
		if v == id {
			return append(ids[:i], ids[i+1:]...)
		}
	}
	return ids
}
