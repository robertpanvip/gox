# GoX 架构

## 分层

```
cmd/                 入口与参数解析（不写业务逻辑）
  gox/               build / jsx / pub / enum / collections / sourcemap /
                     map-lookup / lsp-map / lsp / test
  gox-lsp/           独立 LSP server（stdio JSON-RPC）
pkg/                 对外稳定 API（可被 CLI / 插件 / 其它工具 import）
  compiler/          转译引擎
  sourcemap/         位置映射基础库（无业务依赖）
internal/            仅本仓库 cmd 使用
  lsp/               协议 / server / 诊断 / 符号映射 / 重命名
  buildcheck/        真实 go build 检查（可取消 + LRU）
bindings/cabi/       C-ABI 共享库（JNA）
vdom/ list/          运行时
```

外部前端（IDEA 插件）通过 `bindings/cabi` 或直接 import `pkg` 接入，不碰 `internal`。

## 转译管线

```
.gx 源码
  │ lex（token 流，保留原始位置与空白）
  ▼
transformEnum      enum → 具名类型 + const + String/Parse（注入 strconv import）
  ▼
transformPub       pub 声明处大写，收集「小写原名 → 导出名」到 names
  ▼
transformJSX       JSX → gxvdom.CreateElement 嵌套（注入 vdom import）
  ▼
transformCollections  []T → list.List[T]（全局提升）+ 方法名大写 + 注入 list import
  ▼
collectScope       收集本文件小写声明符号（私有字段/变量/方法/参数）与 import 包名
  ▼
restoreRefs        自身 pub 符号的引用还原为导出名（作用域感知）
  ▼
transformExternal  selector 右侧外部符号机械 upperFirst（私有符号用 scope 排除）
  ▼
emit               拼装 Go 源码，同时记录输出 token → 输入位置的 sourcemap 原始映射
  ▼
.go 源码 + sourcemap
```

**顺序约束**：
- `collectScope` 必须在 `transformPub` 之后（pub 名已大写，不会被误收为私有）；
- 必须在 JSX / collections 之后（它们注入的 import 也要被识别为包名）；
- `restoreRefs` 与 `transformExternal` 都在 scope 之后，共享同一份符号信息。

## 外部符号还原：为何不需要 importer

旧方案"一律写小写"把 `NewRequest` 压成 `newrequest`，丢失了驼峰信息，只能靠 `go/importer` 读导出符号表反查——依赖 export data、要处理歧义与本地 module 兜底。

新约定"只首字母小写、其余驼峰照抄"`newRequest` **保留了完整驼峰**，还原退化为 `upperFirst` 纯字符串操作：零依赖、零歧义、O(1)。唯一要排除的是"自己声明的私有符号"——由 `scope` 在 pub 阶段后收集解决。`Scope.private` 只收录会出现在 selector 右侧的声明（struct 字段、方法名、参数、局部变量），不收类型名/包名，避免误保护同名的外部字段。

## sourcemap

- `emit` 为每个非空白输出 token 记录 `OutLine/OutCol → InLine/InCol` 原始映射。
- `sourcemap.Build` 归并为可查询结构，提供 `GoToGx` / `GxToGo`（最近邻）与 JSON 序列化。
- 生成代码（枚举的 String/Parse、JSX 的 CreateElement、注入的 import）的位置重定位到触发生成的源 token，故编译错误能准确回投到 `.gx` 对应行。

## 编译检查与诊断回投

`buildcheck.Checker`：
- 把 Go 源码写入临时 module（`go.mod` 声明 `go 1.27`），执行 `go build`；
- 解析 `file:line:col: msg` 为结构化诊断；
- `context` 可取消；结果按源码 hash 做 LRU 缓存；
- `AddReplace` 注入 `go.mod replace`，使生成代码能解析到本地运行时（vdom/list）。

诊断的 Go 坐标经 `sourcemap.GoToGx` 转成 `.gx` 坐标后输出（CLI `-check` 与 LSP 诊断共用此路径）。

## LSP

- JSON-RPC over stdio，按 `Content-Length` 帧读写。
- `readMessage` 返回 `RequestMessage.Params`（params 对象本身），各 handler 直接反序列化到具体参数类型。
- 文档状态保存在 `Server.docs[uri]`，符号表懒构建并可标脏重建。
- 符号表收录 enum 成员的三种拼写别名（简名 / 全名 / 跨包小写名）指向同一 `Symbol`；`Symbol.EnumType` 记录常量前缀供重命名派生。
- 诊断在 didOpen/didChange 时触发，经 sourcemap 回投。

## C-ABI

`bindings/cabi` 以 `go build -buildmode=c-shared` 产出共享库，导出 `gx_translate` / `gx_build` / `gx_map_lookup` / `gx_version` / `gx_free`。返回 JSON 字符串，由调用方 `gx_free` 释放。供 IDEA 插件经 JNA 调用，详见 [CABI.md](CABI.md)。
