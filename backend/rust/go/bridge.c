#include <stddef.h>

/* Rust 侧事件回调类型 */
typedef void (*gx_event_cb)(const char* json, void* ctx);

/* Go 的 //export goEventBridge 生成符号（cgo 生成签名为 char*） */
extern void goEventBridge(char* json, void* ctx);

/* Rust 的注册函数 */
extern void gx_window_set_event_cb(void* handle, gx_event_cb cb, void* ctx);

/*
 * C 跳板：由 Go 调用，把导出的 Go 函数指针注册给 Rust。
 * 放在独立 .c 文件，避免 //export 文件 preamble 不能含函数定义的限制。
 */
void install_event_cb(void* handle, void* ctx) {
    gx_window_set_event_cb(handle, (gx_event_cb)goEventBridge, ctx);
}
