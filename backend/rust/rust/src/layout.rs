//! 布局缓存层（BACKEND_ARCHITECTURE_V2 阶段 3：Layout 结果缓存）。
//!
//! 不变量（评审 §四）：改颜色/透明度/transform 等纯绘制属性不得重建布局树。
//! 实现：对场景树计算「布局签名」——布局相关属性（白名单）参与值哈希，其余
//! 属性只参与 key 存在性哈希。签名不变 → 复用上帧 Taffy 树与布局坐标，跳过
//! build + compute_layout（约束求解），仅重跑便宜的 emit（绘制指令编译）。
//!
//! 正确性论证：
//! - build（Taffy Style 生成）只读取白名单属性与文本内容 → 这些值不变时
//!   Taffy 输入完全相同，复用安全；
//! - 非白名单属性（color/bg/border/transform/opacity/scroll-top/...）只被
//!   emit 读取 → emit 每帧照跑，绘制结果永远新鲜；
//! - 误排除只会导致「该重建没重建」的布局 bug，因此白名单采取保守原则：
//!   build/scan 路径读取的属性必须全部列入；任何新增布局属性都要同步维护。

use std::hash::{Hash, Hasher};

use crate::util::TextMeasure;

/// 布局相关属性白名单（值参与哈希）。覆盖 build（Taffy Style / TextMeasure）
/// 与浮层第二遍（gx-role/gx-anchor/gx-placement/gx-offset 定位）的全部读取。
const LAYOUT_PROPS: &[&str] = &[
    // 盒模型与尺寸
    "width", "height", "min-width", "min-height", "max-width", "max-height",
    "padding", "margin", "margin-top", "margin-right", "margin-bottom", "margin-left",
    // flex 布局
    "gap", "row-gap", "column-gap", "flex-direction", "flex-wrap",
    "justify-content", "align-items", "align-content", "flex-grow", "flex-shrink",
    // 定位与溢出（position:absolute 脱离 flex 流；overflow 决定子树 shrink）
    "position", "top", "right", "bottom", "left", "overflow",
    // 文本测量输入（TextMeasure：text + size + line_height + 折叠语义）
    "font-size", "line-height", "white-space",
    // 浮层定位（scan 第二遍）：角色判定影响 position:absolute 与锚点定位
    "gx-role", "gx-anchor", "gx-placement", "gx-offset",
];

/// 计算场景树的布局签名（窗口尺寸参与）。
pub(crate) fn layout_signature(
    root: &crate::scene::SceneNode,
    avail_w: f32,
    avail_h: f32,
) -> u64 {
    let mut h = std::collections::hash_map::DefaultHasher::new();
    avail_w.to_bits().hash(&mut h);
    avail_h.to_bits().hash(&mut h);
    hash_node(root, &mut h);
    h.finish()
}

fn hash_node(n: &crate::scene::SceneNode, h: &mut std::collections::hash_map::DefaultHasher) {
    // 结构身份：类型 discriminant + kind + 稳定节点 id + 文本内容。
    (n.typ as u8).hash(h);
    n.kind.hash(h);
    n.id.hash(h);
    n.text.hash(h);
    // 白名单布局 props：值参与哈希（key 固定顺序遍历，顺序无关）。
    for k in LAYOUT_PROPS {
        if let Some(v) = n.props.get(*k) {
            k.hash(h);
            v.hash(h);
        }
    }
    // 其余 props（纯绘制域）：仅 key 存在性。值变化（颜色过渡、opacity 动画、
    // transform 插值、scroll-top 滚动）不破坏布局复用；key 出现/消失会重建
    // （保守正确：新 key 即使纯绘制也宁可重建）。
    let mut other: Vec<&String> = n
        .props
        .keys()
        .filter(|k| !LAYOUT_PROPS.contains(&k.as_str()))
        .collect();
    other.sort();
    for k in other {
        k.hash(h);
    }
    for c in &n.children {
        hash_node(c, h);
    }
}

/// 跨帧布局缓存：保留 Taffy 树（含 TextMeasure 上下文）与签名。
/// 生命周期同 Renderer；树在签名不匹配的下一帧整体重建。
pub(crate) struct LayoutCache {
    pub(crate) tree: Option<(taffy::TaffyTree<TextMeasure>, taffy::NodeId)>,
    pub(crate) sig: u64,
    /// 观测计数：布局重建次数 / 命中复用次数（测试与诊断用）。
    pub(crate) rebuilds: u64,
    pub(crate) reuses: u64,
}

impl LayoutCache {
    pub(crate) fn new() -> Self {
        LayoutCache { tree: None, sig: 0, rebuilds: 0, reuses: 0 }
    }

    /// 显式失效（签名之外的因素：如未来引入的外部布局输入变化）。
    #[allow(dead_code)] // 预留 API：签名机制已覆盖当前全部失效源
    pub(crate) fn invalidate(&mut self) {
        self.sig = 0;
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::binprotocol::NodeType;
    use crate::paint::DisplayList;

    fn div(id: &str, props: &[(&str, &str)], children: Vec<crate::scene::SceneNode>) -> crate::scene::SceneNode {
        crate::scene::SceneNode {
            typ: NodeType::Div, kind: "div".into(), id: id.into(), text: String::new(),
            props: props.iter().map(|(k, v)| (k.to_string(), v.to_string())).collect(),
            events: vec![], children,
        }
    }

    fn text(id: &str, content: &str, props: &[(&str, &str)]) -> crate::scene::SceneNode {
        crate::scene::SceneNode {
            typ: NodeType::Text, kind: "#text".into(), id: id.into(), text: content.into(),
            props: props.iter().map(|(k, v)| (k.to_string(), v.to_string())).collect(),
            events: vec![], children: vec![],
        }
    }

    fn page(color: &str, value: &str) -> crate::scene::SceneNode {
        div("page", &[("width", "100"), ("height", "50"), ("color", color)],
            vec![text("t", value, &[])])
    }

    /// 不变量验收：改颜色（纯绘制属性）不重建布局树，只重跑 emit。
    #[test]
    fn paint_only_change_reuses_layout() {
        let mut cache = LayoutCache::new();
        let _ = DisplayList::from_scene(&page("#ff0000", "hi"), 800.0, 600.0, &mut cache);
        assert_eq!(cache.rebuilds, 1);
        let _ = DisplayList::from_scene(&page("#00ff00", "hi"), 800.0, 600.0, &mut cache);
        assert_eq!(cache.rebuilds, 1, "颜色变化不得重建布局");
        assert_eq!(cache.reuses, 1);
        let _ = DisplayList::from_scene(&page("#00ff00", "hi"), 800.0, 600.0, &mut cache);
        assert_eq!(cache.reuses, 2);
    }

    /// 布局属性变化必须重建（正确性优先于性能）。
    #[test]
    fn layout_prop_change_rebuilds() {
        let mut cache = LayoutCache::new();
        let _ = DisplayList::from_scene(&page("#fff", "hi"), 800.0, 600.0, &mut cache);
        let _ = DisplayList::from_scene(&page("#fff", "bigger"), 800.0, 600.0, &mut cache);
        assert_eq!(cache.rebuilds, 2, "文本变化应重建");
        let _ = DisplayList::from_scene(&page("#fff", "bigger"), 1024.0, 600.0, &mut cache);
        assert_eq!(cache.rebuilds, 3, "窗口尺寸变化应重建");
    }

    /// 滚动（scroll-top 值变化）复用布局——GPU transform 滚动的关键路径。
    #[test]
    fn scroll_change_reuses_layout() {
        let scene = |top: f32| {
            let mut n = div("vp",
                &[("width", "100"), ("height", "100"), ("overflow", "scroll")],
                (0..5).map(|i| div(&format!("i{}", i), &[("height", "100")], vec![])).collect());
            n.props.insert("scroll-top".into(), format!("{}", top));
            n
        };
        let mut cache = LayoutCache::new();
        let _ = DisplayList::from_scene(&scene(0.0), 800.0, 600.0, &mut cache);
        let _ = DisplayList::from_scene(&scene(150.0), 800.0, 600.0, &mut cache);
        assert_eq!(cache.rebuilds, 1, "滚动不得重建布局");
        assert_eq!(cache.reuses, 1);
    }

    /// 一致性：命中路径与全量路径对布局结果完全一致（形状/文本数量与几何）。
    #[test]
    fn reuse_path_matches_rebuild_path() {
        let mut cache = LayoutCache::new();
        let a = DisplayList::from_scene(&page("#fff", "hi"), 800.0, 600.0, &mut cache);
        let b = DisplayList::from_scene(&page("#000", "hi"), 800.0, 600.0, &mut cache);
        assert_eq!(a.shapes.len(), b.shapes.len());
        assert_eq!(a.texts.len(), b.texts.len());
        for (sa, sb) in a.shapes.iter().zip(b.shapes.iter()) {
            // 颜色不同（绘制域），但几何必须逐项一致。
            assert!((sa.x - sb.x).abs() < 0.01 && (sa.y - sb.y).abs() < 0.01);
            assert!((sa.w - sb.w).abs() < 0.01 && (sa.h - sb.h).abs() < 0.01);
        }
        for (ta, tb) in a.texts.iter().zip(b.texts.iter()) {
            assert!((ta.x - tb.x).abs() < 0.01 && (ta.y - tb.y).abs() < 0.01);
            assert!((ta.size - tb.size).abs() < 0.01);
        }
    }
}
