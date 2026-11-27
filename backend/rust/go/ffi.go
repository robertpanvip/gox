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
extern int         gx_window_take_fullsync(void* handle);
extern void        gx_window_set_event_cb(void* handle, gx_event_cb cb, void* ctx);
extern int         gx_window_pump(void* handle);
extern void        gx_window_close(void* handle);
extern void        gx_window_destroy(void* handle);
extern int         gx_window_set_vsync(void* handle, int mode);
extern int         gx_window_set_mouse_passthrough(void* handle, int enabled);
extern int         gx_window_get_vsync(void* handle);
extern const char* gx_window_version();
extern char* gx_window_debug_info();
extern void gx_window_debug_free(char* ptr);
extern char* gx_clipboard_get();
extern void gx_clipboard_free(char* ptr);
extern int gx_clipboard_set(const char* text);
extern int         gx_measure_text_advances(const char* family, int size, float* narrow, float* wide);
extern uint64_t    gx_bench_frame(const uint8_t* buf, int len, float width, float height, int iters);
extern uint64_t    gx_bench_frame_cached(const uint8_t* buf, int len, float width, float height, int iters);

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
		// 分级投递（评审 P1）：可靠事件不丢、可合并事件保留最新，且全部非阻塞，
		// 绝不卡住 Rust 事件循环线程。
		w.postEvent(ev)
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

// cWindowTakeFullSync 查询 Rust 侧是否因队列过载背压丢弃了未消费帧（读后清零）。
// 返回 1 表示镜像已失步，Go Host 下一帧必须以 FullSync 全量重建
// （BACKEND_ARCHITECTURE_V2 §六.2）。
func cWindowTakeFullSync(handle unsafe.Pointer) int {
	return int(C.gx_window_take_fullsync(handle))
}

func cWindowSetEventCb(handle unsafe.Pointer, ctx uintptr) {
	C.install_event_cb(handle, unsafe.Pointer(ctx))
}

// cWindowPoll 非阻塞泵一轮 Rust winit 事件。
// 返回 1=继续；0=事件循环已退出（应走关闭流程）；2=参数错/内部 panic。
func cWindowPoll(handle unsafe.Pointer) int {
	return int(C.gx_window_pump(handle))
}

// cWindowSetVSync 切换 Rust 侧 present 模式（VSyncControl，P4.5）。
// 返回 0=成功；1=surface 不支持（保持原值）；2=参数/句柄无效。
func cWindowSetVSync(handle unsafe.Pointer, mode int) int {
	return int(C.gx_window_set_vsync(handle, C.int(mode)))
}

// cWindowSetMousePassthrough 设置鼠标穿透模式（MousePassthrough）。
// mode：0=关 1=整窗级 2=区域级。返回 0=成功；1=空句柄；2=参数非法；
// 3=平台不支持/系统库缺失（Rust 侧已记日志）。
func cWindowSetMousePassthrough(handle unsafe.Pointer, mode int) int {
	return int(C.gx_window_set_mouse_passthrough(handle, C.int(mode)))
}

// cWindowGetVSync 读取当前请求的 vsync 模式码（0/1/2；句柄无效 -1）。
func cWindowGetVSync(handle unsafe.Pointer) int {
	return int(C.gx_window_get_vsync(handle))
}

// cWindowClose 只请求 Rust 事件循环退出，不释放 handle 内存（评审 P0 生命周期）。
func cWindowClose(handle unsafe.Pointer) {
	C.gx_window_close(handle)
}

// cWindowDestroy 真正释放 Rust Backend（Arc::from_raw）。
// 必须在 cWindowPoll 返回 0（事件循环已退出）之后调用，杜绝 pump/destroy 竞争
// 下的 use-after-free。
func cWindowDestroy(handle unsafe.Pointer) {
	C.gx_window_destroy(handle)
}

func cVersion() string {
	return C.GoString(C.gx_window_version())
}

// cDebugInfo 拉取 E2E 调试导出 JSON（窗口逻辑尺寸 + 最近一帧命中框）。
func cDebugInfo() string {
	ptr := C.gx_window_debug_info()
	defer C.gx_window_debug_free(ptr)
	return C.GoString(ptr)
}

// cClipboardGet 读取系统剪贴板（无显示服务/feature 关闭返回空串）。
func cClipboardGet() string {
	ptr := C.gx_clipboard_get()
	defer C.gx_clipboard_free(ptr)
	return C.GoString(ptr)
}

// cClipboardSet 写系统剪贴板。返回是否成功（无显示环境 false，不 panic）。
func cClipboardSet(text string) bool {
	cstr := C.CString(text)
	defer C.free(unsafe.Pointer(cstr))
	return int(C.gx_clipboard_set(cstr)) == 0
}

// cMeasureTextAdvances 调 Rust 编辑器字符 advance 实测（gx_measure_text_advances）。
// 返回 narrow（ASCII 样本平均 advance）与 wide（CJK 样本平均 advance）；
// Rust 侧失败（出参空）时返回 0, 0，由上层语义兜底。
func cMeasureTextAdvances(family string, size int) (float64, float64) {
	cFamily := C.CString(family)
	defer C.free(unsafe.Pointer(cFamily))
	var narrow, wide C.float
	rc := C.gx_measure_text_advances(cFamily, C.int(size), &narrow, &wide)
	if rc != 0 {
		return 0, 0
	}
	return float64(narrow), float64(wide)
}

// cBenchFrame 调 Rust 纯 CPU 管线基准，返回 iters 次累计纳秒。
// cBenchFrameCached 调 Rust 缓存命中基准（同一棵树 → 布局复用只跑 emit），
// 与 cBenchFrame 的比值即纯绘制属性变化帧的加速比。
func cBenchFrameCached(buf []byte, width, height float32, iters int) uint64 {
	if len(buf) == 0 {
		return 0
	}
	return uint64(C.gx_bench_frame_cached(
		(*C.uint8_t)(unsafe.Pointer(&buf[0])), C.int(len(buf)),
		C.float(width), C.float(height), C.int(iters)))
}

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
