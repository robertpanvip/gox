//! GX 场景树 → Rust 后端的 wire 协议（JSON）。
//!
//! Go 侧在 gxapp.Window.Draw(root) 时，把 *gxscene.Node 树序列化成下面的 SceneNode
//! 结构（JSON），经 C ABI 传进 Rust；Rust 反序列化后重建布局树并渲染。
//!
//! 设计原则：
//!   - 协议只传"结构 + 样式 + 文本 + 事件标记"，不传函数/指针（handler 在 Go 侧）；
//!   - 节点类型对齐 host：fragment(透明容器) / div(元素) / text(文本)；
//!   - props 用字符串键值（host 的 Props 是 map[string]any，序列化时归一化为 string）；
//!   - id 是 host 生成的跨帧稳定标识，Rust 侧用于命中回传。

use serde::{Deserialize, Serialize};
use std::collections::HashMap;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum NodeType {
    /// 透明包装（#root/#frag/组件/分支），不画，透传 children
    Fragment,
    /// 元素节点：Kind 为标签名，Props 为样式
    Div,
    /// 文本节点：读 text
    Text,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SceneNode {
    pub r#type: NodeType,
    #[serde(default)]
    pub kind: String,
    /// host 生成的稳定 id，命中测试回传给 Go
    #[serde(default)]
    pub id: String,
    /// 样式/属性键值（已归一化为字符串）
    #[serde(default)]
    pub props: HashMap<String, String>,
    /// 仅 text 节点使用
    #[serde(default)]
    pub text: String,
    #[serde(default)]
    pub children: Vec<SceneNode>,
    /// 事件名列表（如 ["onClick"]），仅作标记；真正的 handler 在 Go 侧
    #[serde(default)]
    pub events: Vec<String>,
}

/// 一次 Draw 的顶层消息。
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SceneFrame {
    pub width: u32,
    pub height: u32,
    #[serde(default)]
    pub title: String,
    pub root: SceneNode,
}

/// Rust → Go 的事件消息（JSON），经事件回调传回 Go 侧 DispatchBubble。
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct InputEvent {
    /// "click" / "pointer_down" / "pointer_up" / "pointer_move" / "wheel" /
    /// "key" / "ime_preedit" / "ime_commit" / "focus" / "blur" / "backspace"
    pub kind: String,
    /// 命中的 host 节点 id
    #[serde(default)]
    pub node_id: String,
    /// 事件名（click 时为 "onClick" 等）
    #[serde(default)]
    pub event: String,
    /// ime 文本 / preedit
    #[serde(default)]
    pub text: String,
    /// 位置（点击 / 指针 / 滚轮增量）
    #[serde(default)]
    pub x: f32,
    #[serde(default)]
    pub y: f32,
    /// 修饰键（key / pointer 事件）：空串或 "ctrl" / "ctrl+shift+alt+super" 之一。
    /// 独立字段而非布尔位：翻译层按 contains 判断，可读性与跨语言序列化都更稳。
    #[serde(default)]
    pub mods: String,
}

impl InputEvent {
    pub fn to_json(&self) -> String {
        serde_json::to_string(self).unwrap_or_default()
    }
}
