//go:build windows

package main

import (
	"sync"
	"syscall"
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"

	skia "github.com/robertpanvip/gox-backend-skia"
)

// Windows 的点击穿透走窗口过程子类化：拦截 WM_NCHITTEST，若命中点落在
// "无 scene 内容"的透明区，返回 HTTRANSPARENT 让系统把点击投递给下层窗口；
// 内容区与默认情况交回 GLFW 原窗口过程。语义与 Linux/XShape 一致（区域级
// 精确穿透），且不需要 cgo——用 syscall 调 user32.dll 即可。

const (
	wmNCHITTEST   = 0x0084
	htClient      = 1
	htTransparent = ^uintptr(0) // -1
	gwlpWndProc   = ^uintptr(3) // -4
)

var (
	user32          = syscall.NewLazyDLL("user32.dll")
	procSetWndProc  = setWndProcProc()
	procCallWndProc = user32.NewProc("CallWindowProcW")
	procScreen2Clt  = user32.NewProc("ScreenToClient")
)

// SetWindowLongPtrW 只在 64 位 user32.dll 中导出；32 位上是头文件宏，实际导出
// 名为 SetWindowLongW（两者 GWLP_WNDPROC 数值一致，返回值的指针宽度由 uintptr 承载）。
func setWndProcProc() *syscall.LazyProc {
	if unsafe.Sizeof(uintptr(0)) == 8 {
		return user32.NewProc("SetWindowLongPtrW")
	}
	return user32.NewProc("SetWindowLongW")
}

// ptState 是子类化窗口过程的共享状态。syscall.NewCallback 产生的回调可能在
// 任意线程被 Windows 调用，因此读写都加锁；调用原过程前必须解锁，避免
// 原过程再触发本回调导致死锁。
var ptState struct {
	mu           sync.Mutex
	hwnd         uintptr
	origProc     uintptr
	callback     uintptr
	installed    bool
	enabled      bool
	rects        []inputRect
	physW, physH int32
}

type ptPoint struct{ x, y int32 }

// ptWndProc 是替换后的窗口过程。syscall.NewCallback 要求签名参数与返回值均为 uintptr。
func ptWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	ptState.mu.Lock()
	st := &ptState
	origProc, enabled := st.origProc, st.enabled
	rects := st.rects
	ptState.mu.Unlock()

	if msg == wmNCHITTEST && enabled && origProc != 0 {
		// lParam 低 16 位 x、高 16 位 y，屏幕坐标。
		p := ptPoint{x: int32(int16(lParam)), y: int32(int16(lParam >> 16))}
		// 转成窗口客户区坐标（与 DrawLog 换算出的 inputRect 同一坐标系）。
		procScreen2Clt.Call(hwnd, uintptr(unsafe.Pointer(&p)))
		for _, r := range rects {
			if r.contains(p.x, p.y) {
				return htClient
			}
		}
		return htTransparent
	}
	r, _, _ := procCallWndProc.Call(origProc, hwnd, msg, wParam, lParam)
	return r
}

// applyInputShape 按本帧实际绘制的不透明内容更新窗口的输入判定矩形。
// enabled=false 时保留子类化但关闭拦截，交回 GLFW 原窗口过程（行为与既有一致）。
func applyInputShape(win *glfw.Window, log skia.DrawLog, dpr float32, physW, physH int, enabled bool) {
	if win == nil {
		return
	}
	hwnd := uintptr(unsafe.Pointer(win.GetWin32Window()))
	rects := drawLogToInputRects(log, dpr)

	ptState.mu.Lock()
	defer ptState.mu.Unlock()
	st := &ptState
	if !st.installed {
		cb := syscall.NewCallback(ptWndProc)
		st.callback = cb // 保活：Go 闭包指针需在运行期保持可达
		st.origProc, _, _ = procSetWndProc.Call(hwnd, gwlpWndProc, cb)
		st.installed = true
	}
	st.hwnd = hwnd
	st.enabled = enabled
	st.rects = rects
	st.physW, st.physH = int32(physW), int32(physH)
}

// 平台钩子：Windows 的 WndProc 子类化是事件驱动的，无需每帧轮询。
func pollPassthrough()            {}
func ptDarwinInit(_ *glfw.Window) {}
func darwinRelease()              {}
