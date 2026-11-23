// 技术验证内核：winit 0.30 (IME) + wgpu 25 + tiny-skia (CPU 图形)
// + glyphon 0.9 (cosmic-text shaping / 字体回退) + Taffy 0.5 (布局)
// 目的：验证这套栈在 Mesa 软件渲染下能出窗口、中文正常、IME 事件可渲染。
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
    event::{ElementState, Ime, KeyEvent, WindowEvent},
    event_loop::EventLoop,
    keyboard::{Key, NamedKey},
    window::Window,
};

const W: u32 = 760;
const H: u32 = 640;
const BG_RGB: [u8; 3] = [11, 16, 32];

fn main() {
    let event_loop = EventLoop::new().unwrap();
    event_loop
        .run_app(&mut Application { state: None })
        .unwrap();
}

// ---------- 文本项 ----------
struct TextItem {
    buffer: Buffer,
    x: f32,
    y: f32,
    bounds: (i32, i32),
    color: Color,
}

impl TextItem {
    fn new(
        fs: &mut FontSystem,
        text: &str,
        x: f32,
        y: f32,
        px: f32,
        line_h: f32,
        bounds: (i32, i32),
        color: Color,
        underline: bool,
    ) -> Self {
        let _ = bounds; // 统一用大 bounds，避免 glyphon 局部裁剪误伤字形
        let mut buffer = Buffer::new(fs, Metrics::new(px, line_h));
        buffer.set_size(fs, Some(660.0), Some(80.0));
        let _ = underline; // cosmic-text 0.14 Attrs 无 underline，preedit 用颜色区分
        let attrs = Attrs::new().family(Family::SansSerif);
        buffer.set_text(fs, text, &attrs, Shaping::Advanced);
        buffer.shape_until_scroll(fs, false);
        TextItem { buffer, x, y, bounds: (680, 640), color }
    }

    fn set(&mut self, fs: &mut FontSystem, text: &str, underline: bool) {
        let _ = underline;
        let attrs = Attrs::new().family(Family::SansSerif);
        self.buffer.set_text(fs, text, &attrs, Shaping::Advanced);
        self.buffer.shape_until_scroll(fs, false);
    }
}

// ---------- 窗口状态 ----------
struct State {
    device: wgpu::Device,
    queue: wgpu::Queue,
    surface: wgpu::Surface<'static>,
    config: SurfaceConfiguration,

    // tiny-skia 背景 → GPU 纹理 blit
    bg_texture: wgpu::Texture,
    bg_bind: BindGroup,
    blit_pipeline: RenderPipeline,
    bg_pixels: Vec<u8>,
    bg_bpr: u32,

    // glyphon 文本
    font_system: FontSystem,
    swash_cache: SwashCache,
    viewport: Viewport,
    atlas: TextAtlas,
    text_renderer: TextRenderer,
    texts: Vec<TextItem>,

    // 输入框状态（IME）
    committed: String,
    preedit: String,
    focused: bool,

    // 卡片矩形（Taffy 计算）
    cards: Vec<(f32, f32, f32, f32)>,

    window: Arc<Window>,
}

fn align_up(v: u32, a: u32) -> u32 {
    (v + a - 1) / a * a
}

// tiny-skia 0.11 无 round_rect，手写圆角矩形路径
fn round_rect_path(
    x: f32,
    y: f32,
    w: f32,
    h: f32,
    r: f32,
) -> Option<tiny_skia::Path> {
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

impl State {
    async fn new(window: Arc<Window>) -> Self {
        let size = window.inner_size();

        let instance = Instance::new(&InstanceDescriptor::default());
        let adapter = instance
            .request_adapter(&RequestAdapterOptions::default())
            .await
            .expect("request_adapter");
        eprintln!("[adapter] {:?}", adapter.get_info());
        let (device, queue) = adapter
            .request_device(&DeviceDescriptor::default())
            .await
            .expect("request_device");

        let surface = instance.create_surface(window.clone()).expect("create_surface");
        let swapchain_format = TextureFormat::Bgra8UnormSrgb;
        let config = SurfaceConfiguration {
            usage: TextureUsages::RENDER_ATTACHMENT,
            format: swapchain_format,
            width: size.width,
            height: size.height,
            present_mode: PresentMode::Fifo,
            alpha_mode: CompositeAlphaMode::Opaque,
            view_formats: vec![],
            desired_maximum_frame_latency: 2,
        };
        surface.configure(&device, &config);

        // tiny-skia 背景 pixmap（RGBA8，padding 到 256 对齐）
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

        // blit shader：全屏三角形采样背景纹理
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
"#
                .into(),
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
        let pipeline_layout = device.create_pipeline_layout(&PipelineLayoutDescriptor {
            label: Some("blit-pl"),
            bind_group_layouts: &[&bgl],
            push_constant_ranges: &[],
        });
        let blit_pipeline = device.create_render_pipeline(&RenderPipelineDescriptor {
            label: Some("blit-pipeline"),
            layout: Some(&pipeline_layout),
            vertex: VertexState { module: &shader, entry_point: Some("vs"), buffers: &[], compilation_options: Default::default() },
            primitive: PrimitiveState::default(),
            depth_stencil: None,
            multisample: MultisampleState::default(),
            fragment: Some(FragmentState {
                module: &shader,
                entry_point: Some("fs"),
                compilation_options: Default::default(),
                targets: &[Some(ColorTargetState {
                    format: swapchain_format,
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

        // glyphon 文本渲染器 + 加载中文字体
        let mut font_system = FontSystem::new();
        {
            let db = font_system.db_mut();
            let cjk = "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc";
            if let Err(e) = db.load_font_file(cjk) {
                eprintln!("[font] load {} failed: {:?}", cjk, e);
            }
        }
        let swash_cache = SwashCache::new();
        let cache = Cache::new(&device);
        let viewport = Viewport::new(&device, &cache);
        let mut atlas = TextAtlas::new(&device, &queue, &cache, swapchain_format);
        let text_renderer = TextRenderer::new(&mut atlas, &device, MultisampleState::default(), None);

        let white = Color::rgb(226, 232, 240);
        let cyan = Color::rgb(45, 212, 191);
        let muted = Color::rgb(148, 163, 184);
        let mut texts = vec![
            TextItem::new(&mut font_system, "PULSE  ·  脉冲看板", 28.0, 24.0, 26.0, 34.0, (700, 44), white, false),
            TextItem::new(&mut font_system, "请求量", 44.0, 122.0, 15.0, 22.0, (200, 28), muted, false),
            TextItem::new(&mut font_system, "吞吐量", 272.0, 122.0, 15.0, 22.0, (200, 28), muted, false),
            TextItem::new(&mut font_system, "运行时长", 500.0, 122.0, 15.0, 22.0, (200, 28), muted, false),
            TextItem::new(&mut font_system, "操作员控制台", 44.0, 330.0, 16.0, 24.0, (300, 30), white, false),
            TextItem::new(&mut font_system, "operator", 56.0, 412.0, 18.0, 26.0, (600, 34), white, false),
            TextItem::new(&mut font_system, "拼:", 56.0, 462.0, 14.0, 22.0, (40, 28), cyan, false),
            TextItem::new(&mut font_system, "", 88.0, 462.0, 18.0, 26.0, (560, 34), cyan, true),
        ];

        // IME 模拟注入（沙箱无输入法 daemon 时用环境变量驱动渲染链路验证）
        let committed = std::env::var("GX_COMMIT").unwrap_or_default();
        let preedit = std::env::var("GX_PREEDIT").unwrap_or_default();
        let focused = !committed.is_empty() || !preedit.is_empty();
        if !committed.is_empty() {
            texts[5].set(&mut font_system, &committed, false);
        }
        if !preedit.is_empty() {
            texts[7].set(&mut font_system, &preedit, true);
        }

        // Taffy：算三张卡片位置（row + gap）
        let mut taffy: taffy::TaffyTree<()> = taffy::TaffyTree::new();
        use taffy::prelude::*;
        let card_style = Style {
            size: Size { width: length(0.0), height: length(96.0) },
            flex_grow: 1.0,
            ..Default::default()
        };
        let c1 = taffy.new_leaf(card_style.clone()).unwrap();
        let c2 = taffy.new_leaf(card_style.clone()).unwrap();
        let c3 = taffy.new_leaf(card_style.clone()).unwrap();
        let row = taffy.new_with_children(
            Style {
                display: Display::Flex,
                flex_direction: FlexDirection::Row,
                gap: Size { width: length(14.0), height: length(14.0) },
                size: Size { width: length(704.0), height: length(96.0) },
                padding: Rect {
                    left: length(28.0), right: length(28.0),
                    top: length(0.0), bottom: length(0.0),
                },
                inset: Rect {
                    left: length(0.0), top: length(96.0),
                    right: length(0.0), bottom: length(0.0),
                },
                ..Default::default()
            },
            &[c1, c2, c3],
        ).unwrap();
        taffy.compute_layout(row, Size::MAX_CONTENT).unwrap();
        let mut cards = vec![];
        for n in [c1, c2, c3] {
            let l = taffy.layout(n).unwrap();
            cards.push((l.location.x, l.location.y, l.size.width, l.size.height));
        }
        eprintln!("[taffy] cards = {:?}", cards);

        State {
            device, queue, surface, config,
            bg_texture, bg_bind, blit_pipeline, bg_pixels, bg_bpr: bpr,
            font_system, swash_cache, viewport, atlas, text_renderer, texts,
            committed, preedit, focused, cards, window,
        }
    }

    // tiny-skia 画背景（深墨蓝 + 卡片 + 进度条 + 输入框框）
    fn paint_bg(&mut self) {
        use tiny_skia::{Color, FillRule, Paint, Pixmap, Stroke, Transform};
        let bpr = self.bg_bpr as usize;
        let mut pm = Pixmap::new(W, H).unwrap();
        pm.fill(Color::from_rgba8(BG_RGB[0], BG_RGB[1], BG_RGB[2], 255));
        let mut p = Paint::default();
        p.set_color(Color::from_rgba8(20, 27, 51, 255));
        p.anti_alias = true;
        let radius = 12.0;
        // 卡片
        for &(x, y, w, h) in &self.cards {
            if let Some(path) = round_rect_path(x, y + 96.0, w, h, radius) {
                pm.fill_path(&path, &p, FillRule::Winding, Transform::identity(), None);
            }
        }
        // 进度条底 + 填充
        if let Some(path) = round_rect_path(28.0, 250.0, 704.0, 14.0, 7.0) {
            let mut back = Paint::default();
            back.set_color(Color::from_rgba8(30, 41, 69, 255));
            pm.fill_path(&path, &back, FillRule::Winding, Transform::identity(), None);
        }
        if let Some(path) = round_rect_path(28.0, 250.0, 420.0, 14.0, 7.0) {
            let mut fill = Paint::default();
            fill.set_color(Color::from_rgba8(45, 212, 191, 255));
            pm.fill_path(&path, &fill, FillRule::Winding, Transform::identity(), None);
        }
        // 输入框框（聚焦时青色描边）
        if let Some(path) = round_rect_path(44.0, 402.0, 672.0, 42.0, 8.0) {
            let mut boxfill = Paint::default();
            boxfill.set_color(Color::from_rgba8(15, 23, 42, 255));
            pm.fill_path(&path, &boxfill, FillRule::Winding, Transform::identity(), None);
            let stroke = Stroke { width: if self.focused { 2.0 } else { 1.0 }, ..Default::default() };
            let mut sp = Paint::default();
            sp.set_color(if self.focused {
                Color::from_rgba8(45, 212, 191, 255)
            } else {
                Color::from_rgba8(71, 85, 105, 255)
            });
            pm.stroke_path(&path, &sp, &stroke, Transform::identity(), None);
        }
        // 拷回 padded buffer
        let src = pm.data();
        for y in 0..H as usize {
            let srow = &src[y * W as usize * 4..(y + 1) * W as usize * 4];
            self.bg_pixels[y * bpr..y * bpr + W as usize * 4].copy_from_slice(srow);
        }
        self.queue.write_texture(
            TexelCopyTextureInfo { texture: &self.bg_texture, mip_level: 0, origin: Origin3d::ZERO, aspect: TextureAspect::All },
            &self.bg_pixels,
            TexelCopyBufferLayout { offset: 0, bytes_per_row: Some(self.bg_bpr), rows_per_image: Some(H) },
            Extent3d { width: W, height: H, depth_or_array_layers: 1 },
        );
    }

    fn render(&mut self) {
        self.paint_bg();
        self.viewport.update(&self.queue, Resolution { width: self.config.width, height: self.config.height });
        let areas: Vec<TextArea> = self.texts.iter().map(|t| TextArea {
            buffer: &t.buffer,
            left: t.x,
            top: t.y,
            scale: 1.0,
            bounds: TextBounds { left: -20, top: -20, right: t.bounds.0, bottom: t.bounds.1 },
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
                    view: &view, resolve_target: None,
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

    fn set_input(&mut self, text: &str, underline: bool) {
        self.texts[5].set(&mut self.font_system, &self.committed, false);
        self.texts[7].set(&mut self.font_system, text, underline);
    }
}

// ---------- 应用 / 事件 ----------
struct Application {
    state: Option<State>,
}

impl winit::application::ApplicationHandler for Application {
    fn resumed(&mut self, el: &winit::event_loop::ActiveEventLoop) {
        if self.state.is_some() {
            return;
        }
        let attrs = Window::default_attributes()
            .with_inner_size(LogicalSize::new(W as f64, H as f64))
            .with_title("PULSE - Rust backend (winit+wgpu+tiny-skia+glyphon)")
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
            WindowEvent::Ime(ime) => {
                match ime {
                    Ime::Enabled => {
                        eprintln!("[ime] Enabled");
                        state.focused = true;
                    }
                    Ime::Disabled => {
                        eprintln!("[ime] Disabled");
                        state.focused = false;
                    }
                    Ime::Preedit(s, cursor) => {
                        eprintln!("[ime] Preedit {:?} cursor={:?}", s, cursor);
                        state.preedit = s.clone();
                        state.set_input(&s, true);
                    }
                    Ime::Commit(s) => {
                        eprintln!("[ime] Commit {:?}", s);
                        state.committed.push_str(&s);
                        state.preedit.clear();
                        state.set_input("", false);
                    }
                }
                state.window.request_redraw();
            }
            WindowEvent::KeyboardInput {
                event: KeyEvent { state: ElementState::Pressed, logical_key, .. },
                ..
            } => {
                if logical_key == Key::Named(NamedKey::Backspace) {
                    state.committed.pop();
                    let pe = state.preedit.clone();
                    state.set_input(&pe, !pe.is_empty());
                    state.window.request_redraw();
                } else if logical_key == Key::Named(NamedKey::Escape) {
                    el.exit();
                }
            }
            _ => {}
        }
    }
}
