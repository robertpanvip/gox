//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework CoreGraphics

#import <AppKit/AppKit.h>
#import <CoreGraphics/CoreGraphics.h>
#import <stdlib.h>
#import <string.h>

// macOS 的 NSWindow 没有"按区域设置输入形状"的公开 API（不像 X11 XShape）。
// 等价实现：鼠标在透明区时整窗 ignoresMouseEvents=YES（点击穿透到后面），
// 在有内容的节点上恢复 NO。鼠标位置通过全局 NSEvent 取得，由宿主事件循环
// 每帧调用 ptDarwinPoll，因此即使本窗口不接收鼠标移动也能正确切换。

static NSWindow *pt_window = nil;
static BOOL pt_enabled = NO;
static BOOL pt_ignoring = NO;

// 每个矩形四个 float：x, y, w, h（窗口内容区坐标，左上原点，逻辑点）。
static float *pt_rects = NULL;
static int pt_rect_count = 0;

static void ptDarwinInitC(void *nsWindow) {
    pt_window = (NSWindow *)nsWindow;
}

static void ptDarwinUpdateC(float *rects, int count, int enabled) {
    if (pt_rects != NULL) { free(pt_rects); pt_rects = NULL; }
    pt_rect_count = count;
    if (count > 0) {
        pt_rects = (float *)malloc((size_t)count * 4 * sizeof(float));
        memcpy(pt_rects, rects, (size_t)count * 4 * sizeof(float));
    }
    pt_enabled = enabled ? YES : NO;
    if (!pt_enabled && pt_ignoring) {
        [pt_window setIgnoresMouseEvents:NO];
        pt_ignoring = NO;
    }
}

static void ptDarwinPollC(void) {
    if (pt_window == nil || !pt_enabled) return;
    // 全局鼠标位置（屏幕坐标，原点左下）。窗口忽略鼠标后仍可取到全局位置。
    NSPoint screen = [NSEvent mouseLocation];
    // 用 AppKit 把屏幕点转到窗口内容区坐标，避免手写原点翻转/多屏换算。
    NSPoint local = [pt_window convertPointFromScreen:screen];
    NSRect frame = [pt_window frame];
    // NSWindow frame 原点在左下；convertPointFromScreen 得到的是"窗口坐标"（左下），
    // 转成内容区左上原点的逻辑点：y = 窗口高 - local.y。
    CGFloat lx = local.x;
    CGFloat ly = frame.size.height - local.y;
    BOOL inside = NO;
    for (int i = 0; i < pt_rect_count; i++) {
        float x = pt_rects[i*4+0], y = pt_rects[i*4+1];
        float w = pt_rects[i*4+2], h = pt_rects[i*4+3];
        if (lx >= x && lx < x + w && ly >= y && ly < y + h) { inside = YES; break; }
    }
    BOOL wantIgnore = !inside;
    if (wantIgnore != pt_ignoring) {
        [pt_window setIgnoresMouseEvents:wantIgnore];
        pt_ignoring = wantIgnore;
    }
}

static void ptDarwinReleaseC(void) {
    if (pt_rects != NULL) { free(pt_rects); pt_rects = NULL; }
    pt_rect_count = 0;
    pt_window = nil;
}
*/
import "C"

import (
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"

	skia "github.com/robertpanvip/gox-backend-skia"
)

// ptDarwinInit 绑定 NSWindow（在 Open 创建窗口后调用一次）。
func ptDarwinInit(win *glfw.Window) {
	if win == nil {
		return
	}
	C.ptDarwinInitC(win.GetCocoaWindow())
}

// applyInputShape 更新内容矩形。macOS 下 rects 用逻辑点（dpr 由 convertPoint 统一），
// 这里把物理像素矩形除回 dpr——与 DrawLog 的 logical 坐标口径一致。
func applyInputShape(win *glfw.Window, log skia.DrawLog, dpr float32, physW, physH int, enabled bool) {
	_ = physW
	_ = physH
	rects := drawLogToInputRects(log, 1) // 逻辑点：不乘 dpr
	if dpr > 0 && dpr != 1 {
		for i := range rects {
			rects[i].x = int32(float32(rects[i].x) / dpr)
			rects[i].y = int32(float32(rects[i].y) / dpr)
			rects[i].w = int32(float32(rects[i].w) / dpr)
			rects[i].h = int32(float32(rects[i].h) / dpr)
		}
	}
	var p *C.float
	if len(rects) > 0 {
		flat := make([]float32, 0, len(rects)*4)
		for _, r := range rects {
			flat = append(flat, float32(r.x), float32(r.y), float32(r.w), float32(r.h))
		}
		p = (*C.float)(unsafe.Pointer(&flat[0]))
	}
	ce := C.int(0)
	if enabled {
		ce = 1
	}
	C.ptDarwinUpdateC(p, C.int(len(rects)), ce)
}

// pollPassthrough 由宿主事件循环每帧调用，按当前鼠标位置切换 ignoresMouseEvents。
func pollPassthrough() { C.ptDarwinPollC() }

// darwinRelease 在窗口关闭时释放缓存的矩形。
func darwinRelease() { C.ptDarwinReleaseC() }
