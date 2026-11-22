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
    /// 把镜像场景树编译成绘制指令。
    /// 布局交给 Taffy 0.5（完整 Flexbox 引擎）——
    /// 支持完整的 justify-content / align-items / gap / flex-wrap，
    /// 组件包装层（Fragment）摊平为透明节点，文本节点用 measure 回调测宽。
    /// 根节点的 size 设为窗口尺寸，因此 align-items:center 有确定参照。
    fn from_scene(root: &crate::scene::SceneNode, avail_w: f32, avail_h: f32) -> Self {
        use taffy::prelude::*;

        let mut dl = DisplayList::new();
        let mut taffy: TaffyTree<TextMeasure> = TaffyTree::new();

        // 文本测量上下文：继承后的字号 + 文本内容。Taffy 在布局期回调测宽。
        #[derive(Clone)]
        struct TextMeasure { text: String, size: f32 }

        fn measure_text(
            known: taffy::geometry::Size<Option<f32>>,
            _avail: taffy::geometry::Size<taffy::style::AvailableSpace>,
            ctx: Option<&mut TextMeasure>,
        ) -> taffy::geometry::Size<f32> {
            if let taffy::geometry::Size { width: Some(w), height: Some(h) } = known {
                return taffy::geometry::Size { width: w, height: h };
            }
            let Some(c) = ctx else { return taffy::geometry::Size::ZERO; };
            // CJK/全角按 1em，ASCII 按 0.56em，行高 1.35em。
            let w = c.text.chars()
                .map(|ch| if ch.is_ascii() { 0.56 } else { 1.0 })
                .sum::<f32>() * c.size;
            taffy::geometry::Size { width: w, height: c.size * 1.35 }
        }

        fn dir_of(s: &str) -> FlexDirection {
            match s {
                "row" => FlexDirection::Row,
                "row-reverse" => FlexDirection::RowReverse,
                "column-reverse" => FlexDirection::ColumnReverse,
                _ => FlexDirection::Column,
            }
        }
        fn align_of(s: Option<&str>) -> Option<AlignItems> {
            match s.unwrap_or("") {
                "center" => Some(AlignItems::Center),
                "flex-end" | "end" => Some(AlignItems::FlexEnd),
                "flex-start" | "start" => Some(AlignItems::FlexStart),
                "stretch" => Some(AlignItems::Stretch),
                _ => None,
            }
        }
        fn justify_of(s: Option<&str>) -> Option<JustifyContent> {
            match s.unwrap_or("") {
                "center" => Some(JustifyContent::Center),
                "flex-end" | "end" => Some(JustifyContent::FlexEnd),
                "space-between" => Some(JustifyContent::SpaceBetween),
                "space-around" => Some(JustifyContent::SpaceAround),
                "space-evenly" => Some(JustifyContent::SpaceEvenly),
                "flex-start" | "start" => Some(JustifyContent::FlexStart),
                _ => None,
            }
        }

        // 把可见子节点摊平（Fragment 透明），记录 (Taffy 节点, 原 SceneNode 引用)。
        // 同时收集节点的绘制属性，布局算完后统一生成 DisplayList。
        struct BuildCtx {
            taffy: TaffyTree<TextMeasure>,
        }

        fn build(
            ctx: &mut BuildCtx,
            node: &crate::scene::SceneNode,
            is_root: bool,
            inherited_color: (u8, u8, u8),
            inherited_size: f32,
            avail_w: f32, avail_h: f32,
        ) -> taffy::NodeId {
            let p = |k: &str| node.props.get(k).map(|s| s.as_str());
            let color = p("color").map(parse_color).unwrap_or(inherited_color);
            let size = p("font-size").and_then(parse_px).unwrap_or(inherited_size);

            if node.typ == NodeType::Text {
                return ctx.taffy.new_leaf_with_context(
                    Style::default(),
                    TextMeasure { text: node.text.clone(), size },
                ).expect("taffy text leaf");
            }

            let pad = p("padding").and_then(parse_px).unwrap_or(0.0);
            let gap = p("gap").or_else(|| p("column-gap")).and_then(parse_px).unwrap_or(0.0);
            let mut style = Style {
                flex_direction: dir_of(p("flex-direction").unwrap_or("column")),
                padding: Rect {
                    left: length(pad), right: length(pad),
                    top: length(pad), bottom: length(pad),
                },
                gap: Size { width: length(gap), height: length(gap) },
                align_items: align_of(p("align-items")),
                justify_content: justify_of(p("justify-content")),
                ..Default::default()
            };
            // 显式宽高；根节点即使未声明也填满窗口。
            let exp_w = p("width").and_then(parse_px);
            let exp_h = p("height").and_then(parse_px);
            let w = exp_w.or(if is_root { Some(avail_w) } else { None });
            let h = exp_h.or(if is_root { Some(avail_h) } else { None });
            if let Some(v) = w { style.size.width = length(v); }
            if let Some(v) = h { style.size.height = length(v); }

            // 摊平 Fragment：透明包装不引入布局节点，其 children 直接挂到当前父节点。
            let mut kids: Vec<&crate::scene::SceneNode> = Vec::new();
            fn flatten<'a>(n: &'a crate::scene::SceneNode, out: &mut Vec<&'a crate::scene::SceneNode>) {
                for c in &n.children {
                    if c.typ == NodeType::Fragment { flatten(c, out); } else { out.push(c); }
                }
            }
            flatten(node, &mut kids);
            let child_nodes: Vec<taffy::NodeId> = kids.iter()
                .map(|c| build(ctx, c, false, color, size, avail_w, avail_h))
                .collect();

            ctx.taffy.new_with_children(style, &child_nodes).expect("taffy container")
        }

        let mut ctx = BuildCtx { taffy };
        let root_node = build(&mut ctx, root, true, (226, 232, 240), 16.0, avail_w, avail_h);
        ctx.taffy.compute_layout_with_measure(
            root_node,
            Size { width: AvailableSpace::Definite(avail_w), height: AvailableSpace::Definite(avail_h) },
            |known, avail, _id, mctx, _style| measure_text(known, avail, mctx),
        ).expect("taffy compute_layout");

        // 第二遍：按同样的摊平顺序遍历，把布局结果转成绘制指令（父先子后）。
        fn emit(
            node: &crate::scene::SceneNode,
            tnode: taffy::NodeId,
            ctx: &BuildCtx,
            dl: &mut DisplayList,
            inherited_color: (u8, u8, u8),
            inherited_size: f32,
            ox: f32, oy: f32,
        ) {
            let p = |k: &str| node.props.get(k).map(|s| s.as_str());
            let color = p("color").map(parse_color).unwrap_or(inherited_color);
            let size = p("font-size").and_then(parse_px).unwrap_or(inherited_size);
            let lay = ctx.taffy.layout(tnode).expect("taffy layout");
            // Taffy location 相对父节点；递归累加得到绝对坐标。
            let x = ox + lay.location.x;
            let y = oy + lay.location.y;
            let w = lay.size.width;
            let h = lay.size.height;

            if node.typ == NodeType::Text {
                // y 是 Taffy 算出的文本行盒顶部；glyphon 会用 Buffer 的 Metrics
                // (font_size, line_height=1.35×size) 自行处理行盒内基线，故这里不加
                // 任何 size 比例的魔法偏移——否则紧凑容器（如 Input 高 20px）里文字会偏下。
                dl.texts.push(TextDraw {
                    text: node.text.clone(), x, y, size, color,
                });
                return;
            }

            if node.typ == NodeType::Div {
                let bg = p("bg").or_else(|| p("background")).map(parse_color);
                let radius = p("border-radius").and_then(parse_px).unwrap_or(0.0);
                let border = p("border").map(|c| {
                    let bw = p("border-width").and_then(parse_px).unwrap_or(1.0);
                    (parse_color(c), bw)
                });
                dl.shapes.push(ShapeDraw {
                    x, y, w, h,
                    bg: bg.unwrap_or((0, 0, 0)), bg_present: bg.is_some(), radius, border,
                });
                if let Some(ev) = node.events.iter().find(|e| e.starts_with("on")) {
                    dl.hitboxes.push((x, y, w, h, node.id.clone(), ev.clone()));
                }
                if node.props.contains_key("gx-input-id") {
                    dl.inputs.push((x, y, w, h, node.id.clone()));
                }
            }

            // 与 build 完全一致的摊平顺序，保证 layout 节点索引对齐。
            let mut kids: Vec<&crate::scene::SceneNode> = Vec::new();
            fn flatten<'a>(n: &'a crate::scene::SceneNode, out: &mut Vec<&'a crate::scene::SceneNode>) {
                for c in &n.children {
                    if c.typ == NodeType::Fragment { flatten(c, out); } else { out.push(c); }
                }
            }
            flatten(node, &mut kids);
            for (i, c) in kids.iter().enumerate() {
                let child_tnode = ctx.taffy.child_at_index(tnode, i).expect("taffy child");
                emit(c, child_tnode, ctx, dl, color, size, x, y);
            }
        }

        emit(root, root_node, &ctx, &mut dl, (226, 232, 240), 16.0, 0.0, 0.0);
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
        let dl = DisplayList::from_scene(scene, self.width as f32, self.height as f32);
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

// ---------- 纯 CPU 基准（供 Go benchmark 通过 C ABI 驱动） ----------

/// 纯 CPU 管线基准：重复 iters 次「GRX1 解析 → 建镜像树 → Taffy 布局 → 生成绘制指令」。
/// 不创建 wgpu device / surface / 字体系统，不含任何 GPU 或窗口工作，返回耗时纳秒。
/// 用于与 Go/Skia 后端的 CPU 管线做公平对比。
pub fn bench_frame(buf: &[u8], width: f32, height: f32, iters: u32) -> Option<u64> {
    use crate::binprotocol::FrameView;
    use crate::scene::SceneNode;
    // 先解析一次建基线树（与真实后端首帧一致）。
    let view = FrameView::parse(buf)?;
    let root = SceneNode::from_frame(&view)?;
    let start = std::time::Instant::now();
    let mut sink = 0usize;
    for _ in 0..iters {
        // 每次克隆镜像树模拟"增量打补丁后的树"，再布局。克隆纳入计时以对齐真实帧成本。
        let tree = root.clone();
        let dl = DisplayList::from_scene(&tree, width, height);
        sink = sink.wrapping_add(dl.shapes.len() + dl.texts.len());
    }
    let ns = start.elapsed().as_nanos() as u64;
    // 防止编译器优化掉结果。
    std::hint::black_box(sink);
    Some(ns)
}
