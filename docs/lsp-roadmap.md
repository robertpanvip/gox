# GX LSP 能力矩阵与路线图

以 gopls/vscode-go 常用面为参照系。状态：✅ 已实现（浏览器 goxlsp / 桌面 gox-lsp 双端同源）；
🟡 部分实现；⬜ 未做。日期：2026-11-26。

## 已具备 ✅

| 能力 | 说明 |
| --- | --- |
| initialize / shutdown / 生命周期通知 | didOpen / didChange / didClose；全量同步（textDocumentSync=1） |
| completion | 符号表 + 关键字 + JSX 组件 + **类型感知成员**（点号上下文）+ **import 路径** |
| hover | 本地符号（文档注释）+ **std 真实签名兜底** |
| definition | 本地= .gx 坐标回跳；**std=浏览器内嵌源速览+只读全文 / 桌面 file:// GOROOT 真实文件** |
| signatureHelp | 触发 `(` `,`；LSP label 字节区间；可变参数钳位 |
| references | 声明点 + 全部使用点（跨外部/本地，sourcemap 回投） |
| documentHighlight | 同文件高亮，Kind 区分声明(3)/使用(2) |
| codeAction(quickfix) | **undefined 包名 → 插入 import（单行式/括号块）**；**未使用 import → 删行**；浏览器端已验证"应用后复诊清零"闭环 |
| documentSymbol | 全符号大纲 |
| workspaceSymbol | 已打开文档聚合（v1） |
| rename / formatting | 单文档 rename；compiler.Format |

## 缺口（建议顺序）

### P1 —— 编辑器体感与协议正确性
1. **semanticTokens/full**：ServerCapabilities 里有字段但 initialize 从未声明、无实现——悬空字段，要么接编译器 token 流实现（三端高亮同源），要么删字段，保持能力声明诚实。
2. **completionItem/resolve**：补全候选惰性带出 doc/签名（embedstd 里就有源码，成本低）。
3. **typeDefinition / declaration / implementation**：接口→实现需要类型图反查（go/types 全程序扫描），中等成本。
4. **跨文件 rename + workspace/willRenameFiles**：当前 rename 单文档。
5. **organizeImports**（codeAction source 组）：需要先建 import 全量解析。
6. **diagnostics 主动刷新（pull/refresh，LSP 3.17）**。

### P2 —— 锦上添花
7. **inlayHints**：参数名提示——TypeInfo 已能给出完整签名，成本低收益高。
8. **foldingRange / selectionRange**：折叠与智能扩选；selectionRange 和我们的选区引擎天然契合。
9. **callHierarchy / typeHierarchy**：调用链分析。
10. **增量同步（textDocumentSync=2）**：playground 有 Rope 提速不敏感，但 gox-ide/外部编辑器大文档收益明显。
11. **window/workDoneProgress**：首次类型图构建较长，可上报进度。
12. **gox 组件包桌面跳转**：桌面磁盘上 github.com/robertpanvip/gox/… 无路径可解（当前诚实跳过）；需要 rootUri→module 映射或直接把 embedstd 清单复用到桌面。
13. **协议一致性测试**：用真实客户端（VS Code/gopls 行为规格）跑 golden 场景，替代目前"单测 + CDP 探针"的组合。

## 已知边界（当前版本如实降级，不算 bug）
- 内建函数（append/len 等）：go/types 无公开签名/源码面 → 无 signatureHelp/跳转，静默缺席。
- 源码半截且右括号未闭合的极端语法态：导航链以"编译可过 + 映射可查"为前提，失败一律回退旧行为。
- workspaceSymbol 仅覆盖已打开文档；未建项目索引。
