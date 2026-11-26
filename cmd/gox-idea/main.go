// GoX IDE —— WebStorm 风格的图形 IDE（skia 后端 + GLFW）。
//
// WebStorm 风格三区布局：左侧 PROJECT 项目管理器（内置 SVG 图标，支持目录
// 展开折叠与文件点击打开）；右侧上方是 GX 语法高亮编辑器，下方是 Terminal
// 控制台（Run 输出落地）；两区间隔条均可鼠标拖拽调整尺寸。
// 不传 -path 时用内置示例库，传 -path <目录> 时扫描真实磁盘项目。
// 键盘/鼠标输入由 glwindow.go 翻译成 GX 编辑命令，编辑逻辑零条件编译。
package main

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"time"

	"github.com/robertpanvip/gox-idea/ui"
	"github.com/robertpanvip/gox-idea/ui/interp"
	skia "github.com/robertpanvip/gox-backend-skia"
	gxapp "github.com/robertpanvip/gox/runtime/app"
	"github.com/robertpanvip/gox/widgets"
)

func main() {
	var pathFlag string
	flag.StringVar(&pathFlag, "path", "", "项目根目录（默认用内置示例库；传目录则扫描真实项目树）")
	flag.Parse()

	// 若指定了目录，规整为绝对路径并注入文件读取回调；否则走示例库。
	root := ""
	if pathFlag != "" {
		if info, err := os.Stat(pathFlag); err == nil && info.IsDir() {
			if abs, err := filepath.Abs(pathFlag); err == nil {
				root = abs
			}
		}
	}

	// 窗口尺寸与 Playground() 的 1400x860 界面一致，IDE 外壳贴满整个窗口。
	win := openGL()
	title := "GoX IDE"
	if root != "" {
		title = "GoX IDE — " + filepath.Base(root)
	}
	app, err := gxapp.OpenWith(gxapp.WindowConfig{
		Title:  title,
		Width:  1400,
		Height: 860,
	}, win)
	if err != nil {
		panic(err)
	}
	defer app.Close()

	// P2 宿主接线：Loop 作为 UIExecutor 注入，帧请求/后台 Dispatch/微任务
	// 全部进入共享 Loop 队列，由 Run() 在 GLFW 线程 drain 执行。
	app.SetUIExecutor(win)

	// GUI 端到端测试钩子：设置 GOX_IDEA_STATE_FILE 后，每 100ms 把 UI 状态
	// 快照写到指定文件，供脚本在真实鼠标/键盘操作后硬断言（拖动分隔条、
	// 滚轮滚动、选区、主题、Run 等状态进程外不可见）。快照在 UI 线程生成，
	// 避免与编辑器编辑并发；未设置环境变量时完全不启用，正常用户零开销。
	if stateFile := os.Getenv("GOX_IDEA_STATE_FILE"); stateFile != "" {
		go func() {
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for range ticker.C {
				path := stateFile
				if err := win.Post(func() {
					// 合并 UI 状态与分隔条真实命中框（脚本据此拖动，不写死坐标）。
					data := ui.DebugStateJSON()
					var snap map[string]any
					if json.Unmarshal(data, &snap) == nil {
						snap["splitters"] = win.DebugSplitters()
						snap["scroll_events"] = win.scrollEvents.Load()
						snap["last_scroll_yoff"] = win.lastScrollYOff
						snap["last_scroll_before"] = win.lastScrollBefore
						snap["last_scroll_after"] = win.lastScrollAfter
						ww, wh := win.DebugWindowSize()
						snap["window_w"] = ww
						snap["window_h"] = wh
						if merged, err := json.Marshal(snap); err == nil {
							data = merged
						}
					}
					// 原子替换：E2E 脚本随时可能读到文件，避免读到截断的半截 JSON。
					tmp := path + ".tmp"
					if os.WriteFile(tmp, data, 0644) == nil {
						_ = os.Rename(tmp, path)
					}
				}); err != nil {
					return
				}
			}
		}()
	}

	// 分隔条拖拽改变布局尺寸时，动态子树内部的 height/width 变化在增量帧
	// 后端不产生有效刷新，注入完整重绘回调重建场景树（拖拽频率低，开销可接受）。
	win.rerender = func() {
		_ = app.Render(func() any { return ui.Playground() })
	}

	// 注入项目根与文件读取回调（必须在 Render 前，首帧即见正确项目树）。
	ui.SetProjectRoot(root, func(path string) string {
		b, err := os.ReadFile(path)
		if err != nil {
			return "// 无法读取: " + err.Error()
		}
		return string(b)
	})

	// 字符宽度实测注入：编辑器的点击换算（PickColumn）与光标渲染（PrefixWidth）
	// 依赖字符宽度，默认值 7.83 与桌面 Skia 实际渲染的等宽族（Windows 上 Consolas
	// 13px ≈ 7.15）不符，不注入会导致点击定位随列号累计偏移、插入位置错位。
	// 与 Web 端 playground/web/main.go 的 JS canvas 实测注入同源。
	narrow, wide := skia.MeasureEditorAdvances(widgets.EditorCodeFont(), widgets.EditorCodeFontSize)
	ui.SetCharWidth(narrow)
	ui.SetWideWidth(wide)

	// 桌面端 Run 与网页端同源：解释当前编辑器源码生成场景树，替换预览后重渲染。
	ui.SetRunHandler(func() {
		res, err := interp.Interpret(ui.EditorText())
		if err != nil {
			ui.SetRunResult(nil, err.Error())
		} else {
			ui.SetRunResult(res.Instance, "")
		}
		if rerr := app.Render(func() any { return ui.Playground() }); rerr != nil {
			ui.SetRunResult(nil, "重渲染失败: "+rerr.Error())
		}
	})

	// 主题切换：Apple 明暗双主题。切主题后重建整棵树换肤（token 立即求值）。
	ui.SetThemeHandler(func(light bool) {
		if light {
			widgets.SetTheme(widgets.LightTheme)
		} else {
			widgets.SetTheme(widgets.DarkTheme)
		}
		if rerr := app.Render(func() any { return ui.Playground() }); rerr != nil {
			panic(rerr)
		}
	})

	// 初始化内容：示例模式载入第一个示例；真实模式载入项目树首个文件。
	if root == "" {
		ui.LoadSample(0)
	}

	// 同一份 GX 场景树：桌面与网页唯一的差别只在后端装配。
	if err := app.Render(func() any { return ui.Playground() }); err != nil {
		panic(err)
	}
	if err := app.Run(); err != nil {
		panic(err)
	}
}
