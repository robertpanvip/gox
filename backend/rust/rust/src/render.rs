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

/// 单条文本的内容签名：字形 buffer 只取决于 (内容, 字号)，与坐标/颜色无关。
/// 缓存按 node_id 索引，同一 ID 签名不变即可复用其 buffer。
fn text_sig(t: &TextDraw) -> String {
    let mut s = String::with_capacity(t.text.len() + 16);
    s.push_str(&t.text);
    s.push('@');
    s.push_str(&format!("{:.2}", t.size));
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
    radius: f32,
    border: Option<((u8, u8, u8), f32)>,
    shadow: Option<crate::style::BoxShadow>,
    clip: ClipRect,
    /// img 原语：解码后的图片 + object-fit；None 表示普通矩形。
    img: Option<(std::sync::Arc<tiny_skia::Pixmap>, ObjectFit)>,
    /// canvas 原语：命令式绘制指令；None 表示普通节点。
    canvas: Option<std::sync::Arc<Vec<crate::canvas::CanvasOp>>>,
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
    /// 需要显示滚动条的容器几何（由 emit 计算内容高度后生成）
    scrollbars: Vec<ScrollBar>,
    /// (x, y, w, h, node_id, event, is_overlay, clip)
    hitboxes: Vec<(f32, f32, f32, f32, String, String, bool, ClipRect)>,
    /// (x, y, w, h, node_id)
    inputs: Vec<(f32, f32, f32, f32, String)>,
}

impl DisplayList {
    fn new() -> Self {
        DisplayList { shapes: vec![], texts: vec![], scrollbars: vec![], hitboxes: vec![], inputs: vec![] }
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
            parent_scrollable: bool,
            inherited_color: (u8, u8, u8),
            inherited_size: f32,
            avail_w: f32, avail_h: f32,
        ) -> taffy::NodeId {
            let p = |k: &str| node.props.get(k).map(|s| s.as_str());
            let color = p("color").map(parse_color).unwrap_or(inherited_color);
            let size = p("font-size").and_then(parse_px).unwrap_or(inherited_size);

            if node.typ == NodeType::Text {
                let mut ts = Style::default();
                // overflow 容器的子树不参与压缩，否则内容被压进视口、永不溢出，
                // 滚动与滚动条都失效（标准滚动容器语义：子项 flex-shrink:0）。
                if parent_scrollable { ts.flex_shrink = 0.0; }
                return ctx.taffy.new_leaf_with_context(
                    ts,
                    TextMeasure { text: node.text.clone(), size },
                ).expect("taffy text leaf");
            }

            let pad = p("padding").and_then(parse_px).unwrap_or(0.0);
            let gap = p("gap").or_else(|| p("column-gap")).and_then(parse_px).unwrap_or(0.0);
            // absolute：浮层/遮罩（gx-role）或显式 position:absolute 都脱离父 flex 流。
            // 浮层位置由第二遍按锚点矩形单独计算；通用 absolute 用 inset 由 Taffy 定位。
            let st = crate::style::parse_style(node);
            let is_abs = is_overlay(node) || st.position == crate::style::Position::Absolute;
            let mut style = Style {
                position: if is_abs { Position::Absolute } else { Position::Relative },
                flex_direction: dir_of(p("flex-direction").unwrap_or("column")),
                padding: Rect {
                    left: length(pad), right: length(pad),
                    top: length(pad), bottom: length(pad),
                },
                gap: Size { width: length(gap), height: length(gap) },
                align_items: align_of(p("align-items")),
                justify_content: justify_of(p("justify-content")),
                // overflow 容器的直接子项不压缩，保证内容可溢出 → 滚动可生效。
                flex_shrink: if parent_scrollable { 0.0 } else { 1.0 },
                ..Default::default()
            };
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
                .map(|c| build(ctx, c, false, child_scrollable, color, size, avail_w, avail_h))
                .collect();

            ctx.taffy.new_with_children(style, &child_nodes).expect("taffy container")
        }

        let mut ctx = BuildCtx { taffy };
        let root_node = build(&mut ctx, root, true, false, (226, 232, 240), 16.0, avail_w, avail_h);
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
        ) -> f32 {
            // 主树遍历时跳过浮层子树（它们由后续的遮罩/浮层遍单独绘制，保证层级）。
            if skip_overlays && is_overlay(node) {
                return oy;
            }
            let p = |k: &str| node.props.get(k).map(|s| s.as_str());
            let color = p("color").map(parse_color).unwrap_or(inherited_color);
            let size = p("font-size").and_then(parse_px).unwrap_or(inherited_size);
            let lay = ctx.taffy.layout(tnode).expect("taffy layout");
            // 浮层根用第二遍算出的目标坐标；其余节点沿用 Taffy 相对坐标累加。
            let (x, y) = overrides.get(&node.id).copied()
                .unwrap_or((ox + lay.location.x, oy + lay.location.y));
            let mut w = lay.size.width;
            let mut h = lay.size.height;
            if is_scrim(node) {
                w = avail_w; h = avail_h; // 遮罩覆盖整个画布
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
                // (font_size, line_height=1.35×size) 自行处理行盒内基线，故这里不加
                // 任何 size 比例的魔法偏移——否则紧凑容器（如 Input 高 20px）里文字会偏下。
                if !clip_rejects(my_clip, x, y, size * 4.0, size * 1.35) {
                    dl.texts.push(TextDraw { id: node.id.clone(), text: node.text.clone(), x, y, size, color, clip: my_clip });
                }
                return y + size * 1.35;
            }

            if node.typ == NodeType::Div {
                let bg = p("bg").or_else(|| p("background")).map(parse_color);
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
                    dl.shapes.push(ShapeDraw {
                        x, y, w, h,
                        bg: bg.unwrap_or((0, 0, 0)), bg_present: bg.is_some(), radius, border,
                        shadow: st.shadow.clone(),
                        clip: my_clip,
                        img, canvas,
                    });
                }
                if let Some(ev) = node.events.iter().find(|e| e.starts_with("on")) {
                    dl.hitboxes.push((x, y, w, h, node.id.clone(), ev.clone(), is_overlay(node), my_clip));
                }
                if node.props.contains_key("gx-input-id") {
                    dl.inputs.push((x, y, w, h, node.id.clone()));
                }
            }

            let mut kids: Vec<&crate::scene::SceneNode> = Vec::new();
            flat_children(node, &mut kids);
            let mut subtree_bottom = y + h; // 至少到自身底边
            for (i, c) in kids.iter().enumerate() {
                let child_tnode = ctx.taffy.child_at_index(tnode, i).expect("taffy child");
                let cb = emit(c, child_tnode, ctx, dl, color, size, child_ox, child_oy, overrides, avail_w, avail_h, skip_overlays, my_clip);
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
            &overrides, avail_w, avail_h, true, None);
        // 第二遍：遮罩（拦截外部点击）。
        for ov in overlays.iter().filter(|o| is_scrim(o.node)) {
            emit(ov.node, ov.tnode, &ctx, &mut dl, (226, 232, 240), 16.0, 0.0, 0.0,
                &overrides, avail_w, avail_h, false, None);
        }
        // 第三遍：内容浮层 popover → modal → portal（最上层），按角色分层保证层级稳定。
        for role in ["popover", "modal", "portal"] {
            for ov in overlays.iter().filter(|o| gx_role(o.node) == role) {
                emit(ov.node, ov.tnode, &ctx, &mut dl, (226, 232, 240), 16.0, 0.0, 0.0,
                    &overrides, avail_w, avail_h, false, None);
            }
        }
        dl
    }

    /// 命中测试：浮层/遮罩优先于普通内容；同级内面积最小的包含点命中。
    fn hit(&self, x: f32, y: f32) -> Option<&(f32, f32, f32, f32, String, String, bool, ClipRect)> {
        type Hit<'a> = &'a (f32, f32, f32, f32, String, String, bool, ClipRect);
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
    /// 按稳定 node_id 缓存的字形：只 shape 新增/内容变化的文本，其余跨帧复用。
    /// 虚拟列表滚动跨 item 边界时只有进出视口的 1 个 ID 变化，其余全部命中，
    /// 比"整段签名一致才全复用"更精确——任何内容变化帧都能复用未变的项。
    glyph_cache: std::collections::HashMap<String, GlyphEntry>,
    /// 缓存的文本区域屏幕坐标（供命中/调试）
    cached_areas_dirty: bool,
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
            animator: crate::style::Animator::new(),
            anim_origin: std::time::Instant::now(),
            scrollbar_alpha: 0.0,
            scrollbar_last_top: 0.0,
            scrollbar_last_activity: std::time::Instant::now(),
            frame_dirty: true,
            glyph_cache: std::collections::HashMap::new(),
            cached_areas_dirty: true,
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
                    p.set_color(Color::from_rgba8(s.bg.0, s.bg.1, s.bg.2, 255));
                    p.anti_alias = true;
                    pm.fill_path(&path, &p, FillRule::Winding, Transform::identity(), None);
                }
                // img：object-fit 缩放绘制，圆角时用同形状 Mask 裁剪，与 Skia 端一致。
                if let Some((pix, fit)) = &s.img {
                    Self::draw_image_fit_impl(&mut pm, pix, *fit, rx, ry, rw, rh, s.radius);
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

    pub fn render(&mut self) {
        let Some(scene) = self.scene.as_ref() else { return; };
        // transition：按当前时间推进动画。动画进行中必须逐帧重算（插值值在变）。
        let now_ms = self.anim_origin.elapsed().as_millis();
        self.animator.set_now(now_ms);
        let animating = self.animator.is_active();
        let need_rebuild = self.frame_dirty || animating || self.cached_areas_dirty;

        self.viewport.update(&self.queue, Resolution { width: self.config.width, height: self.config.height });

        if need_rebuild {
            // —— 有变化 / 动画中：全量重算布局、背景光栅化、文本整形 ——
            let animated = self.animator.apply(scene);
            let dl = DisplayList::from_scene(&animated, self.width as f32, self.height as f32);
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
                let mut b = Buffer::new(&mut self.font_system, Metrics::new(t.size, t.size * 1.35));
                b.set_size(&mut self.font_system, Some(700.0), Some(80.0));
                let attrs = Attrs::new().family(Family::SansSerif);
                b.set_text(&mut self.font_system, &t.text, &attrs, Shaping::Advanced);
                b.shape_until_scroll(&mut self.font_system, false);
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
                default_color: Color::rgb(t.color.0, t.color.1, t.color.2),
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
        self.display.hit(x, y).map(|(_, _, _, _, id, ev, _, _)| InputEvent {
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
            color: (0, 0, 0), clip: None,
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
