//go:build !js || !wasm

// native 下无 main 入口：本模块只在 GOOS=js GOARCH=wasm 时构造可执行程序。
// 这个 stub 让 `go build ./...` / `go vet ./...` 在原生平台不报「无 Go 文件」。
package main

func main() {}
