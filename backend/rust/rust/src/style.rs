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

/// 声明式动画解析结果，语义与 Go 端 gxscene.AnimationSpec 严格对齐。
#[derive(Clone, Copy, Debug)]
pub struct AnimationSpec {
    pub duration_ms: i64,
    pub iterations: i64, // -1 = infinite
}

impl AnimationSpec {
    /// 按绝对时钟 now_ms 采样 blink 的 opacity。返回 None 表示不覆盖基础 opacity。
    /// 相位 < 50% → 1.0，否则 0.0；有限次数结束后不覆盖（fill-mode:none）。
    pub fn sample_blink(&self, now_ms: i64) -> Option<f32> {
        if self.duration_ms <= 0 { return None; }
        if self.iterations > 0 && now_ms >= self.duration_ms * self.iterations { return None; }
        let phase = now_ms % self.duration_ms;
        Some(if phase * 2 < self.duration_ms { 1.0 } else { 0.0 })
    }
}

/// 解析 animation 标量（如 "blink 1.06s infinite"），只接受内置 blink 动画。
fn parse_duration_ms(s: &str) -> Option<i64> {
    let s = s.trim();
    if let Some(v) = s.strip_suffix("ms") {
        return v.trim().parse::<f64>().ok().map(|x| x as i64);
    }
    if let Some(v) = s.strip_suffix('s') {
        return v.trim().parse::<f64>().ok().map(|x| (x * 1000.0) as i64);
    }
    s.parse::<f64>().ok().map(|x| (x * 1000.0) as i64)
}

pub fn parse_animation(s: &str) -> Option<AnimationSpec> {
    let lowered = s.trim().to_ascii_lowercase();
    let toks: Vec<&str> = lowered.split_whitespace().collect();
    if toks.is_empty() || toks[0] != "blink" { return None; }
    let mut spec = AnimationSpec { duration_ms: 0, iterations: -1 };
    for t in &toks[1..] {
        if *t == "infinite" { spec.iterations = -1; continue; }
        if let Ok(n) = t.parse::<i64>() {
            if n > 0 { spec.iterations = n; continue; }
        }
        if let Some(d) = parse_duration_ms(t) {
            if d > 0 { spec.duration_ms = d; }
        }
    }
    if spec.duration_ms > 0 { Some(spec) } else { None }
}

/// 从节点 props 读取 animation 并解析。
pub fn animation_from_props(n: &SceneNode) -> Option<AnimationSpec> {
    n.props.get("animation").and_then(|s| parse_animation(s))
}

/// 扫描整棵树是否存在活跃的声明式动画。用于渲染前置判断：周期动画无外部事件
/// 唤醒，即使 transition 静止也必须进入全量重建路径逐帧采样。
pub fn tree_has_animation(n: &SceneNode) -> bool {
    if animation_from_props(n).is_some() { return true; }
    n.children.iter().any(tree_has_animation)
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

// ---------- 伪状态样式覆盖（&:hover / &:active） ----------
//
// host 把样式表展平后，普通属性直出为 props，伪状态子表编码成
// props["&:hover"] = `{"bg":"#2563eb"}` 这样的 JSON 串。本节在布局/绘制前，
// 按当前命中的 hovered/active 节点把对应子表覆盖进一棵临时树副本，
// 与 Animator::apply 同构——不污染常驻镜像树，下一帧状态变了自动还原。

/// 把某个节点的伪状态子表覆盖进它的 props。伪状态 key 用 "&:"，兼容旧 "$:"。
/// 返回是否发生了覆盖（供调用方决定是否要重建 DisplayList）。
fn overlay_pseudo(props: &mut std::collections::HashMap<String, String>, pseudo: &str) -> bool {
    let val = props
        .get(pseudo)
        .or_else(|| props.get(&pseudo.replacen("&:", "$:", 1)))
        .cloned();
    let Some(json) = val else { return false; };
    let Ok(obj) = serde_json::from_str::<serde_json::Value>(&json) else { return false; };
    let Some(map) = obj.as_object() else { return false; };
    let mut changed = false;
    for (k, v) in map {
        let s = match v {
            serde_json::Value::String(x) => x.clone(),
            serde_json::Value::Number(x) => x.to_string(),
            serde_json::Value::Bool(x) => x.to_string(),
            _ => continue,
        };
        props.insert(k.clone(), s);
        changed = true;
    }
    changed
}

/// 判断节点是否禁用。props 由 host 原样透传，禁用标记兼容 bool 与字符串：
/// "true"/"1" 视为禁用；其余（含缺省、"false"/"0"）视为启用。
fn is_disabled(n: &SceneNode) -> bool {
    match n.props.get("disabled").map(|s| s.trim().to_ascii_lowercase()) {
        Some(v) => v == "true" || v == "1",
        None => false,
    }
}

/// 按当前交互状态生成一棵 props 被伪状态覆盖的临时树。
///
/// 覆盖顺序（后者覆盖前者同名属性）：
///   hover → focus → active → disabled
/// 即 active 强于 hover/focus，disabled 又强于三者（禁用态应盖过任何交互态）。
/// 与 Animator::apply 同构：只克隆命中节点的 props，不污染常驻镜像树。
///
/// - hovered / active / focused 为当前命中的节点 ID（None 表示无此状态）。
/// - disabled 是节点自身的持久属性（props["disabled"]），无需外部载体：
///   即便鼠标悬停或聚焦，禁用节点的 &:disabled 都应生效并覆盖交互态。
/// 未命中或节点无对应伪状态子表时返回 clone（语义等价，调用方统一处理）。
pub fn apply_pseudo_state(
    root: &SceneNode,
    hovered: Option<&str>,
    active: Option<&str>,
    focused: Option<&str>,
) -> SceneNode {
    fn walk(
        n: &SceneNode,
        hovered: Option<&str>,
        active: Option<&str>,
        focused: Option<&str>,
    ) -> SceneNode {
        let mut out = n.clone();
        let id = n.id.as_str();
        // 交互态：hover 先、focus 次、active 后，逐级覆盖同名属性。
        if Some(id) == hovered {
            overlay_pseudo(&mut out.props, "&:hover");
        }
        if Some(id) == focused {
            overlay_pseudo(&mut out.props, "&:focus");
        }
        if Some(id) == active {
            overlay_pseudo(&mut out.props, "&:active");
        }
        // 禁用态优先级最高：盖过 hover/focus/active 的覆盖结果。
        if is_disabled(n) {
            overlay_pseudo(&mut out.props, "&:disabled");
        }
        for c in &mut out.children {
            let old = c.clone();
            *c = walk(&old, hovered, active, focused);
        }
        out
    }
    walk(root, hovered, active, focused)
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
    /// 本帧 apply 看到的活跃声明式动画（周期动画无外部事件唤醒，据此维持重绘）。
    has_animation: bool,
}

impl Animator {
    pub fn new() -> Self {
        Self { anims: AnimMap::new(), targets: HashMap::new(), now_ms: 0, has_animation: false }
    }

    pub fn set_now(&mut self, now_ms: u128) { self.now_ms = now_ms; }

    /// 是否仍有进行中的动画。无变化帧可据此决定是否需要继续重绘。
    pub fn is_active(&self) -> bool { !self.anims.is_empty() }

    /// 本帧是否采样到活跃的声明式动画（如 blink），供宿主持续重绘。
    pub fn has_active_animation(&self) -> bool { self.has_animation }

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
        self.has_animation = false; // 每帧重置，记录本帧看到的周期动画
        let mut out = root.clone();
        self.apply_node(root, &mut out);
        out
    }

    fn apply_node(&mut self, src: &SceneNode, dst: &mut SceneNode) {
        // 声明式 animation（周期动画）：按绝对时钟采样 opacity 写进临时树，优先级
        // 高于 transition（对齐 CSS animation > transition）。覆盖值随时间变化，
        // needs_continuous_redraw 据此维持重绘。
        if let Some(spec) = animation_from_props(src) {
            self.has_animation = true;
            if let Some(op) = spec.sample_blink(self.now_ms as i64) {
                dst.props.insert("opacity".into(), format!("{:.3}", op));
            }
        }
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
    fn apply_pseudo_state_overlays_hover_focus_active() {
        let mut n = node(&[]);
        n.id = "btn".into();
        n.props.insert("bg".into(), "#fff".into());
        n.props.insert("&:hover".into(), r##"{"bg":"#eee","color":"#333"}"##.into());
        n.props.insert("&:focus".into(), r##"{"border":"#00f"}"##.into());
        n.props.insert("&:active".into(), r##"{"bg":"#ddd"}"##.into());
        let mut child = node(&[]);
        child.id = "label".into();
        child.props.insert("bg".into(), "#000".into());
        n.children.push(child);

        // 无命中：原样
        let plain = apply_pseudo_state(&n, None, None, None);
        assert_eq!(plain.props["bg"], "#fff");
        assert!(!plain.props.contains_key("color"));

        // hover：子表覆盖 bg + 新增 color
        let hov = apply_pseudo_state(&n, Some("btn"), None, None);
        assert_eq!(hov.props["bg"], "#eee");
        assert_eq!(hov.props["color"], "#333");
        assert_eq!(hov.children[0].props["bg"], "#000", "未命中子节点不受影响");

        // focus：只套 focus 子表
        let foc = apply_pseudo_state(&n, None, None, Some("btn"));
        assert_eq!(foc.props["bg"], "#fff", "focus 未声明 bg 应保留基础值");
        assert_eq!(foc.props["border"], "#00f", "focus 子表新增 border");

        // hover + focus + active：active 覆盖同名 bg，focus 的非冲突属性保留
        let act = apply_pseudo_state(&n, Some("btn"), Some("btn"), Some("btn"));
        assert_eq!(act.props["bg"], "#ddd");
        assert_eq!(act.props["color"], "#333", "hover 的非冲突属性保留");
        assert_eq!(act.props["border"], "#00f", "focus 的非冲突属性保留");

        // 原树不被污染
        assert_eq!(n.props["bg"], "#fff");
    }

    #[test]
    fn apply_pseudo_state_disabled_overrides_and_is_idempotent() {
        let mut n = node(&[]);
        n.id = "btn".into();
        n.props.insert("bg".into(), "#3b82f6".into());
        n.props.insert("disabled".into(), "true".into());
        n.props.insert("&:hover".into(), r##"{"bg":"#2563eb"}"##.into());
        n.props.insert("&:active".into(), r##"{"bg":"#1d4ed8"}"##.into());
        n.props.insert("&:focus".into(), r##"{"bg":"#60a5fa"}"##.into());
        n.props.insert("&:disabled".into(), r##"{"bg":"#9ca3af","opacity":"0.6"}"##.into());

        // 禁用是节点自身属性，无外部载体也应生效，且盖过任何交互态。
        let dis = apply_pseudo_state(&n, Some("btn"), Some("btn"), Some("btn"));
        assert_eq!(dis.props["bg"], "#9ca3af", "disabled 应盖过 hover/focus/active");
        assert_eq!(dis.props["opacity"], "0.6", "disabled 子表新增属性");

        // 字符串 "1" 同样识别为禁用。
        n.props.insert("disabled".into(), "1".into());
        let dis1 = apply_pseudo_state(&n, None, None, None);
        assert_eq!(dis1.props["bg"], "#9ca3af");

        // "false" / 缺省不禁用：交互态正常生效。
        n.props.insert("disabled".into(), "false".into());
        let en = apply_pseudo_state(&n, Some("btn"), None, None);
        assert_eq!(en.props["bg"], "#2563eb", "启用时 hover 正常生效");

        n.props.remove("disabled");
        let en2 = apply_pseudo_state(&n, None, None, None);
        assert_eq!(en2.props["bg"], "#3b82f6", "无 disabled 键时为基础值");
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

    #[test]
    fn parse_animation_blink() {
        let s = parse_animation("blink 1.06s infinite").expect("应解析 blink");
        assert_eq!(s.duration_ms, 1060);
        assert_eq!(s.iterations, -1);
        assert!(parse_animation("blink 500ms 3").is_some());
        assert!(parse_animation("pulse 1s").is_none(), "未内置动画名应忽略");
        assert!(parse_animation("blink").is_none(), "缺时长应忽略");
    }

    #[test]
    fn animation_blink_sample_square_wave() {
        let s = parse_animation("blink 1000ms infinite").unwrap();
        assert_eq!(s.sample_blink(0), Some(1.0));
        assert_eq!(s.sample_blink(499), Some(1.0));
        assert_eq!(s.sample_blink(500), Some(0.0));
        assert_eq!(s.sample_blink(999), Some(0.0));
        assert_eq!(s.sample_blink(1000), Some(1.0));
        let finite = parse_animation("blink 1000ms 2").unwrap();
        assert!(finite.sample_blink(1999).is_some());
        assert!(finite.sample_blink(2000).is_none(), "有限次数结束后不覆盖");
    }

    #[test]
    fn animator_applies_animation_opacity() {
        let mut a = Animator::new();
        a.set_now(0);
        let mut n = node(&[("animation", "blink 1000ms infinite")]);
        n.id = "caret".into();
        let out0 = a.apply(&n);
        assert_eq!(out0.props["opacity"], "1.000");
        assert!(a.has_active_animation());

        a.set_now(600);
        let out1 = a.apply(&n);
        assert_eq!(out1.props["opacity"], "0.000");
        // 常驻树不被污染
        assert!(!n.props.contains_key("opacity"));
    }
}
