//! CPU 接管后端：tiny-skia 软光栅实现与 Wgpu 后端完全相同的 GpuFrame 语义。
//!
//! 设计口径（与 GPU 后端一致，保证后端切换视觉语义不变）：
//!   - gamma 空间预乘 alpha 混合（tiny-skia Pixmap 原生即此口径）；
//!   - 圆角矩形走 tiny-skia 路径 + 抗锯齿填充（GPU 端为 SDF 软边，1px 级
//!     差异可接受，形状语义相同）；
//!   - 渐变与 Go/Skia 端同算法：CSS 角度方向向量 + 投影渐变线；
//!   - 裁剪按矩形交集硬裁（GPU 端为 scissor，同为硬边）；
//!   - 纹理按预乘处理，UV 窗口最近邻取样（GPU 端为线性取样，微差可接受）。

use crate::{GpuFrame, Prim, Quad, Tex, MAX_STOPS};

/// CPU 后端：无 GPU 依赖，纯软光栅。纹理缓存按 (id, key) 去重。
pub struct CpuBackend {
    textures: std::collections::HashMap<u32, (tiny_skia::Pixmap, u64)>,
}

impl Default for CpuBackend {
    fn default() -> Self {
        Self::new()
    }
}

impl CpuBackend {
    pub fn new() -> Self {
        CpuBackend { textures: std::collections::HashMap::new() }
    }

    /// 渲染一帧到像素缓冲（RGBA8 预乘）。供 C ABI / 无 GPU 环境呈现，
    /// 也供 Rust 单测做像素级断言。
    pub fn render(&mut self, frame: &GpuFrame) -> tiny_skia::Pixmap {
        let mut pm = tiny_skia::Pixmap::new(frame.w.max(1), frame.h.max(1))
            .expect("pixmap alloc");
        pm.fill(tiny_skia::Color::from_rgba8(
            frame.clear[0] as u8, frame.clear[1] as u8,
            frame.clear[2] as u8, frame.clear[3] as u8,
        ));
        self.apply(&mut pm, frame);
        pm
    }

    /// 把一帧绘制到已有画布（GPU 不可用时 Rust 后端把结果 blit 上屏）。
    pub fn apply(&mut self, pm: &mut tiny_skia::Pixmap, frame: &GpuFrame) {
        self.sync_textures(frame);
        for prim in &frame.prims {
            match prim {
                Prim::Quad(q) => self.paint_quad(pm, q),
                Prim::Tex(t) => self.paint_tex(pm, t),
            }
        }
    }

    fn sync_textures(&mut self, frame: &GpuFrame) {
        for up in &frame.uploads {
            let hit = self.textures.get(&up.id).map(|(_, k)| *k == up.key).unwrap_or(false);
            if hit {
                continue;
            }
            if let Some(size) = tiny_skia::IntSize::from_wh(up.w.max(1), up.h.max(1)) {
                if let Some(pm) = tiny_skia::Pixmap::from_vec((*up.data).clone(), size) {
                    self.textures.insert(up.id, (pm, up.key));
                }
            }
        }
        // 淘汰本帧未引用的纹理（防泄漏）。
        let live: std::collections::HashSet<u32> =
            frame.uploads.iter().map(|u| u.id).collect();
        self.textures.retain(|id, _| live.contains(id));
    }

    fn paint_quad(&self, pm: &mut tiny_skia::Pixmap, q: &Quad) {
        use tiny_skia::{Color, FillRule, Paint, Stroke, Transform};
        let [x, y, w, h] = q.rect;
        if w <= 0.0 || h <= 0.0 {
            return;
        }
        let Some(path) = round_rect_path(x, y, w, h, q.radius) else { return };
        if let Some(g) = &q.grad {
            // GradientStop 字段是 pub(crate)：先按 pos 排序源数据再构造。
            let mut sorted = g.stops.iter().take(MAX_STOPS).collect::<Vec<_>>();
            if sorted.is_empty() {
                return;
            }
            sorted.sort_by(|a, b| a[0].partial_cmp(&b[0]).unwrap_or(std::cmp::Ordering::Equal));
            let stops: Vec<tiny_skia::GradientStop> = sorted.iter().map(|s| {
                tiny_skia::GradientStop::new(
                    s[0].clamp(0.0, 1.0),
                    Color::from_rgba8(s[1] as u8, s[2] as u8, s[3] as u8, s[4] as u8),
                )
            }).collect();
            let (dx, dy) = (g.dx, g.dy);
            if dx == 0.0 && dy == 0.0 {
                return; // 无方向：上层语义保证不会出现
            }
            let len = crate::gradient_line_len(w, h, dx, dy);
            let cx = x + w / 2.0;
            let cy = y + h / 2.0;
            let start = tiny_skia::Point::from_xy(cx - dx * len, cy - dy * len);
            let end = tiny_skia::Point::from_xy(cx + dx * len, cy + dy * len);
            if let Some(shader) = tiny_skia::LinearGradient::new(
                start, end, stops, tiny_skia::SpreadMode::Pad, Transform::identity(),
            ) {
                let mut p = Paint::default();
                p.shader = shader;
                p.anti_alias = true;
                pm.fill_path(&path, &p, FillRule::Winding, Transform::identity(), None);
            }
            return;
        }
        let a = q.color[3].clamp(0.0, 255.0) as u8;
        if a == 0 {
            return;
        }
        let mut p = Paint::default();
        p.set_color(Color::from_rgba8(q.color[0] as u8, q.color[1] as u8, q.color[2] as u8, a));
        p.anti_alias = true;
        if q.border > 0.0 {
            // 描边环：带心路径径（与 tiny-skia stroke_path / GPU band 同几何）。
            pm.stroke_path(&path, &p, &Stroke { width: q.border, ..Default::default() },
                Transform::identity(), None);
        } else {
            pm.fill_path(&path, &p, FillRule::Winding, Transform::identity(), None);
        }
    }

    fn paint_tex(&mut self, pm: &mut tiny_skia::Pixmap, t: &Tex) {
        let Some((src, _)) = self.textures.get(&t.id) else { return };
        let [x, y, w, h] = t.rect;
        if w <= 0.0 || h <= 0.0 {
            return;
        }
        let [u0, v0, u1, v1] = t.uv;
        let uw = (u1 - u0).max(1.0);
        let vh = (v1 - v0).max(1.0);
        // 最近邻取 UV 窗口（整数化）；窗口裁进源图范围内。
        let ix0 = (u0.max(0.0) as u32).min(src.width().saturating_sub(1));
        let iy0 = (v0.max(0.0) as u32).min(src.height().saturating_sub(1));
        let uw_i = (uw as u32).clamp(1, src.width() - ix0);
        let vh_i = (vh as u32).clamp(1, src.height() - iy0);
        let Some(win) = window_pixmap(src, ix0, iy0, uw_i, vh_i) else { return };

        let sx = w / uw_i as f32;
        let sy = h / vh_i as f32;
        // 缩放到目标尺寸的临时画布 + 圆角遮罩（遮罩坐标系与临时画布一致，
        // 再整体贴回主画布——修正旧实现 mask 相对坐标错位的问题）。
        let dw = (w.max(1.0) as u32).min(4096);
        let dh = (h.max(1.0) as u32).min(4096);
        let Some(mut dst) = tiny_skia::Pixmap::new(dw, dh) else { return };
        dst.draw_pixmap(0, 0, win.as_ref(), &tiny_skia::PixmapPaint::default(),
            tiny_skia::Transform::from_row(sx, 0.0, 0.0, sy, 0.0, 0.0), None);
        if t.radius > 0.0 {
            if let Some(path) = round_rect_path(0.0, 0.0, dw as f32, dh as f32, t.radius) {
                let mut mask = tiny_skia::Mask::new(dw, dh).expect("mask alloc");
                mask.fill_path(&path, tiny_skia::FillRule::Winding, true,
                    tiny_skia::Transform::identity());
                // 用遮罩把 dst 的圆角外像素清零（预乘域直接乘 alpha）。
                let mask_data = mask.data();
                for (px, m) in dst.data_mut().chunks_exact_mut(4).zip(mask_data.chunks_exact(1)) {
                    let ma = m[0] as u32;
                    px[0] = (px[0] as u32 * ma / 255) as u8;
                    px[1] = (px[1] as u32 * ma / 255) as u8;
                    px[2] = (px[2] as u32 * ma / 255) as u8;
                    px[3] = (px[3] as u32 * ma / 255) as u8;
                }
            }
        }
        scale_alpha(&mut dst, t.alpha.clamp(0.0, 1.0));
        pm.draw_pixmap(x as i32, y as i32, dst.as_ref(),
            &tiny_skia::PixmapPaint::default(), tiny_skia::Transform::identity(), None);
    }
}

/// 从源 Pixmap 截取整数像素窗口（预乘数据按行拷贝）。
fn window_pixmap(src: &tiny_skia::Pixmap, x: u32, y: u32, w: u32, h: u32) -> Option<tiny_skia::Pixmap> {
    let mut out = tiny_skia::Pixmap::new(w, h)?;
    let sstride = (src.width() * 4) as usize;
    let dstride = (w * 4) as usize;
    let data = src.data();
    for row in 0..h as usize {
        let s = (y as usize + row) * sstride + x as usize * 4;
        let d = row * dstride;
        out.data_mut()[d..d + dstride].copy_from_slice(&data[s..s + dstride]);
    }
    Some(out)
}

/// 等比压暗预乘 alpha（整体透明度）。
fn scale_alpha(pm: &mut tiny_skia::Pixmap, a: f32) {
    if a >= 1.0 {
        return;
    }
    let q = (a * 256.0) as u32;
    for px in pm.data_mut().chunks_exact_mut(4) {
        px[0] = (px[0] as u32 * q / 256) as u8;
        px[1] = (px[1] as u32 * q / 256) as u8;
        px[2] = (px[2] as u32 * q / 256) as u8;
        px[3] = (px[3] as u32 * q / 256) as u8;
    }
}

/// 圆角矩形路径（二次贝塞尔角近似，与旧 CPU 管线逐字相同——保证布局/
/// 视觉口径不变）。
pub fn round_rect_path(x: f32, y: f32, w: f32, h: f32, r: f32) -> Option<tiny_skia::Path> {
    use tiny_skia::PathBuilder;
    let r = r.min(w / 2.0).min(h / 2.0);
    let mut pb = PathBuilder::new();
    pb.move_to(x + r, y);
    pb.line_to(x + w - r, y);
    pb.quad_to(x + w, y, x + w, y + r);
    pb.line_to(x + w, y + h - r);
    pb.quad_to(x + w, y + h, x + w - r, y + h);
    pb.line_to(x + r, y + h);
    pb.quad_to(x, y + h, x, y + h - r);
    pb.line_to(x, y + r);
    pb.quad_to(x, y, x + r, y);
    pb.close();
    pb.finish()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::GpuFrame;

    #[test]
    fn solid_quad_pixel() {
        let mut b = CpuBackend::new();
        let mut f = GpuFrame::new(8, 8, [0.0, 0.0, 0.0, 255.0]);
        f.quad([2.0, 2.0, 4.0, 4.0], [0.0, 0.0, 0.0, 0.0], [255.0, 0.0, 0.0, 255.0], 0.0, 0.0);
        let pm = b.render(&f);
        let px = pm.pixel(4, 4).unwrap();
        assert_eq!(px.red(), 255);
        assert_eq!(px.green(), 0);
        // 盒外是清屏色。
        let out = pm.pixel(0, 0).unwrap();
        assert_eq!(out.red(), 0);
    }

    #[test]
    fn rounded_corner_alpha_zero() {
        let mut b = CpuBackend::new();
        let mut f = GpuFrame::new(8, 8, [0.0, 0.0, 0.0, 255.0]);
        // 8x8 全尺寸 radius=4 的圆角矩形：四角像素应透明。
        f.quad([0.0, 0.0, 8.0, 8.0], [0.0, 0.0, 0.0, 0.0], [255.0, 255.0, 255.0, 255.0], 4.0, 0.0);
        let pm = b.render(&f);
        let corner = pm.pixel(0, 0).unwrap();
        assert_eq!(corner.red(), 0, "圆角外像素应保持清屏色（黑）");
        let center = pm.pixel(4, 4).unwrap();
        assert_eq!(center.alpha(), 255);
    }

    #[test]
    fn vertical_gradient_css_zero_deg_bottom_is_first_stop() {
        let mut b = CpuBackend::new();
        let mut f = GpuFrame::new(10, 100, [0.0, 0.0, 0.0, 255.0]);
        // CSS 0° = to top：stop0 在底部，stop1 在顶部（与 Go/Skia 端同口径）。
        f.gradient(
            [0.0, 0.0, 10.0, 100.0], [0.0, 0.0, 0.0, 0.0], 0.0,
            crate::Grad {
                dx: 0.0, dy: -1.0,
                stops: vec![
                    [0.0, 255.0, 0.0, 0.0, 255.0],
                    [1.0, 0.0, 0.0, 255.0, 255.0],
                ],
            },
        );
        let pm = b.render(&f);
        let bot = pm.pixel(5, 97).unwrap();
        assert!(bot.red() > 200, "底部应是 stop0（红）: {:?}", bot);
        let top = pm.pixel(5, 2).unwrap();
        assert!(top.blue() > 200, "顶部应是 stop1（蓝）: {:?}", top);
        let mid = pm.pixel(5, 50).unwrap();
        assert!(mid.red() > 100 && mid.blue() > 100, "中点应混出红蓝中间色: {:?}", mid);
    }

    #[test]
    fn border_band_leaves_interior() {
        let mut b = CpuBackend::new();
        let mut f = GpuFrame::new(20, 20, [0.0, 0.0, 0.0, 255.0]);
        // 只画 2px 描边环：中心应保持清屏色。
        f.quad([2.0, 2.0, 16.0, 16.0], [0.0, 0.0, 0.0, 0.0], [0.0, 255.0, 0.0, 255.0], 0.0, 2.0);
        let pm = b.render(&f);
        let center = pm.pixel(10, 10).unwrap();
        assert_eq!(center.green(), 0, "描边环不应填充内部");
        let edge = pm.pixel(2, 10).unwrap();
        assert!(edge.green() > 100, "边框带应着色: {:?}", edge);
        let inner = pm.pixel(5, 10).unwrap();
        assert_eq!(inner.green(), 0, "描边内不应着色");
    }

    #[test]
    fn texture_window_and_alpha() {
        let mut b = CpuBackend::new();
        let mut f = GpuFrame::new(16, 16, [0.0, 0.0, 0.0, 255.0]);
        // 源纹理 8x8 全红（预乘），UV 窗口取右下 4x4 绘到 4x4 区域。
        let mut data = vec![0u8; 8 * 8 * 4];
        for px in data.chunks_mut(4) {
            px.copy_from_slice(&[255, 0, 0, 255]);
        }
        f.uploads.push(crate::TexUpload {
            id: 1, key: 1, w: 8, h: 8,
            data: std::sync::Arc::new(data),
            premul: true,
        });
        f.tex([4.0, 4.0, 4.0, 4.0], [0.0, 0.0, 0.0, 0.0], 1, [4.0, 4.0, 8.0, 8.0], 0.0, 1.0);
        let pm = b.render(&f);
        let p1 = pm.pixel(6, 6).unwrap();
        assert_eq!(p1.red(), 255, "UV 窗口内应采样到红: {:?}", p1);
        let p0 = pm.pixel(0, 0).unwrap();
        assert_eq!(p0.red(), 0, "目标区外应保持清屏色");
    }

    #[test]
    fn texture_upload_dedup_by_key() {
        let mut b = CpuBackend::new();
        let mut f = GpuFrame::new(4, 4, [0.0; 4]);
        let data = vec![255u8; 4 * 4 * 4];
        f.uploads.push(crate::TexUpload {
            id: 5, key: 42, w: 4, h: 4,
            data: std::sync::Arc::new(data.clone()),
            premul: true,
        });
        f.uploads.push(crate::TexUpload {
            id: 5, key: 42, w: 4, h: 4,
            data: std::sync::Arc::new(data),
            premul: true,
        });
        // 同 key 重复上传应命中缓存不重建（间接验证：渲染仍正确）。
        f.tex([0.0, 0.0, 4.0, 4.0], [0.0, 0.0, 0.0, 0.0], 5, [0.0, 0.0, 4.0, 4.0], 0.0, 1.0);
        let pm = b.render(&f);
        assert_eq!(pm.pixel(1, 1).unwrap().red(), 255);
        assert!(b.textures.contains_key(&5));
    }

    #[test]
    fn texture_alpha_fades() {
        let mut b = CpuBackend::new();
        let mut f = GpuFrame::new(4, 4, [0.0, 0.0, 0.0, 255.0]);
        let data = vec![255u8; 4 * 4 * 4];
        f.uploads.push(crate::TexUpload {
            id: 9, key: 1, w: 4, h: 4,
            data: std::sync::Arc::new(data),
            premul: true,
        });
        f.tex([0.0, 0.0, 4.0, 4.0], [0.0, 0.0, 0.0, 0.0], 9, [0.0, 0.0, 4.0, 4.0], 0.0, 0.5);
        let pm = b.render(&f);
        // 预乘红 alpha=0.5：屏幕上红 ≈ 128（gamma 空间直接缩放）。
        let p = pm.pixel(2, 2).unwrap();
        assert!((110..=145).contains(&p.red()), "半透明红应≈128: {:?}", p);
    }
}
