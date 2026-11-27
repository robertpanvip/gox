package skia

import (
	"time"

	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
)

// animation.go —— Skia 后端的声明式动画采样引擎，语义与 Rust/Web 端严格对齐。
//
// 与 transition Animator 的关系：Animator 做「目标值变化 → 插值」的过渡；
// 本文件做「无状态、按绝对时钟采样」的周期动画（当前仅 blink）。二者都用
// 「临时覆盖节点 props → 绘制 → restore」的同构手法，避免污染常驻场景树。
//
// 采样相位用 UnixMilli 取模（见 gxscene.AnimationSpec.Sample），因此无需为每个
// 节点记录起始时间，也无需跨帧持有状态——渲染纯函数化，帧缓存快照安全。

// applyAnimations 在绘制前对本帧场景树应用 animation 的 opacity 采样覆盖，
// 返回 restore 函数（绘制后调用，恢复原始 props，不污染 host 树）。
//
// now 参数由调用方传入（通常 time.Now()），便于测试用固定时刻验证相位。
func applyAnimations(root *gxscene.Node, now time.Time) func() {
	type saved struct {
		n   *gxscene.Node
		old any
		hit bool
	}
	var savedVals []saved
	var visit func(*gxscene.Node)
	visit = func(n *gxscene.Node) {
		if n == nil {
			return
		}
		if n.ID != gxscene.NoNode {
			if spec, ok := gxscene.AnimationFromProps(n.Props); ok {
				op, active := spec.Sample(now.UnixMilli())
				if active {
					old, hit := n.Props[propOpacity]
					n.Props[propOpacity] = op
					savedVals = append(savedVals, saved{n, old, hit})
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
				s.n.Props[propOpacity] = s.old
			} else {
				delete(s.n.Props, propOpacity)
			}
		}
	}
}

// treeHasAnimation 报告树中是否存在活跃动画，供绘制循环决定是否需要持续重绘。
func treeHasAnimation(root *gxscene.Node) bool {
	if root == nil {
		return false
	}
	found := false
	var visit func(*gxscene.Node)
	visit = func(n *gxscene.Node) {
		if n == nil || found {
			return
		}
		if _, ok := gxscene.AnimationFromProps(n.Props); ok {
			found = true
			return
		}
		for _, c := range n.Children {
			visit(c)
		}
	}
	visit(root)
	return found
}

// HasActiveAnimation 是 treeHasAnimation 的导出包装，供宿主（GUI 集成层）在
// 事件循环里判断是否需要维持重绘节奏——blink 这类周期动画没有外部事件唤醒，
// 必须由宿主持续请求下一帧，否则光标只在交互瞬间闪烁一次。
func HasActiveAnimation(root *gxscene.Node) bool { return treeHasAnimation(root) }
