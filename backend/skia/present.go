package skia

import (
	"fmt"
	"time"

	gxapp "github.com/robertpanvip/gox-core/runtime/app"
	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
)

// 本文件让 skia.Window 实现 gxapp.FramePresenter，消费 Host 的增量变更流。
//
// 背景：skia 的帧缓存（framecache.go）原本挂在 Window.Draw 老路径上，每帧都要
// snapshotTree(root) 全量遍历整棵树拍内容快照（O(N) + 字符串拼接）才能判断
// "有没有变化"。而 Web/Rust 后端早已通过 Frame.Changes 做增量提交，唯独 skia
// 因为走 Draw 兼容路径拿不到 Changes——这是三端里唯一的不对称。
//
// Present 接入后：
//   - 无 Change + 无动画 + 非 FullSync：直接复用缓存的 log/hits 与画布，
//     跳过 snapshotTree（O(N) → O(1)）。这是主要收益。
//   - 有 Change 或动画：仍走 renderCached 的全量快照 + 脏矩形局部重绘，
//     保持原有的正确性（结构/颜色/滚动变化都能被快照捕获）。
//   - FullSync：调用方要求强制全量，清掉缓存有效性后走 renderCached。
//
// 正确性依据：所有画面变化都经 Change 流到达——signal 写回 record(ChangeSetAttr/
// ChangeSetText/结构指令)，命令式 CanvasContext 写回也 record(ChangeSetAttr)。
// 唯一例外是 transition 动画：apply 在渲染期临时写 props、不产生 Change，故用
// Animator.Active() 显式排除。FullSync 是丢帧恢复的兜底，必须全量。
//
// Draw 老路径完全不动：headless 测试、PaintTo/PaintToCached 调用方零影响。
var _ gxapp.FramePresenter = (*Window)(nil)

// Present 实现 gxapp.FramePresenter：按 Frame 的变更信息提交一帧。
func (w *Window) Present(frame *gxapp.Frame) error {
	if frame == nil {
		return w.Draw(nil)
	}
	root := frame.Root

	w.mu.Lock()
	if w.canvas.IsNil() {
		w.mu.Unlock()
		return fmt.Errorf("skia: 未 Open，无法 Present")
	}
	w.log = nil
	w.hits = nil
	if root == nil {
		w.mu.Unlock()
		return nil
	}

	animActive := w.anim != nil && w.anim.Active()
	// 增量快速路径：无 Host Change、无动画、非 FullSync、缓存有效、根与尺寸不变。
	// 满足时画面必然未变，O(1) 复用，跳过 O(N) snapshotTree。
	if !frame.FullSync && len(frame.Changes) == 0 && !animActive &&
		w.cache.canReuseFast(root, float32(w.width), float32(w.height)) {
		w.log, w.hits = w.cache.reuseFast()
		w.frames++
		w.mu.Unlock()
		return nil
	}

	// 慢速路径：有变更/动画/FullSync。FullSync 强制让缓存失效，走全量重绘。
	if frame.FullSync {
		w.cache.valid = false
	}
	if w.anim != nil {
		restore := w.anim.apply(root, time.Now())
		w.log, w.hits = renderCached(&w.cache, w.canvas, root, float32(w.width), float32(w.height), w.fonts)
		restore()
	} else {
		w.log, w.hits = renderCached(&w.cache, w.canvas, root, float32(w.width), float32(w.height), w.fonts)
	}
	w.frames++

	// 回流实测尺寸，锁外投递（PublishMeasured 会写 signal，锁内会重入）。
	measured := append([]gxscene.Measured(nil), w.cache.lastMeasured...)
	w.mu.Unlock()

	gxscene.PublishMeasured(measured)
	return nil
}
