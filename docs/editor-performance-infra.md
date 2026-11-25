# Editor 接入性能基础设施：Rope + VEC + 虚拟滚动 + 惰性解析

> 状态：已实现（2026-11-25）
> 相关：`runtime/rope/`、`runtime/signal/slice.go`（Vec）、`widgets/virtualList.gx`、`widgets/editor.gx`

---

## 1. 背景

editor 此前是 GX 里最大的性能债，三项已建好的基础设施全部闲置：

| 基础设施 | 已具备 | editor 改造前 |
| --- | --- | --- |
| `runtime/rope` + SumTree | O(log N) 编辑/行列换算、持久结构共享 | 零引用；文本是 `Signal[string]`，每个按键 `splitLines`+`joinLines` 全量 O(N) |
| `signal.Vec[T]` 自适应 | 小数据 `[]Signal[T]`、>64 自动切 pvector 大后端 | `rowVers` 写死 24 槽，永远走小后端，大后端路径从未触发 |
| `VirtualList` | 视口窗口化，节点数与数据量解耦（Table 已接） | 视图写死 `indices(24)`，第 25 行以后根本看不见；滚轮事件也没接 |

三者是一条依赖链：只有虚拟滚动让行数上千 → Vec 才切大后端 → Rope 的 O(log N) 才有意义。故本次一次性接通全链。

---

## 2. 改造内容

### 2.1 Rope 文本权威（`widgets/editor.gx`）
- `EditorState` 新增 `doc *rope.Rope`，与 `sourceCode Signal[string]` 在 `setSourceFromRope` 同步；对外 string API（`Text()` 等）零变化。
- 新增一套 rope 编辑核心 `ropeInsertText / ropeDeleteRange / ropeBackspaceAt / ropeNewlineAt / ropeDeleteForward`，实例命令（Insert/Backspace/Newline/DeleteForward/Paste/选区删除）全部改走它。
- 行列换算：`moveCursorRel / clampLineCol / MoveWord / SelectAll / SelectLine / SelectedText` 改用 `LineCount/Line/LineColToOffset/OffsetToLineCol`，不再全文 split。
- 撤销栈 `editSnapshot` 新增 `Doc` 字段：持久结构共享让快照从 O(N) 全文拷贝降为 O(1) 根指针赋值；保留 `Text` 兼容老快照。
- **正确性保障**：`widgets/editor_rope_test.go` 用 string 原实现作 oracle，定点用例 + 200×60 随机操作序列对拍，逐字节等价。

### 2.2 VEC 大后端真正生效
- 去掉 `rowVersion/bumpRow` 的 24 行钳制；`rowVers` 通过 `SetLen(lineCount)` 随文档行数伸缩，>64 行自动 `promote()` 到 pvector。
- `signal.Vec.IsBig()` 新增诊断方法，供验证自适应是否生效。
- `TestEditorVecPromotesOnLongDoc` 验证 3 行走小后端、200 行走大后端。

### 2.3 惰性 token 解析（首屏 O(1)）
- `NewEditorState` 不再全文 tokenize；`lineTextCache/tokenCache/textCache` 按行数预留，`nil` token 表示该行未解析。
- `rebuildTokenCache` 只更新行文本快照、把变化行 token 置 nil 失效，返回变化行索引供行版本 bump。
- `lineText/lineCodeGradient/lineRichText/LineText` 首次读取时经 `ensureLineParsed` 现场 highlight 并缓存——**只有进入可见窗口的行才解析**。
- `TestEditorLazyTokenize` 验证：构造 300 行后 0 行解析；读第 150 行仅解析该行；编辑后仅失效该行。

### 2.4 虚拟滚动 + 受控 scrollTop
- `Editor` 视图从固定 `indices(24)` 的 `<For>` 改为 `VirtualList`（`count=lineCount`、固定 `item-height=22` 显式模式零回流、`overscan=3`、`render-item=lineView`）。
- `EditorState` 新增 `scrollTop Signal[float64]` + 非响应式镜像 `scrollTopVal`；`SetScrollTop/ScrollBy/ScrollLines` 写回。
- 光标跟随：`installCursorFollow` effect 仅订阅 `cursorLine/lineCount`，读镜像 `scrollTopVal`（不读 signal，避免与 on-scroll 形成回环），光标出视口自动滚动。
- caret 绝对位置 `caretTopVisible = caretTop - scrollTop` 补偿容器上移。
- `TestEditorVirtualScrollNodeCount` 验证 300 行文档场景树节点数远小于行数。

### 2.5 三端滚轮事件
- 桌面：`glwindow.go` 加 `glfw.SetScrollCallback` → `ui.EditorScrollLines(-yoff*3)`。
- Web：`main.ts` 加 `wheel` 监听（deltaMode 统一折算行数）→ `api.scrollLines()`；`main.go` 暴露该 API；`gox.d.ts` 补类型；走 build.sh 重新生成 dist/main.js 与单文件 playground.html。
- 非 GUI 后端（headless / skia 离屏 / Rust）无输入源，不受影响。

---

## 3. 性能对比（实测，AMD EPYC 9K65）

中间行插入一个字符（最常见编辑热点）：

| 文档规模 | string 全量 | Rope | 提升 |
| --- | --- | --- | --- |
| 10 行 | 646 ns / 496 B | 7341 ns / 2160 B | **rope 慢 ~11×**（小文档树结构常量开销） |
| 1000 行 | 50,580 ns / 49 KB | 6,420 ns / 3.1 KB | **7.9×** |
| 5000 行 | 121,578 ns / 262 KB | 5,255 ns / 3.5 KB | **23×** |

5000 行文档的光标行列定位：

| 方案 | 耗时 | 分配 |
| --- | --- | --- |
| `splitLines` 全量 | 66,181 ns | 81,920 B |
| `rope.LineColToOffset` | 281 ns | 0 B |

**235×**，零分配。

复现：`go test ./widgets -run '^$' -bench 'BenchmarkEditMiddleLine|BenchmarkLineColLookup'`。

### 如实说明交叉点
小文档（约 ≤ 几十行）Rope 因 AVL 节点分配反而更慢——这正是此前"固定 24 行视图下接 rope 无收益"的原因。收益只在长文档显现，所以虚拟滚动（让长文档可见可编辑）是这次改造的必要前提，而非锦上添花。

---

## 4. 验证边界
- **已自动验证**：rope 与 string 逐字节等价（随机差分）、撤销/重做多行粘贴等 28 项编辑器测试、Vec 大后端迁移、惰性解析、虚拟滚动节点数、全仓 `go test ./...`。
- **需真机验证**：桌面滚轮手势（GLFW `SetScrollCallback` 已接、代码编译通过，但沙箱无 GUI 交互）、Web 滚轮在真实浏览器的 deltaMode 归一化与 preventDefault 行为。
- **既有问题（非本次引入）**：`gox test` 24 项自检中的"切片提升"项在干净 HEAD 上同样失败，与本次改动无关。

---

## 5. 文件清单
- `widgets/editor.gx` / `widgets/editor.go`（转译产物）：Rope 文本权威、惰性解析、虚拟滚动视图、scrollTop。
- `widgets/editor_rope_test.go` / `editor_infra_test.go` / `editor_perf_test.go`：新增差分/集成/性能测试。
- `runtime/signal/slice.go`：`Vec.IsBig()` 诊断方法。
- `examples/playground/ui/app.gx` / `app.go`：`EditorScrollLines` 导出命令。
- `examples/playground/desktop/glwindow.go`：GLFW 滚轮回调。
- `examples/playground/web/{main.go,src/gox.d.ts,src/main.ts,dist/main.js,playground.html}`：Web 滚轮事件与构建产物。
