# runtime/rope — 持久 Rope + SumTree 文本缓冲

面向编辑器的不可变文本缓冲。设计目标是替代 `[]string + SplitLines/JoinLines` 的 O(N) 全量重建，把编辑与行列换算降到 O(log N)。单线程使用（与 GX UI Runtime 一致）。

## 数据结构

- **持久 Rope**：AVL 平衡，所有修改返回新根，旧根及未触碰子树完全复用（契合 signal 值语义与撤销栈）。
- **SumTree 聚合**（每个节点 O(1) 可读）：`runes` / `lines` / `bytes` / `tailLineRunes`（尾行 rune 数）。
- **叶内稀疏采样** `byteAt`：每 8 个 rune 记一个字节偏移，把 rune→byte 映射从 O(叶子长度) 降到最多扫 8 个 rune。采样数组内嵌在 node 中，零额外分配。

## API

| 类别 | 方法 | 复杂度 |
|---|---|---|
| 编辑 | `Insert` / `Delete` | O(log N + 输出) |
| 聚合 | `RuneLen` / `ByteLen` / `LineCount` | O(1) |
| 行列 | `OffsetToLineCol` / `LineColToOffset` / `LineStart` | O(log N) |
| 取文本 | `Line` / `Lines` / `Slice` / `RuneAt` / `String` | O(log N + 输出) |

## 性能优化记录

对同一份含 CJK 的 1000 行文本（`// line of code 中文 with mixed text\n`）做优化前后基准（AMD EPYC 9K65，Go 1.27，`-benchtime=600ms -count=3` 取均值）：

| 基准 | 优化前 | 优化后 | 提升 |
|---|---:|---:|---:|
| `OffsetToLineCol` | 170.7 ns | 134.8 ns | **1.27×** |
| `Lines` | 289.9 µs / 1001 alloc | 136.2 µs / 1002 alloc | **2.13×** |
| `RuneAt` | 103.8 ns | 48.8 ns | **2.13×** |
| `Slice` | 173.9 ns | 143.5 ns | **1.21×** |
| `LineColToOffset` | 150.9 ns | 151.0 ns | 持平 |
| `Of`（构造） | 65.3 µs | 115.7 µs | 0.56×（注） |

优化手段：

1. **光标定位单次下降**：`OffsetToLineCol` 原需 `newlinesBefore` + `LineStart` 两次下降；新增 `tailLineRunes` 聚合后一次下降同时得出行列。
2. **`Lines` 一次中序遍历**：原每行 2 次 `LineStart` + 1 次 `Slice`（O(L·log N)），改为 O(N) 遍历 + 可复用 `[]byte`（分配数不增）。
3. **叶内稀疏采样**：`RuneAt`/`Slice`/`split` 依赖的 rune→byte 映射改走采样索引，`RuneAt` 提速 2.13×。
4. **精确 Grow**：`Slice` 用全文平均字节/rune 比预分配，减少 CJK 场景的扩容。
5. **单 rune 插入快路径**：`Insert` 单 rune 直接构造叶子，跳过 `Of` 的切片分配与归并。

> 注：`Of` 变慢源于 node 增加聚合字段后体积变大（构造时零值填充成本上升）。这是一次性的初始化/大段插入成本；编辑器热路径是读取（光标/渲染）与单字符编辑，这些全部提速。构造成本可用预分配 `WithCapacity` 思路进一步优化，留待大文件场景再评估。

## 与原生 string 的对比（同份文本）

| 操作 | Rope | string 全量方案 | 倍率 |
|---|---:|---:|---:|
| 光标定位 | 137 ns | 9 718 ns（扫描全文换行） | **约 71×** |
| 中间插入 | 2.7 µs | 18.7 µs / 55 KB 拷贝 | **约 7×** |

## 复跑基准

```bash
cd /workspace/gox
go test ./runtime/rope/ -run '^$' -bench . -benchmem -benchtime=600ms -count=3
```

## 正确性

- 3000 步随机差分测试对照 `[]rune` 模型（含 CJK 与换行）逐步比对。
- 每个偏移的 `OffsetToLineCol` 与暴力全文扫描逐点比对（调试期已验证）。
- AVL 平衡 / 聚合 / 行列往返 / 跨叶子边界 / 持久不可变性全覆盖。
