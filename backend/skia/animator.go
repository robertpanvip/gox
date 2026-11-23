package skia

import (
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"time"
)

// Animator 是 Skia 后端的 transition 插值引擎，语义与 Rust 端 style.rs::Animator 对齐。
// 它按节点稳定 ID 记录每个属性的动画状态；渲染时对比目标 props 与上次目标，
// 目标变化则从当前插值值起新动画，在 duration 内用缓动函数插值。
//
// 工作方式：apply(root, now) 临时覆盖场景树中在动画属性的值，返回一个 restore
// 函数——调用方绘制完本帧后调用它恢复原始 props，避免污染 host 场景树。
type Animator struct {
	anims   map[animKey]*animState
	targets map[animKey]float64
	colorTg map[animKey]uint32
	curNum  map[animKey]float64
	curCol  map[animKey]uint32
}

type animKey struct {
	nodeID   gxscene.NodeID
	property string
}

type animState struct {
	from, to float64
	start    time.Time
	duration time.Duration
	easing   easing
	// 颜色动画：from/to 为 ARGB，数值插值字段忽略
	color    bool
	fromARGB uint32
	toARGB   uint32
}

func NewAnimator() *Animator {
	return &Animator{
		anims:   make(map[animKey]*animState),
		targets: make(map[animKey]float64),
		colorTg: make(map[animKey]uint32),
		curNum:  make(map[animKey]float64),
		curCol:  make(map[animKey]uint32),
	}
}

// numericProps 是可数值插值的属性集合。
var numericProps = map[string]bool{
	"width": true, "height": true, "opacity": true, "font-size": true,
	"padding": true, "border-radius": true, "border-width": true,
	"top": true, "right": true, "bottom": true, "left": true,
	"gap": true, "row-gap": true, "column-gap": true,
}
// colorProps 是可颜色插值的属性集合。
var colorProps = map[string]bool{"color": true, "bg": true, "background": true, "border": true}

// apply 对本帧场景树应用动画覆盖，返回 restore 函数。
func (a *Animator) apply(root *gxscene.Node, now time.Time) func() {
	type saved struct {
		n   *gxscene.Node
		key string
		old any
		hit bool
	}
	var savedVals []saved
	var visit func(*gxscene.Node)
	visit = func(n *gxscene.Node) {
		if n == nil {
			return
		}
		if trans := parseStyle(n.Props).transition; len(trans) > 0 && n.ID != gxscene.NoNode {
			for _, tr := range trans {
				if tr.property == "all" {
					continue
				}
				if numericProps[tr.property] {
					target, ok := propNum(n.Props[tr.property])
					if !ok {
						continue
					}
					k := animKey{n.ID, tr.property}
					prev, seen := a.targets[k]
					if !seen {
						// 首帧：无动画，直接采用初始值。
						a.targets[k] = target
						a.curNum[k] = target
					} else if prev != target {
						from := target
						if cur, has := a.curNum[k]; has {
							from = cur // 从当前值平滑衔接
						}
						a.anims[k] = &animState{
							from: from, to: target, start: now,
							duration: time.Duration(tr.duration) * time.Millisecond,
							easing:   tr.easing,
						}
						a.targets[k] = target
					}
					if v := a.sampleNum(k, now); v != nil {
						old, hit := n.Props[tr.property]
						n.Props[tr.property] = *v
						savedVals = append(savedVals, saved{n, tr.property, old, hit})
					}
				} else if colorProps[tr.property] {
					cs, ok := propString(n.Props[tr.property])
					if !ok {
						continue
					}
					target, ok := parseColor(cs)
					if !ok {
						continue
					}
					k := animKey{n.ID, tr.property}
					oldT, seen := a.colorTg[k]
					if !seen {
						a.colorTg[k] = target
						a.curCol[k] = target
					} else if oldT != target {
						from := target
						if cur, has := a.curCol[k]; has {
							from = cur
						}
						a.anims[k] = &animState{
							color: true, fromARGB: from, toARGB: target,
							start: now, duration: time.Duration(tr.duration) * time.Millisecond,
							easing: tr.easing,
						}
						a.colorTg[k] = target
					}
					if v := a.sampleColor(k, now); v != nil {
						old, hit := n.Props[tr.property]
						n.Props[tr.property] = argbString(*v)
						savedVals = append(savedVals, saved{n, tr.property, old, hit})
					}
				}
			}
		}
		for _, c := range n.Children {
			visit(c)
		}
	}
	visit(root)
	return func() {
		for _, s := range savedVals {
			if s.hit {
				s.n.Props[s.key] = s.old
			} else {
				delete(s.n.Props, s.key)
			}
		}
	}
}

func (a *Animator) sampleNum(k animKey, now time.Time) *float64 {
	st, ok := a.anims[k]
	if !ok || st.color {
		return nil
	}
	elapsed := now.Sub(st.start)
	if elapsed >= st.duration {
		v := st.to
		a.curNum[k] = v
		delete(a.anims, k)
		return &v
	}
	t := easingValue(st.easing, float32(elapsed.Seconds()/st.duration.Seconds()))
	v := st.from + (st.to-st.from)*float64(t)
	a.curNum[k] = v
	return &v
}

func (a *Animator) sampleColor(k animKey, now time.Time) *uint32 {
	st, ok := a.anims[k]
	if !ok || !st.color {
		return nil
	}
	elapsed := now.Sub(st.start)
	if elapsed >= st.duration {
		v := st.toARGB
		a.curCol[k] = v
		delete(a.anims, k)
		return &v
	}
	t := float64(easingValue(st.easing, float32(elapsed.Seconds()/st.duration.Seconds())))
	r := lerpByte(byte(st.fromARGB>>16&0xff), byte(st.toARGB>>16&0xff), t)
	g := lerpByte(byte(st.fromARGB>>8&0xff), byte(st.toARGB>>8&0xff), t)
	b := lerpByte(byte(st.fromARGB&0xff), byte(st.toARGB&0xff), t)
	al := lerpByte(byte(st.fromARGB>>24&0xff), byte(st.toARGB>>24&0xff), t)
	v := argb(uint32(al), uint32(r), uint32(g), uint32(b))
	a.curCol[k] = v
	return &v
}

func lerpByte(a, b byte, t float64) byte {
	v := float64(a) + (float64(b)-float64(a))*t
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

func argbString(c uint32) string {
	// CSS 8 位 hex 顺序是 #rrggbbaa；parseColor 也按此解析，保持一致。
	return "#" + hex2(byte(c>>16&0xff)) + hex2(byte(c>>8&0xff)) + hex2(byte(c&0xff)) + hex2(byte(c>>24&0xff))
}

func hex2(b byte) string {
	const h = "0123456789abcdef"
	return string([]byte{h[b>>4], h[b&0xf]})
}
