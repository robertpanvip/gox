//go:build android

package main

// rustffi —— BACKEND_ARCHITECTURE_V2 阶段 5 的 Android 渲染接入。
//
// 职责两件事：
//  1. gx_android_* C ABI 的 Go 绑定（Rust 渲染内核，见 backend/rust/rust/src/
//     android_abi.rs）：ANativeWindow 注入 → wgpu surface → GRX3 指令流直通
//     apply/render。
//  2. 经 RunOnJVM 反射链拿 ANativeWindow*：Activity.getWindow().getDecorView()
//     深度遍历找 SurfaceView → getHolder().getSurface() → ANativeWindow_fromSurface。
//     （x/mobile 内部的 EGL surface 不暴露 ANativeWindow，走标准 JNI 链。）
//
// 生命周期契约：surface 随 Activity 可见性销毁重建——CrossOn 注入/重建渲染器，
// CrossOff 销毁（GPU 资源随旧 Renderer drop 释放），与 android_surface.rs 的
// set_native_window 契约一致。

/*
#cgo LDFLAGS: -lgxbackend_rust -ldl -lm -llog -landroid -lc++_static -lc++abi
#include <jni.h>
#include <stdlib.h>
#include <stdint.h>

extern void*       gx_android_new(void* native_window, int width, int height);
extern int         gx_android_apply(void* host, const unsigned char* buf, int len);
extern int         gx_android_render(void* host);
extern int         gx_android_resize(void* host, int width, int height);
extern int         gx_android_set_window(void* host, void* native_window, int width, int height);
extern void        gx_android_destroy(void* host);

// JNI 反射链取 ANativeWindow*（定义在下方 C 函数）：失败返回 NULL。
void* gx_go_native_window_from_activity(JNIEnv* env, jobject activity);
*/
import "C"

import (
	"fmt"
	"log"
	"unsafe"

	mobileapp "golang.org/x/mobile/app"
)

// V2Renderer 持有 Rust 渲染宿主句柄。零值 = 未初始化（surface 未就绪）。
type V2Renderer struct {
	host   unsafe.Pointer // *C.AndroidHost（Rust AndroidHost）
	physW  int
	physH  int
	inited bool
	fails  int // 连续 render 失败计数（>阈值输出诊断日志）
}

var v2r V2Renderer

// v2New 在 surface 就绪（CrossOn）时构造 Rust 渲染宿主。失败返回错误，
// 宿主可继续重试（surface 重建时机不保证一次成功）。
func v2New(nativeWindow unsafe.Pointer, physW, physH int) error {
	if v2r.inited {
		v2Destroy()
	}
	host := C.gx_android_new(nativeWindow, C.int(physW), C.int(physH))
	if host == nil {
		return fmt.Errorf("gx_android_new: wgpu 初始化失败（Vulkan/GLES 均不可用？）")
	}
	v2r.host = host
	v2r.physW, v2r.physH = physW, physH
	v2r.inited = true
	return nil
}

// v2SetWindow surface 重建（同 Activity 再次可见 / 尺寸变化重建）：换句柄重建渲染器。
func v2SetWindow(nativeWindow unsafe.Pointer, physW, physH int) error {
	if !v2r.inited {
		return v2New(nativeWindow, physW, physH)
	}
	if rc := C.gx_android_set_window(v2r.host, nativeWindow, C.int(physW), C.int(physH)); rc != 0 {
		return fmt.Errorf("gx_android_set_window rc=%d", rc)
	}
	v2r.physW, v2r.physH = physW, physH
	return nil
}

// v2Resize 渲染目标尺寸变化（surface 未销毁的 resize 路径）。
func v2Resize(physW, physH int) {
	if !v2r.inited || physW <= 0 || physH <= 0 {
		return
	}
	if physW != v2r.physW || physH != v2r.physH {
		C.gx_android_resize(v2r.host, C.int(physW), C.int(physH))
		v2r.physW, v2r.physH = physW, physH
	}
}

// v2Apply 提交一帧 GRX3 指令流。rc=0 成功；非 0 时返回错误。
func v2Apply(data []byte) error {
	if !v2r.inited {
		return fmt.Errorf("v2: 渲染器未就绪")
	}
	rc := C.gx_android_apply(v2r.host, (*C.uchar)(unsafe.Pointer(&data[0])), C.int(len(data)))
	if rc != 0 {
		return fmt.Errorf("gx_android_apply rc=%d", rc)
	}
	return nil
}

// v2Render 渲染并 present。错误只在连续失败时打日志（避免刷屏）。
func v2Render() error {
	if !v2r.inited {
		return fmt.Errorf("v2: 渲染器未就绪")
	}
	rc := C.gx_android_render(v2r.host)
	if rc != 0 {
		v2r.fails++
		if v2r.fails == 1 || v2r.fails%120 == 0 {
			return fmt.Errorf("gx_android_render rc=%d（连续 %d 次）", rc, v2r.fails)
		}
		return nil // 已记日志，不再向上传
	}
	v2r.fails = 0
	return nil
}

// v2Destroy surface 失效（CrossOff）：销毁渲染器，GPU 资源释放。
func v2Destroy() {
	if v2r.inited {
		C.gx_android_destroy(v2r.host)
		v2r.host = nil
		v2r.inited = false
	}
}

// v2Ready 报告渲染器是否已就绪（E2E 诊断）。
func v2Ready() bool { return v2r.inited }

// v2Size 返回当前渲染目标物理尺寸。
func v2Size() (int, int) { return v2r.physW, v2r.physH }

// ---------------------------------------------------------------- JNI 链

// getNativeWindow 在 JVM 线程上经反射链取当前 ANativeWindow*。
// 链路：activity.getWindow().getDecorView() → 深度遍历找 SurfaceView →
// getHolder().getSurface() → ANativeWindow_fromSurface。
// 失败返回 nil（调用方按 surface 未就绪处理，等待重试）。
func getNativeWindow() unsafe.Pointer {
	var out unsafe.Pointer
	err := mobileapp.RunOnJVM(func(vm, env, ctx uintptr) error {
		out = C.gx_go_native_window_from_activity(
			(*C.JNIEnv)(unsafe.Pointer(env)),
			(C.jobject)(unsafe.Pointer(ctx)),
		)
		return nil
	})
	if err != nil {
		logWarn("JNI 取 ANativeWindow 失败: %v", err)
		return nil
	}
	return out
}

// logWarn 统一走 GoLog tag（logcat 过滤 GXTEST/GX 用）。
func logWarn(format string, args ...any) {
	log.Printf("V2 "+format, args...)
}

