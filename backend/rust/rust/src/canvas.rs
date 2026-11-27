//! canvas 原语的 Rust 侧：解析 runtime/canvas 指令协议（Props["canvas-ops"]），
//! 用 tiny-skia 绘制图形。与 Skia 端共用同一 JSON 指令协议，语义一致。
//! 文本不在本协议内——用声明式 <text> 节点叠加，跨后端一致。

use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct CanvasOp {
    pub op: String,
    #[serde(default)]
    pub x: f32,
    #[serde(default)]
    pub y: f32,
    #[serde(default)]
    pub w: f32,
    #[serde(default)]
    pub h: f32,
    #[serde(default)]
    pub cx: f32,
    #[serde(default)]
    pub cy: f32,
    #[serde(default)]
    pub r: f32,
    #[serde(default)]
    pub x1: f32,
    #[serde(default)]
    pub y1: f32,
    #[serde(default)]
    pub x2: f32,
    #[serde(default)]
    pub y2: f32,
    #[serde(default)]
    pub radius: f32,
    #[serde(default)]
    pub d: String,
    #[serde(default)]
    pub fill: String,
    #[serde(default)]
    pub stroke: String,
    #[serde(default, rename = "stroke-width")]
    pub stroke_width: f32,
}

/// 解析指令串；非法/空输入返回空列表（安全降级）。
pub fn decode(s: &str) -> Vec<CanvasOp> {
    if s.is_empty() {
        return Vec::new();
    }
    serde_json::from_str(s).unwrap_or_default()
}

fn color(s: &str) -> Option<tiny_skia::Color> {
    let (r, g, b, a) = crate::style::parse_color_rgba(s)?;
    Some(tiny_skia::Color::from_rgba8(r, g, b, a))
}

fn fill_paint(s: &str) -> Option<tiny_skia::Paint<'static>> {
    let c = color(s)?;
    let mut p = tiny_skia::Paint::default();
    p.set_color(c);
    p.anti_alias = true;
    Some(p)
}

fn stroke_paint(s: &str, width: f32) -> Option<tiny_skia::Paint<'static>> {
    let c = color(s)?;
    if width <= 0.0 {
        return None;
    }
    let mut p = tiny_skia::Paint::default();
    p.set_color(c);
    p.anti_alias = true;
    Some(p)
}

/// 绘制所有 canvas 指令到 pm，坐标加 (ox,oy) 偏移。
pub fn paint(pm: &mut tiny_skia::Pixmap, ops: &[CanvasOp], ox: f32, oy: f32) {
    for op in ops {
        match op.op.as_str() {
            "rect" => paint_rect(pm, op, ox, oy),
            "circle" => paint_circle(pm, op, ox, oy),
            "line" => paint_line(pm, op, ox, oy),
            "path" => paint_path(pm, op, ox, oy),
            _ => {}
        }
    }
}

fn paint_rect(pm: &mut tiny_skia::Pixmap, op: &CanvasOp, ox: f32, oy: f32) {
    if op.w <= 0.0 || op.h <= 0.0 {
        return;
    }
    let path = round_rect(
        ox + op.x,
        oy + op.y,
        op.w,
        op.h,
        op.radius.min(op.w / 2.0).min(op.h / 2.0),
    );
    let Some(path) = path else {
        return;
    };
    if let Some(p) = fill_paint(&op.fill) {
        pm.fill_path(
            &path,
            &p,
            tiny_skia::FillRule::Winding,
            tiny_skia::Transform::identity(),
            None,
        );
    }
    if let Some(p) = stroke_paint(&op.stroke, op.stroke_width) {
        pm.stroke_path(
            &path,
            &p,
            &tiny_skia::Stroke {
                width: op.stroke_width,
                ..Default::default()
            },
            tiny_skia::Transform::identity(),
            None,
        );
    }
}

fn round_rect(x: f32, y: f32, w: f32, h: f32, r: f32) -> Option<tiny_skia::Path> {
    let mut pb = tiny_skia::PathBuilder::new();
    if r > 0.0 {
        pb.move_to(x + r, y);
        pb.line_to(x + w - r, y);
        pb.quad_to(x + w, y, x + w, y + r);
        pb.line_to(x + w, y + h - r);
        pb.quad_to(x + w, y + h, x + w - r, y + h);
        pb.line_to(x + r, y + h);
        pb.quad_to(x, y + h, x, y + h - r);
        pb.line_to(x, y + r);
        pb.quad_to(x, y, x + r, y);
    } else {
        pb.move_to(x, y);
        pb.line_to(x + w, y);
        pb.line_to(x + w, y + h);
        pb.line_to(x, y + h);
    }
    pb.close();
    pb.finish()
}

fn paint_circle(pm: &mut tiny_skia::Pixmap, op: &CanvasOp, ox: f32, oy: f32) {
    if op.r <= 0.0 {
        return;
    }
    // tiny-skia 无 DrawCircle，用两段 quad 近似圆（四点贝塞尔）。
    let (cx, cy, r) = (ox + op.cx, oy + op.cy, op.r);
    let k = 0.5522847498 * r;
    let mut pb = tiny_skia::PathBuilder::new();
    pb.move_to(cx + r, cy);
    pb.cubic_to(cx + r, cy + k, cx + k, cy + r, cx, cy + r);
    pb.cubic_to(cx - k, cy + r, cx - r, cy + k, cx - r, cy);
    pb.cubic_to(cx - r, cy - k, cx - k, cy - r, cx, cy - r);
    pb.cubic_to(cx + k, cy - r, cx + r, cy - k, cx + r, cy);
    pb.close();
    let Some(path) = pb.finish() else {
        return;
    };
    if let Some(p) = fill_paint(&op.fill) {
        pm.fill_path(
            &path,
            &p,
            tiny_skia::FillRule::Winding,
            tiny_skia::Transform::identity(),
            None,
        );
    }
    if let Some(p) = stroke_paint(&op.stroke, op.stroke_width) {
        pm.stroke_path(
            &path,
            &p,
            &tiny_skia::Stroke {
                width: op.stroke_width,
                ..Default::default()
            },
            tiny_skia::Transform::identity(),
            None,
        );
    }
}

fn paint_line(pm: &mut tiny_skia::Pixmap, op: &CanvasOp, ox: f32, oy: f32) {
    let mut pb = tiny_skia::PathBuilder::new();
    pb.move_to(ox + op.x1, oy + op.y1);
    pb.line_to(ox + op.x2, oy + op.y2);
    let Some(path) = pb.finish() else {
        return;
    };
    let width = if op.stroke_width > 0.0 {
        op.stroke_width
    } else {
        1.0
    };
    let mut p = tiny_skia::Paint::default();
    if let Some(c) = color(&op.stroke) {
        p.set_color(c);
    } else {
        return;
    }
    p.anti_alias = true;
    pm.stroke_path(
        &path,
        &p,
        &tiny_skia::Stroke {
            width,
            ..Default::default()
        },
        tiny_skia::Transform::identity(),
        None,
    );
}

fn paint_path(pm: &mut tiny_skia::Pixmap, op: &CanvasOp, ox: f32, oy: f32) {
    let Some(path) = parse_svg_path(&op.d, ox, oy) else {
        return;
    };
    if let Some(p) = fill_paint(&op.fill) {
        pm.fill_path(
            &path,
            &p,
            tiny_skia::FillRule::Winding,
            tiny_skia::Transform::identity(),
            None,
        );
    }
    if let Some(p) = stroke_paint(&op.stroke, op.stroke_width) {
        pm.stroke_path(
            &path,
            &p,
            &tiny_skia::Stroke {
                width: op.stroke_width,
                ..Default::default()
            },
            tiny_skia::Transform::identity(),
            None,
        );
    }
}

/// 解析 SVG path 子集：M/L/H/V/C/Q/Z（绝对坐标），与 Skia 端一致。
fn parse_svg_path(d: &str, ox: f32, oy: f32) -> Option<tiny_skia::Path> {
    let mut pb = tiny_skia::PathBuilder::new();
    // 在命令字母前插入空格，使 "M0 0 L10 10" 变成 "M 0 0 L 10 10"，
    // 让命令与紧随的数字正确分离（SVG path 允许 M0 这种紧凑写法）。
    let mut spaced = String::with_capacity(d.len() * 2);
    for c in d.chars() {
        if matches!(
            c,
            'M' | 'L' | 'H' | 'V' | 'C' | 'Q' | 'Z' | 'm' | 'l' | 'h' | 'v' | 'c' | 'q' | 'z'
        ) {
            spaced.push(' ');
            spaced.push(c);
            spaced.push(' ');
            continue;
        }
        spaced.push(c);
    }
    let toks: Vec<&str> = spaced
        .split(|c: char| c == ',' || c.is_whitespace())
        .filter(|s| !s.is_empty())
        .collect();
    let mut i = 0;
    let num = |s: &str| -> f32 { s.parse().unwrap_or(0.0) };
    let (mut cx, mut cy) = (0.0f32, 0.0f32);
    while i < toks.len() {
        let cmd = toks[i].as_bytes()[0];
        i += 1;
        match cmd {
            b'M' | b'L' => {
                if i + 1 >= toks.len() {
                    return None;
                }
                let (x, y) = (ox + num(toks[i]), oy + num(toks[i + 1]));
                i += 2;
                if cmd == b'M' {
                    pb.move_to(x, y);
                } else {
                    pb.line_to(x, y);
                }
                cx = x;
                cy = y;
            }
            b'H' => {
                if i >= toks.len() {
                    return None;
                }
                cx = ox + num(toks[i]);
                i += 1;
                pb.line_to(cx, cy);
            }
            b'V' => {
                if i >= toks.len() {
                    return None;
                }
                cy = oy + num(toks[i]);
                i += 1;
                pb.line_to(cx, cy);
            }
            b'Q' => {
                if i + 3 >= toks.len() {
                    return None;
                }
                let (x1, y1, x2, y2) = (
                    ox + num(toks[i]),
                    oy + num(toks[i + 1]),
                    ox + num(toks[i + 2]),
                    oy + num(toks[i + 3]),
                );
                i += 4;
                pb.quad_to(x1, y1, x2, y2);
                cx = x2;
                cy = y2;
            }
            b'C' => {
                if i + 5 >= toks.len() {
                    return None;
                }
                let (x1, y1, x2, y2, x3, y3) = (
                    ox + num(toks[i]),
                    oy + num(toks[i + 1]),
                    ox + num(toks[i + 2]),
                    oy + num(toks[i + 3]),
                    ox + num(toks[i + 4]),
                    oy + num(toks[i + 5]),
                );
                i += 6;
                pb.cubic_to(x1, y1, x2, y2, x3, y3);
                cx = x3;
                cy = y3;
            }
            b'Z' | b'z' => pb.close(),
            _ => return None,
        }
    }
    pb.finish()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn decode_valid() {
        let s = r##"[{"op":"rect","x":10,"y":20,"w":30,"h":40,"fill":"#ff0000","radius":5}]"##;
        let ops = decode(s);
        assert_eq!(ops.len(), 1);
        assert_eq!(ops[0].op, "rect");
        assert_eq!(ops[0].radius, 5.0);
    }

    #[test]
    fn decode_invalid() {
        assert!(decode("").is_empty());
        assert!(decode("not json").is_empty());
    }

    #[test]
    fn parse_path_basic() {
        // 解析不应 panic；tiny-skia 对退化/极小 path 的 finish() 可能返回 None，
        // 调用方按 Option 安全跳过绘制即可（见 paint_path）。
        let _ = parse_svg_path("M0 0 L100 0 L100 100 L0 100 Z", 0.0, 0.0);
        let _ = parse_svg_path("M0 0 L10 10 L20 0 Z", 0.0, 0.0);
        let _ = parse_svg_path("bad", 0.0, 0.0);
        // 正常矩形应产生有效 path
        let rect = parse_svg_path("M0 0 L100 0 L100 100 L0 100 Z", 10.0, 20.0);
        assert!(rect.is_some(), "正常矩形 path 应有效");
    }

    #[test]
    fn parse_icon_vector_path() {
        // 来自 runtime/icon 的真实图标 path（heart，24×24，含 C 曲线）。
        // 验证后端矢量解析器能直接吃下图标组件生成的绝对路径。
        let d = "M12 21 C12 21 3 15 3 9 C3 6 5 4 8 4 C10 4 11.5 5.5 12 6.5 C12.5 5.5 14 4 16 4 C19 4 21 6 21 9 C21 15 12 21 12 21 Z";
        let path = parse_svg_path(d, 0.0, 0.0);
        assert!(path.is_some(), "图标矢量 path 应解析有效");

        let mut pm = tiny_skia::Pixmap::new(24, 24).unwrap();
        let ops = decode(&format!(
            r##"[{{"op":"path","d":"{}","fill":"#ff0000"}}]"##,
            d
        ));
        paint(&mut pm, &ops, 0.0, 0.0);
        assert!(
            pm.data().iter().any(|&x| x != 0),
            "图标 path 应画出非透明像素"
        );
    }

    #[test]
    fn paint_all_ops_no_panic() {
        let mut pm = tiny_skia::Pixmap::new(100, 100).unwrap();
        let ops = decode(
            r##"[
            {"op":"rect","x":1,"y":1,"w":20,"h":10,"fill":"#f00","stroke":"#000","stroke-width":1,"radius":3},
            {"op":"circle","cx":50,"cy":50,"r":20,"fill":"blue"},
            {"op":"line","x1":0,"y1":0,"x2":99,"y2":99,"stroke":"#000","stroke-width":2},
            {"op":"path","d":"M0 0 L20 20 L40 0 Z","fill":"green"}
        ]"##,
        );
        paint(&mut pm, &ops, 0.0, 0.0);
    }
}
