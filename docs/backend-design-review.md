# 架构合理性评估：以 Skia 后端接入为试金石

> 方法：从 main 切出 `feat/skia-backend`，在不改任何核心代码的前提下，
> 接一个基于真实 Skia（pekim/go-skia CGO 绑定）的渲染后端子模块。
> 用「接入需要改多少核心代码、依赖方向、协议面是否够用」来测量现有设计是否合理。
>
> 结论先行：**设计合理，解耦扎实**。接入真 Skia 后端全程核心代码零改动，
> 依赖严格单向，协议面精简且够用。发现的问题集中在「协议靠隐式约定」与
> 「离屏验证手段」两处，均为 additive、可渐进改进，不构成架构缺陷。

---

## 1. 实测数据（不是推断，是本分支跑出来的）

| 指标 | 结果 | 证据 |
|---|---|---|
| 核心代码改动量 | **0 行** | `git status` 显示新增仅 `backend/` + `go.work`；`runtime/`、`pkg/`、`cmd/`、`internal/` 一字未动 |
| 后端 → host 依赖 | 仅 `gxapp.Window`/`WindowConfig` + `gxscene.Node`/`NodeType` | `grep robertpanvip/gox backend/skia/*.go` |
| host → 后端依赖 | **无** | `grep gox-backend-skia runtime/ pkg/ cmd/ internal/` 无命中 |
| 后端实现接口所需方法数 | **5 个**（`Open`/`Draw`/`Run`/`Close`/`Config`） | `gxapp.Window` |
| 模块隔离 | 根 `go test ./...` 不含后端；后端独立 `go test ./...` 通过 | `go list ./...` 无 skia |
| 真 Skia 绘制 | 通过（矩形/文字/背景，CGO 端到端） | 7 项测试全绿，含端到端 |
| 核心回归 | 13 包全绿，`go.work` 未破坏任何东西 | `go test ./...` |

**接入成本：新建一个独立 module、实现 5 个方法、拼一份胶水层。核心零改动。**
这是衡量「设计是否合理」最硬的证据。

---

## 2. 设计合理的地方（保持）

### 2.1 单一、精简的协议面
`gxapp.Window` 只有 5 个方法，加上被动数据 `gxscene.Node` / `Change`。没有庞大接口、
没有需要后端实现的 dozens of callbacks。后端实现成本极低——本后端 1 小时内从零到端到端跑通。

### 2.2 依赖方向严格单向
```
backend/skia  ──import──▶  gxapp / gxscene  ──import──▶  signal
     ▲                          │
     └──────── 无任何反向依赖 ────┘
```
后端可整体剥离成独立仓库（本 module 已做到），host 完全无感。这是「方便后期切到其他仓库」目标的核心保障，实测成立。

### 2.3 用 Go 接口而非 C ABI 做渲染边界
`bindings/cabi` 那条 C ABI 是给 IDE 插件做源码转译的，与渲染无关。渲染走 Go 接口，
类型安全、无跨语言 marshalling 成本。职责分离清晰。

### 2.4 additive 扩展设计
`Node.ID`（整数稳定身份，命中缓存）、`Changes()`（增量补丁）、
合成事件（`SyntheticEvent`）都是可选的：不用就完全无感，用了才生效。新增这些没有 forcing 任何已有后端改动——这是成熟协议设计的标志。

### 2.5 host 不做表现层
host 只产出「结构 + 变化」，样式语义（本后端的 `color`/`bg`/`width`…）、布局、命中、
绘制全归后端。换后端时表现层约定可完全不同，互不污染。`TextBackend` 降级为纯工具、
旧 `Backend` 接口删除，这个方向是对的。

---

## 3. 发现的问题（按严重度）

### 3.1 【中】协议靠隐式约定，无 IDL / 无 Go 编译期契约包
- **现象**：后端必须 `import` host 的 `gxapp`/`gxscene` 才能拿到协议类型；协议变更（改 `Window` 方法、改 `Node` 字段语义）无法被独立版本感知，只能靠文档 + 人工 pin 版本。
- **影响**：两仓库独立发版时易错配（host 改了协议、后端没跟进）。
- **建议（additive）**：抽一个极薄的 `gox/contract`（或 `runtime/proto`）包，只放 `Window`/`Node`/`Change`/事件类型，作为明确版本化的协议锚点；后端只 import 它。协议变更当 breaking change、独立发版。不必现在做，但切仓库前值得做。

### 3.2 【中】`Window` 接口没有「输出目标 / 尺寸变化」的表达
- **现象**：`Draw(root)` 拿到树，但后端如何知道「画到哪」（离屏 vs 窗口）、窗口 resize 后新尺寸？只能靠 `Open(cfg)` 那一次配置 + 后端自己维护状态。
- **影响**：当前离屏后端 OK；真实窗口 resize 需要在 `Run()` 里自行处理并改内部 canvas，host 不参与也不感知。
- **建议**：可选加一个 additive 方法或事件（`OnResize(w,h)`），或约定 `cfg` 可被后端更新后回读 `Config()`。非阻塞。

### 3.3 【低】真 Skia 的离屏 canvas 像素不可读，可观测性靠旁路
- **现象**：`NewCanvasWithDimensions` 在本绑定里是 picture-recording 类，`ReadPixels` 返回 false。本后端用 `DrawLog`（绘制指令记录）作为测试可观测面。
- **影响**：仅影响 offscreen 验证手段，不影响真实 GL surface（可正常 ReadPixels）。
- **建议**：生产后端接 GL surface 后用真实像素做 golden；`DrawLog` 保留为调试观测面即可。

### 3.4 【低】上游 go-skia 静态库链接顺序 bug
- **现象**：`libsvg.a` 依赖 `libskia.a` 的 `SkParse::*`，但上游 LDFLAGS 把 `-lskia` 放前面，链接失败。
- **处理**：后端 `linkflags.go` 用 `--start-group` 修复，不改上游/host。属依赖层问题，非 host 设计问题。
- **建议**：切仓库后 pin go-skia 版本；若上游修复则移除该 workaround。

### 3.5 【提示】事件回路完全靠约定
`Window` 无事件输入方法，命中→`Node.Dispatch`→signal 写入→host 重绘这条回路全靠后端在 `Run()` 内实现 + 文档约定。解耦彻底（host 不掺和），但新人容易漏。建议在协议文档里给一条端到端事件时序图（现有 `docs/backend-integration.md` 已有文字说明，可补图）。

---

## 4. 评分

| 维度 | 评分 | 说明 |
|---|---|---|
| 解耦程度 | A | 核心零改动接入，依赖单向，module 可直接剥离 |
| 协议精简度 | A | 5 方法接口 + 被动数据，实现成本极低 |
| 可扩展性 | A- | additive 扩展设计成熟；缺协议包/IDL 是小扣分项 |
| 可验证性 | B+ | DrawLog 可观；离屏像素不可读需绕道 |
| 文档完备度 | B+ | backend-integration.md 详尽；事件回路可补时序图 |
| **综合** | **A-** | **设计合理、解耦扎实，问题均为 additive 可渐进改进** |

---

## 5. 给作者的一句话

> 用一个真实 CGO 后端做了 1 小时接入实验，核心代码一行没改就跑通了——
> 这本身就是「设计合理」最有力的证明。剩下的不是架构问题，而是把隐式协议
> 显式化（协议包 + 版本化）+ 补可观测手段的工程收尾。
