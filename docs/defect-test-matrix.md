# GX 缺陷清单 · 交互测试矩阵

> 本文把《GX 现有缺陷清单（只收跑不通 / 静默错值 / 必须绕路的问题）》的 A/B/C 全部条目，
> 映射到已落地的回归测试与诊断码，作为"语法特性交互"（Signal × JSX × For × Lambda ×
> 对象字面量）的可查矩阵。CI 跑 `go test ./pkg/compiler ./runtime/...` 即覆盖全部条目。
>
> 总闸门：`pkg/compiler/defect_matrix_test.go` 用表驱动对诊断类条目统一校验
> "编译不硬失败 + 诊断码出现 + 1 基坐标 + Message 非空"，对产物类条目校验 GoSource 片段。

## A 组：静默错值

| 条目 | 问题 | 处理 | 回归测试 | 诊断码 / 产物 |
|---|---|---|---|---|
| A1 | `<For>` 渲染体内函数局部 sig 显示函数地址 | render body 转换期锁定 `jsxPos`，sig 裸读补 `()` | `TestForBodyRewritesFunctionLocalSignals` / `TestForBodySignalRewriteNoRegression` | 产物：`n()` |
| A2 | `class` 对象语法 → `{true true}` | 诊断化（class 仍不支持对象字面量） | `TestUnsupportedAttrDiagnostics` / `TestForEmptyAttrDiagnostic` | `attr-unsupported` |
| A3 | `style` 对象语法 → `{red}` | 已支持：`style={{...}}` 编译成强类型 `gxscene.Style{...}`（驼峰 key、伪状态单词 key） | `TestStyleObjCamelKeysAndValues` / `TestStyleObjPseudoNested` | 产物：`gxscene.Style{...}` |
| A4 | `<For empty={...}>` 静默丢弃 | 报未知属性 + 条件分支修法 | `TestForEmptyAttrDiagnostic` | `attr-unsupported` |
| A5 | 未知属性名静默进产物 | 高置信拼错诊断（`bind:` 前缀、事件名手误），不设封闭白名单 | `TestUnknownAttrDiagnostic` / `TestUnknownAttrNoFalsePositive` | `attr-unknown` |
| A6 | JSX 内 `for range` 产坏 Go | 诊断化 + 不产文本节点 | `TestJSXControlStatementDiagnostic` | `jsx-control-statement` |
| A7 | JSX `//` 注释变可见文本/假属性 | parser 跳过行首/前置空白 `//`，保留 `http://` | `TestJSXLineCommentsIgnored` 等 3 则 | 产物：注释消失 |

## B 组：编不过 / 产非法 Go

| 条目 | 问题 | 处理 | 回归测试 | 诊断码 / 产物 |
|---|---|---|---|---|
| B1 | `Get[[]Row]` 传 `<For>` 推成 `rune` | `genericCallTypeArg` 提取泛型实参 | `TestForEachGetGenericTypeArg` / `TestGenericCallTypeArgNoFalsePositive` | 产物：`Get[[]Row]` |
| B2 | 嵌套 `<For>` 内层推成 `any` | 从外层 item 类型反查 struct 字段，逃逸也登记类型 | `TestB2NestedForElemType` | 产物：`ForGetterOfSignal[Cell]` |
| B3 | `each={getter}` 不自动调用 | `funcLitResultType` + getter 变量补 `()` | `TestB3GetterVariableEach` | 产物：`return rows()` |
| B4 | sig 持 struct 字段赋值编不过 | 深写诊断（整体替换修法） | `TestSignalDeepWriteDiagnostic` | `signal-deep-write` |
| B6 | 事件箭头赋值产非法 Go | 传入函数原值、不加包装：箭头只展开成 Go func 字面量，具名函数/func 字面量/显式 Handler 原样透传 | `TestEventHandlerSimplifiedForms` | 产物：函数原值（不含 `gxscene.Handler(` 包装） |
| B7 | sig 字段简写 `P{v}` 编不过 | `stripSoleSignalShorthand` | `TestStructSignalFieldShorthand` | 产物：`P{v()}` 形态 |
| B8 | computed 返回对象字面量产非法 Go | lambda body 对象字面量判定 | `TestComputedArrowObjectLiteral` / `TestLambdaBlockBodyStillWorks` | 产物：匿名 struct |
| B9 | 组件多根 / `return if` 非法 Go | 诊断化 | `TestJSXMultiRootAndReturnIfDiagnostics` | `jsx-multi-root` / `jsx-return-if` |
| B10 | 属性模板串不插值 | 诊断化（改反引号） | `TestUnsupportedAttrDiagnostics` | `attr-unsupported` |
| B11 | 零 props 组件要声明参数 | 无 props 生成 `Counter()` | `TestB11ZeroPropComponent` | 产物：`Counter()` |

## C 组：必须绕路

| 条目 | 问题 | 处理 | 回归测试 / 文档 |
|---|---|---|---|
| C1 | 列表项显示组件状态要手写 `()` | A1 修复后不再需要 | `TestForBodyRewritesFunctionLocalSignals` |
| C2 | sig slice 改元素字段不刷新（唯一静默错值） | 带索引链深写诊断 | `TestC2SignalSliceElemDeepWriteDiag` + 2 守护 |
| C3 | 嵌套列表两层类型注解 | B2 修复后内层可推断 | `TestB2NestedForElemType` |
| C4 | 组件多根要包 div | B9 诊断化（明确修法） | `TestJSXMultiRootAndReturnIfDiagnostics` |
| C5 | 事件只能写 func 字面量 | 已解除：具名函数 / 箭头 / func 字面量均可，传函数原值 | `TestEventHandlerSimplifiedForms` |
| C6 | props getter 只读（设计选择） | 文档说明绕法 | `docs/language-spec.md` §11 |
| C7 | sig 作实参读成当前值（设计选择） | 文档说明 | `docs/language-spec.md` §11 |

## T3.1/T3.2 收敛状态（已在基线达成）

`refactor-symbol-scope.md` 设计提案的"统一绑定解析"目标已由 `Bindings.ResolveAt` 落地：

- `pkg/compiler/exprast.go`：表达式 AST 的标识符经 `ResolveAt` 解析，仅 `BkSignal`/`BkComputed` 进入引用判定（遮蔽天然排除）。
- `pkg/compiler/signal.go`：`Signal` transform 用 `ResolveAt` 做遮蔽判断（P1-1 根治）。
- `pkg/compiler/pub.go`：`restoreRefs` 遮蔽判断走 `ResolveAt`（M-5）。
- `pkg/compiler/signalfields.go`：sig 字段引用统一 `ResolveAt`。

即"至少 2 个 Transform 不再各自扫 token 判断变量身份"的验收已满足（≥4 个）。`jsxSceneCtx`
仍持 `bindings`/`sigs`/`sigTypes`/`signalFields` 多源，但 sig 识别规则已统一走 `Bindings`，
剩余字段是 JSX 场景特有的类型/位置信息，非重复来源。

## 运行

```bash
# 诊断 + 产物矩阵总闸门
go test ./pkg/compiler -run TestDefectMatrix
# 全部交互回归
go test ./pkg/compiler ./runtime/signal ./runtime/scene ./runtime/app
```
