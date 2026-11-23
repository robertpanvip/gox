// PULSE —— 独立 Rust 渲染后端技术验证
//
// 技术栈（全部 Rust，无 CGO / 无 Go 核心依赖）：
//   - winit 0.30   窗口 + 原生 IME（Preedit / Commit）
//   - wgpu 25      GPU 合成（Mesa llvmpipe 软件渲染已验证）
//   - tiny-skia    CPU 光栅化：卡片 / 进度条 / 按钮 / 输入框（画到纹理）
//   - glyphon 0.9  文本渲染（cosmic-text shaping + CJK 字体回退）
//   - Taffy 0.5    Flex 布局
//
// 两条渲染管线合成：tiny-skia 画图形 → 上传 wgpu 纹理 → blit 全屏 → glyphon 叠加文本。
//
// 响应式模型：用 Rust 的可变状态 + render() 重绘模拟 GX 的 signal。
// 本程序证明：这套栈能渲染中文、能点击按钮、能实时显示 IME 组合串——
// 尤其 IME preedit 是 GLFW 无法做到、而这类自绘输入框真正需要的能力。

use glyphon::{
    Attrs, Buffer, Cache, Color, Family, FontSystem, Metrics, Resolution, Shaping, SwashCache,
    TextArea, TextAtlas, TextBounds, TextRenderer, Viewport,
};
use std::sync::Arc;
use wgpu::{
    BindGroup, BindGroupDescriptor, BindGroupEntry, BindGroupLayoutDescriptor,
    BindGroupLayoutEntry, BindingResource, BindingType, BlendState, ColorTargetState,
    CommandEncoderDescriptor, CompositeAlphaMode, DeviceDescriptor, Extent3d, FragmentState,
    Instance, InstanceDescriptor, LoadOp, MultisampleState, Operations, Origin3d,
    PipelineLayoutDescriptor, PresentMode, PrimitiveState, RenderPassColorAttachment,
    RenderPassDescriptor, RenderPipeline, RenderPipelineDescriptor, RequestAdapterOptions,
    SamplerBindingType, SamplerDescriptor, ShaderModuleDescriptor, ShaderSource,
    SurfaceConfiguration, TextureAspect, TextureDescriptor, TextureDimension, TextureFormat,
    TextureSampleType, TextureUsages, TextureViewDimension, TextureViewDescriptor,
    TexelCopyBufferLayout, TexelCopyTextureInfo, VertexState,
};
use winit::{
    dpi::LogicalSize,
    event::{ElementState, Ime, KeyEvent, MouseButton, WindowEvent},
    event_loop::EventLoop,
    keyboard::{Key, NamedKey},
    window::Window,
};

const W: u32 = 760;
const H: u32 = 560;
const BG: (u8, u8, u8) = (11, 16, 32); // #0b1020
const CYAN: (u8, u8, u8) = (45, 212, 191);
const PURPLE: (u8, u8, u8) = (167, 139, 250);
const ORANGE: (u8, u8, u8) = (251, 146, 60);

fn main() {
    let event_loop = EventLoop::new().unwrap();
    event_loop.run_app(&mut App { state: None }).unwrap();
}

fn align_up(v: u32, a: u32) -> u32 {
    (v + a - 1) / a * a
}

/// tiny-skia 0.11 无 round_rect，手写圆角矩形路径。
fn round_rect_path(x: f32, y: f32, w: f32, h: f32, r: f32) -> Option<tiny_skia::Path> {
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

// ---------------- 文本项 ----------------

struct TextItem {
    buffer: Buffer,
    x: f32,
    y: f32,
    color: Color,
}

impl TextItem {
    fn new(fs: &mut FontSystem, text: &str, x: f32, y: f32, px: f32, lh: f32, color: Color) -> Self {
        let mut buffer = Buffer::new(fs, Metrics::new(px, lh));
        buffer.set_size(fs, Some(660.0), Some(60.0));
        buffer.set_text(fs, text, &Attrs::new().family(Family::SansSerif), Shaping::Advanced);
        buffer.shape_until_scroll(fs, false);
        TextItem { buffer, x, y, color }
    }
    fn set(&mut self, fs: &mut FontSystem, text: &str) {
        self.buffer.set_text(fs, text, &Attrs::new().family(Family::SansSerif), Shaping::Advanced);
        self.buffer.shape_until_scroll(fs, false);
    }
}

// ---------------- 按钮 ----------------

#[derive(Clone, Copy, PartialEq, Debug)]
enum Action {
    Boost,
    Reset,
    Clear,
}

struct ButtonDef {
    label: &'static str,
    rect: (f32, f32, f32, f32), // x, y, w, h
    action: Action,
    rgb: (u8, u8, u8),
}

// ---------------- 应用状态（模拟 signal）----------------

struct Model {
    count: i64,            // BOOST/RESET 联动的计数
    committed: String,     // 输入框已上屏文本
    preedit: String,       // IME 组合串
    focused: bool,         // 输入框是否聚焦
    clicks: u32,           // 点击次数（观测）
}

impl Model {
    fn new() -> Self {
        let committed = std::env::var("GX_COMMIT").unwrap_or_else(|_| "operator".into());
        let preedit = std::env::var("GX_PREEDIT").unwrap_or_default();
        let focused = std::env::var("GX_COMMIT").is_ok() || !preedit.is_empty();
        Model { count: 42, committed, preedit, focused, clicks: 0 }
    }
    // 派生值（对应 Signal.computed）
    fn requests(&self) -> String {
        // Rust 的 format! 无千分位符，手动分组
        let n = 12_840 + self.count * 137;
        let s = n.abs().to_string();
        let grouped: String = s
            .as_bytes()
            .rchunks(3)
            .map(|c| std::str::from_utf8(c).unwrap())
            .collect::<Vec<_>>()
            .into_iter()
            .rev()
            .collect::<Vec<_>>()
            .join(",");
        if n < 0 { format!("-{}", grouped) } else { grouped }
    }
    fn throughput(&self) -> String { format!("{:.1}k/s", 1.2 + self.count as f64 * 0.013) }
    fn uptime(&self) -> String { format!("{:.2}%", 99.98 - (self.count % 7) as f64 * 0.01) }
    fn load_pct(&self) -> f32 { ((self.count * 7) % 100) as f32 / 100.0 }
    fn greeting(&self) -> String {
        let who = if self.committed.is_empty() { "there" } else { &self.committed };
        format!("Hello, {} — 文本由 signal 驱动", who)
    }
}

// ---------------- 窗口状态 ----------------

struct State {
    device: wgpu::Device,
    queue: wgpu::Queue,
    surface: wgpu::Surface<'static>,
    config: SurfaceConfiguration,

    bg_texture: wgpu::Texture,
    bg_bind: BindGroup,
    blit_pipeline: RenderPipeline,
    bg_pixels: Vec<u8>,
    bg_bpr: u32,

    font_system: FontSystem,
    swash_cache: SwashCache,
    viewport: Viewport,
    atlas: TextAtlas,
    text_renderer: TextRenderer,
    texts: Vec<TextItem>,

    model: Model,
    buttons: Vec<ButtonDef>,
    hover: Option<usize>,
    cursor: (f32, f32), // 最近一次鼠标坐标（MouseInput 事件本身不带坐标）
    cards: Vec<(f32, f32, f32, f32)>,
    window: Arc<Window>,
}

// 文本项索引
const T_TITLE: usize = 0;
const T_C1_LABEL: usize = 1;
const T_C1_VAL: usize = 2;
const T_C2_LABEL: usize = 3;
const T_C2_VAL: usize = 4;
const T_C3_LABEL: usize = 5;
const T_C3_VAL: usize = 6;
const T_LOAD_LABEL: usize = 7;
const T_LOAD_VAL: usize = 8;
const T_CONSOLE: usize = 9;
const T_INPUT: usize = 10;
const T_PREEDIT_TAG: usize = 11;
const T_PREEDIT: usize = 12;
const T_GREET: usize = 13;
const T_B0: usize = 14;
const T_B1: usize = 15;
const T_B2: usize = 16;
const T_CLICKS: usize = 17;

impl State {
    async fn new(window: Arc<Window>) -> Self {
        let size = window.inner_size();
        let instance = Instance::new(&InstanceDescriptor::default());
        let adapter = instance
            .request_adapter(&RequestAdapterOptions::default())
            .await
            .expect("request_adapter");
        eprintln!("[adapter] {} / {:?}", adapter.get_info().name, adapter.get_info().backend);
        let (device, queue) = adapter
            .request_device(&DeviceDescriptor::default())
            .await
            .expect("request_device");

        let surface = instance.create_surface(window.clone()).expect("create_surface");
        let fmt = TextureFormat::Bgra8UnormSrgb;
        let config = SurfaceConfiguration {
            usage: TextureUsages::RENDER_ATTACHMENT,
            format: fmt,
            width: size.width,
            height: size.height,
            present_mode: PresentMode::Fifo,
            alpha_mode: CompositeAlphaMode::Opaque,
            view_formats: vec![],
            desired_maximum_frame_latency: 2,
        };
        surface.configure(&device, &config);

        // tiny-skia 背景纹理
        let bpr = align_up(W * 4, 256);
        let bg_pixels = vec![0u8; (bpr * H) as usize];
        let bg_texture = device.create_texture(&TextureDescriptor {
            label: Some("bg"),
            size: Extent3d { width: W, height: H, depth_or_array_layers: 1 },
            mip_level_count: 1,
            sample_count: 1,
            dimension: TextureDimension::D2,
            format: TextureFormat::Rgba8Unorm,
            usage: TextureUsages::TEXTURE_BINDING | TextureUsages::COPY_DST,
            view_formats: &[],
        });
        let bg_view = bg_texture.create_view(&TextureViewDescriptor::default());
        let sampler = device.create_sampler(&SamplerDescriptor {
            mag_filter: wgpu::FilterMode::Linear,
            min_filter: wgpu::FilterMode::Linear,
            ..Default::default()
        });

        let shader = device.create_shader_module(ShaderModuleDescriptor {
            label: Some("blit"),
            source: ShaderSource::Wgsl(
                r#"
struct VSOut { @builtin(position) pos: vec4f, @location(0) uv: vec2f };
@vertex fn vs(@builtin(vertex_index) i: u32) -> VSOut {
  var p = array<vec2f,3>(vec2f(-1.,-1.), vec2f(3.,-1.), vec2f(-1.,3.));
  var uv = array<vec2f,3>(vec2f(0.,1.), vec2f(2.,1.), vec2f(0.,-1.));
  var o: VSOut; o.pos = vec4f(p[i],0.,1.); o.uv = uv[i]; return o;
}
@group(0) @binding(0) var t: texture_2d<f32>;
@group(0) @binding(1) var s: sampler;
@fragment fn fs(in: VSOut) -> @location(0) vec4f { return textureSample(t, s, in.uv); }
"#.into(),
            ),
        });
        let bgl = device.create_bind_group_layout(&BindGroupLayoutDescriptor {
            label: Some("blit-bgl"),
            entries: &[
                BindGroupLayoutEntry {
                    binding: 0,
                    visibility: wgpu::ShaderStages::FRAGMENT,
                    ty: BindingType::Texture {
                        sample_type: TextureSampleType::Float { filterable: true },
                        view_dimension: TextureViewDimension::D2,
                        multisampled: false,
                    },
                    count: None,
                },
                BindGroupLayoutEntry {
                    binding: 1,
                    visibility: wgpu::ShaderStages::FRAGMENT,
                    ty: BindingType::Sampler(SamplerBindingType::Filtering),
                    count: None,
                },
            ],
        });
        let layout = device.create_pipeline_layout(&PipelineLayoutDescriptor {
            label: Some("blit-pl"),
            bind_group_layouts: &[&bgl],
            push_constant_ranges: &[],
        });
        let blit_pipeline = device.create_render_pipeline(&RenderPipelineDescriptor {
            label: Some("blit"),
            layout: Some(&layout),
            vertex: VertexState {
                module: &shader,
                entry_point: Some("vs"),
                buffers: &[],
                compilation_options: Default::default(),
            },
            primitive: PrimitiveState::default(),
            depth_stencil: None,
            multisample: MultisampleState::default(),
            fragment: Some(FragmentState {
                module: &shader,
                entry_point: Some("fs"),
                compilation_options: Default::default(),
                targets: &[Some(ColorTargetState {
                    format: fmt,
                    blend: Some(BlendState::REPLACE),
                    write_mask: wgpu::ColorWrites::ALL,
                })],
            }),
            cache: None,
            multiview: None,
        });
        let bg_bind = device.create_bind_group(&BindGroupDescriptor {
            label: Some("bg-bind"),
            layout: &bgl,
            entries: &[
                BindGroupEntry { binding: 0, resource: BindingResource::TextureView(&bg_view) },
                BindGroupEntry { binding: 1, resource: BindingResource::Sampler(&sampler) },
            ],
        });

        // glyphon + 中文字体
        let mut font_system = FontSystem::new();
        for path in [
            "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
            "/usr/share/fonts/truetype/noto/NotoSans-Regular.ttf",
        ] {
            if let Err(e) = font_system.db_mut().load_font_file(path) {
                eprintln!("[font] {} : {:?}", path, e);
            }
        }
        let swash_cache = SwashCache::new();
        let cache = Cache::new(&device);
        let viewport = Viewport::new(&device, &cache);
        let mut atlas = TextAtlas::new(&device, &queue, &cache, fmt);
        let text_renderer = TextRenderer::new(&mut atlas, &device, MultisampleState::default(), None);

        let white = Color::rgb(226, 232, 240);
        let muted = Color::rgb(148, 163, 184);
        let cy = Color::rgb(CYAN.0, CYAN.1, CYAN.2);
        let model = Model::new();
        let texts = vec![
            TextItem::new(&mut font_system, "PULSE  ·  脉冲看板", 28.0, 20.0, 24.0, 32.0, white),
            TextItem::new(&mut font_system, "请求量", 44.0, 78.0, 14.0, 20.0, muted),
            TextItem::new(&mut font_system, &model.requests(), 44.0, 108.0, 26.0, 34.0, cy),
            TextItem::new(&mut font_system, "吞吐量", 272.0, 78.0, 14.0, 20.0, muted),
            TextItem::new(&mut font_system, &model.throughput(), 272.0, 108.0, 26.0, 34.0, Color::rgb(PURPLE.0, PURPLE.1, PURPLE.2)),
            TextItem::new(&mut font_system, "运行时长", 500.0, 78.0, 14.0, 20.0, muted),
            TextItem::new(&mut font_system, &model.uptime(), 500.0, 108.0, 26.0, 34.0, Color::rgb(ORANGE.0, ORANGE.1, ORANGE.2)),
            TextItem::new(&mut font_system, "SYSTEM LOAD  ·  系统负载", 28.0, 218.0, 14.0, 20.0, muted),
            TextItem::new(&mut font_system, "42%", 620.0, 218.0, 14.0, 20.0, cy),
            TextItem::new(&mut font_system, "操作员控制台 / OPERATOR CONSOLE", 28.0, 288.0, 15.0, 22.0, white),
            TextItem::new(&mut font_system, &model.committed, 44.0, 350.0, 17.0, 26.0, white),
            TextItem::new(&mut font_system, "拼:", 44.0, 398.0, 13.0, 20.0, cy),
            TextItem::new(&mut font_system, &model.preedit, 78.0, 397.0, 17.0, 26.0, cy),
            TextItem::new(&mut font_system, &model.greeting(), 28.0, 450.0, 13.0, 20.0, muted),
            TextItem::new(&mut font_system, "+ BOOST", 0.0, 0.0, 15.0, 22.0, white),
            TextItem::new(&mut font_system, "- RESET", 0.0, 0.0, 15.0, 22.0, white),
            TextItem::new(&mut font_system, "CLEAR", 0.0, 0.0, 15.0, 22.0, white),
            TextItem::new(&mut font_system, "", 560.0, 20.0, 13.0, 20.0, muted),
        ];

        let buttons = vec![
            ButtonDef { label: "+ BOOST", rect: (28.0, 486.0, 150.0, 46.0), action: Action::Boost, rgb: CYAN },
            ButtonDef { label: "- RESET", rect: (190.0, 486.0, 150.0, 46.0), action: Action::Reset, rgb: PURPLE },
            ButtonDef { label: "CLEAR", rect: (352.0, 486.0, 130.0, 46.0), action: Action::Clear, rgb: ORANGE },
        ];

        // Taffy：算三卡片位置（flex row + gap）
        let mut taffy: taffy::TaffyTree<()> = taffy::TaffyTree::new();
        use taffy::prelude::*;
        let cs = Style {
            size: Size { width: length(0.0), height: length(96.0) },
            flex_grow: 1.0,
            ..Default::default()
        };
        let c1 = taffy.new_leaf(cs.clone()).unwrap();
        let c2 = taffy.new_leaf(cs.clone()).unwrap();
        let c3 = taffy.new_leaf(cs.clone()).unwrap();
        let row = taffy.new_with_children(
            Style {
                display: Display::Flex,
                flex_direction: FlexDirection::Row,
                gap: Size { width: length(14.0), height: length(14.0) },
                size: Size { width: length(704.0), height: length(96.0) },
                padding: Rect { left: length(28.0), right: length(28.0), top: length(0.0), bottom: length(0.0) },
                inset: Rect { left: length(0.0), top: length(58.0), right: length(0.0), bottom: length(0.0) },
                ..Default::default()
            },
            &[c1, c2, c3],
        ).unwrap();
        taffy.compute_layout(row, Size::MAX_CONTENT).unwrap();
        let cards: Vec<_> = [c1, c2, c3].iter().map(|&n| {
            let l = taffy.layout(n).unwrap();
            (l.location.x, l.location.y + 58.0, l.size.width, l.size.height)
        }).collect();

        State {
            device, queue, surface, config,
            bg_texture, bg_bind, blit_pipeline, bg_pixels, bg_bpr: bpr,
            font_system, swash_cache, viewport, atlas, text_renderer, texts,
            model, buttons, hover: None, cursor: (0.0, 0.0), cards, window,
        }
    }

    /// 把 Model 的状态同步到文本 buffer（对应响应式重算）。
    fn sync_texts(&mut self) {
        let m = &self.model;
        self.texts[T_C1_VAL].set(&mut self.font_system, &m.requests());
        self.texts[T_C2_VAL].set(&mut self.font_system, &m.throughput());
        self.texts[T_C3_VAL].set(&mut self.font_system, &m.uptime());
        self.texts[T_LOAD_VAL].set(&mut self.font_system, &format!("{:.0}%", m.load_pct() * 100.0));
        self.texts[T_INPUT].set(&mut self.font_system, &m.committed);
        self.texts[T_PREEDIT].set(&mut self.font_system, &m.preedit);
        self.texts[T_GREET].set(&mut self.font_system, &m.greeting());
        self.texts[T_CLICKS].set(&mut self.font_system, &format!("clicks: {}", m.clicks));
    }

    /// tiny-skia 画所有图形到 pixmap，上传为 GPU 纹理。
    fn paint_bg(&mut self) {
        use tiny_skia::{Color, FillRule, Paint, Pixmap, Stroke, Transform};
        let bpr = self.bg_bpr as usize;
        let mut pm = Pixmap::new(W, H).unwrap();
        pm.fill(Color::from_rgba8(BG.0, BG.1, BG.2, 255));

        // 卡片
        let mut card = Paint::default();
        card.set_color(Color::from_rgba8(20, 27, 51, 255));
        card.anti_alias = true;
        for &(x, y, w, h) in &self.cards {
            if let Some(path) = round_rect_path(x, y, w, h, 12.0) {
                pm.fill_path(&path, &card, FillRule::Winding, Transform::identity(), None);
            }
        }

        // 进度条：底 + 动态填充
        if let Some(path) = round_rect_path(28.0, 250.0, 704.0, 14.0, 7.0) {
            let mut p = Paint::default();
            p.set_color(Color::from_rgba8(30, 41, 69, 255));
            pm.fill_path(&path, &p, FillRule::Winding, Transform::identity(), None);
        }
        let fill_w = (704.0 * self.model.load_pct()).max(14.0);
        if let Some(path) = round_rect_path(28.0, 250.0, fill_w, 14.0, 7.0) {
            let mut p = Paint::default();
            p.set_color(Color::from_rgba8(CYAN.0, CYAN.1, CYAN.2, 255));
            pm.fill_path(&path, &p, FillRule::Winding, Transform::identity(), None);
        }

        // 输入框
        if let Some(path) = round_rect_path(28.0, 336.0, 704.0, 44.0, 8.0) {
            let mut p = Paint::default();
            p.set_color(Color::from_rgba8(15, 23, 42, 255));
            pm.fill_path(&path, &p, FillRule::Winding, Transform::identity(), None);
            let stroke = Stroke { width: if self.model.focused { 2.0 } else { 1.0 }, ..Default::default() };
            let mut sp = Paint::default();
            sp.set_color(if self.model.focused {
                Color::from_rgba8(CYAN.0, CYAN.1, CYAN.2, 255)
            } else {
                Color::from_rgba8(71, 85, 105, 255)
            });
            pm.stroke_path(&path, &sp, &stroke, Transform::identity(), None);
        }

        // 按钮（hover 时提亮）
        for (i, b) in self.buttons.iter().enumerate() {
            let (x, y, w, h) = b.rect;
            if let Some(path) = round_rect_path(x, y, w, h, 9.0) {
                let (r, g, bl) = b.rgb;
                let mut p = Paint::default();
                let a = if self.hover == Some(i) { 220 } else { 190 };
                p.set_color(Color::from_rgba8(r, g, bl, a));
                pm.fill_path(&path, &p, FillRule::Winding, Transform::identity(), None);
            }
        }

        // 拷回 padded buffer 并上传
        let src = pm.data();
        for y in 0..H as usize {
            let row = &src[y * W as usize * 4..(y + 1) * W as usize * 4];
            self.bg_pixels[y * bpr..y * bpr + W as usize * 4].copy_from_slice(row);
        }
        self.queue.write_texture(
            TexelCopyTextureInfo { texture: &self.bg_texture, mip_level: 0, origin: Origin3d::ZERO, aspect: TextureAspect::All },
            &self.bg_pixels,
            TexelCopyBufferLayout { offset: 0, bytes_per_row: Some(self.bg_bpr), rows_per_image: Some(H) },
            Extent3d { width: W, height: H, depth_or_array_layers: 1 },
        );
    }

    fn render(&mut self) {
        self.sync_texts();
        self.paint_bg();
        self.viewport.update(&self.queue, Resolution { width: self.config.width, height: self.config.height });

        // 按钮标签定位到各自矩形中心（近似：按字符数估算宽度）
        for (i, b) in self.buttons.iter().enumerate() {
            let (x, y, w, h) = b.rect;
            let label_w = b.label.len() as f32 * 8.5;
            self.texts[T_B0 + i].x = x + (w - label_w) / 2.0;
            self.texts[T_B0 + i].y = y + 14.0;
        }

        let areas: Vec<TextArea> = self.texts.iter().map(|t| TextArea {
            buffer: &t.buffer,
            left: t.x,
            top: t.y,
            scale: 1.0,
            bounds: TextBounds { left: -20, top: -20, right: 700, bottom: 560 },
            default_color: t.color,
            custom_glyphs: &[],
        }).collect();
        self.text_renderer.prepare(
            &self.device, &self.queue, &mut self.font_system, &mut self.atlas,
            &self.viewport, areas, &mut self.swash_cache,
        ).unwrap();

        let frame = self.surface.get_current_texture().unwrap();
        let view = frame.texture.create_view(&TextureViewDescriptor::default());
        let mut enc = self.device.create_command_encoder(&CommandEncoderDescriptor { label: None });
        {
            let mut pass = enc.begin_render_pass(&RenderPassDescriptor {
                label: None,
                color_attachments: &[Some(RenderPassColorAttachment {
                    view: &view,
                    resolve_target: None,
                    ops: Operations { load: LoadOp::Clear(wgpu::Color::BLACK), store: wgpu::StoreOp::Store },
                })],
                depth_stencil_attachment: None,
                timestamp_writes: None,
                occlusion_query_set: None,
            });
            pass.set_pipeline(&self.blit_pipeline);
            pass.set_bind_group(0, &self.bg_bind, &[]);
            pass.draw(0..3, 0..1);
            self.text_renderer.render(&self.atlas, &self.viewport, &mut pass).unwrap();
        }
        self.queue.submit(Some(enc.finish()));
        frame.present();
        self.atlas.trim();
    }

    /// 命中测试：返回鼠标所在按钮索引。
    fn hit_button(&self, x: f32, y: f32) -> Option<usize> {
        self.buttons.iter().position(|b| {
            let (bx, by, bw, bh) = b.rect;
            x >= bx && x <= bx + bw && y >= by && y <= by + bh
        })
    }

    /// 命中输入框区域。
    fn hit_input(&self, x: f32, y: f32) -> bool {
        x >= 28.0 && x <= 732.0 && y >= 336.0 && y <= 380.0
    }

    fn apply_action(&mut self, action: Action) {
        self.model.clicks += 1;
        match action {
            Action::Boost => self.model.count += 10,
            Action::Reset => self.model.count = 0,
            Action::Clear => {
                self.model.committed.clear();
                self.model.preedit.clear();
            }
        }
    }
}

// ---------------- 事件循环 ----------------

struct App {
    state: Option<State>,
}

impl winit::application::ApplicationHandler for App {
    fn resumed(&mut self, el: &winit::event_loop::ActiveEventLoop) {
        if self.state.is_some() {
            return;
        }
        let attrs = Window::default_attributes()
            .with_inner_size(LogicalSize::new(W as f64, H as f64))
            .with_title("PULSE - Rust backend (winit / wgpu / tiny-skia / glyphon / Taffy)")
            .with_resizable(false);
        let window = Arc::new(el.create_window(attrs).unwrap());
        window.set_ime_allowed(true);
        self.state = Some(pollster::block_on(State::new(window)));
    }

    fn window_event(
        &mut self,
        el: &winit::event_loop::ActiveEventLoop,
        _id: winit::window::WindowId,
        event: WindowEvent,
    ) {
        let Some(state) = &mut self.state else { return; };
        match event {
            WindowEvent::Resized(size) => {
                state.config.width = size.width.max(1);
                state.config.height = size.height.max(1);
                state.surface.configure(&state.device, &state.config);
                state.window.request_redraw();
            }
            WindowEvent::RedrawRequested => {
                state.render();
                state.window.request_redraw();
            }
            WindowEvent::CloseRequested => el.exit(),

            WindowEvent::CursorMoved { position, .. } => {
                let (x, y) = (position.x as f32, position.y as f32);
                state.cursor = (x, y);
                let new_hover = state.hit_button(x, y);
                if new_hover != state.hover {
                    state.hover = new_hover;
                    state.window.request_redraw();
                }
            }

            WindowEvent::MouseInput { state: ElementState::Pressed, button: MouseButton::Left, .. } => {
                let (x, y) = state.cursor;
                if let Some(idx) = state.hit_button(x, y) {
                    let action = state.buttons[idx].action;
                    state.apply_action(action);
                    eprintln!("[click] button={:?} count={} clicks={}", action, state.model.count, state.model.clicks);
                    state.window.request_redraw();
                } else if state.hit_input(x, y) {
                    state.model.focused = true;
                    eprintln!("[click] input focus");
                    state.window.request_redraw();
                } else {
                    state.model.focused = false;
                    state.window.request_redraw();
                }
            }

            WindowEvent::Ime(ime) => {
                match ime {
                    Ime::Enabled => {
                        eprintln!("[ime] Enabled");
                        state.model.focused = true;
                        // 把 IME 候选窗定位到输入框右侧
                        state.window.set_ime_cursor_area(
                            winit::dpi::PhysicalPosition::new(56 + state.model.committed.len() as i32 * 10, 350),
                            winit::dpi::PhysicalSize::new(8, 24),
                        );
                    }
                    Ime::Disabled => {
                        eprintln!("[ime] Disabled");
                    }
                    Ime::Preedit(s, cursor) => {
                        eprintln!("[ime] Preedit {:?} cursor={:?}", s, cursor);
                        state.model.preedit = s;
                    }
                    Ime::Commit(s) => {
                        eprintln!("[ime] Commit {:?}", s);
                        state.model.committed.push_str(&s);
                        state.model.preedit.clear();
                    }
                }
                state.window.request_redraw();
            }

            WindowEvent::KeyboardInput {
                event: KeyEvent { state: ElementState::Pressed, logical_key, .. },
                ..
            } => {
                if logical_key == Key::Named(NamedKey::Backspace) && state.model.preedit.is_empty() {
                    // 按 Unicode 标量删除，避免切坏多字节中文
                    state.model.committed.pop();
                    state.window.request_redraw();
                } else if logical_key == Key::Named(NamedKey::Escape) {
                    el.exit();
                }
            }

            _ => {}
        }
    }
}
