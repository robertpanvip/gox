//! 绘制指令域（BACKEND_ARCHITECTURE_V2 阶段 3 第二步：scene/layout/paint 分离）。
//! DisplayList::from_scene = Taffy 布局 + 绘制指令编译（Retained mirror → Layout
//! → Display List 管线的 layout/paint 段）；浮层/滚动条/裁剪/文本绘制计划等
//! 指令域辅助。GPU 渲染执行见 render.rs（Renderer），场景镜像见 scene.rs。

use crate::binprotocol::NodeType;
use crate::util::*;
use glyphon::Buffer;
#[cfg(test)]
use glyphon::{FontSystem, Metrics};

// 与 Skia 后端 backend/skia/overlay.go 完全一致的语义：所有约定走 props 全量透传，
// gx-role 标记浮层角色，gx-anchor 引用锚点 id，gx-placement/gx-offset 定位。

pub(crate) fn gx_role(n: &crate::scene::SceneNode) -> &str {
    n.props.get("gx-role").map(|s| s.trim().to_ascii_lowercase())
        .map(|s| match s.as_str() {
            "popover" => "popover", "modal" => "modal",
            "portal" => "portal", "scrim" => "scrim", _ => "",
        })
        .unwrap_or("")
}
pub(crate) fn is_overlay(n: &crate::scene::SceneNode) -> bool {
    matches!(gx_role(n), "popover" | "modal" | "portal" | "scrim")
}
pub(crate) fn is_popover(n: &crate::scene::SceneNode) -> bool { gx_role(n) == "popover" }
pub(crate) fn is_modal(n: &crate::scene::SceneNode) -> bool { gx_role(n) == "modal" }
pub(crate) fn is_portal(n: &crate::scene::SceneNode) -> bool { gx_role(n) == "portal" }
pub(crate) fn is_scrim(n: &crate::scene::SceneNode) -> bool { gx_role(n) == "scrim" }
/// 内容型浮层（非遮罩）：统一在 scrim 之后绘制。
pub(crate) fn is_content_overlay(n: &crate::scene::SceneNode) -> bool {
    is_popover(n) || is_modal(n) || is_portal(n)
}

/// 视口居中浮层左上角（与 Skia placeCentered 对齐）。
pub(crate) fn place_centered(pw: f32, ph: f32, avail_w: f32, avail_h: f32) -> (f32, f32) {
    let pw = pw.min(avail_w);
    let ph = ph.min(avail_h);
    (((avail_w - pw) / 2.0).max(0.0), ((avail_h - ph) / 2.0).max(0.0))
}

/// 浮层定位结果：side(bottom/top) + align(start/center/end)。
pub(crate) fn parse_placement(s: &str) -> (&'static str, &'static str) {
    let lower = s.trim().to_ascii_lowercase();
    let parts: Vec<&str> = lower.split('-').collect();
    let side = match parts.first().copied() { Some("top") => "top", _ => "bottom" };
    let align = match parts.get(1).copied() {
        Some("center") => "center", Some("end") => "end", _ => "start",
    };
    (side, align)
}

/// 计算浮层左上角绝对坐标，空间不足时翻转（与 Skia placeOverlay 对齐）。
pub(crate) fn place_overlay(
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
pub(crate) fn flat_children<'a>(n: &'a crate::scene::SceneNode, out: &mut Vec<&'a crate::scene::SceneNode>) {
    for c in &n.children {
        if c.typ == NodeType::Fragment { flat_children(c, out); } else { out.push(c); }
    }
}

/// 把统一语义的 CSS linear-gradient 归一化成 GPU 渐变模型（方向向量 +
/// 0..1 色标）。角度换算对齐 CSS：0°=向上，90°=向右，180°=向下；渐变线
/// 穿过盒子中心，长度取盒子在渐变方向上的投影（与 Go
/// paintGradientBackgroundShader / GPU shader 同算法，GPU 端按 quad 宽高
/// 自行求 len，这里只负责方向与色标）。
pub(crate) fn gradient_model(
    g: &crate::text_effects::LinearGradient,
    box_w: f32, box_h: f32,
) -> Option<gx_gpu::Grad> {
    if box_w <= 0.0 || box_h <= 0.0 || g.stops.is_empty() {
        return None;
    }
    let (dx, dy) = gx_gpu::css_angle_to_dir(g.angle);
    // ch 边界型色段无百分比：按最大 end_ch 归一化，保持相对位置。
    let max_ch = g.stops.last().map(|x| x.end_ch.max(1)).unwrap_or(1) as f32;
    let stops: Vec<[f32; 5]> = g.stops.iter().map(|s| {
        let pos = s.offset.unwrap_or_else(|| s.end_ch as f32 / max_ch);
        [pos.clamp(0.0, 1.0),
         s.color.0 as f32, s.color.1 as f32, s.color.2 as f32, s.color.3 as f32]
    }).collect();
    Some(gx_gpu::Grad { dx, dy, stops })
}

/// object-fit：把 iw×ih 源映射进 (rx,ry,rw,rh)（与旧 draw_image_fit_impl
/// 同算法）。返回 (dst_rect, uv_window)。fill 铺满；contain 完整放入；
/// cover 铺满并溢出盒外（溢出部分由祖先 scissor 裁剪，与旧实现一致）。
pub(crate) fn fit_rect(
    iw: f32, ih: f32, fit: ObjectFit,
    rx: f32, ry: f32, rw: f32, rh: f32,
) -> ([f32; 4], [f32; 4]) {
    if iw <= 0.0 || ih <= 0.0 || rw <= 0.0 || rh <= 0.0 {
        return ([0.0, 0.0, 0.0, 0.0], [0.0, 0.0, 0.0, 0.0]);
    }
    if fit == ObjectFit::Fill {
        return ([rx, ry, rw, rh], [0.0, 0.0, iw, ih]);
    }
    let ar = iw / ih;
    let bar = rw / rh;
    let (dx, dy, dw, dh) = if fit == ObjectFit::Contain {
        if ar > bar {
            (rx, ry + (rh - rw / ar) / 2.0, rw, rw / ar)
        } else {
            (rx + (rw - rh * ar) / 2.0, ry, rh * ar, rh)
        }
    } else { // Cover
        if ar > bar {
            (rx + (rw - rh * ar) / 2.0, ry, rh * ar, rh)
        } else {
            (rx, ry + (rh - rw / ar) / 2.0, rw, rw / ar)
        }
    };
    ([dx, dy, dw, dh], [0.0, 0.0, iw, ih])
}

/// canvas 指令列表的内容哈希（逻辑纹理键——emit 每帧重新 decode 产生新
/// Arc，指针不稳定；内容哈希保证同一指令串跨帧拿到同一纹理 id）。
/// 按 rune 区间 [start,end) 计算文本的像素 x 范围，口径与 measure_text 一致
/// （ASCII 0.56em、其余 1em）。用于选区背景矩形定位。
pub(crate) fn selection_px_range(text: &str, start: usize, end: usize, size: f32) -> (f32, f32) {
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
pub(crate) type ClipRect = Option<(f32, f32, f32, f32)>; // x, y, w, h

/// 两个裁剪矩形求交；None 视作无限大。
pub(crate) fn clip_intersect(a: ClipRect, b: ClipRect) -> ClipRect {
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
pub(crate) fn clip_rejects(c: ClipRect, x: f32, y: f32, w: f32, h: f32) -> bool {
    match c {
        None => false,
        Some((cx, cy, cw, ch)) => x > cx + cw || x + w < cx || y > cy + ch || y + h < cy,
    }
}

/// 矩形与裁剪矩形求交，返回交集 (x,y,w,h)；完全在外返回 None。
pub(crate) fn clip_rect_xywh(c: ClipRect, x: f32, y: f32, w: f32, h: f32) -> Option<(f32, f32, f32, f32)> {
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
pub(crate) struct TextDraw {
    /// 稳定节点 ID：作为字形缓存 key，滚动时同一 item 的 ID 不变，
    /// 使未变文本的字形 buffer 可跨帧复用（只 shape 新增/变化项）。
    pub(crate) id: String,
    pub(crate) text: String,
    pub(crate) x: f32,
    pub(crate) y: f32,
    pub(crate) size: f32,
    pub(crate) color: (u8, u8, u8),
    pub(crate) clip: ClipRect,
    /// 统一 TextEffects 解析出的渐变着色分段（文本, RGB）。
    /// 空表示普通单色文字，走 default_color 快路径；非空走 glyphon 富文本 span。
    pub(crate) spans: Vec<(String, (u8, u8, u8))>,
    /// 三端统一 selection 协议解析出的选区；None 表示该文本无选区。
    pub(crate) selection: Option<SelectionDraw>,
    /// font-family 解析出的族名；None 表示默认 SansSerif。
    pub(crate) family: Option<String>,
    /// font-weight 数值（100..900）；None 表示默认字重。纳入 text_sig：
    /// 字重影响字形选择，必须重新 shape。
    pub(crate) weight: Option<u32>,
    /// line-height（像素，0=默认 1.35×size）：Metrics.line_height 决定行盒内基线。
    pub(crate) line_height: f32,
    /// opacity（含声明式动画采样后的值）用于文字 alpha，1.0 为不透明。
    pub(crate) alpha: f32,
}

/// object-fit：图片如何映射进布局盒（与 Skia 端 image.go 语义一致）。
#[derive(Clone, Copy, PartialEq)]
pub(crate) enum ObjectFit { Fill, Contain, Cover }

impl ObjectFit {
    pub(crate) fn parse(s: Option<&str>) -> Self {
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
pub(crate) struct SelectionDraw {
    pub(crate) start: usize,
    pub(crate) end: usize,
    pub(crate) bg: (u8, u8, u8, u8),
    pub(crate) color: Option<(u8, u8, u8)>,
}

/// 从节点标量 props 解析 selection 协议：&::selection JSON 子表 + selection-range。
/// 两者缺一不可；range 非法返回 None（无选区）。JSON 用 serde_json，解析失败静默回退。
pub(crate) fn parse_selection(props: &std::collections::HashMap<String, String>) -> Option<SelectionDraw> {
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
pub(crate) fn text_sig(t: &TextDraw) -> String {
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
pub(crate) struct GlyphEntry {
    pub(crate) sig: String,
    pub(crate) buffer: Buffer,
}

/// 字形增量复用计划（纯逻辑，便于单测）：
/// 返回本帧需要重新 shape 的文本下标，以及需要从缓存淘汰的 node_id。
/// - 当前文本的 id 不在缓存，或签名变化 → 需要 shape（新增项 / 内容变化）
/// - 缓存里有但当前帧不再出现 → 淘汰（滚出视口的项）
pub(crate) fn plan_glyphs(
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
pub(crate) struct ShapeDraw {
    pub(crate) x: f32,
    pub(crate) y: f32,
    pub(crate) w: f32,
    pub(crate) h: f32,
    pub(crate) bg: (u8, u8, u8),
    pub(crate) bg_present: bool,
    /// 通用 CSS linear-gradient 背景（无 gradient-text 标记时作用于容器本身）。
    /// 与文字渐变互斥：带 gradient-text 的 div 其渐变只裁剪到文字，不画背景。
    pub(crate) bg_gradient: Option<crate::text_effects::LinearGradient>,
    pub(crate) radius: f32,
    pub(crate) border: Option<((u8, u8, u8), f32)>,
    pub(crate) shadow: Option<crate::style::BoxShadow>,
    pub(crate) clip: ClipRect,
    /// img 原语：解码后的图片 + object-fit；None 表示普通矩形。
    pub(crate) img: Option<(std::sync::Arc<tiny_skia::Pixmap>, ObjectFit)>,
    /// canvas 原语：命令式绘制指令；None 表示普通节点。
    pub(crate) canvas: Option<std::sync::Arc<Vec<crate::canvas::CanvasOp>>>,
    /// opacity（含声明式动画采样后的值）用于背景 alpha，1.0 为不透明。
    pub(crate) alpha: f32,
    /// video 原语：ffmpeg 解码的当前帧；None 表示普通节点/无帧。
    pub(crate) video: Option<std::sync::Arc<crate::video::VideoFrame>>,
    /// video 节点 ID（用于取帧）；普通节点为空。
    pub(crate) video_id: String,
}

/// 苹果风格滚动条的几何（overlay，不占布局；画在容器右内侧）。
#[derive(Clone, Debug, PartialEq)]
pub(crate) struct ScrollBar {
    /// track 左上角与尺寸（track 覆盖整个容器高度，宽 TRACK_W）
    pub(crate) x: f32, pub(crate) y: f32, pub(crate) track_h: f32,
    /// thumb 顶与高
    pub(crate) thumb_y: f32, pub(crate) thumb_h: f32,
    /// 透明度 0..1（淡入淡出由 render 阶段按滚动活动度驱动）
    pub(crate) alpha: f32,
}

/// macOS 滚动条常量：细、圆头、半透明、overlay。
pub(crate) const SB_TRACK_W: f32 = 8.0;   // track 宽
pub(crate) const SB_THUMB_MIN: f32 = 32.0; // thumb 最小高度

/// 由容器几何与内容高度算滚动条。内容未溢出返回 None（不显示）。
/// 纯函数，便于单测。
pub(crate) fn scrollbar_geometry(
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
pub(crate) struct DisplayList {
    pub(crate) shapes: Vec<ShapeDraw>,
    pub(crate) texts: Vec<TextDraw>,
    /// 布局回流：每个节点的 (id, w, h)，emit 时按 Taffy 结果收集，
    /// 渲染后经 eventCb 送回 Host，供虚拟列表等组件按 node_id 订阅实测高度。
    pub(crate) measured: Vec<(String, f32, f32)>,
    /// 需要显示滚动条的容器几何（由 emit 计算内容高度后生成）
    pub(crate) scrollbars: Vec<ScrollBar>,
    /// (x, y, w, h, node_id, event, is_overlay, clip)
    // 第 9 元素是节点 cursor 属性（CSS cursor 样式，宿主消费）。
    pub(crate) hitboxes: Vec<(f32, f32, f32, f32, String, String, bool, ClipRect, String)>,
    /// (x, y, w, h, node_id)
    pub(crate) inputs: Vec<(f32, f32, f32, f32, String)>,
}

impl DisplayList {
    pub(crate) fn new() -> Self {
        DisplayList { shapes: vec![], texts: vec![], measured: vec![], scrollbars: vec![], hitboxes: vec![], inputs: vec![] }
    }
    /// 把镜像场景树编译成绘制指令。
    /// 布局交给 Taffy 0.5（完整 Flexbox 引擎）——
    /// 支持完整的 justify-content / align-items / gap / flex-wrap，
    /// 组件包装层（Fragment）摊平为透明节点，文本节点用 measure 回调测宽。
    /// 根节点的 size 设为窗口尺寸，因此 align-items:center 有确定参照。
    pub(crate) fn from_scene(
        root: &crate::scene::SceneNode,
        avail_w: f32,
        avail_h: f32,
        cache: &mut crate::layout::LayoutCache,
    ) -> Self {
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

        // ---- 布局缓存（layout.rs）：签名命中复用上帧 Taffy 树，跳过 build+compute ----
        let sig = crate::layout::layout_signature(root, avail_w, avail_h);
        if cache.sig == sig && cache.tree.is_some() {
            cache.reuses += 1;
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
        // scan/emit 阶段只读布局；build 阶段直接传 &mut TaffyTree。
        struct BuildCtx<'a> {
            taffy: &'a TaffyTree<TextMeasure>,
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
            taffy: &mut TaffyTree<TextMeasure>,
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
                return taffy.new_leaf_with_context(
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
                .map(|c| build(taffy, c, false, child_scrollable, color, size, avail_w, avail_h, my_tinh))
                .collect();

            taffy.new_with_children(style, &child_nodes).expect("taffy container")
        }

        let hit = cache.sig == sig && cache.tree.is_some();
        if !hit {
            cache.rebuilds += 1;
            let mut fresh = TaffyTree::new();
            let root_node = build(&mut fresh, root, true, false, (226, 232, 240), 16.0, avail_w, avail_h, TextInherit::default());
            fresh.compute_layout_with_measure(
                root_node,
                Size { width: AvailableSpace::Definite(avail_w), height: AvailableSpace::Definite(avail_h) },
                |known, avail, _id, mctx, _style| measure_text(known, avail, mctx),
            ).expect("taffy compute_layout");
            cache.tree = Some((fresh, root_node));
            cache.sig = sig;
        }
        let (tree, root_node) = cache.tree.as_ref().expect("layout cache populated");
        let ctx = BuildCtx { taffy: tree };

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
        scan(root, *root_node, &ctx, 0.0, 0.0, &mut anchors, &mut overlays);

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
        emit(root, *root_node, &ctx, &mut dl, (226, 232, 240), 16.0, 0.0, 0.0,
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
    pub(crate) fn hit(&self, x: f32, y: f32) -> Option<&(f32, f32, f32, f32, String, String, bool, ClipRect, String)> {
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

#[cfg(test)]
mod scrollbar_tests {
    use super::*;

    #[test]
    pub(crate) fn no_scrollbar_when_content_fits() {
        // 内容高度 <= 容器高度：不显示滚动条。
        assert!(scrollbar_geometry(0.0, 0.0, 100.0, 200.0, 180.0, 0.0, 1.0).is_none());
        assert!(scrollbar_geometry(0.0, 0.0, 100.0, 200.0, 200.0, 0.0, 1.0).is_none());
    }

    #[test]
    pub(crate) fn scrollbar_thumb_geometry() {
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
    pub(crate) fn thumb_min_height_clamped() {
        // 内容极大 → thumb 高被最小高度 32 兜底，且不超容器高。
        let sb = scrollbar_geometry(0.0, 0.0, 100.0, 200.0, 100000.0, 0.0, 1.0).unwrap();
        assert!((sb.thumb_h - SB_THUMB_MIN).abs() < 0.01, "thumb_h={}", sb.thumb_h);
    }

    #[test]
    pub(crate) fn scroll_top_clamped_to_range() {
        // scrollTop 超出 [0,maxScroll] 时 thumb 不越界。
        let over = scrollbar_geometry(0.0, 50.0, 100.0, 200.0, 600.0, 9999.0, 1.0).unwrap();
        assert!(over.thumb_y <= 50.0 + 200.0 - over.thumb_h + 0.01);
        let under = scrollbar_geometry(0.0, 50.0, 100.0, 200.0, 600.0, -100.0, 1.0).unwrap();
        assert!((under.thumb_y - 50.0).abs() < 0.01);
    }

    #[test]
    pub(crate) fn alpha_clamped() {
        let a = scrollbar_geometry(0.0, 0.0, 100.0, 200.0, 600.0, 0.0, 2.0).unwrap();
        assert!((a.alpha - 1.0).abs() < 0.01);
        let b = scrollbar_geometry(0.0, 0.0, 100.0, 200.0, 600.0, 0.0, -1.0).unwrap();
        assert!((b.alpha - 0.0).abs() < 0.01);
    }

    /// 构造一个 overflow=scroll 容器（100×100）内含总高 500 的子树，跑 from_scene。
    pub(crate) fn overflow_scene(scroll_top: f32) -> crate::scene::SceneNode {
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
    pub(crate) fn from_scene_collects_measured() {
        let dl = DisplayList::from_scene(&overflow_scene(0.0), 800.0, 600.0, &mut crate::layout::LayoutCache::new());
        // 回流应覆盖容器 + 5 个 item（非空 id 的节点）。
        assert_eq!(dl.measured.len(), 6, "应为 vp + 5 items 收集实测尺寸: {:?}", dl.measured);
        let item0 = dl.measured.iter().find(|(id, _, _)| id == "item0").expect("item0 应在回流里");
        assert!((item0.2 - 100.0).abs() < 0.5, "item0 实测高度应约 100，实际 {}", item0.2);
    }

    #[test]
    pub(crate) fn from_scene_emits_scrollbar_for_overflow() {
        let dl = DisplayList::from_scene(&overflow_scene(0.0), 800.0, 600.0, &mut crate::layout::LayoutCache::new());
        assert_eq!(dl.scrollbars.len(), 1, "溢出容器应生成一个滚动条");
        let sb = &dl.scrollbars[0];
        // 容器 100 高，内容 500 高 → thumb_h = 100*100/500 = 20 → 被最小高度 32 兜底。
        assert!((sb.thumb_h - SB_THUMB_MIN).abs() < 0.01, "thumb_h={}", sb.thumb_h);
    }

    #[test]
    pub(crate) fn from_scene_no_scrollbar_when_no_overflow() {
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
        let dl = DisplayList::from_scene(&vp, 800.0, 600.0, &mut crate::layout::LayoutCache::new());
        assert!(dl.scrollbars.is_empty(), "未溢出不应有滚动条");
    }

    #[test]
    pub(crate) fn scrollbar_moves_with_scroll() {
        let top = DisplayList::from_scene(&overflow_scene(0.0), 800.0, 600.0, &mut crate::layout::LayoutCache::new());
        let mid = DisplayList::from_scene(&overflow_scene(200.0), 800.0, 600.0, &mut crate::layout::LayoutCache::new());
        assert_eq!(top.scrollbars.len(), 1);
        assert_eq!(mid.scrollbars.len(), 1);
        assert!(mid.scrollbars[0].thumb_y > top.scrollbars[0].thumb_y,
            "滚动后 thumb 应下移: {} -> {}", top.scrollbars[0].thumb_y, mid.scrollbars[0].thumb_y);
    }
}

#[cfg(test)]
mod text_sig_tests {
    use super::*;

    pub(crate) fn td(id: &str, text: &str, size: f32) -> TextDraw {
        TextDraw {
            id: id.into(), text: text.into(), x: 0.0, y: 0.0, size,
            color: (0, 0, 0), clip: None, spans: vec![], selection: None, family: None, weight: None, line_height: 0.0, alpha: 1.0,
        }
    }

    /// 用一组 (id,text,size) 构造"上一帧缓存"：只需签名，buffer 用空占位。
    pub(crate) fn cache_of(items: &[(&str, &str, f32)]) -> std::collections::HashMap<String, GlyphEntry> {
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
    pub(crate) fn sig_depends_only_on_text_and_size() {
        // 签名只看文本+字号，与 id/坐标/颜色无关——这是按 ID 复用的正确性基础。
        let a = td("a", "A", 16.0);
        let moved = TextDraw { id: "b".into(), x: 99.0, y: -6.0, color: (255, 0, 0), ..a.clone() };
        assert_eq!(text_sig(&a), text_sig(&moved));
        assert_ne!(text_sig(&a), text_sig(&td("a", "B", 16.0)));   // 内容变
        assert_ne!(text_sig(&a), text_sig(&td("a", "A", 20.0)));   // 字号变
    }

    #[test]
    pub(crate) fn unchanged_texts_need_no_shape() {
        // 小幅滚动：ID 和文本都没变 → 全部复用，零 shape。
        let cache = cache_of(&[("a", "A", 16.0), ("b", "B", 16.0)]);
        let cur = [td("a", "A", 16.0), td("b", "B", 16.0)];
        let (shaped, evicted) = plan_glyphs(&cur, &cache);
        assert!(shaped.is_empty(), "未变文本应零 shape");
        assert!(evicted.is_empty());
    }

    #[test]
    pub(crate) fn scroll_boundary_shapes_only_new_item() {
        // 虚拟列表跨 item 边界：滚出 "a"、滚入 "c"，"b" 未变 →
        // 只有 "c" 需要 shape，"a" 被淘汰，"b" 复用。
        let cache = cache_of(&[("a", "A", 16.0), ("b", "B", 16.0)]);
        let cur = [td("b", "B", 16.0), td("c", "C", 16.0)];
        let (shaped, evicted) = plan_glyphs(&cur, &cache);
        assert_eq!(shaped, vec![1], "只有滚入的 c 需要 shape");
        assert_eq!(evicted, vec!["a".to_string()], "滚出的 a 应被淘汰");
    }

    #[test]
    pub(crate) fn content_change_shapes_only_changed_item() {
        // 某一项文本变化：只有该项重新 shape，其余复用。
        let cache = cache_of(&[("a", "A", 16.0), ("b", "B", 16.0)]);
        let cur = [td("a", "A", 16.0), td("b", "B2", 16.0)];
        let (shaped, evicted) = plan_glyphs(&cur, &cache);
        assert_eq!(shaped, vec![1], "内容变的 b 需重新 shape");
        assert!(evicted.is_empty());
    }

    #[test]
    pub(crate) fn reorder_keeps_identity() {
        // 顺序变化但 ID+内容不变（如 For 重排）：全部复用，零 shape——
        // 这正是整段签名方案会误判为全量重建的场景。
        let cache = cache_of(&[("a", "A", 16.0), ("b", "B", 16.0)]);
        let cur = [td("b", "B", 16.0), td("a", "A", 16.0)];
        let (shaped, evicted) = plan_glyphs(&cur, &cache);
        assert!(shaped.is_empty(), "重排不应触发 shape");
        assert!(evicted.is_empty());
    }

    #[test]
    pub(crate) fn cold_cache_shapes_all() {
        let cache = cache_of(&[]);
        let cur = [td("a", "A", 16.0), td("b", "B", 16.0)];
        let (shaped, evicted) = plan_glyphs(&cur, &cache);
        assert_eq!(shaped, vec![0, 1]);
        assert!(evicted.is_empty());
    }

    #[test]
    pub(crate) fn gradient_spans_change_signature_for_reshaping() {
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
    pub(crate) fn gradient_model_builds_for_linear_gradient() {
        let g = crate::text_effects::parse_linear_gradient(
            "linear-gradient(90deg, #ff0000 0%, #0000ff 100%)",
        ).unwrap();
        // 90°=向右；盒子 200x100。归一化模型应给出方向（1,0）与 2 个色标。
        let m = gradient_model(&g, 200.0, 100.0).unwrap();
        assert!((m.dx - 1.0).abs() < 1e-6 && m.dy.abs() < 1e-6);
        assert_eq!(m.stops.len(), 2);
        assert!((m.stops[0][0] - 0.0).abs() < 1e-6);
        assert!((m.stops[1][0] - 1.0).abs() < 1e-6);
        // 退化盒子不出模型。
        assert!(gradient_model(&g, 0.0, 100.0).is_none());
    }

    #[test]
    pub(crate) fn fit_rect_matches_object_fit_semantics() {
        use super::ObjectFit;
        // fill：铺满盒子。
        let (d, uv) = fit_rect(100.0, 50.0, ObjectFit::Fill, 0.0, 0.0, 200.0, 200.0);
        assert_eq!(d, [0.0, 0.0, 200.0, 200.0]);
        assert_eq!(uv, [0.0, 0.0, 100.0, 50.0]);
        // contain：2:1 源放 1:1 盒 → 高度受限，水平居中。
        let (d, _) = fit_rect(100.0, 50.0, ObjectFit::Contain, 0.0, 0.0, 100.0, 100.0);
        assert!((d[3] - 50.0).abs() < 1e-6 && (d[2] - 100.0).abs() < 1e-6);
        assert!((d[1] - 25.0).abs() < 1e-6);
        // cover：2:1 源盖 1:1 盒 → 宽度溢出，水平居中（溢出由 scissor 裁）。
        let (d, _) = fit_rect(100.0, 50.0, ObjectFit::Cover, 0.0, 0.0, 100.0, 100.0);
        assert!((d[2] - 200.0).abs() < 1e-6 && (d[0] + 50.0).abs() < 1e-6);
    }

    #[test]
    pub(crate) fn from_scene_inherits_gradient_from_parent_to_text() {
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
        let dl = DisplayList::from_scene(&span, 800.0, 600.0, &mut crate::layout::LayoutCache::new());
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
    pub(crate) fn place_bottom_start() {
        let (x, y) = place_overlay(100.0, 200.0, 80.0, 30.0, 120.0, 60.0, "bottom-start", 8.0, 800.0, 600.0);
        assert_eq!(x, 100.0);
        assert_eq!(y, 238.0); // 200+30+8
    }

    #[test]
    pub(crate) fn place_bottom_center() {
        let (x, _y) = place_overlay(100.0, 200.0, 100.0, 30.0, 120.0, 60.0, "bottom-center", 8.0, 800.0, 600.0);
        assert_eq!(x, 90.0); // 100 + (100-120)/2
    }

    #[test]
    pub(crate) fn place_flip_to_top() {
        // 锚点靠近底部，下方放不下 → 翻到 top。
        let (x, y) = place_overlay(100.0, 520.0, 80.0, 30.0, 120.0, 60.0, "bottom-start", 8.0, 800.0, 600.0);
        assert_eq!(x, 100.0);
        assert_eq!(y, 452.0); // 520 - 8 - 60
    }

    #[test]
    pub(crate) fn place_end_align() {
        let (x, _) = place_overlay(100.0, 200.0, 100.0, 30.0, 60.0, 60.0, "bottom-end", 8.0, 800.0, 600.0);
        assert_eq!(x, 140.0); // 100+100-60
    }

    #[test]
    pub(crate) fn place_centered_modal() {
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
    pub(crate) fn intersect_clips() {
        let a = Some((0.0, 0.0, 100.0, 50.0));
        let b = Some((20.0, 10.0, 60.0, 60.0));
        assert_eq!(clip_intersect(a, b), Some((20.0, 10.0, 60.0, 40.0)));
    }

    #[test]
    pub(crate) fn intersect_with_none() {
        let a = Some((0.0, 0.0, 100.0, 50.0));
        assert_eq!(clip_intersect(a, None), a);
        assert_eq!(clip_intersect(None, a), a);
    }

    #[test]
    pub(crate) fn reject_and_rect() {
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

    pub(crate) fn sel_props() -> std::collections::HashMap<String, String> {
        let mut p = std::collections::HashMap::new();
        p.insert("selection-range".into(), "2:5".into());
        p.insert("&::selection".into(), "{\"bg\":\"#264f7880\",\"color\":\"#ffffff\"}".into());
        p
    }

    #[test]
    pub(crate) fn parse_selection_reads_range_and_colors() {
        let sel = parse_selection(&sel_props()).expect("应解析出选区");
        assert_eq!((sel.start, sel.end), (2, 5));
        assert_eq!(sel.bg.3, 0x80, "半透明 alpha 应保留");
        assert_eq!(sel.color, Some((255, 255, 255)));
    }

    #[test]
    pub(crate) fn parse_selection_rejects_missing_or_bad() {
        let empty = std::collections::HashMap::new();
        assert!(parse_selection(&empty).is_none());
        let mut bad = sel_props();
        bad.insert("selection-range".into(), "5:2".into());
        assert!(parse_selection(&bad).is_none(), "start>=end 应视为无选区");
    }

    #[test]
    pub(crate) fn selection_px_range_matches_measure() {
        let ascii = selection_px_range("abcdef", 2, 5, 10.0);
        assert!((ascii.0 - 11.2).abs() < 0.01, "2*0.56*10=11.2, got {:?}", ascii);
        assert!((ascii.1 - 28.0).abs() < 0.01, "5*0.56*10=28, got {:?}", ascii);
        let cjk = selection_px_range("你ab好", 1, 3, 10.0);
        assert!((cjk.0 - 10.0).abs() < 0.01, "CJK 首字 1em, got {:?}", cjk);
    }

    #[test]
    pub(crate) fn selection_in_text_sig_forces_reshaping() {
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

    pub(crate) fn font_props(stack: &str) -> std::collections::HashMap<String, String> {
        let mut p = std::collections::HashMap::new();
        p.insert("font-family".into(), stack.into());
        p
    }

    #[test]
    pub(crate) fn font_family_maps_monospace_stack() {
        // Editor 等宽栈应映射到已加载的 DejaVu Sans Mono。
        let f = parse_font_family(&font_props(
            "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace"));
        assert_eq!(f.as_deref(), Some("DejaVu Sans Mono"));
    }

    #[test]
    pub(crate) fn font_family_sans_stack_uses_default() {
        let f = parse_font_family(&font_props("system-ui, -apple-system, sans-serif"));
        assert!(f.is_none(), "纯 sans 栈应沿用默认 SansSerif");
        assert!(parse_font_family(&font_props("")).is_none());
    }

    #[test]
    pub(crate) fn font_family_concrete_name_passthrough() {
        let f = parse_font_family(&font_props("Menlo, monospace"));
        assert_eq!(f.as_deref(), Some("DejaVu Sans Mono"), "Menlo 属等宽候选");
        let f2 = parse_font_family(&font_props("Fira Code, monospace"));
        assert_eq!(f2.as_deref(), Some("Fira Code"), "未在等宽名单的具体族名原样使用");
    }

    #[test]
    pub(crate) fn font_family_in_text_sig_forces_reshaping() {
        let base = TextDraw {
            id: "x".into(), text: "abcd".into(), x: 0.0, y: 0.0, size: 13.0,
            color: (0, 0, 0), clip: None, spans: vec![], selection: None, family: None, weight: None, line_height: 0.0, alpha: 1.0,
        };
        let mut mono = base.clone();
        mono.family = Some("DejaVu Sans Mono".into());
        assert_ne!(text_sig(&base), text_sig(&mono), "字体族变化应触发重新 shape");
    }
}

