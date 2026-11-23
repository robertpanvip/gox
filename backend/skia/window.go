package skia

import (
	"fmt"
	"sync"
	"time"

	sk "github.com/pekim/go-skia"
	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// Window 实现 gxapp.Window：一个基于真实 Skia（pekim/go-skia CGO 绑定）的
// 离屏光栅后端。
//
// 它不依赖 GLFW / 窗口系统：每帧 Draw 把场景树遍历成一组绘制指令，逐条调用
// 真实 Skia 绘制 API 画到一块 raster canvas。同时保留一份「绘制指令记录」
// （DrawLog）用于无窗口环境下的可验证性——证明场景树确实被正确布局/分派。
//
// 真实 GUI 窗口集成只需把内部 canvas 换成 GrDirectContext 包装的 GL surface
// （见 README「接入真实窗口」），协议面与绘制遍历逻辑完全不变。
//
// 并发：Skia canvas 非并发安全，Draw 用互斥锁串行化。
type Window struct {
	mu     sync.Mutex
	cfg    gxapp.WindowConfig
	canvas sk.Canvas
	width  int
	height int
	frames int
	closed bool

	// fonts 按字号缓存，避免每帧构造字体（Close 时统一释放）。
	fonts map[int]sk.Font

	// log 记录最近一帧的绘制指令，供测试/调试断言（可选观测面，不属于 host 协议）。
	log DrawLog
	// hits 记录最近一帧每个节点的屏幕矩形，供真实窗口后端命中测试。
	hits []HitBox
	// anim 是 transition 插值引擎；跨帧持有动画状态。
	anim *Animator
}

var _ gxapp.Window = (*Window)(nil) // 编译期断言：满足 host 后端契约

// NewWindow 创建一个 Skia 后端实例。零配置，Open 时按 WindowConfig 分配画布。
// 返回具体 *Window 而非 gxapp.Window，便于调用方访问 DrawLog 等后端特有 API。
func NewWindow() *Window { return &Window{} }

// Open 按配置创建 Skia raster 画布。Backend 提示字段被忽略（本后端就是 skia）。
func (w *Window) Open(cfg gxapp.WindowConfig) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return fmt.Errorf("skia: 已关闭的窗口不能重新 Open")
	}
	if cfg.Width <= 0 {
		cfg.Width = 800
	}
	if cfg.Height <= 0 {
		cfg.Height = 600
	}
	cfg.Backend = "skia"

	canvas := sk.NewCanvasWithDimensions(int32(cfg.Width), int32(cfg.Height), sk.NewSurfaceProps())
	if canvas.IsNil() {
		return fmt.Errorf("skia: 创建 %dx%d raster canvas 失败", cfg.Width, cfg.Height)
	}
	// 初始清成白色，避免未绘制区域是未定义像素。
	canvas.Clear(sk.ColorSetARGB(255, 255, 255, 255))

	if w.canvas.IsNil() == false {
		w.canvas.Delete()
	}
	w.canvas = canvas
	w.width, w.height = cfg.Width, cfg.Height
	w.cfg = cfg
	w.fonts = make(map[int]sk.Font)
	w.log = nil
	w.anim = NewAnimator()
	return nil
}

// Draw 把当前场景树绘制一帧：遍历 Node 树 → 布局 → 逐条调用真实 Skia 绘制 API。
func (w *Window) Draw(root *gxscene.Node) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.canvas.IsNil() {
		return fmt.Errorf("skia: 未 Open，无法 Draw")
	}
	w.log = nil
	w.hits = nil
	if root == nil {
		return nil
	}

	p := &painter{
		canvas: w.canvas,
		width:  float32(w.width),
		height: float32(w.height),
		fonts:  w.fonts,
		log:    &w.log,
		hits:   &w.hits,
	}
	w.canvas.Clear(sk.ColorSetARGB(255, 255, 255, 255))
	// transition：应用本帧动画覆盖（临时写 props），绘制后恢复，避免污染 host 树。
	if w.anim != nil {
		restore := w.anim.apply(root, time.Now())
		p.paintFrame(root, float32(w.width), float32(w.height))
		restore()
	} else {
		p.paintFrame(root, float32(w.width), float32(w.height))
	}
	w.frames++
	return nil
}

// Run 是 headless 后端：无事件循环，立即返回。
// 真实窗口后端在此驱动事件循环（命中测试→Node.Dispatch→signal 写入→host 自动重绘），
// 该职责完全在后端侧——host 的 Window 契约里没有事件方法，这正是解耦的体现。
func (w *Window) Run() error { return nil }

// Close 释放 Skia 资源。
func (w *Window) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	for _, f := range w.fonts {
		f.Delete()
	}
	w.fonts = nil
	if w.canvas.IsNil() == false {
		w.canvas.Delete()
	}
	w.closed = true
	return nil
}

// Config 返回实际生效的配置。
func (w *Window) Config() gxapp.WindowConfig {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cfg
}

// Frames 返回已绘制帧数（可观测指标，便于验证响应式重绘触发）。
func (w *Window) Frames() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.frames
}

// DrawLog 返回最近一帧的绘制指令记录（观测面，非 host 协议）。
// 调用方拿到的是副本，可安全读取。
func (w *Window) DrawLog() DrawLog {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append(DrawLog(nil), w.log...)
}

// Hits 返回最近一帧的命中矩形副本（观测面，非 host 协议）。
// 真实窗口后端据此把鼠标坐标映射到场景节点，再调用 Node.Dispatch 分发事件。
func (w *Window) Hits() []HitBox {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]HitBox(nil), w.hits...)
}

// HitTest 返回覆盖屏幕点 (x, y) 的最上层节点（后绘制者优先）。
// 命中矩形由最近一帧 Draw 记录；无命中返回 nil。
func (w *Window) HitTest(x, y float32) *gxscene.Node {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := len(w.hits) - 1; i >= 0; i-- {
		if w.hits[i].Contains(x, y) {
			return w.hits[i].Node
		}
	}
	return nil
}

// PaintTo 用本后端一致的 painter 把场景树画到任意 Skia canvas（供真实窗口复用布局/绘制协议）。
// width/height 为像素尺寸；返回本帧命中矩形。fonts 由调用方长期持有以复用字体缓存。
func PaintTo(canvas sk.Canvas, width, height float32, root *gxscene.Node, fonts map[int]sk.Font) (DrawLog, []HitBox) {
	var log DrawLog
	var hits []HitBox
	if canvas.IsNil() || root == nil {
		return log, hits
	}
	p := &painter{canvas: canvas, width: width, height: height, fonts: fonts, log: &log, hits: &hits}
	canvas.Clear(sk.ColorSetARGB(255, 255, 255, 255))
	p.paintFrame(root, width, height)
	return log, hits
}
