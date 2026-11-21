# GoX 架构

## 分层

```
cmd/                 入口与参数解析（不写业务逻辑）
  gox/               build / jsx / pub / enum / collections / sourcemap /
                     map-lookup / lsp-map / lsp / test
  gox-lsp/           独立 LSP server（stdio JSON-RPC）
pkg/                 对外稳定 API（可被 CLI / 插件 / 其它工具 import）
  compiler/          转译引擎（纯转译：输入 .gx 源码 → 输出 .go + sourcemap）
  module/            跨文件 GX 模块解析（项目视图 / import 来源 / 依赖图 / 转译调度）
  sourcemap/         位置映射基础库（无业务依赖）
internal/            仅本仓库 cmd 使用
  lsp/               协议 / server / 诊断 / 符号映射 / 重命名
  buildcheck/        真实 go build 检查（可取消 + LRU）
bindings/cabi/       C-ABI 共享库（JNA）
runtime/             运行时组件（统一收纳）
  vdom/ list/ signal/  具体运行时
    signal/           响应式信号：函数式 Signal[T]、依赖图、惰性 computed、串行 effect
                      读路径用 activeObservers 原子计数 + atomic value 走无锁快路径，
                      仅重算期抓 goroutine id 登记依赖（详见 language-spec §11）
```

外部前端（IDEA 插件）通过 `bindings/cabi` 或直接 import `pkg` 接入，不碰 `internal`。

## 转译管线

```
.gx 源码
  │ lex（token 流，保留原始位置与空白）
  ▼
downgradeSignalFields  struct 内 `signal name T` → `name Signal.Signal[T]`（在 Bind 之前！）
  │                     建 signalFieldTable 供后续读写解包；避免 signal 关键字被局部逻辑误判
  ▼
Bind / transformSignals / Analyze / ...对象字面量、模板等 desugar
  ▼
wrapSignalFieldLiterals  构造位置 signal 字段值包 Signal.New（已是 signal 则直通 getter）
rewriteSignalFieldRefs   selector 读写解包：p.name → p.name() / p.name = v → p.name(v)
  ▼
transformEnum      enum → 具名类型 + const + String/Parse（注入 strconv import）
  ▼
transformPub       pub 声明处大写，收集「小写原名 → 导出名」到 names
  ▼
transformJSX       JSX → gxvdom.CreateElement 嵌套（注入 vdom import）
  ▼
transformCollections  []T → list.List[T]（全局提升）+ 方法名大写 + 注入 list import
  ▼
collectScope       收集本文件小写声明符号（私有字段/变量/方法/参数）与 import 包名（含来源）
  ▼
restoreRefs        自身 pub 符号的引用还原为导出名（作用域感知）
  ▼
transformExternal  selector 右侧外部符号还原：Go 来源 upperFirst；GX 来源严格查导出表，
                   查表失败做大小写不敏感唯一匹配 → 追加 import-case 诊断到 Analysis
transformCompositeFields  复合字面量字段名还原：同上按来源分支，GX 查失败报 import-field-case 诊断
  ▼
emit               拼装 Go 源码，同时记录输出 token → 输入位置的 sourcemap 原始映射
  ▼
.go 源码 + sourcemap
```

`CompileWithProject(file, src, module, imports)` 接收 import 来源表（`路径 → ImportInfo`）：
`imports=nil`（默认单文件）时所有 import 按 Go 来源处理，与改造前一致；`CompileWithModule` 委托于它。`collectScopeWithImports` 用来源表给每条 import 打标签（`ImportGo` / `ImportGX`）。

**顺序约束**：
- `downgradeSignalFields` 必须在 `Bind` 之前：降级后 struct 字段是标准 Go `Signal.Signal[T]`，绑定表/作用域/objectlit 才能正确识别它；否则 `signal name T` 会被局部 signal 逻辑误判为 `signal name = expr`（旧行为生成非法 `var name any T`）。
- `wrapSignalFieldLiterals` / `rewriteSignalFieldRefs` 在 signal 局部变量改写与模板展开之后、external 之前：此时右侧表达式已处理，字段名仍是小写、可用原始类型名查 `signalFieldTable`。
- `collectScope` 必须在 `transformPub` 之后（pub 名已大写，不会被误收为私有）；
- 必须在 JSX / collections 之后（它们注入的 import 也要被识别为包名）；
- `restoreRefs` 与 `transformExternal` / `transformCompositeFields` 都在 scope 之后，共享同一份符号信息。

## struct signal 字段（响应式 props）

`pkg/compiler/signalfields.go` 实现 struct 字段级的响应式语言特性。核心是一张 `signalFieldTable`（`type 名 → signal 字段名集合`），三段协作：

1. **降级（Bind 之前）**：`collectSignalFieldDecls` 扫 `type X struct{...}` 找 `signal name T`，`downgradeSignalFields` 改写为 `name Signal.Signal[T]`，Signal token 标记 `IsGen` 由 `ReconcileImports` 统一注入 import。放在 Bind 之前是关键——降级后所有下游都以标准 Go 处理该字段。
2. **构造包装**：`wrapSignalFieldLiterals` 对 `X{name: v}` 中命中的 signal 字段：普通值包 `Signal.New(v)`；已是 signal 的值（`src()`／`Signal.New(...)`）直通 getter 本身。前者实现"字段可独立初始化"，后者实现保鲜——传 getter 而非快照，源 signal 变化会反映到字段。
3. **读写解包**：`rewriteSignalFieldRefs` 用 receiver 的推断类型（`Bindings.ResolveAt(...).Type`，已增强为识别具名复合字面量 `X{...}`）查表，只对命中的 selector 补 `()` / 改写为调用式。普通字段、同名非 signal 字段、外部包符号都不动。

保鲜语义的关键在构造直通：`p := X{name: src}` 必须生成 `name: src`（getter 本身），不能是 `Signal.New(src())`（快照当前值、断开后续更新）。集成测试 `TestIntegrationSignalFieldLiveProps` 固化了这一点（源 signal 更新后持有者读到新值）。

## 外部符号还原：为何不需要 importer

旧方案"一律写小写"把 `NewRequest` 压成 `newrequest`，丢失了驼峰信息，只能靠 `go/importer` 读导出符号表反查——依赖 export data、要处理歧义与本地 module 兜底。

新约定按 Go 真实拼写书写：`newRequest` **保留了完整驼峰**，由 `upperFirst` 纯字符串还原；全大写缩写（`URL`、`StatusOK`）直接写大写、在整条管线里原样保留不改写（大小写转换只作用于小写开头的标识符）。两条路径都零依赖、零歧义、O(1)。唯一要排除的是"自己声明的私有符号"——由 `scope` 在 pub 阶段后收集解决。`Scope.private` 只收录会出现在 selector 右侧的声明（struct 字段、方法名、参数、局部变量），不收类型名/包名，避免误保护同名的外部字段。

**跨文件模块带来的分流**：上述规则针对的是外部 Go 包。本地 GX 模块（`pkg/module` 解析出的同项目 `.gx` 包）走第二条路径——`compiler.ScanExports` 提取被导入方的 `pub` 导出表（顶层符号 + struct pub 字段的 `Type.field` 限定名），`ImportInfo.Origin=ImportGX` 时 selector 与复合字面量字段严格查表、不猜测。

**大小写诊断**：GX 来源不受 `isLowerStart` 门槛限制（写错的 `ADD` 本就是大写开头，先过门槛会永远漏掉）。查表精确命中则还原为导出名；查表失败时对导出表做**大小写不敏感唯一匹配**（排除原名/导出名本身、多候选不猜测），命中即向 `Analysis.diags` 追加 `import-case`（符号）/ `import-field-case`（字段）诊断，位置取书写标识符的原始 GX 坐标。**产物始终原样保留**、严格不改赌——诊断只增强检查器，不改变转译行为。这些诊断进入现成的 GX 语义诊断通道，被 `check.CheckAll`、`gox check -root`、`gox build`（项目模式）与 LSP 消费，无需再靠 `go build` 的 `undefined`/`unexported`。这样既保持单文件转译的零依赖特性，又让 GX→GX 导入保真被导入方的大小写语义。详见 [cross-file-modules-design.md](cross-file-modules-design.md)。

## sourcemap

- `emit` 为每个非空白输出 token 记录 `OutLine/OutCol → InLine/InCol` 原始映射。
- `sourcemap.Build` 归并为可查询结构，提供 `GoToGx` / `GxToGo`（最近邻）与 JSON 序列化。
- 生成代码（枚举的 String/Parse、JSX 的 CreateElement、注入的 import）的位置重定位到触发生成的源 token，故编译错误能准确回投到 `.gx` 对应行。

## 编译检查与诊断回投

`buildcheck.Checker`：
- 把 Go 源码写入临时 module（`go.mod` 声明 `go 1.27`），执行 `go build`；
- 解析 `file:line:col: msg` 为结构化诊断；
- `context` 可取消；结果按源码 hash 做 LRU 缓存；
- `AddReplace` 注入 `go.mod replace`，使生成代码能解析到本地运行时（runtime/vdom、runtime/list）。

诊断的 Go 坐标经 `sourcemap.GoToGx` 转成 `.gx` 坐标后输出（CLI `-check` 与 LSP 诊断共用此路径）。多文件场景用 `CheckFiles([]SourceFile)`：每份产物以相对路径（如 `util/math.go`）写入临时 module，`SetModulePath` 设项目 module 使本地包 import 可解析，诊断的 `File` 字段保留相对路径，CLI 据此选择对应 `.gx` 的 sourcemap 回投到正确文件与行。

## 跨文件 GX 模块（`pkg/module`）

把一组 `.gx` 组织成可互相导入的模块，保持 `compiler` 的纯转译职责：

- `LoadProject(root, modulePath, prefer)`：遍历项目根下所有 `.gx`（跳过 `.git`/`vendor`/`node_modules`），按 `(目录, package)` 分组为 `GXModule`；包 import 路径 = module 路径 + 目录相对路径；收集每包的 `Exports`（`compiler.ScanExports` 的并集）与 import 列表（`compiler.ScanImports`）。
- `Resolve(path)`：命中本地 GX 包 → `ImportGX` + 导出表；否则 `ImportGo`。`prefer=go` 可关闭本地优先。
- `Order()`：按 import 建依赖图，DFS 三色标记给拓扑序（被依赖方先），遇环输出完整路径链 `a → b → a`。
- `Build(entry)`：按拓扑序逐文件调用 `compiler.CompileWithProject`，每一文件带上其所有 import 的来源表；返回 `CompiledFile`（源路径 / 落盘 `.go` 路径 / 产物 / sourcemap）。

依赖方向：`pkg/module` → `pkg/compiler`（单向），compiler 不 import module，避免环依赖。单文件模式即"module 层缺席"的特例（`imports=nil`），零回归。完整设计见 [cross-file-modules-design.md](cross-file-modules-design.md)。

## LSP

- JSON-RPC over stdio，按 `Content-Length` 帧读写。
- `readMessage` 返回 `RequestMessage.Params`（params 对象本身），各 handler 直接反序列化到具体参数类型。
- 文档状态保存在 `Server.docs[uri]`，符号表懒构建并可标脏重建。
- 符号表收录 enum 成员的三种拼写别名（简名 / 全名 / 跨包小写名）指向同一 `Symbol`；`Symbol.EnumType` 记录常量前缀供重命名派生。
- 诊断在 didOpen/didChange 时触发，经 sourcemap 回投。

## C-ABI

`bindings/cabi` 以 `go build -buildmode=c-shared` 产出共享库，导出 `gx_translate` / `gx_build` / `gx_map_lookup` / `gx_version` / `gx_free`。返回 JSON 字符串，由调用方 `gx_free` 释放。供 IDEA 插件经 JNA 调用，详见 [CABI.md](CABI.md)。
