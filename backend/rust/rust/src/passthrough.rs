//! 鼠标穿透（MousePassthrough，P4 遗留补齐）：把窗口从鼠标事件投递路径上摘掉。
//!
//! 三端语义（整窗级开关；区域级精确穿透是后续增强，见 BACKEND_ARCHITECTURE_V2 遗留项）：
//!   - Linux/X11：`XShapeCombineRectangles(ShapeInput)` —— 输入形状为空 = 整窗穿透；
//!     恢复 = 一个覆盖全窗口的矩形。经 dlopen libXext 动态解析符号，不引入链接期
//!     依赖（winit 的 XConnection 已调 XInitThreads，跨线程调用安全）。
//!   - Windows：`WS_EX_TRANSPARENT | WS_EX_LAYERED` 扩展样式开关（整窗级）。
//!   - macOS：`NSWindow.setIgnoresMouseEvents`（整窗级）。
//!
//! 与旧 skia glwindow 实现的关系：旧实现按本帧 DrawLog 逐帧重设 XShape（区域级
//! 精确穿透，"透明区点击落到后面"）。V2 渲染端是 wgpu 全量重绘 + 增量流，尚无
//! 等价的逐帧不透明区域日志，先提供整窗级开关支撑 overlay 场景；区域级在拿到
//! 渲染端内容框日志后按同一口径补齐。

use std::ffi::c_long;
use winit::window::Window;

/// 整窗级穿透开关。成功返回 Ok；平台不支持（Wayland）或系统库缺失返回 Err，
/// Go 侧透传为错误（不静默）。
pub fn set_mouse_passthrough(window: &Window, enabled: bool) -> Result<(), String> {
    #[cfg(target_os = "linux")]
    {
        return x11_apply(window, enabled);
    }
    #[cfg(target_os = "windows")]
    {
        return win32_apply(window, enabled);
    }
    #[cfg(target_os = "macos")]
    {
        return darwin_apply(window, enabled);
    }
    #[cfg(not(any(target_os = "linux", target_os = "windows", target_os = "macos")))]
    {
        let _ = (window, enabled);
        Err("passthrough unsupported on this platform".to_string())
    }
}

// ---------- Linux / X11 ----------

#[cfg(target_os = "linux")]
mod x11 {
    use std::ffi::{c_int, c_long, CString};
    use std::sync::OnceLock;

    /// XRectangle（与 X11/extensions/shape.h 布局一致：short x, y; unsigned short w, h）。
    #[repr(C)]
    #[allow(non_snake_case)]
    pub struct XRectangle {
        pub x: i16,
        pub y: i16,
        pub width: u16,
        pub height: u16,
    }

    pub type XShapeFn = unsafe extern "C" fn(
        display: *mut std::ffi::c_void,
        dest: c_long,
        dest_kind: c_int,
        x_off: c_int,
        y_off: c_int,
        rects: *const XRectangle,
        n_rects: c_int,
        op: c_int,
        ordering: c_int,
    );

    pub type XFlushFn = unsafe extern "C" fn(display: *mut std::ffi::c_void) -> c_int;

    /// X11 shape 常量（shape.h）：ShapeInput=2、ShapeSet=0、Unsorted=0。
    pub const SHAPE_INPUT: c_int = 2;
    pub const SHAPE_SET: c_int = 0;
    pub const UNSORTED: c_int = 0;

    /// dlopen 解析 XShapeCombineRectangles + XFlush（libXext/libX11）。
    /// 缓存一次；失败返回 None（无 X11 运行环境时穿透调用报错，不 panic）。
    pub fn symbols() -> Option<(XShapeFn, XFlushFn)> {
        static SYMS: OnceLock<(usize, usize)> = OnceLock::new();
        let (shape_addr, flush_addr) = *SYMS.get_or_init(|| unsafe {
            for name in ["libXext.so.6", "libXext.so"] {
                let cname = match CString::new(name) {
                    Ok(c) => c,
                    Err(_) => continue,
                };
                let handle = dlopen(cname.as_ptr(), RTLD_NOW);
                if handle.is_null() {
                    continue;
                }
                let sym = match CString::new("XShapeCombineRectangles") {
                    Ok(c) => c,
                    Err(_) => continue,
                };
                let p = dlsym(handle, sym.as_ptr());
                if p.is_null() {
                    continue;
                }
                // XFlush 在 libX11；同一进程内 dlsym 任意句柄都能解析到已加载库。
                let flush_sym = match CString::new("XFlush") {
                    Ok(c) => c,
                    Err(_) => return (0, 0),
                };
                let fp = dlsym(handle, flush_sym.as_ptr());
                if fp.is_null() {
                    return (0, 0);
                }
                return (p as usize, fp as usize);
            }
            (0, 0)
        });
        if shape_addr == 0 || flush_addr == 0 {
            return None;
        }
        Some((
            unsafe { std::mem::transmute::<usize, XShapeFn>(shape_addr) },
            unsafe { std::mem::transmute::<usize, XFlushFn>(flush_addr) },
        ))
    }

    const RTLD_NOW: i32 = 2;

    unsafe extern "C" {
        fn dlopen(filename: *const i8, flag: i32) -> *mut std::ffi::c_void;
        fn dlsym(handle: *mut std::ffi::c_void, symbol: *const i8) -> *mut std::ffi::c_void;
    }
}

#[cfg(target_os = "linux")]
fn x11_apply(window: &Window, enabled: bool) -> Result<(), String> {
    use raw_window_handle::{HasDisplayHandle, HasWindowHandle, RawDisplayHandle, RawWindowHandle};

    let (dpy, xwin) = {
        let dh = window
            .display_handle()
            .map_err(|_| "raw display handle unavailable".to_string())?;
        let wh = window
            .window_handle()
            .map_err(|_| "raw window handle unavailable".to_string())?;
        match (dh.as_raw(), wh.as_raw()) {
            (RawDisplayHandle::Xlib(d), RawWindowHandle::Xlib(w)) => {
                let dpy = d
                    .display
                    .map(|p| p.as_ptr())
                    .ok_or("X11 display pointer unavailable")?;
                (dpy, w.window)
            }
            _ => {
                return Err(
                    "passthrough requires X11 (Wayland has no equivalent API)".to_string(),
                )
            }
        }
    };

    let Some((shape, xflush)) = x11::symbols() else {
        return Err("libXext not found (XShape unavailable)".to_string());
    };

    unsafe {
        if enabled {
            // 空输入形状 = 整窗穿透（n_rects=0）。
            (shape)(
                dpy,
                xwin as c_long,
                x11::SHAPE_INPUT,
                0,
                0,
                std::ptr::null(),
                0,
                x11::SHAPE_SET,
                x11::UNSORTED,
            );
        } else {
            // 全窗口矩形 = 恢复正常接收。
            let size = window.inner_size();
            let rect = x11::XRectangle {
                x: 0,
                y: 0,
                width: size.width.min(u16::MAX as u32) as u16,
                height: size.height.min(u16::MAX as u32) as u16,
            };
            (shape)(
                dpy,
                xwin as c_long,
                x11::SHAPE_INPUT,
                0,
                0,
                &rect,
                1,
                x11::SHAPE_SET,
                x11::UNSORTED,
            );
        }
        // 立即 flush：Xlib 请求是缓冲的，不等 winit 事件线程的下一次操作。
        let _ = (xflush)(dpy);
    }
    Ok(())
}

// ---------- Windows ----------

#[cfg(target_os = "windows")]
mod win32 {
    pub const GWL_EXSTYLE: i32 = -20;
    pub const WS_EX_LAYERED: isize = 0x0008_0000;
    pub const WS_EX_TRANSPARENT: isize = 0x0000_0020;
}

#[cfg(target_os = "windows")]
fn win32_apply(window: &Window, enabled: bool) -> Result<(), String> {
    use raw_window_handle::{HasWindowHandle, RawWindowHandle};

    let hwnd = {
        let h = window
            .window_handle()
            .map_err(|_| "raw window handle unavailable".to_string())?;
        match h.window_handle() {
            RawWindowHandle::Win32(w) => w.hwnd.get() as isize,
            _ => return Err("not a Win32 window".to_string()),
        }
    };

    unsafe extern "system" {
        fn GetWindowLongPtrW(hwnd: isize, index: i32) -> isize;
        fn SetWindowLongPtrW(hwnd: isize, index: i32, value: isize) -> isize;
    }

    unsafe {
        let ex = GetWindowLongPtrW(hwnd, win32::GWL_EXSTYLE);
        let next = if enabled {
            ex | win32::WS_EX_LAYERED | win32::WS_EX_TRANSPARENT
        } else {
            ex & !(win32::WS_EX_LAYERED | win32::WS_EX_TRANSPARENT)
        };
        if SetWindowLongPtrW(hwnd, win32::GWL_EXSTYLE, next) == 0 {
            return Err("SetWindowLongPtrW failed".to_string());
        }
    }
    Ok(())
}

// ---------- macOS ----------

#[cfg(target_os = "macos")]
fn darwin_apply(window: &Window, enabled: bool) -> Result<(), String> {
    use raw_window_handle::{HasWindowHandle, RawWindowHandle};

    let ns_view = {
        let h = window
            .window_handle()
            .map_err(|_| "raw window handle unavailable".to_string())?;
        match h.window_handle() {
            RawWindowHandle::AppKit(w) => w.ns_view.get() as usize,
            _ => return Err("not an AppKit window".to_string()),
        }
    };

    use std::ffi::CString;

    unsafe extern "C" {
        fn objc_msgSend(receiver: usize, selector: usize, ...);
        fn sel_registerName(name: *const i8) -> usize;
    }

    unsafe {
        let ns_win = objc_msgSend(
            ns_view,
            sel_registerName(CString::new("window").unwrap().as_ptr()),
        );
        if ns_win == 0 {
            return Err("NSWindow unavailable".to_string());
        }
        // setIgnoresMouseEvents:（BOOL 参数，按 ARM64 objc ABI 以寄存器传 8 位值）。
        objc_msgSend(
            ns_win,
            sel_registerName(CString::new("setIgnoresMouseEvents:").unwrap().as_ptr()),
            enabled as u8,
        );
    }
    Ok(())
}

#[cfg(all(test, target_os = "linux"))]
mod passthrough_tests {
    use super::x11;

    /// libXext 符号经 dlopen 可解析（XShape 路径的关键依赖；无 X11 运行环境时
    /// 本测试失败即环境缺库，构建机需安装 libxext6）。
    #[test]
    fn xshape_symbols_resolvable() {
        assert!(
            x11::symbols().is_some(),
            "dlopen libXext / XShapeCombineRectangles 失败：运行环境需安装 libxext6"
        );
    }
}
