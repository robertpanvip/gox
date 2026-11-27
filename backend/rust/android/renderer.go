//go:build android

package androidbackend

// V2 渲染内核的 cgo 绑定：gx_android_* C ABI（定义见 backend/rust/rust/src/
// android_abi.rs）+ ANativeWindow 的 JNI 提取（native_window_jni.c）。
//
// 生命周期契约：surface 随 Activity 可见性销毁重建。OnSurfaceReady 注入/
// 重建渲染器，OnSurfaceLost 销毁（GPU 资源随旧 Renderer drop 释放）。

/*
#cgo LDFLAGS: -lgxbackend_rust -ldl -lm -llog -landroid -lc++_static -lc++abi
#cgo android,arm64 LDFLAGS: -L${SRCDIR}/../rust/target/aarch64-linux-android/release
#cgo android,amd64 LDFLAGS: -L${SRCDIR}/../rust/target/x86_64-linux-android/release
#include <jni.h>
#include <stdlib.h>
#include <stdint.h>

extern void*       gx_android_new(void* native_window, int width, int height);
extern int         gx_android_apply(void* host, const unsigned char* buf, int len);
extern int         gx_android_render(void* host);
extern int         gx_android_resize(void* host, int width, int height);
extern int         gx_android_set_window(void* host, void* native_window, int width, int height);
extern void        gx_android_destroy(void* host);
extern const char* gx_android_version();

// JNI 反射链取 ANativeWindow*（定义在 native_window_jni.c）：失败返回 NULL。
void* gx_go_native_window_from_activity(JNIEnv* env, jobject activity);
*/
import "C"

import (
	"fmt"
	"unsafe"

	mobileapp "golang.org/x/mobile/app"
)

// renderer 持有 Rust 渲染宿主句柄。零值 = 未初始化（surface 未就绪）。
type renderer struct {
	host   unsafe.Pointer
	physW  int
	physH  int
	inited bool
	fails  int
}

var v2r renderer

// Version 返回 C ABI 版本串（空串表示静态库缺失/符号不符）。
func Version() string {
	p := C.gx_android_version()
	if p == nil {
		return ""
	}
	return C.GoString(p)
}

// v2New 在 surface 就绪时构造 Rust 渲染宿主。失败可由上层重试。
func v2New(nativeWindow unsafe.Pointer, w, h int) error {
	if v2r.inited {
		v2Destroy()
	}
	host := C.gx_android_new(nativeWindow, C.int(w), C.int(h))
	if host == nil {
		return fmt.Errorf("gx_android_new: wgpu 初始化失败")
	}
	v2r.host, v2r.physW, v2r.physH, v2r.inited = host, w, h, true
	return nil
}

func v2SetWindow(nativeWindow unsafe.Pointer, w, h int) error {
	if !v2r.inited {
		return v2New(nativeWindow, w, h)
	}
	if rc := C.gx_android_set_window(v2r.host, nativeWindow, C.int(w), C.int(h)); rc != 0 {
		return fmt.Errorf("gx_android_set_window rc=%d", rc)
	}
	v2r.physW, v2r.physH = w, h
	return nil
}

func v2Resize(w, h int) {
	if !v2r.inited || w <= 0 || h <= 0 || (w == v2r.physW && h == v2r.physH) {
		return
	}
	C.gx_android_resize(v2r.host, C.int(w), C.int(h))
	v2r.physW, v2r.physH = w, h
}

func v2Apply(data []byte) error {
	if !v2r.inited {
		return fmt.Errorf("v2: 渲染器未就绪")
	}
	if len(data) == 0 {
		return nil
	}
	rc := C.gx_android_apply(v2r.host, (*C.uchar)(unsafe.Pointer(&data[0])), C.int(len(data)))
	if rc != 0 {
		return fmt.Errorf("gx_android_apply rc=%d", rc)
	}
	return nil
}

func v2Render() error {
	if !v2r.inited {
		return fmt.Errorf("v2: 渲染器未就绪")
	}
	if rc := C.gx_android_render(v2r.host); rc != 0 {
		v2r.fails++
		if v2r.fails == 1 || v2r.fails%120 == 0 {
			return fmt.Errorf("gx_android_render rc=%d（连续 %d 次）", rc, v2r.fails)
		}
		return nil
	}
	v2r.fails = 0
	return nil
}

func v2Destroy() {
	if v2r.inited {
		C.gx_android_destroy(v2r.host)
		v2r.host, v2r.inited = nil, false
	}
}

func v2Ready() bool { return v2r.inited }

// nativeWindow 在 JVM 线程上经反射链取当前 ANativeWindow*。
func nativeWindow() unsafe.Pointer {
	var out unsafe.Pointer
	err := mobileapp.RunOnJVM(func(vm, env, ctx uintptr) error {
		out = C.gx_go_native_window_from_activity(
			(*C.JNIEnv)(unsafe.Pointer(env)),
			(C.jobject)(unsafe.Pointer(ctx)),
		)
		return nil
	})
	if err != nil {
		return nil
	}
	return out
}
