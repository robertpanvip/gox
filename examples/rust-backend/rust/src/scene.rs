//! Rust 侧的常驻镜像场景树（Zed 式 retained mode）。
//! 首帧 GRX1 全量建模；后续 GRX2 动作按 nodeID 打补丁，避免每帧传整棵树。

use crate::binprotocol::{FrameView, NodeType};
use std::collections::HashMap;

#[derive(Clone, Debug)]
pub struct SceneNode {
    pub typ: NodeType,
    pub kind: String,
    pub id: String,
    pub text: String,
    pub props: HashMap<String, String>,
    pub events: Vec<String>,
    pub children: Vec<SceneNode>,
}

impl SceneNode {
    fn from_view(view: &FrameView<'_>, idx: usize) -> Self {
        let nv = view.node(idx);
        let mut props = HashMap::new();
        for p in nv.prop_start..nv.prop_start + nv.prop_count {
            let pv = view.prop(p);
            props.insert(pv.key.to_string(), pv.val.to_string());
        }
        let mut events = Vec::new();
        for e in 0..view.event_count {
            let ev = view.event(e);
            if ev.node_index == idx {
                events.push(ev.name.to_string());
            }
        }
        // 直接子节点：parent == idx（DFS 前序保证顺序）
        let mut children = Vec::new();
        for ci in 0..view.node_count {
            if view.node(ci).parent as usize == idx {
                children.push(SceneNode::from_view(view, ci));
            }
        }
        SceneNode {
            typ: nv.typ,
            kind: nv.kind.to_string(),
            id: nv.id.to_string(),
            text: nv.text.to_string(),
            props,
            events,
            children,
        }
    }

    pub fn from_frame(view: &FrameView<'_>) -> Option<Self> {
        if view.node_count == 0 {
            return None;
        }
        Some(SceneNode::from_view(view, 0))
    }

    /// 按 nodeID 递归查找可变引用。
    pub fn find_mut(&mut self, id: &str) -> Option<&mut SceneNode> {
        if self.id == id {
            return Some(self);
        }
        for c in &mut self.children {
            if let Some(found) = c.find_mut(id) {
                return Some(found);
            }
        }
        None
    }

    /// 找到直接子节点中 id == child_id 的位置。
    fn child_index(&self, child_id: &str) -> Option<usize> {
        self.children.iter().position(|c| c.id == child_id)
    }

    /// 在 before_id 之前插入子树；before_id 空则追加末尾。返回是否成功。
    fn insert_child_before(&mut self, child: SceneNode, before_id: &str) -> bool {
        if before_id.is_empty() {
            self.children.push(child);
            return true;
        }
        if let Some(pos) = self.child_index(before_id) {
            self.children.insert(pos, child);
            return true;
        }
        false
    }

}

// ---- GRX2 动作帧解析 ----

const DELTA_MAGIC: u32 = 0x3258_5247; // "GRX2"
const DELTA_HEADER: usize = 16;
const ACTION_SZ: usize = 24;
const ACT_SET_ATTR: u8 = 0;
const ACT_SET_TEXT: u8 = 1;
const ACT_SET_HANDLE: u8 = 2;
const ACT_INSERT_BEFORE: u8 = 3;
const ACT_REMOVE_CHILD: u8 = 4;
const ACT_MOVE_CHILD: u8 = 5;
const ACT_REPLACE_CHILDREN: u8 = 6;

fn u32at(b: &[u8], o: usize) -> u32 {
    u32::from_le_bytes([b[o], b[o + 1], b[o + 2], b[o + 3]])
}
fn u16at(b: &[u8], o: usize) -> u16 {
    u16::from_le_bytes([b[o], b[o + 1]])
}

pub struct DeltaView<'a> {
    buf: &'a [u8],
    count: usize,
    pool_off: usize,
    /// 内嵌子树 blob 区起点 = pool_off + pool_len。
    blob_off: usize,
}

impl<'a> DeltaView<'a> {
    pub fn parse(buf: &'a [u8]) -> Option<Self> {
        if buf.len() < DELTA_HEADER || u32at(buf, 0) != DELTA_MAGIC {
            return None;
        }
        let count = u32at(buf, 8) as usize;
        let pool_len = u32at(buf, 12) as usize;
        let pool_off = DELTA_HEADER + count * ACTION_SZ;
        let blob_off = pool_off + pool_len;
        if blob_off > buf.len() {
            return None;
        }
        Some(DeltaView { buf, count, pool_off, blob_off })
    }

    fn pool_str(&self, off: u32) -> &'a str {
        let o = self.pool_off + off as usize;
        let len = u16at(self.buf, o) as usize;
        std::str::from_utf8(&self.buf[o + 2..o + 2 + len]).unwrap_or("")
    }

    /// 从 blob 区解析一棵内嵌子树（GRX1 迷你帧）。
    fn subtree(&self, off: u32, len: u32) -> Option<SceneNode> {
        let start = self.blob_off + off as usize;
        let end = start + len as usize;
        if end > self.buf.len() {
            return None;
        }
        let view = FrameView::parse(&self.buf[start..end])?;
        SceneNode::from_frame(&view)
    }

    /// 把动作应用到镜像树。返回应用的动作数。
    ///
    /// 分两阶段：
    ///  ① 属性/文本/事件动作就地修改（互不依赖，按序 apply）。
    ///  ② 结构动作先解析成「意图」数据，再逐个执行，避开 Rust 借用冲突。
    pub fn apply(&self, root: &mut SceneNode) -> usize {
        let mut applied = 0;

        // 阶段①：非结构动作。
        for i in 0..self.count {
            let o = DELTA_HEADER + i * ACTION_SZ;
            let kind = self.buf[o];
            if matches!(kind, ACT_INSERT_BEFORE | ACT_REMOVE_CHILD | ACT_MOVE_CHILD | ACT_REPLACE_CHILDREN) {
                continue;
            }
            let node_id = self.pool_str(u32at(self.buf, o + 4));
            let key = self.pool_str(u32at(self.buf, o + 16));
            let val = self.pool_str(u32at(self.buf, o + 20));
            if let Some(n) = root.find_mut(node_id) {
                match kind {
                    ACT_SET_ATTR => { n.props.insert(key.to_string(), val.to_string()); }
                    ACT_SET_TEXT => { n.text = val.to_string(); }
                    ACT_SET_HANDLE => {
                        if !n.events.iter().any(|e| e == key) { n.events.push(key.to_string()); }
                    }
                    _ => {}
                }
                applied += 1;
            }
        }

        // 阶段②：结构动作。先解码为意图，再执行。
        enum StructOp {
            Insert { parent: String, child: SceneNode, before: String },
            Remove { parent: String, child_id: String },
            Move { parent: String, child_id: String, before: String },
            Replace { parent: String, sub: SceneNode },
        }
        let mut ops: Vec<StructOp> = Vec::new();
        for i in 0..self.count {
            let o = DELTA_HEADER + i * ACTION_SZ;
            let kind = self.buf[o];
            let parent = self.pool_str(u32at(self.buf, o + 4)).to_string();
            let child_raw = u32at(self.buf, o + 8);
            let before = self.pool_str(u32at(self.buf, o + 12)).to_string();
            let val_raw = u32at(self.buf, o + 20);
            match kind {
                ACT_INSERT_BEFORE => {
                    if let Some(child) = self.subtree(child_raw, val_raw) {
                        ops.push(StructOp::Insert { parent, child, before });
                    }
                }
                ACT_REMOVE_CHILD => {
                    let child_id = self.pool_str(child_raw).to_string();
                    ops.push(StructOp::Remove { parent, child_id });
                }
                ACT_MOVE_CHILD => {
                    let child_id = self.pool_str(child_raw).to_string();
                    ops.push(StructOp::Move { parent, child_id, before });
                }
                ACT_REPLACE_CHILDREN => {
                    if let Some(sub) = self.subtree(child_raw, val_raw) {
                        ops.push(StructOp::Replace { parent, sub });
                    }
                }
                _ => {}
            }
        }
        for op in ops {
            let ok = match op {
                StructOp::Insert { parent, child, before } => root
                    .find_mut(&parent)
                    .map(|p| p.insert_child_before(child, &before))
                    .unwrap_or(false),
                StructOp::Remove { parent, child_id } => {
                    if let Some(p) = root.find_mut(&parent) {
                        if let Some(pos) = p.child_index(&child_id) {
                            p.children.remove(pos);
                            true
                        } else { false }
                    } else { false }
                }
                StructOp::Move { parent, child_id, before } => {
                    if let Some(p) = root.find_mut(&parent) {
                        if let Some(pos) = p.child_index(&child_id) {
                            let node = p.children.remove(pos);
                            // before 可能是被移动节点自身之后的位置；remove 后重新查找锚点。
                            if before.is_empty() {
                                p.children.push(node);
                                true
                            } else if let Some(ins) = p.child_index(&before) {
                                p.children.insert(ins, node);
                                true
                            } else {
                                // 锚点缺失则退回末尾，保证不丢节点。
                                p.children.push(node);
                                true
                            }
                        } else { false }
                    } else { false }
                }
                StructOp::Replace { parent, sub } => root
                    .find_mut(&parent)
                    .map(|p| { p.children = sub.children; true })
                    .unwrap_or(false),
            };
            if ok { applied += 1; }
        }
        applied
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn put32(b: &mut [u8], o: usize, v: u32) { b[o..o+4].copy_from_slice(&v.to_le_bytes()); }
    fn pool_str(buf: &mut Vec<u8>, s: &str) -> u32 {
        let off = buf.len() as u32;
        buf.extend_from_slice(&(s.len() as u16).to_le_bytes());
        buf.extend_from_slice(s.as_bytes());
        off
    }

    /// 构造一个最小 GRX1 帧：N 个 text 节点，全部 parent=0（root），root 为 Div。
    /// 返回字节；root id="sub"，children id 依次 "c0","c1"...
    fn make_grx1(ids: &[&str]) -> Vec<u8> {
        let mut pool: Vec<u8> = Vec::new();
        let sub_id = pool_str(&mut pool, "sub");
        // 节点：0=root(Div), 1..=children(Text)
        let n = 1 + ids.len();
        let mut nodes = vec![0u8; n * 32];
        // root
        nodes[0] = 1; // Div
        put32(&mut nodes, 4, 0);   // kind_off=0
        put32(&mut nodes, 8, sub_id);
        put32(&mut nodes, 22, ids.len() as u32 * 0); // placeholder
        // child_count at offset 22 (u16)
        nodes[22..24].copy_from_slice(&(ids.len() as u16).to_le_bytes());
        put32(&mut nodes, 24, 0xFFFFFFFF);
        for (i, id) in ids.iter().enumerate() {
            let o = (i + 1) * 32;
            nodes[o] = 2; // Text
            let idoff = pool_str(&mut pool, id);
            let textoff = pool_str(&mut pool, id);
            put32(&mut nodes, o + 8, idoff);
            put32(&mut nodes, o + 12, textoff);
            put32(&mut nodes, o + 24, 0); // parent = root index 0
        }
        // pool 需要放在所有字符串构造完之后（ids 的 pool_str 在循环里追加）
        let total = 24 + nodes.len() + pool.len();
        let mut buf = vec![0u8; total];
        put32(&mut buf, 0, 0x3158_5247);
        put32(&mut buf, 4, 100);
        put32(&mut buf, 8, 100);
        put32(&mut buf, 12, n as u32);
        put32(&mut buf, 16, 0); // prop_count
        put32(&mut buf, 20, pool.len() as u32);
        buf[24..24+nodes.len()].copy_from_slice(&nodes);
        buf[24+nodes.len()..].copy_from_slice(&pool);
        buf
    }

    /// 构造 GRX2 帧。actions: [(kind, node_id, child_or_bloboff, before_id, blob_or_len)]
    /// blob: 追加的 blob 区。child_off 用 blob 区内的绝对偏移（相对于 blob 起点）。
    fn make_grx2(actions: &[(u8, &str, &str, &str, &str)], blob: &[u8]) -> Vec<u8> {
        let mut pool: Vec<u8> = Vec::new();
        let mut act_bytes = vec![0u8; actions.len() * 24];
        for (i, (kind, node, c, before, val)) in actions.iter().enumerate() {
            let o = i * 24;
            act_bytes[o] = *kind;
            put32(&mut act_bytes, o + 4, pool_str(&mut pool, node));
            // c 是字符串（child id / before id）还是数字（blob off）由调用方编码约定：
            // 结构动作 Insert/Replace 的 c/val 是数字字符串（十进制），其余为 id/文本。
            let cv = c.parse::<u32>().ok();
            let c_off = match cv { Some(v) => v, None => pool_str(&mut pool, c) };
            put32(&mut act_bytes, o + 8, c_off);
            put32(&mut act_bytes, o + 12, pool_str(&mut pool, before));
            put32(&mut act_bytes, o + 16, 0);
            let vv = val.parse::<u32>().ok();
            let v_off = match vv { Some(v) => v, None => pool_str(&mut pool, val) };
            put32(&mut act_bytes, o + 20, v_off);
        }
        let total = 16 + act_bytes.len() + pool.len() + blob.len();
        let mut buf = vec![0u8; total];
        put32(&mut buf, 0, 0x3258_5247);
        put32(&mut buf, 4, 100);
        put32(&mut buf, 8, actions.len() as u32);
        put32(&mut buf, 12, pool.len() as u32);
        let mut o = 16;
        buf[o..o+act_bytes.len()].copy_from_slice(&act_bytes); o += act_bytes.len();
        buf[o..o+pool.len()].copy_from_slice(&pool); o += pool.len();
        buf[o..].copy_from_slice(blob);
        buf
    }

    fn base_tree() -> SceneNode {
        SceneNode {
            typ: NodeType::Div, kind: "div".into(), id: "r".into(), text: String::new(),
            props: Default::default(), events: vec![],
            children: vec![
                SceneNode { typ: NodeType::Text, kind: "".into(), id: "a".into(), text: "A".into(),
                    props: Default::default(), events: vec![], children: vec![] },
                SceneNode { typ: NodeType::Text, kind: "".into(), id: "b".into(), text: "B".into(),
                    props: Default::default(), events: vec![], children: vec![] },
                SceneNode { typ: NodeType::Text, kind: "".into(), id: "c".into(), text: "C".into(),
                    props: Default::default(), events: vec![], children: vec![] },
            ],
        }
    }

    fn ids(root: &SceneNode) -> Vec<&str> { root.children.iter().map(|c| c.id.as_str()).collect() }

    #[test]
    fn remove_child() {
        let mut root = base_tree();
        let g2 = make_grx2(&[(ACT_REMOVE_CHILD, "r", "a", "", "")], &[]);
        let v = DeltaView::parse(&g2).unwrap();
        assert_eq!(v.apply(&mut root), 1);
        assert_eq!(ids(&root), ["b", "c"]);
    }

    #[test]
    fn move_child_before() {
        let mut root = base_tree();
        // 把 a 移到 c 之前 → b, a, c
        let g2 = make_grx2(&[(ACT_MOVE_CHILD, "r", "a", "c", "")], &[]);
        let v = DeltaView::parse(&g2).unwrap();
        assert_eq!(v.apply(&mut root), 1);
        assert_eq!(ids(&root), ["b", "a", "c"]);
    }

    #[test]
    fn move_child_to_end() {
        let mut root = base_tree();
        let g2 = make_grx2(&[(ACT_MOVE_CHILD, "r", "a", "", "")], &[]);
        let v = DeltaView::parse(&g2).unwrap();
        v.apply(&mut root);
        assert_eq!(ids(&root), ["b", "c", "a"]);
    }

    #[test]
    fn insert_before() {
        let mut root = base_tree();
        let blob = make_grx1(&["d"]);
        // Insert: c=blob offset(0), before="b", val=blob len
        let g2 = make_grx2(&[(ACT_INSERT_BEFORE, "r", "0", "b", &blob.len().to_string())], &blob);
        let v = DeltaView::parse(&g2).unwrap();
        assert_eq!(v.apply(&mut root), 1);
        // 插入的是整棵子树（id="sub"），位于 b 之前
        assert_eq!(ids(&root), ["a", "sub", "b", "c"]);
    }

    #[test]
    fn replace_children() {
        let mut root = base_tree();
        let blob = make_grx1(&["x", "y"]);
        let g2 = make_grx2(&[(ACT_REPLACE_CHILDREN, "r", "0", "", &blob.len().to_string())], &blob);
        let v = DeltaView::parse(&g2).unwrap();
        assert_eq!(v.apply(&mut root), 1);
        // Replace 用子树 children 覆盖 → x, y
        assert_eq!(ids(&root), ["x", "y"]);
    }
}
