package skia

import (
	gxapp "github.com/robertpanvip/gox-core/runtime/app"
)

// Run 是接入胶水层：业务侧只调这一个函数，不必关心 openWith 的协议细节。
// 依赖方向保持单向（本包 → host），host 代码零改动。
//
//	// GX 业务代码（外部符号小写，编译器还原大写）：
//	//   import skia "github.com/robertpanvip/gox-backend-skia"
//	//   skia.run({ title: "Demo", width: 800 }, () => <div>...</div>)
//
// 返回的 *Window 可用于绘制后取 DrawLog / Frames 做验证。
func Run(cfg gxapp.WindowConfig, build func() any) (*Window, error) {
	w := NewWindow()
	app, err := gxapp.OpenWith(cfg, w)
	if err != nil {
		return nil, err
	}
	defer app.Close()
	if err := app.Render(build); err != nil {
		return nil, err
	}
	if err := app.Run(); err != nil {
		return nil, err
	}
	return w, nil
}

// OpenAndRender 是更细粒度的胶水：只 Open + Render 一帧，不进入事件循环，
// 适合无窗口的批处理/测试/截图场景。调用方负责 Close。
func OpenAndRender(cfg gxapp.WindowConfig, build func() any) (*gxapp.App, *Window, error) {
	w := NewWindow()
	app, err := gxapp.OpenWith(cfg, w)
	if err != nil {
		return nil, nil, err
	}
	if err := app.Render(build); err != nil {
		app.Close()
		return nil, nil, err
	}
	return app, w, nil
}
