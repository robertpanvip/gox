//! GX 独立 GPU 渲染管线（quad 管线）。
//!
//! 架构（三条硬约束）：
//!
//! 1. **GPU 优先**：默认构建（feature `gpu`）走 wgpu（Vulkan/Metal/DX12/GL），
//!    同一份 WGSL shader 三端统一。
//! 2. **CPU 接管同一套 API（打包期选型，非运行时检测）**：绘制指令统一表达为
//!    [`GpuFrame`]（GPU 形状的纯数据模型）。feature `cpu` 构建 tiny-skia 软光栅
//!    实现，feature `gpu` 构建 wgpu 实现——两套实现**不同时编译**（包体积各自
//!    最小），由打包命令选定：
//!      - GPU 版：`cargo build`（默认）
//!      - CPU 版：`cargo build --no-default-features --features cpu`
//!    API（GpuFrame / C ABI）不随 feature 变化，调用方代码零改动。
//!    GPU 构建下适配器不可用直接报错，不静默回退。
//! 3. **独立抽离**：本 crate 不依赖 GX 场景/协议/布局，只消费 `GpuFrame`。
//!    产物 rlib（Rust 后端直用）+ staticlib（Go CGO / Android NDK 经 C ABI）。
//!
//! 绘制模型：
//!   - [`Prim::Quad`]：SDF 圆角矩形（纯色 / 线性渐变 / 描边环），抗锯齿由
//!     shader/SDF 软边给出，CPU 后端用 tiny-skia 路径等效实现；
//!   - [`Prim::Tex`]：纹理四边形（图片/视频/canvas 光栅），UV 窗口采样 +
//!     圆角遮罩，源数据按预乘 alpha 处理（tiny-skia Pixmap 口径）；
//!   - 裁剪：quad 几何已由上层与裁剪矩形求交（与旧 CPU 管线同口径），
//!     GPU 后端另设 scissor 硬裁软边溢出，CPU 后端直接按交集绘制；
//!   - 混合：全链路预乘 alpha、gamma 空间混合（对齐 tiny-skia/Skia raster）。
//!
//! 阴影不在后端实现：上层按旧 6 层近似算法分解成多条 Quad（与 Skia 端
//! paintShadow 同算法），两个后端自然一致。

#[cfg(feature = "gpu")]
pub mod wgpu;
#[cfg(feature = "gpu")]
pub use wgpu::QuadGpu;
#[cfg(feature = "cpu")]
pub mod cpu;
#[cfg(feature = "cpu")]
pub use cpu::CpuBackend;
pub mod capi;

/// 渐变色标最大数（uniform 表按 8 槽/渐变分配）。
pub const MAX_STOPS: usize = 8;
/// 渐变 uniform 表容量（每帧最多引用多少个不同渐变）。
pub const MAX_GRADIENTS: usize = 64;

/// 一帧的绘制指令集合（GPU 形状的纯数据模型）。
#[derive(Default, Debug, Clone)]
pub struct GpuFrame {
    pub w: u32,
    pub h: u32,
    /// 清屏色 RGBA（0..255）。
    pub clear: [f32; 4],
    /// 本帧纹理上传（按 id + key 去重，两个后端共用同一去重口径）。
    pub uploads: Vec<TexUpload>,
    /// 绘制图元，按文档序绘制（后画覆盖先画）。
    pub prims: Vec<Prim>,
}

/// 纹理上传条目。`key` 由调用方给定（如源 Arc 指针 / 视频帧序号），
/// 后端据此跳过内容未变的重复上传。
#[derive(Debug, Clone)]
pub struct TexUpload {
    pub id: u32,
    pub key: u64,
    pub w: u32,
    pub h: u32,
    /// RGBA8，预乘与否由 `premul` 声明（tiny-skia Pixmap 数据为预乘）。
    pub data: std::sync::Arc<Vec<u8>>,
    pub premul: bool,
}

/// 单个图元。
#[derive(Debug, Clone)]
pub enum Prim {
    Quad(Quad),
    Tex(Tex),
}

impl Prim {
    /// 裁剪矩形 [x, y, w, h]；w<=0 表示无裁剪。
    pub fn clip(&self) -> [f32; 4] {
        match self {
            Prim::Quad(q) => q.clip,
            Prim::Tex(t) => t.clip,
        }
    }
}

/// SDF 圆角矩形：纯色填充 / 线性渐变 / 描边环三选一。
/// - `border > 0`：只画描边环（描边与填充由上层拆成两条 Quad，保证
///   「先填充后描边」与旧 tiny-skia 管线同序）；
/// - `grad`：线性渐变覆盖 `color`，方向 (dx, dy) 为单位向量（CSS 角度
///   语义：0°向上=（0,-1），90°向右=（1,0）），渐变线过盒子中心。
#[derive(Debug, Clone)]
pub struct Quad {
    /// 目标矩形 [x, y, w, h]（已与裁剪求交后的几何）。
    pub rect: [f32; 4],
    /// 裁剪矩形 [x, y, w, h]；w<=0 表示无裁剪。
    pub clip: [f32; 4],
    /// 颜色 RGBA（0..255）。
    pub color: [f32; 4],
    pub radius: f32,
    /// 描边宽度；>0 时本 Quad 表示描边环（带心路径径，与 tiny-skia
    /// stroke_path 同几何）。
    pub border: f32,
    pub grad: Option<Grad>,
}

/// 线性渐变。stops 的 pos 已归一化 0..1 且单调不减（上层归一化），
/// 每项 [pos, r, g, b, a]，0..255。上限 [`MAX_STOPS`]。
#[derive(Debug, Clone)]
pub struct Grad {
    pub dx: f32,
    pub dy: f32,
    pub stops: Vec<[f32; 5]>,
}

/// 纹理四边形。源数据经 [`TexUpload`] 按 `id` 注册；UV 窗口为源纹理
/// 像素坐标（支持 object-fit 的子矩形采样）；圆角遮罩按 `radius`。
#[derive(Debug, Clone)]
pub struct Tex {
    /// 目标矩形（已与裁剪求交）。
    pub rect: [f32; 4],
    pub clip: [f32; 4],
    pub id: u32,
    /// UV 窗口 [u0, v0, u1, v1]（源纹理像素坐标）。
    pub uv: [f32; 4],
    pub radius: f32,
    /// 整体透明度 0..1。
    pub alpha: f32,
}

impl GpuFrame {
    pub fn new(w: u32, h: u32, clear: [f32; 4]) -> Self {
        GpuFrame { w, h, clear, uploads: Vec::new(), prims: Vec::new() }
    }

    /// 追加一条纯色圆角矩形。
    pub fn quad(&mut self, rect: [f32; 4], clip: [f32; 4], color: [f32; 4], radius: f32, border: f32) {
        self.prims.push(Prim::Quad(Quad { rect, clip, color, radius, border, grad: None }));
    }

    /// 追加一条线性渐变圆角矩形。
    pub fn gradient(&mut self, rect: [f32; 4], clip: [f32; 4], radius: f32, grad: Grad) {
        self.prims.push(Prim::Quad(Quad {
            rect, clip,
            color: [255.0, 255.0, 255.0, 255.0],
            radius, border: 0.0,
            grad: Some(grad),
        }));
    }

    /// 追加一条纹理四边形。
    pub fn tex(&mut self, rect: [f32; 4], clip: [f32; 4], id: u32, uv: [f32; 4], radius: f32, alpha: f32) {
        self.prims.push(Prim::Tex(Tex { rect, clip, id, uv, radius, alpha }));
    }
}

/// CSS 角度（0°向上、顺时针）转方向向量（屏幕 y 向下）。
/// 与 Go 端 paintGradientBackgroundShader / 旧 tiny-skia gradient_shader 同口径。
pub fn css_angle_to_dir(angle_deg: f32) -> (f32, f32) {
    let rad = angle_deg.to_radians();
    (rad.sin(), -rad.cos())
}

/// 渐变线长度：盒子宽高在方向上的投影绝对值之和的一半。
/// 与 Go/Skia 端同算法——渐变线穿过中心，两端各延伸 len。
pub fn gradient_line_len(w: f32, h: f32, dx: f32, dy: f32) -> f32 {
    (w * dx.abs() + h * dy.abs()) * 0.5
}

/// 把上层 ClipRect（Option）转成 GpuFrame 裁剪矩形；None → w=0（无裁剪）。
pub fn clip_of(c: Option<(f32, f32, f32, f32)>) -> [f32; 4] {
    match c {
        None => [0.0, 0.0, 0.0, 0.0],
        Some((x, y, w, h)) => [x, y, w, h],
    }
}

// ---------- 测试：数据模型纯逻辑（两套后端共享语义的锚点） ----------

#[cfg(test)]
mod tests {
    use super::*;

    fn frame() -> GpuFrame {
        let mut f = GpuFrame::new(100, 80, [10.0, 20.0, 30.0, 255.0]);
        f.quad([0.0, 0.0, 50.0, 40.0], [0.0, 0.0, 0.0, 0.0], [255.0, 0.0, 0.0, 255.0], 4.0, 0.0);
        f
    }

    #[test]
    fn quad_prim_carries_clip() {
        let f = frame();
        match &f.prims[0] {
            Prim::Quad(q) => {
                assert_eq!(q.rect, [0.0, 0.0, 50.0, 40.0]);
                assert_eq!(q.radius, 4.0);
                assert_eq!(q.color[0], 255.0);
            }
            _ => panic!("expect quad"),
        }
    }

    #[test]
    fn tex_prim_carries_uv_and_alpha() {
        let mut f = GpuFrame::new(10, 10, [0.0; 4]);
        f.tex([0.0, 0.0, 8.0, 8.0], [0.0, 0.0, 0.0, 0.0], 7, [0.0, 0.0, 4.0, 4.0], 2.0, 0.5);
        match &f.prims[0] {
            Prim::Tex(t) => {
                assert_eq!(t.id, 7);
                assert_eq!(t.uv, [0.0, 0.0, 4.0, 4.0]);
                assert!((t.alpha - 0.5).abs() < 1e-6);
            }
            _ => panic!("expect tex"),
        }
    }

    #[test]
    fn clip_none_is_zero_width() {
        assert_eq!(clip_of(None), [0.0, 0.0, 0.0, 0.0]);
        assert_eq!(clip_of(Some((1.0, 2.0, 3.0, 4.0))), [1.0, 2.0, 3.0, 4.0]);
        let f = frame();
        // 无裁剪的 quad：clip() 返回 w=0，后端按"无裁剪"处理。
        assert_eq!(f.prims[0].clip(), [0.0, 0.0, 0.0, 0.0]);
    }

    #[test]
    fn css_angle_semantics_match_go_side() {
        // 0° 向上；90° 向右；180° 向下（与 Go paintGradientBackgroundShader 同口径）。
        let (dx, dy) = css_angle_to_dir(0.0);
        assert!(dx.abs() < 1e-6 && (dy + 1.0).abs() < 1e-6);
        let (dx, dy) = css_angle_to_dir(90.0);
        assert!((dx - 1.0).abs() < 1e-6 && dy.abs() < 1e-6);
        let (dx, dy) = css_angle_to_dir(180.0);
        assert!(dx.abs() < 1e-6 && (dy - 1.0).abs() < 1e-6);
    }

    #[test]
    fn gradient_line_matches_projection_formula() {
        // 竖直渐变在 100x50 盒上：len = (100*0 + 50*1)/2 = 25。
        let len = gradient_line_len(100.0, 50.0, 0.0, -1.0);
        assert!((len - 25.0).abs() < 1e-6);
    }
}
