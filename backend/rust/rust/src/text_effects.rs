//! 与 Go `text/effects` 包对齐的统一文本效果语义模型。
//!
//! 分层原则（与 Go 侧完全一致）：
//!   - Scene 只保存声明式样式（CSS 属性字符串），后端把样式解析成这里的 TextEffects；
//!   - Text Effects 与 Text Layout 分离：改颜色 / 阴影 / 渐变不触发重新布局；
//!   - 颜色用 CSS 字符串或 RGBA 元组表示，不暴露 wgpu / glyphon / tiny-skia 对象；
//!   - 后端按自身能力选择实现路径（glyphon 富文本 span / tiny-skia shader / Web CSS）。
//!
//! 本模块是 Rust 后端消费统一语义的入口。它让三端不再各写各的解析：
//! Go 侧 text/effects.Parse 与这里 Parse 输入输出语义一致，Web 走同一份 CSS
//! 数据源（lineCodeGradient），Rust 侧则在此把 CSS 字符串解析成结构化效果。

use std::collections::HashMap;

/// CSS 颜色解析为 (r,g,b,a)。支持 #rgb/#rrggbb/#rrggbbaa/rgb()/rgba()/命名色。
/// 语义与 Go text/effects.Color、Rust style::parse_color_rgba 对齐。
pub fn parse_color_rgba(s: &str) -> Option<(u8, u8, u8, u8)> {
    let s = s.trim().to_ascii_lowercase();
    if let Some(h) = s.strip_prefix('#') {
        let expand = |x: &str| -> String {
            if x.len() == 3 {
                x.chars().map(|c| format!("{}{}", c, c)).collect()
            } else {
                x.to_string()
            }
        };
        let h = expand(h);
        if h.len() == 6 {
            return Some((
                u8::from_str_radix(&h[0..2], 16).ok()?,
                u8::from_str_radix(&h[2..4], 16).ok()?,
                u8::from_str_radix(&h[4..6], 16).ok()?,
                255,
            ));
        }
        if h.len() == 8 {
            return Some((
                u8::from_str_radix(&h[0..2], 16).ok()?,
                u8::from_str_radix(&h[2..4], 16).ok()?,
                u8::from_str_radix(&h[4..6], 16).ok()?,
                u8::from_str_radix(&h[6..8], 16).ok()?,
            ));
        }
    }
    if s.starts_with("rgba(") && s.ends_with(')') {
        let p: Vec<&str> = s[5..s.len() - 1].split(',').collect();
        if p.len() == 4 {
            let r: f32 = p[0].trim().parse().ok()?;
            let g: f32 = p[1].trim().parse().ok()?;
            let b: f32 = p[2].trim().parse().ok()?;
            let a: f32 = p[3].trim().parse().ok()?;
            let av = if a > 1.0 { a as u8 } else { (a * 255.0) as u8 };
            return Some((r as u8, g as u8, b as u8, av));
        }
    }
    if s.starts_with("rgb(") && s.ends_with(')') {
        let p: Vec<&str> = s[4..s.len() - 1].split(',').collect();
        if p.len() == 3 {
            return Some((
                p[0].trim().parse().ok()?,
                p[1].trim().parse().ok()?,
                p[2].trim().parse().ok()?,
                255,
            ));
        }
    }
    Some(match s.as_str() {
        "white" => (255, 255, 255, 255),
        "black" => (0, 0, 0, 255),
        "red" => (220, 40, 40, 255),
        "green" => (40, 180, 60, 255),
        "blue" => (40, 90, 220, 255),
        "gray" | "grey" => (128, 128, 128, 255),
        "yellow" => (230, 200, 40, 255),
        "transparent" => (0, 0, 0, 0),
        _ => return None,
    })
}

/// 一个渐变色段（对应 Go `gradient.go` 的 gradStop）。
/// start_ch/end_ch 是 CSS `ch` 单位边界，用于编辑器按字符精确分段着色。
#[derive(Clone, Debug, PartialEq)]
pub struct GradStop {
    pub color: (u8, u8, u8, u8),
    pub start_ch: i32,
    pub end_ch: i32,
    /// 无单位百分比色标（如 `45%`）；None 表示只有 ch 边界（编辑器分段编码）。
    pub offset: Option<f32>,
}

/// 解析后的线性渐变。angle 为 CSS 角度（0=向上，90=向右，180=向下）。
#[derive(Clone, Debug, PartialEq)]
pub struct LinearGradient {
    pub angle: f32,
    pub stops: Vec<GradStop>,
}

/// 单个 rune 占多少 `ch`。必须与 Go `gradientCells`（backend/skia/gradient.go）
/// 和编辑器 `runeWidth` 完全一致，否则色段边界与文本 rune 错位。
pub fn gradient_cells(r: char) -> i32 {
    if (r as u32) >= 0x2e80 {
        2
    } else {
        1
    }
}

/// 按顶层分隔符切分，忽略括号内的分隔符（rgba(...) 与渐变串本身含逗号）。
fn split_top_level(s: &str, sep: char) -> Vec<String> {
    let mut out = Vec::new();
    let mut depth = 0i32;
    let mut quote: Option<char> = None;
    let mut start = 0usize;
    for (i, c) in s.char_indices() {
        if let Some(q) = quote {
            if c == q {
                quote = None;
            }
            continue;
        }
        match c {
            '\'' | '"' => quote = Some(c),
            '(' | '[' => depth += 1,
            ')' | ']' => depth -= 1,
            _ if c == sep && depth == 0 => {
                out.push(s[start..i].to_string());
                start = i + c.len_utf8();
            }
            _ => {}
        }
    }
    out.push(s[start..].to_string());
    out
}

fn parse_f32(s: &str) -> Option<f32> {
    s.trim().parse::<f32>().ok()
}

/// 解析 CSS 角度（0=向上，90=向右，180=向下）。与 Go parseGradientAngle 对齐。
fn parse_angle(s: &str) -> Option<f32> {
    let s = s.trim().to_ascii_lowercase();
    if let Some(v) = s.strip_suffix("deg") {
        return parse_f32(v);
    }
    if let Some(v) = s.strip_suffix("turn") {
        return parse_f32(v).map(|x| x * 360.0);
    }
    if let Some(rest) = s.strip_prefix("to ") {
        let mut angle = 0.0f32;
        for d in rest.split_whitespace() {
            angle = match d {
                "top" => 0.0,
                "right" => 90.0,
                "bottom" => 180.0,
                "left" => 270.0,
                _ => angle,
            };
        }
        return Some(angle);
    }
    None
}

/// 解析单个色标位置：`12ch` → (Some(12), None, consumed)；
/// `45%` → (None, Some(0.45), consumed)；`0.3` → (None, Some(0.3), consumed)。
fn parse_pos(s: &str) -> (Option<i32>, Option<f32>, bool) {
    let s = s.trim().to_ascii_lowercase();
    if let Some(v) = s.strip_suffix("ch") {
        return (parse_f32(v).map(|x| x as i32), None, true);
    }
    if let Some(v) = s.strip_suffix('%') {
        return (None, parse_f32(v).map(|x| x / 100.0), true);
    }
    if let Some(v) = parse_f32(&s) {
        // 容错：>1 当作百分比（与 Go parseOffset 对齐）。
        return (None, Some(if v > 1.0 { v / 100.0 } else { v }), true);
    }
    (None, None, false)
}

/// 解析 `linear-gradient(...)` 字符串为结构化 LinearGradient。
/// 同时支持编辑器分段编码（`linear-gradient(90deg, #c586c0 0ch 7ch, #d4d4d4 7ch 8ch)`）
/// 与通用 CSS 渐变（`linear-gradient(90deg, #f00 0%, #00f 100%)`）。
pub fn parse_linear_gradient(s: &str) -> Option<LinearGradient> {
    let s = s.trim();
    let low = s.to_ascii_lowercase();
    let prefix = "linear-gradient(";
    if !low.starts_with(prefix) || !s.ends_with(')') {
        return None;
    }
    let inner = &s[prefix.len()..s.len() - 1];
    let mut parts: Vec<String> = split_top_level(inner, ',')
        .into_iter()
        .map(|x| x.trim().to_string())
        .filter(|x| !x.is_empty())
        .collect();
    if parts.is_empty() {
        return None;
    }

    let mut angle = 180.0f32; // CSS 默认：从上到下
    if let Some(a) = parse_angle(&parts[0]) {
        angle = a;
        parts.remove(0);
    }

    let mut stops = Vec::new();
    let mut prev_end_ch = 0i32;
    let mut prev_offset = 0.0f32;
    for part in &parts {
        let fields: Vec<&str> = part.split_whitespace().collect();
        if fields.is_empty() {
            continue;
        }
        // 第一个字段必须是颜色（跳过形状/方向残留）。
        let color = match parse_color_rgba(fields[0]) {
            Some(c) => c,
            None => continue,
        };

        // 收集本段的位置字段。支持三种 CSS 写法：
        //   `#c 0ch 7ch`      → 双 ch 边界（编辑器 buildGradient）
        //   `#c 45%`          → 单百分比 offset
        //   `#c`              → 无位置（顺序均分）
        let mut chs: Vec<i32> = Vec::new();
        let mut off: Option<f32> = None;
        for f in fields.iter().skip(1) {
            let (ch, o, ok) = parse_pos(f);
            if !ok {
                continue;
            }
            if let Some(c) = ch {
                chs.push(c);
            }
            if let Some(x) = o {
                off = Some(x);
            }
        }

        let (start_ch, end_ch) = if chs.len() >= 2 {
            (chs[0], chs[1])
        } else if chs.len() == 1 {
            (prev_end_ch, chs[0])
        } else {
            (prev_end_ch, prev_end_ch)
        };
        prev_end_ch = end_ch;
        prev_offset = off.unwrap_or(prev_offset);
        stops.push(GradStop {
            color,
            start_ch,
            end_ch,
            offset: off,
        });
    }

    let _ = prev_offset; // 顺序均分的占位推进，保留以供将来无位置色标插值
    if stops.is_empty() {
        None
    } else {
        Some(LinearGradient { angle, stops })
    }
}

/// 文字填充：渐变文字把 gradient 作为填充来源；纯色覆盖走 color。
#[derive(Clone, Debug, PartialEq)]
pub enum TextFill {
    None,
    Solid((u8, u8, u8, u8)),
    Gradient(LinearGradient),
}

impl Default for TextFill {
    fn default() -> Self {
        TextFill::None
    }
}

/// 与 Go TextEffects 对齐的统一语义结构。零值表示无效果，
/// 后端走普通文字绘制路径、不分配离屏层。
#[derive(Clone, Debug, Default, PartialEq)]
pub struct TextEffects {
    pub fill: TextFill,
}

impl TextEffects {
    pub fn active(&self) -> bool {
        !matches!(self.fill, TextFill::None)
    }

    /// 从节点 Props 解析统一文本效果。语义与 Go `effects.Parse` 对齐：
    /// 只有 `gradient-text`（或 background-clip 含 text）时，bg 渐变才作用于文字；
    /// 否则渐变是背景，不裁剪到文字。
    pub fn from_props(props: &HashMap<String, String>) -> Self {
        let gradient_text = props
            .get("gradient-text")
            .map(|v| v.trim() == "1" || v.trim().eq_ignore_ascii_case("true"))
            .unwrap_or(false);
        let clip_text = props
            .get("background-clip")
            .or_else(|| props.get("-webkit-background-clip"))
            .map(|v| v.to_ascii_lowercase().contains("text"))
            .unwrap_or(false);

        if gradient_text || clip_text {
            let image = props
                .get("bg")
                .or_else(|| props.get("background"))
                .or_else(|| props.get("background-image"));
            if let Some(g) = image.and_then(|s| parse_linear_gradient(s)) {
                return Self {
                    fill: TextFill::Gradient(g),
                };
            }
        }
        Self {
            fill: TextFill::None,
        }
    }
}

/// 按渐变的 ch 边界把纯文本切成着色分段。语义与 Go `gradientToRich` 完全一致：
/// 相邻同色 rune 合并成一段，减少 glyphon TextArea 数量。
///
/// 返回 (分段文本, RGBA) 列表。未落入任何色段的字符用 fallback 颜色。
pub fn gradient_spans(
    text: &str,
    grad: &LinearGradient,
    fallback: (u8, u8, u8),
) -> Vec<(String, (u8, u8, u8))> {
    let mut out: Vec<(String, (u8, u8, u8))> = Vec::new();
    let mut ch = 0i32;
    let mut si = 0usize;
    for r in text.chars() {
        while si < grad.stops.len() && ch >= grad.stops[si].end_ch {
            si += 1;
        }
        let color =
            if si < grad.stops.len() && ch >= grad.stops[si].start_ch && ch < grad.stops[si].end_ch
            {
                let c = grad.stops[si].color;
                (c.0, c.1, c.2)
            } else {
                fallback
            };
        if let Some(last) = out.last_mut() {
            if last.1 == color {
                last.0.push(r);
                ch += gradient_cells(r);
                continue;
            }
        }
        out.push((r.to_string(), color));
        ch += gradient_cells(r);
    }
    out
}

/// 在已有着色分段（渐变）基础上按 rune 区间覆盖选区前景色。
/// base 为空时所有字符用 fallback 色；[start,end) 内的 rune 用 sel_color。
/// 合并相邻同色字符为段，供 glyphon 逐 span 上色。
pub fn selection_spans(
    text: &str,
    base: &[(String, (u8, u8, u8))],
    fallback: (u8, u8, u8),
    start: usize,
    end: usize,
    sel_color: (u8, u8, u8),
) -> Vec<(String, (u8, u8, u8))> {
    // 先把 base 展开成逐 char 颜色表（rune 顺序）。
    let mut base_colors: Vec<(u8, u8, u8)> = Vec::new();
    if base.is_empty() {
        for _ in text.chars() {
            base_colors.push(fallback);
        }
    } else {
        for seg in base {
            for _ in seg.0.chars() {
                base_colors.push(seg.1);
            }
        }
        // 长度不匹配（理论上不会）时用 fallback 补齐。
        let need = text.chars().count();
        while base_colors.len() < need {
            base_colors.push(fallback);
        }
    }
    let mut out: Vec<(String, (u8, u8, u8))> = Vec::new();
    for (i, r) in text.chars().enumerate() {
        let color = if i >= start && i < end {
            sel_color
        } else {
            base_colors[i]
        };
        if let Some(last) = out.last_mut() {
            if last.1 == color {
                last.0.push(r);
                continue;
            }
        }
        out.push((r.to_string(), color));
    }
    out
}

#[cfg(test)]
mod selection_tests {
    use super::*;

    #[test]
    fn selection_spans_overrides_range() {
        let base = vec![
            ("ab".into(), (1, 1, 1)),
            ("cd".into(), (2, 2, 2)),
            ("ef".into(), (3, 3, 3)),
        ];
        let out = selection_spans("abcdef", &base, (9, 9, 9), 2, 4, (255, 255, 255));
        assert_eq!(
            out,
            vec![
                ("ab".into(), (1, 1, 1)),
                ("cd".into(), (255, 255, 255)),
                ("ef".into(), (3, 3, 3)),
            ]
        );
    }

    #[test]
    fn selection_spans_without_base_uses_fallback() {
        let out = selection_spans("abcd", &[], (7, 7, 7), 1, 2, (255, 0, 0));
        assert_eq!(
            out,
            vec![
                ("a".into(), (7, 7, 7)),
                ("b".into(), (255, 0, 0)),
                ("cd".into(), (7, 7, 7)),
            ]
        );
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_editor_gradient_stops() {
        let g = parse_linear_gradient(
            "linear-gradient(90deg, #c586c0 0ch 7ch, #d4d4d4 7ch 8ch, #ce9178 8ch 12ch)",
        )
        .unwrap();
        assert_eq!(g.angle, 90.0);
        assert_eq!(g.stops.len(), 3);
        assert_eq!(g.stops[0].start_ch, 0);
        assert_eq!(g.stops[0].end_ch, 7);
        assert_eq!(g.stops[0].color, (197, 134, 192, 255));
        assert_eq!(g.stops[1].start_ch, 7);
        assert_eq!(g.stops[1].end_ch, 8);
        assert_eq!(g.stops[2].start_ch, 8);
        assert_eq!(g.stops[2].end_ch, 12);
    }

    #[test]
    fn parse_generic_gradient_offsets() {
        let g = parse_linear_gradient("linear-gradient(180deg, #f00 0%, #00f 100%)").unwrap();
        assert_eq!(g.angle, 180.0);
        assert_eq!(g.stops.len(), 2);
        assert_eq!(g.stops[0].offset, Some(0.0));
        assert_eq!(g.stops[1].offset, Some(1.0));
    }

    #[test]
    fn default_angle_is_180() {
        let g = parse_linear_gradient("linear-gradient(#f00, #00f)").unwrap();
        assert_eq!(g.angle, 180.0);
    }

    #[test]
    fn gradient_spans_merges_adjacent_same_color() {
        let g = parse_linear_gradient("linear-gradient(90deg, #c586c0 0ch 3ch, #d4d4d4 3ch 6ch)")
            .unwrap();
        // f/u/n 在 [0,3) 紫色；c/空格/x 在 [3,6) 灰色并合并为一段。
        let spans = gradient_spans("func x", &g, (212, 212, 212));
        assert_eq!(
            spans,
            vec![
                ("fun".to_string(), (197, 134, 192)),
                ("c x".to_string(), (212, 212, 212)),
            ]
        );
    }

    #[test]
    fn gradient_spans_respects_cjk_double_ch() {
        let g = parse_linear_gradient("linear-gradient(90deg, #ff0000 0ch 2ch, #0000ff 2ch 4ch)")
            .unwrap();
        // 中文各占 2ch：第一个字落在 [0,2) 红，第二个落在 [2,4) 蓝。
        let spans = gradient_spans("中文", &g, (0, 0, 0));
        assert_eq!(
            spans,
            vec![
                ("中".to_string(), (255, 0, 0)),
                ("文".to_string(), (0, 0, 255)),
            ]
        );
    }

    #[test]
    fn from_props_requires_gradient_text_marker() {
        let mut p = HashMap::new();
        p.insert("bg".into(), "linear-gradient(90deg, #f00 0ch 2ch)".into());
        // 无标记：渐变是背景，不作用于文字。
        assert!(!TextEffects::from_props(&p).active());
        // 有标记：渐变成为文字填充。
        p.insert("gradient-text".into(), "1".into());
        assert!(TextEffects::from_props(&p).active());
    }

    #[test]
    fn from_props_accepts_background_clip_text() {
        let mut p = HashMap::new();
        p.insert("bg".into(), "linear-gradient(90deg, #f00 0ch 2ch)".into());
        p.insert("-webkit-background-clip".into(), "text".into());
        assert!(TextEffects::from_props(&p).active());
    }
}
