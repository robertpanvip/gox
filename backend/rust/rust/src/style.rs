//! 样式解析与动画插值。语义与 Skia 后端 backend/skia/style.go 完全对齐：
//! host 只透传 Props 字符串，后端在此解释 position/overflow/transform/transition/shadow。
//! 所有解析失败静默回退默认值，后端永不因脏 props 崩溃。

use crate::scene::SceneNode;
use std::collections::HashMap;

#[derive(Clone, Copy, Debug, PartialEq)]
pub enum Position { Relative, Absolute }

#[derive(Clone, Copy, Debug, PartialEq)]
pub enum Overflow { Visible, Hidden, Scroll, Auto }

#[derive(Clone, Copy, Debug, PartialEq)]
pub enum Easing { Linear, Ease, EaseIn, EaseOut, EaseInOut }

#[derive(Clone, Debug)]
pub struct BoxShadow {
    pub offset_x: f32, pub offset_y: f32,
    pub blur: f32, pub spread: f32,
    pub color: (u8, u8, u8, u8), // ARGB alpha
}

#[derive(Clone, Copy, Debug)]
pub struct Transform {
    pub translate_x: f32, pub translate_y: f32, pub scale: f32,
}
impl Default for Transform {
    fn default() -> Self { Self { translate_x: 0.0, translate_y: 0.0, scale: 1.0 } }
}

#[derive(Clone, Debug)]
pub struct TransitionItem {
    pub property: String,
    pub duration: f32, // ms
    pub delay: f32,    // ms
    pub easing: Easing,
}

/// 一帧解析出的样式。只保留与新增能力相关的字段；flex 仍由 Taffy 直接读 props。
#[derive(Clone, Debug)]
pub struct Style {
    pub opacity: f32,
    pub position: Position,
    pub inset: [f32; 4], // top,right,bottom,left；NaN = auto
    pub overflow: Overflow,
    pub scroll_top: f32,
    pub scroll_left: f32,
    pub transform: Transform,
    pub shadow: Option<BoxShadow>,
    pub transition: Vec<TransitionItem>,
}
impl Default for Style {
    fn default() -> Self {
        Self {
            opacity: 1.0, position: Position::Relative,
            inset: [f32::NAN; 4], overflow: Overflow::Visible,
            scroll_top: 0.0, scroll_left: 0.0,
            transform: Transform::default(), shadow: None, transition: vec![],
        }
    }
}

pub fn parse_px(s: &str) -> Option<f32> {
    s.trim().trim_end_matches("px").trim().parse::<f32>().ok()
}

fn prop<'a>(n: &'a SceneNode, k: &str) -> Option<&'a str> {
    n.props.get(k).map(|s| s.as_str())
}

fn num(v: &str) -> Option<f32> { parse_px(v) }

pub fn parse_style(n: &SceneNode) -> Style {
    let mut s = Style::default();
    if let Some(v) = prop(n, "opacity").and_then(num) {
        s.opacity = v.clamp(0.0, 1.0);
    }
    if let Some(v) = prop(n, "position") {
        if v.trim().eq_ignore_ascii_case("absolute") { s.position = Position::Absolute; }
    }
    for (i, k) in ["top", "right", "bottom", "left"].iter().enumerate() {
        if let Some(v) = prop(n, k) {
            if v.trim().eq_ignore_ascii_case("auto") { s.inset[i] = f32::NAN; }
            else if let Some(x) = num(v) { s.inset[i] = x; }
        }
    }
    if let Some(v) = prop(n, "overflow") {
        s.overflow = match v.trim().to_ascii_lowercase().as_str() {
            "hidden" => Overflow::Hidden, "scroll" => Overflow::Scroll,
            "auto" => Overflow::Auto, _ => Overflow::Visible,
        };
    }
    if let Some(v) = prop(n, "scroll-top").and_then(num) { s.scroll_top = v; }
    if let Some(v) = prop(n, "scroll-left").and_then(num) { s.scroll_left = v; }
    if let Some(v) = prop(n, "transform") { s.transform = parse_transform(v); }
    if let Some(v) = prop(n, "box-shadow") {
        if !v.trim().is_empty() && !v.trim().eq_ignore_ascii_case("none") {
            s.shadow = parse_box_shadow(v);
        }
    }
    if let Some(v) = prop(n, "transition") { s.transition = parse_transition(v); }
    s
}

pub fn parse_transform(s: &str) -> Transform {
    let mut t = Transform::default();
    let low = s.to_ascii_lowercase();
    if let Some(i) = low.find("translate(") {
        if let Some(j) = s[i..].find(')') {
            let inner = &s[i + "translate(".len()..i + j];
            let parts: Vec<&str> = inner.split(',').collect();
            if let Some(v) = parts.get(0).and_then(|x| num(x)) { t.translate_x = v; }
            if let Some(v) = parts.get(1).and_then(|x| num(x)) { t.translate_y = v; }
        }
    }
    if let Some(i) = low.find("translatex(") {
        if let Some(j) = s[i..].find(')') {
            if let Some(v) = num(&s[i + "translatex(".len()..i + j]) { t.translate_x = v; }
        }
    }
    if let Some(i) = low.find("translatey(") {
        if let Some(j) = s[i..].find(')') {
            if let Some(v) = num(&s[i + "translatey(".len()..i + j]) { t.translate_y = v; }
        }
    }
    if let Some(i) = low.find("scale(") {
        if let Some(j) = s[i..].find(')') {
            if let Some(v) = num(&s[i + "scale(".len()..i + j]) { if v > 0.0 { t.scale = v; } }
        }
    }
    t
}

/// 解析 "offsetX offsetY blur spread color"；spread/color 可省略。
pub fn parse_box_shadow(s: &str) -> Option<BoxShadow> {
    let fields: Vec<&str> = s.split_whitespace().collect();
    if fields.len() < 2 { return None; }
    let mut nums = Vec::new();
    let mut color = (0u8, 0u8, 0u8, 90u8);
    for f in &fields {
        if let Some(c) = parse_color_rgba(f) { color = c; continue; }
        if let Some(v) = num(f) { nums.push(v); } else { break; }
    }
    if nums.len() < 2 { return None; }
    Some(BoxShadow {
        offset_x: nums[0], offset_y: nums[1],
        blur: *nums.get(2).unwrap_or(&0.0), spread: *nums.get(3).unwrap_or(&0.0),
        color,
    })
}

/// 解析颜色，返回 (r,g,b,a)。支持 #rgb/#rrggbb/#rrggbbaa/rgb()/rgba()/命名色。
pub fn parse_color_rgba(s: &str) -> Option<(u8, u8, u8, u8)> {
    let s = s.trim().to_ascii_lowercase();
    if let Some(h) = s.strip_prefix('#') {
        let expand = |x: &str| -> String {
            if x.len() == 3 { x.chars().map(|c| format!("{}{}", c, c)).collect() } else { x.to_string() }
        };
        let h = expand(h);
        if h.len() == 6 {
            return Some((
                u8::from_str_radix(&h[0..2], 16).ok()?,
                u8::from_str_radix(&h[2..4], 16).ok()?,
                u8::from_str_radix(&h[4..6], 16).ok()?, 255,
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
        let p: Vec<&str> = s[5..s.len()-1].split(',').collect();
        if p.len() == 4 {
            let r = p[0].trim().parse().ok()?;
            let g = p[1].trim().parse().ok()?;
            let b = p[2].trim().parse().ok()?;
            let a: f32 = p[3].trim().parse().ok()?;
            let av = if a > 1.0 { a as u8 } else { (a * 255.0) as u8 };
            return Some((r, g, b, av));
        }
    }
    if s.starts_with("rgb(") && s.ends_with(')') {
        let p: Vec<&str> = s[4..s.len()-1].split(',').collect();
        if p.len() == 3 {
            return Some((
                p[0].trim().parse().ok()?, p[1].trim().parse().ok()?,
                p[2].trim().parse().ok()?, 255,
            ));
        }
    }
    Some(match s.as_str() {
        "white" => (255, 255, 255, 255), "black" => (0, 0, 0, 255),
        "red" => (220, 40, 40, 255), "green" => (40, 180, 60, 255),
        "blue" => (40, 90, 220, 255), "gray" | "grey" => (128, 128, 128, 255),
        "yellow" => (230, 200, 40, 255),
        _ => return None,
    })
}

pub fn parse_transition(s: &str) -> Vec<TransitionItem> {
    let mut out = Vec::new();
    for seg in s.split(',') {
        let seg = seg.trim();
        if seg.is_empty() || seg.eq_ignore_ascii_case("none") { continue; }
        let mut it = TransitionItem { property: String::new(), duration: 0.0, delay: 0.0, easing: Easing::Ease };
        for tok in seg.split_whitespace() {
            let t = tok.to_ascii_lowercase();
            if let Some(x) = t.strip_suffix("ms") {
                if let Ok(v) = x.parse() { it.duration = v; }
            } else if let Some(x) = t.strip_suffix('s') {
                if let Ok(v) = x.parse::<f32>() { it.duration = v * 1000.0; }
            } else {
                match t.as_str() {
                    "linear" => it.easing = Easing::Linear,
                    "ease-in" => it.easing = Easing::EaseIn,
                    "ease-out" => it.easing = Easing::EaseOut,
                    "ease-in-out" => it.easing = Easing::EaseInOut,
                    "ease" => it.easing = Easing::Ease,
                    _ => { if it.property.is_empty() { it.property = t.clone(); } }
                }
            }
        }
        if !it.property.is_empty() && it.duration > 0.0 { out.push(it); }
    }
    out
}

pub fn easing_value(e: Easing, t: f32) -> f32 {
    let t = t.clamp(0.0, 1.0);
    match e {
        Easing::Linear => t,
        Easing::EaseIn => t * t * t,
        Easing::EaseOut => 1.0 - (1.0 - t).powi(3),
        Easing::EaseInOut => {
            if t < 0.5 { 4.0 * t * t * t } else { 1.0 - (-2.0 * t + 2.0).powi(3) / 2.0 }
        }
        Easing::Ease => t * t * (3.0 - 2.0 * t),
    }
}

// ---------- transition 插值引擎 ----------

/// 正在进行的单属性动画。
#[derive(Clone, Debug)]
struct Anim {
    from: f32,
    to: f32,
    start_ms: u128,
    duration: f32,
    easing: Easing,
}

/// 每个节点每个属性的动画状态。key = (node_id, property)。
type AnimMap = HashMap<(String, String), Anim>;

/// 动画引擎：渲染时用「目标 props vs 上次目标」检测变化并推进插值，
/// 返回一棵 props 被覆盖成当前插值值的临时场景树供本帧布局/绘制。
/// 覆盖是浅拷贝（只克隆变化节点的 props），不污染常驻镜像树。
pub struct Animator {
    anims: AnimMap,
    targets: HashMap<(String, String), f32>,
    now_ms: u128,
}

impl Animator {
    pub fn new() -> Self {
        Self { anims: AnimMap::new(), targets: HashMap::new(), now_ms: 0 }
    }

    pub fn set_now(&mut self, now_ms: u128) { self.now_ms = now_ms; }

    /// 是否仍有进行中的动画。无变化帧可据此决定是否需要继续重绘。
    pub fn is_active(&self) -> bool { !self.anims.is_empty() }

    /// 返回当前插值值（若动画已结束则返回 to 并移除动画）。
    fn sample(&mut self, key: &(String, String)) -> Option<f32> {
        let now = self.now_ms;
        let a = self.anims.get(key)?;
        let elapsed = now.saturating_sub(a.start_ms) as f32;
        if elapsed >= a.duration {
            let to = a.to;
            self.anims.remove(key);
            Some(to)
        } else {
            let t = easing_value(a.easing, elapsed / a.duration);
            Some(a.from + (a.to - a.from) * t)
        }
    }

    /// 对整棵树应用动画覆盖，返回本帧要用的树副本。
    pub fn apply(&mut self, root: &SceneNode) -> SceneNode {
        let mut out = root.clone();
        self.apply_node(root, &mut out);
        out
    }

    fn apply_node(&mut self, src: &SceneNode, dst: &mut SceneNode) {
        // 收集该节点的 transition 声明（属性名 -> duration/easing）。
        let trs = parse_transition(src.props.get("transition").map(|s| s.as_str()).unwrap_or(""));
        if !trs.is_empty() {
            for tr in &trs {
                if tr.property == "all" { continue; } // all 需要枚举属性，留作后续
                let key = (src.id.clone(), tr.property.clone());
                let target = src.props.get(&tr.property).and_then(|s| parse_px(s));
                // 颜色属性用特殊通道，这里只处理数值属性。
                if let Some(target) = target {
                    let prev = self.targets.get(&key).copied();
                    if prev != Some(target) {
                        // 目标变化：从「当前插值值」起新动画，平滑衔接。
                        let from = self.sample(&key).or(prev).unwrap_or(target);
                        self.anims.insert(key.clone(), Anim {
                            from, to: target, start_ms: self.now_ms,
                            duration: tr.duration, easing: tr.easing,
                        });
                        self.targets.insert(key.clone(), target);
                    }
                    if let Some(v) = self.sample(&key) {
                        dst.props.insert(tr.property.clone(), format!("{:.3}", v));
                    }
                }
            }
        }
        for (i, c) in src.children.iter().enumerate() {
            self.apply_node(c, &mut dst.children[i]);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::binprotocol::NodeType;

    fn node(props: &[(&str, &str)]) -> SceneNode {
        let mut p = HashMap::new();
        for (k, v) in props { p.insert(k.to_string(), v.to_string()); }
        SceneNode {
            typ: NodeType::Div, kind: "div".into(), id: "n1".into(), text: String::new(),
            props: p, events: vec![], children: vec![],
        }
    }

    #[test]
    fn parse_position_inset_and_overflow() {
        let n = node(&[("position", "absolute"), ("top", "10"), ("left", "20px"),
                       ("right", "auto"), ("overflow", "scroll"), ("scroll-top", "42")]);
        let s = parse_style(&n);
        assert_eq!(s.position, Position::Absolute);
        assert_eq!(s.inset[0], 10.0);
        assert_eq!(s.inset[3], 20.0);
        assert!(s.inset[1].is_nan());
        assert_eq!(s.overflow, Overflow::Scroll);
        assert_eq!(s.scroll_top, 42.0);
    }

    #[test]
    fn parse_transform_shadow_transition() {
        let n = node(&[("transform", "translate(10px, -4px) scale(1.5)"),
                       ("box-shadow", "0 4px 12px 2px rgba(0,0,0,0.35)"),
                       ("transition", "opacity 300ms ease-in-out")]);
        let s = parse_style(&n);
        assert_eq!(s.transform.translate_x, 10.0);
        assert_eq!(s.transform.translate_y, -4.0);
        assert_eq!(s.transform.scale, 1.5);
        let sh = s.shadow.unwrap();
        assert_eq!(sh.offset_y, 4.0);
        assert_eq!(sh.blur, 12.0);
        assert_eq!(sh.spread, 2.0);
        assert_eq!(s.transition.len(), 1);
        assert_eq!(s.transition[0].duration, 300.0);
        assert_eq!(s.transition[0].easing, Easing::EaseInOut);
    }

    #[test]
    fn easing_curve() {
        assert_eq!(easing_value(Easing::Linear, 0.5), 0.5);
        assert!(easing_value(Easing::EaseIn, 0.5) < 0.2);
        assert!(easing_value(Easing::EaseOut, 0.5) > 0.8);
        assert!((easing_value(Easing::EaseInOut, 0.5) - 0.5).abs() < 0.01);
    }

    #[test]
    fn animator_interpolates_numeric_prop() {
        let mut a = Animator::new();
        let mut n = node(&[("transition", "width 200ms linear"), ("width", "100")]);
        // 首帧：目标 100，无动画历史 → 直接 100
        a.set_now(0);
        let out = a.apply(&n);
        assert_eq!(out.props["width"], "100.000");
        // 目标改为 200 → 启动动画；t=0 → 起点 100
        n.props.insert("width".into(), "200".into());
        a.set_now(1000);
        let out = a.apply(&n);
        let v: f32 = out.props["width"].parse().unwrap();
        assert!((v - 100.0).abs() < 1.0, "t=0 应≈100, got {}", v);
        // t=100ms（中点）→ 150
        a.set_now(1100);
        let out = a.apply(&n);
        let v: f32 = out.props["width"].parse().unwrap();
        assert!((v - 150.0).abs() < 2.0, "中点应≈150, got {}", v);
        // t=200ms（结束）→ 200
        a.set_now(1200);
        let out = a.apply(&n);
        let v: f32 = out.props["width"].parse().unwrap();
        assert!((v - 200.0).abs() < 0.1, "结束应=200, got {}", v);
    }

    #[test]
    fn animator_is_active_tracks_running_anims() {
        let mut a = Animator::new();
        assert!(!a.is_active(), "初始无动画");
        let n = node(&[("transition", "width 200ms linear"), ("width", "0")]);
        a.set_now(0);
        a.apply(&n);
        let mut target = node(&[("transition", "width 200ms linear"), ("width", "100")]);
        a.set_now(0);
        a.apply(&target); // 启动 0→100
        assert!(a.is_active(), "动画进行中应为 active");
        a.set_now(1000); // 超过 duration
        let _ = a.apply(&target); // sample 到期移除动画
        assert!(!a.is_active(), "动画结束后应回到 inactive（允许无变化帧跳过重算）");
    }

    #[test]
    fn animator_smooth_retarget() {
        let mut a = Animator::new();
        let mut n = node(&[("transition", "width 200ms linear"), ("width", "0")]);
        a.set_now(0);
        a.apply(&n);
        n.props.insert("width".into(), "100".into());
        a.set_now(0);
        a.apply(&n); // 开始 0→100
        a.set_now(100); // 中点 ≈50
        let mid = a.apply(&n);
        let v: f32 = mid.props["width"].parse().unwrap();
        assert!((v - 50.0).abs() < 2.0, "中点≈50, got {}", v);
        // 中途改目标 200，应从当前≈50 起而非从 0
        n.props.insert("width".into(), "200".into());
        a.set_now(100);
        let out = a.apply(&n);
        let v: f32 = out.props["width"].parse().unwrap();
        assert!((v - 50.0).abs() < 3.0, "retarget 起点应≈50, got {}", v);
    }
}
