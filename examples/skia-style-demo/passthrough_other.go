//go:build !linux && !windows && !darwin

package main

import (
	"github.com/go-gl/glfw/v3.3/glfw"

	skia "github.com/robertpanvip/gox-backend-skia"
)

// applyInputShape 在非 Linux 平台暂不实现鼠标穿透：窗口整体拦截鼠标。
// 语义与 WindowConfig.MousePassthrough 文档一致（不支持的后端静默忽略）。
// 后续可在 darwin（NSWindow contentArea 命中）与 windows（SetWindowRgn/命中测试）
// 分别实现，接口签名保持不变。
func applyInputShape(_ *glfw.Window, _ skia.DrawLog, _ float32, _, _ int, _ bool) {}

func pollPassthrough()            {}
func ptDarwinInit(_ *glfw.Window) {}
func darwinRelease()              {}
