// UIPromise：把异步结果及其延续回调安排到 UI 执行上下文的可选编排层
// （提案 §7）。它不创建线程模型、不取代 signal.Runtime.Dispatch：后台工作仍
// 由 Go goroutine 执行，本类型只负责把结果经 Executor 安全交回 UI 上下文。
//
// 第一阶段承诺（§7.3）：
//   - Pending 只向 Fulfilled/Rejected 转换一次，重复 settle 无效；
//   - 用户回调只在 Executor 成功投递后、于 UI 上下文执行，绝不在 Promise 内部锁里执行；
//   - 回调 panic 被 recover 并转为下游错误，不逃出 UI Loop；
//   - Then 返回 error 使下游 Rejected；Catch 可恢复；Finally 保留原结果、自身失败则下游失败；
//   - Executor 已关闭/投递失败：下游以可观察的调度错误结束，不静默丢弃；
//   - 无同步 Wait()。
//
// 不承诺（§7.3）：ECMAScript Promise 兼容、thenable assimilation、组合子、
// 自动取消 worker、跨 goroutine 并发 settle 的现实时间先后（仅承诺确定性规则：
// 第一个完成状态转换的调用生效；注册顺序以获取 Promise 锁完成注册的顺序为准）。
//
// 线性化协议（§7.5）：attach 在 p.mu 内决定「加入 pending 列表」还是「立即调度」；
// settle 在 p.mu 内完成状态转换并快照 continuation 列表；投递（PostMicro）与
// 用户回调执行都发生在锁外。Executor 契约「Post/PostMicro 只入队、不在调用栈内
// 执行回调」是本协议成立的前提（loop.Queue 已满足并有测试固化）。
package loop

import (
	"fmt"
	"sync"
)

type promiseState uint8

const (
	statePending promiseState = iota
	stateFulfilled
	stateRejected
)

// UIPromise 是一个已关联 UI Executor 的异步结果句柄。T 为成功值类型。
// 零值不可用，必须经 Go / Resolved / Rejected 创建。
type UIPromise[T any] struct {
	rt Executor

	mu    sync.Mutex
	state promiseState
	value T
	err   error
	conts []continuation[T]
}

// continuation 是等待 settle 后调度到 UI 上下文的一组回调：
// run 在投递成功后执行；投递失败时调用 fail 向下游传播调度错误。
type continuation[T any] struct {
	run  func()
	fail func(error)
}

func newPromise[T any](rt Executor) *UIPromise[T] {
	return &UIPromise[T]{rt: rt}
}

// attach 注册一组 continuation：pending 时入列（保持注册顺序），已 settle 时
// 立即调度。判断与入列/取快照都在 p.mu 内完成——注册与 settle 不存在顺序竞争。
func (p *UIPromise[T]) attach(c continuation[T]) {
	p.mu.Lock()
	if p.state == statePending {
		p.conts = append(p.conts, c)
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	p.dispatch(c)
}

// dispatch 在锁外向 Executor 投递一个 continuation；投递失败 → fail 传播。
func (p *UIPromise[T]) dispatch(c continuation[T]) {
	if err := p.rt.PostMicro(c.run); err != nil {
		c.fail(fmt.Errorf("gx/loop: promise continuation 投递失败: %w", err))
	}
}

// settle 完成状态转换（仅 Pending 可转换，后续 settle 无效），并在锁外按
// 注册顺序投递全部 continuation。
func (p *UIPromise[T]) settle(state promiseState, value T, err error) {
	p.mu.Lock()
	if p.state != statePending {
		p.mu.Unlock()
		return
	}
	p.state = state
	p.value = value
	p.err = err
	list := p.conts
	p.conts = nil
	p.mu.Unlock()
	for _, c := range list {
		p.dispatch(c)
	}
}

func (p *UIPromise[T]) fulfill(v T)      { p.settle(stateFulfilled, v, nil) }
func (p *UIPromise[T]) reject(err error) { p.settle(stateRejected, p.zero(), err) }

func (p *UIPromise[T]) zero() (z T) { return z }

// snapshot 读取已 settle 的终态（调用方保证 state != pending）。
func (p *UIPromise[T]) terminal() (promiseState, T, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state, p.value, p.err
}

// ---------- 构造入口（§7.2） ----------

// Go 在 worker goroutine 执行 fn；成功值 resolve、返回的 error reject。
// fn  panic 时 recover 并以错误 reject。回调结果一律经 rt 投递回 UI 上下文。
// 注意：fn 在后台执行，禁止在其中直接读写 Signal（提案 §7.6）。
func Go[T any](rt Executor, fn func() (T, error)) *UIPromise[T] {
	p := newPromise[T](rt)
	if rt == nil || fn == nil {
		p.reject(ErrClosed)
		return p
	}
	go func() {
		v, err := safeCall0(fn)
		if err != nil {
			p.reject(err)
		} else {
			p.fulfill(v)
		}
	}()
	return p
}

// Resolved 创建已成功完成的 Promise；注册的 continuation 仍经 PostMicro 调度，
// 不存在同步执行路径（§7.3.9）。
func Resolved[T any](rt Executor, value T) *UIPromise[T] {
	p := newPromise[T](rt)
	p.fulfill(value)
	return p
}

// Rejected 创建已失败完成的 Promise。
func Rejected[T any](rt Executor, err error) *UIPromise[T] {
	p := newPromise[T](rt)
	p.reject(err)
	return p
}

// ---------- 链式组合（包级泛型函数：Go 不允许方法带新类型参数，见提案 §7.2） ----------

// Then 成功时执行 onFulfilled 并把结果传给下游；失败时透传错误。
// onFulfilled 在 UI 上下文执行；返回 error 或 panic 都使下游 Rejected。
func Then[T, U any](p *UIPromise[T], onFulfilled func(T) (U, error)) *UIPromise[U] {
	q := newPromise[U](p.rt)
	p.attach(continuation[T]{
		run: func() {
			st, v, e := p.terminal()
			if st == stateRejected {
				q.reject(e)
				return
			}
			out, err := safeCall1(onFulfilled, v)
			if err != nil {
				q.reject(err)
				return
			}
			q.fulfill(out)
		},
		fail: func(e error) { q.reject(e) },
	})
	return q
}

// Catch 失败时执行 onRejected（可恢复为成功值，也可返回新错误继续失败）；
// 成功时透传原值。回调在 UI 上下文执行，panic 转为下游错误。
func Catch[T any](p *UIPromise[T], onRejected func(error) (T, error)) *UIPromise[T] {
	q := newPromise[T](p.rt)
	p.attach(continuation[T]{
		run: func() {
			st, v, e := p.terminal()
			if st == stateFulfilled {
				q.fulfill(v)
				return
			}
			out, err := safeCallErr(onRejected, e)
			if err != nil {
				q.reject(err)
				return
			}
			q.fulfill(out)
		},
		fail: func(fe error) { q.reject(fe) },
	})
	return q
}

// Finally 无论成败都执行 finally；成功时保留原成功值、失败时保留原错误；
// finally 自身返回 error 或 panic 时以下述 error 作为下游失败原因（§7.3.7）。
func Finally[T any](p *UIPromise[T], finally func() error) *UIPromise[T] {
	q := newPromise[T](p.rt)
	p.attach(continuation[T]{
		run: func() {
			st, v, e := p.terminal()
			fe := safeFinally(finally)
			if fe != nil {
				q.reject(fe)
				return
			}
			if st == stateRejected {
				q.reject(e)
				return
			}
			q.fulfill(v)
		},
		fail: func(fe error) { q.reject(fe) },
	})
	return q
}

// ---------- panic 边界（§7.3.4 / §11）：UI 回调 panic 一律 recover 为错误 ----------

var ErrCallbackPanic = fmt.Errorf("gx/loop: UI 回调 panic")

func wrapPanic(r any) error {
	return fmt.Errorf("%w: %v", ErrCallbackPanic, r)
}

func safeCall0[T any](fn func() (T, error)) (v T, err error) {
	defer func() {
		if r := recover(); r != nil {
			var z T
			v, err = z, wrapPanic(r)
		}
	}()
	return fn()
}

func safeCall1[T, U any](fn func(T) (U, error), arg T) (v U, err error) {
	defer func() {
		if r := recover(); r != nil {
			var z U
			v, err = z, wrapPanic(r)
		}
	}()
	return fn(arg)
}

func safeCallErr[T any](fn func(error) (T, error), arg error) (v T, err error) {
	defer func() {
		if r := recover(); r != nil {
			var z T
			v, err = z, wrapPanic(r)
		}
	}()
	return fn(arg)
}

func safeFinally(fn func() error) (err error) {
	if fn == nil {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			err = wrapPanic(r)
		}
	}()
	return fn()
}
