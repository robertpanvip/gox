// widget-showcase：Apple macOS 风格组件库的可视化验证程序。
//
// 用 Skia raster 后端（无 GLFW / 无窗口）把组件库渲染为 PNG，便于逐项对照
// Apple HIG 检查样式。默认深色；设置 GX_THEME=light 输出浅色。
//
//	go run . -o dark.png
//	GX_THEME=light go run . -o light.png
package main

import (
	"os"
	"runtime"
	"strconv"
	"time"

	rb "github.com/robertpanvip/gox-backend-rust"
	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/showcase"
	widgets "github.com/robertpanvip/gox/widgets"
)

func init() { runtime.LockOSThread() }

const W = 900
const H = 4400

func toAny(kids ...*gxscene.Instance) []any {
	out := make([]any, len(kids))
	for i, k := range kids {
		out[i] = k
	}
	return out
}

func col(w, gap string, kids ...*gxscene.Instance) *gxscene.Instance {
	a := toAny(kids...)
	return widgets.Col(gxscene.Props{"width": w, "gap": gap, "children": func() any { return a }})
}
func row(gap string, kids ...*gxscene.Instance) *gxscene.Instance {
	a := toAny(kids...)
	return widgets.Row(gxscene.Props{"gap": gap, "children": func() any { return a }})
}

// group 是一个"设置分组"卡片：标题 + 内容，模拟 macOS Settings 的分组卡片。
func group(title string, w string, kids ...*gxscene.Instance) *gxscene.Instance {
	a := toAny(kids...)
	return widgets.Col(gxscene.Props{
		"width": w, "gap": "12", "padding": "20",
		"bg": widgets.UseToken("panel")(), "border-radius": widgets.RadiusPanel,
		"border": widgets.UseToken("hairline")(), "border-width": "1",
		"box-shadow": widgets.ShadowCard,
		"children":   func() any { return a },
	})
}

func label(s string, size string, tone string) *gxscene.Instance {
	return widgets.Text(gxscene.Props{
		"value": func() any { return s }, "size": size, "tone": tone,
	})
}

func sectionTitle(s string) *gxscene.Instance {
	return sectionTitleIcon(s, "")
}

// sectionTitleIcon 分区标题：可选前置矢量图标，复用预制图标系统。
func sectionTitleIcon(s, iconName string) *gxscene.Instance {
	txt := widgets.Text(gxscene.Props{
		"value": func() any { return s }, "size": widgets.FontHeadline,
		"weight": "bold", "tone": "text",
	})
	if iconName == "" {
		return txt
	}
	return widgets.Row(gxscene.Props{"gap": "8", "align": "center", "children": func() any {
		return toAny(
			widgets.Icon(gxscene.Props{"name": iconName, "size": 17, "tone": "primary"}),
			txt,
		)
	}})
}

func noop() gxscene.Handler { return gxscene.Handler(func(gxscene.EventContext) {}) }

// iconGallery 展示预制矢量图标系统：一批具名图标按网格排布，验证 SVG 在
// host 层解析为 canvas 矢量指令、跨后端一致。
func iconGallery(w string) *gxscene.Instance {
	names := []string{
		"house", "search", "bell", "gear", "mail", "lock", "calendar", "clock",
		"folder", "doc", "heart", "star", "chart", "cart", "cloud", "sun",
		"play", "camera", "location", "tag", "flag", "bookmark", "wifi", "bolt",
	}
	// 每行 8 个图标，图标 22px + 名称标签，间距 8。
	const perRow = 8
	rows := make([]*gxscene.Instance, 0, 3)
	for start := 0; start < len(names); start += perRow {
		end := start + perRow
		if end > len(names) {
			end = len(names)
		}
		cells := make([]*gxscene.Instance, 0, perRow)
		for _, n := range names[start:end] {
			cells = append(cells, widgets.Col(gxscene.Props{
				"width": "80", "gap": "4", "align": "center",
				"children": func() any {
					return toAny(
						widgets.Icon(gxscene.Props{"name": n, "size": 22, "tone": "primary"}),
						widgets.Text(gxscene.Props{
							"value": func() any { return n }, "size": widgets.FontFootnote, "tone": "muted",
						}),
					)
				},
			}))
		}
		rows = append(rows, widgets.Row(gxscene.Props{"gap": "10", "children": func() any { return toAny(cells...) }}))
	}
	return group("图标系统 Icon Set", w,
		sectionTitleIcon("预制矢量图标 · SVG → canvas", "star"),
		widgets.Col(gxscene.Props{"gap": "14", "children": func() any { return toAny(rows...) }}),
	)
}

func buildTree() *gxscene.Instance {
	return showcase.Build(W, H)
}

func str(n int) string {
	if n == 0 {
		return "0"
	}
	out := ""
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		out = string(rune('0'+n%10)) + out
		n /= 10
	}
	if neg {
		out = "-" + out
	}
	return out
}

func main() {
	if os.Getenv("GX_THEME") == "light" {
		widgets.SetTheme(widgets.LightTheme)
	} else {
		widgets.SetTheme(widgets.DarkTheme)
	}

	// V2 接入层装配：gx.use(Backend) → Use(接口值) + 逐窗 Render；后端按
	// gxapp.Backend 接口形状注入，核心零依赖具体后端。
	handle := gxapp.Use(rb.NewBackend())

	// GX_DPR 用于截图/调试：按 dpr 放大窗口尺寸（V2 后端按窗口尺寸驱动布局，
	// 与旧 glwindow 的「逻辑尺寸不变、物理放大」语义不同——截图对比时注意）。
	winW, winH := W, H
	if v := os.Getenv("GX_DPR"); v != "" {
		if d, err := strconv.Atoi(v); err == nil && d > 1 {
			winW, winH = W*d, H*d
		}
	}
	app, err := handle.Render(func() any { return buildTree() },
		gxapp.WindowConfig{Title: "Widget Showcase", Width: winW, Height: winH})
	if err != nil {
		panic(err)
	}
	defer handle.Close() // Handle.Close 级联：窗口 Close + 后端 Shutdown

	// rustbackend 专属扩展（RequestClose 线程安全关窗）按能力经 WindowOf 取回。
	win := handle.WindowOf(app).(*rb.Window)

	// GX_SHOT_MS 指定多少毫秒后自动关窗（配合外部 scrot 截图）。0 表示不自动关。
	autoClose := 0
	if v := os.Getenv("GX_SHOT_MS"); v != "" {
		for _, c := range v {
			autoClose = autoClose*10 + int(c-'0')
		}
	}
	if autoClose > 0 {
		go func() {
			time.Sleep(time.Duration(autoClose) * time.Millisecond)
			win.RequestClose()
		}()
	}
	handle.Run() // Host 唯一主循环（P4 目标态）：app.Run 退化为 no-op，必须走 Handle.Run
}
