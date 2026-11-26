//! 渲染内核：消费二进制协议 FrameView（零拷贝视图），用 tiny-skia 图形 + glyphon 文本渲染。
//! 布局采用与 host flex 语义一致的主轴排布（fragment 透明透传）。

use crate::binprotocol::NodeType;
use crate::protocol::InputEvent;
use glyphon::{
    Attrs, Buffer, Cache, Color, Family, FontSystem, Metrics, Resolution, Shaping, SwashCache,
    TextArea, TextAtlas, TextBounds, TextRenderer, Viewport, Weight,
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

// 文本测量上下文：继承后的字号 + 行高（像素，0=默认 1.35×size）+ 文本内容。
// Taffy 在布局期回调测宽。行高与折叠语义必须与 emit/绘制端同口径，否则
// 布局高度与文字落点错位。
#[derive(Clone)]
struct TextMeasure { text: String, size: f32, line_height: f32 }

fn measure_text(
    known: taffy::geometry::Size<Option<f32>>,
    _avail: taffy::geometry::Size<taffy::style::AvailableSpace>,
    ctx: Option<&mut TextMeasure>,
) -> taffy::geometry::Size<f32> {
    if let taffy::geometry::Size { width: Some(w), height: Some(h) } = known {
        return taffy::geometry::Size { width: w, height: h };
    }
    let Some(c) = ctx else { return taffy::geometry::Size::ZERO; };
    // CJK/全角按 1em，ASCII 按 0.56em，行高 1.35em（或显式 line-height）。
    let w = c.text.chars()
        .map(|ch| if ch.is_ascii() { 0.56 } else { 1.0 })
        .sum::<f32>() * c.size;
    let h = if c.line_height > 0.0 { c.line_height } else { c.size * 1.35 };
    taffy::geometry::Size { width: w, height: h }
}

// ---------- 浮层（popover/modal/portal/scrim）协议辅助 ----------
// 与 Skia 后端 backend/skia/overlay.go 完全一致的语义：所有约定走 props 全量透传，
// gx-role 标记浮层角色，gx-anchor 引用锚点 id，gx-placement/gx-offset 定位。

fn gx_role(n: &crate::scene::SceneNode) -> &str {
    n.props.get("gx-role").map(|s| s.trim().to_ascii_lowercase())
        .map(|s| match s.as_str() {
            "popover" => "popover", "modal" => "modal",
            "portal" => "portal", "scrim" => "scrim", _ => "",
        })
        .unwrap_or("")
}
fn is_overlay(n: &crate::scene::SceneNode) -> bool {
    matches!(gx_role(n), "popover" | "modal" | "portal" | "scrim")
}
fn is_popover(n: &crate::scene::SceneNode) -> bool { gx_role(n) == "popover" }
fn is_modal(n: &crate::scene::SceneNode) -> bool { gx_role(n) == "modal" }
fn is_portal(n: &crate::scene::SceneNode) -> bool { gx_role(n) == "portal" }
fn is_scrim(n: &crate::scene::SceneNode) -> bool { gx_role(n) == "scrim" }
/// 内容型浮层（非遮罩）：统一在 scrim 之后绘制。
fn is_content_overlay(n: &crate::scene::SceneNode) -> bool {
    is_popover(n) || is_modal(n) || is_portal(n)
}

/// 视口居中浮层左上角（与 Skia placeCentered 对齐）。
fn place_centered(pw: f32, ph: f32, avail_w: f32, avail_h: f32) -> (f32, f32) {
    let pw = pw.min(avail_w);
    let ph = ph.min(avail_h);
    (((avail_w - pw) / 2.0).max(0.0), ((avail_h - ph) / 2.0).max(0.0))
}

/// 浮层定位结果：side(bottom/top) + align(start/center/end)。
fn parse_placement(s: &str) -> (&'static str, &'static str) {
    let lower = s.trim().to_ascii_lowercase();
    let parts: Vec<&str> = lower.split('-').collect();
    let side = match parts.first().copied() { Some("top") => "top", _ => "bottom" };
    let align = match parts.get(1).copied() {
        Some("center") => "center", Some("end") => "end", _ => "start",
    };
    (side, align)
}

/// 计算浮层左上角绝对坐标，空间不足时翻转（与 Skia placeOverlay 对齐）。
fn place_overlay(
    ax: f32, ay: f32, aw: f32, ah: f32,
    pw: f32, ph: f32, placement: &str, offset: f32,
    avail_w: f32, avail_h: f32,
) -> (f32, f32) {
    let (side, align) = parse_placement(placement);
    let below = ay + ah + offset + ph <= avail_h;
    let above = ay - offset - ph >= 0.0;
    let side = if side == "bottom" && !below && above { "top" }
               else if side == "top" && !above && below { "bottom" }
               else { side };
    let mut y = if side == "top" { ay - offset - ph } else { ay + ah + offset };
    if y < 0.0 { y = 0.0; }
    if y + ph > avail_h && ph <= avail_h { y = avail_h - ph; }

    let end_x = ax + aw - pw;
    let mut x = match align {
        "center" => ax + (aw - pw) / 2.0,
        "end" => end_x,
        _ => ax,
    };
    if x < 0.0 {
        if align == "end" && ax >= 0.0 { x = ax; } else { x = 0.0; }
    } else if x + pw > avail_w {
        if align == "start" && end_x >= 0.0 { x = end_x; } else { x = avail_w - pw; }
    }
    (x, y)
}

/// 展开一个节点的可见子节点（Fragment 透明摊平），与 build/emit 顺序一致。
fn flat_children<'a>(n: &'a crate::scene::SceneNode, out: &mut Vec<&'a crate::scene::SceneNode>) {
    for c in &n.children {
        if c.typ == NodeType::Fragment { flat_children(c, out); } else { out.push(c); }
    }
}

fn align_up(v: u32, a: u32) -> u32 {
    (v + a - 1) / a * a
}

/// box-shadow 的多层半透明圆角矩形近似（与 Skia 端 paintShadow 同算法）。
fn paint_shadow_tiny(
    pm: &mut tiny_skia::Pixmap,
    s: &ShapeDraw,
    sh: &crate::style::BoxShadow,
) {
    use tiny_skia::{Color, FillRule, Paint, Transform};
    if sh.blur <= 0.0 && sh.spread <= 0.0 && sh.offset_x == 0.0 && sh.offset_y == 0.0 {
        return;
    }
    let base_a = if sh.color.3 > 0 { sh.color.3 as f32 } else { 90.0 };
    const LAYERS: i32 = 6;
    for i in (1..=LAYERS).rev() {
        let t = i as f32 / LAYERS as f32;
        let expand = sh.spread + sh.blur * t;
        let x = s.x + sh.offset_x - expand;
        let y = s.y + sh.offset_y - expand;
        let w = s.w + 2.0 * expand;
        let h = s.h.max(1.0) + 2.0 * expand;
        if w <= 0.0 || h <= 0.0 { continue; }
        let a = (base_a * (1.0 - t) / LAYERS as f32 * 1.8).clamp(0.0, 255.0) as u8;
        if let Some(path) = round_rect_path(x, y, w, h, s.radius + expand) {
            let mut p = Paint::default();
            p.set_color(Color::from_rgba8(sh.color.0, sh.color.1, sh.color.2, a));
            p.anti_alias = true;
            // 阴影也受祖先 overflow 裁剪：用交集矩形限制绘制范围。
            if let Some((cx, cy, cw, ch)) = s.clip {
                let ix = x.max(cx); let iy = y.max(cy);
                let ir = (x + w).min(cx + cw); let ib = (y + h).min(cy + ch);
                if ir <= ix || ib <= iy { continue; }
            }
            pm.fill_path(&path, &p, FillRule::Winding, Transform::identity(), None);
        }
    }
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

/// 把统一语义的 CSS linear-gradient 转成 tiny-skia shader。
/// 角度换算对齐 CSS：0°=向上，90°=向右，180°=向下；渐变线穿过盒子中心，
/// 长度取盒子在渐变方向上的投影（与 Go paintGradientBackgroundShader 同算法）。
fn gradient_shader(
    g: &crate::text_effects::LinearGradient,
    box_rect: (f32, f32, f32, f32),
) -> Option<tiny_skia::Shader<'static>> {
    use tiny_skia::{Color, GradientStop, Point, SpreadMode, Transform};
    let (x, y, w, h) = box_rect;
    if w <= 0.0 || h <= 0.0 || g.stops.is_empty() {
        return None;
    }
    let rad = g.angle.to_radians();
    // CSS 角度以"向上"为 0 顺时针，转成方向向量 (dx, dy)（屏幕 y 向下）。
    let dx = rad.sin();
    let dy = -rad.cos();
    let cx = x + w / 2.0;
    let cy = y + h / 2.0;
    // 渐变线长度 = 盒子宽高在方向上的投影绝对值之和。
    let len = (w * dx.abs() + h * dy.abs()) * 0.5;
    let start = Point::from_xy(cx - dx * len, cy - dy * len);
    let end = Point::from_xy(cx + dx * len, cy + dy * len);

    let stops: Vec<GradientStop> = g.stops.iter().map(|s| {
        let pos = s.offset.unwrap_or_else(|| {
            // ch 边界型色段无百分比：按最大 end_ch 归一化，保持相对位置。
            let max_ch = g.stops.last().map(|x| x.end_ch.max(1)).unwrap_or(1) as f32;
            s.end_ch as f32 / max_ch
        });
        GradientStop::new(pos, Color::from_rgba8(s.color.0, s.color.1, s.color.2, s.color.3))
    }).collect();
    tiny_skia::LinearGradient::new(start, end, stops, SpreadMode::Pad, Transform::identity())
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

/// 从 CSS font-family 栈解析出 cosmic-text 可用的族名。
/// 返回 None 表示沿用默认 SansSerif（含 system-ui/-apple-system/sans-serif 栈）。
/// 栈里的等宽意图（monospace/ui-monospace/Menlo/Consolas/SFMono...）映射到已加载的
/// DejaVu Sans Mono；具体族名原样交给 cosmic-text，未加载时它会回退默认。
fn parse_font_family(props: &std::collections::HashMap<String, String>) -> Option<String> {
    let stack = props.get("font-family")?.as_str();
    if stack.trim().is_empty() {
        return None;
    }
    let mut want_mono = false;
    for raw in stack.split(',') {
        let name = raw.trim().trim_matches(|c| c == '"' || c == '\'');
        if name.is_empty() {
            continue;
        }
        let lower = name.to_ascii_lowercase();
        if matches!(lower.as_str(),
            "monospace" | "ui-monospace" | "fixed" | "menlo" | "consolas" |
            "sfmono-regular" | "dejavu sans mono" | "noto sans mono" | "liberation mono" | "courier new")
        {
            want_mono = true;
            continue;
        }
        if matches!(lower.as_str(),
            "sans-serif" | "serif" | "system-ui" | "system" | "-apple-system" | "ui-sans-serif")
        {
            continue;
        }
        // 其余具体族名原样使用（cosmic-text 未加载会自行回退）。
        return Some(name.to_string());
    }
    if want_mono {
        Some("DejaVu Sans Mono".to_string())
    } else {
        None
    }
}

/// 按 rune 区间 [start,end) 计算文本的像素 x 范围，口径与 measure_text 一致
/// （ASCII 0.56em、其余 1em）。用于选区背景矩形定位。
fn selection_px_range(text: &str, start: usize, end: usize, size: f32) -> (f32, f32) {
    let mut x0 = 0.0;
    let mut x1 = 0.0;
    for (i, ch) in text.chars().enumerate() {
        let w = if ch.is_ascii() { 0.56 } else { 1.0 } * size;
        if i < start { x0 += w; }
        if i < end { x1 += w; }
    }
    (x0, x1)
}

// ---------- DisplayList ----------

/// 裁剪矩形（画布绝对坐标）；None 表示不裁剪。
type ClipRect = Option<(f32, f32, f32, f32)>; // x, y, w, h

/// 两个裁剪矩形求交；None 视作无限大。
fn clip_intersect(a: ClipRect, b: ClipRect) -> ClipRect {
    match (a, b) {
        (None, x) | (x, None) => x,
        (Some((ax, ay, aw, ah)), Some((bx, by, bw, bh))) => {
            let x = ax.max(bx);
            let y = ay.max(by);
            let r = (ax + aw).min(bx + bw);
            let bot = (ay + ah).min(by + bh);
            if r > x && bot > y { Some((x, y, r - x, bot - y)) } else { Some((x, y, 0.0, 0.0)) }
        }
    }
}

/// 矩形是否被裁剪完全剔除。
fn clip_rejects(c: ClipRect, x: f32, y: f32, w: f32, h: f32) -> bool {
    match c {
        None => false,
        Some((cx, cy, cw, ch)) => x > cx + cw || x + w < cx || y > cy + ch || y + h < cy,
    }
}

/// 矩形与裁剪矩形求交，返回交集 (x,y,w,h)；完全在外返回 None。
fn clip_rect_xywh(c: ClipRect, x: f32, y: f32, w: f32, h: f32) -> Option<(f32, f32, f32, f32)> {
    match c {
        None => Some((x, y, w, h)),
        Some((cx, cy, cw, ch)) => {
            let nx = x.max(cx);
            let ny = y.max(cy);
            let nr = (x + w).min(cx + cw);
            let nb = (y + h).min(cy + ch);
            if nr > nx && nb > ny { Some((nx, ny, nr - nx, nb - ny)) } else { None }
        }
    }
}

#[derive(Clone)]
struct TextDraw {
    /// 稳定节点 ID：作为字形缓存 key，滚动时同一 item 的 ID 不变，
    /// 使未变文本的字形 buffer 可跨帧复用（只 shape 新增/变化项）。
    id: String,
    text: String,
    x: f32,
    y: f32,
    size: f32,
    color: (u8, u8, u8),
    clip: ClipRect,
    /// 统一 TextEffects 解析出的渐变着色分段（文本, RGB）。
    /// 空表示普通单色文字，走 default_color 快路径；非空走 glyphon 富文本 span。
    spans: Vec<(String, (u8, u8, u8))>,
    /// 三端统一 selection 协议解析出的选区；None 表示该文本无选区。
    selection: Option<SelectionDraw>,
    /// font-family 解析出的族名；None 表示默认 SansSerif。
    family: Option<String>,
    /// font-weight 数值（100..900）；None 表示默认字重。纳入 text_sig：
    /// 字重影响字形选择，必须重新 shape。
    weight: Option<u32>,
    /// line-height（像素，0=默认 1.35×size）：Metrics.line_height 决定行盒内基线。
    line_height: f32,
    /// opacity（含声明式动画采样后的值）用于文字 alpha，1.0 为不透明。
    alpha: f32,
}

/// object-fit：图片如何映射进布局盒（与 Skia 端 image.go 语义一致）。
#[derive(Clone, Copy, PartialEq)]
enum ObjectFit { Fill, Contain, Cover }

impl ObjectFit {
    fn parse(s: Option<&str>) -> Self {
        match s.unwrap_or("").trim().to_ascii_lowercase().as_str() {
            "contain" => ObjectFit::Contain,
            "cover" => ObjectFit::Cover,
            _ => ObjectFit::Fill,
        }
    }
}

/// 选区绘制指令（三端统一 selection 协议的 Rust 侧表示）。
/// rgb 为 &::selection 的 color（覆盖选中字形前景），bg 为背景色含 alpha；
/// range 为 selection-range 的 rune 半开区间 [start,end)。
#[derive(Clone, Debug)]
struct SelectionDraw {
    start: usize,
    end: usize,
    bg: (u8, u8, u8, u8),
    color: Option<(u8, u8, u8)>,
}

/// 从节点标量 props 解析 selection 协议：&::selection JSON 子表 + selection-range。
/// 两者缺一不可；range 非法返回 None（无选区）。JSON 用 serde_json，解析失败静默回退。
fn parse_selection(props: &std::collections::HashMap<String, String>) -> Option<SelectionDraw> {
    let range = props.get("selection-range")?.trim();
    let (a, b) = range.split_once(|c| c == ':' || c == ',')?;
    let start: usize = a.trim().parse().ok()?;
    let end: usize = b.trim().parse().ok()?;
    if end <= start { return None; }
    let raw = props.get("&::selection")?;
    let v: serde_json::Value = serde_json::from_str(raw).ok()?;
    let bg_s = v.get("bg").and_then(|x| x.as_str()).unwrap_or("");
    let color_s = v.get("color").and_then(|x| x.as_str()).unwrap_or("");
    let bg = crate::style::parse_color_rgba(bg_s)?;
    let color = crate::style::parse_color_rgba(color_s)
        .map(|(r, g, b, _)| (r, g, b));
    Some(SelectionDraw { start, end, bg, color })
}

/// 单条文本的内容签名：字形 buffer 取决于 (内容, 字号, 着色分段)，与坐标无关。
/// 缓存按 node_id 索引，同一 ID 签名不变即可复用其 buffer。
/// 渐变分段纳入签名：颜色/分界变化时重排 span，但坐标滚动不影响签名。
fn text_sig(t: &TextDraw) -> String {
    let mut s = String::with_capacity(t.text.len() + 16);
    s.push_str(&t.text);
    s.push('@');
    s.push_str(&format!("{:.2}", t.size));
    // 字体族变化需重新 shape（等宽/比例字形的字形与度量不同）。
    if let Some(f) = &t.family {
        s.push_str(&format!("@fam{}", f));
    }
    // 字重影响字形选择，纳入签名；行高不影响字形可省略（跟随 Metrics）。
    if let Some(wgt) = t.weight {
        s.push_str(&format!("@w{}", wgt));
    }
    if !t.spans.is_empty() {
        s.push('@');
        for (seg, c) in &t.spans {
            s.push_str(seg);
            s.push_str(&format!(":{:02x}{:02x}{:02x};", c.0, c.1, c.2));
        }
    }
    // 选区前景色改变字形着色，纳入签名以触发重排；背景色不影响字形可省略。
    if let Some(sel) = &t.selection {
        if let Some((r, g, b)) = sel.color {
            s.push_str(&format!("@sel{}:{}:{:02x}{:02x}{:02x}", sel.start, sel.end, r, g, b));
        }
    }
    s
}

/// 按稳定 node_id 缓存的字形条目：签名变了才需要重新 shape。
#[derive(Clone)]
struct GlyphEntry {
    sig: String,
    buffer: Buffer,
}

/// 字形增量复用计划（纯逻辑，便于单测）：
/// 返回本帧需要重新 shape 的文本下标，以及需要从缓存淘汰的 node_id。
/// - 当前文本的 id 不在缓存，或签名变化 → 需要 shape（新增项 / 内容变化）
/// - 缓存里有但当前帧不再出现 → 淘汰（滚出视口的项）
fn plan_glyphs(
    texts: &[TextDraw],
    cache: &std::collections::HashMap<String, GlyphEntry>,
) -> (Vec<usize>, Vec<String>) {
    let mut shaped = Vec::new();
    let mut live = std::collections::HashSet::with_capacity(texts.len());
    for (i, t) in texts.iter().enumerate() {
        live.insert(t.id.clone());
        let sig = text_sig(t);
        let hit = cache.get(&t.id).map(|e| e.sig == sig).unwrap_or(false);
        if !hit {
            shaped.push(i);
        }
    }
    let evicted = cache.keys().filter(|id| !live.contains(*id)).cloned().collect();
    (shaped, evicted)
}

#[derive(Clone)]
struct ShapeDraw {
    x: f32,
    y: f32,
    w: f32,
    h: f32,
    bg: (u8, u8, u8),
    bg_present: bool,
    /// 通用 CSS linear-gradient 背景（无 gradient-text 标记时作用于容器本身）。
    /// 与文字渐变互斥：带 gradient-text 的 div 其渐变只裁剪到文字，不画背景。
    bg_gradient: Option<crate::text_effects::LinearGradient>,
    radius: f32,
    border: Option<((u8, u8, u8), f32)>,
    shadow: Option<crate::style::BoxShadow>,
    clip: ClipRect,
    /// img 原语：解码后的图片 + object-fit；None 表示普通矩形。
    img: Option<(std::sync::Arc<tiny_skia::Pixmap>, ObjectFit)>,
    /// canvas 原语：命令式绘制指令；None 表示普通节点。
    canvas: Option<std::sync::Arc<Vec<crate::canvas::CanvasOp>>>,
    /// opacity（含声明式动画采样后的值）用于背景 alpha，1.0 为不透明。
    alpha: f32,
    /// video 原语：ffmpeg 解码的当前帧；None 表示普通节点/无帧。
    video: Option<std::sync::Arc<crate::video::VideoFrame>>,
    /// video 节点 ID（用于取帧）；普通节点为空。
    video_id: String,
}

/// 苹果风格滚动条的几何（overlay，不占布局；画在容器右内侧）。
#[derive(Clone, Debug, PartialEq)]
struct ScrollBar {
    /// track 左上角与尺寸（track 覆盖整个容器高度，宽 TRACK_W）
    x: f32, y: f32, track_h: f32,
    /// thumb 顶与高
    thumb_y: f32, thumb_h: f32,
    /// 透明度 0..1（淡入淡出由 render 阶段按滚动活动度驱动）
    alpha: f32,
}

/// macOS 滚动条常量：细、圆头、半透明、overlay。
const SB_TRACK_W: f32 = 8.0;   // track 宽
const SB_THUMB_MIN: f32 = 32.0; // thumb 最小高度

/// 由容器几何与内容高度算滚动条。内容未溢出返回 None（不显示）。
/// 纯函数，便于单测。
fn scrollbar_geometry(
    cx: f32, cy: f32, cw: f32, ch: f32,
    content_h: f32, scroll_top: f32, alpha: f32,
) -> Option<ScrollBar> {
    let max_scroll = content_h - ch;
    if max_scroll <= 0.5 || ch <= 0.0 || cw <= 0.0 {
        return None; // 内容未溢出：不显示滚动条
    }
    let thumb_h = SB_THUMB_MIN.max(ch * ch / content_h).min(ch);
    let travel = ch - thumb_h; // thumb 可移动的垂直距离
    let st = scroll_top.max(0.0).min(max_scroll);
    let thumb_y = cy + travel * (st / max_scroll);
    // track 贴在容器右内侧，留 2px 边距；thumb 与 track 同宽、居中。
    Some(ScrollBar {
        x: cx + cw - SB_TRACK_W - 2.0,
        y: cy,
        track_h: ch,
        thumb_y,
        thumb_h,
        alpha: alpha.max(0.0).min(1.0),
    })
}

#[derive(Clone)]
struct DisplayList {
    shapes: Vec<ShapeDraw>,
    texts: Vec<TextDraw>,
    /// 布局回流：每个节点的 (id, w, h)，emit 时按 Taffy 结果收集，
    /// 渲染后经 eventCb 送回 Host，供虚拟列表等组件按 node_id 订阅实测高度。
    measured: Vec<(String, f32, f32)>,
    /// 需要显示滚动条的容器几何（由 emit 计算内容高度后生成）
    scrollbars: Vec<ScrollBar>,
    /// (x, y, w, h, node_id, event, is_overlay, clip)
    // 第 9 元素是节点 cursor 属性（CSS cursor 样式，宿主消费）。
    hitboxes: Vec<(f32, f32, f32, f32, String, String, bool, ClipRect, String)>,
    /// (x, y, w, h, node_id)
    inputs: Vec<(f32, f32, f32, f32, String)>,
}

impl DisplayList {
    fn new() -> Self {
        DisplayList { shapes: vec![], texts: vec![], measured: vec![], scrollbars: vec![], hitboxes: vec![], inputs: vec![] }
    }
    /// 把镜像场景树编译成绘制指令。
    /// 布局交给 Taffy 0.5（完整 Flexbox 引擎）——
    /// 支持完整的 justify-content / align-items / gap / flex-wrap，
    /// 组件包装层（Fragment）摊平为透明节点，文本节点用 measure 回调测宽。
    /// 根节点的 size 设为窗口尺寸，因此 align-items:center 有确定参照。
    fn from_scene(root: &crate::scene::SceneNode, avail_w: f32, avail_h: f32) -> Self {
        use taffy::prelude::*;

        let mut dl = DisplayList::new();

        // video 原语：先从场景树收集所有 video 节点的播放意图，同步解码器进程。
        // 必须在 emit 取帧之前调用，保证本帧拿到的是最新同步状态。
        fn collect_videos(n: &crate::scene::SceneNode, out: &mut Vec<(String, crate::video::VideoSpec)>) {
            if n.kind.eq_ignore_ascii_case("video") && !n.id.is_empty() {
                let g = |k: &str| n.props.get(k).map(|s| s.as_str()).unwrap_or("");
                let truthy = |k: &str| matches!(g(k), "true" | "1");
                out.push((n.id.clone(), crate::video::VideoSpec {
                    src: g("src").to_string(),
                    playback_id: g("playback-id").to_string(),
                    seek: g("seek-to").to_string(),
                    playing: truthy("playing"),
                    autoplay: truthy("autoplay"),
                    loop_: truthy("loop"),
                    muted: truthy("muted"),
                }));
            }
            for c in &n.children {
                collect_videos(c, out);
            }
        }
        let mut vspecs = Vec::new();
        collect_videos(root, &mut vspecs);
        crate::video::sync(&vspecs);

        let mut taffy: TaffyTree<TextMeasure> = TaffyTree::new();

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
        fn wrap_of(s: Option<&str>) -> FlexWrap {
            match s.unwrap_or("").trim().to_ascii_lowercase().as_str() {
                "wrap" => FlexWrap::Wrap,
                "wrap-reverse" => FlexWrap::WrapReverse,
                _ => FlexWrap::NoWrap,
            }
        }
        fn align_content_of(s: Option<&str>) -> Option<AlignContent> {
            match s.unwrap_or("").trim().to_ascii_lowercase().as_str() {
                "center" => Some(AlignContent::Center),
                "flex-start" | "start" => Some(AlignContent::FlexStart),
                "flex-end" | "end" => Some(AlignContent::FlexEnd),
                "space-between" => Some(AlignContent::SpaceBetween),
                "space-around" => Some(AlignContent::SpaceAround),
                "space-evenly" => Some(AlignContent::SpaceEvenly),
                "stretch" => Some(AlignContent::Stretch),
                _ => None,
            }
        }

        // 把可见子节点摊平（Fragment 透明），记录 (Taffy 节点, 原 SceneNode 引用)。
        // 同时收集节点的绘制属性，布局算完后统一生成 DisplayList。
        struct BuildCtx {
            taffy: TaffyTree<TextMeasure>,
        }

        /// 文本相关继承链（CSS 继承语义）：字重 / text-align / line-height(px) /
        /// white-space 折叠。build（测量）与 emit（绘制）共用同一份继承口径。
        #[derive(Clone, Copy, Default)]
        struct TextInherit {
            weight: Option<u32>,
            align: u8,
            line_h: f32,
            collapse_ws: bool,
        }

        impl TextInherit {
            /// 节点自身 props 覆盖继承值（font-weight/text-align/line-height/
            /// white-space 均为 CSS 继承属性）。
            fn derive(props: &std::collections::HashMap<String, String>, size: f32, parent: Self) -> Self {
                let line_h = props.get("line-height").and_then(|s| {
                    let (f, px) = crate::style::line_height_of(s);
                    if f > 0.0 { Some(f * size) } else if px > 0.0 { Some(px) } else { None }
                }).or(if parent.line_h > 0.0 { Some(parent.line_h) } else { None })
                    .unwrap_or(0.0);
                Self {
                    weight: crate::style::font_weight_from_props(props).or(parent.weight),
                    align: props.get("text-align").map(|s| crate::style::text_align_of(s)).unwrap_or(parent.align),
                    line_h,
                    collapse_ws: props.get("white-space").map(|s| crate::style::white_space_collapses(s)).unwrap_or(parent.collapse_ws),
                }
            }
        }

        fn build(
            ctx: &mut BuildCtx,
            node: &crate::scene::SceneNode,
            is_root: bool,
            parent_scrollable: bool,
            inherited_color: (u8, u8, u8),
            inherited_size: f32,
            avail_w: f32, avail_h: f32,
            tinh: TextInherit,
        ) -> taffy::NodeId {
            let p = |k: &str| node.props.get(k).map(|s| s.as_str());
            let color = p("color").map(parse_color).unwrap_or(inherited_color);
            let size = p("font-size").and_then(parse_px).unwrap_or(inherited_size);
            let my_tinh = TextInherit::derive(&node.props, size, tinh);

            if node.typ == NodeType::Text {
                let mut ts = Style::default();
                // overflow 容器的子树不参与压缩，否则内容被压进视口、永不溢出，
                // 滚动与滚动条都失效（标准滚动容器语义：子项 flex-shrink:0）。
                if parent_scrollable { ts.flex_shrink = 0.0; }
                // white-space:normal/nowrap 与绘制端同口径折叠空白。
                let text = if my_tinh.collapse_ws {
                    crate::style::collapse_whitespace(&node.text)
                } else {
                    node.text.clone()
                };
                return ctx.taffy.new_leaf_with_context(
                    ts,
                    TextMeasure { text, size, line_height: my_tinh.line_h },
                ).expect("taffy text leaf");
            }

            // padding：1~4 值 CSS 简写（与 Skia/Web 对齐）；负值无意义裁到 0。
            let zero = length(0.0);
            let pad_rect: taffy::geometry::Rect<taffy::style::LengthPercentage> =
                p("padding").and_then(crate::style::parse_edges)
                .map(|e| Rect {
                    top: length(e[0].max(0.0)), right: length(e[1].max(0.0)),
                    bottom: length(e[2].max(0.0)), left: length(e[3].max(0.0)),
                })
                .unwrap_or(Rect { top: zero, right: zero, bottom: zero, left: zero });
            // margin：1~4 值简写 + 四边单独覆盖（CSS 级联顺序），负值裁到 0。
            // taffy 的 margin 是 Rect<LengthPercentageAuto>，与 padding 类型不同。
            let auto_zero = taffy::style_helpers::length(0.0);
            let mut margin_rect: taffy::geometry::Rect<taffy::style::LengthPercentageAuto> =
                p("margin").and_then(crate::style::parse_edges)
                .map(|e| Rect {
                    top: length(e[0].max(0.0)), right: length(e[1].max(0.0)),
                    bottom: length(e[2].max(0.0)), left: length(e[3].max(0.0)),
                })
                .unwrap_or(Rect { top: auto_zero, right: auto_zero, bottom: auto_zero, left: auto_zero });
            for (i, key) in ["margin-top", "margin-right", "margin-bottom", "margin-left"].iter().enumerate() {
                if let Some(v) = p(key).and_then(parse_px) {
                    let d = length(v.max(0.0));
                    match i {
                        0 => margin_rect.top = d,
                        1 => margin_rect.right = d,
                        2 => margin_rect.bottom = d,
                        _ => margin_rect.left = d,
                    }
                }
            }
            // gap：简写兜底双轴；row-gap/column-gap 分别覆盖纵向/横向（CSS 语义，
            // 修正旧实现拿 column-gap 顶替 gap 用的问题）。
            let gap_base = p("gap").and_then(parse_px);
            let col_gap = p("column-gap").and_then(parse_px).or(gap_base).unwrap_or(0.0);
            let row_gap = p("row-gap").and_then(parse_px).or(gap_base).unwrap_or(0.0);
            // absolute：浮层/遮罩（gx-role）或显式 position:absolute 都脱离父 flex 流。
            // 浮层位置由第二遍按锚点矩形单独计算；通用 absolute 用 inset 由 Taffy 定位。
            let st = crate::style::parse_style(node);
            let is_abs = is_overlay(node) || st.position == crate::style::Position::Absolute;
            let mut style = Style {
                position: if is_abs { Position::Absolute } else { Position::Relative },
                flex_direction: dir_of(p("flex-direction").unwrap_or("column")),
                padding: pad_rect,
                margin: margin_rect,
                gap: Size { width: length(col_gap), height: length(row_gap) },
                align_items: align_of(p("align-items")),
                align_content: align_content_of(p("align-content")),
                flex_wrap: wrap_of(p("flex-wrap")),
                justify_content: justify_of(p("justify-content")),
                // overflow 容器的直接子项不压缩，保证内容可溢出 → 滚动可生效。
                flex_shrink: if parent_scrollable { 0.0 } else { 1.0 },
                ..Default::default()
            };
            // 显式 flex-grow / flex-shrink 属性覆盖默认（补齐与 Skia/Web 的消费差）。
            if let Some(v) = p("flex-grow").and_then(parse_px) { style.flex_grow = v.max(0.0); }
            if let Some(v) = p("flex-shrink").and_then(parse_px) { style.flex_shrink = v.max(0.0); }
            // min/max 尺寸约束。
            if let Some(v) = p("min-width").and_then(parse_px) { style.min_size.width = length(v); }
            if let Some(v) = p("min-height").and_then(parse_px) { style.min_size.height = length(v); }
            if let Some(v) = p("max-width").and_then(parse_px) { style.max_size.width = length(v); }
            if let Some(v) = p("max-height").and_then(parse_px) { style.max_size.height = length(v); }
            // inset：top/right/bottom/left（NaN=auto 跳过）。与 Skia 语义一致。
            if is_abs {
                let i = st.inset;
                if !i[0].is_nan() { style.inset.top = length(i[0]); }
                if !i[1].is_nan() { style.inset.right = length(i[1]); }
                if !i[2].is_nan() { style.inset.bottom = length(i[2]); }
                if !i[3].is_nan() { style.inset.left = length(i[3]); }
            }
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
            let child_scrollable = st.overflow != crate::style::Overflow::Visible;
            let child_nodes: Vec<taffy::NodeId> = kids.iter()
                .map(|c| build(ctx, c, false, child_scrollable, color, size, avail_w, avail_h, my_tinh))
                .collect();

            ctx.taffy.new_with_children(style, &child_nodes).expect("taffy container")
        }

        let mut ctx = BuildCtx { taffy };
        let root_node = build(&mut ctx, root, true, false, (226, 232, 240), 16.0, avail_w, avail_h, TextInherit::default());
        ctx.taffy.compute_layout_with_measure(
            root_node,
            Size { width: AvailableSpace::Definite(avail_w), height: AvailableSpace::Definite(avail_h) },
            |known, avail, _id, mctx, _style| measure_text(known, avail, mctx),
        ).expect("taffy compute_layout");

        // ---- 浮层第二遍布局 ----
        // 预遍历收集锚点矩形与浮层信息（坐标累加方式与 build/emit 完全一致）。
        struct OverlayInfo<'a> {
            node: &'a crate::scene::SceneNode,
            tnode: taffy::NodeId,
            x: f32, y: f32, w: f32, h: f32, // 主布局里的初始矩形（浮层的 size 有效，location 待覆盖）
        }
        fn scan<'a>(
            node: &'a crate::scene::SceneNode, tnode: taffy::NodeId, ctx: &BuildCtx,
            ox: f32, oy: f32,
            anchors: &mut std::collections::HashMap<String, (f32, f32, f32, f32)>,
            overlays: &mut Vec<OverlayInfo<'a>>,
        ) {
            let lay = ctx.taffy.layout(tnode).unwrap();
            let x = ox + lay.location.x;
            let y = oy + lay.location.y;
            let w = lay.size.width;
            let h = lay.size.height;
            if let Some(id) = node.props.get("gx-anchor-id") {
                anchors.insert(id.clone(), (x, y, w, h));
            }
            if is_overlay(node) {
                overlays.push(OverlayInfo { node, tnode, x, y, w, h });
            }
            let mut kids: Vec<&crate::scene::SceneNode> = Vec::new();
            flat_children(node, &mut kids);
            for (i, c) in kids.iter().enumerate() {
                let child_tnode = ctx.taffy.child_at_index(tnode, i).unwrap();
                scan(c, child_tnode, ctx, x, y, anchors, overlays);
            }
        }
        let mut anchors = std::collections::HashMap::new();
        let mut overlays: Vec<OverlayInfo> = Vec::new();
        scan(root, root_node, &ctx, 0.0, 0.0, &mut anchors, &mut overlays);

        // 计算每个浮层的目标绝对左上角，按 node.id 索引（emit 时套用）。
        let mut overrides: std::collections::HashMap<String, (f32, f32)> = std::collections::HashMap::new();
        for ov in &overlays {
            if is_scrim(ov.node) {
                overrides.insert(ov.node.id.clone(), (0.0, 0.0)); // 遮罩铺满，尺寸在 emit 时按画布覆盖
                continue;
            }
            if is_modal(ov.node) {
                // 视口居中（无锚点），与 Skia 端一致。
                overrides.insert(ov.node.id.clone(), place_centered(ov.w, ov.h, avail_w, avail_h));
                continue;
            }
            if is_portal(ov.node) {
                // 传送门根平移到画布原点（子树坐标随之相对画布左上角）。
                // portal 根自身透明，负坐标无视觉影响；与 Skia 端"子树平移到原点"对齐。
                overrides.insert(ov.node.id.clone(), (-ov.x, -ov.y));
                continue;
            }
            if !is_popover(ov.node) { continue; }
            let mut target = (ov.x, ov.y);
            if let Some(anchor_id) = ov.node.props.get("gx-anchor") {
                if let Some(&(ax, ay, aw, ah)) = anchors.get(anchor_id) {
                    let offset = ov.node.props.get("gx-offset")
                        .and_then(|s| parse_px(s.as_str())).unwrap_or(6.0);
                    let placement = ov.node.props.get("gx-placement")
                        .map(|s| s.as_str()).unwrap_or("bottom-start");
                    target = place_overlay(ax, ay, aw, ah, ov.w, ov.h, placement, offset, avail_w, avail_h);
                }
            }
            overrides.insert(ov.node.id.clone(), target);
        }

        // ---- emit：主树（跳过浮层）→ 遮罩 → 浮层，保证绘制层级 ----
        // 返回子树在"未施加本容器 scroll 偏移前"的绝对内容底边 Y，
        // 供 overflow 容器计算内容高度（内容高度 = 子树底边 - 容器顶）。
        fn emit(
            node: &crate::scene::SceneNode,
            tnode: taffy::NodeId,
            ctx: &BuildCtx,
            dl: &mut DisplayList,
            inherited_color: (u8, u8, u8),
            inherited_size: f32,
            ox: f32, oy: f32,
            overrides: &std::collections::HashMap<String, (f32, f32)>,
            avail_w: f32, avail_h: f32,
            skip_overlays: bool,
            parent_clip: ClipRect,
            inherited_gradient: Option<crate::text_effects::LinearGradient>,
            inherited_selection: Option<SelectionDraw>,
            inherited_family: Option<String>,
            tinh: TextInherit,
        ) -> f32 {
            // 主树遍历时跳过浮层子树（它们由后续的遮罩/浮层遍单独绘制，保证层级）。
            if skip_overlays && is_overlay(node) {
                return oy;
            }
            let p = |k: &str| node.props.get(k).map(|s| s.as_str());
            let color = p("color").map(parse_color).unwrap_or(inherited_color);
            let size = p("font-size").and_then(parse_px).unwrap_or(inherited_size);
            // 统一 TextEffects：当前节点声明 gradient-text / background-clip:text + bg 渐变时，
            // 解析出渐变并传给文本子节点；否则沿用祖先渐变。语义与 Go text/effects.Parse 一致。
            let my_gradient = match crate::text_effects::TextEffects::from_props(&node.props).fill {
                crate::text_effects::TextFill::Gradient(g) => Some(g),
                _ => inherited_gradient,
            };
            // selection 与渐变同理：父 span 声明的 selection-range 继承给文本子节点，
            // 子节点自身声明则覆盖（Editor 代码行 span 声明区间，文本子节点消费）。
            let my_selection = parse_selection(&node.props).or(inherited_selection);
            // font-family 与渐变同理：父 span 声明的字体族继承给文本子节点。
            let my_family = parse_font_family(&node.props).or(inherited_family);
            // 文本继承链（字重/对齐/行高/空白折叠），与 build 的测量口径一致。
            let my_tinh = TextInherit::derive(&node.props, size, tinh);
            let lay = ctx.taffy.layout(tnode).expect("taffy layout");
            // 浮层根用第二遍算出的目标坐标；其余节点沿用 Taffy 相对坐标累加。
            let (x, y) = overrides.get(&node.id).copied()
                .unwrap_or((ox + lay.location.x, oy + lay.location.y));
            let mut w = lay.size.width;
            let mut h = lay.size.height;
            if is_scrim(node) {
                w = avail_w; h = avail_h; // 遮罩覆盖整个画布
            }

            // 回流实测尺寸：按稳定 node.id 上报最终布局宽高。虚拟列表等组件
            // 按 item 根节点 ID 订阅，据此修正 offsets 与锚定（对齐 virtua）。
            if !node.id.is_empty() {
                dl.measured.push((node.id.clone(), w, h));
            }

            // overflow 容器：本节点矩形成为子树的新裁剪边界；子树坐标减 scroll 偏移。
            let st = crate::style::parse_style(node);
            let my_clip = if node.typ == NodeType::Div && st.overflow != crate::style::Overflow::Visible {
                clip_intersect(parent_clip, Some((x, y, w, h)))
            } else {
                parent_clip
            };
            let child_ox = x - st.scroll_left;
            let child_oy = y - st.scroll_top;

            if node.typ == NodeType::Text {
                // y 是 Taffy 算出的文本行盒顶部；glyphon 会用 Buffer 的 Metrics
                // (font_size, line_height) 自行处理行盒内基线，故这里不加
                // 任何 size 比例的魔法偏移——否则紧凑容器（如 Input 高 20px）里文字会偏下。
                let line_h = if my_tinh.line_h > 0.0 { my_tinh.line_h } else { size * 1.35 };
                // white-space:normal/nowrap 与测量端同口径折叠空白。
                let text = if my_tinh.collapse_ws {
                    crate::style::collapse_whitespace(&node.text)
                } else {
                    node.text.clone()
                };
                // text-align：盒宽大于内容宽时按 center/right 平移（近似测宽与
                // measure_text 同口径：ASCII 0.56em / 其它 1em）。
                let tw = text.chars()
                    .map(|ch| if ch.is_ascii() { 0.56 } else { 1.0 })
                    .sum::<f32>() * size;
                let dx = crate::style::align_offset(my_tinh.align, w, tw);
                if !clip_rejects(my_clip, x, y, size * 4.0, size * 1.35) {
                    // 统一渐变文字：按 ch 边界把文本切成着色分段，glyphon 逐 span 上色。
                    // 无渐变时 spans 为空 → 走 default_color 快路径，零额外分配。
                    let mut spans = my_gradient.as_ref()
                        .map(|g| crate::text_effects::gradient_spans(&text, g, color))
                        .unwrap_or_default();
                    // 三端统一 selection：用本节点或父 span 继承的 selection 协议，
                    // 选中区间前景覆盖 base 分段。
                    let selection = my_selection.clone();
                    if let Some(sel) = &selection {
                        if let Some(fg) = sel.color {
                            spans = crate::text_effects::selection_spans(
                                &text, &spans, color, sel.start, sel.end, fg);
                        }
                    }
                    dl.texts.push(TextDraw {
                        id: node.id.clone(), text, x: x + dx, y, size, color,
                        clip: my_clip, spans, selection, family: my_family.clone(),
                        alpha: st.opacity,
                        weight: my_tinh.weight, line_height: my_tinh.line_h,
                    });
                }
                return y + line_h;
            }

            if node.typ == NodeType::Div {
                let bg_raw = p("bg").or_else(|| p("background"));
                let bg = bg_raw.map(parse_color);
                // 通用渐变背景：带 gradient-text 标记的节点渐变只作用于文字，
                // 不画背景（与 Go paint.go 的例外一致）；其余节点的 bg 渐变画到容器。
                let is_gradient_text = node.props.get("gradient-text")
                    .map(|v| v.trim() == "1" || v.trim().eq_ignore_ascii_case("true"))
                    .unwrap_or(false)
                    || node.props.get("background-clip").or_else(|| node.props.get("-webkit-background-clip"))
                    .map(|v| v.to_ascii_lowercase().contains("text"))
                    .unwrap_or(false);
                let bg_gradient = if is_gradient_text {
                    None
                } else {
                    bg_raw.and_then(crate::text_effects::parse_linear_gradient)
                };
                let radius = p("border-radius").and_then(parse_px).unwrap_or(0.0);
                let border = p("border").map(|c| {
                    let bw = p("border-width").and_then(parse_px).unwrap_or(1.0);
                    (parse_color(c), bw)
                });
                if !clip_rejects(my_clip, x, y, w, h) {
                    // img 原语：<img src> 走原生解码（data URI / 本地路径），与 Go 端
                    // runtime/imgutil 的 src 语义一致。解码按 src 缓存（image.rs）。
                    let img = if node.kind.eq_ignore_ascii_case("img") {
                        p("src").and_then(|src| crate::image::load(src))
                            .map(|pix| (std::sync::Arc::new(pix), ObjectFit::parse(p("object-fit"))))
                    } else {
                        None
                    };
                    // canvas 原语：解析 Props["canvas-ops"]（runtime/canvas 协议）。
                    let canvas = if node.kind.eq_ignore_ascii_case("canvas") {
                        let ops = crate::canvas::decode(p("canvas-ops").unwrap_or(""));
                        if ops.is_empty() { None } else { Some(std::sync::Arc::new(ops)) }
                    } else {
                        None
                    };
                    // video 原语：ffmpeg 子进程解码动画（video.rs），当前帧在此取。
                    let is_video = node.kind.eq_ignore_ascii_case("video");
                    let video_frame = if is_video {
                        crate::video::frame(&node.id).map(std::sync::Arc::new)
                    } else {
                        None
                    };
                    dl.shapes.push(ShapeDraw {
                        x, y, w, h,
                        bg: bg.unwrap_or((0, 0, 0)),
                        // 有渐变时纯色底不绘制，由 LinearGradient shader 填充（对齐 Skia/canvas 端）。
                        bg_present: bg.is_some() && bg_gradient.is_none(),
                        bg_gradient,
                        radius, border,
                        shadow: st.shadow.clone(),
                        clip: my_clip,
                        img, canvas,
                        alpha: st.opacity,
                        video: video_frame,
                        video_id: if is_video { node.id.clone() } else { String::new() },
                    });
                }
                // pointer-events:none：节点自身不作为鼠标目标（仍绘制，后代照常
                // 命中——CSS 命中语义）；cursor 随命中框透出供宿主切换系统光标。
                let pe_none = node.props.get("pointer-events")
                    .map(|s| s.trim().eq_ignore_ascii_case("none"))
                    .unwrap_or(false);
                if let Some(ev) = node.events.iter().find(|e| e.starts_with("on")) {
                    if !pe_none {
                        dl.hitboxes.push((x, y, w, h, node.id.clone(), ev.clone(), is_overlay(node), my_clip,
                            node.props.get("cursor").cloned().unwrap_or_default()));
                    }
                }
                if node.props.contains_key("gx-input-id") && !pe_none {
                    dl.inputs.push((x, y, w, h, node.id.clone()));
                }
            }

            let mut kids: Vec<&crate::scene::SceneNode> = Vec::new();
            flat_children(node, &mut kids);
            let mut subtree_bottom = y + h; // 至少到自身底边
            // z-index：按值稳定排序绘制/命中顺序（负值垫底、正值置顶，同值保持
            // 文档序）。Taffy 子节点顺序不变，按原始下标取 child_tnode。
            let ordered: Vec<(usize, &crate::scene::SceneNode)> = {
                let mut v: Vec<(usize, &crate::scene::SceneNode)> =
                    kids.iter().enumerate().map(|(i, c)| (i, *c)).collect();
                v.sort_by_key(|&(_, c)| crate::style::parse_style(c).z_index);
                v
            };
            for &(orig_i, c) in ordered.iter() {
                let child_tnode = ctx.taffy.child_at_index(tnode, orig_i).expect("taffy child");
                let cb = emit(c, child_tnode, ctx, dl, color, size, child_ox, child_oy, overrides, avail_w, avail_h, skip_overlays, my_clip, my_gradient.clone(), my_selection.clone(), my_family.clone(), my_tinh);
                if cb > subtree_bottom {
                    subtree_bottom = cb;
                }
            }

            // overflow 容器：内容未滚动底边 = 子树底边 + scrollTop（还原子树被减去的偏移）。
            // 内容高度 = 未滚动底边 - 容器顶。溢出则生成滚动条几何（alpha 由 render 驱动）。
            if node.typ == NodeType::Div && st.overflow != crate::style::Overflow::Visible {
                let content_h = (subtree_bottom + st.scroll_top - y).max(h);
                if let Some(sb) = scrollbar_geometry(x, y, w, h, content_h, st.scroll_top, 0.0) {
                    dl.scrollbars.push(sb);
                }
            }

            // 返回给父的"未施加本节点 scroll 前"的内容底边：
            // 本节点子树坐标是 child_oy = y - scroll_top，加回 scroll_top 得到父坐标系下的位置。
            if node.typ == NodeType::Div && st.overflow != crate::style::Overflow::Visible {
                subtree_bottom + st.scroll_top
            } else {
                subtree_bottom
            }
        }

        // 第一遍：主树，跳过浮层子树。
        emit(root, root_node, &ctx, &mut dl, (226, 232, 240), 16.0, 0.0, 0.0,
            &overrides, avail_w, avail_h, true, None, None, None, None, TextInherit::default());
        // 第二遍：遮罩（拦截外部点击）。
        for ov in overlays.iter().filter(|o| is_scrim(o.node)) {
            emit(ov.node, ov.tnode, &ctx, &mut dl, (226, 232, 240), 16.0, 0.0, 0.0,
                &overrides, avail_w, avail_h, false, None, None, None, None, TextInherit::default());
        }
        // 第三遍：内容浮层 popover → modal → portal（最上层），按角色分层保证层级稳定。
        for role in ["popover", "modal", "portal"] {
            for ov in overlays.iter().filter(|o| gx_role(o.node) == role) {
                emit(ov.node, ov.tnode, &ctx, &mut dl, (226, 232, 240), 16.0, 0.0, 0.0,
                    &overrides, avail_w, avail_h, false, None, None, None, None, TextInherit::default());
            }
        }
        dl
    }

    /// 命中测试：浮层/遮罩优先于普通内容；同级内面积最小的包含点命中。
    /// 第 9 元素是 cursor 形状（宿主据此切换系统光标）。
    fn hit(&self, x: f32, y: f32) -> Option<&(f32, f32, f32, f32, String, String, bool, ClipRect, String)> {
        type Hit<'a> = &'a (f32, f32, f32, f32, String, String, bool, ClipRect, String);
        fn min_area<'a, I: Iterator<Item = Hit<'a>>>(items: I) -> Option<Hit<'a>> {
            items.min_by(|a, b| (a.2 * a.3).partial_cmp(&(b.2 * b.3)).unwrap_or(std::cmp::Ordering::Equal))
        }
        let contains = |h: &Hit<'_>| {
            if !(x >= h.0 && x <= h.0 + h.2 && y >= h.1 && y <= h.1 + h.3) { return false; }
            // 祖先 overflow 裁剪：点必须落在裁剪矩形内。
            match h.7 {
                None => true,
                Some((cx, cy, cw, ch)) => x >= cx && x <= cx + cw && y >= cy && y <= cy + ch,
            }
        };
        // 浮层优先（popover 内容不该被底层小按钮抢走命中）。
        min_area(self.hitboxes.iter().filter(|h| h.6 && contains(h)))
            .or_else(|| min_area(self.hitboxes.iter().filter(|h| !h.6 && contains(h))))
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
    /// 最近一次布局导出的实测尺寸，待渲染后经 eventCb 回传 Host（回流）。
    pending_measured: Vec<(String, f32, f32)>,
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
    glyph_cache: std::collections::HashMap<String, GlyphEntry>,
    /// 缓存的文本区域屏幕坐标（供命中/调试）
    cached_areas_dirty: bool,
    /// 最近光标位置（MouseInput 事件不带坐标）
    cursor: (f32, f32),
    /// 当前 hover/active/focused 命中的节点 ID（伪状态样式 &:hover/&:active/&:focus 用）。
    /// lib.rs 在 CursorMoved / MouseInput 事件里经 set_hover/set_active/set_focused 更新。
    hovered_id: Option<String>,
    active_id: Option<String>,
    focused_id: Option<String>,
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
        let text_renderer = TextRenderer::new(&mut atlas, &device, MultisampleState::default(), None);

        Renderer {
            device, queue, surface, config,
            bg_texture, bg_bind, blit_pipeline, bg_pixels, bg_bpr: bpr, width: w, height: h,
            font_system, swash_cache, viewport, atlas, text_renderer,
            scene: None,
            bg: (11, 16, 32),
            display: DisplayList::new(),
            pending_measured: vec![],
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
    pub fn take_measured(&mut self) -> Vec<(String, f32, f32)> {
        std::mem::take(&mut self.pending_measured)
    }

    /// 命中测试：返回坐标处的 node_id（供 hover/active 伪状态使用）。
    /// 与 hit_click 共用同一命中规则（浮层优先、最小面积、裁剪检查）。
    pub fn hit_node_id(&self, x: f32, y: f32) -> Option<String> {
        self.display.hit(x, y).map(|h| h.4.clone())
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

    /// 按 object-fit 把图片绘制进 (rx,ry,rw,rh)，radius>0 时用圆角 Mask 裁剪。
    fn draw_image_fit_impl(
        pm: &mut tiny_skia::Pixmap,
        pix: &tiny_skia::Pixmap,
        fit: ObjectFit,
        rx: f32, ry: f32, rw: f32, rh: f32, radius: f32,
    ) {
        let iw = pix.width() as f32;
        let ih = pix.height() as f32;
        if iw <= 0.0 || ih <= 0.0 || rw <= 0.0 || rh <= 0.0 {
            return;
        }
        // 计算目标区（object-fit）：fill 铺满；contain 完整放入；cover 铺满并裁剪。
        let (dx, dy, dw, dh) = if fit == ObjectFit::Fill {
            (rx, ry, rw, rh)
        } else {
            let ar = iw / ih;
            let bar = rw / rh;
            if fit == ObjectFit::Contain {
                if ar > bar {
                    let h = rw / ar;
                    (rx, ry + (rh - h) / 2.0, rw, h)
                } else {
                    let w = rh * ar;
                    (rx + (rw - w) / 2.0, ry, w, rh)
                }
            } else { // Cover
                if ar > bar {
                    let w = rh * ar;
                    (rx + (rw - w) / 2.0, ry, w, rh)
                } else {
                    let h = rw / ar;
                    (rx, ry + (rh - h) / 2.0, rw, h)
                }
            }
        };
        let dw_i = dw.max(1.0) as u32;
        let dh_i = dh.max(1.0) as u32;
        // 缩放绘制到与目标同尺寸的临时 Pixmap（draw_pixmap 用 Transform 缩放）。
        let mut tmp = match tiny_skia::Pixmap::new(dw_i, dh_i) {
            Some(v) => v,
            None => return,
        };
        let sx = dw / iw;
        let sy = dh / ih;
        let ts = tiny_skia::Transform::from_scale(sx, sy);
        tmp.draw_pixmap(
            0, 0, pix.as_ref(),
            &tiny_skia::PixmapPaint::default(),
            ts, None,
        );
        let paint = tiny_skia::PixmapPaint::default();
        if radius > 0.0 {
            // 圆角：生成同形状 Mask，按目标区合成。
            if let Some(path) = round_rect_path(0.0, 0.0, dw_i as f32, dh_i as f32, radius) {
                let mut mask = tiny_skia::Mask::new(dw_i, dh_i).unwrap();
                mask.fill_path(
                    &path,
                    tiny_skia::FillRule::Winding,
                    true,
                    tiny_skia::Transform::identity(),
                );
                pm.draw_pixmap(
                    dx as i32, dy as i32, tmp.as_ref(), &paint,
                    tiny_skia::Transform::identity(), Some(&mask),
                );
                return;
            }
        }
        pm.draw_pixmap(
            dx as i32, dy as i32, tmp.as_ref(), &paint,
            tiny_skia::Transform::identity(), None,
        );
    }

    /// 把苹果风格滚动条画进主 Pixmap：thumb 圆角、半透明，随 alpha 淡入淡出。
    /// track 本身不画底色（macOS overlay 滚动条平时不可见，仅滚动时浮现 thumb）。
    fn paint_scrollbars(&self, pm: &mut tiny_skia::Pixmap, bars: &[ScrollBar]) {
        use tiny_skia::{Color, FillRule, Paint, Transform};
        for b in bars {
            let alpha = self.scrollbar_alpha; // 淡入淡出由 render 阶段统一驱动
            if alpha <= 0.01 { continue; }
            let r = SB_TRACK_W * 0.5; // 圆头（胶囊形）
            if let Some(path) = round_rect_path(b.x, b.thumb_y, SB_TRACK_W, b.thumb_h, r) {
                let a = (alpha * 0.55 * 255.0) as u8; // 半透明（macOS thumb 约 0.4~0.6）
                let mut p = Paint::default();
                p.set_color(Color::from_rgba8(120, 120, 128, a)); // 中性灰
                p.anti_alias = true;
                pm.fill_path(&path, &p, FillRule::Winding, Transform::identity(), None);
            }
        }
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
            // 与祖先 overflow 裁剪矩形求交；完全在外则跳过。
            let (rx, ry, rw, rh) = match clip_rect_xywh(s.clip, s.x, s.y, s.w, s.h) {
                Some(v) => v,
                None => continue,
            };
            if rw <= 0.0 || rh <= 0.0 {
                continue;
            }
            // box-shadow：多层半透明圆角矩形近似（与 Skia 端同算法）。
            if let Some(sh) = &s.shadow {
                paint_shadow_tiny(&mut pm, s, sh);
            }
            if let Some(path) = round_rect_path(rx, ry, rw, rh, s.radius) {
                if s.bg_present {
                    let mut p = Paint::default();
                    // opacity 作用于元素整体：声明式 blink 动画采样后写进 st.opacity，
                    // 此处按 alpha 淡入淡出（对齐 Skia paintElement / Web CSS opacity）。
                    let a = (s.alpha.clamp(0.0, 1.0) * 255.0) as u8;
                    p.set_color(Color::from_rgba8(s.bg.0, s.bg.1, s.bg.2, a));
                    p.anti_alias = true;
                    pm.fill_path(&path, &p, FillRule::Winding, Transform::identity(), None);
                }
                // 通用 linear-gradient 背景：一次构造 shader + 一次 fill_path，
                // 与 Go 桌面端 paintGradientBackgroundShader 路径一致（无逐像素条带）。
                if let Some(g) = &s.bg_gradient {
                    if let Some(shader) = gradient_shader(g, (rx, ry, rw, rh)) {
                        let mut p = Paint::default();
                        p.shader = shader;
                        p.anti_alias = true;
                        pm.fill_path(&path, &p, FillRule::Winding, Transform::identity(), None);
                    }
                }
                // img：object-fit 缩放绘制，圆角时用同形状 Mask 裁剪，与 Skia 端一致。
                if let Some((pix, fit)) = &s.img {
                    Self::draw_image_fit_impl(&mut pm, pix, *fit, rx, ry, rw, rh, s.radius);
                }
                // video：ffmpeg 解码的当前帧，按 contain 缩放绘制 + 圆角裁剪。
                if let Some(frame) = &s.video {
                    if let Some(size) = tiny_skia::IntSize::from_wh(frame.w, frame.h) {
                    if let Some(pix) = tiny_skia::Pixmap::from_vec(
                        frame.data.clone(), size,
                    ) {
                        Self::draw_image_fit_impl(&mut pm, &pix, ObjectFit::Contain, rx, ry, rw, rh, s.radius);
                    }
                    }
                }
                // canvas 原语：命令式指令画到主 pm，坐标相对本节点裁剪后的盒。
                if let Some(ops) = &s.canvas {
                    crate::canvas::paint(&mut pm, ops, rx, ry);
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
        // 三端统一 selection：在文字层之前画选区背景矩形（glyphon 文字随后绘制）。
        // 区间是 rune 索引，用与 measure_text 一致的估算口径换算像素宽度。
        for t in &dl.texts {
            let Some(sel) = &t.selection else { continue; };
            let (sx0, sx1) = selection_px_range(&t.text, sel.start, sel.end, t.size);
            if sx1 <= sx0 { continue; }
            let (rx, ry, rw, rh) = match clip_rect_xywh(t.clip, t.x + sx0, t.y, sx1 - sx0, t.size * 1.35) {
                Some(v) => v, None => continue,
            };
            if rw <= 0.0 || rh <= 0.0 { continue; }
            let path = match round_rect_path(rx, ry, rw, rh, 0.0) { Some(v) => v, None => continue };
            let mut p = Paint::default();
            p.set_color(Color::from_rgba8(sel.bg.0, sel.bg.1, sel.bg.2, sel.bg.3));
            p.anti_alias = true;
            pm.fill_path(&path, &p, FillRule::Winding, Transform::identity(), None);
        }
        // 苹果风格滚动条画在内容之上（overlay），参与整体淡入淡出。
        if !dl.scrollbars.is_empty() {
            self.paint_scrollbars(&mut pm, &dl.scrollbars);
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
        if self.scene.as_ref().map(|s| crate::style::tree_has_animation(s)).unwrap_or(false) {
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
                    .as_millis() < 1700
            {
                return true;
            }
        }
        false
    }

    pub fn render(&mut self) {
        let Some(scene) = self.scene.as_ref() else { return; };
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

        self.viewport.update(&self.queue, Resolution { width: self.config.width, height: self.config.height });

        if need_rebuild {
            // —— 有变化 / 动画中：全量重算布局、背景光栅化、文本整形 ——
            let animated = self.animator.apply(scene);
            // 伪状态样式：按当前 hover/focus/active 命中把对应子表覆盖进
            // 临时树（不改常驻镜像树）。disabled 由节点自身属性判定、优先级最高。
            let styled = crate::style::apply_pseudo_state(
                &animated,
                self.hovered_id.as_deref(),
                self.active_id.as_deref(),
                self.focused_id.as_deref(),
            );
            let dl = DisplayList::from_scene(&styled, self.width as f32, self.height as f32);
            let bg = self.bg;

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
            self.paint_shapes(&dl, bg);

            // 字形增量复用（按稳定 node_id）：
            // glyphon 的 Buffer 只取决于 (文本, 字号)，与坐标/颜色无关。每帧按
            // dl.texts 的 node_id 查缓存：ID 与签名都在 → 直接复用；新 ID 或内容
            // 变了 → 只 shape 这一个；最后淘汰当前帧不再出现的 ID（滚出视口）。
            // 虚拟列表用 index 作 key、节点 ID 稳定，跨 item 边界时只有 1 个新 ID
            // 需要 shape，其余全部命中——比"整段签名一致才全复用"精确得多，且对
            // 任意内容变化帧都生效（不再限于纯滚动帧）。动画改的是坐标/颜色时
            // 签名不变可安全复用；若真改了字号/文本，签名变化会触发重新 shape。
            let (shaped_idx, evicted) = plan_glyphs(&dl.texts, &self.glyph_cache);
            let shaped = shaped_idx.len();
            for i in shaped_idx {
                let t = &dl.texts[i];
                let lh = if t.line_height > 0.0 { t.line_height } else { t.size * 1.35 };
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
                    let styled: Vec<(&str, Attrs)> = t.spans.iter().map(|(seg, c)| {
                        (seg.as_str(), attrs.clone().color(Color::rgb(c.0, c.1, c.2)))
                    }).collect();
                    b.set_rich_text(&mut self.font_system, styled, &attrs, Shaping::Advanced, None);
                }
                self.glyph_cache.insert(t.id.clone(), GlyphEntry { sig: text_sig(t), buffer: b });
            }
            for id in evicted {
                self.glyph_cache.remove(&id);
            }
            #[cfg(debug_assertions)]
            if shaped > 0 || shaped < dl.texts.len() {
                eprintln!("[rust-backend] glyph reuse: {} reused, {} shaped",
                    dl.texts.len().saturating_sub(shaped), shaped);
            }
            self.frame_dirty = false;
            self.cached_areas_dirty = false;
        }

        // 从当前 DisplayList + 按 ID 缓存的 buffers 组装 areas（有无变化都要做，因为 prepare 每帧消费 areas）。
        let areas: Vec<TextArea> = self.display.texts.iter().filter_map(|t| {
            let b = match self.glyph_cache.get(&t.id) {
                Some(e) => &e.buffer,
                None => return None, // 理论上不会发生；防御性跳过缺字形的项
            };
            let bounds = match t.clip {
                Some((cx, cy, cw, ch)) => TextBounds {
                    left: (cx - t.x - 20.0) as i32, top: (cy - t.y - 20.0) as i32,
                    right: (cx + cw - t.x) as i32, bottom: (cy + ch - t.y) as i32,
                },
                None => TextBounds { left: -20, top: -20, right: 720, bottom: self.height as i32 },
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
        self.display.hit(x, y).map(|(_, _, _, _, id, ev, _, ..)| InputEvent {
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
        // 简化：仅更新 surface 配置（种子 demo 固定尺寸）。
        self.config.width = w.max(1);
        self.config.height = h.max(1);
        self.surface.configure(&self.device, &self.config);
        // 尺寸相关缓存失效：下一帧必须重算布局与文本。
        self.frame_dirty = true;
        self.cached_areas_dirty = true;
        self.glyph_cache.clear(); // 字号不变，但清空避免与旧尺寸残留帧误命中
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

#[cfg(test)]
mod scrollbar_tests {
    use super::*;

    #[test]
    fn no_scrollbar_when_content_fits() {
        // 内容高度 <= 容器高度：不显示滚动条。
        assert!(scrollbar_geometry(0.0, 0.0, 100.0, 200.0, 180.0, 0.0, 1.0).is_none());
        assert!(scrollbar_geometry(0.0, 0.0, 100.0, 200.0, 200.0, 0.0, 1.0).is_none());
    }

    #[test]
    fn scrollbar_thumb_geometry() {
        // 容器 100×200，内容高 600 → maxScroll=400。
        // thumb_h = 200*200/600 = 66.67（> 最小 32）。
        // scrollTop=0 → thumb 在顶；scrollTop=400 → thumb 在底。
        let top = scrollbar_geometry(0.0, 0.0, 100.0, 200.0, 600.0, 0.0, 1.0).unwrap();
        assert!((top.thumb_h - 66.67).abs() < 1.0, "thumb_h={}", top.thumb_h);
        assert!((top.thumb_y - 0.0).abs() < 0.01, "thumb_y at top={}", top.thumb_y);
        assert!((top.y - 0.0).abs() < 0.01);
        assert!((top.x - (100.0 - SB_TRACK_W - 2.0)).abs() < 0.01, "track 贴右内侧");

        let bottom = scrollbar_geometry(0.0, 0.0, 100.0, 200.0, 600.0, 400.0, 1.0).unwrap();
        let expected_y = 200.0 - top.thumb_h; // travel = ch - thumb_h
        assert!((bottom.thumb_y - expected_y).abs() < 0.01, "thumb_y at bottom={}", bottom.thumb_y);

        // 中途 scrollTop=200 → thumb 走一半行程。
        let mid = scrollbar_geometry(0.0, 0.0, 100.0, 200.0, 600.0, 200.0, 1.0).unwrap();
        assert!((mid.thumb_y - expected_y / 2.0).abs() < 0.5, "thumb_y at mid={}", mid.thumb_y);
    }

    #[test]
    fn thumb_min_height_clamped() {
        // 内容极大 → thumb 高被最小高度 32 兜底，且不超容器高。
        let sb = scrollbar_geometry(0.0, 0.0, 100.0, 200.0, 100000.0, 0.0, 1.0).unwrap();
        assert!((sb.thumb_h - SB_THUMB_MIN).abs() < 0.01, "thumb_h={}", sb.thumb_h);
    }

    #[test]
    fn scroll_top_clamped_to_range() {
        // scrollTop 超出 [0,maxScroll] 时 thumb 不越界。
        let over = scrollbar_geometry(0.0, 50.0, 100.0, 200.0, 600.0, 9999.0, 1.0).unwrap();
        assert!(over.thumb_y <= 50.0 + 200.0 - over.thumb_h + 0.01);
        let under = scrollbar_geometry(0.0, 50.0, 100.0, 200.0, 600.0, -100.0, 1.0).unwrap();
        assert!((under.thumb_y - 50.0).abs() < 0.01);
    }

    #[test]
    fn alpha_clamped() {
        let a = scrollbar_geometry(0.0, 0.0, 100.0, 200.0, 600.0, 0.0, 2.0).unwrap();
        assert!((a.alpha - 1.0).abs() < 0.01);
        let b = scrollbar_geometry(0.0, 0.0, 100.0, 200.0, 600.0, 0.0, -1.0).unwrap();
        assert!((b.alpha - 0.0).abs() < 0.01);
    }

    /// 构造一个 overflow=scroll 容器（100×100）内含总高 500 的子树，跑 from_scene。
    fn overflow_scene(scroll_top: f32) -> crate::scene::SceneNode {
        use crate::binprotocol::NodeType;
        let mut kids = Vec::new();
        for i in 0..5 {
            kids.push(crate::scene::SceneNode {
                typ: NodeType::Div, kind: "div".into(), id: format!("item{}", i),
                text: String::new(),
                props: [("height".to_string(), "100".to_string())].into_iter().collect(),
                events: vec![], children: vec![],
            });
        }
        let mut props: std::collections::HashMap<String, String> = [
            ("width".to_string(), "100".to_string()),
            ("height".to_string(), "100".to_string()),
            ("overflow".to_string(), "scroll".to_string()),
        ].into_iter().collect();
        props.insert("scroll-top".to_string(), format!("{}", scroll_top));
        crate::scene::SceneNode {
            typ: NodeType::Div, kind: "div".into(), id: "vp".into(), text: String::new(),
            props, events: vec![], children: kids,
        }
    }

    #[test]
    fn from_scene_collects_measured() {
        let dl = DisplayList::from_scene(&overflow_scene(0.0), 800.0, 600.0);
        // 回流应覆盖容器 + 5 个 item（非空 id 的节点）。
        assert_eq!(dl.measured.len(), 6, "应为 vp + 5 items 收集实测尺寸: {:?}", dl.measured);
        let item0 = dl.measured.iter().find(|(id, _, _)| id == "item0").expect("item0 应在回流里");
        assert!((item0.2 - 100.0).abs() < 0.5, "item0 实测高度应约 100，实际 {}", item0.2);
    }

    #[test]
    fn from_scene_emits_scrollbar_for_overflow() {
        let dl = DisplayList::from_scene(&overflow_scene(0.0), 800.0, 600.0);
        assert_eq!(dl.scrollbars.len(), 1, "溢出容器应生成一个滚动条");
        let sb = &dl.scrollbars[0];
        // 容器 100 高，内容 500 高 → thumb_h = 100*100/500 = 20 → 被最小高度 32 兜底。
        assert!((sb.thumb_h - SB_THUMB_MIN).abs() < 0.01, "thumb_h={}", sb.thumb_h);
    }

    #[test]
    fn from_scene_no_scrollbar_when_no_overflow() {
        // 容器高 100，只放 1 个高 100 的子项 → 不溢出，无滚动条。
        use crate::binprotocol::NodeType;
        let vp = crate::scene::SceneNode {
            typ: NodeType::Div, kind: "div".into(), id: "vp".into(), text: String::new(),
            props: [
                ("width".to_string(), "100".to_string()),
                ("height".to_string(), "100".to_string()),
                ("overflow".to_string(), "auto".to_string()),
            ].into_iter().collect(),
            events: vec![],
            children: vec![crate::scene::SceneNode {
                typ: NodeType::Div, kind: "div".into(), id: "one".into(), text: String::new(),
                props: [("height".to_string(), "100".to_string())].into_iter().collect(),
                events: vec![], children: vec![],
            }],
        };
        let dl = DisplayList::from_scene(&vp, 800.0, 600.0);
        assert!(dl.scrollbars.is_empty(), "未溢出不应有滚动条");
    }

    #[test]
    fn scrollbar_moves_with_scroll() {
        let top = DisplayList::from_scene(&overflow_scene(0.0), 800.0, 600.0);
        let mid = DisplayList::from_scene(&overflow_scene(200.0), 800.0, 600.0);
        assert_eq!(top.scrollbars.len(), 1);
        assert_eq!(mid.scrollbars.len(), 1);
        assert!(mid.scrollbars[0].thumb_y > top.scrollbars[0].thumb_y,
            "滚动后 thumb 应下移: {} -> {}", top.scrollbars[0].thumb_y, mid.scrollbars[0].thumb_y);
    }
}

#[cfg(test)]
mod text_sig_tests {
    use super::*;

    fn td(id: &str, text: &str, size: f32) -> TextDraw {
        TextDraw {
            id: id.into(), text: text.into(), x: 0.0, y: 0.0, size,
            color: (0, 0, 0), clip: None, spans: vec![], selection: None, family: None, weight: None, line_height: 0.0, alpha: 1.0,
        }
    }

    /// 用一组 (id,text,size) 构造"上一帧缓存"：只需签名，buffer 用空占位。
    fn cache_of(items: &[(&str, &str, f32)]) -> std::collections::HashMap<String, GlyphEntry> {
        let mut m = std::collections::HashMap::new();
        for (id, text, size) in items {
            let t = td(id, text, *size);
            // buffer 对 plan_glyphs 的判定无影响，这里用 new 占位即可。
            let mut fs = FontSystem::new();
            m.insert((*id).into(), GlyphEntry {
                sig: text_sig(&t),
                buffer: Buffer::new(&mut fs, Metrics::new(*size, *size * 1.35)),
            });
        }
        m
    }

    #[test]
    fn sig_depends_only_on_text_and_size() {
        // 签名只看文本+字号，与 id/坐标/颜色无关——这是按 ID 复用的正确性基础。
        let a = td("a", "A", 16.0);
        let moved = TextDraw { id: "b".into(), x: 99.0, y: -6.0, color: (255, 0, 0), ..a.clone() };
        assert_eq!(text_sig(&a), text_sig(&moved));
        assert_ne!(text_sig(&a), text_sig(&td("a", "B", 16.0)));   // 内容变
        assert_ne!(text_sig(&a), text_sig(&td("a", "A", 20.0)));   // 字号变
    }

    #[test]
    fn unchanged_texts_need_no_shape() {
        // 小幅滚动：ID 和文本都没变 → 全部复用，零 shape。
        let cache = cache_of(&[("a", "A", 16.0), ("b", "B", 16.0)]);
        let cur = [td("a", "A", 16.0), td("b", "B", 16.0)];
        let (shaped, evicted) = plan_glyphs(&cur, &cache);
        assert!(shaped.is_empty(), "未变文本应零 shape");
        assert!(evicted.is_empty());
    }

    #[test]
    fn scroll_boundary_shapes_only_new_item() {
        // 虚拟列表跨 item 边界：滚出 "a"、滚入 "c"，"b" 未变 →
        // 只有 "c" 需要 shape，"a" 被淘汰，"b" 复用。
        let cache = cache_of(&[("a", "A", 16.0), ("b", "B", 16.0)]);
        let cur = [td("b", "B", 16.0), td("c", "C", 16.0)];
        let (shaped, evicted) = plan_glyphs(&cur, &cache);
        assert_eq!(shaped, vec![1], "只有滚入的 c 需要 shape");
        assert_eq!(evicted, vec!["a".to_string()], "滚出的 a 应被淘汰");
    }

    #[test]
    fn content_change_shapes_only_changed_item() {
        // 某一项文本变化：只有该项重新 shape，其余复用。
        let cache = cache_of(&[("a", "A", 16.0), ("b", "B", 16.0)]);
        let cur = [td("a", "A", 16.0), td("b", "B2", 16.0)];
        let (shaped, evicted) = plan_glyphs(&cur, &cache);
        assert_eq!(shaped, vec![1], "内容变的 b 需重新 shape");
        assert!(evicted.is_empty());
    }

    #[test]
    fn reorder_keeps_identity() {
        // 顺序变化但 ID+内容不变（如 For 重排）：全部复用，零 shape——
        // 这正是整段签名方案会误判为全量重建的场景。
        let cache = cache_of(&[("a", "A", 16.0), ("b", "B", 16.0)]);
        let cur = [td("b", "B", 16.0), td("a", "A", 16.0)];
        let (shaped, evicted) = plan_glyphs(&cur, &cache);
        assert!(shaped.is_empty(), "重排不应触发 shape");
        assert!(evicted.is_empty());
    }

    #[test]
    fn cold_cache_shapes_all() {
        let cache = cache_of(&[]);
        let cur = [td("a", "A", 16.0), td("b", "B", 16.0)];
        let (shaped, evicted) = plan_glyphs(&cur, &cache);
        assert_eq!(shaped, vec![0, 1]);
        assert!(evicted.is_empty());
    }

    #[test]
    fn gradient_spans_change_signature_for_reshaping() {
        // 同一文本/字号，仅渐变分段颜色不同 → 签名必须不同，否则 span 不会更新。
        let mut a = td("code", "func", 13.0);
        a.spans = vec![("fun".into(), (197, 134, 192)), ("c".into(), (212, 212, 212))];
        let mut b = td("code", "func", 13.0);
        b.spans = vec![("fun".into(), (255, 0, 0)), ("c".into(), (0, 0, 255))];
        assert_ne!(text_sig(&a), text_sig(&b), "渐变颜色变化应触发重新 shape");

        // 坐标移动不影响签名（滚动零 shape）。
        let moved = TextDraw { x: 99.0, y: -6.0, ..a.clone() };
        assert_eq!(text_sig(&a), text_sig(&moved));
    }

    #[test]
    fn gradient_shader_builds_for_linear_gradient() {
        let g = crate::text_effects::parse_linear_gradient(
            "linear-gradient(90deg, #ff0000 0%, #0000ff 100%)",
        ).unwrap();
        // 90°=水平渐变；盒子 200x100。shader 应成功构造（非退化、色标 ≥2）。
        assert!(gradient_shader(&g, (10.0, 10.0, 200.0, 100.0)).is_some());
        // 退化盒子不构造 shader。
        assert!(gradient_shader(&g, (0.0, 0.0, 0.0, 100.0)).is_none());
    }

    #[test]
    fn from_scene_inherits_gradient_from_parent_to_text() {
        // 模拟编辑器代码行：<span gradient-text="1" bg=linear-gradient(...)>文本</span>。
        // 验证父 span 解析出的渐变能传到文本子节点并生成着色分段（三端统一的核心链路）。
        use crate::binprotocol::NodeType;
        let text = crate::scene::SceneNode {
            typ: NodeType::Text, kind: String::new(), id: "code-text".into(),
            text: "ab".into(), props: Default::default(), events: vec![], children: vec![],
        };
        let mut props = std::collections::HashMap::new();
        props.insert("gradient-text".into(), "1".into());
        props.insert("bg".into(),
            "linear-gradient(90deg, #ff0000 0ch 1ch, #0000ff 1ch 2ch)".into());
        props.insert("white-space".into(), "pre".into());
        let span = crate::scene::SceneNode {
            typ: NodeType::Div, kind: "span".into(), id: "code-line".into(),
            text: String::new(), props, events: vec![], children: vec![text],
        };
        let dl = DisplayList::from_scene(&span, 800.0, 600.0);
        let td = dl.texts.iter().find(|t| t.id == "code-text")
            .expect("文本节点应进入 DisplayList");
        assert_eq!(td.spans.len(), 2, "应按 ch 边界切成两段: {:?}", td.spans);
        assert_eq!(td.spans[0], ("a".to_string(), (255, 0, 0)));
        assert_eq!(td.spans[1], ("b".to_string(), (0, 0, 255)));
    }
}

#[cfg(test)]
mod overlay_tests {
    use super::*;

    #[test]
    fn place_bottom_start() {
        let (x, y) = place_overlay(100.0, 200.0, 80.0, 30.0, 120.0, 60.0, "bottom-start", 8.0, 800.0, 600.0);
        assert_eq!(x, 100.0);
        assert_eq!(y, 238.0); // 200+30+8
    }

    #[test]
    fn place_bottom_center() {
        let (x, _y) = place_overlay(100.0, 200.0, 100.0, 30.0, 120.0, 60.0, "bottom-center", 8.0, 800.0, 600.0);
        assert_eq!(x, 90.0); // 100 + (100-120)/2
    }

    #[test]
    fn place_flip_to_top() {
        // 锚点靠近底部，下方放不下 → 翻到 top。
        let (x, y) = place_overlay(100.0, 520.0, 80.0, 30.0, 120.0, 60.0, "bottom-start", 8.0, 800.0, 600.0);
        assert_eq!(x, 100.0);
        assert_eq!(y, 452.0); // 520 - 8 - 60
    }

    #[test]
    fn place_end_align() {
        let (x, _) = place_overlay(100.0, 200.0, 100.0, 30.0, 60.0, 60.0, "bottom-end", 8.0, 800.0, 600.0);
        assert_eq!(x, 140.0); // 100+100-60
    }

    #[test]
    fn place_centered_modal() {
        let (x, y) = place_centered(200.0, 100.0, 800.0, 600.0);
        assert_eq!(x, 300.0);
        assert_eq!(y, 250.0);
        // 超画布时 clamp 后贴左上。
        let (x, y) = place_centered(900.0, 500.0, 800.0, 600.0);
        assert_eq!(x, 0.0);
        assert_eq!(y, 50.0);
    }
}

#[cfg(test)]
mod clip_tests {
    use super::*;

    #[test]
    fn intersect_clips() {
        let a = Some((0.0, 0.0, 100.0, 50.0));
        let b = Some((20.0, 10.0, 60.0, 60.0));
        assert_eq!(clip_intersect(a, b), Some((20.0, 10.0, 60.0, 40.0)));
    }

    #[test]
    fn intersect_with_none() {
        let a = Some((0.0, 0.0, 100.0, 50.0));
        assert_eq!(clip_intersect(a, None), a);
        assert_eq!(clip_intersect(None, a), a);
    }

    #[test]
    fn reject_and_rect() {
        let c = Some((0.0, 0.0, 100.0, 50.0));
        assert!(clip_rejects(c, 0.0, -40.0, 100.0, 30.0)); // 滚出上方
        assert!(!clip_rejects(c, 0.0, 40.0, 100.0, 30.0)); // 部分在内
        assert_eq!(clip_rect_xywh(c, 0.0, 40.0, 100.0, 30.0), Some((0.0, 40.0, 100.0, 10.0)));
        assert_eq!(clip_rect_xywh(c, 0.0, -40.0, 100.0, 30.0), None);
    }

}

#[cfg(test)]
mod selection_tests {
    use super::*;

    fn sel_props() -> std::collections::HashMap<String, String> {
        let mut p = std::collections::HashMap::new();
        p.insert("selection-range".into(), "2:5".into());
        p.insert("&::selection".into(), "{\"bg\":\"#264f7880\",\"color\":\"#ffffff\"}".into());
        p
    }

    #[test]
    fn parse_selection_reads_range_and_colors() {
        let sel = parse_selection(&sel_props()).expect("应解析出选区");
        assert_eq!((sel.start, sel.end), (2, 5));
        assert_eq!(sel.bg.3, 0x80, "半透明 alpha 应保留");
        assert_eq!(sel.color, Some((255, 255, 255)));
    }

    #[test]
    fn parse_selection_rejects_missing_or_bad() {
        let empty = std::collections::HashMap::new();
        assert!(parse_selection(&empty).is_none());
        let mut bad = sel_props();
        bad.insert("selection-range".into(), "5:2".into());
        assert!(parse_selection(&bad).is_none(), "start>=end 应视为无选区");
    }

    #[test]
    fn selection_px_range_matches_measure() {
        let ascii = selection_px_range("abcdef", 2, 5, 10.0);
        assert!((ascii.0 - 11.2).abs() < 0.01, "2*0.56*10=11.2, got {:?}", ascii);
        assert!((ascii.1 - 28.0).abs() < 0.01, "5*0.56*10=28, got {:?}", ascii);
        let cjk = selection_px_range("你ab好", 1, 3, 10.0);
        assert!((cjk.0 - 10.0).abs() < 0.01, "CJK 首字 1em, got {:?}", cjk);
    }

    #[test]
    fn selection_in_text_sig_forces_reshaping() {
        let base = TextDraw {
            id: "x".into(), text: "abcd".into(), x: 0.0, y: 0.0, size: 13.0,
            color: (0, 0, 0), clip: None, spans: vec![], selection: None, family: None, weight: None, line_height: 0.0, alpha: 1.0,
        };
        let mut a = base.clone();
        let mut b = base;
        a.selection = Some(SelectionDraw {
            start: 1, end: 3, bg: (38, 79, 120, 128), color: Some((255, 255, 255)),
        });
        b.selection = Some(SelectionDraw {
            start: 2, end: 4, bg: (38, 79, 120, 128), color: Some((255, 255, 255)),
        });
        assert_ne!(text_sig(&a), text_sig(&b), "选区区间变化应触发重排");
    }

    fn font_props(stack: &str) -> std::collections::HashMap<String, String> {
        let mut p = std::collections::HashMap::new();
        p.insert("font-family".into(), stack.into());
        p
    }

    #[test]
    fn font_family_maps_monospace_stack() {
        // Editor 等宽栈应映射到已加载的 DejaVu Sans Mono。
        let f = parse_font_family(&font_props(
            "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace"));
        assert_eq!(f.as_deref(), Some("DejaVu Sans Mono"));
    }

    #[test]
    fn font_family_sans_stack_uses_default() {
        let f = parse_font_family(&font_props("system-ui, -apple-system, sans-serif"));
        assert!(f.is_none(), "纯 sans 栈应沿用默认 SansSerif");
        assert!(parse_font_family(&font_props("")).is_none());
    }

    #[test]
    fn font_family_concrete_name_passthrough() {
        let f = parse_font_family(&font_props("Menlo, monospace"));
        assert_eq!(f.as_deref(), Some("DejaVu Sans Mono"), "Menlo 属等宽候选");
        let f2 = parse_font_family(&font_props("Fira Code, monospace"));
        assert_eq!(f2.as_deref(), Some("Fira Code"), "未在等宽名单的具体族名原样使用");
    }

    #[test]
    fn font_family_in_text_sig_forces_reshaping() {
        let base = TextDraw {
            id: "x".into(), text: "abcd".into(), x: 0.0, y: 0.0, size: 13.0,
            color: (0, 0, 0), clip: None, spans: vec![], selection: None, family: None, weight: None, line_height: 0.0, alpha: 1.0,
        };
        let mut mono = base.clone();
        mono.family = Some("DejaVu Sans Mono".into());
        assert_ne!(text_sig(&base), text_sig(&mono), "字体族变化应触发重新 shape");
    }
}

#[cfg(test)]
mod parity_tests {
    use super::*;
    use crate::binprotocol::NodeType;
    use crate::scene::SceneNode;

    fn div(id: &str, props: &[(&str, &str)], children: Vec<SceneNode>) -> SceneNode {
        SceneNode {
            typ: NodeType::Div, kind: "div".into(), id: id.into(), text: String::new(),
            props: props.iter().map(|(k, v)| (k.to_string(), v.to_string())).collect(),
            events: vec![], children,
        }
    }

    fn text(id: &str, text: &str, props: &[(&str, &str)]) -> SceneNode {
        SceneNode {
            typ: NodeType::Text, kind: "#text".into(), id: id.into(), text: text.into(),
            props: props.iter().map(|(k, v)| (k.to_string(), v.to_string())).collect(),
            events: vec![], children: vec![],
        }
    }

    // ---- Taffy 布局映射 ----

    #[test]
    fn hitboxes_expose_cursor_and_skip_pointer_events() {
        let mut btn = div("btn", &[("width", "50"), ("height", "20"), ("cursor", "pointer")], vec![]);
        btn.events = vec!["on-click".into()];
        let mut pe = div("pe", &[("width", "50"), ("height", "20"), ("pointer-events", "none")], vec![]);
        pe.events = vec!["on-click".into()];
        let root = div("root", &[("width", "200"), ("height", "60"), ("flex-direction", "row")], vec![btn, pe]);
        let dl = DisplayList::from_scene(&root, 400.0, 200.0);
        assert_eq!(dl.hitboxes.len(), 1, "pointer-events:none 不登记命中框");
        let hb = &dl.hitboxes[0];
        assert_eq!(hb.4, "btn");
        assert_eq!(hb.8, "pointer", "cursor 随命中框透出");
    }

    #[test]
    fn text_align_and_whitespace_and_weight_in_text_draws() {
        let root = div("root", &[("width", "100"), ("text-align", "center"),
                                ("white-space", "normal"), ("font-weight", "700")],
            vec![text("t", "a   b", &[])]);
        let dl = DisplayList::from_scene(&root, 200.0, 100.0);
        assert_eq!(dl.texts.len(), 1);
        let t = &dl.texts[0];
        assert_eq!(t.text, "a b", "white-space:normal 折叠空白");
        assert_eq!(t.weight, Some(700), "font-weight 进入 TextDraw");
        // 近似宽 = (0.56*2 + 1*1空格?) "a b" = a(0.56)+空格(0.56)+b(0.56) = 1.68em*16=26.88
        let tw = t.text.chars().map(|c| if c.is_ascii() { 0.56 } else { 1.0 }).sum::<f32>() * t.size;
        let want_dx = (100.0 - tw) / 2.0;
        assert!((t.x - want_dx).abs() < 0.5, "center 落笔 x = {}, want ≈{}", t.x, want_dx);
    }

    #[test]
    fn line_height_overrides_text_draw() {
        let root = div("root", &[("width", "100"), ("line-height", "2")],
            vec![text("t", "hi", &[])]);
        let dl = DisplayList::from_scene(&root, 200.0, 100.0);
        let t = &dl.texts[0];
        assert!((t.line_height - 32.0).abs() < 0.1, "line-height:2 × 16px = 32, got {}", t.line_height);
    }

    #[test]
    fn line_height_inherits_from_parent() {
        // text 子节点自身无 line-height，应继承父容器的 1.5。
        let root = div("root", &[("width", "100"), ("line-height", "1.5")],
            vec![div("wrap", &[], vec![text("t", "hi", &[])])]);
        let dl = DisplayList::from_scene(&root, 200.0, 100.0);
        let t = &dl.texts[0];
        assert!((t.line_height - 24.0).abs() < 0.1, "继承 1.5×16=24, got {}", t.line_height);
    }

    #[test]
    fn z_index_reorders_paint_order() {
        let mk = |id: &str, bg: &str, z: Option<&str>| {
            let mut props = vec![("width", "30"), ("height", "10"), ("bg", bg)];
            let mut p: Vec<(&str, &str)> = props.drain(..).collect();
            if let Some(zv) = z { p.push(("z-index", zv)); }
            div(id, &p, vec![])
        };
        let root = div("root", &[("width", "60"), ("height", "60")], vec![
            mk("a", "#010101", Some("5")),
            mk("b", "#020202", None),
            mk("c", "#030303", Some("-1")),
        ]);
        let dl = DisplayList::from_scene(&root, 100.0, 100.0);
        // 绘制顺序：c(-1) → b(0) → a(5)，同值保持文档序。
        let bgs: Vec<(u8, u8, u8)> = dl.shapes.iter().filter(|s| s.bg_present).map(|s| s.bg).collect();
        assert_eq!(bgs, vec![(0x03, 0x03, 0x03), (0x02, 0x02, 0x02), (0x01, 0x01, 0x01)],
            "z-index 应重排绘制顺序: {:?}", bgs);
    }

    #[test]
    fn text_sig_includes_weight() {
        let mut t = TextDraw {
            id: "t".into(), text: "hi".into(), x: 0.0, y: 0.0, size: 16.0,
            color: (0, 0, 0), clip: None, spans: vec![], selection: None, family: None,
            weight: None, line_height: 0.0, alpha: 1.0,
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
        let mut children = vec![];
        if let Some(arr) = v["children"].as_array() {
            for c in arr {
                children.push(tw_build(c, id_counter));
            }
        }
        if kind == "text" {
            SceneNode {
                typ: NodeType::Text, kind: "#text".into(), id, text: v["text"].as_str().unwrap_or("").into(),
                props, events: vec![], children,
            }
        } else {
            // Skia 端对全部 div 登记命中框；镜像对齐：每个 div 视作可命中。
            SceneNode {
                typ: NodeType::Div, kind: "div".into(), id, text: String::new(),
                props, events: vec!["on-click".into()], children,
            }
        }
    }

    fn tw_hex(c: (u8, u8, u8)) -> String {
        format!("#{:02x}{:02x}{:02x}", c.0, c.1, c.2)
    }

    #[derive(Clone)]
    struct TwRect { bg: String, x: f32, y: f32, w: f32, h: f32 }
    #[derive(Clone)]
    struct TwText { text: String, x: f32, y: f32 }

    fn tw_observe(dl: &DisplayList, bg_by_id: &std::collections::HashMap<String, String>) -> (Vec<TwRect>, Vec<TwText>) {
        let mut rects = vec![];
        for s in &dl.shapes {
            if s.bg_present && s.bg_gradient.is_none() {
                rects.push(TwRect { bg: tw_hex(s.bg), x: s.x, y: s.y, w: s.w, h: s.h });
            }
        }
        let mut texts = vec![];
        for t in &dl.texts {
            texts.push(TwText { text: t.text.clone(), x: t.x, y: t.y });
        }
        // 命中观测对齐 Skia HitTest：逆序（自顶向下）第一个包含点的命中框。
        let _ = bg_by_id;
        (rects, texts)
    }

    fn tw_hit_at(dl: &DisplayList, bg_by_id: &std::collections::HashMap<String, String>, px: f32, py: f32) -> (String, String) {
        for hb in dl.hitboxes.iter().rev() {
            let (x, y, w, h, id, _, _, clip, cursor) = hb;
            let contains = px >= *x && px < x + *w && py >= *y && py < y + *h;
            if contains && !clip_rejects(*clip, px, py, 0.0, 0.0) {
                return (bg_by_id.get(id).cloned().unwrap_or_default(), cursor.clone());
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
        let raw = std::fs::read_to_string(&path).unwrap_or_else(|e| panic!("读取用例 {}: {}", path, e));
        let suite: serde_json::Value = serde_json::from_str(&raw).unwrap();
        let (vw, vh) = {
            let vp = suite["viewport"].as_array().unwrap();
            (vp[0].as_f64().unwrap() as f32, vp[1].as_f64().unwrap() as f32)
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
                if let Some(bg) = n.props.get("bg") { m.insert(n.id.clone(), bg.clone()); }
                for c in &n.children { collect(c, m); }
            }
            collect(&root, &mut bg_by_id);
            let dl = DisplayList::from_scene(&root, vw, vh);
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
                            None => case_fails.push(format!("[rust] {}: rect {} 未绘制（属性未消费或颜色未透传）", name, bg)),
                            Some(r) => {
                                for (got, want, label) in [
                                    (r.x, e["x"].as_f64().unwrap_or(0.0), "x"),
                                    (r.y, e["y"].as_f64().unwrap_or(0.0), "y"),
                                    (r.w, e["w"].as_f64().unwrap_or(0.0), "w"),
                                    (r.h, e["h"].as_f64().unwrap_or(0.0), "h"),
                                ] {
                                    let tol = e["tol"].as_f64().unwrap_or(1.0);
                                    if !near(got, want, tol) {
                                        case_fails.push(format!("[rust] {}: rect {} {}={:.1} want {}", name, bg, label, got, want));
                                    }
                                }
                            }
                        }
                    }
                    "text" | "text-dx" | "text-dy" => {
                        let txt = e["text"].as_str().unwrap_or("");
                        match first_text(txt) {
                            None => case_fails.push(format!("[rust] {}: text {:?} 未绘制（文本属性未消费或内容被错误改写）", name, txt)),
                            Some(t) => match kind {
                                "text-dx" => {
                                    let want = e["x"].as_f64().unwrap_or(0.0);
                                    let tol = e["tol"].as_f64().unwrap_or(1.0);
                                    if !near(t.x, want, tol) {
                                        case_fails.push(format!("[rust] {}: text {:?} x={:.1} want {}(±{})", name, txt, t.x, want, tol));
                                    }
                                }
                                "text-dy" => {
                                    let anc = e["anchor"].as_str().unwrap_or("");
                                    match first_text(anc) {
                                        None => case_fails.push(format!("[rust] {}: anchor text {:?} 未绘制", name, anc)),
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
                            },
                        }
                    }
                    "order" => {
                        let bgs: Vec<&str> = e["bgs"].as_array().unwrap().iter().filter_map(|v| v.as_str()).collect();
                        let mut pos: Vec<usize> = vec![];
                        for bg in &bgs {
                            match rects.iter().position(|r| r.bg == *bg) {
                                Some(i) => pos.push(i),
                                None => { case_fails.push(format!("[rust] {}: order {:?} 有 bg 未绘制", name, bgs)); pos.clear(); break; }
                            }
                        }
                        if !pos.is_empty() {
                            for w in pos.windows(2) {
                                if w[0] >= w[1] {
                                    case_fails.push(format!("[rust] {}: order {:?} z-index 绘制顺序不符", name, bgs));
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
                                case_fails.push(format!("[rust] {}: hit({},{}) 命中了 {}，期望空白区", name, px, py, bg));
                            }
                        } else if bg != want_target {
                            case_fails.push(format!("[rust] {}: hit({},{}) = {}, want {}（pointer-events 命中豁免或 z 序不符）", name, px, py, bg, want_target));
                        }
                        if let Some(wc) = e["cursor"].as_str() {
                            if !wc.is_empty() && cursor != wc {
                                case_fails.push(format!("[rust] {}: hit({},{}) cursor = {}, want {}", name, px, py, cursor, wc));
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
            let _ = std::fs::write(&out, serde_json::to_string_pretty(&report).unwrap_or_default());
        }
        assert!(fails.is_empty(), "三端矩阵 Rust 端失败 {} 项:\n{}", fails.len(), fails.join("\n"));
    }
}
