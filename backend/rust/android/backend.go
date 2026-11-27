//go:build android

// Package androidbackend 是 gox-backend-rust 的 Android 能力接入层。
//
// 与桌面端 rustbackend.Backend 同构：实现 gxapp.Backend + 可选能力接口，装配层
// 一行 gxapp.Use(androidbackend.NewBackend()) 即可注入。区别只在平台事件源——
// Android 的生命周期/size/paint/touch 由 golang.org/x/mobile 的 app.Main 驱动，
// 因此 Window 把这些事件以公开方法暴露，由装配层转发（见 Window.OnSurfaceReady /
// OnSurfaceLost / OnSize / OnPaint / OnTouch）。
//
// 渲染路径与桌面共享同一 Rust 内核：
//   - 主路径：GRX3 序列化 → gx_android_apply → gx_android_render（wgpu 直通
//     ANativeWindow buffer queue）；
//   - 回退路径：skia CPU raster → GLES2 blit（wgpu 初始化失败时）。
//
// 本包不依赖 playground/ui：编辑器专属的点击/滚动语义由 Window 的 TouchSink
// 回调交给上层业务，本包只负责坐标换算、阈值判定与命中框产出。
package androidbackend

import (
	"errors"

	gxapp "github.com/robertpanvip/gox-core/runtime/app"
)

var errWindowNil = errors.New("androidbackend: NewWindow 返回 nil")

// Backend 实现 gxapp.Backend 及可选能力接口。装配形态与桌面一致：
//
//	handle := gxapp.Use(androidbackend.NewBackend())
//	app, _ := handle.Render(func() any { return ui.Playground() }, cfg)
//	win := handle.WindowOf(app).(*androidbackend.Window)
type Backend struct{}

var (
	_ gxapp.Backend             = Backend{}
	_ gxapp.BackendInitializer  = Backend{}
	_ gxapp.BackendDescriptor   = Backend{}
	_ gxapp.BackendCapabilities = Backend{}
)

// NewBackend 返回 gx.use(Backend) 的注入点。
func NewBackend() gxapp.Backend { return Backend{} }

func (Backend) Name() string { return "android" }

// InitBackend 预探测 C ABI（静态库缺失 / 符号不符在首窗前尽早报错）。
func (Backend) InitBackend() error {
	if Version() == "" {
		return errors.New("androidbackend: C ABI 探测失败（Rust 静态库缺失或符号不符）")
	}
	return nil
}

func (Backend) ShutdownBackend() {}

func (Backend) Describe() gxapp.BackendInfo {
	return gxapp.BackendInfo{
		Name:     "android",
		Version:  Version(),
		Protocol: "GRX3",
		Renderer: "wgpu+glyphon (skia-cpu/GLES2 fallback)",
	}
}

func (Backend) Capabilities() gxapp.Capabilities {
	return gxapp.Capabilities{Incremental: true}
}

// NewWindow 为 NativeActivity 创建唯一窗口实例。
func (Backend) NewWindow(cfg gxapp.WindowConfig) (gxapp.Window, error) {
	w := NewWindow()
	if w == nil {
		return nil, errWindowNil
	}
	return w, nil
}
