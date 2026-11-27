package gxweb

import (
	gxapp "github.com/robertpanvip/gox-core/runtime/app"
)

// Backend 实现 gxapp.Backend 接口：接入层按接口形状注入，核心零依赖本包。
//
// 装配形态（对应 GX 的 gx.use(Backend)，与桌面后端同一接入路径）：
//
//	app := gxapp.Use(gxweb.NewBackend())
//	app.Render(func() any { return ui.App() },
//	    gxapp.WindowConfig{Title: "WASM Demo", Width: 1200, Height: 800})
//	app.Run()
//
// Web 端特殊性：窗口挂在 DOM 容器上，selector 由 NewWindow 的 cfg 之外的
// 途径指定——本实现的约定是 WindowConfig.X/Y 之外的扩展字段不适用，selector
// 由 Backend 构造函数携带（NewBackendWithSelector），默认 "#app"。
type webBackend struct {
	selector string
}

// 确保满足接入接口（gxapp.Backend）。
var _ gxapp.Backend = webBackend{}

// Name 返回后端标识（故障归因 Fault.Backend / 调试输出用）。
func (b webBackend) Name() string { return "web" }

// NewWindow 为一扇窗口创建独立 Window 实例（挂到 Backend 携带的 DOM selector）。
func (b webBackend) NewWindow(cfg gxapp.WindowConfig) (gxapp.Window, error) {
	return Open(b.selector)
}

// NewBackend 返回 gxweb 的接入接口值（gx.use(Backend) 的注入点），默认挂 "#app"。
func NewBackend() gxapp.Backend { return webBackend{selector: "#app"} }

// NewBackendWithSelector 返回挂指定 DOM 容器的接入接口值（多窗各自容器）。
func NewBackendWithSelector(selector string) gxapp.Backend {
	return webBackend{selector: selector}
}

// Describe 实现可选描述接口（gxapp.BackendDescriptor）。
func (b webBackend) Describe() gxapp.BackendInfo {
	return gxapp.BackendInfo{
		Name:     "web",
		Version:  "gxbackend-web 0.1.0",
		Protocol: "DOM mirror incremental",
		Renderer: "browser DOM",
	}
}

// Capabilities 实现可选能力声明（gxapp.BackendCapabilities）。
func (b webBackend) Capabilities() gxapp.Capabilities {
	return gxapp.Capabilities{
		MultiWindow: true, // 无平台窗口管理器约束，每窗挂各自 DOM 容器
	}
}
