//! Android target 的桌面 ABI stub：backend/rust/go 包的 ffi.go 声明了完整
//! 桌面 ABI 面（gx_window_* / gx_clipboard_*），android 交叉编译时这些 winit
//! 符号不编译（宿主是 Go x/mobile NativeActivity，渲染走 android_abi.rs）。
//! 为让 Go 侧包在 android target 也能编译链接（复用其序列化器），这里提供
//! 同签名空实现：任何调用都返回失败并打日志——生产路径绝不调用。

use std::ffi::{c_char, c_int, c_uchar, c_void};

macro_rules! stub_warn {
    ($name:literal) => {
        eprintln!(concat!("[gx-android] ", $name, " 是桌面 ABI，Android 端不可用（请走 gx_android_*）"));
    };
}

#[no_mangle]
pub extern "C" fn gx_window_new(
    _width: c_int,
    _height: c_int,
    _title: *const c_char,
) -> *mut c_void {
    stub_warn!("gx_window_new");
    std::ptr::null_mut()
}

#[no_mangle]
pub extern "C" fn gx_window_new_ex(
    _width: c_int,
    _height: c_int,
    _title: *const c_char,
    _decorated: c_int,
    _transparent: c_int,
    _always_on_top: c_int,
    _opacity: f32,
) -> *mut c_void {
    stub_warn!("gx_window_new_ex");
    std::ptr::null_mut()
}

#[no_mangle]
pub extern "C" fn gx_window_pump(_handle: *mut c_void) -> c_int {
    stub_warn!("gx_window_pump");
    1
}

#[no_mangle]
pub extern "C" fn gx_window_close(_handle: *mut c_void) {
    stub_warn!("gx_window_close");
}

#[no_mangle]
pub extern "C" fn gx_window_destroy(_handle: *mut c_void) {
    stub_warn!("gx_window_destroy");
}

#[no_mangle]
pub extern "C" fn gx_window_set_vsync(_handle: *mut c_void, _mode: c_int) -> c_int {
    -1
}

#[no_mangle]
pub extern "C" fn gx_window_get_vsync(_handle: *mut c_void) -> c_int {
    -1
}

#[no_mangle]
pub extern "C" fn gx_window_debug_info() -> *mut c_char {
    stub_warn!("gx_window_debug_info");
    b"android: use gx_android_* ABI\0".as_ptr() as *mut c_char
}

#[no_mangle]
pub unsafe extern "C" fn gx_window_debug_free(_ptr: *mut c_char) {}

#[no_mangle]
pub extern "C" fn gx_clipboard_get() -> *mut c_char {
    std::ptr::null_mut()
}

#[no_mangle]
pub unsafe extern "C" fn gx_clipboard_free(_ptr: *mut c_char) {}

#[no_mangle]
pub extern "C" fn gx_clipboard_set(_text: *const c_char) -> c_int {
    -1
}
