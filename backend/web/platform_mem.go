//go:build !js || !wasm

package gxweb

// newPlatformDriver 在 native 平台返回内存驱动。selector 被忽略——native 下没有
// 真实 document，节点树挂在内存根上，供单测 / 快照 / headless 使用。
func newPlatformDriver(selector string) Driver {
	return newMemDriver()
}
