# UI 调度循环 P0：接口现状核对与决议

> 对应《GX 设计提案：UI 调度循环与 UIPromise》§13 阶段 P0 交付物：代码现状说明、接口决议。
> 核对基线：main@966c761。本文只陈述代码事实与决议，不改变任何既有语义。

## 1. 现有接口与生命周期事实

### 1.1 `runtime/app`（gxapp）

| 事实 | 位置 |
| --- | --- |
| `UIExecutor` 契约为 `Post(fn func()) error`，线程保证归后端、Host 不比 goroutine id | `runtime/app/dispatcher.go:43` |
| `FrameDispatcher`：`RequestFrame/SetSink/Close`；多次请求合并为一次执行的帧去抖契约 | `dispatcher.go:13-31` |
| `UIExecutorDispatcher` 以 `pending` 标志去抖，经 `exec.Post(runQueued)` 投递；投递失败清标志允许重试；`Close` 后 `runQueued` 空转 | `dispatcher.go:78-146` |
| `InlineDispatcher` 同步执行，明确不承诺跨 goroutine 合并（headless/测试语义） | `dispatcher.go:48-75` |
| `QueueDispatcher`/`CurrentGoid` 保留为兼容层，已标 Deprecated | `dispatcher.go:149-243` |
| `App.SetUIExecutor(exec)`：替换为 `UIExecutorDispatcher`，并把 `exec.Post` 适配为 `signal.TaskPoster` 注入本 App 的 `signal.Runtime`（含全局租约桥接），须在 Render 前调用 | `app.go:199-231` |
| `App.Dispatch(fn)` 一律经本 App `signal.Runtime.Dispatch`，错误语义三态：未初始化 / 已关闭 / 被拒绝，非 nil 即 fn 未执行 | `app.go:248-255`、`signal/safe.go:52-62` |
| `App.Close` 顺序：`closed.Store → runtime.Close → globalLease.Release → teardownTree → dispatcher.Close → win.Close`（closeOnce 幂等） | `app.go:599-621` |
| `App.Run()` 把执行权交给窗口后端 `win.Run()`，App 层不自建主循环 | `app.go:585-` |
| 绘制在 commit gate（goid 可重入锁）内执行，与 Scene Mutation 互斥；panic 时 defer 释放 | `app.go:272-` |

### 1.2 `runtime/signal`

| 事实 | 位置 |
| --- | --- |
| `TaskPoster func(fn func()) bool`；返回 false 表示 UI 已关闭、任务丢弃 | `signal/safe.go:64` |
| `Runtime` 仅为投递上下文：`SetPoster/Close/Dispatch/InUI/Closed/SetErrorHandler`；每 App 独立实例，隔离的是投递器与关闭状态，不是响应式核心 | `safe.go:81-186` |
| `SyncPoster` 在当前 goroutine 立即执行 fn——只允许 headless/测试显式选用 | `safe.go:68-76` |
| `AfterEffects(fn)`：帧边界 hook，注册表 RWMutex，`flushDepth` 为包级单实例；effect 批次同步跑完后在最外层触发一次 | `signal/flush.go:28-77` |
| observers / ownerStack / batch / flush 深度等依赖追踪核心仍是包级共享、单执行上下文——提案 §1.2 的边界描述与代码一致 | `flush.go` 头注释、`owner.go` |

### 1.3 两端宿主循环现状

- 桌面：`examples/playground/desktop/glwindow.go` 的 `Run()` 主线程事件循环为 `WaitEventsTimeout(20ms)` + `paintNow()`，GL 调用锁定主线程（`runtime.LockOSThread`）。
- Web：`examples/playground/web/main.go` 使用 gxweb 后端提供的 RAF `UIExecutor`（`app.SetUIExecutor(win)`），复用浏览器事件循环。

## 2. 接口决议

1. **不改动 `app.UIExecutor` / `FrameDispatcher` 的含义**（提案 §6.1）。Loop 的投递侧接口 `loop.Executor{Post, PostMicro}` 独立于 `UIExecutor`；需要把 App 挂到 Loop 上时，用适配器实现 `UIExecutor`（`Post` 转发 `loop.Post`），不为接口相容而弱化任何一方契约。
2. **Loop 第一阶段 API 按提案 §3.3 落地**：`Post/PostMicro/RunMicrotasks/Stop`。`RunMicrotasks` 返回 `DrainReport{Ran, Remaining, HitBudget}`——提案 §4.2 要求宿主能判断"本次检查点是否还有积压"，故 drain 结果作为公共返回值而非内部方法；`RunOneMacro/RunOne/HasPending/Stats/Closed` 为实现层公共方法，供宿主循环与观测使用，暂不提升为 `Loop` 接口成员。
3. **投递契约收紧**（UIPromise §7.5 的前提）：`loop.Queue` 的 `Post/PostMicro` 只入队、绝不在调用栈内执行回调；已有测试 `TestPostNotSyncExecuted` 固化。注意 `signal.SyncPoster` 不满足该契约，因此**不得把 SyncPoster 场景当作 Loop/ Promise 的等效宿主**（headless 测试 Promise 时需显式换成 Loop 或队列型投递器）。
4. **微任务检查点定义**：宿主每执行一个宏任务（或一轮宿主事件处理后）调用一次 `RunMicrotasks`；drain 期间新产生的微任务在同一批次按 FIFO 收尾，直到清空或预算（数量默认 1024、时间可选）耗尽。预算耗尽返回 `HitBudget=true, Remaining>0`，由宿主决定立即再 drain 或让出。
5. **唤醒协议**：`Config.Wake` 在队列从空转非空时在提交方 goroutine 上调用一次（锁外），宿主 drain 到空后重新 arm。桌面适配器应接 `glfw` 平台唤醒（若绑定可用）或依赖现有 20ms `WaitEventsTimeout` 轮询；Web 适配器接浏览器调度原语。唤醒只做提示，正确性只依赖队列锁协议——不存在"唤醒丢失导致任务永久滞留"。
6. **关闭协议**（提案 §8 表落地）：`Stop` 幂等、丢弃未执行任务并计入 `Dropped`（不假装执行成功）；关闭判定与入队在同一把锁内完成，`Stop` 返回后的 `Post` 恰好得到 `ErrClosed`；drain 循环每轮重查 closed，防 Stop 竞态。`signal.Runtime.Close` 与 Loop `Stop` 的先后顺序**留待 P2 在 App 生命周期中固定并测试**（现状 App.Close 不含 Loop，P0 只登记为待办，见 §4）。
7. **错误体系**：`gx/loop` 定义 `ErrClosed`、`ErrQueueRejected`（预留），与 `signal.ErrUIRuntime*` 三态错误互补不重叠；Loop 描述调度失败，Runtime 层保持现有包装。
8. **panic 策略**：默认 recover、计数 `Stats.Panics`、经 `Config.OnTaskPanic(TaskInfo, recovered)` 上报，不静默吞掉；单个任务 panic 不中断宿主循环、不逃出 Loop（提案 §11 第一阶段建议）。
9. **多 App 模型**：第一阶段进程级共享 Loop（提案决议 7）。核对确认依赖追踪核心为包级单实例（§1.2 表），共享 Loop + 串行执行 UI 任务与现状一致；不承诺、也不做任何并行响应式核心。
10. **观测指标**（提案 §9 第一阶段清单）：`Stats{PostedMacro, PostedMicro, RanMacro, RanMicro, Dropped, Panics, MacroPending, MicroPending}`。不做有界队列阻塞提交方；任务等待时长/慢任务归 P5 Watchdog。

## 3. P1 验收对照（main 实测）

| 提案 §12 P1 条目 | 测试 | 结果 |
| --- | --- | --- |
| 宏/微 FIFO 与检查点顺序 | `TestFifoOrder` `TestMacroMicroCheckpointOrder` | PASS |
| 微任务中继续 PostMicro（同批） | `TestPostMicroInsideDrainSameBatch` | PASS |
| 预算耗尽后剩余可续处理 | `TestMicroBudgetExhaustion` `TestTimeBudget` | PASS |
| Stop 幂等 / Stop 后 ErrClosed / 丢弃计数 | `TestStopSemantics` | PASS |
| Post 不在调用栈内执行 | `TestPostNotSyncExecuted` | PASS |
| 多 goroutine 并发 Post 无丢失重复 | `TestConcurrentPostNoLossNoDup` | PASS |
| Post 与 Stop 竞态确定性（执行+丢弃=接收） | `TestPostStopRaceDeterministic` | PASS |
| 唤醒协议（空→非空一次、关闭不唤醒） | `TestWakeOnlyWhenPendingEmptyToNonEmpty` | PASS |
| panic recover 且不中断 | `TestPanicRecoveredAndIsolated` | PASS |
| RunMicrotasks 重入安全 | `TestRunMicrotasksReentrantSafe` | PASS |
| `-race` 无数据竞争 | 全部测试 `go test -race` | PASS |

## 4. 遗留到 P2/P3 的核对项（本阶段不做）

- App.Close 与 Loop.Stop / Runtime.Close 的固定顺序及测试（§8 表要求，接入宿主后生效）。
- 桌面：后台 Post 唤醒 `WaitEventsTimeout` 的实测延迟分布；UI 线程断言（真实窗口）。
- Web：`queueMicrotask` 与 Go WASM 回调时序的浏览器实测；若语义无法保证，`PostMicro` 在 Web 标记为受限能力（提案 §5.2 允许）。
- Loop 与 `UIExecutorDispatcher` 帧去抖的关系保持现状：Loop 不触发 Draw，帧合并仍归 Dispatcher/AfterEffects。

## 5. P4 UIPromise / P5 Watchdog 验收对照（main 实测）

P4（提案 §12 P4）：

| 条目 | 测试 | 结果 |
| --- | --- | --- |
| settle 只成功转换一次，后续无效 | `TestSettleOnceFirstWins` | PASS |
| Then 在 settle 前/后注册；注册与 settle 并发线性化 | `TestThenBeforeAndAfterSettle` `TestThenSettleConcurrentLinearized` | PASS |
| continuation 经 PostMicro 微任务调度、不在调用栈同步执行 | `TestPromiseThenRunsInUIContext` `TestMicroCheckpointPlacement` | PASS |
| Then 返回 error / Catch 恢复与再失败 / Finally 保值保错与自身失败传播 | `TestThenErrorPropagates` `TestCatchRecoverAndRethrow` `TestFinallySemantics` | PASS |
| 回调 panic recover 为下游错误、不逃出 Loop | `TestCallbackPanicBecomesError` | PASS |
| Executor 关闭 → 下游以可观察调度错误结束，回调不执行 | `TestExecutorClosedPropagates` | PASS |
| worker 结果经 UI 上下文；worker 返回 error / panic 均 reject | `TestGoWorkerToUIContext` `TestGoRejectAndPanic` | PASS |
| 多 Then 注册顺序 | `TestMultipleThenRegistrationOrder` | PASS |
| `-race` 覆盖并发注册与 settle | 全包 `go test -race` | PASS |

P5（提案 §10.4 / §12 P5）：

| 条目 | 测试 | 结果 |
| --- | --- | --- |
| 可控阻塞回调触发超时事件：slow→severe→stuck 严格升级、每级一次、每任务仅采一次栈 | `TestWatchdogSlowSevereStuckProgression` | PASS |
| 任务结束后不再报告该任务 | 同上（结束后再观察 40ms 断言无事件） | PASS |
| 正常任务不误报 | `TestWatchdogFastTaskNoFalsePositive` | PASS |
| Loop 关闭后监测 goroutine 退出；Stop 幂等 | `TestWatchdogExitsOnLoopStop` | PASS |
| 栈采集失败（panic）不导致监测崩溃、不影响 Loop | `TestWatchdogStackCaptureFailureIsolated` | PASS |
| Report 回调 panic 隔离，监测循环存活 | `TestWatchdogReportPanicIsolated` | PASS |
| 诊断逻辑（含慢栈采集）不阻塞 UI 任务执行 | `TestWatchdogDiagnosticsDoNotBlockUI` | PASS |
| §9 观测扩展（等待时间、拒绝计数、drain 耗时） | `TestStatsObservability` | PASS |

Watchdog 定位重申（§10.1）：只诊断、不恢复——不能中断阻塞任务、不能解除死锁、
不保证界面在阻塞期间继续绘制；以上任何测试都不以「恢复死循环」为验收条件。
