package gxapp

import gxscene "github.com/robertpanvip/gox/runtime/scene"

// Frame 是一次 Host → Backend 的完整提交（Host 架构评审 P0：全量快照与增量补丁
// 的统一提交契约）。
//
// 过去 Window.Draw(root) 只把场景树交给后端，增量变更流 Changes 却留在 App 上，
// 消费增量的后端必须反向持有 App 引用（如 Rust 后端的 BindApp）——接入不自然，
// 且全量与补丁的关系没有明确定义。Frame 把三者打包成一次原子提交：
//
//   - Root：当前完整场景树，后端据此建/重建镜像（全量基线）。
//   - Changes：自上次成功 Present 以来的有序增量。后端实现 FramePresenter 时
//     Host 自动开启变更记录，无需再调用 EnableChanges；老 Draw 路径不记录。
//   - Version：单调递增的提交序号，后端可用它去重/排序/判断是否漏帧。
//   - FullSync：为 true 时本帧必须忽略 Changes、直接用 Root 全量重建镜像。
//     典型触发：上一次 Present 失败，期间产生的变更已在 Host 缓冲按序保留，
//     但后端镜像可能停在更早版本，只有全量重建才能恢复一致。
//
// 消费规则：
//   - FullSync=true 或首帧或后端丢失基线时，用 Root 建全量镜像；
//   - 否则按顺序 apply Changes 做增量更新，用 Version 校验连续性；
//   - 若某帧 Changes 为空但 Version 前进，说明这批变化都被相等性短路，画面无变化。
//
// Root 消费契约（同步消费）：Root 是可变指针，仅在 Present 调用期间稳定。
// 后端必须在 Present 返回前完成对 Root 与 Changes 的读取；若需异步延迟消费，
// 必须在 Present 内自行复制所需数据。Host 不复制整棵树，也不保证 Present
// 返回后 Root 不被后续 Scene Mutation 修改（这些 Mutation 由 commit gate 与
// Present 串行化，不会并发，但会在其返回后继续）。
//
// Changes 由 App 在 Present 前从 Instance 取出（peek，不清空）并随帧交付；
// 只有 Present 成功才确认消费并清空缓冲，失败则缓冲保留，下次以 FullSync 重建。
// 后端不再需要反向访问 App。
type Frame struct {
	Root     *gxscene.Node
	Changes  []gxscene.Change
	Version  uint64
	FullSync bool
}

// FramePresenter 是可选的绘制契约：实现它的 Window 后端每帧收到完整 Frame。
// App.drawNow 检测到 Window 实现此接口时优先调用 Present，否则回退到 Window.Draw(root)
// （老后端零改动，兼容路径行为不变）。
//
// Present 与 Draw 二选一即可；两者都实现时 Present 优先。Present 在 commit gate 之后
// 调用——它拿到的是一个已经提交完成、不会再被 mutation 改动的稳定快照。
type FramePresenter interface {
	Present(frame *Frame) error
}
