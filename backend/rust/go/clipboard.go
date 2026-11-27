package rustbackend

// 剪贴板桥接（阶段 4 输入面）：桌面翻译层 Ctrl+C/V/X 消费。
// Rust 侧经 arboard 读写系统剪贴板；本文件只做 Go 侧包装，
// 语义与旧 GLFW 路径的 glfw.GetClipboardString / SetClipboardString 对齐：
// 失败（无显示服务 / Wayland 无 data-control）返回空串 / false，不 panic。

// ClipboardGet 读取系统剪贴板文本（失败返回空串）。
func ClipboardGet() string { return cClipboardGet() }

// ClipboardSet 写系统剪贴板文本。返回是否成功。
func ClipboardSet(text string) bool { return cClipboardSet(text) }
