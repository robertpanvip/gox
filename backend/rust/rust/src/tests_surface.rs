//! SurfaceProvider 平台解耦契约测试（BACKEND_ARCHITECTURE_V2 阶段 3）。
//!
//! 验收标准："同一个 Renderer 能挂载不同平台 Surface"。本文件作为内核侧
//! 证明：一个**不引入任何窗口库**的宿主即可实现 SurfaceProvider 并引用
//! 渲染内核的公开 API——winit 依赖只存在于 lib.rs 的平台适配层。

#![cfg(test)]

use crate::render::SurfaceProvider;
use raw_window_handle::{RawDisplayHandle, RawWindowHandle};

/// 纯 mock 宿主：模拟 Android NDK / iOS CAMetalLayer / 嵌入式平台。
/// 不 import winit、不创建真实窗口——这正是平台适配层所需的最小接口面。
struct MockSurface {
    w: u32,
    h: u32,
}

impl SurfaceProvider for MockSurface {
    fn pixel_size(&self) -> (u32, u32) {
        (self.w, self.h)
    }
    fn raw_display_handle(&self) -> RawDisplayHandle {
        // mock 句柄从不被真实后端消费（本测试不创建 wgpu surface），
        // dangling 指针即可。
        RawDisplayHandle::AppKit(raw_window_handle::AppKitDisplayHandle::new())
    }
    fn raw_window_handle(&self) -> RawWindowHandle {
        RawWindowHandle::AppKit(raw_window_handle::AppKitWindowHandle::new(
            std::ptr::NonNull::dangling(),
        ))
    }
    fn request_redraw(&self) {
        // mock：平台事件循环唤醒点
    }
}

#[test]
fn surface_provider_is_object_safe_and_platform_free() {
    // trait object：Renderer::new(Arc<dyn SurfaceProvider>) 接受任意平台宿主。
    let provider: std::sync::Arc<dyn SurfaceProvider> =
        std::sync::Arc::new(MockSurface { w: 1920, h: 1080 });
    assert_eq!(provider.pixel_size(), (1920, 1080));
    // handle 构造不 panic、返回 AppKit 变体（证明句柄是纯数据，无窗口库回调）。
    assert!(matches!(
        provider.raw_window_handle(),
        RawWindowHandle::AppKit(_)
    ));
}

/// 契约：render.rs（渲染内核）不得 import 任何窗口库。
/// 若有人重新引入 winit/UIKit 等，此测试立即失败，守住阶段 3 的解耦成果。
#[test]
fn render_kernel_has_no_window_library_dependency() {
    const KERNEL: &str = include_str!("render.rs");
    for banned in ["use winit", "extern crate winit", "use tao", "use glutin"] {
        assert!(!KERNEL.contains(banned), "渲染内核不得依赖窗口库: {banned}");
    }
}
