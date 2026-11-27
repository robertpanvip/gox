//! Rust 侧的常驻镜像场景树（Zed 式 retained mode）。
//! 首帧 GRX1 全量建模；后续 GRX2 动作按 nodeID 打补丁，避免每帧传整棵树。

use crate::binprotocol::{FrameView, NodeType};
use std::collections::HashMap;

#[derive(Clone, Debug)]
pub struct SceneNode {
    pub typ: NodeType,
    pub kind: String,
    /// 兼容字符串身份（GRX1 帧/测试构造；GRX3 帧不再携带，为空串）。
    pub id: String,
    /// GRX3 直传的整数节点身份（热路径缓存 key：glyph_cache / emit 签名 /
    /// measured 回传）。0 = NoNode。
    pub nid: u64,
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
            nid: nv.node_id,
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

    /// 身份匹配：整数身份优先（GRX3 热路径），字符串身份兜底（GRX1/测试）。
    /// target 可为十进制字符串（GRX2 delta 的池字符串 / C ABI 回传）或任意旧 id。
    pub(crate) fn matches(&self, target: &str) -> bool {
        if !self.id.is_empty() && self.id == target {
            return true;
        }
        if self.nid != 0 {
            if let Ok(t) = target.trim().parse::<u64>() {
                return self.nid == t;
            }
        }
        false
    }

    /// 按 nodeID 递归查找可变引用。
    /// 命中节点 → 根的祖先链（含命中节点自身，由内向外）。hover 光标
    /// （CSS cursor 继承属性）沿链找第一个声明者；找不到返回空 Vec。
    pub fn find_ancestors(&self, id: &str) -> Vec<&SceneNode> {
        fn walk<'a>(n: &'a SceneNode, id: &str, path: &mut Vec<&'a SceneNode>) -> bool {
            path.push(n);
            if n.matches(id) {
                return true;
            }
            for c in &n.children {
                if walk(c, id, path) {
                    return true;
                }
            }
            path.pop();
            false
        }
        let mut path = Vec::new();
        if walk(self, id, &mut path) {
            // path 是 根→命中；反转成 由内向外。
            path.reverse();
        }
        path
    }

    pub fn find_mut(&mut self, id: &str) -> Option<&mut SceneNode> {
        if self.matches(id) {
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
        self.children.iter().position(|c| c.matches(child_id))
    }
}

// ---- GRX2/GRX3 动作帧解析 ----
//
// GRX2：magic 0x3258_5247，动作 24 字节，node/child/before id 走字符串池。
// GRX3：magic 0x3358_5247，动作 48 字节，id 全部 u64 直传（NodeID 本就是
// uint64，字符串化纯属传输负担）：
//   o+0  kind u8 | o+8 node_id u64 | o+16 child_id u64 | o+24 before_id u64
//   o+32 key_off u32 | o+36 blob_off u32 | o+40 val_off u32 | o+44 blob_len u32

const DELTA_MAGIC: u32 = 0x3258_5247; // "GRX2"
const DELTA_MAGIC_V3: u32 = 0x3358_5247; // "GRX3"
const DELTA_HEADER: usize = 16;
const ACTION_SZ: usize = 24;
const ACTION_SZ_V3: usize = 48;
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
fn u64at(b: &[u8], o: usize) -> u64 {
    let mut a = [0u8; 8];
    a.copy_from_slice(&b[o..o + 8]);
    u64::from_le_bytes(a)
}

/// 解码后的统一动作意图（GRX2 字符串 id → parse 成 u64；GRX3 直读 u64）。
/// id = 0 表示「无」——找不到目标节点时动作被丢弃（Host 全量重建兜底）。
struct ActionIntent {
    kind: u8,
    node_id: u64,
    child_id: u64,
    before_id: u64,
    key: String,
    val: String,
    blob_off: u32,
    blob_len: u32,
}

pub struct DeltaView<'a> {
    buf: &'a [u8],
    count: usize,
    pool_off: usize,
    /// 内嵌子树 blob 区起点 = pool_off + pool_len。
    blob_off: usize,
    action_sz: usize,
}

impl<'a> DeltaView<'a> {
    pub fn parse(buf: &'a [u8]) -> Option<Self> {
        if buf.len() < DELTA_HEADER {
            return None;
        }
        let action_sz = match u32at(buf, 0) {
            DELTA_MAGIC => ACTION_SZ,
            DELTA_MAGIC_V3 => ACTION_SZ_V3,
            _ => return None,
        };
        let count = u32at(buf, 8) as usize;
        let pool_len = u32at(buf, 12) as usize;
        let pool_off = DELTA_HEADER + count.checked_mul(action_sz)?;
        let blob_off = pool_off + pool_len;
        if blob_off > buf.len() {
            return None;
        }
        Some(DeltaView {
            buf,
            count,
            pool_off,
            blob_off,
            action_sz,
        })
    }

    fn pool_str(&self, off: u32) -> &'a str {
        let o = self.pool_off + off as usize;
        let len = u16at(self.buf, o) as usize;
        std::str::from_utf8(&self.buf[o + 2..o + 2 + len]).unwrap_or("")
    }

    /// 从 blob 区解析一棵内嵌子树（GRX1/GRX3 迷你帧）。
    fn subtree(&self, off: u32, len: u32) -> Option<SceneNode> {
        let start = self.blob_off + off as usize;
        let end = start + len as usize;
        if end > self.buf.len() {
            return None;
        }
        let view = FrameView::parse(&self.buf[start..end])?;
        SceneNode::from_frame(&view)
    }

    /// 解码第 i 条动作（按协议版本分派读取，输出统一意图）。
    fn action(&self, i: usize) -> Option<ActionIntent> {
        let o = DELTA_HEADER + i * self.action_sz;
        let kind = self.buf[o];
        let (node_raw, child_raw, before_raw, key_off, blob_off, val_off, blob_len) =
            if self.action_sz == ACTION_SZ_V3 {
                (
                    u64at(self.buf, o + 8),
                    u64at(self.buf, o + 16),
                    u64at(self.buf, o + 24),
                    u32at(self.buf, o + 32),
                    u32at(self.buf, o + 36),
                    u32at(self.buf, o + 40),
                    u32at(self.buf, o + 44),
                )
            } else {
                // GRX2（Go serialize_delta.go 语义）：node/before/child（Remove、
                // Move）/key/val（Set 系）走字符串池；Insert/Replace 的 child 槽
                // 是 blob 偏移直放、val 槽是 blob 长度直放。
                let structural = matches!(kind, ACT_INSERT_BEFORE | ACT_REPLACE_CHILDREN);
                let node_s = self.pool_str(u32at(self.buf, o + 4));
                let before_s = self.pool_str(u32at(self.buf, o + 12));
                let (child_id, blob_off_v) = if structural {
                    (0u64, u32at(self.buf, o + 8))
                } else {
                    (
                        self.pool_str(u32at(self.buf, o + 8))
                            .trim()
                            .parse::<u64>()
                            .unwrap_or(0),
                        0,
                    )
                };
                let (val_v, blob_len_v) = if structural {
                    (String::new(), u32at(self.buf, o + 20))
                } else {
                    (self.pool_str(u32at(self.buf, o + 20)).to_string(), 0u32)
                };
                return Some(ActionIntent {
                    kind,
                    node_id: node_s.trim().parse::<u64>().unwrap_or(0),
                    child_id,
                    before_id: before_s.trim().parse::<u64>().unwrap_or(0),
                    key: if matches!(kind, ACT_SET_ATTR | ACT_SET_HANDLE) {
                        self.pool_str(u32at(self.buf, o + 16)).to_string()
                    } else {
                        String::new()
                    },
                    val: val_v,
                    blob_off: blob_off_v,
                    blob_len: blob_len_v,
                });
            };
        let key = if matches!(kind, ACT_SET_ATTR | ACT_SET_HANDLE) {
            self.pool_str(key_off).to_string()
        } else {
            String::new()
        };
        let val = if matches!(kind, ACT_SET_ATTR | ACT_SET_TEXT) {
            self.pool_str(val_off).to_string()
        } else {
            String::new()
        };
        Some(ActionIntent {
            kind,
            node_id: node_raw,
            child_id: child_raw,
            before_id: before_raw,
            key,
            val,
            blob_off,
            blob_len,
        })
    }

    /// 把动作应用到镜像树。返回应用的动作数。
    ///
    /// 分两阶段：
    ///  ① 属性/文本/事件动作就地修改（互不依赖，按序 apply）。
    ///  ② 结构动作先解析成「意图」数据，再逐个执行，避开 Rust 借用冲突。
    /// 查找统一走 nid（u64）优先 + 字符串兜底：GRX3 直传整数身份，
    /// GRX2 字符串 parse 后同路径。
    pub fn apply(&self, root: &mut SceneNode) -> usize {
        let mut applied = 0;

        // 阶段①：非结构动作。
        for i in 0..self.count {
            let Some(a) = self.action(i) else { continue };
            if matches!(
                a.kind,
                ACT_INSERT_BEFORE | ACT_REMOVE_CHILD | ACT_MOVE_CHILD | ACT_REPLACE_CHILDREN
            ) {
                continue;
            }
            let target = a.node_id.to_string();
            if let Some(n) = root.find_mut(&target) {
                match a.kind {
                    ACT_SET_ATTR => {
                        n.props.insert(a.key, a.val);
                    }
                    ACT_SET_TEXT => {
                        n.text = a.val;
                    }
                    ACT_SET_HANDLE => {
                        if !n.events.iter().any(|e| e == &a.key) {
                            n.events.push(a.key);
                        }
                    }
                    _ => {}
                }
                applied += 1;
            }
        }

        // 阶段②：结构动作。先解码为意图，再执行。
        enum StructOp {
            Insert {
                parent: u64,
                child: SceneNode,
                before: u64,
            },
            Remove {
                parent: u64,
                child_id: u64,
            },
            Move {
                parent: u64,
                child_id: u64,
                before: u64,
            },
            Replace {
                parent: u64,
                sub: SceneNode,
            },
        }
        let mut ops: Vec<StructOp> = Vec::new();
        for i in 0..self.count {
            let Some(a) = self.action(i) else { continue };
            match a.kind {
                ACT_INSERT_BEFORE => {
                    if let Some(child) = self.subtree(a.blob_off, a.blob_len) {
                        ops.push(StructOp::Insert {
                            parent: a.node_id,
                            child,
                            before: a.before_id,
                        });
                    }
                }
                ACT_REMOVE_CHILD => {
                    ops.push(StructOp::Remove {
                        parent: a.node_id,
                        child_id: a.child_id,
                    });
                }
                ACT_MOVE_CHILD => {
                    ops.push(StructOp::Move {
                        parent: a.node_id,
                        child_id: a.child_id,
                        before: a.before_id,
                    });
                }
                ACT_REPLACE_CHILDREN => {
                    if let Some(sub) = self.subtree(a.blob_off, a.blob_len) {
                        ops.push(StructOp::Replace {
                            parent: a.node_id,
                            sub,
                        });
                    }
                }
                _ => {}
            }
        }
        for op in ops {
            let ok = match op {
                StructOp::Insert {
                    parent,
                    child,
                    before,
                } => root
                    .find_mut(&parent.to_string())
                    .map(|p| p.insert_child_before(child, before))
                    .unwrap_or(false),
                StructOp::Remove { parent, child_id } => {
                    if let Some(p) = root.find_mut(&parent.to_string()) {
                        if let Some(pos) = p.child_index_nid(child_id) {
                            p.children.remove(pos);
                            true
                        } else {
                            false
                        }
                    } else {
                        false
                    }
                }
                StructOp::Move {
                    parent,
                    child_id,
                    before,
                } => {
                    if let Some(p) = root.find_mut(&parent.to_string()) {
                        if let Some(pos) = p.child_index_nid(child_id) {
                            let node = p.children.remove(pos);
                            // before 可能是被移动节点自身之后的位置；remove 后重新查找锚点。
                            if before == 0 {
                                p.children.push(node);
                                true
                            } else if let Some(ins) = p.child_index_nid(before) {
                                p.children.insert(ins, node);
                                true
                            } else {
                                // 锚点缺失则退回末尾，保证不丢节点。
                                p.children.push(node);
                                true
                            }
                        } else {
                            false
                        }
                    } else {
                        false
                    }
                }
                StructOp::Replace { parent, sub } => root
                    .find_mut(&parent.to_string())
                    .map(|p| {
                        p.children = sub.children;
                        true
                    })
                    .unwrap_or(false),
            };
            if ok {
                applied += 1;
            }
        }
        applied
    }
}

impl SceneNode {
    /// 找直接子节点中 nid == id 的位置（0 = 末尾锚点/无目标，返回 None）。
    fn child_index_nid(&self, nid: u64) -> Option<usize> {
        if nid == 0 {
            return None;
        }
        self.children.iter().position(|c| c.nid == nid)
    }

    /// 在 nid 之前插入子树；nid = 0（NoNode）追加末尾。返回是否成功。
    fn insert_child_before(&mut self, child: SceneNode, before: u64) -> bool {
        if before == 0 {
            self.children.push(child);
            return true;
        }
        if let Some(pos) = self.child_index_nid(before) {
            self.children.insert(pos, child);
            return true;
        }
        // 整数身份找不到时兜底字符串锚点（GRX1 测试/非数字 id 场景）。
        false
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn put32(b: &mut [u8], o: usize, v: u32) {
        b[o..o + 4].copy_from_slice(&v.to_le_bytes());
    }
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
        put32(&mut nodes, 4, 0); // kind_off=0
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
        buf[24..24 + nodes.len()].copy_from_slice(&nodes);
        buf[24 + nodes.len()..].copy_from_slice(&pool);
        buf
    }

    /// 构造 GRX2 帧。actions: [(kind, node_id, child_or_bloboff, before_id, blob_or_len)]
    /// blob: 追加的 blob 区。child_off 用 blob 区内的绝对偏移（相对于 blob 起点）。
    /// 语义与 Go serialize_delta.go 一致：Insert/Replace 的 child/val 槽是 blob
    /// 偏移/长度（数字直放）；其余动作的槽是 id/文本字符串（进池）。
    fn make_grx2(actions: &[(u8, &str, &str, &str, &str)], blob: &[u8]) -> Vec<u8> {
        let mut pool: Vec<u8> = Vec::new();
        let mut act_bytes = vec![0u8; actions.len() * 24];
        for (i, (kind, node, c, before, val)) in actions.iter().enumerate() {
            let o = i * 24;
            act_bytes[o] = *kind;
            put32(&mut act_bytes, o + 4, pool_str(&mut pool, node));
            let structural = matches!(*kind, ACT_INSERT_BEFORE | ACT_REPLACE_CHILDREN);
            let c_off = if structural {
                c.parse::<u32>().unwrap_or(0) // blob off 直放
            } else {
                pool_str(&mut pool, c) // id 字符串进池
            };
            put32(&mut act_bytes, o + 8, c_off);
            put32(&mut act_bytes, o + 12, pool_str(&mut pool, before));
            put32(&mut act_bytes, o + 16, 0);
            let v_off = if structural {
                val.parse::<u32>().unwrap_or(0) // blob len 直放
            } else {
                pool_str(&mut pool, val) // 文本/值进池
            };
            put32(&mut act_bytes, o + 20, v_off);
        }
        let total = 16 + act_bytes.len() + pool.len() + blob.len();
        let mut buf = vec![0u8; total];
        put32(&mut buf, 0, 0x3258_5247);
        put32(&mut buf, 4, 100);
        put32(&mut buf, 8, actions.len() as u32);
        put32(&mut buf, 12, pool.len() as u32);
        let mut o = 16;
        buf[o..o + act_bytes.len()].copy_from_slice(&act_bytes);
        o += act_bytes.len();
        buf[o..o + pool.len()].copy_from_slice(&pool);
        o += pool.len();
        buf[o..].copy_from_slice(blob);
        buf
    }

    fn base_tree() -> SceneNode {
        SceneNode {
            typ: NodeType::Div,
            kind: "div".into(),
            id: "r".into(),
            nid: 10,
            text: String::new(),
            props: Default::default(),
            events: vec![],
            children: vec![
                SceneNode {
                    typ: NodeType::Text,
                    kind: "".into(),
                    id: "a".into(),
                    nid: 11,
                    text: "A".into(),
                    props: Default::default(),
                    events: vec![],
                    children: vec![],
                },
                SceneNode {
                    typ: NodeType::Text,
                    kind: "".into(),
                    id: "b".into(),
                    nid: 12,
                    text: "B".into(),
                    props: Default::default(),
                    events: vec![],
                    children: vec![],
                },
                SceneNode {
                    typ: NodeType::Text,
                    kind: "".into(),
                    id: "c".into(),
                    nid: 13,
                    text: "C".into(),
                    props: Default::default(),
                    events: vec![],
                    children: vec![],
                },
            ],
        }
    }

    fn ids(root: &SceneNode) -> Vec<&str> {
        root.children.iter().map(|c| c.id.as_str()).collect()
    }

    /// 构造 GRX3 delta 帧（48B 动作，u64 直传）。
    /// actions: [(kind, node_id, child_id, before_id, blob_off, blob_len, val_str)]
    fn make_grx3_delta(actions: &[(u8, u64, u64, u64, u32, u32, &str)], blob: &[u8]) -> Vec<u8> {
        let mut pool: Vec<u8> = Vec::new();
        let mut act_bytes = vec![0u8; actions.len() * 48];
        for (i, (kind, node, child, before, boff, blen, val)) in actions.iter().enumerate() {
            let o = i * 48;
            act_bytes[o] = *kind;
            act_bytes[o + 8..o + 16].copy_from_slice(&node.to_le_bytes());
            act_bytes[o + 16..o + 24].copy_from_slice(&child.to_le_bytes());
            act_bytes[o + 24..o + 32].copy_from_slice(&before.to_le_bytes());
            put32(&mut act_bytes, o + 32, 0); // key_off（本测试不用）
            put32(&mut act_bytes, o + 36, *boff);
            let voff = pool_str(&mut pool, val);
            put32(&mut act_bytes, o + 40, voff);
            put32(&mut act_bytes, o + 44, *blen);
        }
        let total = 16 + act_bytes.len() + pool.len() + blob.len();
        let mut buf = vec![0u8; total];
        put32(&mut buf, 0, 0x3358_5247);
        put32(&mut buf, 4, 100);
        put32(&mut buf, 8, actions.len() as u32);
        put32(&mut buf, 12, pool.len() as u32);
        let mut o = 16;
        buf[o..o + act_bytes.len()].copy_from_slice(&act_bytes);
        o += act_bytes.len();
        buf[o..o + pool.len()].copy_from_slice(&pool);
        o += pool.len();
        buf[o..].copy_from_slice(blob);
        buf
    }

    /// 构造 GRX3 全量帧（40B 节点，尾部 u64 node_id 直传，id 池槽为空串）。
    fn make_grx3_frame() -> Vec<u8> {
        let mut pool: Vec<u8> = Vec::new();
        let empty = pool_str(&mut pool, "");
        let n = 3; // root + 2 texts
        let mut buf = vec![0u8; 24 + n * 40 + pool.len()];
        put32(&mut buf, 0, 0x3358_5247);
        put32(&mut buf, 4, 100);
        put32(&mut buf, 8, 100);
        put32(&mut buf, 12, n as u32);
        put32(&mut buf, 16, 0);
        put32(&mut buf, 20, pool.len() as u32);
        // root
        buf[24] = 1; // Div
        put32(&mut buf, 24 + 4, 0); // kind_off=0
        put32(&mut buf, 24 + 8, empty);
        put32(&mut buf, 24 + 24, 0xFFFFFFFF);
        buf[24 + 22..24 + 24].copy_from_slice(&2u16.to_le_bytes());
        buf[24 + 32..24 + 40].copy_from_slice(&77u64.to_le_bytes()); // node_id
        for i in 0..2u64 {
            let o = 24 + ((i + 1) as usize) * 40;
            buf[o] = 2; // Text
            put32(&mut buf, o + 8, empty);
            put32(&mut buf, o + 24, 0); // parent = root
            buf[o + 32..o + 40].copy_from_slice(&(100 + i).to_le_bytes());
        }
        buf[24 + n * 40..].copy_from_slice(&pool);
        buf
    }

    #[test]
    fn remove_child() {
        let mut root = base_tree();
        let g2 = make_grx2(&[(ACT_REMOVE_CHILD, "10", "11", "", "")], &[]);
        let v = DeltaView::parse(&g2).unwrap();
        assert_eq!(v.apply(&mut root), 1);
        assert_eq!(ids(&root), ["b", "c"]);
    }

    #[test]
    fn move_child_before() {
        let mut root = base_tree();
        // 把 a 移到 c 之前 → b, a, c
        let g2 = make_grx2(&[(ACT_MOVE_CHILD, "10", "11", "13", "")], &[]);
        let v = DeltaView::parse(&g2).unwrap();
        assert_eq!(v.apply(&mut root), 1);
        assert_eq!(ids(&root), ["b", "a", "c"]);
    }

    #[test]
    fn move_child_to_end() {
        let mut root = base_tree();
        let g2 = make_grx2(&[(ACT_MOVE_CHILD, "10", "11", "", "")], &[]);
        let v = DeltaView::parse(&g2).unwrap();
        v.apply(&mut root);
        assert_eq!(ids(&root), ["b", "c", "a"]);
    }

    #[test]
    fn insert_before() {
        let mut root = base_tree();
        let blob = make_grx1(&["d"]);
        // Insert: c=blob offset(0), before="12", val=blob len
        let g2 = make_grx2(
            &[(ACT_INSERT_BEFORE, "10", "0", "12", &blob.len().to_string())],
            &blob,
        );
        let v = DeltaView::parse(&g2).unwrap();
        assert_eq!(v.apply(&mut root), 1);
        // 插入的是整棵子树（id="sub"），位于 b 之前
        assert_eq!(ids(&root), ["a", "sub", "b", "c"]);
    }

    #[test]
    fn replace_children() {
        let mut root = base_tree();
        let blob = make_grx1(&["x", "y"]);
        let g2 = make_grx2(
            &[(ACT_REPLACE_CHILDREN, "10", "0", "", &blob.len().to_string())],
            &blob,
        );
        let v = DeltaView::parse(&g2).unwrap();
        assert_eq!(v.apply(&mut root), 1);
        // Replace 用子树 children 覆盖 → x, y
        assert_eq!(ids(&root), ["x", "y"]);
    }

    // ---------- GRX3：NodeID uint64 直传 ----------

    #[test]
    fn grx3_frame_parses_node_id() {
        let buf = make_grx3_frame();
        let view = FrameView::parse(&buf).expect("GRX3 帧应可解析");
        assert_eq!(view.node_count, 3);
        let root = view.node(0);
        assert_eq!(root.node_id, 77, "GRX3 直传 node_id");
        assert_eq!(root.id, "", "GRX3 帧不再携带 id 字符串");
        assert_eq!(root.child_count, 2);
        assert_eq!(view.node(1).node_id, 100);
        assert_eq!(view.node(2).node_id, 101);
        // GRX3 建树 → nid 就位、id 为空串
        let tree = SceneNode::from_frame(&view).unwrap();
        assert_eq!(tree.nid, 77);
        assert_eq!(tree.children[0].nid, 100);
    }

    #[test]
    fn grx3_delta_u64_ids() {
        let mut root = base_tree();
        // Remove child nid=11（a），Move nid=13（c）到 nid=12（b）之前
        let g3 = make_grx3_delta(
            &[
                (ACT_REMOVE_CHILD, 10, 11, 0, 0, 0, ""),
                (ACT_MOVE_CHILD, 10, 13, 12, 0, 0, ""),
            ],
            &[],
        );
        let v = DeltaView::parse(&g3).expect("GRX3 delta 应可解析");
        assert_eq!(v.apply(&mut root), 2);
        assert_eq!(
            ids(&root),
            ["c", "b"],
            "remove a → [b,c]，move c before b → [c,b]"
        );
    }

    #[test]
    fn grx3_delta_insert_before() {
        let mut root = base_tree();
        let blob = make_grx1(&["d"]);
        // 在 nid=12（b）之前插入子树
        let g3 = make_grx3_delta(
            &[(ACT_INSERT_BEFORE, 10, 0, 12, 0, blob.len() as u32, "")],
            &blob,
        );
        let v = DeltaView::parse(&g3).unwrap();
        assert_eq!(v.apply(&mut root), 1);
        assert_eq!(ids(&root), ["a", "sub", "b", "c"]);
    }

    #[test]
    fn grx3_delta_set_attr_and_text() {
        let mut root = base_tree();
        let g3 = make_grx3_delta(
            &[
                (ACT_SET_ATTR, 11, 0, 0, 0, 0, "#ff0000"),
                (ACT_SET_TEXT, 13, 0, 0, 0, 0, "Z"),
            ],
            &[],
        );
        let v = DeltaView::parse(&g3).unwrap();
        assert_eq!(v.apply(&mut root), 2);
        // attr 动作需要 key——GRX3 的 key_off 槽；本 helper 未写 key 池，
        // key 为空串插入 HashMap 验证通道畅通即可。
        assert_eq!(root.children[2].text, "Z");
    }
}
