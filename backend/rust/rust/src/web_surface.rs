//! Web 平台 SurfaceProvider（BACKEND_ARCHITECTURE_V2 阶段 6）。
//!
//! wasm32-unknown-unknown 浏览器目标：宿主（JS glue / wasm-bindgen）把
//! `<canvas>` 的 DOM id 注入本适配器，wgpu WebGPU/WebGL 后端经
//! WebWindowHandle 拿到 canvas。渲染内核零改动——与 winit / Android / iOS
//! 同一套 `Renderer::new(Arc<dyn SurfaceProvider>)`。
//!
//! 契约：
//! - `handle` 是宿主侧注册表编号（wasm-bindgen `JsValue` → index，或等价
//!   机制），对应一个已挂载的 `<canvas>`；canvas 从 DOM 卸载前宿主须先
//!   销毁 Renderer。
//! - 浏览器 resize（devicePixelRatio 变化/旋转）经 `set_size` 更新。
//! - wasm 上 `Renderer::new` 是 async：宿主用 `wasm_bindgen_futures::
//!   spawn_local` 驱动，**不得** pollster::block_on（wasm 单线程会死锁）。
//! - 本文件零窗口库依赖，全部行为可在桌面环境无头测试。

use crate::render::SurfaceProvider;
use raw_window_handle::{RawDisplayHandle, RawWindowHandle};
use std::sync::Mutex;

pub struct WebSurface {
    inner: Mutex<Inner>,
    redraw: Box<dyn Fn() + Send + Sync>,
}

struct Inner {
    /// canvas 注册表编号（rwh 0.6 WebWindowHandle.id 是 u32：宿主侧经
    /// wasm-bindgen 注册表或等价机制取得，指向已挂载的 `<canvas>`）。
    handle: u32,
    w: u32,
    h: u32,
}

impl WebSurface {
    pub fn from_canvas_handle(
        handle: u32,
        w: u32,
        h: u32,
        redraw: Box<dyn Fn() + Send + Sync>,
    ) -> Self {
        assert!(
            handle != 0,
            "WebSurface: canvas handle 不能为 0（宿主注册失败应重试而不是注入无效编号）"
        );
        WebSurface {
            inner: Mutex::new(Inner { handle, w, h }),
            redraw,
        }
    }

    /// 浏览器 resize / DPR 变化：更新像素尺寸（下一帧由内核读取重配 surface）。
    pub fn set_size(&self, w: u32, h: u32) {
        let mut g = self.inner.lock().expect("lock");
        g.w = w;
        g.h = h;
    }
}

impl SurfaceProvider for WebSurface {
    fn pixel_size(&self) -> (u32, u32) {
        let g = self.inner.lock().expect("lock");
        (g.w, g.h)
    }
    fn raw_display_handle(&self) -> RawDisplayHandle {
        RawDisplayHandle::Web(raw_window_handle::WebDisplayHandle::new())
    }
    fn raw_window_handle(&self) -> RawWindowHandle {
        let g = self.inner.lock().expect("lock");
        RawWindowHandle::Web(raw_window_handle::WebWindowHandle::new(g.handle))
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

    #[test]
    fn web_surface_handles_and_size() {
        let s = WebSurface::from_canvas_handle(7, 1280, 800, Box::new(|| {}));
        assert_eq!(s.pixel_size(), (1280, 800));
        assert!(matches!(s.raw_display_handle(), RawDisplayHandle::Web(_)));
        match s.raw_window_handle() {
            RawWindowHandle::Web(h) => {
                assert_eq!(h.id, 7);
            }
            other => panic!("应为 Web 变体: {other:?}"),
        }
    }

    #[test]
    fn web_surface_resize_updates_size() {
        let s = WebSurface::from_canvas_handle(7, 1280, 800, Box::new(|| {}));
        // 旋转 + DPR 变化。
        s.set_size(800, 1280);
        assert_eq!(s.pixel_size(), (800, 1280));
    }

    #[test]
    fn web_surface_request_redraw_fires_callback() {
        let hits = Arc::new(AtomicUsize::new(0));
        let h2 = hits.clone();
        let s = WebSurface::from_canvas_handle(
            7,
            100,
            100,
            Box::new(move || { h2.fetch_add(1, Ordering::SeqCst); }),
        );
        s.request_redraw();
        s.request_redraw();
        assert_eq!(hits.load(Ordering::SeqCst), 2);
    }

    #[test]
    #[should_panic(expected = "不能为 0")]
    fn web_surface_rejects_zero_handle() {
        let _ = WebSurface::from_canvas_handle(0, 100, 100, Box::new(|| {}));
    }
}
