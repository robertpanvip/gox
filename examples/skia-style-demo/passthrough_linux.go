//go:build linux

package main

/*
#cgo LDFLAGS: -lX11 -lXext
#include <X11/Xlib.h>
#include <X11/extensions/shape.h>
#include <stdlib.h>
*/
import "C"

import (
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"

	skia "github.com/robertpanvip/gox-backend-skia"
)

// setInputShape 把窗口的输入形状设为 rects（物理像素，内容区左上原点）。
// rects 为空表示形状为空——整个窗口鼠标穿透。这是"透明区点击落到后面"的关键：
// X11 只把输入事件投递到 input shape 覆盖的窗口区域，其余区域直接穿透给下层窗口。
func setInputShape(dpy unsafe.Pointer, win uintptr, rects []inputRect) {
	if dpy == nil || win == 0 {
		return
	}
	d := (*C.Display)(dpy)
	w := C.Window(win)
	n := len(rects)
	var p *C.XRectangle
	if n > 0 {
		p = (*C.XRectangle)(C.malloc(C.size_t(n) * C.size_t(unsafe.Sizeof(C.XRectangle{}))))
		defer C.free(unsafe.Pointer(p))
		arr := unsafe.Slice(p, n)
		for i, r := range rects {
			arr[i] = C.XRectangle{
				x: C.short(r.x), y: C.short(r.y),
				width: C.ushort(r.w), height: C.ushort(r.h),
			}
		}
	}
	C.XShapeCombineRectangles(d, w, C.ShapeInput, 0, 0, p, C.int(n), C.ShapeSet, C.Unsorted)
}

// applyInputShape 按本帧实际绘制的不透明内容更新窗口输入形状。
// 用 DrawLog 而非 hitbox：hitbox 对所有节点（含纯透明容器）都记录，若据此设形状
// 会把整窗都算作输入区、失去穿透；DrawLog 只含真正画出的矩形/文字/图片。
// enabled=false 时恢复整窗可点击（一个覆盖全窗口的矩形），与既有行为一致。
func applyInputShape(win *glfw.Window, log skia.DrawLog, dpr float32, physW, physH int, enabled bool) {
	if win == nil {
		return
	}
	dpy := unsafe.Pointer(glfw.GetX11Display())
	xw := uintptr(win.GetX11Window())
	if !enabled {
		// 恢复默认：整窗可点击。
		setInputShape(dpy, xw, []inputRect{{0, 0, int32(physW), int32(physH)}})
		return
	}
	setInputShape(dpy, xw, drawLogToInputRects(log, dpr))
}

// 平台钩子：Linux 的 XShape 是 OS 级输入形状，设置一次即生效，无需每帧轮询。
func pollPassthrough()            {}
func ptDarwinInit(_ *glfw.Window) {}
func darwinRelease()              {}
