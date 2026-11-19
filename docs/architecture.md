# gox 架构

## 目录结构

```
gox/
├── cmd/
│   ├── gox/           CLI：build / jsx / pub / enum / collections / external / all
│   │                      sourcemap / map-lookup / lsp-map / lsp / test
│   └── gox-lsp/       独立 LSP server 入口（stdio）
├── pkg/               ← 对外稳定 API
│   ├── compiler/      转译引擎（token / lexer / enum / pub / extsym / external /
│   │                      collections / jsx / pipeline）
│   └── sourcemap/     双向位置映射（无业务依赖）
├── internal/          ← 工具内部实现
│   ├── lsp/           LSP 协议 / server / 诊断 / 符号映射
│   └── buildcheck/    真实 go build 检查（可取消 + LRU）
├── bindings/cabi/     C-ABI 共享库（go build -buildmode=c-shared，供 IDEA JNA）
├── vdom/              createElement 运行时
├── gxcollections/     List[T] + Map/Filter/Reduce 运行时
├── examples/ testdata/ docs/
```

## 转译管线

```
.gx 源码
  │
  ├─ lex         词法分析：token 流（空白/换行/注释保留，JSX 作为整体 token）
  │
  ├─ enum        pub enum / enum → type + const 块；收集 pub 名字到 names
  │
  ├─ jsx         <tag> → gxvdom.CreateElement 嵌套；注入 vdom import
  │
  ├─ pub         删除 pub、大写声明名；收集「小写→大写」到 names
  │
  ├─ collections 检测 X.map/filter/reduce → 方法名大写；按需提升接收者 []T→List[T]；
  │              注入 gxcollections import
  │
  ├─ restoreRefs 用 names 把引用处的自身 pub 符号还原为大写
  │
  ├─ external    go/importer 读导出符号表，还原外部符号（包级精确 + 全局唯一）
  │
  └─ emit        拼装 token 流 → Go 源码 + sourcemap 原始数据
```

### 设计要点

- **token 流上分层**：每层输出仍是 token 流（空白 token 保留），未改写部分原样输出，可读性好、sourcemap 精确。
- **两步还原**：自身 pub 符号（restoreRefs）与外部符号（external）分开。前者靠 pub/enum 层收集的 `names`，后者靠 importer 符号表；两者不冲突（已大写的不会被再改）。
- **sourcemap**：emit 时每个输出 token 起点记录「Go 行:列 → GX 行:列」；Generator 位置排序后支持双向最近邻查询。生成代码被改写处映射到原构造起点。

## sourcemap

`pkg/sourcemap` 无业务依赖，只处理行列对：

- `Build(file, raw)` 构造，内部按 Generated / Original 各排一份。
- `GoToGx(line, col)` / `GxToGo(line, col)`：最近的前一个映射，同行时按列偏移线性修正。
- `MarshalJSON` / `UnmarshalJSON`：可读 JSON（`{version, file, sources, mappings:[{go,gx}]}`）。

CLI：`gox sourcemap <file>` 导出，`gox map-lookup <map.json> <line> <col>` 反查。

## LSP

`internal/lsp` 实现最小 JSON-RPC over stdio：

| 能力 | 实现 |
|---|---|
| 实时诊断 | didOpen/didChange → compile + buildcheck → 经 sourcemap 把 Go 诊断映射回 GX 位置 → publishDiagnostics |
| 定义跳转 / hover | `BuildSymbolMap` 从 GX 源码轻量抽取 func/type/enum/var 符号 |
| 补全 | 符号表 + 关键字 + map/filter/reduce |
| documentSymbol | 符号表 |

诊断落点用 sourcemap 的 `GoToGx`，因此错误位置指回 GX 源码而非生成代码。

## buildcheck

`internal/buildcheck.Checker`：

- 把 Go 源码写入临时 module，`exec.CommandContext` 跑 `go build ./...`。
- 解析 `file:line:col: msg` → `Diagnostic`。
- context 取消立即返回 `context.Canceled`。
- LRU 缓存（key = 源码 SHA-256），默认容量 64/128，避免 LSP 反复构建。

> 注：注入了本地 module 依赖（如 gxcollections）的生成代码，LSP 场景需在临时 module 里加 `replace` 指向工程目录；CLI 的 `gox build` 只做语法校验（`go/parser`），不解析依赖，因此不受影响。

## C-ABI（JNA）

`bindings/cabi` 用 cgo `//export` + `-buildmode=c-shared` 生成 `libgox.so` + `libgox.h`：

| 函数 | 作用 |
|---|---|
| `gx_translate(const char* source) -> char*` | 转译，返回 `{goSource, sourceMap, error}` JSON |
| `gx_validate(const char* goSource) -> char*` | Go 语法校验，返回 `{valid, error}` |
| `gx_map_lookup(const char* mapJson, int line, int col) -> char*` | 位置反查，返回 `{line, col, ok}` |
| `gx_version() -> char*` | 版本字符串 |
| `gx_free(void*)` | 释放上述返回的字符串 |

约定：返回字符串由 Go `C.CString` 分配，调用方必须用 `gx_free` 释放。

```bash
go build -buildmode=c-shared -o libgox.so ./bindings/cabi
```
