# GoX 统一符号表 + 作用域 + 绑定解析改造方案

> 状态：设计提案（待评审）。目标是把分散在各 transform 里的「名字 → 声明 / 作用域」知识收敛为一套贯穿管线的绑定解析服务，从根上消除 signal 遮蔽（P1-1）、pub 遮蔽（M-5）、selector 误保护（L-7）这类「同名冲突」缺陷，并为后续表达式 AST 打地基。
>
> 本方案只解决**符号绑定**这一层，不重写 Go Parser、不新增语法。

---

## 1. 为什么要做

第二轮外部审查的 P1-1（signal 名被参数 / 局部变量遮蔽）不是孤立 bug，而是架构症状：同一份「变量名 → 声明 / 作用域」知识，在编译器里被独立实现了 5 遍以上，彼此零散、坐标基准不一、规则不一致。

| 重复能力 | 各实现位置（`pkg/compiler/`） |
|---|---|
| 收集函数参数名 | `analysis.go:778` `collectParams`；`analysis_sig.go:121/187/223/247`；`scope.go:192→218` `collectParenIdents`；`signal.go:164-174`；`unionguard.go:161` `parseParamBindings`；`typeinfer.go:43`；`objectlit.go:521`；`collections.go:121` |
| 收集局部 `:=` / `var` 名 | `analysis.go:729-775`；`scope.go:148-184`；`signal.go:175-193`；`pub.go:180-195`；`typeinfer.go:74/119` |
| 定位函数参数括号 / 函数体 | `analysis_sig.go:153` `funcParamParens`；内联版 `signal.go:123-152` / `unionguard.go:58-88` / `analysis.go:691-726` / `scope.go:192-215`；`findFuncBodyBrace` `unionguard.go:293`；`pub.go:159-174` 另写朴素扫描 |
| 判断声明区 / 语句边界 | `signal.go:414` `isSignalDeclTarget` / `signal.go:441` `inDeclZone`；`destructure.go:105` `destructStmtStart`；`objectlit.go:213` `exprStartBefore`；`typeinfer.go:153` `exprEndInStmt` / `signal.go:487` `signalExprEnd` |
| 判断标识符是否在某作用域内 | `analysis.go:411` `scopeOf` / `425` `declaredTypeAt` / `443` `narrowAt`；`signal.go:199` `isShadowed`；`pub.go:223-228` 内联；`external.go:165`；`unionguard.go:371` |

直接后果：

- **规则不一致**：signal 用「函数体区间」判遮蔽，`pub.restoreRefs` 用「整个函数体」，`scope.private` 是文件级全局（L-7 的根因）。同一段代码里同名标识符在不同 transform 得到不同结论。
- **坐标基准冲突**：`Symbol` / `NarrowFact` / `FuncSig` 用 token 下标，`Guard` / `ElseShadow` / 诊断用 `Pos`（行列）。`typeinfer.go:20-21` 明确注释「desugar 改写的 token 流会让 Analysis 下标漂移，故每次重扫」——同一份知识被迫算两遍。
- **新特性放大问题**：Signal 在 Analyze 之前（`pipeline.go:62-63`），拿不到语义层，只能自带轻量分析（`signal.go:104-107`），这正是 P1-1 漏判遮蔽的直接原因。

预期的收益：

1. 消除一整类同名冲突缺陷（已修的 P1-1/M-5/L-7 从「打补丁」变成「结构上不可能」）。
2. 删掉 ~5 套重复逻辑，减少代码量与维护面。
3. 让诊断 / sourcemap 用稳定 Pos，消除 `idxNearGx`（`analysis.go:302`）的近似误差。
4. 为表达式 AST 提供现成的绑定基础（AST 节点的 name 直接解析到 Binding）。

---

## 2. 设计原则

| 原则 | 说明 |
|---|---|
| **Pos 是稳定身份，下标是易失坐标** | 所有 transform 保留原始 `Pos`（`token.go:58-66` 已有 Offset/Line/Col；现有 Guard/diag 已用 Pos 免疫漂移）。Binding 以 `Pos`（具体用 `Offset`，见 §3.1）为身份；下标仅在单次扫描内使用，绝不跨 transform 持久化。 |
| **一次构建，全程查询** | 在 lex 之后构建一次绑定表，贯穿 signal → Analyze → 所有 desugar → emit。各 transform 从「扫 token 猜」改为「按当前 token 的 Pos 查表」。 |
| **词法作用域解析（shadowing-aware）** | `ResolveAt(name, pos)` 沿作用域链从内到外解析，天然处理参数 / 局部变量 / 字段 / 包级符号的同名遮蔽，替代各 transform 手写的排除规则。 |
| **单一事实来源，渐进迁移** | 新表与现有 `Analysis` 先并行、对拍验证，再逐个把消费者切到新 API，最后删除旧逻辑。不搞 big-bang 重写。 |

---

## 3. 数据结构

### 3.1 稳定身份用 Offset

`Pos` 三者中，`Offset`（0 基字节偏移）在 lex 时确定、单调、不受插入/删除 token 影响，最适合做作用域边界比较（`start <= off < end`）。`Line/Col` 用于诊断展示与 sourcemap。统一用 `Pos` 作为 key 类型，内部比较以 `Offset` 为准（`keyOf` 现用 `{line,col}`，见 `analysis.go:237-239`；新表统一改用 Offset，见 §3.5 兼容策略）。

### 3.2 Binding（绑定）

```go
// BindingKind 是一个名字被声明的语义类别。
// 它取代各 transform 里零散的「这是不是 signal / 是不是参数 / 是不是 pub」判断。
type BindingKind uint8

const (
    BkParam BindingKind = iota // 函数/方法形参
    BkReceiver               // 方法接收者
    BkLocal                  // := 短变量
    BkVar                    // var 声明（局部或包级）
    BkConst                  // const 声明
    BkFunc                   // 函数名 / 方法名
    BkType                   // type 声明（含别名）
    BkField                  // struct 字段（含匿名 struct）
    BkEnumMember             // enum 成员
    BkSignal                 // signal 声明 → 运行期 Signal
    BkComputed               // Signal.computed 派生的只读 signal
    BkImport                 // import 包名 / 别名
    BkRangeVar               // for x := range 的迭代变量
    BkLabel                  // goto / break / continue 标签
)

// Binding 是一个声明的规范化表示。DeclPos 是它在原始源码中的稳定身份。
type Binding struct {
    ID       int          // 绑定表内唯一编号，便于生成唯一临时名
    Name     string       // 声明名（保留源码大小写）
    Kind     BindingKind
    Type     string       // 类型文本（尽力推断，未知为 ""）；复用现有 typeFromTokens / inferExprType
    DeclPos  Pos          // 声明 token 的位置（稳定身份）
    Scope    *Scope       // 所属作用域
    // IsPub 由 pub/enum 阶段回填：导出名映射的镜像，供 restoreRefs 统一查询
    IsPub    bool
    PubName  string       // 导出后的名字（如 foo → Foo）
}
```

### 3.3 Scope（作用域）与 ScopeTree

```go
// ScopeKind 区分作用域种类；selector 保护等规则依赖它（字段/方法只在 BkField/BkFunc 有效）。
type ScopeKind uint8
const (
    SkGlobal ScopeKind = iota // 包级
    SkFunc                    // 函数体（含接收者/参数）
    SkBlock                   // if/for/switch/裸 {} 块
    SkStruct                  // struct 字面量/定义体（字段名空间）
)

// Scope 是词法作用域树的一个节点。边界用 Pos(Offset) 表示，不受下标漂移影响。
type Scope struct {
    ID       int
    Kind     ScopeKind
    Parent   *Scope
    Children []*Scope
    From, To Pos             // 覆盖的 token 区间（半开），按 Offset 比较
    Bindings map[string][]*Binding // 同名可多次声明（不同块），按 DeclPos 排序
}
```

`ScopeTree` 提供 `ScopeAt(pos) *Scope`（找包含 pos 的最深作用域），是 `ResolveAt` 的基础。

### 3.4 Bindings（绑定表 / 解析服务）

```go
// Bindings 是贯穿管线的绑定解析服务。一次构建，全局查询。
type Bindings struct {
    scopes   []*Scope
    byPos    map[int]*Binding  // DeclPos.Offset → Binding，声明点 O(1) 命中
    all      []*Binding
    root     *Scope            // SkGlobal
    // 生成 token 可能复用源 Pos；用 IsGen 排除，避免把 `count()` 的括号误当声明。
}

// —— 核心查询 API（各 transform 统一调用）——

// ResolveAt 解析 pos 处对 name 的引用，沿作用域链从内到外，返回词法可见的绑定。
// 这是消除遮蔽类 bug 的核心：signal/pub/typeinfer 不再自己扫 token 判断遮蔽。
func (b *Bindings) ResolveAt(name string, pos Pos) *Binding

// DeclAt 返回 pos 处的声明绑定（供「声明目标不改写」判断，取代 isSignalDeclTarget）。
func (b *Bindings) DeclAt(pos Pos) *Binding

// ScopeAt 返回包含 pos 的最深作用域。
func (b *Bindings) ScopeAt(pos Pos) *Scope

// SelAt 解析 selector `recv.sel` 中 sel 位置 pos 的绑定，供 selector 保护判断。
// 返回 nil 表示 sel 无本地绑定（可能是外部符号 / 需大写的 pub 字段）。
func (b *Bindings) SelAt(sel string, recvPos, selPos Pos) *Binding
```

关键语义：

- `ResolveAt("count", pos)`：若 pos 落在 `getCount(count int)` 的函数体内，沿作用域链先命中参数的 `Binding{Kind: BkParam}`；signal 只改 `Kind==BkSignal|BkComputed` 的 binding → **P1-1 结构上不可能再误改参数**。
- `restoreRefs`：命中 `IsPub==true` 才大写；命中局部遮蔽（BkLocal/BkParam）不动 → **M-5 消除**。
- `external` 的 selector：`SelAt` 只对 `BkField/BkFunc/BkMethod` 返回绑定并判私有；局部变量同名不再误保护 → **L-7 消除**。

### 3.5 与现有 Analysis 的兼容

- 阶段 1–4：`Bindings` 与 `Analysis` 并行，`Result` 增加字段 `Bindings *Bindings`（可选），旧字段不动。
- `Analysis.TypeAtGx` 等 AST 侧查询逐步改为「先用 `Bindings.ResolveAt` 定位 Binding，再取 Type」，消除 `idxNearGx`（`analysis.go:302-332`）的近似。
- 阶段 5：`Symbol` 与 `Binding` 合流（可由 `Binding` 派生 `Symbol`），`Analysis.symbols` 成为 `Bindings.all` 的视图。**此时才统一坐标基准**——这是全案风险最高的一步，单独一个阶段 + 完整回归。

---

## 4. 构建算法（Binder）

一次线性扫描 `toks`（`bindings.go`，新增文件），用栈维护当前作用域：

```
Binder.Build(toks):
    root = newScope(SkGlobal, whole file)
    stack = [root]
    for i, t in toks:
        switch:
        case t.is("func"):
            解析接收者/参数/返回 → 建 SkFunc scope，压栈
            // 复用 parseParamSeg（unionguard.go:122，已是共享件）
        case t.is("signal"):
            name = nextSig; 建 BkSignal，绑定当前 scope；标记该 name 为 signal
        case t.is("var"/"const") / t.is(":=") / t.is("range"):
            建 BkVar/BkConst/BkLocal/BkRangeVar，绑定当前 scope
        case t.is("type") / struct 字段:
            建 BkType / BkField（struct 用 SkStruct scope）
        case t.is("enum") 成员:
            建 BkEnumMember
        case import spec:
            建 BkImport（复用 collectImportSpecs scope.go:42）
        case "{" / "}":
            压入/弹出 SkBlock（func 体的 "{" 复用当前 SkFunc，不额外建块）
    Bindings.byPos[t.DeclPos.Offset] = binding
    Bindings.scopes ...
```

要点：

- **不推断类型也能建绑定**：类型推断失败只影响 `Type` 字段，不影响遮蔽解析。构建阶段不调用 `typeFromTokens` 等重逻辑，保证 O(n) 且无副作用。
- **signal 声明识别直接用 `t.is("signal")`**：此时 signal transform 尚未运行，声明原貌可见。
- **struct / 匿名 struct 字段**：复用现有 `collectStructFields`（`scope.go:267`）的嵌套深度处理。
- **短变量重声明**：`x, y := ...` 中若某名字在当前 scope 已存在且类型相同，复用 Binding（Go 短声明语义）；否则新建。
- **for/switch 的 init 语句**：其声明作用域覆盖整个块，需在压 SkBlock 前登记。

---

## 5. 各 transform 的迁移映射

| Transform | 现在的做法 | 迁移后 | 删除的旧逻辑 |
|---|---|---|---|
| `transformSignals`（在 Analyze 前） | `collectSignalVars` + `collectSignalShadows` + `inDeclZone` + `isSignalDeclTarget` | 每个候选标识符 `ResolveAt(name, t.Pos)`，仅当绑定 `Kind∈{BkSignal,BkComputed}` 才改写；右侧表达式同理 | `collectSignalShadows` / `isShadowed` / `inDeclZone` / `isSignalDeclTarget` |
| `restoreRefs`（pub） | `collectLocalShadows` + 内联区间判断 | `ResolveAt(name, pos)`，命中 `IsPub` 才大写；局部遮蔽自动排除 | `shadowRange` / `collectLocalShadows` |
| `transformExternal` | `scope.IsPrivate` / `IsPrivateField` / `HasImport` | `SelAt` + `ResolveAt`，统一用 Binding.Kind 判断 | `Scope` 的三个 map（合入 Binding） |
| `inferIdentType` / `lookupVarType` | 每次重扫 token（因下标漂移） | `ResolveAt(name, usePos).Type`，稳定且 O(1) | 各 `inferIdentFrom*` / `lookupVarType*` 的重扫逻辑 |
| `inferContextFuncType`（lambda） | 扫签名文本 + `an.FuncSig` | `ResolveAt(callee).Type` 直接取函数类型；参数绑定可查 | 上下文函数类型的文本解析兜底可保留为 fallback |
| `detectDestructures` | 自行判断目标与字段 | `ResolveAt` 确认解构目标绑定 + 字段存在性 | `rhsIdentifier` 的临时判断 |
| `collectUnionGuards` | `parseParamSeg` 等 | 复用 Binder 的参数解析产物 | 重复的 `findFuncBodyBrace` 等（保留公共件） |

**signal 迁移的可行性是全案关键**：signal 在 Analyze 前，但 Binder 在 lex 后、signal 前构建，因此 signal 可直接查表。这解决了当前「signal 拿不到符号表」的结构性矛盾。

---

## 6. 管线调整

```
toks := New(src).AllTokens()
bindings := Bind(toks)                  // ← 新增：一次构建，贯穿全程
toks, defs := stripDefaults(toks)
toks = transformSignals(toks, bindings) // ← 查表改写（P1-1 根治）
analysis := Analyze(toks, bindings)     // ← 消费绑定表（渐进：先 ignoring，后合流）
... 其余 transform（逐步接收 bindings） ...
scope := collectScope(toks)             // 阶段 4 后删除，改由 bindings 提供
toks = restoreRefs(toks, bindings)      // ← 查表
toks = transformExternal(toks, bindings)
goSrc, sm := emit(toks, file)
```

`CompileStage`（`pipeline.go:135`）只跑单阶段，需保证 Binder 在所有 stage 前构建，返回值的 `Bindings` 可供调试 stage 使用。

---

## 7. 分阶段迁移计划

每阶段独立可回滚、独立全绿（go test / vet / build / race）后才进入下一阶段。

| 阶段 | 内容 | 验证 | 风险 |
|---|---|---|---|
| **P0 数据结构 + Binder** | 新增 `bindings.go`：`Binding`/`Scope`/`Bindings` + `Build` + 查询 API；加 `TestBindings*` 单元对拍 | 新表对现有 golden 样本的解析结果做断言；不动任何 transform | 低（纯新增） |
| **P1 signal 迁移** | `transformSignals` 接 bindings，删轻量遮蔽分析 | P1-1 三个回归测试 + signal 全量 + 真实 go build | 中（作用域边界） |
| **P2 pub 迁移** | `restoreRefs` 接 bindings，删 `collectLocalShadows` | M-5/L-7 回归 + pub 全量 + golden | 中 |
| **P3 类型查询迁移** | `inferIdentType` / `lookupVarType` 改查 Binding.Type | 对象字面量 / 模板 / lambda 全量 + golden | 中（Type 推断一致性） |
| **P4 external/scope 合流** | selector 保护改 `SelAt`，删除 `Scope` map | L-7 / external 全量 + golden | 中 |
| **P5 Analysis 合流** | `Symbol` 由 `Binding` 派生，统一坐标基准 | AST 侧全量（buildcheck/check）+ 诊断回投对拍 | **高** |
| **P6 诊断精确化** | 诊断/sourcemap 用 `Binding.DeclPos`，去 `idxNearGx` 近似 | LSP 诊断坐标对拍 | 中 |

建议：P0–P4 先落地（收益明确、风险可控），P5/P6 在 P0–P4 稳定一段时间后再做——P5 是唯一的高风险阶段，需要专门的诊断对拍基线。

---

## 8. 与表达式 AST 的关系

本方案是三层建议（符号系统 → 表达式 AST → 运行时语义）的第一层，且是后两层的前置：

- **符号绑定是 AST 的语义底座**：AST 节点的 `Ident` 需要解析到唯一声明才有意义。`ResolveAt` 已提供这个能力，AST 阶段可直接复用 `Binding`。
- **本方案不引入 AST**：仍以 token 流 + Pos 为操作对象，保持现有 transform 架构，避免大爆炸式重写。等符号层稳定后，可逐个把「扫 token 的模式匹配」替换为「遍历 GX 扩展表达式 AST」，而绑定表无需改动。
- **边界**：GX 扩展表达式（lambda `=>`、对象字面量 `{k:v}`、解构 `{a,b}:=p`、Signal 读写）才需要 AST；普通 Go 表达式继续交给官方工具链，不重复造 parser（与既有「方案 B」决策一致）。

---

## 9. 风险与缓解

| 风险 | 缓解 |
|---|---|
| **Pos 在生成 token 上失真**：多个 transform 生成 token 复用源 Pos，`byPos` 可能冲突 | 生成 token 标 `IsGen`；查询前跳过 IsGen；`byPos` 只登记用户声明 token |
| **短变量重声明 / 闭包捕获语义复杂** | 阶段 0 用对拍测试覆盖 Go 短声明语义；不确定时保守新建 Binding 而非复用 |
| **作用域边界用 Pos 比较的边界误差**（如 `}` 的 Pos 归属） | 明确半开区间 `[From, To)`，用 Offset 比较；单元测试覆盖边界 |
| **P5 坐标合流引入大范围回归** | 单独阶段 + 诊断对拍基线 + 可回滚；P0–P4 稳定后再动 |
| **性能**：多次 `ScopeAt` 栈查找 | Binder O(n) 一次构建；`ScopeAt` 用有序区间 + 二分；总体应快于现在的多遍重扫 |
| **并行期两个真相源不一致** | 阶段内以「新表对拍旧逻辑」为硬性门禁；不一致必须先解释再推进 |

---

## 10. 验收标准

1. **缺陷根除**：P1-1 / M-5 / L-7 的回归测试在对应 transform 迁移后仍全绿，且相关手写排除逻辑被删除（用 grep 确认无残留）。
2. **重复消除**：§1 表格中的重复函数数量显著下降（目标：`collectSignalShadows`/`collectLocalShadows`/`inDeclZone` 等专项补丁函数归零）。
3. **全量质量门禁**：每阶段 `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...` 全绿；golden 快照无意外变更。
4. **稳定性**：绑定表对同一源码多次构建结果一致；对拍测试证明 Pos 锚定在全部 transform 后仍可定位到正确声明。
5. **诊断精度**（P6）：LSP 诊断回投坐标与人工标注一致，`idxNearGx` 近似路径被替换。

---

## 11. 不做的部分

- 不重写 Go Parser、不解析标准 Go 表达式（沿用「方案 B：go/parser AST + token side table + sourcemap」的分工）。
- 不在本方案内新增语法（字段重命名解构、嵌套解构、Signal Dispose 等均属语言演进，另行提案）。
- 不改运行时语义（Effect 调度、Computed 失效已在第二轮 P1 修复中处理）。

---

## 12. 实施结果（2026-11-20）

方案落地后的实际状态与偏差记录（以代码为准）：

| 阶段 | 状态 | 落地内容 | 备注 |
|---|---|---|---|
| **P0** | ✅ 完成 | 新增 `pkg/compiler/bindings.go`：`BindingKind`/`Binding`/`ScopeKind`/`BindScope`/`Bindings` + `Bind(toks)` 一次线性扫描 + `ResolveAt`/`DeclAt`/`ScopeAt`/`TypeAt`/`SelAt` 查询 API；11 个 `TestBind*` 单元测试对拍 | 因与既有 `scope.go:Scope` 类型重名，作用域类型命名为 `BindScope` |
| **P1** | ✅ 完成 | `transformSignals` 接收 bindings，`isSignalRefAt` 用 `ResolveAt` 判遮蔽；删除 `collectSignalShadows`/`isShadowed`（106 行专项补丁）。`inDeclZone`/`isSignalDeclTarget` 作为声明形态防御层保留 | P1-1a/b 回归测试 + signal 全量通过 |
| **P2** | ✅ 完成 | `restoreRefs` 接收 bindings，局部遮蔽由 `ResolveAt` 判定；删除 `shadowRange`/`collectLocalShadows`（57 行）。M-5/L-7 回归通过 | |
| **P3** | 🟡 数据层完成 | Binder 补全 `Binding.Type`（参数/var/`:=` 字面量）；新增 `TypeAt` API + 对拍测试，证明绑定表类型与 `inferIdentType` 一致 | **未切换** `inferIdentType`/`lookupVarType` 的调用者：它们在 desugar 后token 流上工作，切换需跨多文件传 bindings 且回退成本高；保留现有重扫以确保零回归。类型查询的绑定表路径已就绪，待 AST 阶段统一接入 |
| **P4** | ⚠️ 评估后回滚 | 尝试将 selector 私有保护改为 bindings 判定 | **不可行**：`Bind` 在原始流（lex 后）构建，而 (1) import 由 JSX/collections/信号 transform 在管线中注入、(2) 对象字面量在 desugar 后才展开成匿名 struct，Bind 均看不到。迁移导致 destructure 回归（`p.name`→`p.Name`），已回滚。`collectScope` 在管线后段收集的设计正确，保留。`SelAt` 作为 API 保留供 AST 阶段使用 |

### 关键结论

- **核心目标达成**：P1/P2 把 signal 遮蔽（P1-1）、pub 遮蔽（M-5）两类同名冲突从「手写区间补丁」变成「词法作用域解析」，结构上不可能再误改参数/局部变量。
- **一处架构边界**：绑定表适合在 lex 后一次构建、贯穿查询「源码里写了什么声明」；但**管线中动态注入的符号**（import、匿名 struct 字段）必须由后段 `collectScope` 兜底。二者是互补关系，不是替代关系——这是 P4 回滚揭示的约束。
- **对后续 AST 阶段**：`ResolveAt`/`TypeAt` 已提供稳定的「标识符 → 声明/类型」查询，AST 节点可直接复用；P3 调用者切换与 P4 selector 合流建议放到 AST 阶段一并处理（届时注入符号已有 AST 表示）。
- **质量门禁**：每阶段 `go test ./...` 全绿；golden 无意外变更。

---

## 13. 全面接入（2026-11-20 续）

针对「绑定表建好后旧代码没接入」的问题，本轮把词法层面的符号查询全面切到绑定表。

### 13.1 能力盘点：散落查询分三类

| 类别 | 函数 | 性质 | 处理 |
|---|---|---|---|
| **A 词法名字→类型** | `inferIdentType`/`FromParams`/`FromDecl`/`FromRange`、`lookupVarType`、`exprIsInt`、`inferValueType`(Ident 分支) | 重复实现「名字→声明类型」 | 切到 `Bindings.TypeAt` |
| **B 函数签名反查** | `declaredParamTypeAt`、`inferReturnExpectType`、`inferArgExpectType`、`declaredVoidFunc`、`inferContextFuncType`(实参分支) | 查 func 签名第 i 参数/返回类型 | 切到绑定表的原始文本签名索引 `FuncSigRaw` |
| **C 表达式形状推断** | `inferExprType`、`inferExprLitType`、`inferParamTypeFromBody`、`inferCollectionElemType`、`inferValueType`(字面量分支) | 纯 token 形状分析，与符号表无关 | **不接入**（非符号表职责） |

### 13.2 落地内容

- **统一查询入口** `lookupIdentType(b, toks, name, useIdx)`：优先 `b.TypeAtIdx`（Pos 稳定、O(1)、天然遮蔽正确），未命中回退旧扫描。所有 A 类调用点经此入口。
- **Binder 类型能力补齐**：
  - 修 `paramsTypeText` —— 原实现在 `[]int` 里插空格成 `[] int`，导致 `structs[typ]` 键匹配失败；统一用 `exprText` 原样拼接。
  - `upgradeRangeVars` 补 range 元素类型（`sliceElemType`），与旧 `inferIdentFromRange` 对齐。
  - `scanVarConst`/`bindLHS`/参数均已填 `Binding.Type`。
- **签名索引** `Bindings.funcSigs`：`scanFunc` 时为具名函数记录原始文本签名（参数类型列表/返回类型/isVoid），保留别名不展开。查询 `FuncParamAt`/`FuncResult`/`FuncIsVoid`。`Analysis.FuncSig`（语义层、展开别名）与新索引（词法层、原始文本）互补。
- **参数透传**：`pipeline` → `transformLambdas`/`transformObjectLits`/`expandDestructures`/`transformTemplates` 及各内部 helper 均接收 `*Bindings`。

### 13.3 验证

- **对拍测试**（`bindings_test.go` 新增 3 项）：`TestBindTypeQueryParity`（参数/var/`:=`/range/链式/遮蔽逐一比对）、`TestBindFuncSigParity`（参数/返回/isVoid）、`TestBindShadowTypePrefersBinding`（遮蔽场景证明绑定表给出正确类型）。全绿。
- **回退触发率诊断**：5 个代表性编译场景（参数/对象字面量/模板/range/解构）中 `lookupIdentType` 的回退分支命中 **0 次** —— 绑定表已实质接管 A 类查询，回退仅为长尾安全网。
- **质量门禁**：`go test -race ./...`、`go vet ./...`、`go build ./...` 全绿，golden 无意外变更。

### 13.4 保留回退的理由

`inferIdentType` 系列与 `declaredParamTypeAt`/`declaredVoidFunc` 的旧扫描体保留为回退层：
- 它们互相依赖（链式字面量推断 `i := j; j := 0`），构成完整回退子系统，删一损全；
- `inferExprLitType` 仍被 Binder 自身用于字面量类型推断（C 类，必要）；
- `Analysis.FuncSig` 展开别名，不满足对象字面量「保留原始类型名」的需求，故 B 类回退也有价值。
绑定表是主路径（回退率 0），旧扫描是安全网——这是「双轨过渡」的最终形态，而非未清理的死代码。

---

## 14. P5 调研与落地（2026-11-20）

P5 立项目标是「`Symbol` 由 `Binding` 派生、统一坐标基准、消除 `collectSymbols` 重扫」。调研后按证据收窄了范围，并记录完整合流的障碍。

### 14.1 调研发现（决定性证据）

| 发现 | 证据 | 影响 |
|---|---|---|
| `Analysis.Symbols()` 生产代码零消费者 | grep 全仓：仅测试调用；LSP `symbolmap.go` 用独立正则扫描，不消费它 | "消除重扫"收益面比想象窄；真正消费符号数据的只有 `declaredTypeAt`（类型检查核心） |
| `Symbol.Type` 是语义类型，`Binding.Type` 是词法文本 | `Type` 是 `{Name, Members}` 结构，联合/命名别名展开由 `typeFromTokens`+`namedTypes` 算；`Binding.Type` 只是字符串 | Binding 无法直接替代 Symbol 的 Type，语义计算必须在 Analysis 侧保留 |
| signal 声明的 Pos 在 transform 后漂移 | `signal count = 0` → `var count = Signal.New(0)`，生成 token 复用 `signal` 的 off=13，而 Binding 的 `count` 声明是 off=20 | Binding.DeclPos 无法映射到 Analyze 后流的 token 下标，"统一坐标基准"对 signal 失效 |
| `collectParams` 共享类型参数丢失 | `a, b int\|string` 只有 b 进符号表，a 的联合类型丢失 → `_ = a + 1` **漏报** union-operand | 真实 bug，有修复价值 |

### 14.2 落地内容（收窄为高价值参数合流）

- **`collectParams` 改用 `parseParamBindings`**（与 Binder 同源的共享类型切分），修复 `a, b int|string` 中 a 的参数符号与联合类型丢失。
- 修复前红灯验证：`TestAnalyzeSharedTypeParams` 断言 a、b 都是 `int|string`，改动前失败（symbols 只有 `b:int|string`），改动后通过。
- check 层回归 `TestSharedTypeParamBothUnions`：共享类型参数 a、b 的未收窄 `+` 运算都报 union-operand（改动前只报 b）。
- 语义 Type 计算仍在 Analysis 侧（`typeFromTokens`），Binding 只提供同源的名字/类型段切分能力——不强行派生整个 Symbol。

### 14.3 对拍与门禁

- **基线快照对拍**（改动前后 `Symbols()`/`declaredTypeAt`/诊断逐项比对）：唯一差异是 SRC1 `a, b int` 多收了 `a`（预期修复），其余逐字一致。
- golden 哈希无变更；`go test -race`/`vet`/`build ./...` 全绿，gofmt clean。

### 14.4 完整 P5 合流暂缓的理由

「Symbol 全部由 Binding 派生 + 统一坐标基准」不做，原因：
1. **消费者缺失**：`Symbols()` 生产代码零调用，消除重扫的实际收益仅是去掉一个测试消费的中间数组。
2. **语义分层**：`Symbol.Type`（联合/别名展开的语义类型）与 `Binding.Type`（词法文本）是不同抽象，强行派生会丢语义；Analysis 侧的 `typeFromTokens` 是必要的语义计算层。
3. **坐标硬障碍**：signal 声明 Pos 在 transform 后漂移，Binding 的原始流 Pos 无法统一到 Analyze 后流坐标——这是「统一坐标基准」无法跨越的结构性问题（与 P4 的注入符号时序问题同类）。

结论：**符号系统的架构合流（P4/P5 完整形态）在当前 token-transform 架构下不可行**，其收益要在「表达式 AST 阶段」才有实质载体——届时 AST 节点直接持有 Binding 引用，无需再靠 Pos 跨流映射。本次只落地了调研中发现的真实 bug 修复。

---

## 15. 表达式 AST 阶段（E0/E1，2026-11-20）

前面的结论是：符号架构合流要等「表达式 AST」才有载体。本轮正式启动这一层，按「逐个替换、避免大爆炸」的节奏推进，先拿 signal 开刀。

### 15.1 为什么 signal 是合适的首个切入点

signal transform 位于管线最前段（Analyze 之前），处理的是**原始 token 流**——AST 节点用 `Pos.Offset` 索引，与原始 token 一一对应，**不存在 P4/P5 的坐标漂移**（那些问题都发生在 desugar 后流）。这让「AST 节点直接持有 Binding」第一次有了稳定载体。

### 15.2 E0：AST 地基层（提交 a6ce1da）

- 新增 `pkg/compiler/exprast.go`：`GXExpr` 节点持有 `Pos.Offset`（稳定锚点）+ `Binding` 引用 + token 区间；`SignalAST` 在原始流一次构建，`byOffset` 以 Offset 为键 O(1) 查询，不受 token 下标漂移影响。
- `ParseSignalExprs` 解析 signal 声明/读/写/复合/自增节点，形态排除（selector 右侧、字段键、声明目标、声明区、已调用）与旧逻辑逐条对齐；遮蔽由 `ResolveAt` 的 `BkSignal`/`BkComputed` 判定天然处理。
- 4 个单元测试：声明节点、读写分类、遮蔽、与旧判定全量对拍，全绿。**纯新增，不改任何 transform 行为**。

### 15.3 E1：signal 识别收敛到 AST（本轮）

- `transformSignals` 改为构建 `SignalAST`，早退条件用 `ast.HasSignal()`；`rewriteSignals`/`rewriteSignalReads` 消费 AST 节点 Kind 驱动改写，**主循环删除「名字集合 + 遮蔽 + 形态排除」的重复扫 token 判定**（净删 84 行）。
- 等价性验证（三重）：
  1. **节点级对拍** `TestExprASTParityWithLegacy`：AST 认定的 signal 引用 offset 集合与旧判定完全一致。
  2. **端到端对拍**：16 个覆盖全分支的样本（声明/读写/复合/自增/RHS 读取/computed/effect/遮蔽/selector 排除/参数组/嵌套/链式赋值），新旧版本编译输出**逐字一致**。
  3. **全量门禁**：`go test -race`/`go vet`/`go build ./...` 全绿，golden 无变化。
- `collectSignalVars`/`isSignalRefAt` 仅留作对拍参照，生产路径已不调用。

### 15.4 后续（E2/E3）

- **E2**：把 lambda / 对象字面量 / 解构 / 模板的 detect 逐个切到 AST 层，删除各自的扫 token 消歧补丁。这些 transform 在 desugar 中后段工作，需处理 offset→当前流下标的映射（signal 阶段不涉及），风险高于 E1，逐个做、各自对拍。
- **E3**：Analysis 类型/诊断直接读 AST 节点（P6 已判 `idxNearGx` 收益低，暂缓）。
