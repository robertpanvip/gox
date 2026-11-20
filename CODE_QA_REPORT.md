# GX 编译器全程序质检报告

质检日期：2026-11-20　范围：全仓 16924 行 Go 代码（39 个 compiler 文件 + runtime + internal + cmd + bindings）

## 检验方法与可信度标注

- **【实测】** = 我在 `/tmp` 副本编译 `Compile`/`CompileStage` 观察生成代码，或用 `go build` / `go test -race` 复现，原仓库未改动
- **【审查】** = 代码路径确认存在问题，但受时序/环境限制未稳定复现

静态工具结果：`go vet ./...` 零告警；`go test -race ./...` 全绿（但 signal 的逻辑竞态 race detector 检测不到，见 H-10）；无真实 `panic()` 调用；无遗留 TODO。

---

## 阻断级（生成代码无法编译 / 进程崩溃）

| ID | 位置 | 问题 | 验证 |
|---|---|---|---|
| B-1 | `enum.go:204-236,140-168` | `pub enum E { a = 1; b }` 中省略值的成员复用上一常量值，`String()` 生成 `case EA`+`case EB`（同值），go build 报 `duplicate case EB` | 【实测】go build 失败 |
| B-2 | `signal.go:139-154,99-112` | computed 无循环依赖检测，`c = Computed(func(){ return c()+1 })` 无限递归 → `fatal error: stack overflow`（不可 recover，直接杀进程） | 【审查】代码路径确认 |

## 高（生成错误代码 / 合法语法被拒 / 静默语义错误）

| ID | 位置 | 问题 | 验证 |
|---|---|---|---|
| H-1 | `lambda.go:544-585` | 嵌套 lambda 用替换前旧下标从后往前替换，内层展开后外层区间错位，生成 `func(x any) any {return (y => y + x)} {return y + x})`（`=>` 残留、片段重复） | 【实测】 |
| H-2 | `lambda.go:304-334` | 函数实参 `apply(x => x)` / 赋值 `var f func(int) int = x => x` 未从上下文推断类型，降级 `func(x any) any`，与期望签名不符 → go build 类型错误 | 【实测】 |
| H-3 | `signal.go:108-129,186-203,216-231` | 赋值/声明/复合赋值右侧的 signal 读取未改写：`signal b = a` → `Signal.New(a)`（应 `a()`）；`a += b` → `a(a() + b)`（应 `b()`） | 【实测】 |
| H-4 | `scope.go:245-274` + `external.go:164-168` | `collectStructFields` 每行只收第一个标识符，`type T struct { a, b int }` 的 `b` 漏保护，`x.b` → `x.B` 误大写 → go build 失败 | 【实测】 |
| H-5 | `importmanager.go:119-141` + `collections.go:243-270` | 局部变量名恰好是占位别名时遮蔽导入：`list := "hi"` 后集合链式生成 `xs := list.List[int]{...}`，`list.List` 被局部变量遮蔽 | 【实测】 |
| H-6 | `pipeline.go:157-187` | `CompileStage(src,"all")` 中 `toks, defaultDefs := ...` 的 `:=` 使 `toks` 被 shadow，所有 transform 改的是局部副本，`all` 阶段完全不转译、原样输出（CLI `gox all` 及调试 API 受影响） | 【实测】 |
| H-7 | `analysis.go:873-889` | `if c {} else if v is T {}` 链首非守卫时 `continue`，`else if` 守卫被 `processed` 跳过，合法写法被误报 `is 守卫仅支持简单形态` | 【实测】 |
| H-8 | `signal.go:34-37,99-112` | `observer` 是包级全局，`withObserver` 在锁外执行回调（L106），并发下互相覆盖，依赖登记到错误节点；赋值都在锁内，race detector 检测不到（逻辑竞态） | 【审查】 |
| H-9 | `signal.go:141-154,156-169` | `st.value = f()`（L149）在锁外写，读在锁内（L131/166），并发读同一 dirty computed 无锁保护、重复执行 `f()` | 【审查】 |
| H-10 | `lsp/server.go:300,308` | `Content-Length` 用 `Atoi` 忽略错误且允许负值，`Content-Length: -1` → `make([]byte,-1)` panic，LSP 进程崩溃 | 【审查】 |

## 中（静默降级 / 坐标错位 / 语义瑕疵）

| ID | 位置 | 问题 |
|---|---|---|
| M-1 | `lambda.go:590-598` | `tokenizeTypeText` 用 `New(text).AllTokens()`，生成 token 的 Pos 是片段局部坐标（1:1 起），未设为 lambda 位置 → sourcemap 回投到第 1 行 |
| M-2 | `unionguard.go:59-85` | 匿名函数 `func(v int\|string)` 的联合参数不收集运行时守卫，擦除为 any 后无入口校验 |
| M-3 | `analysis.go:69-83` | `Type.Accepts` 中 `value == ""`（推断失败）一律 `return true`，本应报"无法确认类型"却静默放过 |
| M-4 | `signal.go:108-129,131-147` | `signal x` 缺 `=` 时未校验，`eq` 指向后续语句，吞掉后续代码生成 `Signal.New(main(){...})` |
| M-5 | `pub.go:148-166` | `restoreRefs` 全局替换无作用域概念，局部变量与 pub 名同名被误大写：`pub var foo; func f(){ foo:=2 }` → `Foo:=2` |
| M-6 | `destructure.go:275-292` | 复杂右值被拼进每个字段声明重复求值：`var {a,b}=mk()` → `mk()` 调用两次，副作用重复 |
| M-7 | `enum.go:156-168` | 位标志 `enum F { read = 1<<iota; write }` 的 `String()` 用 `Contains("<<")` 跳过所有标志 case，`FRead.String()` 返回 "1" 而非 "read" |
| M-8 | `sourcemap.go:94-99,113-118` | 列偏移用 `res.Column += (col - Gen.Column)` 线性叠加，隐含生成列与源码列 1:1 假设，展开/缩短代码时列坐标错位 |
| M-9 | `vdom/runtime.go:342-368` | keyed diff 混用旧索引（删除）、新索引（命中）、父路径（新增）三套 Path 坐标系，重排+删增同时发生时补丁作用错位 |
| M-10 | `signal.go:99-112` | `withObserver` 缺 `defer` 恢复，用户回调 panic 时（即使外层 recover）全局 observer 永久污染 |

## 低（健壮性 / 一致性 / 精度）

| ID | 位置 | 问题 |
|---|---|---|
| L-1 | `lambda.go:375-379` | 无上下文时 `len(x)` 一律把 x 推断为 `string`，实际可能是 slice/map/array/chan |
| L-2 | `template.go:62-65` | 模板插值内所有 token 的 Pos 覆盖为模板起点，插值表达式内的诊断只能定位到模板开头 |
| L-3 | `analysis.go:748-761` | `a, b := ...` 只收集紧邻 `:=` 的最后一个变量符号 |
| L-4 | `lexer.go:139-152,155-178` | 未闭合块注释/字符串静默吞到 EOF，无原生诊断 |
| L-5 | `analysis.go:513-554` | 分组 `type (...)` 只按换行分段，同行分号分隔的别名解析失败 |
| L-6 | `signal.go:280-317` | `inDeclZone` 漏判 `func f(a, b int)` 中逗号前的首个参数，同名 signal 参数被误改写 |
| L-7 | `scope.go:124-175` | `Scope.private` 文件级全局，不区分作用域，某函数私有名保护全文件同名外部 selector |
| L-8 | `enum.go:252-264`、`jsx.go` | enum/JSX 生成代码 sourcemap 坐标为粗粒度单点，诊断回投到元素起始行 |
| L-9 | `vdom/runtime.go:277-287,344-366,130-173` | 类型化 nil `*VNode` 解引用崩溃；重复 key 未处理；组件调用失败路径不缓存、多返回值只取第一个 |
| L-10 | `lsp/server.go:79,142-147,167,411-414`、`cabi/main.go:44-75`、`check.go:138-140`、`buildcheck.go:231-232` | 多处忽略写入/JSON/Atoi 错误；`check.go` 的 `an==nil` 分支与注释不符 |
| L-11 | 14 个文件 | 未 gofmt（纯字段对齐/注释空格，不影响语义）：compiler 的 token/pipeline/analysis/importmanager/defaults/lambda/objectlit/destructure、lsp 4 文件、vdom/runtime |

## 工具链提示（非代码缺陷）

- 系统默认 `go`/`gofmt` 是 1.21.4，仓库声明 `go 1.27` 并使用方法级泛型（`List[E].Map[R any]`）。必须用 `GOTOOLCHAIN=go1.27.3` 编译；用旧 gofmt 检查会把 `list.go` 误报为 `method must have no type parameters`。

## 确认无问题的模块

`token.go`（UTF-8/rune 正确）、`union.go`、`typeguard.go`、`template.go` 解析、`defaults.go`、`objectlit.go`、`collections.go` 融合逻辑、`importmanager.go` 别名复用、`external.go` 判定本身、signal 闭包捕获与 selector 排除、`runtime/list.go`、`internal/buildcheck.go` 并发设计、`check/rules.go`、`cmd/*`。畸形输入批量测试未发现 panic。

## 修复优先级建议

1. **立即**：B-1（enum）、B-2（signal 栈溢出）、H-6（all 阶段，一行改动）、H-10（LSP 崩溃）
2. **本轮**：H-1/H-2/H-4/H-5（生成非法代码，均无上下文推断或替换区间问题）、H-3（signal 语义）
3. **下轮**：H-8/H-9/H-10（signal 并发重构，工作量最大）、M 组
4. **有空**：L 组 + 统一用 go1.27 gofmt 格式化 14 个文件

---

# 修复记录（2026-11-20）

本报告所列问题已系统性修复。下表为逐项状态；所有修复均附带回归测试，
`GOPROXY=https://goproxy.cn,direct GOTOOLCHAIN=go1.27.3 go test ./...` 全绿，
`go test -race ./runtime/signal/` 全绿，全仓已用 go1.27 gofmt 格式化。

## 已修复（阻断 / 高 / 中）

| ID | 状态 | 修复要点 |
|---|---|---|
| B-1 | ✅ | `computeDuplicateValues` 用 `types.Eval` 按成员序号求值常量，省略值沿用上一表达式，位标志 String 不再重复 case |
| B-2 | ✅ | computed 增加 `computing` 重入检测，循环依赖改为可 recover 的 panic |
| H-1 | ✅ | `tokenizeTypeText` 过滤 AllTokens 的 EOF 并修正 Pos；transformLambdas 改为最内层优先迭代替换。回归 `TestNestedLambda` |
| H-2 | ✅ | `inferContextFuncType` 支持函数实参/赋值场景的类型推断；`parseSigParams` 按深度切分嵌套函数类型参数。回归 `TestLambdaContextFuncArg*` |
| H-3 | ✅ | signal 声明/简单赋值/复合赋值右侧表达式经 `rewriteSignalReads` 递归改写。回归 `TestSignalRHSReads` |
| H-4 | ✅ | `collectStructFields` 收集逗号分隔的全部字段名并跳过嵌套深度。回归 `TestStructSharedFieldNamesProtected` |
| H-5 | ✅ | `ImportManager` 把用户源码标识符登记为已占用名，运行时占位别名冲突时自动重命名（list2）。回归 `TestImportAliasNotShadowedByLocal` |
| H-6 | ✅ | `CompileStage(src,"all")` 消除 `toks` shadow，case 内直接 emit 返回 |
| H-7 | ✅ | `collectGuards` 对链首非守卫的 else-if 守卫作为独立链起点处理。回归 `TestElseIfGuardWithNonGuardHead` |
| H-8 | ✅ | signal 观察者栈按 goroutine id 隔离（observers map），并发 withObserver 不再互相覆盖。回归 `TestConcurrentEffectsIsolatedDeps` |
| H-9 | ✅ | computed 结果在锁内写回，computing 带 goroutine 归属 + Cond 等待，并发读只重算一次。回归 `TestComputedConcurrentReadComputesOnce` |
| H-10 | ✅ | LSP `Content-Length` 校验 Atoi 错误与负值，不再 `make([]byte,-1)` panic |
| M-1 | ✅ | 同 H-1：tokenizeTypeText 修正生成 token 的 Pos 为 lambda 位置 |
| M-2 | ✅ | `collectUnionGuards` 正确定位匿名函数参数括号并插入守卫。回归 `TestAnonFuncUnionParamGuard` |
| M-3 | ✅ | `Type.Accepts` 对空值返回 false（不再静默放过），any 仍接受一切 |
| M-4 | ✅ | `signal x` 缺 `=` 时降级为 `var x any`，不吞后续语句。回归 `TestSignalNoInitNotSwallowNext` |
| M-5 | ✅ | `restoreRefs` 用局部遮蔽区间跳过与 pub 同名的局部变量。回归 `TestRestoreRefsSkipsLocalShadow` |
| M-6 | ✅ | 复杂解构右值只求值一次（临时变量），简单标识符保持原样。回归 `TestDestructureUnknownTypeLenient` |
| M-7 | ✅ | 位标志 String 保留标志成员 case（golden 已同步） |
| M-10 | ✅ | signal withObserver 用 defer 恢复观察者栈，回调 panic 不污染 |
| L-6 | ✅ | `inDeclZone` 放宽到名字后跟逗号/右括号，`func f(a, b int)` 首参不再误改写。回归 `TestSignalParamFirstInGroup` |
| L-11 | ✅ | 全仓用 go1.27 gofmt 格式化（含报告列出的 14 个文件） |

## 部分修复 / 保持现状（低优先级健壮性/精度）

| ID | 状态 | 说明 |
|---|---|---|
| L-5 | ✅ | 分组 type 支持分号分隔别名 |
| L-3 | ✅ | `a, b := ...` 收集逗号前每个变量符号 |
| L-10 | ◐ | writeMessage 的 Fprintf 错误已检查；其余 internal 结构体的 json.Marshal 忽略错误在已知类型下不会失败，保留 |
| M-8 | ○ | sourcemap 列偏移线性叠加假设——仅影响诊断列精度，不影响生成代码正确性 |
| M-9 | ○ | vdom keyed diff 三套 Path 坐标系——运行时边界场景，改动风险高于收益 |
| L-1 | ○ | `len(x)` 无上下文时仍推断 string（改 any 会生成 `len(any)` 非法 Go，当前更保守） |
| L-2 | ○ | 模板插值 token Pos 精度——仅影响诊断定位，不影响正确性 |
| L-4 | ○ | 未闭合注释/字符串诊断——需改 Lexer API 暴露错误，收益低 |
| L-7 | ✅ | selector 保护表只含 struct 字段/方法名，局部参数名不再误保护。回归 `TestSelectorNotProtectedByLocalParam` |
| L-8 | ○ | enum/JSX sourcemap 粗粒度——仅影响诊断定位 |
| L-9 | ○ | vdom keyed diff 坐标/nil 健壮性——运行时边界场景，改动风险高于收益 |

图例：✅ 已修复并测试　◐ 部分修复　○ 保持现状（低风险收益权衡）

---

# 第二轮外部审查修复记录（2026-11-20）

针对第二轮源码审查提出的 Signal 运行时问题，逐项用真实编译 / 并发探针实证后修复。
三项 P1 均确认为真 bug（非静态推断误报）。所有修复附回归测试，
`go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...` 全绿。

| ID | 结论 | 修复要点 | 回归测试 |
|---|---|---|---|
| P1-1 | ✅ 真 bug | signal 名被函数参数 / 局部变量遮蔽时被误改写为 `count()`（真实 `go build` 报 `cannot call count (int)`）。新增 `collectSignalShadows` 做轻量作用域区间分析，遮蔽区内的同名标识符保持原样 | `TestSignalNameShadowedByParam` / `TestSignalNameShadowedByLocal` |
| P1-2 | ✅ 真 bug | computed 的 f() 锁外计算期间源被写并标脏，旧结果写回时冲掉 dirty。改为版本号机制（node.version），写回前比对版本，变化则丢弃旧结果重试至稳定 | `TestComputedNoStaleOnConcurrentWrite` |
| P1-3 | ✅ 真 bug | 观察者栈用共享 `node.prev`，两个 goroutine 同时执行同一节点互相覆盖；effect 回调并发执行引发用户数据竞争。改为按 goroutine 的独立栈切片；effect 经 `scheduleEffect` 串行调度并合并重复请求 | `TestEffectSerialUnderConcurrentWrites` / `TestNestedComputedConcurrentRead` |

## 本轮保持现状的审查建议（非 bug，属设计权衡 / 后续演进）

| 建议 | 状态 | 说明 |
|---|---|---|
| 写入边界语义（多参数只用首个 / computed 写入忽略 / 相等值不触发 / Dispose） | ○ | 属 API 语义扩展，非正确性 bug；Dispose 生命周期是后续演进项 |
| goid() 用 runtime.Stack 属非官方技巧 | ○ | 当前无官方 goroutine-local；改动需重设 API 契约，风险高于收益，已在代码注释标注 |
| 对象解构字段重命名 / 嵌套解构 | ○ | 报告本身建议暂不增加，先把简单解构的作用域/类型/求值处理完善 |
| 统一符号表 + 作用域 + 表达式 AST 改造 | ○ | 架构级演进，另立方案评审后分阶段落地；本轮先以补丁解除 P1 正确性风险 |
