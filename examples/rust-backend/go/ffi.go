package rustbackend

/*
#cgo CFLAGS: -I${SRCDIR}/../rust/target/release
#cgo LDFLAGS: -L${SRCDIR}/../rust/target/release -lgxbackend_rust -lpthread -ldl -lm
#include <stdlib.h>
#include <stdint.h>

typedef void (*gx_event_cb)(const char* json, void* ctx);

extern void*       gx_window_new(int width, int height, const char* title);
extern int         gx_window_draw(void* handle, const uint8_t* buf, int len);
extern void        gx_window_set_event_cb(void* handle, gx_event_cb cb, void* ctx);
extern int         gx_window_run(void* handle);
extern void        gx_window_close(void* handle);
extern const char* gx_window_version();
extern uint64_t    gx_bench_frame(const uint8_t* buf, int len, float width, float height, int iters);

// 定义在 bridge.c：把 Go 导出回调注册给 Rust。
extern void install_event_cb(void* handle, void* ctx);
*/
import "C"

import (
	"encoding/json"
	"unsafe"
)

//export goEventBridge
func goEventBridge(jsonPtr *C.char, ctx unsafe.Pointer) {
	s := C.GoString(jsonPtr)
	var ev inputEvent
	if err := json.Unmarshal([]byte(s), &ev); err != nil {
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

// ---- C 调用的 Go 封装（window.go 只用这些，不直接 import "C"）----

func cWindowNew(width, height int, title string) unsafe.Pointer {
	ct := C.CString(title)
	defer C.free(unsafe.Pointer(ct))
	return C.gx_window_new(C.int(width), C.int(height), ct)
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
