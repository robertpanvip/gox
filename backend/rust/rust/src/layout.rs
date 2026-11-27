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
    "width",
    "height",
    "min-width",
    "min-height",
    "max-width",
    "max-height",
    "padding",
    "margin",
    "margin-top",
    "margin-right",
    "margin-bottom",
    "margin-left",
    // flex 布局
    "gap",
    "row-gap",
    "column-gap",
    "flex-direction",
    "flex-wrap",
    "justify-content",
    "align-items",
    "align-content",
    "flex-grow",
    "flex-shrink",
    // 定位与溢出（position:absolute 脱离 flex 流；overflow 决定子树 shrink）
    "position",
    "top",
    "right",
    "bottom",
    "left",
    "overflow",
    // 文本测量输入（TextMeasure：text + size + line_height + 折叠语义）
    "font-size",
    "line-height",
    "white-space",
    // 浮层定位（scan 第二遍）：角色判定影响 position:absolute 与锚点定位
    "gx-role",
    "gx-anchor",
    "gx-placement",
    "gx-offset",
];

/// 计算场景树的布局签名（窗口尺寸参与）。
pub(crate) fn layout_signature(root: &crate::scene::SceneNode, avail_w: f32, avail_h: f32) -> u64 {
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
    /// DisplayList 指令级增量缓存（布局命中帧的 emit 段复用）。
    pub(crate) emit: EmitCache,
}

impl LayoutCache {
    pub(crate) fn new() -> Self {
        LayoutCache {
            tree: None,
            sig: 0,
            rebuilds: 0,
            reuses: 0,
            emit: EmitCache::new(),
        }
    }

    /// 显式失效（签名之外的因素：如未来引入的外部布局输入变化）。
    #[allow(dead_code)] // 预留 API：签名机制已覆盖当前全部失效源
    pub(crate) fn invalidate(&mut self) {
        self.sig = 0;
    }
}

// ---------- DisplayList 指令级增量（emit 缓存） ----------

/// 单个子树 emit 产出的指令段（paint::NodeCmds 切片快照）+ emit 返回值。
pub(crate) struct EmitSlot {
    /// 产生该段时的子树渲染签名。
    pub(crate) sig: u64,
    pub(crate) cmds: crate::paint::NodeCmds,
    /// emit 返回值：子树内容底边 Y（供父计算 overflow 内容高度）。
    pub(crate) bottom: f32,
}

/// per-节点绘制指令缓存（BACKEND_ARCHITECTURE_V2 阶段 3 收尾）：
/// 布局命中帧的 emit 按「子树渲染签名」复用上帧指令段——签名相等 ⇔ emit
/// 的全部输入（子树内容 + 全 props 值 + 继承链滚动哈希 + 绝对坐标 + 裁剪）
/// 相等 ⇔ 输出指令段等价。命中时跳过 parse_style / collapse_whitespace /
/// 渐变切分 / 递归编译，只做段拷贝。
///
/// key = Taffy NodeId：布局复用帧间稳定；rebuild 帧树整体重建、NodeId 漂移，
/// 因此 rebuild 帧不启用本缓存（签名表为空，查询自然 miss，帧末全淘汰）。
/// 淘汰与 glyph_cache 同模式：每帧收集 live，帧末 retain 掉未出现的槽。
pub(crate) struct EmitCache {
    map: std::collections::HashMap<taffy::NodeId, EmitSlot>,
    live: std::collections::HashSet<taffy::NodeId>,
    /// 观测计数：命中 / 未命中（测试与诊断用）。
    pub(crate) hits: u64,
    pub(crate) misses: u64,
}

impl EmitCache {
    pub(crate) fn new() -> Self {
        EmitCache {
            map: std::collections::HashMap::new(),
            live: std::collections::HashSet::new(),
            hits: 0,
            misses: 0,
        }
    }

    /// 帧开始：清空本帧 live 集（帧末滚动淘汰的前置）。
    pub(crate) fn begin_frame(&mut self) {
        self.live.clear();
    }

    /// 帧结束：淘汰本帧未出现的槽（滚出视口 / 删除的子树）。
    pub(crate) fn end_frame(&mut self) {
        self.map.retain(|k, _| self.live.contains(k));
    }

    /// 查询：槽存在且签名一致才命中（签名不同视为 miss 并覆盖旧槽）。
    pub(crate) fn lookup(&mut self, key: taffy::NodeId, sig: u64) -> Option<&EmitSlot> {
        let hit = self.map.get(&key).map(|s| s.sig == sig).unwrap_or(false);
        if hit {
            self.hits += 1;
            self.live.insert(key);
            self.map.get(&key)
        } else {
            self.misses += 1;
            None
        }
    }

    /// 存槽（同 tnode 新签名覆盖旧段；同时标记 live）。
    pub(crate) fn store(
        &mut self,
        key: taffy::NodeId,
        sig: u64,
        cmds: crate::paint::NodeCmds,
        bottom: f32,
    ) {
        self.live.insert(key);
        self.map.insert(key, EmitSlot { sig, cmds, bottom });
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::binprotocol::NodeType;
    use crate::paint::DisplayList;

    fn div(
        id: &str,
        props: &[(&str, &str)],
        children: Vec<crate::scene::SceneNode>,
    ) -> crate::scene::SceneNode {
        crate::scene::SceneNode {
            typ: NodeType::Div,
            kind: "div".into(),
            id: id.into(),
            text: String::new(),
            nid: 1000,
            props: props
                .iter()
                .map(|(k, v)| (k.to_string(), v.to_string()))
                .collect(),
            events: vec![],
            children,
        }
    }

    fn text(id: &str, content: &str, props: &[(&str, &str)]) -> crate::scene::SceneNode {
        crate::scene::SceneNode {
            typ: NodeType::Text,
            kind: "#text".into(),
            id: id.into(),
            text: content.into(),
            nid: 1001,
            props: props
                .iter()
                .map(|(k, v)| (k.to_string(), v.to_string()))
                .collect(),
            events: vec![],
            children: vec![],
        }
    }

    fn page(color: &str, value: &str) -> crate::scene::SceneNode {
        div(
            "page",
            &[("width", "100"), ("height", "50"), ("color", color)],
            vec![text("t", value, &[])],
        )
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
            let mut n = div(
                "vp",
                &[("width", "100"), ("height", "100"), ("overflow", "scroll")],
                (0..5)
                    .map(|i| div(&format!("i{}", i), &[("height", "100")], vec![]))
                    .collect(),
            );
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

    // ---------- emit 指令缓存（阶段 3 收尾）验收 ----------

    /// 静态场景第三帧起应命中指令缓存，且输出与全量路径逐项一致。
    #[test]
    fn emit_cache_hits_and_preserves_output() {
        let mut cache = LayoutCache::new();
        let a = DisplayList::from_scene(&page("#fff", "hi"), 800.0, 600.0, &mut cache); // rebuild 帧
        let _ = DisplayList::from_scene(&page("#fff", "hi"), 800.0, 600.0, &mut cache); // 命中帧：miss + 存槽
        let c = DisplayList::from_scene(&page("#fff", "hi"), 800.0, 600.0, &mut cache); // 命中帧：hit
        assert!(cache.emit.hits >= 1, "静态场景应命中指令缓存");
        assert_eq!(a.shapes.len(), c.shapes.len());
        assert_eq!(a.texts.len(), c.texts.len());
        assert_eq!(a.hitboxes.len(), c.hitboxes.len());
        assert_eq!(a.inputs.len(), c.inputs.len());
        assert_eq!(a.scrollbars.len(), c.scrollbars.len());
        assert_eq!(a.measured.len(), c.measured.len());
        for (sa, sb) in a.shapes.iter().zip(c.shapes.iter()) {
            assert_eq!((sa.x, sa.y, sa.w, sa.h), (sb.x, sb.y, sb.w, sb.h));
            assert_eq!(sa.bg, sb.bg);
            assert_eq!(sa.bg_present, sb.bg_present);
            assert_eq!(sa.clip, sb.clip);
        }
        for (ta, tb) in a.texts.iter().zip(c.texts.iter()) {
            assert_eq!(ta.text, tb.text);
            assert_eq!(ta.color, tb.color);
            assert_eq!((ta.x, ta.y, ta.size), (tb.x, tb.y, tb.size));
            assert_eq!(ta.clip, tb.clip);
            assert_eq!(ta.spans.len(), tb.spans.len());
        }
        assert_eq!(a.measured, c.measured);
    }

    /// 纯绘制属性（继承 color）变化：布局命中，但继承链哈希变化必须使
    /// 子树指令缓存失效——文字颜色更新为新值。
    #[test]
    fn emit_cache_invalidates_on_inherited_color_change() {
        let mut cache = LayoutCache::new();
        let _ = DisplayList::from_scene(&page("#ff0000", "hi"), 800.0, 600.0, &mut cache);
        let _ = DisplayList::from_scene(&page("#ff0000", "hi"), 800.0, 600.0, &mut cache);
        let dl = DisplayList::from_scene(&page("#00ff00", "hi"), 800.0, 600.0, &mut cache);
        assert_eq!(cache.rebuilds, 1, "颜色变化不得重建布局");
        assert!(cache.emit.misses >= 1, "继承键变化应使指令缓存失效");
        let t = dl.texts.iter().find(|t| t.text == "hi").expect("文本节点");
        assert_eq!(t.color, (0, 255, 0), "缓存失效后文字颜色必须更新");
    }

    /// 滚动帧：scroll-top 参与签名（全 props 值哈希），容器子树缓存失效，
    /// 子项坐标必须正确更新——滚动视觉正确性优先于缓存收益。
    #[test]
    fn emit_cache_invalidates_on_scroll() {
        let scene = |top: f32| {
            let mut n = div(
                "vp",
                &[("width", "100"), ("height", "400"), ("overflow", "scroll")],
                (0..5)
                    .map(|i| {
                        div(
                            &format!("i{}", i),
                            &[("height", "100"), ("bg", "#222")],
                            vec![],
                        )
                    })
                    .collect(),
            );
            n.props.insert("scroll-top".into(), format!("{}", top));
            n
        };
        let mut cache = LayoutCache::new();
        let before = DisplayList::from_scene(&scene(0.0), 800.0, 600.0, &mut cache);
        let _ = DisplayList::from_scene(&scene(0.0), 800.0, 600.0, &mut cache);
        let after = DisplayList::from_scene(&scene(150.0), 800.0, 600.0, &mut cache);
        assert_eq!(cache.rebuilds, 1, "滚动不得重建布局");
        // 裁剪语义：滚出视口的子项被 clip_rejects 剔除，可见子项（跳过容器
        // 自身）必须整体平移 -150px——验证缓存失效后输出正确。
        let ys_before: Vec<f32> = before.shapes.iter().skip(1).map(|s| s.y).collect();
        let ys_after: Vec<f32> = after.shapes.iter().skip(1).map(|s| s.y).collect();
        assert!(
            !ys_after.is_empty() && !ys_before.is_empty(),
            "可见子项不得为空"
        );
        for y in &ys_after {
            assert!(
                ys_before.iter().any(|b| (b - 150.0 - y).abs() < 0.01),
                "after y={} 必须对应 before y={}（整体上移 150px）",
                y,
                y + 150.0
            );
        }
        // 滚动条 thumb 位置随内容滚动下移。
        assert_eq!(
            before.scrollbars.len(),
            after.scrollbars.len(),
            "滚动条几何应持续生成"
        );
        if let (Some(b1), Some(b2)) = (before.scrollbars.first(), after.scrollbars.first()) {
            assert!(b2.thumb_y > b1.thumb_y, "滚动后 thumb 应下移");
        }
    }

    /// 子树内容变化（文本）：布局 rebuild → 指令缓存全淘汰，输出为新内容。
    #[test]
    fn emit_cache_invalidates_on_text_change() {
        let mut cache = LayoutCache::new();
        let _ = DisplayList::from_scene(&page("#fff", "hi"), 800.0, 600.0, &mut cache);
        let dl = DisplayList::from_scene(&page("#fff", "hello"), 800.0, 600.0, &mut cache);
        assert_eq!(cache.rebuilds, 2, "文本变化应重建布局");
        assert!(dl.texts.iter().any(|t| t.text == "hello"), "新文本必须出现");
        assert!(!dl.texts.iter().any(|t| t.text == "hi"), "旧文本不得残留");
    }

    /// 成本分解测量（155 节点，与 cgo 基准 cards=30 同规模）：
    /// clone 树 / 全量 from_scene / 命中路径 from_scene 三段耗时。
    /// `cargo test bench_emit_cache_breakdown -- --ignored --nocapture`
    #[test]
    #[ignore]
    fn bench_emit_cache_breakdown() {
        use std::time::Instant;
        let card = |i: usize| {
            div(
                &format!("c{}", i),
                &[
                    ("bg", "#141b33"),
                    ("width", "200"),
                    ("height", "96"),
                    ("padding", "14"),
                    ("border-radius", "12"),
                ],
                vec![
                    div(
                        &format!("c{}l", i),
                        &[("color", "#94a3b8"), ("font-size", "12")],
                        vec![text(&format!("c{}lt", i), "Card label", &[])],
                    ),
                    div(
                        &format!("c{}v", i),
                        &[("color", "#e2e8f0"), ("font-size", "26")],
                        vec![text(&format!("c{}vt", i), "12345", &[])],
                    ),
                ],
            )
        };
        let root = div(
            "root",
            &[
                ("flex-direction", "column"),
                ("width", "760"),
                ("padding", "20"),
                ("align-items", "center"),
            ],
            vec![
                div(
                    "title",
                    &[("color", "#e2e8f0"), ("font-size", "26")],
                    vec![text("tt", "Benchmark Dashboard", &[])],
                ),
                div(
                    "row",
                    &[("flex-direction", "row"), ("gap", "12")],
                    (0..30).map(card).collect(),
                ),
            ],
        );
        let n = 500;
        let t = Instant::now();
        for _ in 0..n {
            let c = root.clone();
            std::hint::black_box(&c);
        }
        eprintln!(
            "clone tree          : {:>10.1} us/op",
            t.elapsed().as_secs_f64() * 1e6 / n as f64
        );

        let t = Instant::now();
        let mut rebuilds = 0u64;
        for _ in 0..n {
            let mut c = LayoutCache::new();
            let dl = DisplayList::from_scene(&root, 760.0, 560.0, &mut c);
            std::hint::black_box(&dl);
            rebuilds = c.rebuilds;
        }
        eprintln!(
            "from_scene (rebuild): {:>10.1} us/op (rebuilds={})",
            t.elapsed().as_secs_f64() * 1e6 / n as f64,
            rebuilds
        );

        let mut cache = LayoutCache::new();
        let _ = DisplayList::from_scene(&root, 760.0, 560.0, &mut cache);
        let t = Instant::now();
        for _ in 0..n {
            let dl = DisplayList::from_scene(&root, 760.0, 560.0, &mut cache);
            std::hint::black_box(&dl);
        }
        eprintln!(
            "from_scene (cached) : {:>10.1} us/op (hits={} misses={} layout reuses={})",
            t.elapsed().as_secs_f64() * 1e6 / n as f64,
            cache.emit.hits,
            cache.emit.misses,
            cache.reuses
        );
    }
}
