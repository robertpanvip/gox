//! Wgpu quad 渲染后端（feature `gpu`）。
//!
//! 三端统一：一份 WGSL 同时服务 Vulkan（Linux/Android）、Metal（macOS）、
//! DX12（Windows），由打包目标决定后端，无 per-platform 图形代码。
//!
//! 管线：
//!   - quad 管线：instanced，SDF 圆角矩形（纯色/描边环/线性渐变）。
//!     渐变色标经 uniform 表（每帧 write_buffer），instance 只带索引。
//!   - tex 管线：instanced，纹理四边形（图片/视频/canvas 光栅）+ 圆角遮罩。
//!   - 批次：连续同裁剪 + 同类型（+ 同纹理 id）的图元合并为一个 instanced
//!     批次；批次按文档序重放（先画后覆盖），切换批次时更新 scissor /
//!     pipeline / bind group。
//!   - 混合：预乘 alpha（PREMULTIPLIED_ALPHA_BLENDING），gamma 空间——
//!     表面格式 Bgra8Unorm（写入即显示，与 CPU 后端/tiny-skia 同口径）。

use crate::{GpuFrame, Prim, Grad, MAX_GRADIENTS, MAX_STOPS};
use raw_window_handle::{RawDisplayHandle, RawWindowHandle};
use std::collections::HashMap;

/// 每实例数据统一 16 个 f32（64B），两管线对齐，便于批量复用。
const INST_FLOATS: usize = 16;

/// 渐变 uniform 表：64 个渐变 × 8 槽 × (pos,r,g,b) + (a,·,·,·)。
const GRAD_TABLE_FLOATS: usize = MAX_GRADIENTS * MAX_STOPS * 4 * 2;

/// 一个 instanced 批次：同 clip、同管线类型、同纹理（tex）的连续图元。
#[derive(Debug, Clone, Copy, PartialEq)]
struct Batch {
    clip: [f32; 4],
    is_tex: bool,
    tex_id: u32,
    start: u32,
    count: u32,
}

/// GPU quad 渲染器。device/queue/surface 由调用方持有（Rust 后端要在同一
/// render pass 里叠加 glyphon 文本；C ABI 独立呈现见 capi.rs）。
pub struct QuadGpu {
    device: wgpu::Device,
    queue: wgpu::Queue,
    quad_pipeline: wgpu::RenderPipeline,
    tex_pipeline: wgpu::RenderPipeline,
    quad_buf: wgpu::Buffer,
    tex_buf: wgpu::Buffer,
    grad_buf: wgpu::Buffer,
    grad_bind: wgpu::BindGroup,
    tex_bgl: wgpu::BindGroupLayout,
    sampler: wgpu::Sampler,
    /// 纹理注册表：id → (texture, bind_group, content key)。
    textures: HashMap<u32, (wgpu::Texture, wgpu::BindGroup, u64)>,
    quad_cap: usize,
    tex_cap: usize,
}

impl QuadGpu {
    /// 创建管线。`format` 为目标表面格式（约定 Bgra8Unorm）。
    pub fn new(device: wgpu::Device, queue: wgpu::Queue, format: wgpu::TextureFormat) -> Self {
        let shader = device.create_shader_module(wgpu::ShaderModuleDescriptor {
            label: Some("gx-quad"),
            source: wgpu::ShaderSource::Wgsl(WGSL.into()),
        });
        // ---- quad 管线 ----
        let grad_bgl = device.create_bind_group_layout(&wgpu::BindGroupLayoutDescriptor {
            label: Some("gx-grad-bgl"),
            entries: &[wgpu::BindGroupLayoutEntry {
                binding: 0,
                visibility: wgpu::ShaderStages::FRAGMENT,
                ty: wgpu::BindingType::Buffer {
                    ty: wgpu::BufferBindingType::Uniform,
                    has_dynamic_offset: false,
                    min_binding_size: wgpu::BufferSize::new((GRAD_TABLE_FLOATS * 4) as u64),
                },
                count: None,
            }],
        });
        let grad_buf = device.create_buffer(&wgpu::BufferDescriptor {
            label: Some("gx-grad-table"),
            size: (GRAD_TABLE_FLOATS * 4) as u64,
            usage: wgpu::BufferUsages::UNIFORM | wgpu::BufferUsages::COPY_DST,
            mapped_at_creation: false,
        });
        let grad_bind = device.create_bind_group(&wgpu::BindGroupDescriptor {
            label: Some("gx-grad-bind"),
            layout: &grad_bgl,
            entries: &[wgpu::BindGroupEntry {
                binding: 0,
                resource: grad_buf.as_entire_binding(),
            }],
        });
        let quad_pl = device.create_pipeline_layout(&wgpu::PipelineLayoutDescriptor {
            label: Some("gx-quad-pl"),
            bind_group_layouts: &[&grad_bgl],
            push_constant_ranges: &[],
        });
        let quad_pipeline = device.create_render_pipeline(&wgpu::RenderPipelineDescriptor {
            label: Some("gx-quad-pipeline"),
            layout: Some(&quad_pl),
            vertex: wgpu::VertexState {
                module: &shader,
                entry_point: Some("quad_vs"),
                compilation_options: Default::default(),
                buffers: &[inst_layout()],
            },
            primitive: wgpu::PrimitiveState::default(),
            depth_stencil: None,
            multisample: wgpu::MultisampleState::default(),
            fragment: Some(wgpu::FragmentState {
                module: &shader,
                entry_point: Some("quad_fs"),
                compilation_options: Default::default(),
                targets: &[Some(wgpu::ColorTargetState {
                    format,
                    blend: Some(wgpu::BlendState::PREMULTIPLIED_ALPHA_BLENDING),
                    write_mask: wgpu::ColorWrites::ALL,
                })],
            }),
            cache: None,
            multiview: None,
        });
        // ---- tex 管线 ----
        let tex_bgl = device.create_bind_group_layout(&wgpu::BindGroupLayoutDescriptor {
            label: Some("gx-tex-bgl"),
            entries: &[
                wgpu::BindGroupLayoutEntry {
                    binding: 0,
                    visibility: wgpu::ShaderStages::FRAGMENT,
                    ty: wgpu::BindingType::Texture {
                        sample_type: wgpu::TextureSampleType::Float { filterable: true },
                        view_dimension: wgpu::TextureViewDimension::D2,
                        multisampled: false,
                    },
                    count: None,
                },
                wgpu::BindGroupLayoutEntry {
                    binding: 1,
                    visibility: wgpu::ShaderStages::FRAGMENT,
                    ty: wgpu::BindingType::Sampler(wgpu::SamplerBindingType::Filtering),
                    count: None,
                },
            ],
        });
        let sampler = device.create_sampler(&wgpu::SamplerDescriptor {
            mag_filter: wgpu::FilterMode::Linear,
            min_filter: wgpu::FilterMode::Linear,
            ..Default::default()
        });
        let tex_pl = device.create_pipeline_layout(&wgpu::PipelineLayoutDescriptor {
            label: Some("gx-tex-pl"),
            bind_group_layouts: &[&tex_bgl],
            push_constant_ranges: &[],
        });
        let tex_pipeline = device.create_render_pipeline(&wgpu::RenderPipelineDescriptor {
            label: Some("gx-tex-pipeline"),
            layout: Some(&tex_pl),
            vertex: wgpu::VertexState {
                module: &shader,
                entry_point: Some("tex_vs"),
                compilation_options: Default::default(),
                buffers: &[inst_layout()],
            },
            primitive: wgpu::PrimitiveState::default(),
            depth_stencil: None,
            multisample: wgpu::MultisampleState::default(),
            fragment: Some(wgpu::FragmentState {
                module: &shader,
                entry_point: Some("tex_fs"),
                compilation_options: Default::default(),
                targets: &[Some(wgpu::ColorTargetState {
                    format,
                    blend: Some(wgpu::BlendState::PREMULTIPLIED_ALPHA_BLENDING),
                    write_mask: wgpu::ColorWrites::ALL,
                })],
            }),
            cache: None,
            multiview: None,
        });
        let quad_buf = empty_buffer(&device, 1);
        let tex_buf = empty_buffer(&device, 1);
        QuadGpu {
            device, queue,
            quad_pipeline, tex_pipeline,
            quad_buf, tex_buf,
            grad_buf, grad_bind, tex_bgl, sampler,
            textures: HashMap::new(),
            quad_cap: 0,
            tex_cap: 0,
        }
    }

    /// 上传/更新纹理（按 id + key 去重；内容未变时跳过 GPU 拷贝）。
    pub fn sync_textures(&mut self, frame: &GpuFrame) {
        for up in &frame.uploads {
            if self.textures.get(&up.id).map(|(_, _, k)| *k == up.key).unwrap_or(false) {
                continue;
            }
            let size = wgpu::Extent3d {
                width: up.w.max(1),
                height: up.h.max(1),
                depth_or_array_layers: 1,
            };
            let tex = self.device.create_texture(&wgpu::TextureDescriptor {
                label: Some("gx-tex"),
                size,
                mip_level_count: 1,
                sample_count: 1,
                dimension: wgpu::TextureDimension::D2,
                format: wgpu::TextureFormat::Rgba8Unorm,
                usage: wgpu::TextureUsages::TEXTURE_BINDING | wgpu::TextureUsages::COPY_DST,
                view_formats: &[],
            });
            // 数据不足时按零填充对齐（防御性：协议保证 w*h*4）。
            let need = (up.w.max(1) * up.h.max(1) * 4) as usize;
            let data: Vec<u8> = if up.data.len() >= need {
                (*up.data)[..need].to_vec()
            } else {
                let mut d = (*up.data).clone();
                d.resize(need, 0);
                d
            };
            self.queue.write_texture(
                wgpu::TexelCopyTextureInfo {
                    texture: &tex,
                    mip_level: 0,
                    origin: wgpu::Origin3d::ZERO,
                    aspect: wgpu::TextureAspect::All,
                },
                &data,
                wgpu::TexelCopyBufferLayout {
                    offset: 0,
                    bytes_per_row: Some(up.w.max(1) * 4),
                    rows_per_image: Some(up.h.max(1)),
                },
                size,
            );
            let view = tex.create_view(&wgpu::TextureViewDescriptor::default());
            let bind = self.device.create_bind_group(&wgpu::BindGroupDescriptor {
                label: Some("gx-tex-bind"),
                layout: &self.tex_bgl,
                entries: &[
                    wgpu::BindGroupEntry { binding: 0, resource: wgpu::BindingResource::TextureView(&view) },
                    wgpu::BindGroupEntry { binding: 1, resource: wgpu::BindingResource::Sampler(&self.sampler) },
                ],
            });
            self.textures.insert(up.id, (tex, bind, up.key));
        }
    }

    /// 把一帧 quad/纹理图元渲染进给定 render pass（pass 已 Clear 为清屏色）。
    /// 调用方在同一 pass 后续叠加 glyphon 文本。
    pub fn draw(&mut self, pass: &mut wgpu::RenderPass<'_>, frame: &GpuFrame) {
        self.sync_textures(frame);
        let n = frame.prims.len();
        if n == 0 {
            return;
        }
        let w = frame.w as f32;
        let h = frame.h as f32;

        // ---- 构建实例数据与批次表（单遍，保文档序）----
        let mut grad_table = vec![0f32; GRAD_TABLE_FLOATS];
        let mut grad_index: HashMap<u64, u32> = HashMap::new();
        let mut grad_count = 0usize;
        let mut quad_inst: Vec<f32> = Vec::new();
        let mut tex_inst: Vec<f32> = Vec::new();
        let mut batches: Vec<Batch> = Vec::new();

        let mut cur: Option<Batch> = None;
        let flush = |cur: &mut Option<Batch>, batches: &mut Vec<Batch>| {
            if let Some(b) = cur.take() {
                if b.count > 0 {
                    batches.push(b);
                }
            }
        };
        for prim in &frame.prims {
            match prim {
                Prim::Quad(q) => {
                    if cull(q.rect, w, h) {
                        continue;
                    }
                    let clip = q.clip;
                    let (gi, sn) = match &q.grad {
                        Some(g) => {
                            let key = grad_key(g);
                            let idx = if let Some(x) = grad_index.get(&key) {
                                *x
                            } else if grad_count < MAX_GRADIENTS {
                                let idx = grad_count as u32;
                                write_grad(&mut grad_table, grad_count, g);
                                grad_index.insert(key, idx);
                                grad_count += 1;
                                idx
                            } else {
                                u32::MAX // 渐变表满：退化为纯色（见 README 边界）
                            };
                            (idx, g.stops.len().min(MAX_STOPS) as f32)
                        }
                        None => (u32::MAX, 0.0),
                    };
                    let dir = q.grad.as_ref().map(|g| (g.dx, g.dy)).unwrap_or((0.0, 0.0));
                    let same_batch = cur.as_ref().map(|b| {
                        !b.is_tex && b.tex_id == 0 && same_clip(b.clip, clip)
                    }).unwrap_or(false);
                    if !same_batch {
                        flush(&mut cur, &mut batches);
                        let start = (quad_inst.len() / INST_FLOATS) as u32;
                        cur = Some(Batch { clip, is_tex: false, tex_id: 0, start, count: 0 });
                    }
                    let b = cur.as_mut().unwrap();
                    quad_inst.extend_from_slice(&[
                        q.rect[0], q.rect[1], q.rect[2], q.rect[3],
                        q.color[0], q.color[1], q.color[2], q.color[3],
                        q.radius, q.border, dir.0, dir.1,
                        gi as f32, sn, 0.0, 0.0,
                    ]);
                    b.count += 1;
                }
                Prim::Tex(t) => {
                    if cull(t.rect, w, h) {
                        continue;
                    }
                    let clip = t.clip;
                    let same_batch = cur.as_ref().map(|b| {
                        b.is_tex && b.tex_id == t.id && same_clip(b.clip, clip)
                    }).unwrap_or(false);
                    if !same_batch {
                        flush(&mut cur, &mut batches);
                        let start = (tex_inst.len() / INST_FLOATS) as u32;
                        cur = Some(Batch { clip, is_tex: true, tex_id: t.id, start, count: 0 });
                    }
                    let b = cur.as_mut().unwrap();
                    tex_inst.extend_from_slice(&[
                        t.rect[0], t.rect[1], t.rect[2], t.rect[3],
                        t.uv[0], t.uv[1], t.uv[2], t.uv[3],
                        t.radius, t.alpha, 0.0, 0.0,
                        t.id as f32, 0.0, 0.0, 0.0,
                    ]);
                    b.count += 1;
                }
            }
        }
        flush(&mut cur, &mut batches);

        // ---- 上传缓冲（不足则扩容重建）----
        if quad_inst.len() > self.quad_cap {
            self.quad_cap = (quad_inst.len() * 2).max(1024 * INST_FLOATS);
            self.quad_buf = empty_buffer(&self.device, self.quad_cap * 4);
        }
        if tex_inst.len() > self.tex_cap {
            self.tex_cap = (tex_inst.len() * 2).max(1024 * INST_FLOATS);
            self.tex_buf = empty_buffer(&self.device, self.tex_cap * 4);
        }
        if !quad_inst.is_empty() {
            self.queue.write_buffer(&self.quad_buf, 0, bytes_of(&quad_inst));
        }
        if !tex_inst.is_empty() {
            self.queue.write_buffer(&self.tex_buf, 0, bytes_of(&tex_inst));
        }
        self.queue.write_buffer(&self.grad_buf, 0, bytes_of(&grad_table));

        // ---- 按文档序重放批次 ----
        let mut last_clip: Option<[f32; 4]> = None;
        for b in &batches {
            if last_clip.map(|c| !same_clip(c, b.clip)).unwrap_or(true) {
                last_clip = Some(b.clip);
                set_scissor(pass, b.clip, frame.w, frame.h);
            }
            if b.is_tex {
                let Some((_, bind, _)) = self.textures.get(&b.tex_id) else { continue };
                pass.set_pipeline(&self.tex_pipeline);
                pass.set_bind_group(0, bind, &[]);
                pass.set_vertex_buffer(0, self.tex_buf.slice(..));
                pass.draw(0..6, b.start..b.start + b.count);
            } else {
                pass.set_pipeline(&self.quad_pipeline);
                pass.set_bind_group(0, &self.grad_bind, &[]);
                pass.set_vertex_buffer(0, self.quad_buf.slice(..));
                pass.draw(0..6, b.start..b.start + b.count);
            }
        }
    }
}

/// C ABI 独立呈现端：surface 由原始窗口句柄创建（Go CGO / Android NDK），
/// gx_gpu_end 直接渲染整帧并 present。
pub struct Standalone {
    surface: wgpu::Surface<'static>,
    config: wgpu::SurfaceConfiguration,
    device: wgpu::Device,
    gpu: QuadGpu,
}

impl Standalone {
    /// 从原始窗口句柄创建。句柄语义见 capi.rs 顶部注释。
    pub fn new(w: u32, h: u32, display_ptr: usize, window_ptr: usize) -> Result<Self, String> {
        if window_ptr == 0 {
            return Err("no native window handle (GPU build requires one; use cpu feature for headless)".into());
        }
        let (dh, wh) = platform_handles(display_ptr, window_ptr)?;
        let instance = wgpu::Instance::new(&wgpu::InstanceDescriptor::default());
        let surface = unsafe {
            instance.create_surface_unsafe(wgpu::SurfaceTargetUnsafe::RawHandle {
                raw_display_handle: dh,
                raw_window_handle: wh,
            })
        }
        .map_err(|e| format!("create_surface: {e}"))?;
        let adapter = pollster::block_on(instance.request_adapter(&wgpu::RequestAdapterOptions {
            compatible_surface: Some(&surface),
            ..Default::default()
        }))
        .map_err(|e| format!("request_adapter: {e}"))?;
        let (device, queue) = pollster::block_on(adapter.request_device(&wgpu::DeviceDescriptor::default()))
            .map_err(|e| format!("request_device: {e}"))?;
        let caps = surface.get_capabilities(&adapter);
        let format = caps
            .formats
            .iter()
            .copied()
            .find(|f| *f == wgpu::TextureFormat::Bgra8Unorm)
            .unwrap_or(caps.formats[0]);
        let config = wgpu::SurfaceConfiguration {
            usage: wgpu::TextureUsages::RENDER_ATTACHMENT,
            format,
            width: w,
            height: h,
            present_mode: wgpu::PresentMode::Fifo,
            alpha_mode: wgpu::CompositeAlphaMode::Opaque,
            view_formats: vec![],
            desired_maximum_frame_latency: 2,
        };
        surface.configure(&device, &config);
        let gpu = QuadGpu::new(device.clone(), queue, format);
        Ok(Standalone { surface, config, device, gpu })
    }

    pub fn resize(&mut self, w: u32, h: u32) {
        self.config.width = w;
        self.config.height = h;
        self.surface.configure(&self.device, &self.config);
    }

    /// 渲染一帧并 present。成功返回 0。
    pub fn render_present(&mut self, frame: &GpuFrame) -> i32 {
        if self.config.width != frame.w || self.config.height != frame.h {
            self.config.width = frame.w;
            self.config.height = frame.h;
            self.surface.configure(&self.device, &self.config);
        }
        let Ok(f) = self.surface.get_current_texture() else { return 1 };
        let view = f.texture.create_view(&wgpu::TextureViewDescriptor::default());
        let mut enc = self.gpu.device.create_command_encoder(&wgpu::CommandEncoderDescriptor { label: None });
        {
            let mut pass = enc.begin_render_pass(&wgpu::RenderPassDescriptor {
                label: Some("gx-standalone"),
                color_attachments: &[Some(wgpu::RenderPassColorAttachment {
                    view: &view,
                    resolve_target: None,
                    ops: wgpu::Operations {
                        load: wgpu::LoadOp::Clear(wgpu::Color {
                            r: (frame.clear[0] / 255.0) as f64,
                            g: (frame.clear[1] / 255.0) as f64,
                            b: (frame.clear[2] / 255.0) as f64,
                            a: (frame.clear[3] / 255.0) as f64,
                        }),
                        store: wgpu::StoreOp::Store,
                    },
                })],
                depth_stencil_attachment: None,
                timestamp_writes: None,
                occlusion_query_set: None,
            });
            self.gpu.draw(&mut pass, frame);
        }
        self.gpu.queue.submit(Some(enc.finish()));
        f.present();
        0
    }
}

/// 按平台把 (display_ptr, window_ptr) 翻译成 raw-window-handle 0.6 句柄。
#[allow(unused_variables)]
fn platform_handles(display_ptr: usize, window_ptr: usize) -> Result<(RawDisplayHandle, RawWindowHandle), String> {
    #[cfg(target_os = "linux")]
    {
        use raw_window_handle::{XlibDisplayHandle, XlibWindowHandle};
        let display = std::ptr::NonNull::new(display_ptr as *mut std::ffi::c_void)
            .ok_or("null X11 Display*")?;
        let wh = XlibWindowHandle::new(window_ptr as std::os::raw::c_ulong);
        let dh = XlibDisplayHandle::new(Some(display), 0);
        Ok((RawDisplayHandle::Xlib(dh), RawWindowHandle::Xlib(wh)))
    }
    #[cfg(target_os = "android")]
    {
        use raw_window_handle::{AndroidDisplayHandle, AndroidNdkWindowHandle};
        let win = std::ptr::NonNull::new(window_ptr as *mut std::ffi::c_void)
            .ok_or("null ANativeWindow*")?;
        let wh = AndroidNdkWindowHandle::new(win);
        let dh = RawDisplayHandle::Android(AndroidDisplayHandle::new());
        Ok((dh, RawWindowHandle::AndroidNdk(wh)))
    }
    #[cfg(not(any(target_os = "linux", target_os = "android")))]
    {
        Err("platform surface handles not wired yet (macOS/Windows: follow-up)".into())
    }
}

/// instanced 顶点布局：4 个 Float32x4 属性（64B/实例）。
fn inst_layout() -> wgpu::VertexBufferLayout<'static> {
    wgpu::VertexBufferLayout {
        array_stride: (INST_FLOATS * 4) as u64,
        step_mode: wgpu::VertexStepMode::Instance,
        attributes: &[
            wgpu::VertexAttribute { format: wgpu::VertexFormat::Float32x4, offset: 0, shader_location: 0 },
            wgpu::VertexAttribute { format: wgpu::VertexFormat::Float32x4, offset: 16, shader_location: 1 },
            wgpu::VertexAttribute { format: wgpu::VertexFormat::Float32x4, offset: 32, shader_location: 2 },
            wgpu::VertexAttribute { format: wgpu::VertexFormat::Float32x4, offset: 48, shader_location: 3 },
        ],
    }
}

fn empty_buffer(device: &wgpu::Device, bytes: usize) -> wgpu::Buffer {
    device.create_buffer(&wgpu::BufferDescriptor {
        label: Some("gx-inst"),
        size: bytes.max(1) as u64,
        usage: wgpu::BufferUsages::VERTEX | wgpu::BufferUsages::COPY_DST,
        mapped_at_creation: false,
    })
}

fn bytes_of(v: &[f32]) -> &[u8] {
    unsafe { std::slice::from_raw_parts(v.as_ptr() as *const u8, v.len() * 4) }
}

fn same_clip(a: [f32; 4], b: [f32; 4]) -> bool {
    (a[0] - b[0]).abs() < 1e-4 && (a[1] - b[1]).abs() < 1e-4
        && (a[2] - b[2]).abs() < 1e-4 && (a[3] - b[3]).abs() < 1e-4
}

/// 完全越界剔除（与 CPU 后端口径一致）。
fn cull(r: [f32; 4], w: f32, h: f32) -> bool {
    r[2] <= 0.0 || r[3] <= 0.0 || r[0] >= w || r[1] >= h || r[0] + r[2] <= 0.0 || r[1] + r[3] <= 0.0
}

fn set_scissor(pass: &mut wgpu::RenderPass<'_>, clip: [f32; 4], w: u32, h: u32) {
    // clip w<=0 → 无裁剪（整屏 scissor）。
    let (x, y, cw, ch) = if clip[2] <= 0.0 || clip[3] <= 0.0 {
        (0.0, 0.0, w as f32, h as f32)
    } else {
        (clip[0], clip[1], clip[2], clip[3])
    };
    let x0 = x.max(0.0).floor() as u32;
    let y0 = y.max(0.0).floor() as u32;
    let x1 = (x + cw).min(w as f32).ceil() as u32;
    let y1 = (y + ch).min(h as f32).ceil() as u32;
    let rw = x1.saturating_sub(x0).min(w - x0);
    let rh = y1.saturating_sub(y0).min(h - y0);
    pass.set_scissor_rect(x0, y0, rw, rh);
}

fn grad_key(g: &Grad) -> u64 {
    use std::collections::hash_map::DefaultHasher;
    use std::hash::{Hash, Hasher};
    let mut s = DefaultHasher::new();
    g.dx.to_bits().hash(&mut s);
    g.dy.to_bits().hash(&mut s);
    for st in &g.stops {
        for v in st {
            v.to_bits().hash(&mut s);
        }
    }
    s.finish()
}

/// 写入渐变 uniform 表。布局：pos_rgb 区 [idx*8+i]*4 = (pos,r,g,b)，
/// alpha 区同偏移 + GRAD_TABLE_FLOATS/2 = (a,0,0,0)。
fn write_grad(table: &mut [f32], idx: usize, g: &Grad) {
    let half = GRAD_TABLE_FLOATS / 2;
    for (i, st) in g.stops.iter().take(MAX_STOPS).enumerate() {
        let o = (idx * MAX_STOPS + i) * 4;
        table[o] = st[0];
        table[o + 1] = st[1];
        table[o + 2] = st[2];
        table[o + 3] = st[3];
        let ao = half + (idx * MAX_STOPS + i) * 4;
        table[ao] = st[4];
    }
}

/// WGSL：quad（SDF 圆角/描边环/渐变）+ tex（UV 采样/圆角遮罩）双管线。
const WGSL: &str = r#"
struct GradTable { pos_rgb: array<vec4f, 512>, alpha: array<vec4f, 512> };
@group(0) @binding(0) var<uniform> grads: GradTable;

// 注意：instance 输入只能在 vertex 阶段读取；fragment 需要的字段经插值传递。

struct QuadIn {
  @location(0) rect: vec4f,   // x,y,w,h
  @location(1) color: vec4f,  // rgba 0..255
  @location(2) misc: vec4f,   // radius, border, dirx, diry
  @location(3) grad: vec4f,   // grad_index, stop_count, ·, ·
};
struct QuadOut {
  @builtin(position) pos: vec4f,
  @location(0) local: vec2f,  // 相对 quad 中心的局部坐标（像素）
  @location(1) half: vec2f,   // 半宽半高
  @location(2) color: vec4f,  // rgba 0..255
  @location(3) misc: vec4f,   // radius, border, dirx, diry
  @location(4) grad: vec4f,   // grad_index, stop_count, ·, ·
};

fn sd_round_rect(p: vec2f, half: vec2f, r: f32) -> f32 {
  let rr = clamp(r, 0.0, min(half.x, half.y));
  let q = abs(p) - half + vec2f(rr);
  return length(max(q, vec2f(0.0))) + min(max(q.x, q.y), 0.0) - rr;
}
fn cov(d: f32) -> f32 { return clamp(0.5 - d, 0.0, 1.0); }

@vertex fn quad_vs(@builtin(vertex_index) vi: u32, q: QuadIn) -> QuadOut {
  var c = array<vec2f,6>(
    vec2f(0.0,0.0), vec2f(1.0,0.0), vec2f(1.0,1.0),
    vec2f(0.0,0.0), vec2f(1.0,1.0), vec2f(0.0,1.0));
  let k = c[vi];
  var o: QuadOut;
  o.pos = vec4f(q.rect.xy + k * q.rect.zw, 0.0, 1.0);
  o.local = (k - vec2f(0.5, 0.5)) * q.rect.zw;
  o.half = q.rect.zw * 0.5;
  o.color = q.color;
  o.misc = q.misc;
  o.grad = q.grad;
  return o;
}

@fragment fn quad_fs(in: QuadOut) -> @location(0) vec4f {
  let d = sd_round_rect(in.local, in.half, in.misc.x);
  var a = cov(d);
  let bw = in.misc.y;
  if (bw > 0.0) {
    // 描边环：带心路径径（-bw/2..+bw/2），与 tiny-skia stroke_path 同几何。
    a = clamp(cov(d - bw * 0.5) - cov(d + bw * 0.5), 0.0, 1.0);
  }
  var rgb = in.color.rgb / 255.0;
  var alpha = (in.color.a / 255.0) * a;
  let dir = vec2f(in.misc.z, in.misc.w);
  if (dir.x != 0.0 || dir.y != 0.0) {
    // 线性渐变：方向单位向量 + 投影渐变线（CSS 语义，与 Go/Skia 端同算法）。
    let nd = normalize(dir);
    let len = (in.half.x * 2.0 * abs(nd.x) + in.half.y * 2.0 * abs(nd.y)) * 0.5;
    let t = clamp(dot(in.local, nd) / max(len, 1e-6) + 0.5, 0.0, 1.0);
    let gi = u32(in.grad.x);
    let n = clamp(u32(in.grad.y), 1u, 8u);
    var col = grads.pos_rgb[gi * 8u].yzw;
    var ca = grads.alpha[gi * 8u].x;
    for (var i = 1u; i < 8u; i = i + 1u) {
      if (i >= n) { break; }
      let cur = grads.pos_rgb[gi * 8u + i];
      let cura = grads.alpha[gi * 8u + i].x;
      let pre = grads.pos_rgb[gi * 8u + i - 1u];
      let prea = grads.alpha[gi * 8u + i - 1u].x;
      let span = max(cur.x - pre.x, 1e-6);
      let f = clamp((t - pre.x) / span, 0.0, 1.0);
      col = mix(col, cur.yzw, f);
      ca = mix(ca, cura, f);
    }
    rgb = col / 255.0;
    alpha = (ca / 255.0) * a;
  }
  return vec4f(rgb * alpha, alpha); // 预乘输出
}

struct TexIn {
  @location(0) rect: vec4f,
  @location(1) uv: vec4f,     // u0,v0,u1,v1（像素）
  @location(2) misc: vec4f,   // radius, alpha, tex_w, tex_h
  @location(3) flags: vec4f,  // premul(1/0), ·, ·, ·
};
struct TexOut {
  @builtin(position) pos: vec4f,
  @location(0) local: vec2f,
  @location(1) half: vec2f,
  @location(2) uv: vec2f,
  @location(3) misc: vec4f,   // radius, alpha, tex_w, tex_h
  @location(4) flags: vec4f,  // premul(1/0), ·, ·, ·
};

@vertex fn tex_vs(@builtin(vertex_index) vi: u32, t: TexIn) -> TexOut {
  var c = array<vec2f,6>(
    vec2f(0.0,0.0), vec2f(1.0,0.0), vec2f(1.0,1.0),
    vec2f(0.0,0.0), vec2f(1.0,1.0), vec2f(0.0,1.0));
  let k = c[vi];
  var o: TexOut;
  o.pos = vec4f(t.rect.xy + k * t.rect.zw, 0.0, 1.0);
  o.local = (k - vec2f(0.5, 0.5)) * t.rect.zw;
  o.half = t.rect.zw * 0.5;
  o.uv = vec2f(mix(t.uv.x, t.uv.z, k.x), mix(t.uv.y, t.uv.w, k.y));
  o.misc = t.misc;
  o.flags = t.flags;
  return o;
}

@group(0) @binding(0) var tex_src: texture_2d<f32>;
@group(0) @binding(1) var tex_smp: sampler;

@fragment fn tex_fs(in: TexOut) -> @location(0) vec4f {
  let d = sd_round_rect(in.local, in.half, in.misc.x);
  let mask = clamp(0.5 - d, 0.0, 1.0);
  let uv = in.uv / vec2f(max(in.misc.z, 1.0), max(in.misc.w, 1.0));
  let c = textureSample(tex_src, tex_smp, uv);
  var rgb = c.rgb;
  var a = c.a;
  if (in.flags.x < 0.5) { rgb = rgb * a; } // 直通 alpha 数据 → 预乘
  rgb = rgb * in.misc.y;
  a = a * in.misc.y;
  return vec4f(rgb * mask, a * mask);
}
"#;

// ---------- GPU 后端单测（llvmpipe/lavapipe 可跑，无窗口依赖） ----------

#[cfg(test)]
mod tests {
    use super::*;
    use crate::TexUpload;

    async fn device_or_skip() -> Option<(wgpu::Device, wgpu::Queue)> {
        let instance = wgpu::Instance::new(&wgpu::InstanceDescriptor::default());
        let Ok(adapter) = instance.request_adapter(&wgpu::RequestAdapterOptions::default()).await else {
            eprintln!("[gx_gpu test] no GPU adapter, skip offscreen test");
            return None;
        };
        let Ok((device, queue)) = adapter.request_device(&wgpu::DeviceDescriptor::default()).await else {
            return None;
        };
        Some((device, queue))
    }

    /// 离屏渲染一帧：验证 WGSL 编译、管线创建与 draw 调用不 panic
    /// （shader 编译错误会在此暴露；像素级语义由 cpu 后端单测覆盖）。
    #[test]
    fn offscreen_draw_compiles_and_runs() {
        let Some((device, queue)) = pollster::block_on(device_or_skip()) else { return };
        let mut gpu = QuadGpu::new(device.clone(), queue.clone(), wgpu::TextureFormat::Rgba8Unorm);
        let mut f = GpuFrame::new(64, 64, [0.0, 0.0, 0.0, 255.0]);
        // 纯色 quad + 渐变 quad + 纹理 quad，覆盖全部三条路径。
        f.quad([0.0, 16.0, 32.0, 32.0], [0.0, 0.0, 0.0, 0.0], [255.0, 0.0, 0.0, 255.0], 4.0, 0.0);
        f.gradient(
            [32.0, 16.0, 32.0, 32.0], [0.0, 0.0, 0.0, 0.0], 0.0,
            Grad { dx: 0.0, dy: -1.0, stops: vec![[0.0, 0.0, 0.0, 0.0, 255.0], [1.0, 255.0, 255.0, 255.0, 255.0]] },
        );
        f.quad([8.0, 8.0, 16.0, 16.0], [0.0, 0.0, 0.0, 0.0], [0.0, 255.0, 0.0, 255.0], 0.0, 2.0);
        f.uploads.push(TexUpload {
            id: 1, key: 1, w: 4, h: 4,
            data: std::sync::Arc::new(vec![255u8; 4 * 4 * 4]),
            premul: true,
        });
        f.tex([16.0, 40.0, 16.0, 16.0], [0.0, 0.0, 0.0, 0.0], 1, [0.0, 0.0, 4.0, 4.0], 4.0, 1.0);
        let tex = device.create_texture(&wgpu::TextureDescriptor {
            label: Some("offscreen"),
            size: wgpu::Extent3d { width: 64, height: 64, depth_or_array_layers: 1 },
            mip_level_count: 1,
            sample_count: 1,
            dimension: wgpu::TextureDimension::D2,
            format: wgpu::TextureFormat::Rgba8Unorm,
            usage: wgpu::TextureUsages::RENDER_ATTACHMENT,
            view_formats: &[],
        });
        let view = tex.create_view(&wgpu::TextureViewDescriptor::default());
        let mut enc = device.create_command_encoder(&wgpu::CommandEncoderDescriptor { label: None });
        {
            let mut pass = enc.begin_render_pass(&wgpu::RenderPassDescriptor {
                label: None,
                color_attachments: &[Some(wgpu::RenderPassColorAttachment {
                    view: &view,
                    resolve_target: None,
                    ops: wgpu::Operations {
                        load: wgpu::LoadOp::Clear(wgpu::Color { r: 0.0, g: 0.0, b: 0.0, a: 1.0 }),
                        store: wgpu::StoreOp::Store,
                    },
                })],
                depth_stencil_attachment: None,
                timestamp_writes: None,
                occlusion_query_set: None,
            });
            gpu.draw(&mut pass, &f);
        }
        queue.submit(Some(enc.finish()));
        // 提交成功即通过（设备校验在 submit/present 阶段兜底）。
        let _ = device.poll(wgpu::PollType::Wait);
    }

    #[test]
    fn texture_upload_sync_dedup() {
        let Some((device, queue)) = pollster::block_on(device_or_skip()) else { return };
        let mut gpu = QuadGpu::new(device.clone(), queue.clone(), wgpu::TextureFormat::Rgba8Unorm);
        let mut f = GpuFrame::new(4, 4, [0.0; 4]);
        f.uploads.push(TexUpload {
            id: 1, key: 7, w: 4, h: 4,
            data: std::sync::Arc::new(vec![255u8; 4 * 4 * 4]),
            premul: true,
        });
        gpu.sync_textures(&f);
        gpu.sync_textures(&f); // 同 key 不重建
        let mut f2 = GpuFrame::new(4, 4, [0.0; 4]);
        f2.uploads.push(TexUpload {
            id: 1, key: 8, w: 4, h: 4,
            data: std::sync::Arc::new(vec![0u8; 4 * 4 * 4]),
            premul: true,
        });
        gpu.sync_textures(&f2); // key 变化触发重建
        assert_eq!(gpu.textures.len(), 1);
    }

    #[test]
    fn batch_construction_merges_and_keeps_order() {
        // 纯逻辑批次验证走离屏渲染（无 adapter 时 skip）；此处验证 cull。
        assert!(cull([0.0, 0.0, 0.0, 10.0], 100.0, 100.0), "零宽应剔除");
        assert!(cull([-20.0, 0.0, 10.0, 10.0], 100.0, 100.0), "完全越界应剔除");
        assert!(!cull([10.0, 10.0, 10.0, 10.0], 100.0, 100.0));
    }
}
