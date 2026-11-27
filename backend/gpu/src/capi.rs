//! C ABI（staticlib）：供 Go CGO（含 backend/skia 的 Go 桌面端）与
//! Android NDK 消费。头文件见 gx_gpu.h。
//!
//! 后端由打包期 cargo feature 决定（feature `gpu` → wgpu；feature `cpu`
//! → tiny-skia 软光栅），**不做运行时检测**，两套实现不同时编译。
//! API 形状在两种构建下完全一致。
//!
//! 帧模型：begin 累积 → quad/tex 追加图元 → end 渲染呈现（GPU 构建直接
//! present 到 surface；CPU 构建光栅到像素缓冲，经 gx_gpu_read_rgba 取回）。
//!
//! 原始窗口句柄（仅 GPU 构建需要）：
//!   - Linux/X11：display_ptr = Display*，window_ptr = Window(XID)
//!   - Android：window_ptr = ANativeWindow*（display_ptr 忽略）
//!   - 无句柄（无头）：GPU 构建返回空指针报错；CPU 构建正常离屏工作。

use crate::{GpuFrame, TexUpload};

/// C ABI quad 参数（与 crate::Quad 字段一一对应，见 gx_gpu.h）。
#[repr(C)]
pub struct CQuad {
    pub rect: [f32; 4],
    pub clip: [f32; 4],
    pub color: [f32; 4],
    pub radius: f32,
    pub border: f32,
    pub dir: [f32; 2],
    /// 8 组 [pos, r, g, b, a]，0..255；不足部分忽略。
    pub stops: [f32; 40],
    pub stop_count: i32,
}

/// C ABI 纹理四边形参数。
#[repr(C)]
pub struct CTex {
    pub rect: [f32; 4],
    pub clip: [f32; 4],
    pub id: u32,
    pub uv: [f32; 4],
    pub radius: f32,
    pub alpha: f32,
}

/// 后端类型常量：gx_gpu_backend_kind 返回值。
pub const GX_GPU_KIND_GPU: i32 = 1;
pub const GX_GPU_KIND_CPU: i32 = 2;

/// 设备句柄：begin 累积的帧 + 编译期选定的后端。
pub struct GxGpuDev {
    frame: GpuFrame,
    inner: Backend,
}

enum Backend {
    #[cfg(feature = "gpu")]
    Wgpu(crate::wgpu::Standalone),
    #[cfg(feature = "cpu")]
    Cpu(crate::cpu::CpuBackend, Option<tiny_skia::Pixmap>),
}

/// 创建设备。GPU 构建需要 (display, window) 原始句柄；CPU 构建两者传 0。
/// 失败返回空指针（GPU 构建下适配器/表面不可用时发生，不回退 CPU）。
#[no_mangle]
pub extern "C" fn gx_gpu_new(
    w: i32, h: i32,
    display_ptr: usize, window_ptr: usize,
) -> *mut GxGpuDev {
    let w = w.max(1) as u32;
    let h = h.max(1) as u32;
    let frame = GpuFrame::new(w, h, [0.0, 0.0, 0.0, 255.0]);
    #[cfg(feature = "gpu")]
    {
        match crate::wgpu::Standalone::new(w, h, display_ptr, window_ptr) {
            Ok(s) => return Box::into_raw(Box::new(GxGpuDev { frame, inner: Backend::Wgpu(s) })),
            Err(e) => {
                eprintln!("[gx_gpu] GPU backend init failed: {e}");
                return std::ptr::null_mut();
            }
        }
    }
    #[cfg(all(not(feature = "gpu"), feature = "cpu"))]
    {
        let _ = (display_ptr, window_ptr); // CPU 后端无需窗口
        Box::into_raw(Box::new(GxGpuDev { frame, inner: Backend::Cpu(crate::cpu::CpuBackend::new(), None) }))
    }
    #[cfg(not(any(feature = "gpu", feature = "cpu")))]
    {
        let _ = (w, h, display_ptr, window_ptr);
        eprintln!("[gx_gpu] no backend compiled: enable feature gpu or cpu");
        std::ptr::null_mut()
    }
}

/// 释放设备。
#[no_mangle]
pub extern "C" fn gx_gpu_free(dev: *mut GxGpuDev) {
    if !dev.is_null() {
        unsafe { drop(Box::from_raw(dev)) };
    }
}

/// 返回编译期选定的后端：1=GPU(wgpu) 2=CPU(tiny-skia)。
#[no_mangle]
pub extern "C" fn gx_gpu_backend_kind(_dev: *mut GxGpuDev) -> i32 {
    #[cfg(feature = "gpu")]
    {
        let _ = _dev;
        GX_GPU_KIND_GPU
    }
    #[cfg(all(not(feature = "gpu"), feature = "cpu"))]
    {
        let _ = _dev;
        GX_GPU_KIND_CPU
    }
    #[cfg(not(any(feature = "gpu", feature = "cpu")))]
    0
}

/// 调整目标尺寸（GPU：重配 surface；CPU：下一帧按新尺寸光栅）。
#[no_mangle]
pub extern "C" fn gx_gpu_resize(dev: *mut GxGpuDev, w: i32, h: i32) {
    let Some(dev) = (unsafe { dev.as_mut() }) else { return };
    let (w, h) = (w.max(1) as u32, h.max(1) as u32);
    match &mut dev.inner {
        #[cfg(feature = "gpu")]
        Backend::Wgpu(s) => s.resize(w, h),
        #[cfg(feature = "cpu")]
        Backend::Cpu(_, _) => {
            dev.frame.w = w;
            dev.frame.h = h;
        }
    }
}

/// 开始一帧：清空图元并设置尺寸与清屏色（clear 为 4 个 f32，0..255）。
#[no_mangle]
pub extern "C" fn gx_gpu_begin(dev: *mut GxGpuDev, w: i32, h: i32, clear: *const f32) {
    let Some(dev) = (unsafe { dev.as_mut() }) else { return };
    let w = w.max(1) as u32;
    let h = h.max(1) as u32;
    let mut c = [0.0f32; 4];
    if !clear.is_null() {
        for (i, v) in c.iter_mut().enumerate() {
            *v = unsafe { *clear.add(i) };
        }
    }
    let uploads = std::mem::take(&mut dev.frame.uploads);
    dev.frame = GpuFrame::new(w, h, c);
    dev.frame.uploads = uploads; // 纹理注册跨帧有效，draw 时按 (id,key) 去重
}

/// 上传纹理（RGBA8；premul 非 0 表示预乘 alpha）。同 (id,key) 重复调用
/// 由后端去重。
#[no_mangle]
pub extern "C" fn gx_gpu_upload_rgba(
    dev: *mut GxGpuDev,
    id: u32, key: u64,
    w: i32, h: i32,
    data: *const u8, len: i32,
    premul: i32,
) {
    let Some(dev) = (unsafe { dev.as_mut() }) else { return };
    if data.is_null() || len <= 0 || w <= 0 || h <= 0 {
        return;
    }
    let need = (w * h * 4) as usize;
    if (len as usize) < need {
        return;
    }
    let buf = unsafe { std::slice::from_raw_parts(data, need) }.to_vec();
    dev.frame.uploads.push(TexUpload {
        id, key,
        w: w as u32, h: h as u32,
        data: std::sync::Arc::new(buf),
        premul: premul != 0,
    });
}

/// 释放纹理（当前帧模型下纹理随设备生命周期，预留显式回收口）。
#[no_mangle]
pub extern "C" fn gx_gpu_release_tex(_dev: *mut GxGpuDev, _id: u32) {
    // 两个后端的纹理缓存均为"本帧引用即存活"语义；显式回收待帧模型
    // 演进（虚拟列表大图集）时启用，当前为 no-op 以保持 ABI 稳定。
}

/// 追加一条 quad（渐变走 stops/stop_count + dir；纯色时 stop_count=0）。
#[no_mangle]
pub extern "C" fn gx_gpu_quad(dev: *mut GxGpuDev, q: *const CQuad) {
    let Some(dev) = (unsafe { dev.as_mut() }) else { return };
    let Some(q) = (unsafe { q.as_ref() }) else { return };
    if q.stop_count > 0 {
        let mut stops = Vec::with_capacity(q.stop_count.min(8) as usize);
        for i in 0..(q.stop_count.min(8)) as usize {
            let o = i * 5;
            stops.push([
                q.stops[o], q.stops[o + 1], q.stops[o + 2], q.stops[o + 3], q.stops[o + 4],
            ]);
        }
        dev.frame.prims.push(crate::Prim::Quad(crate::Quad {
            rect: q.rect, clip: q.clip, color: q.color,
            radius: q.radius, border: q.border,
            grad: Some(crate::Grad { dx: q.dir[0], dy: q.dir[1], stops }),
        }));
    } else {
        dev.frame.prims.push(crate::Prim::Quad(crate::Quad {
            rect: q.rect, clip: q.clip, color: q.color,
            radius: q.radius, border: q.border, grad: None,
        }));
    }
}

/// 追加一条纹理四边形。
#[no_mangle]
pub extern "C" fn gx_gpu_tex(dev: *mut GxGpuDev, t: *const CTex) {
    let Some(dev) = (unsafe { dev.as_mut() }) else { return };
    let Some(t) = (unsafe { t.as_ref() }) else { return };
    dev.frame.prims.push(crate::Prim::Tex(crate::Tex {
        rect: t.rect, clip: t.clip, id: t.id, uv: t.uv,
        radius: t.radius, alpha: t.alpha,
    }));
}

/// 结束一帧：渲染并呈现。返回 0=成功，1=句柄无效。
/// GPU 构建：present 到 surface。CPU 构建：光栅到像素缓冲，
/// 随后可用 gx_gpu_read_rgba 取回。
#[no_mangle]
pub extern "C" fn gx_gpu_end(dev: *mut GxGpuDev) -> i32 {
    let Some(dev) = (unsafe { dev.as_mut() }) else { return 1 };
    match &mut dev.inner {
        #[cfg(feature = "gpu")]
        Backend::Wgpu(s) => s.render_present(&dev.frame),
        #[cfg(feature = "cpu")]
        Backend::Cpu(cpu, out) => {
            *out = Some(cpu.render(&dev.frame));
            0
        }
    }
}

/// 取回 CPU 后端最近一帧的像素（RGBA8 预乘）。GPU 构建恒返回 2（像素
/// 直接呈现到屏幕，不回读）。缓冲不足返回 -1；无帧返回 3。
#[no_mangle]
pub extern "C" fn gx_gpu_read_rgba(dev: *mut GxGpuDev, out: *mut u8, cap: i32) -> i32 {
    let Some(dev) = (unsafe { dev.as_mut() }) else { return 1 };
    #[cfg(feature = "cpu")]
    {
        let Backend::Cpu(_, Some(pm)) = &mut dev.inner else { return 3 };
        let need = pm.data().len();
        if (cap as usize) < need {
            return -1;
        }
        unsafe {
            std::ptr::copy_nonoverlapping(pm.data().as_ptr(), out, need);
        }
        0
    }
    #[cfg(not(feature = "cpu"))]
    {
        let _ = (dev, out, cap);
        2
    }
}

// ---------- 测试：C ABI 数据面纯逻辑（无窗口依赖） ----------

#[cfg(test)]
mod tests {
    use super::*;

    fn cquad() -> CQuad {
        CQuad {
            rect: [0.0, 0.0, 10.0, 10.0],
            clip: [0.0, 0.0, 0.0, 0.0],
            color: [255.0, 0.0, 0.0, 255.0],
            radius: 2.0,
            border: 0.0,
            dir: [0.0, 0.0],
            stops: [0.0; 40],
            stop_count: 0,
        }
    }

    #[test]
    fn cquad_layout_matches_documented_c_struct() {
        // gx_gpu.h 与 Go 侧按此偏移布局，任何变化必须同步三处。
        assert_eq!(std::mem::size_of::<CQuad>(), 57 * 4); // rect4+clip4+color4+radius+border+dir2+stops40+count
        assert_eq!(std::mem::size_of::<CTex>(), 15 * 4); // rect4+clip4+id+uv4+radius+alpha
        let q = cquad();
        assert_eq!(q.rect, [0.0, 0.0, 10.0, 10.0]);
    }
}
