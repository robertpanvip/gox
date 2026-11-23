package rustbackend

/*
#cgo CFLAGS: -I${SRCDIR}/../rust/target/release
#cgo LDFLAGS: -L${SRCDIR}/../rust/target/release -lgxbackend_rust -lpthread -ldl -lm
#include <stdlib.h>
#include <stdint.h>

typedef void (*gx_event_cb)(const char* json, void* ctx);

extern void*       gx_window_new(int width, int height, const char* title);
extern void*       gx_window_new_ex(int width, int height, const char* title, int decorated, int transparent, int always_on_top, float opacity);
extern int         gx_window_draw(void* handle, const uint8_t* buf, int len);
extern void        gx_window_set_event_cb(void* handle, gx_event_cb cb, void* ctx);
extern int         gx_window_run(void* handle);
extern void        gx_window_close(void* handle);
extern const char* gx_window_version();
extern uint64_t    gx_bench_frame(const uint8_t* buf, int len, float width, float height, int iters);

// 镜像一致性探针（纯 CPU，不依赖窗口/GPU）。
extern void        gx_mirror_reset();
extern int         gx_mirror_apply(const uint8_t* buf, int len);
extern int         gx_mirror_node_count();
extern int         gx_mirror_dump(char* out, int cap);
extern uint64_t    gx_mirror_counts();

// 定义在 bridge.c：把 Go 导出回调注册给 Rust。
extern void install_event_cb(void* handle, void* ctx);
*/
import "C"

import (
	"encoding/json"
	"unsafe"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

//export goEventBridge
func goEventBridge(jsonPtr *C.char, ctx unsafe.Pointer) {
	s := C.GoString(jsonPtr)
	var ev inputEvent
	if err := json.Unmarshal([]byte(s), &ev); err != nil {
		return
	}
	// 布局回流事件：不进业务 events 通道，直接发布到 scene 回流总线。
	// 回调会写 signal → requestDraw，经 UIExecutorDispatcher 投递到 UI 线程，
	// 不会在本回调（Rust 事件线程）里重入 Draw。
	if ev.Kind == "measured" {
		publishMeasuredWire(ev.Text)
		return
	}
	windowsMu.Lock()
	w := windows[uintptr(ctx)]
	windowsMu.Unlock()
	if w != nil {
		select {
		case w.events <- ev:
		default: // 满则丢弃，不阻塞 Rust 事件循环
		}
	}
}

// publishMeasuredWire 解析 Rust 的 [[id, w, h], ...] 并发布到 scene 回流总线。
func publishMeasuredWire(text string) {
	var rows [][3]any
	if err := json.Unmarshal([]byte(text), &rows); err != nil {
		return
	}
	ms := make([]gxscene.Measured, 0, len(rows))
	for _, r := range rows {
		id, ok := r[0].(string)
		if !ok || id == "" {
			continue
		}
		w, wok := r[1].(float64)
		h, hok := r[2].(float64)
		if !wok || !hok {
			continue
		}
		ms = append(ms, gxscene.Measured{
			ID: gxscene.ParseNodeID(id),
			W:  w,
			H:  h,
		})
	}
	gxscene.PublishMeasured(ms)
}

// ---- C 调用的 Go 封装（window.go 只用这些，不直接 import "C"）----

func cWindowNew(width, height int, title string) unsafe.Pointer {
	ct := C.CString(title)
	defer C.free(unsafe.Pointer(ct))
	return C.gx_window_new(C.int(width), C.int(height), ct)
}

// cWindowNewEx 携带外观配置创建窗口。decorated: -1=默认/0=无边框/1=有边框；
// transparent/alwaysOnTop 为 0 或 1；opacity<=0 或 >1 表示不设置。
func cWindowNewEx(width, height int, title string, decorated int, transparent, alwaysOnTop bool, opacity float32) unsafe.Pointer {
	ct := C.CString(title)
	defer C.free(unsafe.Pointer(ct))
	return C.gx_window_new_ex(
		C.int(width), C.int(height), ct,
		C.int(decorated),
		cBool(transparent), cBool(alwaysOnTop),
		C.float(opacity),
	)
}

func cBool(v bool) C.int {
	if v {
		return 1
	}
	return 0
}

func cWindowDraw(handle unsafe.Pointer, buf []byte) int {
	if len(buf) == 0 {
		return 0
	}
	return int(C.gx_window_draw(handle, (*C.uint8_t)(unsafe.Pointer(&buf[0])), C.int(len(buf))))
}

func cWindowSetEventCb(handle unsafe.Pointer, ctx uintptr) {
	C.install_event_cb(handle, unsafe.Pointer(ctx))
}

func cWindowRun(handle unsafe.Pointer) int {
	return int(C.gx_window_run(handle))
}

func cWindowClose(handle unsafe.Pointer) {
	C.gx_window_close(handle)
}

func cVersion() string {
	return C.GoString(C.gx_window_version())
}

// cBenchFrame 调 Rust 纯 CPU 管线基准，返回 iters 次累计纳秒。
func cBenchFrame(buf []byte, width, height float32, iters int) uint64 {
	if len(buf) == 0 {
		return 0
	}
	return uint64(C.gx_bench_frame(
		(*C.uint8_t)(unsafe.Pointer(&buf[0])), C.int(len(buf)),
		C.float(width), C.float(height), C.int(iters),
	))
}

// ---- 镜像一致性探针封装（测试用）----

func mirrorReset() { C.gx_mirror_reset() }
func mirrorApply(buf []byte) int {
	if len(buf) == 0 {
		return 2
	}
	return int(C.gx_mirror_apply((*C.uint8_t)(unsafe.Pointer(&buf[0])), C.int(len(buf))))
}
func mirrorNodeCount() int { return int(C.gx_mirror_node_count()) }
func mirrorCounts() (applied, failed uint64) {
	c := uint64(C.gx_mirror_counts())
	return c >> 32, c & 0xFFFFFFFF
}
func mirrorDump() string {
	buf := make([]byte, 1<<20) // 1MB 足够 1000 节点的 id:text 摘要
	n := int(C.gx_mirror_dump((*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf))))
	if n < 0 {
		return ""
	}
	return string(buf[:n])
}
