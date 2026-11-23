# Backend 架构：统一契约与可独立拆分

> 设计目标：GoX 定义统一的 Backend 契约，允许多个 Backend 实现并存；Go 与 Rust
> 实现目前在本仓库维护，但**从一开始就按「可独立拆分」的边界设计**，未来可零改动
> 迁移到各自仓库。本文是 [backend-integration.md](backend-integration.md)（host ↔
> 后端接入细节）的上位设计文档。

## 1. 核心原则

1. **核心只依赖契约，不依赖实现**：`runtime/app`（gxapp）定义 Backend Contract 与
   注册表，永不 import 任何具体后端包。依赖方向严格单向：**后端 → 核心**。
2. **契约分两层**：Go-facing 契约约束 Go 类型；跨语言 Wire 契约约束非 Go 运行时。
   两者职责不同，不可混为一谈（见 §2）。
3. **按名字装配，不按类型硬编码**：装配层（`main`）注册「名字 → 工厂」，业务用
   `Config.Backend` 选择。新增后端只加一行注册，核心零改动。
4. **可独立拆分是设计约束，不是未来重构**：每个后端在目录、module、依赖上自洽，
   迁移到独立仓库时只需改 module 路径，不改代码。

## 2. 两层契约模型（Contract + Adapter）

Go 的 interface 只能约束 Go 类型；Rust 后端以独立进程 / 动态库 / WASM 运行时，
**无法直接实现一个 Go interface**。因此契约显式分两层：

| 层 | 契约内容 | 约束对象 | 定义位置 |
|---|---|---|---|
| **Backend Contract**（Go-facing） | `Window` / `FramePresenter` / `UIExecutor` / `Frame` / `WindowConfig` / Node / Change / 事件约定 | 所有 Go 实现与 Go Adapter | `runtime/app/backend.go`（权威入口）+ app.go/frame.go/dispatcher.go |
| **Wire Contract**（跨语言） | GRX1 全量帧 / GRX2 增量动作 / C ABI / InputEvent JSON | 非 Go 运行时（Rust 等） | `backend/rust/PROTOCOL.md` |

- **Go Backend**（如 `backend/skia`）：直接实现 Contract 的 Go interface，同进程调用，无适配层。
- **Rust Backend**（`backend/rust`）：Go Adapter（`go/` 包，cgo 实现 Contract）把每次
  Contract 调用翻译成 Wire 调用（C ABI），Rust Runtime（`rust/`）遵守 Wire Contract。

```
                 ┌──────── Backend Contract（Go-facing，runtime/app）────────┐
                 │  Window · FramePresenter · UIExecutor · Frame · Config    │
                 └───────────────────────────────────────────────────────────┘
                    ▲ 实现                                  ▲ 实现
                    │                                        │
          Go Backend（backend/skia）              Go Adapter（backend/rust/go）
          直接实现 interface                       把 Contract 调用 → Wire 调用
                                                              │ Wire Contract
                                                              ▼
                                                   Rust Runtime（backend/rust/rust）
```

**关键收益**：Rust 后端若从 FFI 改为独立进程 IPC，只需替换 Go Adapter 内部实现，
Contract 与核心零改动。这正是「两层分开」的意义。

## 3. 目录与模块结构

```
gox/
├── compiler/ · pkg/ · internal/ · runtime/     核心：编译器 + 运行时 + Contract + Registry
├── runtime/app/backend.go                      契约权威入口 + 类型别名 + 注册表
├── backend/
│   ├── skia/                    (module gox-backend-skia)     Go Backend
│   └── rust/                                                  Rust Backend
│       ├── go/                  (module gox-backend-rust)     Go Adapter + go.mod
│       │   └── cmd/*/                                         装配层示例（Register 用法）
│       ├── rust/                (Cargo.toml)                  Rust Runtime
│       └── PROTOCOL.md                                        Wire Contract
├── examples/                    含 GLFW/Skia 演示装配层
└── go.work                      本地联调纳入全部子模块
```

依赖方向：后端 module 通过 `replace` 指向核心（`=> ../..` 等），核心**不**声明对后端的
依赖。go.work 仅用于本地联调，不进入publish 后的 module 图。

## 4. 发现与选择机制

```go
// 装配层（main / examples）注册需要的后端——核心不 import 实现。
gxapp.MustRegister("skia", func(cfg gxapp.WindowConfig) (gxapp.Window, error) {
    return skia.NewWindow(), nil
})
gxapp.MustRegister("rust", func(cfg gxapp.WindowConfig) (gxapp.Window, error) {
    return rust.NewWindow(), nil
})

// 业务按名字选择；未注册的名字返回清晰错误（含已注册列表）。
app, err := gxapp.OpenBackend(gxapp.Config{Backend: "rust", ...})
```

`Open` / `OpenWith` 旧签名保持不变：`Open` 仍默认 Headless，`OpenWith` 仍支持直接
注入实例。新增的 `Register` / `OpenBackend` 是增量 API，老代码零改动。

## 5. 未来拆分为独立仓库

| 仓库 | 内容 | 依赖 |
|---|---|---|
| `gox` | 编译器、运行时、Scene、Backend Contract + Registry | 无后端依赖 |
| `gox-backend-go` | Go 原生实现（直接实现 Contract） | `gox`（仅 Contract） |
| `gox-backend-rust` | Go Adapter + Rust Runtime + Wire 协议 | `gox`（仅 Contract） |

迁移步骤（无需改代码）：

1. 把 `backend/skia/` 或 `backend/rust/` 目录整体移出新仓库；
2. 后端 module 的 `replace github.com/robertpanvip/gox => ../..` 改为正式版本号；
3. 装配层的 `Register` 改为 import 新 module 路径；
4. 核心 `gox` 无需任何改动——它从未 import 后端。

## 6. 契约稳定性约束

- Contract 的任何变更都是破坏性变更，需版本化（Wire 协议已有版本探测 `Version()`）。
- 后端向后兼容：老 `Draw(root)` 路径永久保留；`FramePresenter` 是可选增强。
- 新增能力走「可选接口 + 检测」（如 `UIExecutor`），不强行塞进 `Window`，避免强迫
  所有后端实现用不到的能力。

### 6.1 窗口外观配置与 C ABI 兼容

- `WindowConfig` 的 `Decorated / Transparent / AlwaysOnTop / Opacity / IconPath` 是纯配置扩展，
  老后端忽略新字段即可（零值 = 原行为），不破坏 `Window` 契约。
- Rust 后端新增 `gx_window_new_ex(w,h,title,decorated,transparent,always_on_top,opacity)`，
  旧的 `gx_window_new` 保留为兼容包装（内部转发，decorated=-1 用默认）。**改动 Rust 侧后必须
  重新 `cargo build --release` 生成静态库**，否则 Go 侧 cgo 链接会因缺少新符号失败。静态库
  是构建产物、不入 git，由各环境从源码构建（见 `backend/rust/BENCHMARK.md`）。
- `Decorated *bool` 用指针三态（nil 默认 / false 无边框 / true 有边框），Go 侧 `decoratedC`
  翻译成 C 的 -1/0/1，避免 bool 零值无法区分"未设置"与"明确关闭"的经典陷阱。
