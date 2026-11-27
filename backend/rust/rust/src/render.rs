//! 渲染内核：消费二进制协议 FrameView（零拷贝视图），用 tiny-skia 图形 + glyphon 文本渲染。
//! 布局采用与 host flex 语义一致的主轴排布（fragment 透明透传）。

use crate::paint::*;
use crate::protocol::InputEvent;
use crate::util::*;
use glyphon::{
    Attrs, Buffer, Cache, Color, Family, FontSystem, Metrics, Resolution, Shaping, SwashCache,
    TextArea, TextAtlas, TextBounds, TextRenderer, Viewport, Weight,
};
use raw_window_handle::{RawDisplayHandle, RawWindowHandle};
use wgpu::{
    CompositeAlphaMode, DeviceDescriptor, Instance, InstanceDescriptor, MultisampleState,
    PresentMode, RequestAdapterOptions, SurfaceConfiguration, TextureFormat, TextureUsages,
};

/// 平台无关 Surface 提供者（BACKEND_ARCHITECTURE_V2 §七）：
/// "Renderer 可以跨平台，Surface 必须平台原生化"。渲染内核不 import 任何
/// 窗口库——winit / Android NDK / iOS CAMetalLayer / 嵌入式宿主各自实现本
/// trait，把原始句柄与像素尺寸注入 Renderer。
///
/// 安全契约：返回的句柄在 Renderer 存活期间必须保持有效。Renderer 同时持有
/// provider 本体（Arc<dyn SurfaceProvider>），且 surface 字段先于 provider
/// 字段声明（Rust 按声明顺序 drop：surface 先销毁，句柄来源最后销毁）。
pub trait SurfaceProvider: Send + Sync {
    /// 当前像素尺寸（物理像素；0 会被渲染内核钳到 1）。
    fn pixel_size(&self) -> (u32, u32);
    /// 当前逻辑尺寸（点；E2E 调试导出/命中坐标与逻辑布局对齐）。
    /// 默认与物理尺寸一致（scale=1 的平台/无头提供者）。
    fn logical_size(&self) -> (f32, f32) {
        let (w, h) = self.pixel_size();
        (w as f32, h as f32)
    }
    /// 平台 display 原始句柄（wgpu SurfaceTargetUnsafe::RawHandle 用）。
    fn raw_display_handle(&self) -> RawDisplayHandle;
    /// 平台 window/surface 原始句柄。
    fn raw_window_handle(&self) -> RawWindowHandle;
    /// 请求一次重绘（winit request_redraw / Choreographer / CADisplayLink）。
    /// 渲染内核不关心具体机制，只负责"脏了就喊一声"。
    fn request_redraw(&self);
}

/// GPU 纹理缓存条目（Renderer 侧一级缓存）：按内容 key 索引，data 持 Arc
/// 跨帧保活；GPU/CPU 后端还有 (id, key) 二级去重，内容未变零上传。
struct TexEntry {
    id: u32,
    w: u32,
    h: u32,
    premul: bool,
    data: std::sync::Arc<Vec<u8>>,
    /// true=图片（key=源 Pixmap 指针，跨帧常驻）；false=视频帧/canvas 光栅
    /// （key 含帧序/代数，本帧不再引用即淘汰）。
    persistent: bool,
}

pub struct Renderer {
    pub(crate) device: wgpu::Device,
    pub(crate) queue: wgpu::Queue,
    config: SurfaceConfiguration,
    /// 本 surface 支持的 present 模式集合（创建期查询缓存）。present 线程化后
    /// 由 take_surface_parts 随 surface 一并移交 PresentHub（VSyncControl 依据）。
    present_modes: Vec<PresentMode>,
    /// wgpu surface（P4.6 present 线程化：创建后由 take_surface_parts 移交
    /// 专用渲染线程，本端 prepare 不再触碰）。
    surface: Option<wgpu::Surface<'static>>,
    /// GPU quad 管线（gx_gpu crate：三端统一 WGSL，GPU 优先）。
    pub(crate) gpu: gx_gpu::QuadGpu,
    /// 纹理一级缓存：内容 key → 条目。
    tex_cache: std::collections::HashMap<u64, TexEntry>,
    /// 逻辑键（图片源指针 / 视频节点 id 哈希 / canvas 内容哈希）→ 纹理 id。
    tex_ids: std::collections::HashMap<u64, u32>,
    next_tex_id: u32,
    /// 重建代数（canvas 光栅缓存键成分）。
    rebuild_gen: u64,
    width: u32,
    height: u32,
    /// 逻辑尺寸（点，scale 折算后）；resize 时随物理尺寸同步更新（E2E 调试导出）。
    logical: (f32, f32),

    font_system: FontSystem,
    swash_cache: SwashCache,
    pub(crate) viewport: Viewport,
    pub(crate) atlas: TextAtlas,
    pub(crate) text_renderer: TextRenderer,

    /// 常驻镜像场景树（Zed 式）：首帧快照建立，后续增量动作打补丁
    scene: Option<crate::scene::SceneNode>,
    /// 跨帧布局缓存（layout.rs）：纯绘制属性变化只重跑 emit，不重建布局。
    layout_cache: crate::layout::LayoutCache,
    /// 窗口背景色
    bg: (u8, u8, u8),
    /// 最近一次 DisplayList（供命中测试）
    display: DisplayList,
    /// 最近一次布局导出的实测尺寸，待渲染后经 eventCb 回传 Host（回流）。
    pending_measured: Vec<(u64, f32, f32)>,
    /// 区域级穿透（MousePassthrough 区域模式）上一次应用的输入形状（物理像素）。
    /// None = 尚未应用（首次/模式切换后必发）；Some 相同 = 跳过 XShape 调用。
    last_region: Option<Vec<crate::passthrough::InputRect>>,
    /// transition 插值引擎（跨帧持有动画状态）
    animator: crate::style::Animator,
    /// 动画时钟原点
    anim_origin: std::time::Instant,
    /// 苹果滚动条淡入淡出：当前 alpha、上一次观测到的首个滚动条 scrollTop、
    /// 最近一次滚动发生的时刻（用于滚停后延迟淡出）。
    scrollbar_alpha: f32,
    scrollbar_last_top: f32,
    scrollbar_last_activity: std::time::Instant,
    /// 自上次 render 以来是否收到新帧数据（无变化帧跳过昂贵的布局/光栅化/文本整形）
    frame_dirty: bool,
    /// 上次 render 观测到的视频帧序号；新帧到达时置 frame_dirty 驱动重绘。
    video_frame_seq: u64,
    /// 按稳定 node_id 缓存的字形：只 shape 新增/内容变化的文本，其余跨帧复用。
    /// 虚拟列表滚动跨 item 边界时只有进出视口的 1 个 ID 变化，其余全部命中，
    /// 比"整段签名一致才全复用"更精确——任何内容变化帧都能复用未变的项。
    glyph_cache: std::collections::HashMap<u64, GlyphEntry>,
    /// 缓存的文本区域屏幕坐标（供命中/调试）
    cached_areas_dirty: bool,
    /// 最近光标位置（MouseInput 事件不带坐标）
    cursor: (f32, f32),
    /// 当前 hover/active/focused 命中的节点 ID（伪状态样式 &:hover/&:active/&:focus 用）。
    /// lib.rs 在 CursorMoved / MouseInput 事件里经 set_hover/set_active/set_focused 更新。
    hovered_id: Option<String>,
    active_id: Option<String>,
    focused_id: Option<String>,
    /// 平台 Surface 提供者（见 SurfaceProvider）。必须声明在 surface 之后：
    /// drop 时 surface 先销毁，raw handle 来源最后销毁（安全契约）。
    provider: std::sync::Arc<dyn SurfaceProvider>,
}

impl Renderer {
    /// 从平台注入的 SurfaceProvider 构建渲染器（不依赖任何窗口库）。
    pub async fn new(provider: std::sync::Arc<dyn SurfaceProvider>) -> Self {
        let (raw_w, raw_h) = provider.pixel_size();
        let w = raw_w.max(1);
        let h = raw_h.max(1);
        let logical = provider.logical_size();
        let instance = Instance::new(&InstanceDescriptor::default());
        let adapter = instance
            .request_adapter(&RequestAdapterOptions::default())
            .await
            .expect("request_adapter");
        eprintln!(
            "[rust-backend] adapter: {} ({:?})",
            adapter.get_info().name,
            adapter.get_info().backend
        );
        let (device, queue) = adapter
            .request_device(&DeviceDescriptor::default())
            .await
            .expect("request_device");
        // 平台原生化注入：raw handle 建 surface（§七）。句柄在 Renderer 存活期
        // 保持有效由 provider 的安全契约保证（Renderer 持有 provider 本体）。
        let surface = unsafe {
            instance.create_surface_unsafe(wgpu::SurfaceTargetUnsafe::RawHandle {
                raw_display_handle: provider.raw_display_handle(),
                raw_window_handle: provider.raw_window_handle(),
            })
        }
        .expect("create_surface");
        // Bgra8Unorm（非 sRGB target）：写入即显示，gamma 空间混合——与
        // tiny-skia/Skia raster 旧管线同口径，也避免 glyphon 文本双次提亮。
        let fmt = TextureFormat::Bgra8Unorm;
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
        // VSyncControl（P4.5 高刷专项）：创建期缓存本 surface 的可切换 present
        // 模式集合，运行时经 gx_window_set_vsync 切换（Fifo/Mailbox/Immediate）。
        let present_modes = surface.get_capabilities(&adapter).present_modes;

        let mut font_system = FontSystem::new();
        for path in [
            "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
            "/usr/share/fonts/truetype/noto/NotoSans-Regular.ttf",
            // 等宽字体：Editor 代码行 font-family 栈含 monospace 时使用，
            // 三端统一（Web ui-monospace / Skia DejaVu Sans Mono）。
            "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
            "/usr/share/fonts/truetype/noto/NotoSansMono-Regular.ttf",
        ] {
            if let Err(e) = font_system.db_mut().load_font_file(path) {
                eprintln!("[rust-backend] font {} : {:?}", path, e);
            }
        }
        let swash_cache = SwashCache::new();
        let cache = Cache::new(&device);
        let viewport = Viewport::new(&device, &cache);
        let mut atlas = TextAtlas::new(&device, &queue, &cache, fmt);
        let text_renderer =
            TextRenderer::new(&mut atlas, &device, MultisampleState::default(), None);

        let gpu = gx_gpu::QuadGpu::new(device.clone(), queue.clone(), fmt);
        Renderer {
            logical,
            device,
            queue,
            config,
            present_modes,
            surface: Some(surface),
            gpu,
            tex_cache: std::collections::HashMap::new(),
            tex_ids: std::collections::HashMap::new(),
            next_tex_id: 1,
            rebuild_gen: 0,
            width: w,
            height: h,
            font_system,
            swash_cache,
            viewport,
            atlas,
            text_renderer,
            scene: None,
            layout_cache: crate::layout::LayoutCache::new(),
            bg: (11, 16, 32),
            display: DisplayList::new(),
            pending_measured: vec![],
            last_region: None,
            animator: crate::style::Animator::new(),
            anim_origin: std::time::Instant::now(),
            scrollbar_alpha: 0.0,
            scrollbar_last_top: 0.0,
            scrollbar_last_activity: std::time::Instant::now(),
            frame_dirty: true,
            video_frame_seq: 0,
            glyph_cache: std::collections::HashMap::new(),
            cached_areas_dirty: true,
            cursor: (0.0, 0.0),
            hovered_id: None,
            active_id: None,
            focused_id: None,
            provider,
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
                self.frame_dirty = true;
                self.cached_areas_dirty = true;
                return true;
            }
        } else if let Some(delta) = DeltaView::parse(bytes) {
            // 增量动作：按 nodeID 打补丁
            if let Some(root) = self.scene.as_mut() {
                delta.apply(root);
                // 字形复用按 node_id + 内容签名逐项判定，无需区分是否纯滚动帧。
                self.frame_dirty = true;
                self.cached_areas_dirty = true;
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

    /// 取走本帧布局导出的实测尺寸（回流到 Host）。取后清空，避免重复回传。
    pub fn take_measured(&mut self) -> Vec<(u64, f32, f32)> {
        std::mem::take(&mut self.pending_measured)
    }

    /// 命中测试：返回坐标处的 node_id（供 hover/active 伪状态使用）。
    /// 与 hit_click 共用同一命中规则（浮层优先、最小面积、裁剪检查）。
    pub fn hit_node_id(&self, x: f32, y: f32) -> Option<String> {
        self.display.hit(x, y).map(|h| h.4.to_string())
    }

    /// 设置 hover 节点；返回 true 表示发生了变化（调用方据此决定 request_redraw）。
    pub fn set_hover(&mut self, id: Option<String>) -> bool {
        if self.hovered_id == id {
            false
        } else {
            self.hovered_id = id;
            self.frame_dirty = true; // 状态变了必须重算样式覆盖
            true
        }
    }

    /// 设置 active 节点（鼠标按下/抬起）；返回是否变化。
    pub fn set_active(&mut self, id: Option<String>) -> bool {
        if self.active_id == id {
            false
        } else {
            self.active_id = id;
            self.frame_dirty = true;
            true
        }
    }

    /// 设置 focused 节点（点击输入框 / 失焦）；返回是否变化。
    /// 变化置 frame_dirty，下一帧重算 &:focus 覆盖。
    pub fn set_focused(&mut self, id: Option<String>) -> bool {
        if self.focused_id == id {
            false
        } else {
            self.focused_id = id;
            self.frame_dirty = true;
            true
        }
    }

    // ---------- GPU 帧构建（GpuFrame：GPU/CPU 后端共用的纯数据） ----------

    /// 注册/获取纹理：logical 键 → 稳定 id；content key 变化 → 新上传条目
    /// （GPU/CPU 后端按 (id, key) 二级去重，内容未变零拷贝）。
    #[allow(clippy::too_many_arguments)]
    fn ensure_tex(
        &mut self,
        frame: &mut gx_gpu::GpuFrame,
        logical: u64,
        content: u64,
        w: u32,
        h: u32,
        premul: bool,
        data: std::sync::Arc<Vec<u8>>,
        persistent: bool,
    ) -> u32 {
        let id = match self.tex_ids.get(&logical) {
            Some(v) => *v,
            None => {
                let id = self.next_tex_id;
                self.next_tex_id += 1;
                self.tex_ids.insert(logical, id);
                id
            }
        };
        let cached = self
            .tex_cache
            .get(&content)
            .map(|e| e.id == id)
            .unwrap_or(false);
        if !cached {
            self.tex_cache.insert(
                content,
                TexEntry {
                    id,
                    w,
                    h,
                    premul,
                    data: data.clone(),
                    persistent,
                },
            );
        }
        frame.uploads.push(gx_gpu::TexUpload {
            id,
            key: content,
            w,
            h,
            data,
            premul,
        });
        id
    }

    /// 把 DisplayList 编译成 GpuFrame。绘制序与旧 tiny-skia 管线逐 shape 一致：
    /// shadow → bg → gradient → img/video → canvas → border；随后 selection
    /// 背景、滚动条 thumb（overlay）；glyphon 文本由 render 在同一 pass 最后叠加。
    fn build_gpu_frame(&mut self, dl: &DisplayList) -> gx_gpu::GpuFrame {
        let mut f = gx_gpu::GpuFrame::new(
            self.width,
            self.height,
            [self.bg.0 as f32, self.bg.1 as f32, self.bg.2 as f32, 255.0],
        );
        let mut used_keys: Vec<u64> = Vec::new();
        for s in &dl.shapes {
            if s.w <= 0.0 || s.h <= 0.0 {
                continue;
            }
            let clip = gx_gpu::clip_of(s.clip);
            // 与祖先 overflow 裁剪矩形求交；完全在外则跳过（与旧管线同口径）。
            let (rx, ry, rw, rh) = match clip_rect_xywh(s.clip, s.x, s.y, s.w, s.h) {
                Some(v) => v,
                None => continue,
            };
            if rw <= 0.0 || rh <= 0.0 {
                continue;
            }
            // box-shadow：6 层半透明圆角矩形近似（与 Skia 端 paintShadow 同
            // 算法）；几何不裁（旧实现仅做剔除），溢出由 scissor/交集裁剪兜底。
            if let Some(sh) = &s.shadow {
                let base_a = if sh.color.3 > 0 {
                    sh.color.3 as f32
                } else {
                    90.0
                };
                const LAYERS: i32 = 6;
                for i in (1..=LAYERS).rev() {
                    let t = i as f32 / LAYERS as f32;
                    let expand = sh.spread + sh.blur * t;
                    let sx = s.x + sh.offset_x - expand;
                    let sy = s.y + sh.offset_y - expand;
                    let sw = s.w + 2.0 * expand;
                    let shh = s.h.max(1.0) + 2.0 * expand;
                    if sw <= 0.0 || shh <= 0.0 {
                        continue;
                    }
                    let a = (base_a * (1.0 - t) / LAYERS as f32 * 1.8).clamp(0.0, 255.0);
                    f.quad(
                        [sx, sy, sw, shh],
                        clip,
                        [sh.color.0 as f32, sh.color.1 as f32, sh.color.2 as f32, a],
                        s.radius + expand,
                        0.0,
                    );
                }
            }
            if s.bg_present {
                // opacity 作用于元素整体：声明式 blink 动画采样后写进 st.opacity，
                // 此处按 alpha 淡入淡出（对齐 Skia paintElement / Web CSS opacity）。
                let a = (s.alpha.clamp(0.0, 1.0) * 255.0) as f32;
                f.quad(
                    [rx, ry, rw, rh],
                    clip,
                    [s.bg.0 as f32, s.bg.1 as f32, s.bg.2 as f32, a],
                    s.radius,
                    0.0,
                );
            }
            // 通用 linear-gradient 背景：与 Go 桌面端 paintGradientBackgroundShader
            // 路径一致；GPU 端按 quad 尺寸求渐变线长度。
            if let Some(g) = &s.bg_gradient {
                if let Some(grad) = gradient_model(g, rw, rh) {
                    f.gradient([rx, ry, rw, rh], clip, s.radius, grad);
                }
            }
            // img 原语：解码按 src 缓存（image.rs），纹理按源指针跨帧常驻，
            // 只上传一次（旧管线每帧全屏上传，此处为净收益）。
            if let Some((pix, fit)) = &s.img {
                let (dst, uv) = fit_rect(
                    pix.width() as f32,
                    pix.height() as f32,
                    *fit,
                    rx,
                    ry,
                    rw,
                    rh,
                );
                if dst[2] > 0.0 && dst[3] > 0.0 {
                    let logical = std::sync::Arc::as_ptr(pix) as u64;
                    let id = self.ensure_tex(
                        &mut f,
                        logical,
                        logical,
                        pix.width(),
                        pix.height(),
                        true,
                        std::sync::Arc::new(pix.data().to_vec()),
                        true,
                    );
                    f.tex(dst, clip, id, uv, s.radius, 1.0);
                }
            }
            // video 原语：ffmpeg 解码的当前帧，contain 缩放 + 圆角裁剪；
            // 新帧序号变化触发重上传（同帧不重传）。
            if let Some(vf) = &s.video {
                let logical = {
                    use std::collections::hash_map::DefaultHasher;
                    use std::hash::{Hash, Hasher};
                    let mut h = DefaultHasher::new();
                    s.video_id.hash(&mut h);
                    h.finish()
                };
                let content = logical ^ crate::video::frame_seq().rotate_left(1);
                let id = self.ensure_tex(
                    &mut f,
                    logical,
                    content,
                    vf.w.max(1),
                    vf.h.max(1),
                    true,
                    std::sync::Arc::new(vf.data.clone()),
                    false,
                );
                let (dst, uv) =
                    fit_rect(vf.w as f32, vf.h as f32, ObjectFit::Contain, rx, ry, rw, rh);
                if dst[2] > 0.0 && dst[3] > 0.0 {
                    used_keys.push(content);
                    f.tex(dst, clip, id, uv, s.radius, 1.0);
                }
            }
            // canvas 原语：命令式指令光栅到本盒纹理（坐标相对裁剪盒左上，
            // 与旧实现 paint(pm, ops, rx, ry) 的落点语义一致）。
            if let Some(ops) = &s.canvas {
                let cw = rw.max(1.0) as u32;
                let chh = rh.max(1.0) as u32;
                if let Some(mut tmp) = tiny_skia::Pixmap::new(cw, chh) {
                    crate::canvas::paint(&mut tmp, ops, 0.0, 0.0);
                    let logical = canvas_ops_key(ops);
                    let content = logical ^ self.rebuild_gen.rotate_left(32);
                    let id = self.ensure_tex(
                        &mut f,
                        logical,
                        content,
                        cw,
                        chh,
                        true,
                        std::sync::Arc::new(tmp.data().to_vec()),
                        false,
                    );
                    used_keys.push(content);
                    f.tex(
                        [rx, ry, rw, rh],
                        clip,
                        id,
                        [0.0, 0.0, cw as f32, chh as f32],
                        s.radius,
                        1.0,
                    );
                }
            }
            // 描边：带心环（GPU band / CPU stroke 同几何）；旧实现强制不透明。
            if let Some((c, bw)) = s.border {
                f.quad(
                    [rx, ry, rw, rh],
                    clip,
                    [c.0 as f32, c.1 as f32, c.2 as f32, 255.0],
                    s.radius,
                    bw,
                );
            }
        }
        // 三端统一 selection：在文字层之前画选区背景矩形（glyphon 文字随后绘制）。
        // 区间是 rune 索引，用与 measure_text 一致的估算口径换算像素宽度。
        for t in &dl.texts {
            let Some(sel) = &t.selection else { continue };
            let (sx0, sx1) = selection_px_range(&t.text, sel.start, sel.end, t.size);
            if sx1 <= sx0 {
                continue;
            }
            let Some((rx, ry, rw, rh)) =
                clip_rect_xywh(t.clip, t.x + sx0, t.y, sx1 - sx0, t.size * 1.35)
            else {
                continue;
            };
            if rw <= 0.0 || rh <= 0.0 {
                continue;
            }
            f.quad(
                [rx, ry, rw, rh],
                gx_gpu::clip_of(t.clip),
                [
                    sel.bg.0 as f32,
                    sel.bg.1 as f32,
                    sel.bg.2 as f32,
                    sel.bg.3 as f32,
                ],
                0.0,
                0.0,
            );
        }
        // 苹果风格滚动条画在内容之上（overlay），参与整体淡入淡出。
        if self.scrollbar_alpha > 0.01 {
            let a = (self.scrollbar_alpha * 0.55 * 255.0) as f32;
            for b in &dl.scrollbars {
                f.quad(
                    [b.x, b.thumb_y, SB_TRACK_W, b.thumb_h],
                    [0.0, 0.0, 0.0, 0.0],
                    [120.0, 120.0, 128.0, a],
                    SB_TRACK_W * 0.5,
                    0.0,
                );
            }
        }
        // 淘汰本帧未引用的非持久纹理（视频旧帧 / canvas 旧代）。
        let used: std::collections::HashSet<u64> = used_keys.into_iter().collect();
        self.tex_cache
            .retain(|k, e| e.persistent || used.contains(k));
        f
    }

    /// 事件驱动重绘：渲染完成后判断场景是否仍有"会随时间自行变化"的内容。
    /// 只有返回 true 时才继续 request_redraw；否则停止，直到下一帧/事件/新帧唤醒。
    /// 覆盖三类持续动画源：transition 动画、播放中的视频、滚动条淡入淡出过渡。
    pub fn needs_continuous_redraw(&self) -> bool {
        // transition 动画进行中
        if self.animator.is_active() {
            return true;
        }
        // 声明式周期动画（如 Editor 光标 blink）无外部事件唤醒，必须持续重绘。
        // 直接扫描常驻场景树，不依赖 apply 的调用时序。
        if self
            .scene
            .as_ref()
            .map(|s| crate::style::tree_has_animation(s))
            .unwrap_or(false)
        {
            return true;
        }
        // 视频正在播放：解码线程持续产出新帧，必须保持重绘调度。
        if crate::video::has_active() {
            return true;
        }
        // 滚动条淡入淡出过渡期（滚动后 1.2s~1.7s）；alpha 为 0 或 1 时静止。
        let has_bar = !self.display.scrollbars.is_empty();
        if has_bar {
            let a = self.scrollbar_alpha;
            if (a > 0.0 && a < 1.0)
                || std::time::Instant::now()
                    .duration_since(self.scrollbar_last_activity)
                    .as_millis()
                    < 1700
            {
                return true;
            }
        }
        false
    }

    /// 帧准备（P4.6 present 线程化）：布局/文本整形/GpuFrame 构建等 CPU 与
    /// 纹理上传工作，**不触碰 surface**——acquire/encode/present 由专用渲染
    /// 线程执行（lib.rs present_loop），Fifo 背压的阻塞只落在渲染线程。
    /// 返回 None 表示无可提交内容（未建镜像树）。
    pub fn prepare(&mut self) -> Option<gx_gpu::GpuFrame> {
        let Some(scene) = self.scene.as_ref() else {
            return None;
        };
        // transition：按当前时间推进动画。动画进行中必须逐帧重算（插值值在变）。
        let now_ms = self.anim_origin.elapsed().as_millis();
        self.animator.set_now(now_ms);
        // 周期动画（blink）无外部事件唤醒：即使 transition 静止、frame_dirty=false，
        // 也必须把 animating 置位，让本帧进入 apply/重绘路径逐帧采样 opacity。
        let animating = self.animator.is_active() || crate::style::tree_has_animation(scene);
        // video 新帧：解码线程独立推进帧序号，变化则强制重算（重画当前帧）。
        let vseq = crate::video::frame_seq();
        if vseq != self.video_frame_seq {
            self.video_frame_seq = vseq;
            self.frame_dirty = true;
        }
        let need_rebuild = self.frame_dirty || animating || self.cached_areas_dirty;
        self.rebuild_gen = self.rebuild_gen.wrapping_add(1);

        self.viewport.update(
            &self.queue,
            Resolution {
                width: self.config.width,
                height: self.config.height,
            },
        );

        // 帧数据来源：重建帧用新 DisplayList，静止帧沿用上一帧
        // （纯数据构建，成本低；GPU 端 (id,key) 去重保证零纹理上传）。
        let src: DisplayList;
        if need_rebuild {
            // —— 有变化 / 动画中：全量重算布局、GPU 帧数据、文本整形 ——
            let animated = self.animator.apply(scene);
            // 伪状态样式：按当前 hover/focus/active 命中把对应子表覆盖进
            // 临时树（不改常驻镜像树）。disabled 由节点自身属性判定、优先级最高。
            let styled = crate::style::apply_pseudo_state(
                &animated,
                self.hovered_id.as_deref(),
                self.active_id.as_deref(),
                self.focused_id.as_deref(),
            );
            let dl = DisplayList::from_scene(
                &styled,
                self.width as f32,
                self.height as f32,
                &mut self.layout_cache,
            );

            // 滚动条淡入淡出：以首个滚动条的 thumb_y 作为滚动位置信号。
            // 位置变化（或首次出现）视为活动 → alpha 拉满并刷新时间戳。
            // 注意：无变化帧跳过 Pixmap 重绘，因此淡出要借助"仍在重绘的帧"（如动画）
            // 才会推进；纯静止时滚动条保持在最后状态（详见文档诚实边界）。
            let bar_pos = dl.scrollbars.first().map(|b| b.thumb_y).unwrap_or(0.0);
            let has_bar = !dl.scrollbars.is_empty();
            let now = std::time::Instant::now();
            if has_bar && (bar_pos - self.scrollbar_last_top).abs() > 0.1 {
                self.scrollbar_last_top = bar_pos;
                self.scrollbar_last_activity = now;
            }
            let idle_ms = now.duration_since(self.scrollbar_last_activity).as_millis();
            // 滚动后 ~1.2s 开始淡出，再 ~0.5s 淡到 0。
            let target_alpha = if !has_bar {
                0.0
            } else if idle_ms < 1200 {
                1.0
            } else if idle_ms < 1700 {
                1.0 - (idle_ms - 1200) as f32 / 500.0
            } else {
                0.0
            };
            self.scrollbar_alpha = target_alpha;

            self.display = dl.clone();
            // 导出本帧实测尺寸供回流（无变化帧沿用上一帧，PublishMeasured 会去重）。
            self.pending_measured = dl.measured.clone();
            src = dl;

            // 字形增量复用（按稳定 node_id）：
            // glyphon 的 Buffer 只取决于 (文本, 字号)，与坐标/颜色无关。每帧按
            // dl.texts 的 node_id 查缓存：ID 与签名都在 → 直接复用；新 ID 或内容
            // 变了 → 只 shape 这一个；最后淘汰当前帧不再出现的 ID（滚出视口）。
            // 虚拟列表用 index 作 key、节点 ID 稳定，跨 item 边界时只有 1 个新 ID
            // 需要 shape，其余全部命中——比"整段签名一致才全复用"精确得多，且对
            // 任意内容变化帧都生效（不再限于纯滚动帧）。动画改的是坐标/颜色时
            // 签名不变可安全复用；若真改了字号/文本，签名变化会触发重新 shape。
            let (shaped_idx, evicted) = plan_glyphs(&src.texts, &self.glyph_cache);
            let shaped = shaped_idx.len();
            for i in shaped_idx {
                let t = &src.texts[i];
                let lh = if t.line_height > 0.0 {
                    t.line_height
                } else {
                    t.size * 1.35
                };
                let mut b = Buffer::new(&mut self.font_system, Metrics::new(t.size, lh));
                b.set_size(&mut self.font_system, Some(700.0), Some(80.0));
                let attrs = match &t.family {
                    Some(f) => Attrs::new().family(Family::Name(f)),
                    None => Attrs::new().family(Family::SansSerif),
                };
                // font-weight：cosmic-text 按字重挑字形（400=常规 / 700=粗体），
                // 与 Skia 的 FontStyleBold、Web 的 font-weight 数值语义对齐。
                let attrs = match t.weight {
                    Some(w) => attrs.weight(Weight(w as u16)),
                    None => attrs,
                };
                if t.spans.is_empty() {
                    // 快路径：普通单色文字，颜色走 TextArea.default_color。
                    b.set_text(&mut self.font_system, &t.text, &attrs, Shaping::Advanced);
                    b.shape_until_scroll(&mut self.font_system, false);
                } else {
                    // 统一渐变文字：按 ch 分段逐 span 着色。段内颜色编进 Attrs，
                    // glyphon 渲染时优先读 glyph.color_opt（见 text_render.rs:245）。
                    let styled: Vec<(&str, Attrs)> = t
                        .spans
                        .iter()
                        .map(|(seg, c)| {
                            (seg.as_str(), attrs.clone().color(Color::rgb(c.0, c.1, c.2)))
                        })
                        .collect();
                    b.set_rich_text(
                        &mut self.font_system,
                        styled,
                        &attrs,
                        Shaping::Advanced,
                        None,
                    );
                }
                self.glyph_cache.insert(
                    t.id,
                    GlyphEntry {
                        sig: text_sig(t),
                        buffer: b,
                    },
                );
            }
            for id in evicted {
                self.glyph_cache.remove(&id);
            }
            #[cfg(debug_assertions)]
            if shaped > 0 || shaped < src.texts.len() {
                eprintln!(
                    "[rust-backend] glyph reuse: {} reused, {} shaped",
                    src.texts.len().saturating_sub(shaped),
                    shaped
                );
            }
            self.frame_dirty = false;
            self.cached_areas_dirty = false;
        } else {
            src = self.display.clone();
        }

        // GpuFrame：GPU 形状纯数据（shadow/bg/渐变/纹理/描边/selection/滚动条），
        // 由 gx_gpu 的 GPU 管线执行；CPU 后端可接管同一份数据（打包期选型）。
        let frame = self.build_gpu_frame(&src);

        // 从当前 DisplayList + 按 ID 缓存的 buffers 组装 areas（有无变化都要做，因为 prepare 每帧消费 areas）。
        let areas = Self::build_text_areas(&self.glyph_cache, &self.display.texts, self.height as i32);
        self.text_renderer
            .prepare(
                &self.device,
                &self.queue,
                &mut self.font_system,
                &mut self.atlas,
                &self.viewport,
                areas,
                &mut self.swash_cache,
            )
            .unwrap();
        self.atlas.trim();
        Some(frame)
    }

    /// 从当前 DisplayList + 按 ID 缓存的 buffers 组装 areas（有无变化都要做，因为 prepare 每帧消费 areas）。
    /// 独立参数形式：TextArea 借用 glyph_cache 的 Buffer，若经 &self 方法借用
    /// 会与后续 text_renderer.prepare 的 &mut 字段借用冲突（字段级不相交才可行）。
    fn build_text_areas<'a>(
        glyphs: &'a std::collections::HashMap<u64, GlyphEntry>,
        texts: &'a [crate::paint::TextDraw],
        win_h: i32,
    ) -> Vec<TextArea<'a>> {
        texts
            .iter()
            .filter_map(|t| {
                let b = match glyphs.get(&t.id) {
                    Some(e) => &e.buffer,
                    None => return None, // 理论上不会发生；防御性跳过缺字形的项
                };
                let bounds = match t.clip {
                    Some((cx, cy, cw, ch)) => TextBounds {
                        left: (cx - t.x - 20.0) as i32,
                        top: (cy - t.y - 20.0) as i32,
                        right: (cx + cw - t.x) as i32,
                        bottom: (cy + ch - t.y) as i32,
                    },
                    None => TextBounds {
                        left: -20,
                        top: -20,
                        right: 720,
                        bottom: win_h,
                    },
                };
                Some(TextArea {
                    buffer: b,
                    left: t.x,
                    top: t.y,
                    scale: 1.0,
                    bounds,
                    // opacity（含声明式 blink 采样）作用于文字 alpha，对齐 Skia/Web。
                    // glyphon 颜色为 pre-multiplied，按 alpha 缩放三通道。
                    default_color: {
                        let a = t.alpha.clamp(0.0, 1.0);
                        Color::rgba(
                            (t.color.0 as f32 * a) as u8,
                            (t.color.1 as f32 * a) as u8,
                            (t.color.2 as f32 * a) as u8,
                            (a * 255.0) as u8,
                        )
                    },
                    custom_glyphs: &[],
                })
            })
            .collect()
    }

    /// 点击命中 → 生成 click 事件 JSON
    pub fn hit_click(&self, x: f32, y: f32) -> Option<InputEvent> {
        self.display
            .hit(x, y)
            .map(|(_, _, _, _, id, ev, _, ..)| InputEvent {
                kind: "click".into(),
                node_id: id.to_string(),
                event: ev.clone(),
                text: String::new(),
                x,
                y,
                mods: String::new(),
            })
    }

    /// 当前逻辑尺寸（provider 实时查询——resize/缩放变化立即反映）。
    fn provider_logical(&self) -> (f32, f32) {
        self.provider.logical_size()
    }

    /// E2E 调试导出：最近一帧全部命中框（x/y/w/h + label/testid/kind）+ 窗口
    /// 逻辑尺寸。label 取 props["label"] > 自身 text > 首个子孙 text（与旧
    /// skia 装配层 nodeLabel 同语义）；kind 取节点 kind。JSON 一次性序列化，
    /// 供 gx_window_debug_info C ABI 直接透传。
    pub fn debug_boxes_json(&self) -> String {
        #[derive(serde::Serialize)]
        struct BoxJson {
            label: String,
            testid: String,
            kind: String,
            class: String,
            #[serde(rename = "data-line")]
            data_line: String,
            x: f32,
            y: f32,
            w: f32,
            h: f32,
        }
        #[derive(serde::Serialize)]
        struct DebugInfo {
            win_w: f32,
            win_h: f32,
            boxes: Vec<BoxJson>,
        }
        fn label_of(n: &crate::scene::SceneNode) -> String {
            if let Some(v) = n.props.get("label") {
                if !v.is_empty() {
                    return v.clone();
                }
            }
            if !n.text.is_empty() {
                return n.text.clone();
            }
            for c in &n.children {
                let s = label_of(c);
                if !s.is_empty() {
                    return s;
                }
            }
            String::new()
        }
        fn find<'a>(
            root: &'a crate::scene::SceneNode,
            nid: u64,
        ) -> Option<&'a crate::scene::SceneNode> {
            fn walk<'a>(
                n: &'a crate::scene::SceneNode,
                nid: u64,
            ) -> Option<&'a crate::scene::SceneNode> {
                if n.nid == nid {
                    return Some(n);
                }
                for c in &n.children {
                    if let Some(f) = walk(c, nid) {
                        return Some(f);
                    }
                }
                None
            }
            walk(root, nid)
        }
        let (win_w, win_h) = self.provider_logical();
        let scene = self.scene.as_ref();
        let boxes: Vec<BoxJson> = self
            .display
            .hitboxes
            .iter()
            .filter_map(|h| {
                let node = scene.and_then(|sc| find(sc, h.4));
                node.map(|n| BoxJson {
                    label: label_of(n),
                    testid: n.props.get("testid").cloned().unwrap_or_default(),
                    kind: if n.kind.is_empty() {
                        format!("{:?}", n.typ)
                    } else {
                        n.kind.clone()
                    },
                    class: n.props.get("class").cloned().unwrap_or_default(),
                    data_line: n.props.get("data-line").cloned().unwrap_or_default(),
                    x: h.0,
                    y: h.1,
                    w: h.2,
                    h: h.3,
                })
            })
            .collect();
        serde_json::to_string(&DebugInfo {
            win_w,
            win_h,
            boxes,
        })
        .unwrap_or_default()
    }

    /// 查节点 CSS cursor 属性（hover 悬停切系统光标形状）。
    /// 命中节点自身未声明时向上层找——cursor 是 CSS 继承属性，容器声明
    /// "pointer" 其子文本也应是手型。找不到返回 None（默认箭头）。
    pub fn node_cursor(&self, id: &str) -> Option<String> {
        if let Some(scene) = &self.scene {
            // 命中节点 → 根的祖先链；沿链找第一个声明 cursor 的（CSS 继承语义）。
            for node in scene.find_ancestors(id) {
                if let Some(c) = node.props.get("cursor") {
                    if !c.is_empty() {
                        return Some(c.clone());
                    }
                }
            }
        }
        None
    }

    /// 滚轮路由（渲染端自治，同 web 端浏览器职责）：把 dy*lineH 像素滚进
    /// 光标下最近的 overflow:auto/scroll 容器（镜像树祖先链查找，scroll-top
    /// 夹紧写回 + request_redraw）。返回 true 表示已处理（不回传 Go）；
    /// 光标在编辑器区（class gx-editor，编辑逻辑层信号驱动）或无可滚动容器
    /// 时返回 false，由 lib.rs 决定是否回传 wheel 事件。
    pub fn scroll_container_at(&mut self, x: f32, y: f32, delta_px: f32) -> bool {
        use crate::scene::SceneNode;
        let hit = match self.display.hit(x, y) {
            Some(h) => h,
            None => return false,
        };
        let scene = match self.scene.as_ref() {
            Some(s) => s,
            None => return false,
        };
        // 光标下命中节点的祖先链（含自身，内→外）。
        let chain = scene.find_ancestors(&hit.4.to_string());
        // 沿祖先链找最近可滚动容器；编辑器（class 含 gx-editor）不在此处理。
        let mut container: Option<&SceneNode> = None;
        for node in &chain {
            let ov = node
                .props
                .get("overflow")
                .map(|s| s.trim().to_ascii_lowercase());
            if ov.as_deref() != Some("auto") && ov.as_deref() != Some("scroll") {
                continue;
            }
            if node
                .props
                .get("class")
                .map(|c| c.contains("gx-editor"))
                .unwrap_or(false)
            {
                return false; // 编辑器滚动走 Go 翻译层（EditorScrollLines 信号）
            }
            container = Some(node);
            break;
        }
        let Some(container) = container else {
            return false;
        };
        // 命中框（容器自身盒子，坐标含当前滚动偏移）。
        let cbox = self
            .display
            .hitboxes
            .iter()
            .find(|h| h.4 == container.nid)
            .cloned();
        let Some(box_rect) = cbox else {
            return false;
        };
        // 内容总高：容器子树内全部命中框的最大底边（含滚动偏移，加回当前值得内容高）。
        let cur = container
            .props
            .get("scroll-top")
            .and_then(|v| v.trim().parse::<f32>().ok())
            .unwrap_or(0.0);
        let sub_of = |nid: u64| -> bool {
            scene
                .find_ancestors(&nid.to_string())
                .iter()
                .any(|n| n.nid == container.nid)
        };
        let mut max_bottom = box_rect.1 + box_rect.3;
        for h in &self.display.hitboxes {
            if sub_of(h.4) && h.1 + h.3 > max_bottom {
                max_bottom = h.1 + h.3;
            }
        }
        let max_scroll = (max_bottom - box_rect.1 + cur - box_rect.3).max(0.0);
        let next = (cur + delta_px).clamp(0.0, max_scroll);
        if next == cur {
            return true; // 已到边界：吃掉事件，不回传
        }
        // 镜像树打补丁（渲染端表现层状态，Go 树未变即不被 Host 帧覆盖）。
        // container 的只读借用（chain/scene）至此已全部结束，可安全可变打补丁。
        let container_nid = container.nid;
        drop(chain);
        drop(scene);
        if let Some(scene_mut) = self.scene.as_mut() {
            find_by_nid_mut(scene_mut, container_nid, &mut |n: &mut SceneNode| {
                n.props.insert("scroll-top".into(), format!("{next}"));
            });
        }
        self.frame_dirty = true;
        self.request_redraw();
        true
    }

    /// 点击是否落在输入框 → 生成 focus 事件
    pub fn hit_input(&self, x: f32, y: f32) -> Option<InputEvent> {
        self.display
            .inputs
            .iter()
            .filter(|(bx, by, bw, bh, _)| x >= *bx && x <= bx + bw && y >= *by && y <= by + bh)
            .min_by(|a, b| {
                (a.2 * a.3)
                    .partial_cmp(&(b.2 * b.3))
                    .unwrap_or(std::cmp::Ordering::Equal)
            })
            .map(|(_, _, _, _, id)| InputEvent {
                kind: "focus".into(),
                node_id: id.to_string(),
                event: String::new(),
                text: String::new(),
                x,
                y,
                mods: String::new(),
            })
    }

    pub fn resize(&mut self, w: u32, h: u32) {
        // 仅更新本端尺寸/缓存（P4.6：surface 重配置由渲染线程经 PresentHub 执行）。
        self.config.width = w.max(1);
        self.config.height = h.max(1);
        self.logical = self.provider_logical();
        // 尺寸相关缓存失效：下一帧必须重算布局与文本。
        self.frame_dirty = true;
        self.cached_areas_dirty = true;
        self.glyph_cache.clear(); // 字号不变，但清空避免与旧尺寸残留帧误命中
    }

    /// 请求平台重绘一帧（转发 SurfaceProvider，内核不感知窗口机制）。
    pub fn request_redraw(&self) {
        self.provider.request_redraw();
    }

    /// 区域级穿透（MousePassthrough 区域模式）：从本帧 DisplayList 提取
    /// 「有内容」的矩形集合（窗口内容区物理像素，左上原点）。口径与旧
    /// skia glwindow 的 drawLogToInputRects 一致：只收真正画出来的不透明
    /// 内容（bg/渐变/图片/canvas/video/边框）+ 可交互命中框（无 bg 的
    /// 按钮/输入框也要可点）。空结果 = 整窗穿透。
    ///
    /// 带变更缓存：与上次应用结果相同返回 None（跳过 XShape 调用），
    /// 不同（或首帧/模式切换）返回 Some 并更新缓存。
    pub fn take_region_rects(&mut self, scale: f32) -> Option<Vec<crate::passthrough::InputRect>> {
        // 裁剪到窗口物理边界：滚动容器的内容盒可超出窗口底（命中框随内容移动），
        // 越界矩形无输入意义，反而让 XShape 集合无谓膨胀。
        let rects = region_rects(&self.display, scale, self.width, self.height);
        if self.last_region.as_deref() == Some(&rects[..]) {
            return None; // 与上次相同：无需重设 XShape
        }
        self.last_region = Some(rects.clone());
        Some(rects)
    }

    /// 模式切换（关闭→区域）后强制下一次 take_region_rects 必发。
    pub fn invalidate_region(&mut self) {
        self.last_region = None;
    }

    /// 移交 surface 与 present 模式集合（P4.6 present 线程化）：resumed 创建
    /// 渲染器后由宿主调用，surface 从此由专用渲染线程独占（acquire/present
    /// 不再占用事件循环线程）。config 克隆给渲染线程，本端保留尺寸字段供
    /// viewport/布局使用。
    pub fn take_surface_parts(
        &mut self,
    ) -> (wgpu::Surface<'static>, SurfaceConfiguration, Vec<PresentMode>) {
        let surface = self.surface.take().expect("surface taken twice");
        (surface, self.config.clone(), std::mem::take(&mut self.present_modes))
    }

    /// 同步渲染并 present（Android 直通路径专用：surface 不移交 PresentHub，
    /// 帧调度由宿主 paintClock 驱动，调用线程直接 acquire/encode/present）。
    /// 与桌面渲染线程每帧逻辑同源（prepare → acquire → draw → text → present）。
    /// 返回 false 表示本帧无可绘内容或 surface 出错（已自愈重配置，宿主可跳过）。
    pub fn render_now(&mut self) -> bool {
        let Some(frame) = self.prepare() else {
            return false;
        };
        let tex = {
            let Some(surface) = self.surface.as_ref() else {
                return false;
            };
            match surface.get_current_texture() {
                Ok(tex) => tex,
                Err(e) => {
                    // Outdated/Lost 自愈：按当前配置重建 surface，跳过本帧。
                    eprintln!("[rust-backend] android get_current_texture: {e}");
                    surface.configure(&self.device, &self.config);
                    return false;
                }
            }
        };
        let view = tex.texture.create_view(&Default::default());
        let mut enc = self
            .device
            .create_command_encoder(&wgpu::CommandEncoderDescriptor { label: None });
        {
            let mut pass = enc.begin_render_pass(&wgpu::RenderPassDescriptor {
                label: None,
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
            self.gpu.draw(&mut pass, &frame);
            let _ = self.text_renderer.render(&self.atlas, &self.viewport, &mut pass);
        }
        self.queue.submit(Some(enc.finish()));
        tex.present();
        true
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
        let mut cache = crate::layout::LayoutCache::new();
        let dl = DisplayList::from_scene(&tree, width, height, &mut cache);
        sink = sink.wrapping_add(dl.shapes.len() + dl.texts.len());
    }
    let ns = start.elapsed().as_nanos() as u64;
    // 防止编译器优化掉结果。
    std::hint::black_box(sink);
    Some(ns)
}

/// 缓存命中路径基准：同一棵树重复 from_scene，验证布局复用（签名命中 →
/// 跳过 build+compute_layout，只跑 emit）的实际收益。与 bench_frame 的
/// 比值即「绘制属性变化帧相对全量帧的加速比」。
pub fn bench_frame_cached(buf: &[u8], width: f32, height: f32, iters: u32) -> Option<u64> {
    use crate::binprotocol::FrameView;
    use crate::scene::SceneNode;
    let view = FrameView::parse(buf)?;
    let root = SceneNode::from_frame(&view)?;
    let start = std::time::Instant::now();
    let mut sink = 0usize;
    let mut cache = crate::layout::LayoutCache::new();
    for _ in 0..iters {
        // 同一棵树（克隆模拟宿主每帧重建 styled 树，签名不变）→ 布局复用。
        let tree = root.clone();
        let dl = DisplayList::from_scene(&tree, width, height, &mut cache);
        sink = sink.wrapping_add(dl.shapes.len() + dl.texts.len());
    }
    assert!(cache.reuses > 0, "基准必须走到复用路径");
    let ns = start.elapsed().as_nanos() as u64;
    std::hint::black_box(sink);
    Some(ns)
}

#[cfg(test)]
mod parity_tests {
    use super::*;
    use crate::binprotocol::NodeType;
    use crate::scene::SceneNode;

    fn div(id: &str, props: &[(&str, &str)], children: Vec<SceneNode>) -> SceneNode {
        SceneNode {
            typ: NodeType::Div,
            kind: "div".into(),
            id: id.into(),
            text: String::new(),
            nid: 1008,
            props: props
                .iter()
                .map(|(k, v)| (k.to_string(), v.to_string()))
                .collect(),
            events: vec![],
            children,
        }
    }

    fn text(id: &str, text: &str, props: &[(&str, &str)]) -> SceneNode {
        SceneNode {
            typ: NodeType::Text,
            kind: "#text".into(),
            id: id.into(),
            text: text.into(),
            nid: 1009,
            props: props
                .iter()
                .map(|(k, v)| (k.to_string(), v.to_string()))
                .collect(),
            events: vec![],
            children: vec![],
        }
    }

    // ---- Taffy 布局映射 ----

    #[test]
    fn hitboxes_expose_cursor_and_skip_pointer_events() {
        let mut btn = div(
            "btn",
            &[("width", "50"), ("height", "20"), ("cursor", "pointer")],
            vec![],
        );
        btn.events = vec!["on-click".into()];
        let mut pe = div(
            "pe",
            &[
                ("width", "50"),
                ("height", "20"),
                ("pointer-events", "none"),
            ],
            vec![],
        );
        pe.events = vec!["on-click".into()];
        let root = div(
            "root",
            &[
                ("width", "200"),
                ("height", "60"),
                ("flex-direction", "row"),
            ],
            vec![btn, pe],
        );
        let dl =
            DisplayList::from_scene(&root, 400.0, 200.0, &mut crate::layout::LayoutCache::new());
        assert_eq!(dl.hitboxes.len(), 1, "pointer-events:none 不登记命中框");
        let hb = &dl.hitboxes[0];
        assert_eq!(hb.4, 1008, "btn 节点 nid（测试 helper 顺序分配）");
        assert_eq!(hb.8, "pointer", "cursor 随命中框透出");
    }

    #[test]
    fn text_align_and_whitespace_and_weight_in_text_draws() {
        let root = div(
            "root",
            &[
                ("width", "100"),
                ("text-align", "center"),
                ("white-space", "normal"),
                ("font-weight", "700"),
            ],
            vec![text("t", "a   b", &[])],
        );
        let dl =
            DisplayList::from_scene(&root, 200.0, 100.0, &mut crate::layout::LayoutCache::new());
        assert_eq!(dl.texts.len(), 1);
        let t = &dl.texts[0];
        assert_eq!(t.text, "a b", "white-space:normal 折叠空白");
        assert_eq!(t.weight, Some(700), "font-weight 进入 TextDraw");
        // 近似宽 = (0.56*2 + 1*1空格?) "a b" = a(0.56)+空格(0.56)+b(0.56) = 1.68em*16=26.88
        let tw = t
            .text
            .chars()
            .map(|c| if c.is_ascii() { 0.56 } else { 1.0 })
            .sum::<f32>()
            * t.size;
        let want_dx = (100.0 - tw) / 2.0;
        assert!(
            (t.x - want_dx).abs() < 0.5,
            "center 落笔 x = {}, want ≈{}",
            t.x,
            want_dx
        );
    }

    #[test]
    fn line_height_overrides_text_draw() {
        let root = div(
            "root",
            &[("width", "100"), ("line-height", "2")],
            vec![text("t", "hi", &[])],
        );
        let dl =
            DisplayList::from_scene(&root, 200.0, 100.0, &mut crate::layout::LayoutCache::new());
        let t = &dl.texts[0];
        assert!(
            (t.line_height - 32.0).abs() < 0.1,
            "line-height:2 × 16px = 32, got {}",
            t.line_height
        );
    }

    #[test]
    fn line_height_inherits_from_parent() {
        // text 子节点自身无 line-height，应继承父容器的 1.5。
        let root = div(
            "root",
            &[("width", "100"), ("line-height", "1.5")],
            vec![div("wrap", &[], vec![text("t", "hi", &[])])],
        );
        let dl =
            DisplayList::from_scene(&root, 200.0, 100.0, &mut crate::layout::LayoutCache::new());
        let t = &dl.texts[0];
        assert!(
            (t.line_height - 24.0).abs() < 0.1,
            "继承 1.5×16=24, got {}",
            t.line_height
        );
    }

    #[test]
    fn z_index_reorders_paint_order() {
        let mk = |id: &str, bg: &str, z: Option<&str>| {
            let mut props = vec![("width", "30"), ("height", "10"), ("bg", bg)];
            let mut p: Vec<(&str, &str)> = props.drain(..).collect();
            if let Some(zv) = z {
                p.push(("z-index", zv));
            }
            div(id, &p, vec![])
        };
        let root = div(
            "root",
            &[("width", "60"), ("height", "60")],
            vec![
                mk("a", "#010101", Some("5")),
                mk("b", "#020202", None),
                mk("c", "#030303", Some("-1")),
            ],
        );
        let dl =
            DisplayList::from_scene(&root, 100.0, 100.0, &mut crate::layout::LayoutCache::new());
        // 绘制顺序：c(-1) → b(0) → a(5)，同值保持文档序。
        let bgs: Vec<(u8, u8, u8)> = dl
            .shapes
            .iter()
            .filter(|s| s.bg_present)
            .map(|s| s.bg)
            .collect();
        assert_eq!(
            bgs,
            vec![(0x03, 0x03, 0x03), (0x02, 0x02, 0x02), (0x01, 0x01, 0x01)],
            "z-index 应重排绘制顺序: {:?}",
            bgs
        );
    }

    #[test]
    fn text_sig_includes_weight() {
        let mut t = TextDraw {
            id: 7,
            text: "hi".into(),
            x: 0.0,
            y: 0.0,
            size: 16.0,
            color: (0, 0, 0),
            clip: None,
            spans: vec![],
            selection: None,
            family: None,
            weight: None,
            line_height: 0.0,
            alpha: 1.0,
        };
        assert!(!text_sig(&t).contains("@w"));
        t.weight = Some(700);
        assert!(text_sig(&t).contains("@w700"), "字重变化应触发重新 shape");
    }
}

// ---------------------------------------------------------------------------
// 三端一致性矩阵测试（与 backend/skia/threeway_test.go、backend/web/threeway_test.go
// 共享同一份 testdata/threeway/cases.json）。任何一条失败都意味着 Rust 端该
// 样式属性未实现、Taffy 映射传值错误或与 Web/Skia 语义不一致。
// 运行：cargo test --manifest-path backend/rust/rust/Cargo.toml threeway_matrix
// ---------------------------------------------------------------------------
#[cfg(test)]
mod threeway_tests {
    use super::*;
    use crate::binprotocol::NodeType;
    use crate::scene::SceneNode;

    fn tw_build(v: &serde_json::Value, id_counter: &mut usize) -> SceneNode {
        let kind = v["kind"].as_str().unwrap_or("div");
        let mut props: std::collections::HashMap<String, String> = std::collections::HashMap::new();
        if let Some(map) = v["props"].as_object() {
            for (k, pv) in map {
                props.insert(k.clone(), pv.as_str().unwrap_or_default().to_string());
            }
        }
        if let Some(name) = v["name"].as_str() {
            props.insert("gx-name".into(), name.into());
        }
        *id_counter += 1;
        let id = id_counter.to_string();
        let nid = *id_counter as u64;
        let mut children = vec![];
        if let Some(arr) = v["children"].as_array() {
            for c in arr {
                children.push(tw_build(c, id_counter));
            }
        }
        if kind == "text" {
            SceneNode {
                typ: NodeType::Text,
                kind: "#text".into(),
                id,
                nid,
                text: v["text"].as_str().unwrap_or("").into(),
                props,
                events: vec![],
                children,
            }
        } else {
            // Skia 端对全部 div 登记命中框；镜像对齐：每个 div 视作可命中。
            SceneNode {
                typ: NodeType::Div,
                kind: "div".into(),
                id,
                nid,
                text: String::new(),
                props,
                events: vec!["on-click".into()],
                children,
            }
        }
    }

    fn tw_hex(c: (u8, u8, u8)) -> String {
        format!("#{:02x}{:02x}{:02x}", c.0, c.1, c.2)
    }

    #[derive(Clone)]
    struct TwRect {
        bg: String,
        x: f32,
        y: f32,
        w: f32,
        h: f32,
    }
    #[derive(Clone)]
    struct TwText {
        text: String,
        x: f32,
        y: f32,
    }

    fn tw_observe(
        dl: &DisplayList,
        bg_by_id: &std::collections::HashMap<String, String>,
    ) -> (Vec<TwRect>, Vec<TwText>) {
        let mut rects = vec![];
        for s in &dl.shapes {
            if s.bg_present && s.bg_gradient.is_none() {
                rects.push(TwRect {
                    bg: tw_hex(s.bg),
                    x: s.x,
                    y: s.y,
                    w: s.w,
                    h: s.h,
                });
            }
        }
        let mut texts = vec![];
        for t in &dl.texts {
            texts.push(TwText {
                text: t.text.clone(),
                x: t.x,
                y: t.y,
            });
        }
        // 命中观测对齐 Skia HitTest：逆序（自顶向下）第一个包含点的命中框。
        let _ = bg_by_id;
        (rects, texts)
    }

    fn tw_hit_at(
        dl: &DisplayList,
        bg_by_id: &std::collections::HashMap<String, String>,
        px: f32,
        py: f32,
    ) -> (String, String) {
        for hb in dl.hitboxes.iter().rev() {
            let (x, y, w, h, id, _, _, clip, cursor) = hb;
            let contains = px >= *x && px < x + *w && py >= *y && py < y + *h;
            if contains && !clip_rejects(*clip, px, py, 0.0, 0.0) {
                return (
                    bg_by_id.get(&id.to_string()).cloned().unwrap_or_default(),
                    cursor.clone(),
                );
            }
        }
        (String::new(), String::new())
    }

    fn near(a: f32, b: f64, tol: f64) -> bool {
        ((a as f64 - b).abs()) <= tol
    }

    #[test]
    fn threeway_matrix() {
        let manifest = env!("CARGO_MANIFEST_DIR");
        let path = std::env::var("GX_THREEWAY_CASES")
            .unwrap_or_else(|_| format!("{}/../../../testdata/threeway/cases.json", manifest));
        let raw =
            std::fs::read_to_string(&path).unwrap_or_else(|e| panic!("读取用例 {}: {}", path, e));
        let suite: serde_json::Value = serde_json::from_str(&raw).unwrap();
        let (vw, vh) = {
            let vp = suite["viewport"].as_array().unwrap();
            (
                vp[0].as_f64().unwrap() as f32,
                vp[1].as_f64().unwrap() as f32,
            )
        };

        let mut fails: Vec<String> = vec![];
        let mut case_reports: Vec<serde_json::Value> = vec![];
        for case in suite["cases"].as_array().unwrap() {
            let name = case["name"].as_str().unwrap_or("?").to_string();
            let mut counter = 0usize;
            let mut bg_by_id = std::collections::HashMap::new();
            let root = tw_build(&case["scene"], &mut counter);
            // 构造后收集 id → bg 便于命中目标识别。
            fn collect(n: &SceneNode, m: &mut std::collections::HashMap<String, String>) {
                if let Some(bg) = n.props.get("bg") {
                    m.insert(n.id.clone(), bg.clone());
                }
                for c in &n.children {
                    collect(c, m);
                }
            }
            collect(&root, &mut bg_by_id);
            let dl = DisplayList::from_scene(&root, vw, vh, &mut crate::layout::LayoutCache::new());
            let (rects, texts) = tw_observe(&dl, &bg_by_id);
            let mut case_fails: Vec<String> = vec![];
            let first_rect = |bg: &str| rects.iter().find(|r| r.bg == bg).cloned();
            let first_text = |s: &str| texts.iter().find(|t| t.text == s).cloned();

            for e in case["expect"].as_array().unwrap() {
                let kind = e["kind"].as_str().unwrap_or("");
                match kind {
                    "rect" | "text" | "text-dx" | "text-dy" | "order" | "hit" => {}
                    _ => continue, // style/css 仅 Web 端评估
                }
                match kind {
                    "rect" => {
                        let bg = e["bg"].as_str().unwrap_or("");
                        match first_rect(bg) {
                            None => case_fails.push(format!(
                                "[rust] {}: rect {} 未绘制（属性未消费或颜色未透传）",
                                name, bg
                            )),
                            Some(r) => {
                                for (got, want, label) in [
                                    (r.x, e["x"].as_f64().unwrap_or(0.0), "x"),
                                    (r.y, e["y"].as_f64().unwrap_or(0.0), "y"),
                                    (r.w, e["w"].as_f64().unwrap_or(0.0), "w"),
                                    (r.h, e["h"].as_f64().unwrap_or(0.0), "h"),
                                ] {
                                    let tol = e["tol"].as_f64().unwrap_or(1.0);
                                    if !near(got, want, tol) {
                                        case_fails.push(format!(
                                            "[rust] {}: rect {} {}={:.1} want {}",
                                            name, bg, label, got, want
                                        ));
                                    }
                                }
                            }
                        }
                    }
                    "text" | "text-dx" | "text-dy" => {
                        let txt = e["text"].as_str().unwrap_or("");
                        match first_text(txt) {
                            None => case_fails.push(format!(
                                "[rust] {}: text {:?} 未绘制（文本属性未消费或内容被错误改写）",
                                name, txt
                            )),
                            Some(t) => {
                                match kind {
                                    "text-dx" => {
                                        let want = e["x"].as_f64().unwrap_or(0.0);
                                        let tol = e["tol"].as_f64().unwrap_or(1.0);
                                        if !near(t.x, want, tol) {
                                            case_fails.push(format!(
                                                "[rust] {}: text {:?} x={:.1} want {}(±{})",
                                                name, txt, t.x, want, tol
                                            ));
                                        }
                                    }
                                    "text-dy" => {
                                        let anc = e["anchor"].as_str().unwrap_or("");
                                        match first_text(anc) {
                                            None => case_fails.push(format!(
                                                "[rust] {}: anchor text {:?} 未绘制",
                                                name, anc
                                            )),
                                            Some(a) => {
                                                let want = e["dy"].as_f64().unwrap_or(0.0);
                                                let tol = e["tol"].as_f64().unwrap_or(1.0);
                                                if !near(t.y - a.y, want, tol) {
                                                    case_fails.push(format!("[rust] {}: text {:?} dy={:.1} want {}(±{})", name, txt, t.y - a.y, want, tol));
                                                }
                                            }
                                        }
                                    }
                                    _ => {}
                                }
                            }
                        }
                    }
                    "order" => {
                        let bgs: Vec<&str> = e["bgs"]
                            .as_array()
                            .unwrap()
                            .iter()
                            .filter_map(|v| v.as_str())
                            .collect();
                        let mut pos: Vec<usize> = vec![];
                        for bg in &bgs {
                            match rects.iter().position(|r| r.bg == *bg) {
                                Some(i) => pos.push(i),
                                None => {
                                    case_fails.push(format!(
                                        "[rust] {}: order {:?} 有 bg 未绘制",
                                        name, bgs
                                    ));
                                    pos.clear();
                                    break;
                                }
                            }
                        }
                        if !pos.is_empty() {
                            for w in pos.windows(2) {
                                if w[0] >= w[1] {
                                    case_fails.push(format!(
                                        "[rust] {}: order {:?} z-index 绘制顺序不符",
                                        name, bgs
                                    ));
                                    break;
                                }
                            }
                        }
                    }
                    "hit" => {
                        let px = e["x"].as_f64().unwrap_or(0.0) as f32;
                        let py = e["y"].as_f64().unwrap_or(0.0) as f32;
                        let (bg, cursor) = tw_hit_at(&dl, &bg_by_id, px, py);
                        let want_target = e["target"].as_str().unwrap_or("");
                        if want_target.is_empty() {
                            if !bg.is_empty() {
                                case_fails.push(format!(
                                    "[rust] {}: hit({},{}) 命中了 {}，期望空白区",
                                    name, px, py, bg
                                ));
                            }
                        } else if bg != want_target {
                            case_fails.push(format!("[rust] {}: hit({},{}) = {}, want {}（pointer-events 命中豁免或 z 序不符）", name, px, py, bg, want_target));
                        }
                        if let Some(wc) = e["cursor"].as_str() {
                            if !wc.is_empty() && cursor != wc {
                                case_fails.push(format!(
                                    "[rust] {}: hit({},{}) cursor = {}, want {}",
                                    name, px, py, cursor, wc
                                ));
                            }
                        }
                    }
                    _ => {}
                }
            }
            // 收集 per-case 结果（供报告落盘与总断言）。
            case_reports.push(serde_json::json!({
                "name": name,
                "pass": case_fails.is_empty(),
                "fails": case_fails,
            }));
            for f in &case_fails {
                fails.push(f.clone());
            }
        }

        // 报告落盘（供脚本汇总矩阵）。
        if let Ok(out) = std::env::var("GX_THREEWAY_OUT") {
            let report = serde_json::json!({"backend": "rust", "cases": case_reports});
            let _ = std::fs::write(
                &out,
                serde_json::to_string_pretty(&report).unwrap_or_default(),
            );
        }
        assert!(
            fails.is_empty(),
            "三端矩阵 Rust 端失败 {} 项:\n{}",
            fails.len(),
            fails.join("\n")
        );
    }
}

/// 按 nid 在镜像树找节点并执行补丁（滚动偏移写回用）。
fn find_by_nid_mut(
    root: &mut crate::scene::SceneNode,
    nid: u64,
    patch: &mut dyn FnMut(&mut crate::scene::SceneNode),
) -> bool {
    if root.nid == nid {
        patch(root);
        return true;
    }
    for c in &mut root.children {
        if find_by_nid_mut(c, nid, patch) {
            return true;
        }
    }
    false
}

/// 从 DisplayList 提取区域级穿透的输入形状（物理像素）。见 take_region_rects。
fn region_rects(
    dl: &DisplayList,
    scale: f32,
    phys_w: u32,
    phys_h: u32,
) -> Vec<crate::passthrough::InputRect> {
    let mut rects: Vec<crate::passthrough::InputRect> = Vec::new();
    let mut push = |x: f32, y: f32, w: f32, h: f32| {
        // 物理像素 + 窗口边界裁剪（与 [0,0,phys_w,phys_h] 求交）。
        let mut x0 = (x * scale).floor() as i32;
        let mut y0 = (y * scale).floor() as i32;
        let mut x1 = ((x + w) * scale).ceil() as i32;
        let mut y1 = ((y + h) * scale).ceil() as i32;
        x0 = x0.max(0);
        y0 = y0.max(0);
        x1 = x1.min(phys_w as i32);
        y1 = y1.min(phys_h as i32);
        if x1 - x0 <= 0 || y1 - y0 <= 0 {
            return;
        }
        rects.push(crate::passthrough::InputRect {
            x: x0,
            y: y0,
            w: x1 - x0,
            h: y1 - y0,
        });
    };
    for sh in &dl.shapes {
        // 真正画出来的内容：纯色底（bg_present）、渐变（shader 画底）、
        // 图片、canvas、video、边框；opacity==0 的节点整树不可见。
        let painted = sh.bg_present
            || sh.bg_gradient.is_some()
            || sh.img.is_some()
            || sh.canvas.is_some()
            || sh.video.is_some()
            || sh.border.is_some();
        if painted && sh.alpha > 0.0 {
            push(sh.x, sh.y, sh.w, sh.h);
        }
    }
    // 可交互命中框：无 bg 的按钮/文字输入等也必须可点（与 Web 命中语义一致）。
    for hb in &dl.hitboxes {
        push(hb.0, hb.1, hb.2, hb.3);
    }
    rects
}

#[cfg(test)]
mod region_tests {
    use super::*;

    #[test]
    fn region_rects_filters_content_and_scale() {
        let mut dl = DisplayList::new();
        // 有 bg 的内容矩形（逻辑 10,20,30,40 → 物理 ×2）。
        dl.shapes.push(ShapeDraw {
            x: 10.0, y: 20.0, w: 30.0, h: 40.0,
            bg: (255, 0, 0),
            bg_present: true,
            bg_gradient: None,
            radius: 0.0,
            border: None,
            shadow: None,
            clip: ClipRect::default(),
            img: None,
            canvas: None,
            alpha: 1.0,
            video: None,
            video_id: String::new(),
        });
        // 透明节点（无 bg 无图片）：不算内容。
        dl.shapes.push(ShapeDraw {
            x: 0.0, y: 0.0, w: 500.0, h: 500.0,
            bg: (0, 0, 0),
            bg_present: false,
            bg_gradient: None,
            radius: 0.0,
            border: None,
            shadow: None,
            clip: ClipRect::default(),
            img: None,
            canvas: None,
            alpha: 1.0,
            video: None,
            video_id: String::new(),
        });
        // opacity=0 的内容：整树不可见，剔除。
        dl.shapes.push(ShapeDraw {
            x: 1.0, y: 1.0, w: 9.0, h: 9.0,
            bg: (0, 255, 0),
            bg_present: true,
            bg_gradient: None,
            radius: 0.0,
            border: None,
            shadow: None,
            clip: ClipRect::default(),
            img: None,
            canvas: None,
            alpha: 0.0,
            video: None,
            video_id: String::new(),
        });
        // 无 bg 的可交互命中框：必须可点。
        dl.hitboxes.push((100.0, 200.0, 50.0, 25.0, 7, "onclick".into(), false, ClipRect::default(), String::new()));

        // 部分越界命中框（右下角 20x10 逻辑像素在窗外）：裁剪到窗口物理边界（500x450）。
        dl.hitboxes.push((240.0, 220.0, 40.0, 40.0, 9, "onclick".into(), false, ClipRect::default(), String::new()));

        let rects = region_rects(&dl, 2.0, 500, 450);
        assert_eq!(rects.len(), 3, "透明容器与 opacity=0 剔除；bg 内容 + 命中框 + 裁剪命中框保留");
        assert_eq!(rects[0], crate::passthrough::InputRect { x: 20, y: 40, w: 60, h: 80 });
        assert_eq!(rects[1], crate::passthrough::InputRect { x: 200, y: 400, w: 100, h: 50 });
        assert_eq!(rects[2], crate::passthrough::InputRect { x: 480, y: 440, w: 20, h: 10 }, "越界部分裁掉");
    }
}
