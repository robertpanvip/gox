# P2 桌面端接入 UI Loop：实现说明与真机验收清单

> 对应提案 §5.1 / §13 P2。沙箱验证记录（2026-11-25）：补装 libgl1-mesa-dev /
> xorg-dev 后 `go build ./desktop/` 编译通过（go-skia 自带 linux-amd64 预编译
> 静态库）；在 Xvfb + Mesa 软渲染下桌面版**稳定运行 12s、stdout/stderr 零错误**
> ——单进程事件循环 + Loop drain + Watchdog 启动路径已在真实 GLFW 窗口上验证。
> 下方清单中「唤醒延迟、线程断言、真实键鼠」仍需用户机器复核（沙箱无 GPU/
> 真实输入设备；提案明确不得以设计代替桌面线程与唤醒的实际测试）。

## 接线方式（不新建第二个事件循环）

- `openGL()` 创建进程级共享 `loop.Queue`；`Wake` 使用 `glfw.PostEmptyEvent`
  （`glfw.Init` 完成后才生效，由 `glfwWokeReady` 门控）。
- `glWindow` 实现 `gxapp.UIExecutor`：`Post` 只入队，执行一律发生在
  `Run()` 主循环的 `drainLoop()` ——即 `runtime.LockOSThread` 绑定的
  GLFW 线程，线程亲和性由宿主循环保证，Host 不比 goroutine id。
- `main.go` 在 `Render` 前 `app.SetUIExecutor(win)`：帧合并走
  `UIExecutorDispatcher`（经 `win.Post` 入 Loop），后台 `app.Dispatch` 走
  `signal.Runtime → win.Post`，同一 Loop 串行消费；`SetUIExecutor` 语义未变。
- `Run()`：每轮 `WaitEventsTimeout(20ms)` 后先 `drainLoop()`（一个宏任务 +
  一次微任务检查点，预算未清空继续消耗）再 `paintNow()`；Loop 排空后 Wake
  重新 arm，下一次后台 Post 即时打断 20ms 等待。
- `Close()`：先停 Watchdog，再 `uiLoop.Stop()`（拒绝新任务，未执行任务丢弃并
  计数，提案 §8），最后释放 GL / Window 资源；`App.Close` 既有的
  `runtime.Close → teardown → dispatcher.Close → win.Close` 顺序不变。
- Watchdog 采用 §10.2 建议默认阈值（50ms / 200ms / 2s），诊断写 stderr。

## 真机验收清单（§12 P2）

1. 后台 goroutine 调 `app.Dispatch` / `win.Post`：任务在下一个 drain 轮次即
   执行；`PostEmptyEvent` 唤醒生效后延迟应远小于 20ms（对比禁用 Wake）。
2. 所有 UI 回调（绘制、Dispatch 闭包、微任务）在 GLFW 线程执行：GL 调用
   无黑屏/上下文错误；可在回调内断言线程标识。
3. 进程内仅一个窗口主循环；`drainLoop` 不阻塞等待任何任务。
4. 窗口关闭后 `win.Post` 与 `app.Dispatch` 均返回可识别错误
   （`loop.ErrClosed` / `signal.ErrUIRuntime*`）。
5. 连续多次 `RequestFrame` 合并为一次绘制（`Frames()`/`FrameVersion()` 观测），
   无每任务一次无意义 Draw。
6. 长时运行：快任务不触发 Watchdog 误报；出现 ≥50ms 任务时 stderr 有分级日志，
   ≥200ms 附带一次栈快照。
