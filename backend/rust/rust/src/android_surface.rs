//! Android NDK 原生 SurfaceProvider（BACKEND_ARCHITECTURE_V2 阶段 5）。
//!
//! 宿主侧（Go x/mobile NativeActivity）在 EGL/Choreographer 线程拿到
//! `ANativeWindow*` 后经 C ABI 注入本适配器；渲染内核（render.rs）零改动——
//! `Renderer::new(Arc<dyn SurfaceProvider>)` 接受任何平台实现。
//!
//! 句柄有效性契约：`ANativeWindow*` 的生命周期由宿主管理。Android 的
//! surface 会随 lifecycle 销毁重建（onSurfaceDestroyed/onSurfaceCreated），
//! 宿主必须在重建时销毁旧 Renderer 或调用 `set_native_window` 换新指针；
//! Renderer 持有 provider 引用但不延长 ANativeWindow 生命周期（与 winit
//! 同语义：句柄来源由宿主保活）。
//!
//! 本文件不 import 任何窗口库：AndroidNdkWindowHandle 是 raw-window-handle
//! 的纯数据类型，任何平台都可构造（真机消费由 wgpu 在 android target 完成）。
//! 这使得 provider 的全部行为可以在桌面环境无头测试。

use crate::render::SurfaceProvider;
use raw_window_handle::{RawDisplayHandle, RawWindowHandle};
use std::sync::Mutex;

pub struct AndroidSurface {
    inner: Mutex<Inner>,
    redraw: Box<dyn Fn() + Send + Sync>,
}

struct Inner {
    /// `ANativeWindow*`（宿主注入；NonNull 契约在构造时强制）。
    native_window: std::ptr::NonNull<std::ffi::c_void>,
    w: u32,
    h: u32,
}

// 安全性：本指针从不被解引用——只作为 opaque 句柄转发给 wgpu 的
// SurfaceTargetUnsafe。访问全部经 Mutex（数据竞争安全）；生命周期由宿主
// 契约保证（set_native_window / Renderer 销毁时序），与 winit 适配器同语义。
unsafe impl Send for Inner {}
unsafe impl Sync for Inner {}

impl AndroidSurface {
    /// 从宿主注入的 `ANativeWindow*` 构造。ptr 为空视为宿主 bug，立即 panic
    /// （晚 panic 不如早 panic：空句柄进 wgpu 会是更难查的 UB）。
    pub fn from_native_window(
        ptr: *mut std::ffi::c_void,
        w: u32,
        h: u32,
        redraw: Box<dyn Fn() + Send + Sync>,
    ) -> Self {
        assert!(
            !ptr.is_null(),
            "AndroidSurface: ANativeWindow 指针不能为空（surface 创建失败应走重试而不是注入空指针）"
        );
        AndroidSurface {
            inner: Mutex::new(Inner {
                native_window: std::ptr::NonNull::new(ptr).expect("nonnull"),
                w,
                h,
            }),
            redraw,
        }
    }

    /// lifecycle 重建 surface：换新 ANativeWindow 指针与尺寸（onSurfaceCreated）。
    pub fn set_native_window(&self, ptr: *mut std::ffi::c_void, w: u32, h: u32) {
        assert!(!ptr.is_null(), "set_native_window: 指针不能为空");
        let mut g = self.inner.lock().expect("lock");
        g.native_window = std::ptr::NonNull::new(ptr).expect("nonnull");
        g.w = w;
        g.h = h;
    }
}

impl SurfaceProvider for AndroidSurface {
    fn pixel_size(&self) -> (u32, u32) {
        let g = self.inner.lock().expect("lock");
        (g.w, g.h)
    }
    fn raw_display_handle(&self) -> RawDisplayHandle {
        RawDisplayHandle::Android(raw_window_handle::AndroidDisplayHandle::new())
    }
    fn raw_window_handle(&self) -> RawWindowHandle {
        let g = self.inner.lock().expect("lock");
        RawWindowHandle::AndroidNdk(raw_window_handle::AndroidNdkWindowHandle::new(
            g.native_window,
        ))
    }
    fn request_redraw(&self) {
        (self.redraw)()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicUsize, Ordering};
    use std::sync::Arc;

    /// 假 ANativeWindow：真实宿主注入的是 NativeActivity 的 ANativeWindow；
    /// 无头测试用对齐地址的伪指针验证句柄数据通路（从不解引用）。
    fn fake_window_ptr() -> *mut std::ffi::c_void {
        0x1000 as *mut std::ffi::c_void
    }

    #[test]
    fn android_surface_handles_and_size() {
        let s = AndroidSurface::from_native_window(fake_window_ptr(), 1080, 2400, Box::new(|| {}));
        assert_eq!(s.pixel_size(), (1080, 2400));
        assert!(matches!(s.raw_display_handle(), RawDisplayHandle::Android(_)));
        match s.raw_window_handle() {
            RawWindowHandle::AndroidNdk(h) => {
                assert_eq!(h.a_native_window.as_ptr(), fake_window_ptr());
            }
            other => panic!("应为 AndroidNdk 变体: {other:?}"),
        }
    }

    #[test]
    fn android_surface_lifecycle_recreate() {
        let s = AndroidSurface::from_native_window(fake_window_ptr(), 1080, 2400, Box::new(|| {}));
        // 旋转屏幕：onSurfaceDestroyed → onSurfaceCreated(w×h 交换)。
        s.set_native_window(0x2000 as *mut std::ffi::c_void, 2400, 1080);
        assert_eq!(s.pixel_size(), (2400, 1080));
        match s.raw_window_handle() {
            RawWindowHandle::AndroidNdk(h) => {
                assert_eq!(h.a_native_window.as_ptr(), 0x2000 as *mut _);
            }
            other => panic!("应为 AndroidNdk 变体: {other:?}"),
        }
    }

    #[test]
    fn android_surface_request_redraw_fires_callback() {
        let hits = Arc::new(AtomicUsize::new(0));
        let h2 = hits.clone();
        let s = AndroidSurface::from_native_window(
            fake_window_ptr(),
            100,
            100,
            Box::new(move || { h2.fetch_add(1, Ordering::SeqCst); }),
        );
        s.request_redraw();
        s.request_redraw();
        assert_eq!(hits.load(Ordering::SeqCst), 2, "request_redraw 必须转发到宿主唤醒点");
    }

    #[test]
    #[should_panic(expected = "不能为空")]
    fn android_surface_rejects_null_window() {
        let _ = AndroidSurface::from_native_window(std::ptr::null_mut(), 100, 100, Box::new(|| {}));
    }
}
