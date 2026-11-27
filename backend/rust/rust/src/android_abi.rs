//! Android C ABI（BACKEND_ARCHITECTURE_V2 阶段 5 的 Go Framework 集成层）。
//!
//! 宿主 = Go x/mobile NativeActivity（保留生命周期/Choreographer/touch 全部
//! 平台能力），本 ABI 只注入"渲染内核"：Go 拿到 ANativeWindow* 后构造
//! AndroidSurface + Renderer，再把场景指令流（binprotocol，与桌面 draw 协议
//! 同一份 GRX3 编码）apply 进镜像并 render/present。
//!
//! 与桌面 gx_window_* 的关系：桌面走 winit pump_events 事件循环；Android 的
//! 帧时机由宿主 paintClock（Choreographer vsync）驱动，因此这里不做队列/
//! 事件循环，只有"同步 apply + render"两条直通路径——帧调度留在 Go 侧
//! （FrameScheduler dirty 门控），Rust 侧零状态膨胀。
//!
//! 指针契约与 android_surface.rs 一致：ANativeWindow* 由宿主管理生命周期，
//! surface 重建时必须调 gx_android_set_window 换新指针（或销毁重建）。

use crate::android_surface::AndroidSurface;
use crate::render::Renderer;
use std::ffi::{c_int, c_uchar, c_void};
use std::sync::{Arc, Mutex};

/// Go 宿主持有的渲染句柄：surface provider + 渲染内核成对存放。
/// Renderer 放 Option 以支持"surface 重建期短暂无渲染器"（lifecycle 窗口）。
pub struct AndroidHost {
    surface: Arc<AndroidSurface>,
    renderer: Mutex<Option<Renderer>>,
}

/// 构造渲染宿主：注入 ANativeWindow* 并初始化 wgpu（Vulkan/GLES 自动选后端）。
/// 同步阻塞完成（adapter/device 请求），宿主应在与 surface 绑定的线程调用。
/// 返回 NULL 表示 wgpu 初始化失败（日志已输出，宿主可重试或回退）。
#[no_mangle]
pub extern "C" fn gx_android_new(
    native_window: *mut c_void,
    width: c_int,
    height: c_int,
) -> *mut AndroidHost {
    if native_window.is_null() || width <= 0 || height <= 0 {
        eprintln!("[gx-android] gx_android_new: 非法参数（空句柄或非正尺寸）");
        return std::ptr::null_mut();
    }
    let surface = Arc::new(AndroidSurface::from_native_window(
        native_window,
        width as u32,
        height as u32,
        Box::new(|| {}), // request_redraw：Android 的帧时机由宿主 paintClock 驱动，无需回调
    ));
    let renderer = match pollster::block_on(Renderer::new(surface.clone())) {
        r => r,
    };
    let host = Box::new(AndroidHost {
        surface,
        renderer: Mutex::new(Some(renderer)),
    });
    Box::into_raw(host)
}

/// 提交一帧场景指令流（binprotocol / GRX3 编码，与 gx_window_draw 同协议）。
/// 返回 0=ok，1=参数错，2=渲染器不可用（surface 重建窗口期，宿主应重试）。
#[no_mangle]
pub extern "C" fn gx_android_apply(
    host: *mut AndroidHost,
    buf: *const c_uchar,
    len: c_int,
) -> c_int {
    if host.is_null() || buf.is_null() || len <= 0 {
        return 1;
    }
    let h = unsafe { &*host };
    let mut g = match h.renderer.lock() {
        Ok(g) => g,
        Err(_) => return 2,
    };
    let r = match g.as_mut() {
        Some(r) => r,
        None => return 2,
    };
    let slice = unsafe { std::slice::from_raw_parts(buf, len as usize) };
    if r.apply_frame(slice) {
        0
    } else {
        // apply_frame false：镜像重建需要 FullSync（与桌面语义一致）。
        3
    }
}

/// 渲染并 present 当前镜像（宿主在 paint 时机调用；同步直通，无内部队列）。
/// 返回 0=ok，2=渲染器不可用。
#[no_mangle]
pub extern "C" fn gx_android_render(host: *mut AndroidHost) -> c_int {
    if host.is_null() {
        return 1;
    }
    let h = unsafe { &*host };
    let mut g = match h.renderer.lock() {
        Ok(g) => g,
        Err(_) => return 2,
    };
    match g.as_mut() {
        Some(r) => {
            r.render();
            0
        }
        None => 2,
    }
}

/// surface 尺寸变化（宿主 size 事件）：resize 渲染目标。
#[no_mangle]
pub extern "C" fn gx_android_resize(host: *mut AndroidHost, width: c_int, height: c_int) -> c_int {
    if host.is_null() || width <= 0 || height <= 0 {
        return 1;
    }
    let h = unsafe { &*host };
    let mut g = match h.renderer.lock() {
        Ok(g) => g,
        Err(_) => return 2,
    };
    if let Some(r) = g.as_mut() {
        r.resize(width as u32, height as u32);
    }
    0
}

/// lifecycle surface 重建：换新 ANativeWindow 指针并重建渲染器（旧 GPU 资源
/// 随旧 Renderer 一并释放）。返回 0=ok。
#[no_mangle]
pub extern "C" fn gx_android_set_window(
    host: *mut AndroidHost,
    native_window: *mut c_void,
    width: c_int,
    height: c_int,
) -> c_int {
    if host.is_null() || native_window.is_null() || width <= 0 || height <= 0 {
        return 1;
    }
    let h = unsafe { &*host };
    h.surface.set_native_window(native_window, width as u32, height as u32);
    let renderer = pollster::block_on(Renderer::new(h.surface.clone()));
    let mut g = match h.renderer.lock() {
        Ok(g) => g,
        Err(_) => return 2,
    };
    *g = Some(renderer);
    0
}

/// 销毁渲染宿主（GPU 资源随 Renderer drop 释放；ANativeWindow 由宿主释放）。
#[no_mangle]
pub extern "C" fn gx_android_destroy(host: *mut AndroidHost) {
    if host.is_null() {
        return;
    }
    unsafe {
        drop(Box::from_raw(host));
    }
}

/// C ABI 版本探针（供 Go 侧 InitBackend 预检静态库/符号是否齐全）。
/// 返回静态 'static 字符串，Go 侧只读取、不释放。
#[no_mangle]
pub extern "C" fn gx_android_version() -> *const std::os::raw::c_char {
    b"gxbackend-android 0.1.0\0".as_ptr() as *const std::os::raw::c_char
}
