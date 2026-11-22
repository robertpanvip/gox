//! 渲染内核：消费二进制协议 FrameView（零拷贝视图），用 tiny-skia 图形 + glyphon 文本渲染。
//! 布局采用与 host flex 语义一致的主轴排布（fragment 透明透传）。

use crate::binprotocol::NodeType;
use crate::protocol::InputEvent;
use glyphon::{
    Attrs, Buffer, Cache, Color, Family, FontSystem, Metrics, Resolution, Shaping, SwashCache,
    TextArea, TextAtlas, TextBounds, TextRenderer, Viewport,
};
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
use winit::window::Window;

fn align_up(v: u32, a: u32) -> u32 {
    (v + a - 1) / a * a
}

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

fn parse_color(s: &str) -> (u8, u8, u8) {
    let s = s.trim();
    if let Some(hex) = s.strip_prefix('#') {
        let h = if hex.len() == 3 {
            hex.chars().map(|c| format!("{}{}", c, c)).collect::<String>()
        } else {
            hex.to_string()
        };
        if h.len() >= 6 {
            return (
                u8::from_str_radix(&h[0..2], 16).unwrap_or(255),
                u8::from_str_radix(&h[2..4], 16).unwrap_or(255),
                u8::from_str_radix(&h[4..6], 16).unwrap_or(255),
            );
        }
    }
    match s.to_lowercase().as_str() {
        "white" => (255, 255, 255),
        "black" => (0, 0, 0),
        _ => (226, 232, 240),
    }
}

fn parse_px(s: &str) -> Option<f32> {
    s.trim().trim_end_matches("px").trim().parse::<f32>().ok()
}

// ---------- DisplayList ----------

#[derive(Clone)]
struct TextDraw {
    text: String,
    x: f32,
    y: f32,
    size: f32,
    color: (u8, u8, u8),
}

#[derive(Clone)]
struct ShapeDraw {
    x: f32,
    y: f32,
    w: f32,
    h: f32,
    bg: (u8, u8, u8),
    bg_present: bool,
    radius: f32,
    border: Option<((u8, u8, u8), f32)>,
}

#[derive(Clone)]
struct DisplayList {
    shapes: Vec<ShapeDraw>,
    texts: Vec<TextDraw>,
    /// (x, y, w, h, node_id, event)
    hitboxes: Vec<(f32, f32, f32, f32, String, String)>,
    /// (x, y, w, h, node_id)
    inputs: Vec<(f32, f32, f32, f32, String)>,
}

impl DisplayList {
    fn new() -> Self {
        DisplayList { shapes: vec![], texts: vec![], hitboxes: vec![], inputs: vec![] }
    }

    /// 把镜像场景树编译成绘制指令（主轴排布，fragment 透明透传）。
    fn from_scene(root: &crate::scene::SceneNode) -> Self {
        let mut dl = DisplayList::new();
        // 估算文本占用尺寸：CJK/全角按 1em，ASCII 按 0.56em，行高 1.35em。
        // 布局引擎（Taffy 精简版）本应返回精确 measure，这里给零深度文本一个近似值，
        // 保证父节点主轴能推进，避免所有文本堆叠在同一坐标。
        fn text_size(text: &str, size: f32) -> (f32, f32) {
            let w = text.chars().map(|c| {
                if c.is_ascii() { 0.56 } else { 1.0 }
            }).sum::<f32>() * size;
            (w, size * 1.35)
        }

        // 返回节点实际占用尺寸 (w, h)，供父节点沿主轴推进游标。
        // 难点：包装型 Div（卡片内的 label/value 容器）没有显式宽高，其尺寸应由
        // 子树内容回流得到，否则父级主轴不推进 → 文本全部堆叠在同一坐标。
        fn walk(node: &crate::scene::SceneNode, ox: f32, oy: f32, dl: &mut DisplayList) -> (f32, f32) {
            let prop = |k: &str| node.props.get(k).map(|s| s.as_str());
            let exp_w = prop("width").and_then(parse_px).unwrap_or(0.0);
            let exp_h = prop("height").and_then(parse_px).unwrap_or(0.0);
            let pad = prop("padding").and_then(parse_px).unwrap_or(0.0);
            let dir = prop("flex-direction").unwrap_or("column");
            let gap = prop("gap")
                .or_else(|| if dir.starts_with("row") { prop("column-gap") } else { prop("row-gap") })
                .and_then(parse_px).unwrap_or(0.0);
            let is_row = dir == "row" || dir == "row-reverse";

            let size = prop("font-size").and_then(parse_px).unwrap_or(16.0);
            let color = prop("color").map(parse_color).unwrap_or((226, 232, 240));

            // 文本叶子：直接绘制并返回估算尺寸。
            if node.typ == NodeType::Text {
                dl.texts.push(TextDraw {
                    text: node.text.clone(), x: ox, y: oy + size * 0.2, size, color,
                });
                return text_size(&node.text, size);
            }

            // 容器：先占位 push 自身 shape（保持"父先子后"绘制顺序，父背景才不会盖住子），
            // 再递归子树收集内容尺寸，最后回填自身宽高。
            let shape_idx = if node.typ == NodeType::Div {
                let bg = prop("bg").or_else(|| prop("background")).map(parse_color);
                let radius = prop("border-radius").and_then(parse_px).unwrap_or(0.0);
                let border = prop("border").map(|c| {
                    let bw = prop("border-width").and_then(parse_px).unwrap_or(1.0);
                    (parse_color(c), bw)
                });
                let idx = dl.shapes.len();
                dl.shapes.push(ShapeDraw {
                    x: ox, y: oy, w: 0.0, h: 0.0, // 占位，递归后回填
                    bg: bg.unwrap_or((0, 0, 0)),
                    bg_present: bg.is_some(),
                    radius, border,
                });
                Some(idx)
            } else {
                None
            };

            let mut cx = ox + pad;
            let mut cy = oy + pad;
            let mut content_w = 0.0f32;
            let mut content_h = 0.0f32;
            for child in &node.children {
                let (cw, ch) = walk(child, cx, cy, dl);
                if is_row {
                    cx += cw + gap;
                    content_w = cx - ox - pad;
                    content_h = content_h.max(ch);
                } else {
                    cy += ch + gap;
                    content_h = cy - oy - pad;
                    content_w = content_w.max(cw);
                }
            }
            if !node.children.is_empty() {
                if is_row { content_w -= gap; } else { content_h -= gap; }
            }
            // 显式宽高优先；否则用内容尺寸 + 内边距。
            let used_w = if exp_w > 0.0 { exp_w } else { content_w + pad * 2.0 };
            let used_h = if exp_h > 0.0 { exp_h } else { content_h + pad * 2.0 };

            if let Some(idx) = shape_idx {
                dl.shapes[idx].w = used_w;
                dl.shapes[idx].h = used_h;
                if let Some(ev) = node.events.iter().find(|e| e.starts_with("on")) {
                    dl.hitboxes.push((ox, oy, used_w, used_h, node.id.clone(), ev.clone()));
                }
                if node.props.contains_key("gx-input-id") {
                    dl.inputs.push((ox, oy, used_w, used_h, node.id.clone()));
                }
            }
            (used_w, used_h)
        }
        walk(root, 0.0, 0.0, &mut dl);
        dl
    }

    /// 命中测试：面积最小的包含点 (x,y) 的 hitbox 优先。
    fn hit(&self, x: f32, y: f32) -> Option<&(f32, f32, f32, f32, String, String)> {
        self.hitboxes
            .iter()
            .filter(|(bx, by, bw, bh, _, _)| x >= *bx && x <= bx + bw && y >= *by && y <= by + bh)
            .min_by(|a, b| (a.2 * a.3).partial_cmp(&(b.2 * b.3)).unwrap_or(std::cmp::Ordering::Equal))
    }
}

// ---------- Renderer ----------

pub struct Renderer {
    device: wgpu::Device,
    queue: wgpu::Queue,
    surface: wgpu::Surface<'static>,
    config: SurfaceConfiguration,
    bg_texture: wgpu::Texture,
    bg_bind: BindGroup,
    blit_pipeline: RenderPipeline,
    bg_pixels: Vec<u8>,
    bg_bpr: u32,
    width: u32,
    height: u32,

    font_system: FontSystem,
    swash_cache: SwashCache,
    viewport: Viewport,
    atlas: TextAtlas,
    text_renderer: TextRenderer,

    /// 常驻镜像场景树（Zed 式）：首帧快照建立，后续增量动作打补丁
    scene: Option<crate::scene::SceneNode>,
    /// 窗口背景色
    bg: (u8, u8, u8),
    /// 最近一次 DisplayList（供命中测试）
    display: DisplayList,
    /// 最近光标位置（MouseInput 事件不带坐标）
    cursor: (f32, f32),
    window: std::sync::Arc<Window>,
}

impl Renderer {
    pub async fn new(window: std::sync::Arc<Window>) -> Self {
        let size = window.inner_size();
        let w = size.width.max(1);
        let h = size.height.max(1);
        let instance = Instance::new(&InstanceDescriptor::default());
        let adapter = instance
            .request_adapter(&RequestAdapterOptions::default())
            .await
            .expect("request_adapter");
        eprintln!("[rust-backend] adapter: {} ({:?})", adapter.get_info().name, adapter.get_info().backend);
        let (device, queue) = adapter
            .request_device(&DeviceDescriptor::default())
            .await
            .expect("request_device");
        let surface = instance.create_surface(window.clone()).expect("create_surface");
        let fmt = TextureFormat::Bgra8UnormSrgb;
        let config = SurfaceConfiguration {
            usage: TextureUsages::RENDER_ATTACHMENT,
            format: fmt,
            width: w,
            height: h,
            present_mode: PresentMode::Fifo,
            alpha_mode: CompositeAlphaMode::Opaque,
            view_formats: vec![],
            desired_maximum_frame_latency: 2,
        };
        surface.configure(&device, &config);

        let bpr = align_up(w * 4, 256);
        let bg_pixels = vec![0u8; (bpr * h) as usize];
        let bg_texture = device.create_texture(&TextureDescriptor {
            label: Some("bg"),
            size: Extent3d { width: w, height: h, depth_or_array_layers: 1 },
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

        let mut font_system = FontSystem::new();
        for path in [
            "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
            "/usr/share/fonts/truetype/noto/NotoSans-Regular.ttf",
        ] {
            if let Err(e) = font_system.db_mut().load_font_file(path) {
                eprintln!("[rust-backend] font {} : {:?}", path, e);
            }
        }
        let swash_cache = SwashCache::new();
        let cache = Cache::new(&device);
        let viewport = Viewport::new(&device, &cache);
        let mut atlas = TextAtlas::new(&device, &queue, &cache, fmt);
        let text_renderer = TextRenderer::new(&mut atlas, &device, MultisampleState::default(), None);

        Renderer {
            device, queue, surface, config,
            bg_texture, bg_bind, blit_pipeline, bg_pixels, bg_bpr: bpr, width: w, height: h,
            font_system, swash_cache, viewport, atlas, text_renderer,
            scene: None,
            bg: (11, 16, 32),
            display: DisplayList::new(),
            cursor: (0.0, 0.0),
            window,
        }
    }

    /// 注入一帧协议数据：GRX1 全量快照重建镜像树；GRX2 增量动作打补丁。
    /// 返回是否成功应用（供日志/观测）。
    pub fn apply_frame(&mut self, bytes: &[u8]) -> bool {
        use crate::binprotocol::FrameView;
        use crate::scene::{DeltaView, SceneNode};
        if let Some(view) = FrameView::parse(bytes) {
            // 全量快照：建镜像树
            if let Some(root) = SceneNode::from_frame(&view) {
                self.bg = root
                    .props
                    .get("bg")
                    .map(|s| parse_color(s))
                    .unwrap_or((11, 16, 32));
                self.scene = Some(root);
                return true;
            }
        } else if let Some(delta) = DeltaView::parse(bytes) {
            // 增量动作：按 nodeID 打补丁
            if let Some(root) = self.scene.as_mut() {
                delta.apply(root);
                return true;
            }
        }
        false
    }

    pub fn set_cursor(&mut self, x: f32, y: f32) {
        self.cursor = (x, y);
    }

    pub fn cursor(&self) -> (f32, f32) {
        self.cursor
    }

    fn paint_shapes(&mut self, dl: &DisplayList, bg: (u8, u8, u8)) {
        use tiny_skia::{Color, FillRule, Paint, Pixmap, Stroke, Transform};
        let bpr = self.bg_bpr as usize;
        let mut pm = Pixmap::new(self.width, self.height).unwrap();
        pm.fill(Color::from_rgba8(bg.0, bg.1, bg.2, 255));
        for s in &dl.shapes {
            if s.w <= 0.0 || s.h <= 0.0 {
                continue;
            }
            if let Some(path) = round_rect_path(s.x, s.y, s.w, s.h, s.radius) {
                if s.bg_present {
                    let mut p = Paint::default();
                    p.set_color(Color::from_rgba8(s.bg.0, s.bg.1, s.bg.2, 255));
                    p.anti_alias = true;
                    pm.fill_path(&path, &p, FillRule::Winding, Transform::identity(), None);
                }
                if let Some((c, bw)) = s.border {
                    let mut p = Paint::default();
                    p.set_color(Color::from_rgba8(c.0, c.1, c.2, 255));
                    pm.stroke_path(
                        &path,
                        &p,
                        &Stroke { width: bw, ..Default::default() },
                        Transform::identity(),
                        None,
                    );
                }
            }
        }
        let src = pm.data();
        for y in 0..self.height as usize {
            let row = &src[y * self.width as usize * 4..(y + 1) * self.width as usize * 4];
            self.bg_pixels[y * bpr..y * bpr + self.width as usize * 4].copy_from_slice(row);
        }
        self.queue.write_texture(
            TexelCopyTextureInfo { texture: &self.bg_texture, mip_level: 0, origin: Origin3d::ZERO, aspect: TextureAspect::All },
            &self.bg_pixels,
            TexelCopyBufferLayout { offset: 0, bytes_per_row: Some(self.bg_bpr), rows_per_image: Some(self.height) },
            Extent3d { width: self.width, height: self.height, depth_or_array_layers: 1 },
        );
    }

    pub fn render(&mut self) {
        let Some(scene) = self.scene.as_ref() else { return; };
        let dl = DisplayList::from_scene(scene);
        let bg = self.bg;
        self.display = dl.clone();
        self.paint_shapes(&dl, bg);

        // 文本：每个 text 一个 buffer
        let mut buffers: Vec<Buffer> = Vec::new();
        for t in &dl.texts {
            let mut b = Buffer::new(&mut self.font_system, Metrics::new(t.size, t.size * 1.35));
            b.set_size(&mut self.font_system, Some(700.0), Some(80.0));
            let attrs = Attrs::new().family(Family::SansSerif);
            b.set_text(&mut self.font_system, &t.text, &attrs, Shaping::Advanced);
            b.shape_until_scroll(&mut self.font_system, false);
            buffers.push(b);
        }
        self.viewport.update(&self.queue, Resolution { width: self.config.width, height: self.config.height });
        let areas: Vec<TextArea> = dl.texts.iter().zip(buffers.iter()).map(|(t, b)| TextArea {
            buffer: b,
            left: t.x,
            top: t.y,
            scale: 1.0,
            bounds: TextBounds { left: -20, top: -20, right: 720, bottom: self.height as i32 },
            default_color: Color::rgb(t.color.0, t.color.1, t.color.2),
            custom_glyphs: &[],
        }).collect();
        self.text_renderer.prepare(
            &self.device, &self.queue, &mut self.font_system, &mut self.atlas,
            &self.viewport, areas, &mut self.swash_cache,
        ).unwrap();

        let f = self.surface.get_current_texture().unwrap();
        let view = f.texture.create_view(&TextureViewDescriptor::default());
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
        f.present();
        self.atlas.trim();
    }

    /// 点击命中 → 生成 click 事件 JSON
    pub fn hit_click(&self, x: f32, y: f32) -> Option<InputEvent> {
        self.display.hit(x, y).map(|(_, _, _, _, id, ev)| InputEvent {
            kind: "click".into(),
            node_id: id.clone(),
            event: ev.clone(),
            text: String::new(),
            x,
            y,
        })
    }

    /// 点击是否落在输入框 → 生成 focus 事件
    pub fn hit_input(&self, x: f32, y: f32) -> Option<InputEvent> {
        self.display
            .inputs
            .iter()
            .filter(|(bx, by, bw, bh, _)| x >= *bx && x <= bx + bw && y >= *by && y <= by + bh)
            .min_by(|a, b| (a.2 * a.3).partial_cmp(&(b.2 * b.3)).unwrap_or(std::cmp::Ordering::Equal))
            .map(|(_, _, _, _, id)| InputEvent {
                kind: "focus".into(),
                node_id: id.clone(),
                event: String::new(),
                text: String::new(),
                x,
                y,
            })
    }

    pub fn resize(&mut self, w: u32, h: u32) {
        // 简化：仅更新 surface 配置（种子 demo 固定尺寸）
        self.config.width = w.max(1);
        self.config.height = h.max(1);
        self.surface.configure(&self.device, &self.config);
    }

    pub fn window(&self) -> &Window {
        &self.window
    }
}
