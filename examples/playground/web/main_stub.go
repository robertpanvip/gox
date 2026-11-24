//go:build !js || !wasm

// native 下的空 main：让 go build ./... 在原生平台不因 build tag 排除全部文件而报错。
// 真正的网页入口是 main.go（js && wasm）。
package main

func main() {}
