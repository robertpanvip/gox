package gxapp

// HostedWindow 是窗口后端的可选升级接口（P4 目标态，ui-loop-p4-multiwindows.md §4.2）：
// 实现后窗口由 Host 的**单一主循环**轮询驱动，Window.Run() 退化为 no-op——
// 后端不再各自阻塞跑事件循环，而是被 Host 逐轮 Poll。
//
// 未实现本接口的老后端（自驱 Run 的 GLFW 类窗口）不受影响：Host.Run 检测到
// 非 HostedWindow 窗口时回退为并行 goroutine 自驱路径（首版行为，向后兼容）。
//
// 绘制不在本接口里：HostedWindow 路径下全部 UI 任务（Dispatch / RAF / 帧重绘）
// 汇入 Host 的唯一 UI Loop（openEntry 注入 loopPoster），Host 主循环每轮
// DrainTurn 时统一执行——帧重绘任务（dispatcher sink → drawNowErr）自然走
// commit gate 与 Frame 契约，FramePresenter 后端在 Present 里收增量、无变化帧
// 由后端短路。因此这里只保留「取事件」与「请求关闭」两个后端必须提供的动作。
type HostedWindow interface {
	Window

	// Poll 非阻塞处理本窗口待处理事件（输入 / resize / close 请求等）。
	// 返回错误或 panic 都会归因上报 FaultHandler 并**熔断本窗口**，
	// Host 主循环继续驱动其余窗口（故障隔离域 = 窗口）。
	Poll() error

	// ShouldClose 报告本窗口是否请求关闭（用户点了关闭按钮 / 后端判定应退出）。
	// 返回 true 后 Host 会 CloseWindow 关闭它；全部窗口关闭则 Host.Run 返回。
	ShouldClose() bool
}
