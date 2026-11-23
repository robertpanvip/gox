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
//   - Changes：自上次 Present 以来的有序增量（需先 EnableChanges；未开启时为空）。
//   - Version：单调递增的提交序号，后端可用它去重/排序/判断是否漏帧。
//
// 消费规则：
//   - 首帧或后端丢失基线时，用 Root 建全量镜像；
//   - 之后每帧按顺序 apply Changes 做增量更新，用 Version 校验连续性；
//   - 若某帧 Changes 为空但 Version 前进，说明这批变化都被相等性短路，画面无变化。
//
// Changes 由 App 在 Present 前从 Instance 取出并随帧交付；后端不再需要反向访问 App。
type Frame struct {
	Root    *gxscene.Node
	Changes []gxscene.Change
	Version uint64
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

