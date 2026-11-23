# GX Node 属性协议规范

> 本文把 `gxscene.Node` 携带的字段按用途分为五类，明确每类的**值类型、单位、缺失行为**，
> 供后端实现者作为速查表。它是 [backend-integration.md](backend-integration.md) §2.2/2.3 的
> 细化，不新增任何必做接口；`Node` 的字段全部导出，后端可自由遍历，未规定的值按「缺失行为」
> 处理即可。
>
> 核心原则（与 golden rule 一致）：**host 只负责产出结构化的节点与属性，不解释单位、不计算
> 布局、不做命中测试**。所有语义由后端填充和消费。

## 0. 通用约定

- `Node.Props map[string]any` 的值类型是动态的，但每类属性有约定的 Go 类型（下表）。后端
  按下表断言；类型不符时按「缺失行为」处理，不要 panic。
- 属性名与 JSX 里**原样一致**（`fontSize`、` onClick` 去掉空格后为 `onClick` 等）；数值类
  属性 host 不转换单位，原样传递。
- `Props` 中不含事件（事件在 `Node.Handlers`）；`Handlers` 不含普通属性。两者职责互斥。

## 1. 通用字段（所有元素节点）

| 属性 | Go 类型 | 语义 | 缺失行为 |
|---|---|---|---|
| `id` | `string` | 元素标识；host 不保证跨帧唯一性语义，仅原样透传 | 无 id |
| `class` | `string` | 空格分隔的 class 列表（对象语法 `{{...}}` 在编译期被诊断拒绝，见缺陷清单 A2） | 空串 / 不存在 |
| `style` | `string` | CSS 风格声明串；host 不解析，整串透传 | 空串 / 不存在 |
| `key` | `any` | keyed 列表的键；host 用它做节点身份与复用（`Node.ID` 形如 `.../[key]`），后端一般忽略 | 用结构位置作身份 |

> `class`/`style` 恒为字符串——这是**刻意收窄**：对象语法会产生静默错值，已在编译器层
> 改为带坐标诊断（`attr-unsupported`），所以后端无需处理 map 形态。

## 2. 布局字段

host **不计算布局**；这些只是 JSS 风格的布局提示，原样放入 `Props`，由后端解释。

| 属性 | Go 类型 | 单位规则 | 缺失行为 |
|---|---|---|---|
| `width` / `height` | `string` 或数值 | 带 `px` 后缀=像素；`%` 后缀=父容器百分比；裸数字=后端默认单位（通常 px）。host 不转换 | 由后端布局算法决定 |
| `minWidth` / `maxWidth` / `minHeight` / `maxHeight` | `string` 或数值 | 同上 | 无约束 |
| `x` / `y` | `string` 或数值 | 相对父容器的偏移；单位规则同上 | 0 |
| `padding` / `margin` | `string` | CSS 简写串（`px`/`%`），host 不解析 | 0 |
| `flex` / `flexDirection` / `alignItems` / `justifyContent` | `string` | flexbox 风格串，后端按需实现 | 后端默认布局 |

**px 与百分比的判定**：host 不做单位换算，把带后缀的完整字符串透传（如 `"120px"`、
`"50%"`）。后端若只支持 px，遇到 `%` 应按「按父容器比例解析」或降级为忽略——不可把 `"50%"`
当裸数字 `50` 处理。裸数值（如 `120`）按后端默认单位解释，建议默认 px。

## 3. 文本字段

仅 `NodeText` 节点使用 `Node.Text` 承载文本；下列字段在**所属元素节点**的 `Props` 中，作为
文本的排版提示。host 不测量字体、不排版，原样透传。

| 属性 | Go 类型 | 语义 | 缺失行为 |
|---|---|---|---|
| `fontSize` | `string` 或数值 | 字号；带 `px` / `pt` / `em` 后缀按单位解释，裸数值后端默认 px | 后端默认字号 |
| `fontFamily` | `string` | 字体族名 | 后端默认字体 |
| `fontWeight` | `string` 或数值 | `normal`/`bold`/`100`~`900` | `normal` |
| `color` | `string` | 颜色串（`#rgb`/`#rrggbb`/命名色），host 不解析 | 后端默认前景色 |
| `lineHeight` | `string` 或数值 | 行高；裸数值通常表示倍数（如 `1.5`），带单位表示绝对高度 | 后端默认行高 |
| `textAlign` | `string` | `left`/`center`/`right`/`justify` | `left` |

**对齐**：`textAlign` 恒为小写枚举串；后端遇到未知值按 `left` 处理。字体/行高/对齐三者
相互独立，任一缺失都不影响其余字段的解释。

## 4. 事件字段

事件**不进** `Props`，全部在 `Node.Handlers map[string]any`，key 为 JSX 原样事件名
（如 `onClick`）。详见 [backend-integration.md](backend-integration.md) §2.3。

| 项 | 约定 |
|---|---|
| key | 原样事件名，`on*` 无条件透传（无白名单） |
| value | handler 原值；推荐签名 `func(gxscene.EventContext)`，但后端可自定签名 |
| 推荐读取 | `node.Handler(name) any`；若为推荐签名可 `node.Dispatch(name, ctx) bool` |
| 合成事件 | 可选 `EventContext.Synthetic *SyntheticEvent`，字段见 event.go；未填则退回裸透传 |
| 冒泡/捕获 | 可选 `node.DispatchBubble` / `DispatchCapture`，由后端在命中测试后决定调用 |

## 5. 后端扩展字段

这些是**后端私有挂载点**，host 只提供位置、不解释内容：

| 字段 | 类型 | 用途 |
|---|---|---|
| `Node.ID` | `string` | host 生成、跨帧稳定的逻辑 id，供命中缓存/焦点跟踪；后端可忽略 |
| `Node.Layout` | `any` | 后端存放自己布局结果（rect/尺寸）的位置，避免旁路 `map[*Node]any`；host 恒不填充 |
| `EventContext.Raw` | `any` | 平台原生事件对象，后端填入、业务原样可取 |
| `SyntheticEvent.Data` | `map[string]any` | 后端自定义扩展位，host 不解释 |

## 6. 增量变更中的属性

启用 `EnableChanges()` 后，属性变化通过 `gxscene.Change` 传出（后端可选消费做增量重绘）：

| ChangeKind | Key | Value | 触发场景 |
|---|---|---|---|
| `ChangeSetAttr` | 属性名（`style` 也走这里） | 新属性值 | 动态属性 getter 变化 |
| `ChangeSetText` | 空 | 新文本 | 文本节点值变化 |
| `ChangeSetHandler` | 事件名 | handler 原值 | 事件 handler 换绑 |

结构变化（增删/移动/替换子节点）另见 `ChangeReplaceChildren` 等；后端可忽略全部 Change
继续走全量 `Draw`，协议是 additive 的。

## 7. 一致性检查清单（后端接入自测）

1. 遍历到 `NodeFragment` 时**不绘制**，只透传 `Children`。
2. `class`/`style` 按字符串处理，不假设 map。
3. 布局属性遇到 `%` 按百分比解析，不当裸数字。
4. 文本 `fontSize`/`lineHeight` 裸数值按后端默认单位/倍数解释，未知 `textAlign` 降级 `left`。
5. 事件只读 `Handlers`，不读 `Props`；`on*` 无白名单。
6. `Node.Layout` / `Node.ID` 可用可不用，不用时无副作用。
