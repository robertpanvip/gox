//go:build js && wasm

package gxweb

import "syscall/js"

// newPlatformDriver 在 wasm 下返回 syscall/js 驱动。selector 形如 "#root" / ".app" /
// "body"；查不到时回退 document.body，保证 playground 不会因为挂载点写错而白屏。
func newPlatformDriver(selector string) Driver {
	doc := js.Global().Get("document")
	var root js.Value
	if selector != "" {
		root = doc.Call("querySelector", selector)
	}
	if !root.Truthy() {
		root = doc.Get("body")
	}
	return newJSDriver(root)
}
