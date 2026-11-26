# Playground 浏览器内类型检查层（embedstd）实现记录

## 机制
- `internal/typecheck`：`Lookup` 虚拟文件系统接口 + `FSRoot(fs.FS)` 通用实现；
  `Importer` 持久会话（std/gox 类型图一次构建跨编辑复用）；诊断只经 Error 回调
  收集（返回首错与回调同源，重复收集会翻倍——`TestCheckNoDuplicateError` 锁定）。
- `cmd/gox-stdlib-gen`：构建期生成器。闭包 = `go list -deps ./runtime/... ./widgets/... ./pkg/sourcemap`，
  过滤复用 `DiskLookup` 同一套 GOOS/GOARCH/build-tag/cgo 规则（无第二份名单），
  输出 `_srcs/{std,gox}` + `VERSION`（锁 go 版本，生成与编译必须同工具链）。
- `internal/typecheck/embedstd`：`//go:embed all:_srcs`；未生成数据时 `Lookup()=nil`
  → goxlsp 类型层整体缺席（零行为变化安全阀）；数据目录带下划线前缀，go 工具链
  与 .gitignore 双重隔离，仓库零增长。
- `pkg/goxlsp/session`：GoSource 非空且 embedstd 就绪时，诊断经
  `SourceMap.GoToGx` 回投 .gx 坐标，Code="type"，持久 Importer 挂会话。

## 实测数字（2026-11-25，go1.27.4，Chrome 158 headless）
| 指标 | 数值 |
|---|---|
| 生成源码量 | 197 包 / 10.4MB（js/wasm 视图；linux-only/cgo 文件天然排除） |
| app.wasm | ≈29MB（raw，嵌入源码为主要增量） |
| playground.html 单文件 | 6.26MB → **10.28MB**（+4.0MB，优于 4.6MB 预估） |
| 浏览器内首次类型检查就绪 | **9ms**（setSource→type 诊断可读，含 std 图首建） |
| 诊断质量 | `4:16 type: cannot use n (variable of type int) as string value...`，坐标回投 .gx，零 `check.go` 泄露 |

## 使用
```
go run ./cmd/gox-stdlib-gen                     # 构建 wasm 前生成（playground build.sh 应接入）
GOOS=js GOARCH=wasm go build ... examples/playground/web
node scripts/selfcheck.js gsyntax <playground.html 绝对路径>   # 类型层端到端探针
```
类型层覆盖白名单闭包（gox runtime/widgets/pkg + std 197 包）；用户 import 白名单外
包时产生一条 could-not-import 提示后继续（优雅降级）。完整 go build 级兜底仍在桌面
gox-lsp / CLI `gox check -check`。

## 类型感知成员补全（P1+）

点号上下文（`b.`、`strings.`）的补全从正则符号表升级为 go/types 真实类型查询，
浏览器与桌面同源：

- 核心：`internal/typecheck.Complete(src, byteOffset, lookup)` —— 光标前插
  占位标识符补全语法 → 定位点号左侧表达式 → 持久 Importer 求类型 →
  枚举包级导出成员 + 方法集；跨包未导出成员过滤；无新依赖（未引入 x/tools）。
- 浏览器（pkg/goxlsp → Gox.lspComplete）：数据源 `embedstd.Lookup()`。
- 桌面（internal/lsp）：数据源 `typecheck.DiskLookup(GOROOT/src, GOOS, GOARCH)`，
  惰性建会话；诊断仍走原有 go build 管线，类型层零干扰。
- 零回归契约：非点号上下文原样返回旧符号表；无模块根/数据未生成时整体回退。
- 坐标链：`.gx (line,col)` → GxToGo → Go 字节偏移（1 基）→ 候选按 Go 字节
  偏移回投 `.gx`（UTF-16 安全）；构建脚本先跑 `cmd/gox-stdlib-gen` 保证
  embed 数据与 wasm 同工具链。

实测（headless Chrome 158，`node scripts/selfcheck.js comp playground.html`）：
`{"typeHit":true,"kind":2,"detail":"func(s string) string","fallbackItems":17}`
——*strings.Replacer 上补出 Replace 方法带真实签名，非点号路径 17 项候选不丢。

## 参数签名提示（signature help）

光标位于调用实参区（触发字符 `(` / `,`）时，浏览器与桌面 LSP 均给出真实签名：

- 核心：`internal/typecheck/signature.go`——对未写完的调用，截断光标后缀、注入占位
  实参 `GxSigX_`，并按未闭合括号栈（类型感知配对，跳过字符串/注释/字面量）倒序补齐
  闭合符，还原成可解析 probe；再从持久 Importer 的类型图读取被调 `*types.Signature`。
  Label 与各参数字节区间遵循 LSP `ParameterInformation.label` 约定；active 为顶层
  逗号数并随可变参数钳位。内建函数（append/len 等）在 go/types 无公开签名访问器，
  按"无提示"降级，不猜测。
- 浏览器：`Gox.lspSignature(line, col)`（`pkg/goxlsp`，数据源 embedstd）。
- 桌面：`textDocument/signatureHelp`，触发字符已在 initialize 注册（数据源本机 GOROOT）。
- 零回归契约：非调用上下文 / 类型层缺席 / 坐标映射失败 → null 或空 help，
  编辑器隐藏提示即可。

实测（headless Chrome 158，`node scripts/selfcheck.js sig playground.html`）：
`{"ok":true,"label":"Sprintf(format string, a ...any) string"}`——括号后与逗号后的
active 分别为 0/1，非调用位置返回 null；单测红→绿对照见
`internal/typecheck/signature_test.go`。

## 类型化导航（definition / import 路径补全 / 类型化 hover）

三类能力共用 internal/typecheck 的同一持久类型图（与签名提示同址），零新依赖：

- **声明跳转**：`Importer.Definition`（navigation.go）定位光标标识符的
  `types.Object` → 声明位置。用户文件内符号 → Kind=local，宿主经 SourceMap
  GoToGx 回投 .gx 光标；std 符号 → Kind=std，带 Pkg/File/Line/Snippet/Doc
  （声明片段与上方紧邻注释直接从虚拟 FS 取回）。内建函数（append/len 等）
  在 go/types 没有源码面，按无提示降级——不猜。
- **浏览器 UX（两段式）**：`Gox.lspDefinition(line,col)` 与 `Gox.stdSource(pkg,file)`；
  playground 前端 `Ctrl/Cmd + /` 触发——本地符号直接落光标，std 符号弹右侧
  速览浮卡（位置+doc+签名），卡内"打开完整文件"渲染内嵌源只读浮层。
- **桌面 UX（原生）**：`textDocument/definition` 对 std 符号返回
  `file://$GOROOT/src/<pkg>/<file>` 真实 URI，任何 LSP 编辑器天然就是
  "开新标签页跳到声明"；本地符号走原符号表路径，零回归。
- **import 路径补全**：importctx.go `ImportContext` 纯语法判定单行式与括号块
  （源码半截、编译不过也能用）；浏览器清单来自 `embedstd.Packages()`（内嵌
  闭包全目录，internal/testdata 整支剔除），桌面清单来自本机 GOROOT/src 顶层。
- **类型化 hover**：符号表缺席时用 `Importer.TypeInfo` 给真实签名+声明位置提示；
  本地命中符号表时行为不变。

实测（headless Chrome 158，`node scripts/selfcheck.js nav playground.html`）：
八项检查全 true——`{"okDef":true,"okHov":true,"okLocal":true,"okFull":true,
"okImp":true,"okPop":true,"okView":true}`（后两项为真实按键派发驱动的前端浮层与
只读全文视图）。回归：sig/comp/gsyntax/stacking 四探针全绿，全仓 25 包 ok、
race 通过。已知边界：gox 组件包（widgets 等）在桌面端没有磁盘路径可跳，
统一降级；浏览器端可跳（内嵌源含 gox 闭包）。

## 引用与 QuickFix（P2）

`Importer.References` 复用同一持久类型图：声明点（外部符号带 Pkg/File 内嵌源坐标）+
用户文件全部使用点（SourceMap 回投 .gx）。`QuickFixes(src, diags, pkgNames)` 纯文本规则：
`undefined: pkg` → 在 package 后/最后一个单行 import 后/import 块首插入 import；
`"pkg" imported and not used` → 删该行。候选包名：浏览器 embedstd.Packages()，
桌面 GOROOT/src 顶层清单。双端入口：`Gox.lspReferences/lspCodeActions`；
`textDocument/references`、`textDocument/documentHighlight`、`textDocument/codeAction`、
`workspace/symbol`（已打开文档）。

实测（headless Chrome，`node scripts/selfcheck.js nav2 playground.html`）：
`{"ok":true,"okRefs":true,"okFix":true,"okFixClean":true,"okUnused":true,"okUnusedClean":true}`
——浏览器里真的执行"接受修复→重设源码→复诊"闭环，undefined 与未使用 import 均清零。
完整能力矩阵与未做清单见 docs/lsp-roadmap.md。

## 语义 token / 参数提示 / 惰性文档（P3）

同一类型图的第三波增量，全部双端同源：

- **semanticTokens**：`typecheck.Tokens(src)` 用 go/scanner 对 .gx 文本做语法级分类
  （package/import/type/var/const/func 语境 + 后随左括号判定函数），`EncodeTokens` 输出
  LSP 五元组编码；initialize 正式声明 legend（12 类，Full），协议侧悬空字段转正。
  不依赖类型图——任何编辑态都有着色。
- **completionItem/resolve**：成员候选 Data 携带 `pkg.Name`；桌面 `completionItem/resolve`
  经 `typecheck.DocOf` 从 GOROOT/embed 源解析声明 doc 首段（ParseComments，轻量独立解析）；
  浏览器等价面 `Gox.lspResolveDoc(pkg,name)`。
- **inlayHints**：`Importer.ParamHints` 扫调用实参——具名形参 + 字符串字面量实参给
  `name:` 提示，可变参数不提示；坐标经 SourceMap 回投 .gx 0 基。桌面
  `textDocument/inlayHints`，浏览器 `Gox.lspInlayHints`。

实测（headless Chrome，`node scripts/selfcheck.js p1 playground.html`）：
`{"ok":true,"okTok":true,"okHint":true,"okDoc":true}`；基线（无实现）如实报
`no-p1-exports`。nav2 浏览器闭环里 import 补全候选已达 60 项上限（前缀过滤后），
证明 import 语境判定在真实键入流生效。

## typeDefinition / implementation / 跨文件 rename（导航二期）

同一持久类型图上的第二波增量（internal/typecheck/p2.go）：

- **TypeDecl**：光标符号 → obj.Type() → 解指针/展开别名 → 命名类型声明。universe
  类型（string/error）与匿名结构没有源码可跳，如实返回 false；宿主隐藏提示即可。
- **Implementations**：接口类型名或接口方法 → 本源内满足该接口的命名类型列表。
  判定走 types.Implements + 对象同一性，不是文本匹配；跨包全闭包反查在 roadmap。
- **MultiRename**：把同包多文件一次性交给 types 检查（files 自包含，无需 lookup），
  按 Uses/Defs 对象同一性收集每个文件的全部出现——局部变量遮蔽天然免疫
  （红测试 TestMultiRenameNoCrossTalk 专门钉这条）。桌面 rename 采取保守接入：
  所有打开文档都能编译且确凿命中才走强类型 WorkspaceEdit，否则整体回退旧
  符号表单文件路径（含枚举成员特殊形态），零行为回归。
- 桌面 `textDocument/typeDefinition`、`textDocument/implementation`；浏览器
  `Gox.lspTypeDefinition`、`Gox.lspImplementation`；能力位已在 initialize 声明。

实测（headless Chrome 158，`node scripts/selfcheck.js nav3 playground.html`）：
`{"ok":true,"okTD":true,"okImpl":true}`；基线（无实现）如实报 `no-nav3-exports`。
全探针回归 nav2/nav/sig/comp/gsyntax/stacking/p1 全绿，race 四包通过。
