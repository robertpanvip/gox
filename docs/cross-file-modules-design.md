# 跨文件 GX 模块解析与来源敏感大小写还原 —— 改造设计

> 状态：**P0–P4 已全部实现**（基于 `3156d32`）。实现对照见文末 §13（含 P4 补充）。
>
> 本文基于当前 `main`（`3156d32`）源码核对，逐条给出改造的切入点、数据结构、管线顺序与迁移路径。
>
> 要解决的问题：当前 GoX 只能单文件转译，所有 `import` 都当作 Go 包处理，`pkg.Name` 统一走 `external.go` 的 `upperFirst` 还原。因此**无法导入另一份 `.gx` 代码并保留它自己的大小写语义**。本文设计一套跨文件模块解析机制，并在符号还原处区分「GX 来源 / Go 来源」。

---

## 1. 现状核对（已逐条验证）

| 断言 | 是否属实 | 代码依据 |
|---|---|---|
| 无 `.gx` 导入 `.gx`，只支持单文件编译 | ✅ | `Compile(file,src)` / `CompileWithModule` 只收单一源码（`pipeline.go:50-55`）；`cmd/gox build <file.gx>` 单文件入参；`collectImportSpecs` 只解析 Go import 路径（`scope.go:41+`） |
| `pkg.Name` 统一走 `upperFirst`，无来源分支 | ✅ | `transformExternal`（`external.go:127-171`）对 import 包名左侧的 selector 一律 `upperFirst(sel)`；`Scope.imports` 是 `map[string]bool`，无来源信息 |
| 现有代码无切入点 | ✅ | 设计文档 `architecture.md`「为何不需要 importer」明确这是**刻意设计**；`language-spec.md` 写「大小写语义由 pub 与约定承担」。没有 module/包解析层 |

**唯一的多 `.gx` 接触点**是 `cmd/gox lsp-map <dir>`（`main.go:311+`）：它遍历目录、把每份 `.gx` 的符号合并给 LSP，但**不参与编译**，不解析 import，不做符号绑定。可作为模块解析器的符号来源复用，但需独立重构成真正的解析层。

### 1.1 现有「大小写约定」的精确定义

这是改造必须保留或替代的语义基础：

- **书写约定**：标识符按 Go 真实拼写写；全大写缩写（`URL`、`StatusOK`）原样保留；声明用 `pub` 显式导出。
- **还原规则**：对小写开头的标识符做机械 `upperFirst`（`URL` 这类全大写不动）。
- **防误伤**：`transformPub` 收集本文件 `pubNames`（小写→导出名）；`collectScope`（pub 之后）收集私有名与 import 包名；`restoreRefs` 用绑定表排除被遮蔽的局部引用；`transformExternal` 对 selector 右侧：自己的 pub 名用记录名、自己的私有名保持、其余（外部符号）`upperFirst`。
- **设计收益**：零依赖、零类型推导、O(1)、行为完全可预测（`architecture.md`）。

> 关键洞察：现有规则把「所有非本文件的 selector 右侧」当成**同一种东西**（外部 Go 符号）。要支持 GX 跨文件导入，本质是把这个隐含的「单一来源假设」拆成两条显式路径。

---

## 2. 目标与非目标

### 2.1 目标

1. **GX 模块可互相导入**：`import "example.com/proj/util"` 如果该路径解析到一组 `.gx` 源文件，则按 GX 模块处理。
2. **来源敏感的大小写**：
   - Go 包来源：维持现状（`upperFirst` 兼容小写写法 + 真实拼写原样）。
   - GX 模块来源：严格保留被导入方声明的大小写（导出名由被导入方的 `pub` 决定），**不再**对 GX 符号做 `upperFirst` 猜测。
3. **复用 Go 工具链**：产物仍是标准 `.go`，GX 模块转译后就是一个普通 Go 包，Go 负责跨包类型检查与链接。
4. **诊断可回投**：跨文件的编译/类型错误能映射回对应 `.gx` 文件与行。

### 2.2 非目标（明确不做）

- 不做 Go 那样的 GOPATH/module 版本解析（`go.mod` 语义仍交给 Go）。
- 不做跨模块类型推导/IDE 级语义服务（本方案只解决「符号来源与大小写」）。
- 不改变单文件转译的行为（零回归是硬约束）。
- 不在首期支持循环依赖的复杂场景（见 §9 风险）。

---

## 3. 架构分层

在现有 `pkg/compiler` 之外新增一个模块解析层，保持 `compiler` 的「纯转译」职责不变：

```
cmd/gox build <entry.gx> [-module path] [-root dir]
        │
        ▼
┌─────────────────────────────────────────────┐
│ pkg/module   （新增）                        │
│  - Project：以入口文件 + 根目录建立项目视图   │
│  - Resolver：import 路径 → 来源(Go/GX) + 路径 │
│  - GXModule：解析一组 .gx，提取导出符号表      │
│  - LoadGraph：依赖图 + 拓扑序 + 循环检测       │
└─────────────────────────────────────────────┘
        │  提供：每个 import 的 {path, pkg, origin, exports}
        ▼
┌─────────────────────────────────────────────┐
│ pkg/compiler （改造）                        │
│  - Scope.imports: map[string]ImportOrigin    │  ← 带来源
│  - transformExternal / restoreRefs：按来源分支 │
│  - 既有单文件管线保持不变                      │
└─────────────────────────────────────────────┘
        │
        ▼
  逐文件 .go 产物 + 各自 sourcemap → go build
```

**为什么独立成 `pkg/module` 而不是塞进 `compiler`**：
- `compiler` 当前是「输入字符串 → 输出字符串」的纯函数式接口（`Compile`），引入文件系统/依赖图会污染它；
- 模块解析需要有目录 IO、路径解析、缓存，职责与 token 转换完全不同；
- 分层后单文件模式是「module 层缺席」的特例，`compiler` 对来源信息做 nil 兜底即可零回归。

---

## 4. 核心数据结构

### 4.1 import 来源标记（改造 `Scope`）

当前：

```go
type Scope struct {
    imports       map[string]bool // import 包名 / 别名
    private       map[string]bool
    privateFields map[string]bool
}
```

改为：

```go
type ImportOrigin uint8

const (
    ImportGo ImportOrigin = iota // 外部 Go 包：维持 upperFirst 兼容
    ImportGX                     // GX 模块：符号大小写由被导入方 pub 决定
)

// ImportInfo 单个 import 的解析结果。
// - Pkg: 源码中使用的包名/别名（selector 左侧）
// - Origin: Go / GX
// - Exports: 仅 GX 来源有效——「本文件书写的名字 → 被导入方导出名」。
//            由被导入方 pubNames 反向建立：被导入方 `pub func foo` → 导出 `Foo`，
//            则本表记录 foo → Foo；严格来自被导入方，不做 upperFirst 猜测。
type ImportInfo struct {
    Pkg     string
    Path    string
    Origin  ImportOrigin
    Exports map[string]string
}

type Scope struct {
    imports       map[string]ImportInfo // ← 由 bool 升级
    private       map[string]bool
    privateFields map[string]bool
}
```

`HasImport(name)` 保持签名兼容（内部查 `imports[name]` 存在性），新增 `ImportOf(name) (ImportInfo, bool)` 供还原阶段取来源与导出表。

### 4.2 GX 模块描述（`pkg/module`）

```go
// GXModule 一份（或一组同包）.gx 解析后的产物。
type GXModule struct {
    ImportPath string            // 逻辑 import 路径
    Dir        string            // 源文件目录
    Files      []string          // .gx 文件列表（同包）
    // Exports 汇总该包所有 pub 声明的「源名 → 导出名」。
    // 注意：导出名来自被导入方 transformPub 的 names 表，严格保真。
    Exports    map[string]string
    // 每个文件的转译结果，供 driver 落盘。
    Outputs    []*CompiledFile
}

type CompiledFile struct {
    SourcePath string
    GoSource   string
    SourceMap  *sourcemap.SourceMap
}
```

### 4.3 导出符号表的提取

复用 `transformPub` 已经收集的 `names`（小写→导出名）。有两种提取方式：

- **方案 A（推荐，改动小）**：给 `CompileWithModule` 增加一个可选的「只跑 pub 阶段并导出 names」的模式，或复用 `CompileStage(src,"pub")` 并记录 names。`CompileStage` 目前丢弃 `names`，需让它在 pub 阶段把 names 一并返回。
- **方案 B**：新增 `ScanExports(src) map[string]string`，只做 lex + transformPub，返回 names。职责单一、速度快，推荐作为 `pkg/module` 的入口。

> 由于导出符号表只需「声明处的大写结果」，方案 B 最少耦合：它在 GX 源上做 lex → `transformEnum`/`transformPub` → 取 names，不跑 sig/JSX/类型等无关阶段。

---

## 5. 跨文件模块解析流程

### 5.1 项目视图建立

```
build <entry.gx> -root <dir> -module <path>
```

1. 从入口文件所在目录（或显式 `-root`）建立项目根。
2. 扫描根目录下所有 `.gx`，按 `package` 声明分组为 GX 包。
3. 用 `-module` 前缀 + 相对路径推导每个包的逻辑 import 路径（与 Go module 一致）。
4. 建立 `importPath → GXModule` 索引。

### 5.2 import 解析（Resolver）

对每条 import：

```
resolve(importPath):
  if importPath 命中本地 GX 包索引  → ImportGX + 该模块 Exports
  else if importPath == "<module>/runtime/*" → ImportGo（运行时包，特殊保留）
  else                              → ImportGo（外部 Go 包，维持现状）
```

**歧义裁决**：同名路径同时存在本地 `.gx` 包与 Go 包时，优先本地 GX（因为用户显式在 GX 项目里写了这个路径）。该决策需在文档中明示，并提供 `-prefer=go` 逃生开关。

### 5.3 依赖图与拓扑序

- 对每个 GX 模块扫描其 `.gx` 的 import，构建有向图。
- 拓扑排序决定转译顺序（被依赖方先转译，才能拿到它的 Exports）。
- 检测环：发现环时给出清晰的 `.gx` 路径链诊断（如 `a.gx → b.gx → a.gx`）。首期对环直接报错，不做 Go 风格的循环容忍。

### 5.4 逐文件转译

对拓扑序中的每个 `.gx` 文件调用 `CompileWithModule`，并把「当前项目所有 import 的来源信息」传入。被导入 GX 模块的 Exports 在转译导入方之前已就绪（拓扑序保证）。

---

## 6. 来源敏感的大小写还原（核心改造）

### 6.1 `transformExternal` 的分支

现状（无条件 `upperFirst`）：

```go
if scope.HasImport(left) {
    out[k].Text = upperFirst(sel) // 一律机械大写
    continue
}
```

改为按来源分支：

```go
if info, ok := scope.ImportOf(left); ok {
    switch info.Origin {
    case ImportGo:
        out[k].Text = upperFirst(sel)          // 维持现状
    case ImportGX:
        if real, ok := info.Exports[sel]; ok {
            out[k].Text = real                 // 严格用被导入方导出名
        } else if _, isPub := info.Exports[upperFirst(sel)]; isPub {
            // 书写方已用导出名（渐进迁移）：原样保留
        } else {
            // 不在导出表：可能是被导入方的私有符号或未 pub 的标识符。
            // 保持原样，交由 Go 类型检查报错——不做 upperFirst 猜测。
        }
    }
    continue
}
```

要点：
- **GX 来源严格查表**，不猜。这实现了「导入 GX 代码严格区分大小写」。
- 书写方用小写 `foo` 引用被导入方的 `pub func foo`（导出 `Foo`）：查表 `foo → Foo`，还原正确。
- 书写方用大写 `Foo` 直接引用：命中「已用导出名」分支，原样保留（渐进迁移兼容）。
- 写错大小写（如 `FOO`）：不在表中，保持原样不改赌；对导出表做大小写不敏感唯一匹配命中 `Foo` 时，向 `Analysis.diags` 追加 `import-case` 诊断（含正确拼写与 GX 坐标），由检查器/LSP 直接报出，不必等 `go build` 的 `undefined`——**严格性由「不改产物 + 检查器明确诊断」共同体现**（实现增补，见 §13）。

### 6.2 `transformCompositeFields` 的分支

同理，`lib.Box{value: 9}` 的字段名还原也要区分来源：

```go
if info, ok := scope.ImportOf(pkgName); ok && info.Origin == ImportGX {
    // GX 来源：字段名严格查被导入方的字段导出表（Extends Exports 或单独字段表）。
    // 保持原样/查表，不做 upperFirst。
} else {
    promoteLiteralFields(...) // Go 来源维持现状
}
```

> 注：GX 模块的 struct 字段若需 `pub` 导出，其导出名同样来自被导入方 `transformPub` 的字段大写结果，可并入 Exports（用限定名如 `Type.field` 区分）。首期可先只支持顶层符号，字段级作为二期。

### 6.3 `restoreRefs` 的兼容性

`restoreRefs` 处理的是**本文件**的 pub 符号裸引用，与 import 来源无关，保持不动。但要确保绑定表排除遮蔽的逻辑（`ResolveAt`）不被跨文件改动破坏。

### 6.4 `ImportManager` 与运行时包

`ImportManager` 注入的运行时 import（`Signal` / `list` / `gxscene` / `strconv`）都是 Go 来源，维持现状。需确保它们不会被 GX 解析器误判为本地 GX 包（用 `<module>/runtime/*` 路径前缀白名单排除）。

---

## 7. 管线与 API 改造点

### 7.1 API 变更

```go
// 新增：带模块上下文的转译。
// mod 为 nil 时等价于原 CompileWithModule（单文件 / 零回归）。
func CompileWithProject(file, src, module string, proj *module.Project) (*Result, error)

// module 层新增：
func LoadProject(root, modulePath string) (*Project, error)
func (p *Project) Build(entry string) ([]*CompiledFile, error)
```

`CompileWithModule` 保持原签名与行为（内部委托 `CompileWithProject(..., nil)`），保证现有 CLI / LSP / selftest 零改动。

### 7.2 `cmd/gox build` 扩展

- 新增 `-root <dir>`（项目根，默认取入口文件目录）与 `-prefer=go|gx`（歧义裁决）。
- 当检测到项目内有多份 `.gx` 或入口 import 了本地 GX 包时，走 `Project.Build`；否则走原单文件 `build`。
- `-check`（真实 go build）从单文件扩展到多文件产物目录。

### 7.3 需要改动的现有文件清单

| 文件 | 改动 |
|---|---|
| `pkg/compiler/scope.go` | `imports` 升级为 `map[string]ImportInfo`，新增 `ImportOf` |
| `pkg/compiler/external.go` | `transformExternal` / `transformCompositeFields` 按来源分支 |
| `pkg/compiler/pipeline.go` | 新增 `CompileWithProject`，传模块上下文；原入口委托 |
| `pkg/compiler/pub.go` | 新增/暴露 `ScanExports`（提取 pub 表）|
| `cmd/gox/main.go` | `build` 支持项目模式与 `-root`/`-prefer` |
| 新增 `pkg/module/*.go` | Project / Resolver / GXModule / LoadGraph |
| `docs/language-spec.md`、`docs/architecture.md` | 更新 import 与大小写章节 |

---

## 8. 诊断与 sourcemap

- 每个 `.gx` 独立 emit，各自持有 sourcemap（现有 `emit` 已按 file 生成）。
- `buildcheck` 的错误回投需要知道「Go 产物文件 → 源 `.gx` 文件」的映射：`CompiledFile` 携带该关系，driver 据此选择对应 sourcemap。
- 跨文件符号错误（如引用了不存在的 GX 导出名）在转译期即可发现（Exports 查表失败），诊断回投到**引用方** `.gx` 位置。

---

## 9. 兼容策略与风险

| 风险 | 说明 | 缓解 |
|---|---|---|
| 现有单文件行为回归 | `upperFirst` 规则被大量测试锁定 | `proj=nil` 走原路径；全量测试 + 新旧对拍 |
| `upperFirst` 与「严格大小写」语义冲突 | 旧写法 `newRequest` 对 Go 包仍要兼容 | 只对 `ImportGX` 关闭 `upperFirst`，Go 路径不动 |
| 导入路径歧义 | 本地 `.gx` 与 Go 包同名 | `-prefer` 开关 + 文档明示默认本地优先 |
| 循环依赖 | GX 模块互相 import | 首期检测并报错；拓扑序无法处理时给清晰链路 |
| 字段级大小写 | `lib.Box{value:9}` 的字段名 | 首期支持顶层符号，字段级二期（用限定名并入 Exports）|
| 导出表一致性 | 被导入方 `pub` 与书写方引用大小写不一致 | 严格查表 + 编译期诊断，不猜测 |
| 构建产物落盘 | 多文件 `.go` 需写入正确目录/module | 按 GX 包目录结构生成，保持 import 路径与 Go module 一致 |

---

## 10. 分阶段迁移（每阶段可独立验证）

| 阶段 | 交付 | 验证 |
|---|---|---|
| **P0 基础设施** | `ImportInfo` / `Scope.imports` 升级；`ImportOf`；`ScanExports` | 现有测试零回归；`upperFirst` 行为对 Go 来源不变 |
| **P1 来源分支** | `transformExternal` / `transformCompositeFields` 按来源分支（`ImportGX` 查表） | 单测：Go 来源维持、GX 来源严格查表、写错大小写报错 |
| **P2 模块解析** | `pkg/module`：Project / Resolver / 拓扑序 / 循环检测 | 多 `.gx` 样例项目解析正确；环检测用例 |
| **P3 项目级转译** | `CompileWithProject` + `cmd/gox build` 项目模式 | 入口 `.gx` 导入另一 `.gx`，产物可 `go build` 运行 |
| **P4 诊断与文档** ✅ | 字段级大小写；跨文件错误回投；语言规范/架构文档更新 | 端到端：跨文件类型错误回投到正确 `.gx` 行；字段级严格查表 |

**实现状态**：P0–P4 全部完成（见 §13）。

---

## 11. 一个端到端示例（目标形态）

```
project/
  go.mod          module example.com/proj
  main.gx
  util/
    math.gx
```

`util/math.gx`：
```gx
package util
pub func add(a int, b int) int { return a + b }   // 导出名 Add
```

`main.gx`：
```gx
package main
import "example.com/proj/util"
func main() {
    println(util.add(1, 2))   // 小写书写 → 查 GX 导出表 add → Add
}
```

转译产物：
```go
// util/math.go
package util
func Add(a int, b int) int { return a + b }

// main.go
package main
import "example.com/proj/util"
func main() { println(util.Add(1, 2)) }
```

若把 `main.gx` 写成 `util.ADD(1,2)`（错误大小写）：不在 GX 导出表 → 保持 `ADD` 不改产物；大小写不敏感唯一匹配到导出名 `Add` → GX 检查器报 `main.gx:<行>:<列>: import-case: 符号 "ADD" 大小写错误：包 … 导出的是 "Add"`。`gox check main.gx -root <根>` 与 `gox build`（项目模式）都能直接看到这条诊断，无需 `go build`。这就是「严格区分大小写」的落地语义。

---

## 12. 总结

- 这不是修 bug，而是**突破当前架构的刻意边界**（单文件 + 零依赖字符串还原）。
- 改造的核心只有两件事：① 给 import 打来源标签；② 让大小写还原按来源分支（Go 维持 `upperFirst`，GX 严格查被导入方导出表）。
- 模块解析层独立成 `pkg/module`，`pkg/compiler` 保持纯转译；单文件模式作为 `proj=nil` 特例，零回归可期。
- 建议按 P0→P3 推进，先端到端打通「GX 导入 GX + 严格大小写」，再补字段级与诊断。

---

## 13. 实现对照（P0–P4 全部完成）

下表记录设计到代码的落地位置（实现时对个别命名做了收敛，语义一致）：

| 阶段 | 设计 | 实际落地 |
|---|---|---|
| P0 | `Scope.imports` 升级为 `map[string]ImportInfo`；`ImportOf` | `pkg/compiler/scope.go`：`ImportOrigin`/`ImportInfo`/`collectScopeWithImports`；`collectScope(toks)` 保留为 nil 视图 |
| P0 | `ScanExports(src)` | `pkg/compiler/pub.go`：复用 `transformEnum`+`transformPub` 的 names 副作用 |
| P0 | 供模块层复用的扫描入口 | `pkg/compiler/scope.go`：`ScanImports` / `ScanPackageName` |
| P1 | `transformExternal` 按来源分支 | `pkg/compiler/external.go`：GX 来源严格查 `info.Exports` 不受 `isLowerStart` 门槛限制，写错大小写原样保留 + `import-case` 诊断；Go 来源维持 `upperFirst` |
| P1 | `transformCompositeFields` 按来源分支 | 同文件：Go 来源 `promoteLiteralFields`，GX 来源走 P4 限定名查表 |
| P2 | `pkg/module`：Project/Resolver/GXModule/依赖图/拓扑序/环检测 | 新增 `pkg/module/module.go`：`LoadProject`/`Resolve`/`Order`/`Build` |
| P3 | `CompileWithProject` | `pkg/compiler/pipeline.go`：新增入口；`CompileWithModule` 委托为 `imports=nil` |
| P3 | CLI 项目模式 | `cmd/gox/main.go`：`-root`/`-prefer`；多文件产物逐份落盘 |
| P3/P4 | 多文件诊断回投 | `internal/buildcheck`：`SourceFile`/`CheckFiles`/`SetModulePath`；CLI 按 `.go`→`.gx` sourcemap 回投到对应文件与行（含子目录包） |
| **P4** | 字段级大小写（设计 §6.2 二期） | `pkg/compiler/pub.go:scanFieldExports` 收集 `Type.field → Field` 限定名并入 Exports；`external.go:promoteGXFields` 对 GX 来源复合字面量按限定名查表，未 pub/写错大小写原样保留 |
| **P4** | 语言规范/架构文档更新 | `docs/language-spec.md`（§4.1 来源分流、§13 跨文件项目）、`docs/architecture.md`（分层/管线/跨文件模块节） |
| **增补** | 大小写错误由检查器直接诊断（不再只靠 Go） | `external.go:caseFoldTopLevelExport/caseFoldGXField` 做大小写不敏感唯一匹配 → `Analysis.diags` 追加 `import-case`/`import-field-case`；`CompiledFile.Analysis` 承载；`gox check -root` / `gox build` / LSP 消费 |

**注册测试**：
- `pkg/compiler/importorigin_test.go`：ScanExports（顶层 + 字段限定名）、Go/GX 来源分支、写错大小写、nil 零回归、collectScopeWithImports、GX 复合字面量字段严格查表/写错保留/未 pub 保留；大小写诊断：`TestImportCaseDiag*`（selector/字段诊断、正确写法与真拼写错误不报、Go 来源不报、多候选不猜测）。
- `pkg/module/module_test.go`：分组、Resolve、拓扑序、环检测、跨文件转译、错误大小写保留、跨文件 struct 字段还原；`TestBuildCaseMismatchDiagnostics` / `TestBuildCorrectCaseNoDiagnostics`（检查器直接报大小写诊断 / 正确写法无误报）。
- `internal/buildcheck/buildcheck_test.go`：多包诊断 File 指向子目录产物。

**端到端验证**（临时两包项目）：
- `main.gx` → `util/math.gx`：`gox build main.gx -root <dir> -check` → 产物 `util.Add` / `util.Add(1,2)`，真机 `go run` 输出 `3`。
- 写错大小写 `util.ADD(...)` + `util.box{VALUE:9}`：`gox check main.gx -root <dir>` 直接报 `main.gx:4:18: import-case: 符号 "ADD" 大小写错误：包 … 导出的是 "Add"` 与 `main.gx:5:18: import-field-case: 字段 "VALUE" 大小写错误：类型 box 导出的是 "Value"`；`gox build`（项目模式）同样输出；产物保持 `ADD`/`VALUE` 原样交 Go 兜底。
- 字段级：`model.box{value:9, name:"x"}` → `model.Box{Value:9, Name:"x"}`，未 pub 的 `secret` 保持，selector `b.value` → `b.Value`，真机 `go run` 输出 `9 x`。
- 子目录包类型错误：`util/math.gx` 内 `var s string = 123` → `-check` 报 `util/math.gx:3:20: cannot use 123 ... as string`，回投到正确文件与行。

**与设计的偏差**：
- 设计 §4.1 中 `ImportInfo.Pkg` 由 `collectScopeWithImports` 用源码实际包名覆盖，无需调用方预填。
- 设计 §5.3 拓扑序用 DFS 三色标记实现，环诊断输出完整路径链 `a → b → a`。
- 设计 §7.1 的 Project 结构在本实现中保持内聚（`packages`/`order` 未导出），对外只暴露 `LoadProject`/`Resolve`/`Order`/`Build`。
- 项目 module 路径优先从 `<root>/go.mod` 读取，缺失时回退 `-module`（见 `cmd/gox/main.go:readProjectModule`）。
- 字段级限定名同时存「类型原名」与「upperFirst 类型名」两种 key，复合字面量查表时两种类型名写法与两种字段写法都尝试，兼容渐进迁移。
