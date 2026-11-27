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
    if let taffy::geometry::Size {
        width: Some(w),
        height: Some(h),
    } = known
    {
        return taffy::geometry::Size {
            width: w,
            height: h,
        };
    }
    let Some(c) = ctx else {
        return taffy::geometry::Size::ZERO;
    };
    // CJK/全角按 1em，ASCII 按 0.56em，行高 1.35em（或显式 line-height）。
    let w = c
        .text
        .chars()
        .map(|ch| if ch.is_ascii() { 0.56 } else { 1.0 })
        .sum::<f32>()
        * c.size;
    let h = if c.line_height > 0.0 {
        c.line_height
    } else {
        c.size * 1.35
    };
    taffy::geometry::Size {
        width: w,
        height: h,
    }
}

pub(crate) fn canvas_ops_key(ops: &[crate::canvas::CanvasOp]) -> u64 {
    use std::collections::hash_map::DefaultHasher;
    use std::hash::{Hash, Hasher};
    let mut h = DefaultHasher::new();
    for op in ops {
        op.op.hash(&mut h);
        for v in [
            op.x,
            op.y,
            op.w,
            op.h,
            op.cx,
            op.cy,
            op.r,
            op.x1,
            op.y1,
            op.x2,
            op.y2,
            op.radius,
            op.stroke_width,
        ] {
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
            hex.chars()
                .map(|c| format!("{}{}", c, c))
                .collect::<String>()
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
pub(crate) fn parse_font_family(
    props: &std::collections::HashMap<String, String>,
) -> Option<String> {
    let stack = props.get("font-family")?;
    resolve_family_stack(stack)
}

/// 从 CSS font-family 栈文本解析 cosmic-text 族名（parse_font_family 的无 props 核心）。
/// 供绘制端（props map）与测量端（C ABI 直传栈字符串）共用同一套解析语义。
pub(crate) fn resolve_family_stack(stack: &str) -> Option<String> {
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
        if matches!(
            lower.as_str(),
            "monospace"
                | "ui-monospace"
                | "fixed"
                | "menlo"
                | "consolas"
                | "sfmono-regular"
                | "dejavu sans mono"
                | "noto sans mono"
                | "liberation mono"
                | "courier new"
        ) {
            want_mono = true;
            continue;
        }
        if matches!(
            lower.as_str(),
            "sans-serif" | "serif" | "system-ui" | "system" | "-apple-system" | "ui-sans-serif"
        ) {
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

// ---- 编辑器字符 advance 实测（Go 桌面装配层注入 ui.SetCharWidth 用）----
//
// 语义与 skia 后端 MeasureEditorAdvances 对齐：narrow = ASCII 代表样本的平均
// advance；wide = CJK 代表字符的平均 advance（cosmic-text 自动 CJK 回退）。
// 点击换算（PickColumn）与光标渲染（PrefixWidth）依赖这两个值——不实测会让
// 点击定位随列号累计偏移。真实 shaping（glyphon Buffer）而非估算系数，与
// 绘制端 render.rs 的字形来源完全同源，保证测量值=渲染 advance。

use glyphon::{Attrs, Buffer, Family, FontSystem, Metrics, Shaping};
use std::sync::{Mutex, OnceLock};

/// 窄字符代表样本：覆盖字母/数字/常见标点/空格（与 skia 端同一串）。
const ADVANCE_NARROW_SAMPLE: &str = "abcdefghij0123456789{}()<>=+-*/. ";
/// 宽字符代表样本：CJK（与 skia 端同一串）。
const ADVANCE_WIDE_SAMPLE: &str = "中中中中";

/// 进程级共享 FontSystem：字体库加载（扫描系统字体目录）开销大，测量是
/// 低频调用（装配层启动时一次），缓存后重复调用接近零开销（对齐 skia 语义）。
static MEASURE_FONT_SYSTEM: OnceLock<Mutex<FontSystem>> = OnceLock::new();

/// 对一段文本做真实 shaping 并返回平均字符 advance。
fn shaping_advance(fs: &mut FontSystem, text: &str, family: Option<&str>, size: f32) -> f32 {
    let mut b = Buffer::new(fs, Metrics::new(size, size * 1.35));
    // 足够大的包裹宽度，保证单行不折行（advance 求和才是完整行宽）。
    b.set_size(fs, Some(4096.0), Some(400.0));
    let attrs = match family {
        Some(f) => Attrs::new().family(Family::Name(f)),
        None => Attrs::new().family(Family::SansSerif),
    };
    b.set_text(fs, text, &attrs, Shaping::Advanced);
    b.shape_until_scroll(fs, false);
    let mut total = 0.0f32;
    for run in b.layout_runs() {
        for g in run.glyphs {
            total += g.w; // cosmic-text 0.14：LayoutGlyph.w 即该字形的 advance 宽度
        }
    }
    total / text.chars().count().max(1) as f32
}

/// 实测编辑器代码字体的 narrow/wide 平均 advance。
/// familyStack 是 CSS font-family 栈文本（与绘制端 parse_font_family 同一套解析）。
pub(crate) fn measure_advances(family_stack: &str, size: f32) -> (f32, f32) {
    let family = resolve_family_stack(family_stack);
    let mutex = MEASURE_FONT_SYSTEM.get_or_init(|| Mutex::new(FontSystem::new()));
    let mut fs = mutex.lock().unwrap();
    let n = shaping_advance(&mut fs, ADVANCE_NARROW_SAMPLE, family.as_deref(), size);
    let w = shaping_advance(&mut fs, ADVANCE_WIDE_SAMPLE, family.as_deref(), size);
    (n, w)
}

#[cfg(test)]
mod measure_tests {
    use super::*;

    /// 测量端与绘制端同源：narrow/wide 必须为正（字体库在无头环境也能加载），
    /// 且等宽字体栈下 ASCII/CJK advance 关系符合等宽渲染预期。
    #[test]
    fn measure_advances_positive_and_ordering() {
        let (n, w) = measure_advances("Consolas, 'JetBrains Mono', monospace", 13.0);
        assert!(n > 0.0, "narrow advance 应为正, got {n}");
        assert!(w > 0.0, "wide advance 应为正, got {w}");
        // CJK 全角在等宽族下通常 ≥ 单个 ASCII advance（DejaVu Sans Mono CJK 回退 2:1 或更宽）。
        assert!(w >= n, "CJK advance({w}) 不应小于 ASCII advance({n})");
    }

    /// 空栈（默认 SansSerif）也能测出正 advance；结果应与显式栈稳定一致（缓存复用）。
    #[test]
    fn measure_advances_default_family_and_stability() {
        let (n1, w1) = measure_advances("", 13.0);
        assert!(n1 > 0.0 && w1 > 0.0);
        let (n2, w2) = measure_advances("", 13.0);
        assert_eq!(n1, n2, "同参数重复调用应命中共享 FontSystem 缓存");
        assert_eq!(w1, w2);
    }
}
