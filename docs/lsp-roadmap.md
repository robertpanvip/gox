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
| semanticTokens/full | ✅ 本轮落地：语法级分类（keyword/string/comment/number/namespace/type/function/variable/constant/operator…），双端同源 typecheck.Tokens+EncodeTokens；initialize 已声明 legend（12 类，Full=true）——原悬空字段就此转正 |
| completionItem/resolve | ✅ 本轮落地：候选携带 Data=pkg.Name；桌面 completionItem/resolve 从 GOROOT/embed 源取 doc 首段；浏览器等价面 Gox.lspResolveDoc(pkg,name) |
| typeDefinition | ✅ 本轮落地：光标符号类型→声明（指针解引用、别名展开；universe/匿名类型如实缺席）；桌面 `textDocument/typeDefinition` + 浏览器 `Gox.lspTypeDefinition` |
| implementation | ✅ 本轮落地：接口类型名/接口方法→本源内满足实现的命名类型（对象同一性判定）；桌面 `textDocument/implementation` + 浏览器 `Gox.lspImplementation`。跨包全闭包反查在 roadmap |
| implementation（跨包·窄口径） | ✅ 本轮落地：接口反查除用户文件外，同时扫描本文件 import 声明直接引用的包（`file.Imports` + `info.PkgNameOf`，不依赖高版本 Info 字段）——导出、非接口、非泛型的命名类型，值或指针满足都计入，对象同一性去重；全闭包扫描仍留在 roadmap |
| organizeImports | ✅ 已具备：`typecheck.OrganizeImports` 纯词法——连续单行 import（≥2）合并为括号块；块内按空行分子组，std 段在前、非 std 在后、段间空行、路径升序；别名/点导入与 `import "C"` 保留；含注释的块保守跳过；幂等（有序即不动）。桌面 `codeAction` 常驻 `source.organizeImports`（`CodeActionProvider` 升级为 kinds 列表声明），浏览器 `lspCodeActions` 同步带区域替换编辑（TextEdit `endLine`，0 基 end-exclusive） |
| pull diagnostics | ✅ 已具备：3.17 `textDocument/diagnostic`，复用 publishDiagnostics 同一 compileAndCheck 管线，返回 `DiagnosticReport{kind:"full",items}`；`diagnosticProvider` 已声明，interFileDependencies/workspaceDiagnostics 如实置 false——能力声明与实现严格对齐 |
| rename（跨文件） | ✅ 本轮落地：同包多文件一次 types 检查 + Uses/Defs 对象同一性定位，WorkspaceEdit 按文档分发（局部遮蔽免疫，red 有专项用例）；任一文档编译不过整体回退旧单文件符号表路径，零回归。`workspace/willRenameFiles` 仍在 roadmap |
| workspace 文件重命名 | ✅ 本轮落地：`workspace/willRenameFiles`（目标 URI 被占用时报错，不静默覆盖）+ `workspace/didRenameFiles`（文档表迁移、符号缓存标脏、旧 URI 清空诊断、新 URI 换发）；gox 文件包语义独立故无跨文件编辑，能力声明 filters=`**/*.gx` 与实际一致 |
| inlayHints | ✅ 本轮落地：具名形参 + 字符串字面量实参提示（可变参数不提示）；坐标经 SourceMap 回投 .gx；桌面 textDocument/inlayHints + 浏览器 Gox.lspInlayHints |
| rename / formatting | 单文档 rename；compiler.Format |

## 缺口（建议顺序）

### P1 —— 剩余
（本轮清零。下一优先级直接进入 P2。）

### P2 —— 锦上添花
5. **foldingRange / selectionRange**：折叠与智能扩选；selectionRange 和我们的选区引擎天然契合。
6. **callHierarchy / typeHierarchy**：调用链分析。
7. **增量同步（textDocumentSync=2）**：playground 有 Rope 提速不敏感，但 gox-tui/外部编辑器大文档收益明显。
8. **window/workDoneProgress**：首次类型图构建较长，可上报进度。
9. **gox 组件包桌面跳转**：桌面磁盘上 github.com/robertpanvip/gox/… 无路径可解（当前诚实跳过）；需要 rootUri→module 映射或直接把 embedstd 清单复用到桌面。
10. **协议一致性测试**：用真实客户端（VS Code/gopls 行为规格）跑 golden 场景，替代目前"单测 + CDP 探针"的组合。
11. **semanticTokens 升格语义级**：当前实现是语法级分类（不依赖类型图，快且稳）；类型感知类别（区分 method/parameter、常量折叠等）可基于既有 Importer 增量升级。

## 已知边界（当前版本如实降级，不算 bug）
- 内建函数（append/len 等）：go/types 无公开签名/源码面 → 无 signatureHelp/跳转，静默缺席。
- 源码半截且右括号未闭合的极端语法态：导航链以"编译可过 + 映射可查"为前提，失败一律回退旧行为。
- workspaceSymbol 仅覆盖已打开文档；未建项目索引。
- organizeImports 对含注释的 import 块整体跳过（不做行内重排），避免注释错位；pull diagnostics 为文档级，工作区级未启用（能力如实声明）。
- semanticTokens 为语法级（词法 + 关键字语境），类型类别不做推断——保证任意编辑态都有着色且零依赖类型图就绪；resolve 与 inlayHints 则依赖类型图/embed 源，缺席时静默降级。
