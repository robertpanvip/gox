//! 二进制场景协议解析（零拷贝）：直接把字节 buffer 当结构数组读，无反序列化分配。
//! 布局见 PROTOCOL.md，与 Go serialize_bin.go 对齐。

const MAGIC: u32 = 0x3158_5247; // "GRX1"
const HEADER: usize = 24;
const NODE_SZ: usize = 32;
const PROP_SZ: usize = 12;
const EVENT_SZ: usize = 12;

fn u32at(b: &[u8], o: usize) -> u32 {
    u32::from_le_bytes([b[o], b[o + 1], b[o + 2], b[o + 3]])
}
fn u16at(b: &[u8], o: usize) -> u16 {
    u16::from_le_bytes([b[o], b[o + 1]])
}

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum NodeType {
    Fragment,
    Div,
    Text,
}

/// 节点视图（借用 buffer，零拷贝）。
#[derive(Clone, Copy)]
pub struct NodeView<'a> {
    pub typ: NodeType,
    pub has_events: bool,
    pub kind: &'a str,
    pub id: &'a str,
    pub text: &'a str,
    pub prop_start: usize,
    pub prop_count: usize,
    pub child_count: usize,
    pub parent: u32,
}

/// 属性视图。
#[derive(Clone, Copy)]
pub struct PropView<'a> {
    pub key: &'a str,
    pub val: &'a str,
}

/// 事件视图：事件名 + 目标节点索引。
#[derive(Clone, Copy)]
pub struct EventView<'a> {
    pub node_index: usize,
    pub name: &'a str,
}

/// 零拷贝帧视图。
pub struct FrameView<'a> {
    buf: &'a [u8],
    pub width: u32,
    pub height: u32,
    pub node_count: usize,
    pub prop_count: usize,
    pub event_count: usize,
    nodes_off: usize,
    props_off: usize,
    events_off: usize,
    pool_off: usize,
}

impl<'a> FrameView<'a> {
    /// 解析 buffer；返回 None 表示 magic/长度非法。
    pub fn parse(buf: &'a [u8]) -> Option<Self> {
        if buf.len() < HEADER {
            return None;
        }
        if u32at(buf, 0) != MAGIC {
            return None;
        }
        let width = u32at(buf, 4);
        let height = u32at(buf, 8);
        let node_count = u32at(buf, 12) as usize;
        let prop_count = u32at(buf, 16) as usize;
        let pool_len = u32at(buf, 20) as usize;

        // header 没传 event_count：从总长推导（events 段 = 总长 - header - nodes - props - pool）
        let nodes_off = HEADER;
        let props_off = nodes_off + node_count * NODE_SZ;
        let events_bytes = buf
            .len()
            .checked_sub(props_off + prop_count * PROP_SZ + pool_len)?;
        if events_bytes % EVENT_SZ != 0 {
            return None;
        }
        let event_count = events_bytes / EVENT_SZ;
        let events_off = props_off + prop_count * PROP_SZ;
        let pool_off = events_off + event_count * EVENT_SZ;

        // 边界检查
        if pool_off + pool_len > buf.len() {
            return None;
        }
        Some(FrameView {
            buf,
            width,
            height,
            node_count,
            prop_count,
            event_count,
            nodes_off,
            props_off,
            events_off,
            pool_off,
        })
    }

    #[inline]
    fn pool_str(&self, off: u32) -> &'a str {
        let o = self.pool_off + off as usize;
        let len = u16at(self.buf, o) as usize;
        let s = &self.buf[o + 2..o + 2 + len];
        std::str::from_utf8(s).unwrap_or("")
    }

    #[inline]
    pub fn node(&self, i: usize) -> NodeView<'a> {
        let o = self.nodes_off + i * NODE_SZ;
        let typ = match self.buf[o] {
            1 => NodeType::Div,
            2 => NodeType::Text,
            _ => NodeType::Fragment,
        };
        let has_events = self.buf[o + 1] & 0x1 != 0;
        NodeView {
            typ,
            has_events,
            kind: self.pool_str(u32at(self.buf, o + 4)),
            id: self.pool_str(u32at(self.buf, o + 8)),
            text: self.pool_str(u32at(self.buf, o + 12)),
            prop_start: u32at(self.buf, o + 16) as usize,
            prop_count: u16at(self.buf, o + 20) as usize,
            child_count: u16at(self.buf, o + 22) as usize,
            parent: u32at(self.buf, o + 24),
        }
    }

    #[inline]
    pub fn prop(&self, i: usize) -> PropView<'a> {
        let o = self.props_off + i * PROP_SZ;
        PropView {
            key: self.pool_str(u32at(self.buf, o)),
            val: self.pool_str(u32at(self.buf, o + 4)),
        }
    }

    #[inline]
    pub fn event(&self, i: usize) -> EventView<'a> {
        let o = self.events_off + i * EVENT_SZ;
        EventView {
            node_index: u32at(self.buf, o) as usize,
            name: self.pool_str(u32at(self.buf, o + 4)),
        }
    }

    /// 查某节点的属性（线性扫，属性数很少）。
    pub fn prop_value(&self, n: &NodeView<'a>, key: &str) -> Option<&'a str> {
        for i in n.prop_start..n.prop_start + n.prop_count {
            let p = self.prop(i);
            if p.key == key {
                return Some(p.val);
            }
        }
        None
    }
}
