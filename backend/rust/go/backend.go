package rustbackend

import (
	"errors"

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
