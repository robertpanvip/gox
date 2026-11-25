// Watchdog：纯诊断设施（提案 §10），不是调度设施。
//
// 它能发现「某个 UI 任务执行时间过长」，并尽可能记录任务类型、任务 ID、
// 开始时间与耗时；它不能抢占或杀死正在运行的 Go 回调、不能解除
// mutex/channel 死锁、不能从无限循环中恢复 UI 线程，也不保证界面在阻塞
// 期间仍能继续绘制（提案 §10.1 / §14：Watchdog 只报告，不声称恢复）。
//
// 实现方式：监测 goroutine 以固定周期轮询 Queue.CurrentTask()（exec 在任务
// 开始前记录类型/ID/开始时间，结束后清位）。同一任务按 slow→severe→stuck
// 严格升序、每级只上报一次；级别 >= Severe 时在同一任务上采集一次栈。
// 阈值可配置（下面默认值是诊断建议，不是性能保证，也不构成死锁判定）。
package loop

import (
	"runtime"
	"sync"
	"time"
)

// 默认阈值（提案 §10.2 初始建议）。
const (
	DefaultSlowTask    = 50 * time.Millisecond
	DefaultSevereTask  = 200 * time.Millisecond
	DefaultStuckTask   = 2 * time.Second
	DefaultWatchPeriod = 20 * time.Millisecond
)

// WatchLevel 是耗时诊断级别。
type WatchLevel uint8

const (
	LevelSlow   WatchLevel = iota + 1 // 慢任务：记录类型/耗时/来源
	LevelSevere                       // 严重卡顿：提升日志级别并采集栈
	LevelStuck                        // 长时间阻塞：采集栈并上报
)

func (v WatchLevel) String() string {
	switch v {
	case LevelSevere:
		return "severe"
	case LevelStuck:
		return "stuck"
	default:
		return "slow"
	}
}

// WatchEvent 是一次耗时诊断事件。
type WatchEvent struct {
	Task    TaskInfo
	Elapsed time.Duration
	Level   WatchLevel
	// Stack 仅在级别 >= Severe 时采集，且同一任务只采一次；采集失败为空。
	Stack string
}

// WatchConfig 是 Watchdog 配置。零值字段采用默认建议阈值。
type WatchConfig struct {
	// Slow / Severe / Stuck 三级阈值，必须 0 < Slow <= Severe <= Stuck（由默认值兜底）。
	// 注意：这些值是诊断默认值，不是性能保证，也不是「达到阈值就判定死锁」。
	Slow   time.Duration
	Severe time.Duration
	Stuck  time.Duration
	// CheckInterval 是轮询周期；默认 20ms。
	CheckInterval time.Duration
	// Report 接收诊断事件；在监测 goroutine 上被调用，实现不应长时间阻塞
	// （长时间阻塞只会延迟后续诊断，不会阻塞 UI 任务执行）。panic 被隔离。
	Report func(WatchEvent)
	// StackCapture 采集「当前执行任务」的栈。默认用 runtime.Stack 全进程快照，
	// 且同一任务只在升到 Severe 及以上时调用一次——不做周期性无条件 dump
	// （提案 §10.3：避免无条件、频繁抓取所有 goroutine 的栈）。
	StackCapture func() string
}

// Watchdog 依附于一个 Queue 做只读观测；自身不执行任何 UI 任务。
type Watchdog struct {
	l   *Queue
	cfg WatchConfig

	startOnce sync.Once
	stopOnce  sync.Once
	stop      chan struct{}
	done      chan struct{}

	// 仅监测 goroutine 内部状态（跨 tick 记忆同一任务的上报进度）。
	curID    uint64
	maxLevel WatchLevel
	stacked  bool
}

// NewWatchdog 创建监测器（未启动）。非法/零值阈值取 §10.2 默认建议。
func NewWatchdog(l *Queue, cfg WatchConfig) *Watchdog {
	if cfg.Slow <= 0 {
		cfg.Slow = DefaultSlowTask
	}
	if cfg.Severe <= 0 {
		cfg.Severe = DefaultSevereTask
	}
	if cfg.Stuck <= 0 {
		cfg.Stuck = DefaultStuckTask
	}
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = DefaultWatchPeriod
	}
	if cfg.StackCapture == nil {
		cfg.StackCapture = defaultStackCapture
	}
	return &Watchdog{l: l, cfg: cfg, stop: make(chan struct{}), done: make(chan struct{})}
}

// Start 启动监测 goroutine；幂等。
func (w *Watchdog) Start() { w.startOnce.Do(func() { go w.run() }) }

// Stop 停止监测并等待 goroutine 退出；幂等。
func (w *Watchdog) Stop() {
	w.stopOnce.Do(func() { close(w.stop) })
	<-w.done
}

// Done 在监测 goroutine 退出后关闭（主动 Stop 或 Loop 被关闭都会退出）。
func (w *Watchdog) Done() <-chan struct{} { return w.done }

func (w *Watchdog) run() {
	defer close(w.done)
	tick := time.NewTicker(w.cfg.CheckInterval)
	defer tick.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-w.l.ClosedSignal(): // Loop 关闭后监测 goroutine 退出（§10.4）
			return
		case <-tick.C:
			w.check()
		}
	}
}

// check 对当前执行中的任务做一次分级判定。任务切换或结束即重置进度，
// 因此「任务结束后不再报告该任务」（§10.4）。
func (w *Watchdog) check() {
	task, start, active := w.l.CurrentTask()
	if !active {
		w.curID, w.maxLevel, w.stacked = 0, 0, false
		return
	}
	if task.ID != w.curID { // 换任务：重新计
		w.curID, w.maxLevel, w.stacked = task.ID, 0, false
	}
	elapsed := time.Since(start)
	var lvl WatchLevel
	switch {
	case elapsed >= w.cfg.Stuck:
		lvl = LevelStuck
	case elapsed >= w.cfg.Severe:
		lvl = LevelSevere
	case elapsed >= w.cfg.Slow:
		lvl = LevelSlow
	default:
		return // 正常耗时任务不误报（§10.4）
	}
	if lvl <= w.maxLevel { // 同一任务同一级别只报一次，级别只升不降
		return
	}
	w.maxLevel = lvl
	ev := WatchEvent{Task: task, Elapsed: elapsed, Level: lvl}
	if lvl >= LevelSevere && !w.stacked {
		w.stacked = true
		ev.Stack = w.safeStack()
	}
	if w.cfg.Report != nil {
		w.safeReport(ev)
	}
}

// safeStack：栈采集失败不影响监测与 Loop（§10.4「验证栈采集失败不会导致
// Loop 崩溃」——采集在监测 goroutine 上，且 panic 被 recover）。
func (w *Watchdog) safeStack() (out string) {
	defer func() {
		if r := recover(); r != nil {
			out = ""
		}
	}()
	return w.cfg.StackCapture()
}

// safeReport：Report 回调 panic 只丢弃本次事件，监测 goroutine 存活。
func (w *Watchdog) safeReport(ev WatchEvent) {
	defer func() { _ = recover() }()
	w.cfg.Report(ev)
}

// defaultStackCapture 全 goroutine 快照（低频：每任务在 Severe 时一次）。
func defaultStackCapture() string {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	return string(buf[:n])
}
