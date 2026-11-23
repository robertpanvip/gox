// Package gxapp 的 Backend 契约入口。
//
// 本文件把分散在 app.go / frame.go / dispatcher.go 的后端协议面集中为一份
// 「Backend Contract」权威声明，并引入 Backend 注册表。它不改变任何既有类型，
// 只用类型别名把它们聚合为单一入口，因此 host 与所有已实现后端零改动。
//
// # 两层契约模型（Contract + Adapter）
//
// Go 的 interface 只能约束 Go 类型；Rust 后端若以独立进程 / 动态库 / WASM
// 形式运行，无法直接实现一个 Go interface。因此契约显式分两层：
//
//	┌───────────────────── Backend Contract（本文件，Go-facing）──────────────────┐
//	│  Window            窗口生命周期：Open / Draw / Run / Close / Config        │
//	│  FramePresenter    可选：接收 Frame{Root,Changes,Version,FullSync} 提交    │
//	│  UIExecutor        可选：把绘制回调投递到正确 UI 执行环境                  │
//	│  FrameDispatcher   帧合并与派发策略                                        │
//	│  WindowConfig / Frame / Node / Change / 事件约定                           │
//	└────────────────────────────────────────────────────────────────────────────┘
//	          ▲ 实现                              ▲ 实现
//	          │                                    │
//	   Go Backend（如 backend/skia）        Go Adapter（cgo/WASM IPC stub）
//	   直接实现 Go interface              把 Contract 调用翻译成 Wire 调用
//	                                              │ Wire Contract（跨语言）
//	                                              ▼
//	                                   Rust Runtime（C ABI / 二进制协议 / 事件）
//
//	核心 gxapp 只依赖 Contract 与注册表，不 import 任何具体后端实现；具体后端
//	由装配层（main / examples）通过 Register 注入。这样每个后端都能独立成仓：
//	  - gox                 核心：compiler + runtime + Contract + Registry
//	  - gox-backend-go      Go 原生实现（直接实现 Contract）
//	  - gox-backend-rust    Rust 实现（Go Adapter + Rust Runtime + Wire 协议）
//
// 依赖方向严格单向：后端 → host；host 永不反向依赖后端。
package gxapp

import (
	"fmt"
	"sort"
	"sync"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// ---- 契约别名：把分散定义的协议类型聚合为单一 Backend 入口 ----
//
// 这些是 type alias（非新类型），与原始类型完全等价，后端可任选其一引用。

// BackendWindow 是窗口生命周期契约（别名自 Window）。
type BackendWindow = Window

// BackendPresenter 是场景提交契约（别名自 FramePresenter）。
type BackendPresenter = FramePresenter

// BackendExecutor 是 UI 线程投递契约（别名自 UIExecutor）。
type BackendExecutor = UIExecutor

// BackendDispatcher 是帧派发契约（别名自 FrameDispatcher）。
type BackendDispatcher = FrameDispatcher

// BackendFrame 是一次 Host → Backend 的完整提交（别名自 Frame）。
type BackendFrame = Frame

// BackendConfig 是窗口/后端配置（别名自 WindowConfig）。
type BackendConfig = WindowConfig

// BackendNode / BackendChange 是场景树与增量数据类型（别名自 gxscene）。
type BackendNode = gxscene.Node
type BackendChange = gxscene.Change

// BackendFactory 构造一个后端实例。装配层实现它并注册到注册表。
// 传入的是已规范化的配置；工厂可据此选择具体窗口实现。
type BackendFactory func(cfg WindowConfig) (Window, error)

// ---- Backend 注册表 ----
//
// 核心只持有「名字 → 工厂」的映射，不 import 任何具体后端。装配层（main /
// examples）在启动时 Register 需要的后端，随后按 Config.Backend 选择。
// 這让后端可独立成仓：新增后端只需在装配层多一行 Register，核心零改动。

var registry = struct {
	mu   sync.RWMutex
	fact map[string]BackendFactory
}{fact: make(map[string]BackendFactory)}

// Register 注册一个命名后端工厂。重复注册同名会覆盖（后注册者胜），
// 便于装配层用自定义实现替换默认。name 非空且大小写敏感。
func Register(name string, factory BackendFactory) error {
	if name == "" {
		return fmt.Errorf("gxapp: Backend 名不能为空")
	}
	if factory == nil {
		return fmt.Errorf("gxapp: Backend %q 的工厂不能为 nil", name)
	}
	registry.mu.Lock()
	registry.fact[name] = factory
	registry.mu.Unlock()
	return nil
}

// MustRegister 同 Register，但注册失败时 panic（适合初始化期确定性配置）。
func MustRegister(name string, factory BackendFactory) {
	if err := Register(name, factory); err != nil {
		panic(err)
	}
}

// Registered 报告指定名字是否已注册。
func Registered(name string) bool {
	registry.mu.RLock()
	_, ok := registry.fact[name]
	registry.mu.RUnlock()
	return ok
}

// Backends 返回已注册后端名的排序快照（供诊断/CLI 列举）。
func Backends() []string {
	registry.mu.RLock()
	out := make([]string, 0, len(registry.fact))
	for name := range registry.fact {
		out = append(out, name)
	}
	registry.mu.RUnlock()
	sort.Strings(out)
	return out
}

// OpenBackend 按 cfg.Backend 从注册表选择后端并打开应用。
//
// "headless" 或空串走内置 HeadlessWindow（无需注册）；其余名字必须已 Register。
// Open / OpenWith 的行为不变——它们不查注册表，便于老代码零改动。
func OpenBackend(cfg WindowConfig) (*App, error) {
	cfg = cfg.normalize()
	if cfg.Backend == "headless" {
		return Open(cfg)
	}
	registry.mu.RLock()
	factory, ok := registry.fact[cfg.Backend]
	registry.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("gxapp: 未注册的 Backend %q（已注册: %v）", cfg.Backend, Backends())
	}
	win, err := factory(cfg)
	if err != nil {
		return nil, fmt.Errorf("gxapp: 创建 Backend %q 失败: %w", cfg.Backend, err)
	}
	if win == nil {
		return nil, fmt.Errorf("gxapp: Backend %q 的工厂返回 nil", cfg.Backend)
	}
	return OpenWith(cfg, win)
}
