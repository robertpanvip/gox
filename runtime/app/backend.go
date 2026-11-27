// Package gxapp 的 Backend 契约入口。
//
// 本文件是分散在 app.go / frame.go / dispatcher.go 的后端协议面的权威说明，
// 并引入可选的 Backend 注册表。契约类型（Window / FramePresenter / UIExecutor /
// Frame）本身定义在各自文件里，本文件不复制别名、不改变任何既有类型，
// 因此 host 与所有已实现后端零改动。
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
)

// ---- 契约类型 ----
//
// 后端契约统一使用原始名字 Window / FramePresenter / UIExecutor / Frame，
// 不再另设 BackendXxx 别名——两套命名只会增加认知负担，且不提供任何额外能力。
//
// # Backend 注入：OpenWith 是基础，Register 是可选便利
//
// 核心架构只依赖 OpenWith(cfg, win)——装配层直接构造后端实例注入即可工作，
// 这是拆仓后各后端接入的基础机制。Register / OpenBackend 只是建立在 OpenWith
// 之上的可选便利层：便于按 cfg.Backend 名字集中装配。核心能力不依赖注册表。
// Register 允许同名覆盖（后注册者胜），因此注册顺序会影响 OpenBackend 最终选到的实现；
// 这是有意的替换机制，装配层应保证同名只注册一次或明确覆盖顺序。

// BackendFactory 构造一个后端实例。装配层实现它并注册到注册表。
// 传入的是已规范化的配置；工厂可据此选择具体窗口实现。
type BackendFactory func(cfg WindowConfig) (Window, error)

// ---- Backend 接入接口（gx.use(Backend) 的注入点）----
//
// 接入层的后端可插拔走接口注入：gxapp 只定义接口形状，具体后端实现它并在
// 装配处传入（gxapp.Use(rb.NewBackend())）。核心不 import 任何具体后端，
// 也不做字符串分发——注册表（Register / UseNamed）只是建立在接口注入之上的
// 可选便利层，供集中装配与测试桩替换。

// Backend 是后端接入接口：接入层只依赖这个形状。
type Backend interface {
	// Name 返回后端标识（故障归因 Fault.Backend / 调试输出用）。
	Name() string
	// NewWindow 为一扇窗口创建 Window 实例。Host 多窗口下每窗调用一次，
	// 后端可据此为每窗分配独立资源（事件循环 / 渲染上下文）。
	// 返回的 Window 生命周期：Host 依次 Open → Draw → Run → Close。
	NewWindow(cfg WindowConfig) (Window, error)
}

// BackendFunc 是 Backend 的函数适配器：已有 BackendFactory（注册表工厂 /
// 测试桩）无需改造即可作为接口值注入。
type BackendFunc struct {
	NameStr    string
	NewWindowF BackendFactory
}

// Name 实现 Backend.Name。
func (b BackendFunc) Name() string {
	if b.NameStr == "" {
		return "backend"
	}
	return b.NameStr
}

// NewWindow 实现 Backend.NewWindow。
func (b BackendFunc) NewWindow(cfg WindowConfig) (Window, error) {
	if b.NewWindowF == nil {
		return &HeadlessWindow{}, nil
	}
	return b.NewWindowF(cfg)
}

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

// lookupBackend 按名查注册表工厂（Host 多窗口装配用，不打开窗口）。
func lookupBackend(name string) (BackendFactory, bool) {
	registry.mu.RLock()
	f, ok := registry.fact[name]
	registry.mu.RUnlock()
	return f, ok
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
