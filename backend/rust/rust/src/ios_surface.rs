//! iOS UIKit 原生 SurfaceProvider（BACKEND_ARCHITECTURE_V2 阶段 5 最小验证）。
//!
//! 宿主侧（gox iOS 工程 / future x/mobile iOS 路径）从 `UIView`（推荐直接用
//! `CAMetalLayer` 宿主视图）取 `ui_view` 指针注入；渲染内核零改动。
//!
//! 与 AndroidSurface 同一套契约：句柄由宿主保活（iOS 的 UIView 可随
//! lifecycle 迁移重建，宿主须在重建时调用 `set_ui_view`）；本文件零窗口库
//! 依赖，全部行为可在桌面环境无头测试。

use crate::render::SurfaceProvider;
use raw_window_handle::{RawDisplayHandle, RawWindowHandle};
use std::sync::Mutex;

pub struct IosSurface {
    inner: Mutex<Inner>,
    redraw: Box<dyn Fn() + Send + Sync>,
}

struct Inner {
    /// `UIView*`（宿主注入；实际承载渲染的是其 CAMetalLayer 子层）。
    ui_view: std::ptr::NonNull<std::ffi::c_void>,
    w: u32,
    h: u32,
}

// 安全性：同 AndroidSurface——指针只作为 opaque 句柄转发 wgpu，从不解引用；
// 访问全部经 Mutex。宿主保证 UIView 生命周期覆盖 Renderer 存活期。
unsafe impl Send for Inner {}
unsafe impl Sync for Inner {}

impl IosSurface {
    pub fn from_ui_view(
        ptr: *mut std::ffi::c_void,
        w: u32,
        h: u32,
        redraw: Box<dyn Fn() + Send + Sync>,
    ) -> Self {
        assert!(!ptr.is_null(), "IosSurface: UIView 指针不能为空");
        IosSurface {
            inner: Mutex::new(Inner {
                ui_view: std::ptr::NonNull::new(ptr).expect("nonnull"),
                w,
                h,
            }),
            redraw,
        }
    }

    /// lifecycle 重建（视图控制器重建/转屏）：换新 UIView 指针与尺寸。
    pub fn set_ui_view(&self, ptr: *mut std::ffi::c_void, w: u32, h: u32) {
        assert!(!ptr.is_null(), "set_ui_view: 指针不能为空");
        let mut g = self.inner.lock().expect("lock");
        g.ui_view = std::ptr::NonNull::new(ptr).expect("nonnull");
        g.w = w;
        g.h = h;
    }
}

impl SurfaceProvider for IosSurface {
    fn pixel_size(&self) -> (u32, u32) {
        let g = self.inner.lock().expect("lock");
        (g.w, g.h)
    }
    fn raw_display_handle(&self) -> RawDisplayHandle {
        RawDisplayHandle::UiKit(raw_window_handle::UiKitDisplayHandle::new())
    }
    fn raw_window_handle(&self) -> RawWindowHandle {
        let g = self.inner.lock().expect("lock");
        RawWindowHandle::UiKit(raw_window_handle::UiKitWindowHandle::new(g.ui_view))
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

    fn fake_view_ptr() -> *mut std::ffi::c_void {
        0x3000 as *mut std::ffi::c_void
    }

    #[test]
    fn ios_surface_handles_and_size() {
        let s = IosSurface::from_ui_view(fake_view_ptr(), 1179, 2556, Box::new(|| {}));
        assert_eq!(s.pixel_size(), (1179, 2556));
        assert!(matches!(s.raw_display_handle(), RawDisplayHandle::UiKit(_)));
        match s.raw_window_handle() {
            RawWindowHandle::UiKit(h) => {
                assert_eq!(h.ui_view.as_ptr(), fake_view_ptr());
            }
            other => panic!("应为 UiKit 变体: {other:?}"),
        }
    }

    #[test]
    fn ios_surface_lifecycle_recreate() {
        let s = IosSurface::from_ui_view(fake_view_ptr(), 1179, 2556, Box::new(|| {}));
        s.set_ui_view(0x4000 as *mut std::ffi::c_void, 2556, 1179);
        assert_eq!(s.pixel_size(), (2556, 1179));
        match s.raw_window_handle() {
            RawWindowHandle::UiKit(h) => {
                assert_eq!(h.ui_view.as_ptr(), 0x4000 as *mut _);
            }
            other => panic!("应为 UiKit 变体: {other:?}"),
        }
    }

    #[test]
    fn ios_surface_request_redraw_fires_callback() {
        let hits = Arc::new(AtomicUsize::new(0));
        let h2 = hits.clone();
        let s = IosSurface::from_ui_view(
            fake_view_ptr(),
            100,
            100,
            Box::new(move || { h2.fetch_add(1, Ordering::SeqCst); }),
        );
        s.request_redraw();
        assert_eq!(hits.load(Ordering::SeqCst), 1);
    }

    #[test]
    #[should_panic(expected = "不能为空")]
    fn ios_surface_rejects_null_view() {
        let _ = IosSurface::from_ui_view(std::ptr::null_mut(), 100, 100, Box::new(|| {}));
    }
}
