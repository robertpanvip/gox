//! 渲染内核共享工具：文本测量（Taffy 回调）与属性解析（颜色/尺寸/字体）。
//! 从 render.rs 拆出（阶段 3 第二步），paint.rs 与 render.rs 共用。

// 文本测量上下文：继承后的字号 + 行高（像素，0=默认 1.35×size）+ 文本内容。
// Taffy 在布局期回调测宽。行高与折叠语义必须与 emit/绘制端同口径，否则
// 布局高度与文字落点错位。
#[derive(Clone)]
pub(crate) struct TextMeasure {
    pub(crate) text: String,
    pub(crate) size: f32,
    pub(crate) line_height: f32,
}

pub(crate) fn measure_text(
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

pub(crate) fn canvas_ops_key(ops: &[crate::canvas::CanvasOp]) -> u64 {
    use std::collections::hash_map::DefaultHasher;
    use std::hash::{Hash, Hasher};
    let mut h = DefaultHasher::new();
    for op in ops {
        op.op.hash(&mut h);
        for v in [op.x, op.y, op.w, op.h, op.cx, op.cy, op.r, op.x1, op.y1, op.x2, op.y2, op.radius, op.stroke_width] {
            v.to_bits().hash(&mut h);
        }
        for st in [&op.d, &op.fill, &op.stroke] {
            st.hash(&mut h);
        }
    }
    h.finish()
}

pub(crate) fn parse_color(s: &str) -> (u8, u8, u8) {
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

pub(crate) fn parse_px(s: &str) -> Option<f32> {
    s.trim().trim_end_matches("px").trim().parse::<f32>().ok()
}

/// 从 CSS font-family 栈解析出 cosmic-text 可用的族名。
/// 返回 None 表示沿用默认 SansSerif（含 system-ui/-apple-system/sans-serif 栈）。
/// 栈里的等宽意图（monospace/ui-monospace/Menlo/Consolas/SFMono...）映射到已加载的
/// DejaVu Sans Mono；具体族名原样交给 cosmic-text，未加载时它会回退默认。
pub(crate) fn parse_font_family(props: &std::collections::HashMap<String, String>) -> Option<String> {
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
