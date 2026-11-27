package gxapp

import (
	"github.com/robertpanvip/gox/runtime/scheduler"
)

// SetFrameClock 启用统一帧调度（BACKEND_ARCHITECTURE_V2 阶段 1：Signal 更新与
// Present 解耦）。必须在首次 Render 之前调用。
//
// 启用后的行为变化：
//
//   - Signal 更新不再立即触发绘制，只标脏 + 向平台时钟预约一帧；
//     同一刷新周期内的多次更新合并为一次提交（验收标准）。
//   - 绘制发生在平台的帧时机上（RAF / Choreographer / vsync 对齐），
//     60/90/120/144Hz 由平台时钟决定，不是 Go 定时器硬编码。
//   - 无变化时不预约帧、不绘制（无变化零持续绘制的 Host 侧保证；
//     动画由 RAF 续帧机制经由同一时钟请求下一帧）。
//   - 动画（el.requestAnimationFrame 续帧）统一由 FrameClock 驱动，
//     取代「goroutine 异步续帧 / skia dirty 维持 / Android paint.Event」
//     三套后端各自的续帧机制。
//
// 未调用时行为完全不变（InlineDispatcher / UIExecutorDispatcher 老路径），
// headless 与既有测试零影响。
//
// 典型用法：
//
//	clock := scheduler.NewManualClock()            // 测试 / headless
//	clock := scheduler.NewTickerClock(time.Second/60) // 桌面兜底
//	// Web/Android 后端各自提供 RAF/Choreographer 实现的 FrameClock
//	app.SetFrameClock(clock)
func (a *App) SetFrameClock(clock scheduler.FrameClock) {
	if a == nil || clock == nil || a.closed.Load() {
		return
	}
	a.frameClock = clock
	s := scheduler.New(clock)
	s.SetOnFrame(func(ft scheduler.FrameTime) { _ = a.drawNowErr() })
	a.frameSched = s
}

// FrameScheduler 返回统一帧调度器（未启用 FrameClock 时返回 nil）。
// 平台可在窗口事件里直接操作它：后台暂停 Pause / 恢复 Resume / 取消帧 CancelFrame。
func (a *App) FrameScheduler() *scheduler.Scheduler { return a.frameSched }

// requestDrawFrame 统一派发一次「重绘请求」：启用 FrameClock 时走 Scheduler
// （合并到下一个平台帧时机），否则走 FrameDispatcher（老路径，行为不变）。
// 所有绘制入口（帧边界 / RAF 续帧 / 命令式写回 / Draw）统一经过这里。
func (a *App) requestDrawFrame() {
	if a.frameSched != nil {
		a.frameSched.RequestFrame()
		return
	}
	a.dispatcher.RequestFrame()
}

// closeFrameClock 释放帧调度资源（App.Close 调用）：停调度器与兜底时钟。
func (a *App) closeFrameClock() {
	if a.frameSched != nil {
		a.frameSched.Close()
	}
	if a.frameClock != nil {
		if c, ok := a.frameClock.(interface{ Close() }); ok {
			c.Close()
		}
	}
}
