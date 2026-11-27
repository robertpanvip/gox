//go:build js && wasm

package gxweb

import (
	"sync/atomic"
	"syscall/js"
	"time"

	"github.com/robertpanvip/gox/runtime/scheduler"
)

// rafClock 把浏览器的 requestAnimationFrame 适配为 scheduler.FrameClock
// （BACKEND_ARCHITECTURE_V2 阶段 1：Signal 更新与 Present 解耦的 Web 实现）。
//
// 与 postRAF 的关系：postRAF 是「UIExecutor 任务队列」（多条任务同帧依次执行）；
// rafClock 是「帧时机时钟」——RequestFrame 在下一 RAF 时刻派发一次 OnFrame，
// 由 FrameScheduler 的 dirty 门控保证无变化零绘制、同周期多次更新合并一帧。
// 与 postRAF 的去重注释同理：每次 RequestFrame 都注册 RAF（回调幂等空跑），
// 不依赖「同帧只注册一次」——个别 RAF 回调被 wasm 事件机吃掉时后续注册仍会
// 驱动帧推进，不会永久卡死。
type rafClock struct {
	fn        atomic.Value // func(scheduler.FrameTime)
	pending   atomic.Bool
	onRequest func()       // 注册下一 RAF（由 jsDriver 注入 postRAF）
	last      atomic.Int64 // 纳秒，Delta 基准
}

// FrameClock 返回统一帧时钟。浏览器环境用 requestAnimationFrame 驱动；
// memDriver（无浏览器，测试/headless wasm）返回 ManualClock 由宿主驱动。
// 用法：app.SetFrameClock(win.FrameClock())（Render 之前调用）。
func (w *Window) FrameClock() scheduler.FrameClock {
	if jsd, ok := w.driver.(*jsDriver); ok {
		return jsd.frameClock()
	}
	return scheduler.NewManualClock()
}

func (d *jsDriver) frameClock() scheduler.FrameClock {
	c := &rafClock{}
	c.onRequest = func() { d.postRAF(c.emit) }
	return c
}

func (c *rafClock) Now() time.Duration {
	return time.Duration(jsNow())
}

func (c *rafClock) OnFrame(fn func(scheduler.FrameTime)) { c.fn.Store(fn) }

// RequestFrame 在下一 RAF 派发一帧。不做 pending 短路——多注册无害
// （Scheduler 的 dirty 门控使多余回调空跑），换来抗回调丢失的健壮性。
func (c *rafClock) RequestFrame() {
	c.pending.Store(true)
	if c.onRequest != nil {
		c.onRequest()
	}
}

func (c *rafClock) CancelFrame() { c.pending.Store(false) }

// emit 在 RAF 回调里执行：派发一次 OnFrame（是否绘制由 Scheduler 的 dirty 决定）。
func (c *rafClock) emit() {
	now := jsNow()
	c.last.Store(now)
	if fn, ok := c.fn.Load().(func(scheduler.FrameTime)); ok && fn != nil {
		fn(scheduler.FrameTime{Timestamp: time.Duration(now)})
	}
}

// jsNow 返回 performance.now()（毫秒浮点）的纳秒 int64；无 performance 时退 wall clock。
func jsNow() int64 {
	p := js.Global().Get("performance")
	if p.Truthy() {
		ms := p.Call("now").Float()
		return int64(ms * float64(time.Millisecond))
	}
	return time.Since(processStart).Nanoseconds()
}

var processStart = time.Now()
