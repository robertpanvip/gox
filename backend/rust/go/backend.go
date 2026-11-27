package rustbackend

import (
	"errors"
	"sync"

	gxapp "github.com/robertpanvip/gox/runtime/app"
)

// Backend 实现 gxapp.Backend 接口：接入层按接口形状注入，核心零依赖本包。
//
// 装配形态（对应 GX 的 gx.use(Backend)）：
//
//	app := gxapp.Use(rustbackend.NewBackend())
//	app.Render(func() any { return ui.App() },
//	    gxapp.WindowConfig{Title: "Main Window", Width: 1200, Height: 800})
//	app.Render(func() any { return ui.Settings() },
//	    gxapp.WindowConfig{Title: "Settings", Width: 600, Height: 400})
//	app.Run() // 全部窗口退出才返回
//
// 每扇窗口调用一次 NewWindow，得到独立 Window 实例（独立事件泵 / 渲染上下文）。
// Window 实现的 UIExecutor / WindowBinder 扩展接口由 Host 在装配时按能力探测注入，
// 本类型无需转发。
// errBackendWindowNil：NewWindow 不会被拒的情况，防御性兜底。
var errBackendWindowNil = errors.New("rustbackend: 创建窗口失败")

type Backend struct{}

// 确保满足接入接口（gxapp.Backend）。
var _ gxapp.Backend = Backend{}

// Name 返回后端标识（故障归因 Fault.Backend / 调试输出用）。
func (Backend) Name() string { return "rust" }

// ---- 可选能力段（gxapp.BackendInitializer / BackendDescriptor /
// BackendCapabilities，Host 装配时探测注入）----

// initOnce：进程级 Init 幂等保护（InitBackend 首次生效，重复调用零开销）。
var initOnce sync.Once

// InitBackend 实现可选生命周期钩子：预探测 C ABI（静态库缺失 / 符号不符
// 在首个窗口创建前尽早报错）。字体库等进程级资源由渲染端惰性加载。
func (Backend) InitBackend() error {
	var err error
	initOnce.Do(func() {
		if Version() == "" {
			err = errors.New("rustbackend: C ABI 探测失败（静态库缺失或符号不符）")
		}
	})
	return err
}

// ShutdownBackend 实现可选生命周期钩子：本包无进程级全局资源需显式释放
// （每窗资源随 Window.Close 释放），保持空实现。
func (Backend) ShutdownBackend() {}

// Describe 实现可选描述接口：版本串来自 C ABI gx_window_version，
// 协议标识与渲染器为静态声明（与构建 feature 对应）。
func (Backend) Describe() gxapp.BackendInfo {
	return gxapp.BackendInfo{
		Name:     "rust",
		Version:  Version(),
		Protocol: "GRX3",
		Renderer: "wgpu+glyphon (tiny-skia CPU fallback)",
	}
}

// Capabilities 实现可选能力声明。MultiWindow=false：当前每窗独立 winit
// EventLoop（macOS 主线程约束待 P4 目标态改造）；Incremental=true：GRX3
// 全量帧 + delta 动作流；Transparent=true：winit with_transparent；
// VSyncControl=true：gx_window_set_vsync 运行时切换 Fifo/Mailbox/Immediate
// （P4.5 高刷专项；Fifo 默认对齐屏幕刷新率，Mailbox 视驱动支持）；
// MousePassthrough=false：XShape 命中形状未接线。
func (Backend) Capabilities() gxapp.Capabilities {
	return gxapp.Capabilities{
		Incremental:  true,
		Transparent:  true,
		VSyncControl: true,
	}
}

// NewWindow 为一扇窗口创建独立 Window 实例。窗口配置在 Open 阶段（Host 装配）传入。
func (Backend) NewWindow(cfg gxapp.WindowConfig) (gxapp.Window, error) {
	w := NewWindow()
	if w == nil {
		return nil, errBackendWindowNil
	}
	return w, nil
}

// NewBackend 返回 rustbackend 的接入接口值（gx.use(Backend) 的注入点）。
func NewBackend() gxapp.Backend { return Backend{} }
