// Package loop 提供进程级共享的 UI 调度循环（GX 设计提案：UI 调度循环与 UIPromise，P1 阶段）。
//
// 定位（提案 §3.2）：Loop 只是一个任务调度器——
//   - 接收宏任务 / 微任务；
//   - 队列自身并发安全；
//   - 任务只在宿主允许的 UI 上下文中被执行（何时执行完全由宿主决定）；
//   - 关闭后拒绝新任务，返回稳定错误；
//   - 提供「有任务待处理」的唤醒回调，供后端接自己的主循环。
//
// Loop 不创建事件循环、不管理 Window、不决定何时绘制、不改变 Signal 的
// 同步 Effect 语义。桌面端由窗口后端主循环驱动 drain；Web 端复用浏览器
// 事件循环，Loop 只做队列与适配。
//
// 执行规则（提案 §4.1）：
//  1. 宿主从自己的事件循环取出一个宏任务执行（RunOneMacro）；
//  2. 在宿主定义的微任务检查点调用 RunMicrotasks 清空（或在预算处停下）微任务队列；
//  3. 微任务执行期间新产生的微任务继续进入同一队列，同一次 drain 按 FIFO 执行；
//  4. drain 预算耗尽时 Remaining>0，由宿主依据报告尽快再次调度；
//  5. 帧合并仍归现有 Dispatcher / signal.AfterEffects，Loop 不触发 Draw。
//
// Post / PostMicro 契约：只负责入队，不得在调用栈内同步执行任务回调
// （UIPromise 的线性化设计依赖这条契约）。
package loop

import (
	"errors"
	"sync"
	"time"
)

// 稳定错误（提案 §11）。命名与 signal 的 ErrUIRuntime* 体系不冲突：
// Loop 层描述调度失败，Runtime 层可包装映射。
var (
	// ErrClosed：Loop 已 Stop，任务被拒绝且不会在任何线程执行。
	ErrClosed = errors.New("gx/loop: UI loop is closed")
	// ErrQueueRejected：预留给出将来可能有容量策略的拒绝路径（第一阶段无容量上限，不会返回）。
	ErrQueueRejected = errors.New("gx/loop: task rejected by loop")
)

// defaultMicroBudget 是单次微任务 drain 的默认数量预算（提案 §4.2 的公平性让步）。
const defaultMicroBudget = 1024

// Executor 是投递侧接口（提案 §3.3）。
type Executor interface {
	// Post 提交一个宏任务。入队成功返回 nil；Loop 关闭返回 ErrClosed。
	// fn 不在调用栈内执行。fn 为 nil 视为无效提交，返回 nil 且不排队。
	Post(fn func()) error
	// PostMicro 提交一个微任务。语义同 Post，但只在 RunMicrotasks 的检查点执行。
	PostMicro(fn func()) error
}

// Loop 在投递侧接口之上补充微任务 drain 与生命周期（提案 §3.3）。
type Loop interface {
	Executor
	// RunMicrotasks 由宿主在明确的微任务检查点调用，执行当前微任务队列。
	// 返回本次执行数量、剩余积压与是否因预算中止；Remaining>0 时宿主应尽快再次调度。
	RunMicrotasks() DrainReport
	// Stop 幂等关闭：此后 Post/PostMicro 返回 ErrClosed，队列中尚未执行的任务被丢弃
	// （计入 Dropped，不假装执行成功）。正在执行的回调允许自然返回。
	Stop() error
}

// TaskKind 区分宏任务与微任务（观测/Watchdog 用）。
type TaskKind uint8

const (
	KindMacro TaskKind = 1
	KindMicro TaskKind = 2
)

func (k TaskKind) String() string {
	if k == KindMicro {
		return "micro"
	}
	return "macro"
}

// TaskInfo 描述一次任务投递/执行的观测信息（提案 §10.2：类型 + 任务 ID；
// 提交来源栈由 P5 Watchdog 按需补充，不在此膨胀）。
type TaskInfo struct {
	Kind TaskKind
	ID   uint64
}

// DrainReport 是 RunMicrotasks 的结果（提案 §4.2：不得在微任务未清空时宣称检查点完整结束）。
type DrainReport struct {
	Ran       int  // 本次 drain 实际执行的任务数
	Remaining int  // drain 结束后队列中剩余微任务数
	HitBudget bool // 是否因预算（数量或时间）提前中止
}

// Complete 报告本次检查点是否已彻底清空微任务队列。
func (r DrainReport) Complete() bool { return !r.HitBudget && r.Remaining == 0 }

// Config 是 Loop 构造配置。零值可用（默认预算、无时间预算、无回调）。
type Config struct {
	// MaxMicrotasksPerDrain：单次微任务 drain 的最大任务数。
	// 0 = 默认 1024；负数 = 不设数量预算（谨慎，恶意回调可长期占用 UI 上下文）。
	MaxMicrotasksPerDrain int
	// MicroDrainTimeBudget：单次 drain 的时间预算，0 = 不设时间预算。
	// 与数量预算取先到者。
	MicroDrainTimeBudget time.Duration
	// Wake：队列从「无待处理任务」变为「有待处理任务」时，在 Post/PostMicro 的
	// 调用 goroutine 上被调用一次（锁外）。实现必须轻量、非阻塞、可并发调用——
	// 桌面端接 glfw.PostEmptyEvent 等平台唤醒，Web 端接 schedule 回调。
	// 唤醒丢失不会造成任务滞留：入队与 wake 判定在同一把锁内完成。
	Wake func()
	// OnTaskPanic：任务回调 panic 被 recover 后的上报钩子（提案 §11：不得静默吞掉）。
	// 默认策略为 recover + 计数 + 本钩子；钩子为 nil 时仍可通过 Stats 观测。
	OnTaskPanic func(TaskInfo, any)
}

type task struct {
	info   TaskInfo
	fn     func()
	posted time.Time
}

// Stats 是积压与吞吐观测（提案 §9；第一阶段观测优先，不做有界队列阻塞）。
type Stats struct {
	PostedMacro  int64
	PostedMicro  int64
	RanMacro     int64
	RanMicro     int64
	Dropped      int64 // Stop 时丢弃的未执行任务数
	Panics       int64 // 任务 panic 被 recover 的次数
	MacroPending int
	MicroPending int
}

// Queue 是 Loop 的默认实现：进程级共享 UI Loop 的队列与生命周期协议。
//
// 同步协议（提案 §8 / §14「Post 与 Stop 竞态」）：队列、关闭标志、统计计数
// 全部由同一把 mutex 保护；任务执行一律在锁外。任何一次 Post 要么在 Stop
// 前被接收（随后被执行、或被 Stop 丢弃并计数），要么返回 ErrClosed——
// 不存在「Stop 后又被当作正常接收」的中间态。
type Queue struct {
	mu     sync.Mutex
	cfg    Config
	macro  []task
	micro  []task
	closed bool
	seq    uint64

	// wakeBusy：自上次入队触发的 wake 之后，宿主尚未开始 drain。
	// 仅用于合并高频 wake（宿主可 poll 时允许错过合并，不允许错过最后一批）。
	// drain（RunOneMacro/RunMicrotasks 实际取任务）后重新 arm。
	wakeBusy bool
	// draining：RunMicrotasks 重入保护。微任务回调内再调 RunMicrotasks 直接返回，
	// 避免嵌套 drain 打乱 FIFO 与预算语义。
	draining bool

	st Stats
	// accepted/run/dropped 一致性计数由 st 汇总。
}

var _ Loop = (*Queue)(nil)

// New 按配置创建一个 Loop（第一阶段为进程级共享实例，由宿主创建并持有）。
func New(cfg Config) *Queue {
	return &Queue{cfg: cfg}
}

// Post 入队一个宏任务（契约：不在调用栈内执行）。
func (l *Queue) Post(fn func()) error {
	return l.push(KindMacro, fn)
}

// PostMicro 入队一个微任务（契约：不在调用栈内执行）。
func (l *Queue) PostMicro(fn func()) error {
	return l.push(KindMicro, fn)
}

func (l *Queue) push(kind TaskKind, fn func()) error {
	if fn == nil {
		return nil
	}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return ErrClosed
	}
	l.seq++
	t := task{info: TaskInfo{Kind: kind, ID: l.seq}, fn: fn, posted: time.Now()}
	if kind == KindMicro {
		l.micro = append(l.micro, t)
		l.st.PostedMicro++
	} else {
		l.macro = append(l.macro, t)
		l.st.PostedMacro++
	}
	firstPending := len(l.macro)+len(l.micro) == 1 && !l.wakeBusy
	needWake := firstPending && l.cfg.Wake != nil
	if needWake {
		l.wakeBusy = true
	}
	l.mu.Unlock()
	// 锁外唤醒宿主：Wake 可能是平台 API（glfw.PostEmptyEvent / postMessage），
	// 绝不能在持锁状态下跨越进后端。
	if needWake {
		l.cfg.Wake()
	}
	return nil
}

// RunOneMacro 从宏任务队列取出一个任务并在当前 goroutine 执行；队列空或已
// 关闭返回 false。微任务检查点由宿主在返回后自行决定何时调用 RunMicrotasks。
//
// 桌面宿主示例（glwindow.Run）：
//
//	for loop.RunOneMacro() { }   // 每轮事件先清宏任务
//	loop.RunMicrotasks()          // 微任务边界
func (l *Queue) RunOneMacro() bool {
	l.mu.Lock()
	if l.closed || len(l.macro) == 0 {
		l.mu.Unlock()
		return false
	}
	t := l.macro[0]
	l.macro = l.macro[1:]
	if len(l.macro)+len(l.micro) == 0 {
		l.wakeBusy = false // 队列再次空，下一批入队重新唤醒
	}
	l.mu.Unlock()
	l.exec(t)
	l.mu.Lock()
	l.st.RanMacro++
	l.mu.Unlock()
	return true
}

// RunOne 是「宏任务优先、否则微任务」的单任务 drain（宿主只想取一个任务时使用）。
// 返回 false 表示没有任何可执行任务（空或已关闭）。
func (l *Queue) RunOne() bool {
	if l.RunOneMacro() {
		return true
	}
	l.mu.Lock()
	if l.closed || len(l.micro) == 0 {
		l.mu.Unlock()
		return false
	}
	t := l.micro[0]
	l.micro = l.micro[1:]
	if len(l.macro)+len(l.micro) == 0 {
		l.wakeBusy = false
	}
	l.mu.Unlock()
	l.exec(t)
	l.mu.Lock()
	l.st.RanMicro++
	l.mu.Unlock()
	return true
}

// RunMicrotasks 在宿主检查点上 drain 微任务队列（提案 §4.1 规则 3-5、§4.2 预算）。
// drain 期间新入队的微任务在同一批次按 FIFO 继续执行，直到队列清空或预算耗尽。
// 重入保护：微任务回调内部再调用本方法时直接返回当前剩余，不嵌套 drain。
func (l *Queue) RunMicrotasks() DrainReport {
	l.mu.Lock()
	if l.closed || l.draining {
		rem := len(l.micro)
		l.mu.Unlock()
		return DrainReport{Remaining: rem}
	}
	l.draining = true
	budget := l.cfg.MaxMicrotasksPerDrain
	if budget == 0 {
		budget = defaultMicroBudget
	}
	deadline := time.Time{}
	if l.cfg.MicroDrainTimeBudget > 0 {
		deadline = time.Now().Add(l.cfg.MicroDrainTimeBudget)
	}
	rep := DrainReport{}
	for {
		if l.closed { // Stop 竞态：drain 中途关闭立即停手，剩余已计入 Dropped
			break
		}
		if budget >= 0 && rep.Ran >= budget {
			rep.HitBudget = true
			break
		}
		if !deadline.IsZero() && rep.Ran > 0 && !time.Now().Before(deadline) {
			rep.HitBudget = true
			break
		}
		if len(l.micro) == 0 {
			break
		}
		t := l.micro[0]
		l.micro = l.micro[1:]
		l.mu.Unlock()
		l.exec(t) // 锁外执行；期间其他 goroutine 可继续 PostMicro（同批 FIFO 收尾）
		l.mu.Lock()
		l.st.RanMicro++
		rep.Ran++
	}
	rep.Remaining = len(l.micro)
	if len(l.macro)+len(l.micro) == 0 {
		l.wakeBusy = false
	}
	l.draining = false
	l.mu.Unlock()
	return rep
}

// exec 执行单个任务并落实 panic 策略（recover + 计数 + 钩子；不静默、不逃出 Loop）。
func (l *Queue) exec(t task) {
	defer func() {
		if r := recover(); r != nil {
			l.mu.Lock()
			l.st.Panics++
			hook := l.cfg.OnTaskPanic
			l.mu.Unlock()
			if hook != nil {
				hook(t.info, r)
			}
		}
	}()
	t.fn()
}

// Stop 幂等关闭 Loop（提案 §8）。丢弃尚未执行的任务并计入 Dropped；返回 nil。
// 与 Post 的互斥由同一把锁保证：Stop 返回后任何 Post 都得到 ErrClosed。
func (l *Queue) Stop() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	l.st.Dropped += int64(len(l.macro) + len(l.micro))
	l.macro = nil
	l.micro = nil
	l.mu.Unlock()
	return nil
}

// Closed 报告 Loop 是否已关闭。
func (l *Queue) Closed() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closed
}

// HasPending 返回当前待处理的宏/微任务数量（宿主调度参考）。
func (l *Queue) HasPending() (macro, micro int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.macro), len(l.micro)
}

// Stats 返回观测快照（提案 §9 背压指标）。
func (l *Queue) Stats() Stats {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.st
	return s
}
