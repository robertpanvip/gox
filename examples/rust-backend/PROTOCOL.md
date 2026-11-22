# GX → Rust 后端 二进制场景协议 (v1)

目标：**零解析开销**。场景树扁平化成定长 C struct 数组 + 字符串池；Rust 侧直接按偏移读取，无需 JSON 解析 / 字符串分配。

## 设计原则

- **host 协议本身是好的**：GX 已有 `gxscene.Node` 全量树与 `gxscene.Change` 增量指令。本协议只解决"如何高效地把这些结构跨 C ABI 传过去"，不改变 host 语义。
- **字符串池去重**：属性名（如 `bg`、`width`）在整棵树里重复几百次，只在池里存一次，节点用 `u32` 索引引用。
- **小端 + 定长**：所有整数 little-endian，struct 无 padding（手动布局），Rust 侧按字节偏移读。

## 内存布局

整个帧是一个连续的字节 buffer，分三段：

```
┌──────────────┬──────────────┬──────────────┬──────────────┬──────────────┐
│ FrameHeader  │ Node[]       │ Prop[]       │ Event[]      │ StringPool   │
│ (24 bytes)   │ (24 B each)  │ (12 B each)  │ (12 B each)  │ (变长 bytes) │
└──────────────┴──────────────┴──────────────┴──────────────┴──────────────┘
```

### FrameHeader (24 B, 小端)

| 偏移 | 类型 | 字段 | 说明 |
|---|---|---|---|
| 0 | u32 | magic | `0x31585247` ("GRX1") |
| 4 | u32 | width | 窗口宽 |
| 8 | u32 | height | 窗口高 |
| 12 | u32 | node_count | 节点数 |
| 16 | u32 | prop_count | 属性数 |
| 20 | u32 | str_pool_len | 字符串池字节数 |

### Node (24 B)

| 偏移 | 类型 | 字段 | 说明 |
|---|---|---|---|
| 0 | u8 | type | 0=fragment 1=div 2=text |
| 1 | u8 | flags | bit0: has_events |
| 2 | u16 | title_len（仅 root） | 保留 |
| 4 | u32 | kind_off | 字符串池偏移 |
| 8 | u32 | id_off | host 稳定 id 池偏移 |
| 12 | u32 | text_off | 文本池偏移（text 节点） |
| 16 | u32 | prop_start | 该节点属性在 Prop[] 的起始索引 |
| 20 | u32 | prop_count | 属性个数 |

子节点关系：节点按 **DFS 前序** 排列，每个节点额外有隐式的 `child_count`（存在保留字段或由下一个兄弟推导）。
为简化，v1 用 **parent 索引 + sibling 顺序**：增加字段见下。

> v1 简化：Node 扩为 32 B，增加 `parent`(u32) 与 `child_count`(u32)，children 为紧跟其后的连续节点（DFS）。

### Node v1 (32 B, 定稿)

| 偏移 | 类型 | 字段 |
|---|---|---|
| 0 | u8 | type (0/1/2) |
| 1 | u8 | flags (bit0 has_events) |
| 2 | u16 | reserved |
| 4 | u32 | kind_off |
| 8 | u32 | id_off |
| 12 | u32 | text_off |
| 16 | u32 | prop_start |
| 20 | u16 | prop_count |
| 22 | u16 | child_count |
| 24 | u32 | parent (节点索引, 0xFFFFFFFF=无) |
| 28 | u32 | reserved2 |

### Prop (12 B)

| 偏移 | 类型 | 字段 |
|---|---|---|
| 0 | u32 | key_off |
| 4 | u32 | val_off |
| 8 | u32 | reserved |

### Event (12 B)

| 偏移 | 类型 | 字段 |
|---|---|---|
| 0 | u32 | node_index |
| 4 | u32 | event_name_off |
| 8 | u32 | reserved |

### StringPool

连续字节，每个字符串用 `u16 长度 + 字节` 前缀（不 NUL 结尾）。所有 off 指向长度字段起点。
相同字符串（属性名、事件名）去重。

## C ABI

```c
// 传入二进制帧；返回 0=ok, 非0=错误
int gx_window_draw_bin(void* handle, const uint8_t* buf, int len);
```

## 性能预期

- 序列化：Go 侧一次遍历 + 字符串池哈希，几百节点 < 50µs
- 传输：一次 C 调用，零拷贝（指针直传）
- 解析：Rust 侧按偏移读取，零分配（view 到 buffer 上）

## v2：增量动作协议 GRX2（retained-mode）

v1 每帧传全量树，简单但 O(N)。v2 采用 **Zed 式 retained-mode**：
Rust 侧常驻一棵镜像场景树 `SceneNode`，Go 只在变化时发**动作**，Rust 对镜像树打补丁。
稳态下每帧只有几十字节，与场景规模无关。

### 工作流

```
首帧: Go serializeFrameBin(GRX1) ──> Rust SceneNode::from_frame() 建镜像树
后续: Go app.TakeChanges() ──> serializeDelta(GRX2) ──> Rust DeltaView::apply(&mut root)
      └─ 结构动作(Insert/Remove/Move/Replace) ──> 子树打成内嵌 GRX1 blob 随帧发送
```

### 内存布局

```
┌──────────────┬──────────────┬──────────────┬──────────────┐
│ DeltaHeader  │ Action[]     │ StringPool   │ SubtreeBlobs │
│ (16 bytes)   │ (24 B each)  │ (变长 bytes) │ (变长 bytes) │
└──────────────┴──────────────┴──────────────┴──────────────┘
```

#### DeltaHeader (16 B, 小端)

| 偏移 | 类型 | 字段 | 说明 |
|---|---|---|---|
| 0 | u32 | magic | `0x32585247` ("GRX2") |
| 4 | u32 | width | 窗口宽 |
| 8 | u32 | action_count | 动作数 |
| 12 | u32 | pool_len | StringPool 字节数（blob 区长度由此推导） |

帧尾部在 StringPool 之后还有一段 **SubtreeBlobs** 区，用于携带结构动作的子树：
每个 blob 是一个**内嵌的迷你 GRX1 帧**（完整 24B header + 节点/属性/事件/字符串池）。
blob 区总长 = `len(buf) - (DELTA_HEADER + action_count*24 + pool_len)`，Rust 侧按此推导，Header 无需新增字段。

#### Action (24 B)

| 偏移 | 类型 | 字段 | 说明 |
|---|---|---|---|
| 0 | u8 | code | 动作类型（见下） |
| 1 | u8 | flags | 保留 |
| 2 | u16 | reserved | |
| 4 | u32 | node_id_off | 目标节点 host id（字符串池偏移） |
| 8 | u32 | arg1 | 参数1：字符串池偏移 **或** blob 区偏移 |
| 12 | u32 | arg2 | 参数2：字符串池偏移（锚点 id） |
| 16 | u32 | arg3 | 参数3：字符串池偏移（key） |
| 20 | u32 | arg4 | 参数4：字符串池偏移 **或** blob 长度 |

#### 动作类型

| code | 名称 | 参数 | 语义 |
|---|---|---|---|
| 0 | SetAttr | node_id, arg3=key, arg4=value | 更新目标节点属性（覆盖/新增） |
| 1 | SetText | node_id, arg4=text | 更新 text 节点文本 |
| 2 | SetHandler | node_id, arg3=event | 更新事件处理器标记 |
| 3 | InsertBefore | node_id=父, arg1=blob_off, arg2=before_id, arg4=blob_len | 解析 blob 子树，插到 before_id 之前（空=末尾） |
| 4 | RemoveChild | node_id=父, arg1=child_id | 移除指定子节点 |
| 5 | MoveChild | node_id=父, arg1=child_id, arg2=before_id | 移动已有子节点到 before_id 之前（空=末尾） |
| 6 | ReplaceChildren | node_id=父, arg1=blob_off, arg4=blob_len | 用 blob 子树的 children 整体替换目标 children |

StringPool 编码与 v1 相同（`u16 长度 + 字节`）。

### 当前实现与权衡

- **全部 7 种动作已实现**：属性/文本/事件 3 种 + 结构 4 种（InsertBefore / RemoveChild / MoveChild / ReplaceChildren）。
- **子树携带**：`InsertBefore` 的 `Value(*Node)` 与 `ReplaceChildren`（host 传 nil，改从最新 root 按 NodeID 重读）
  都被编码成内嵌 GRX1 blob；Rust 侧 `DeltaView::subtree()` 递归复用 `SceneNode::from_frame` 解析后挂接。
- **回退路径**：仅当 Change.Value 既非 `*Node`、又无法从 root 按 id 定位子树时才回退 GRX1 全量（防御性，正常 host 变更不触发）。
- **事件回传**：Rust → Go 仍走 JSON（`InputEvent`，事件低频，JSON 的解析开销可忽略）。

### C ABI（不变）

两个协议共用同一入口，Rust 通过 magic 自动分辨 v1/v2：

```c
// buf 前缀 magic: 0x31585247=GRX1 全量, 0x32585247=GRX2 增量
int gx_window_draw(void* handle, const uint8_t* buf, int len);
```
