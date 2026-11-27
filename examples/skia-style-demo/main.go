// 高级样式验证 demo（V2 rustbackend）：position:absolute / box-shadow / overflow 虚拟滚动 / transition。
// 首帧截图验证 absolute/shadow/虚拟滚动；1 秒后定时器触发 transition，验证动画插值。
// GX_AUTOCLOSE_MS=N 支持脚本化截图验收。
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	rb "github.com/robertpanvip/gox-backend-rust"
	gxapp "github.com/robertpanvip/gox-core/runtime/app"
	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
	"github.com/robertpanvip/gox-core/runtime/signal"
	widgets "github.com/robertpanvip/gox-core/widgets"
)

func label(value, color, size string) *gxscene.Instance {
	return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "span"},
		{Code: gxscene.OpAttr, Key: "color", Arg: color},
		{Code: gxscene.OpAttr, Key: "font-size", Arg: size},
		{Code: gxscene.OpChildText, Arg: value},
		{Code: gxscene.OpClose},
	}})
}

// 包级 signal：buildTree 与定时器 goroutine 共享（原 main 局部变量上提）。
var (
	scrollTop = signal.New(0.0)
	barWidth  = signal.New(60.0) // transition 目标宽度
	barColor  = signal.New("#4ade80")
	fruit     = signal.New("苹果 Apple") // Select 当前选中值
)

func main() {
	cfg := gxapp.WindowConfig{
		Title: "Skia Advanced Styles", Width: 900, Height: 1600,
	}
	// GX_NODECOR=1 时验证无边框 + 透明窗口配置（不设 bg 的场景区可透出桌面）。
	if os.Getenv("GX_NODECOR") == "1" {
		cfg.Decorated = gxapp.NoDecorations()
		cfg.Transparent = true
	}
	// 鼠标穿透（GX_PASSTHROUGH=1 区域级 / GX_PASSTHROUGH=whole 整窗级）：
	// V2 MousePassthrough 已接线。区域级恢复旧 glwindow 语义——透明区点击落到
	// 后面窗口、内容区可点，输入形状随帧自动更新（内容矩形变更才调 XShape）；
	// 整窗级为全窗穿透。区域级目前仅 X11 支持（Win/macOS 需回退整窗级）。
	passthrough := strings.ToLower(os.Getenv("GX_PASSTHROUGH"))
	handle := gxapp.Use(rb.NewBackend()) // V2 接入层：gx.use(Backend) 接口值注入
	app, err := handle.Render(func() any {
		return buildTree()
	}, cfg)
	if err != nil {
		panic(err)
	}
	defer handle.Close() // Handle.Close 级联：窗口 Close + 后端 Shutdown

	win := handle.WindowOf(app).(*rb.Window)
	switch passthrough {
	case "1", "region", "true":
		if err := win.SetMousePassthroughMode(rb.PassthroughRegion); err != nil {
			panic(err)
		}
	case "whole":
		if err := win.SetMousePassthroughMode(rb.PassthroughWhole); err != nil {
			panic(err)
		}
	}
	win.BindApp(app) // GRX3 增量流
	win.BindApp(app) // GRX3 增量流
	win.SetDispatcher(func(ev rb.InputEvent) {
		switch ev.Kind {
		case "ime_commit":
			for _, ch := range ev.Text {
				widgets.InputChar(focusedInput(app), ch)
			}
		case "focus":
			widgets.Focus(findNode(app, ev.NodeID))
		case "backspace":
			widgets.InputBackspace(focusedInput(app))
		}
	})

	// 定时器：滚动列表 + 触发 transition（动画由帧循环持续推进）。
	go func() {
		time.Sleep(1200 * time.Millisecond)
		barWidth(520)
		barColor("#38bdf8")
		for i := 1; i <= 12; i++ {
			time.Sleep(120 * time.Millisecond)
			scrollTop(float64(i * 40))
		}
	}()

	// 脚本化验收：GX_AUTOCLOSE_MS=N 毫秒后自动关窗退出（截图/CI 冒烟）。
	if v := os.Getenv("GX_AUTOCLOSE_MS"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			go func() {
				time.Sleep(time.Duration(n) * time.Millisecond)
				win.RequestClose()
			}()
		}
	}

	handle.Run() // Host 唯一主循环（P4 目标态）
}

// buildTree 构造全部样式验证场景（原 main 内 Render 闭包抽出）。
func buildTree() any {
	return widgets.Col(gxscene.Props{
		"width": "900", "height": "1600", "padding": "20", "gap": "14", "bg": "#0b1020",
		"children": func() any {
			return []any{
				label("高级样式验证 · Skia", "#7dd3fc", "20"),

				// 1) absolute：相对卡片（position:relative）右上角的徽标。
				gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
					{Code: gxscene.OpElem, Arg: "div"},
					{Code: gxscene.OpAttr, Key: "width", Arg: "580"}, {Code: gxscene.OpAttr, Key: "height", Arg: "56"},
					{Code: gxscene.OpAttr, Key: "bg", Arg: "#161d2e"}, {Code: gxscene.OpAttr, Key: "border-radius", Arg: "8"},
					// 徽标：absolute 相对本卡片定位（卡片是最近 positioned 祖先）
					{Code: gxscene.OpElem, Arg: "div"},
					{Code: gxscene.OpAttr, Key: "position", Arg: "absolute"},
					{Code: gxscene.OpAttr, Key: "top", Arg: "8"}, {Code: gxscene.OpAttr, Key: "right", Arg: "12"},
					{Code: gxscene.OpAttr, Key: "width", Arg: "56"}, {Code: gxscene.OpAttr, Key: "height", Arg: "24"},
					{Code: gxscene.OpAttr, Key: "bg", Arg: "#f43f5e"}, {Code: gxscene.OpAttr, Key: "border-radius", Arg: "12"},
					{Code: gxscene.OpChildText, Arg: "NEW"}, {Code: gxscene.OpClose},
					{Code: gxscene.OpClose},
				}}),

				// 2) box-shadow：带阴影的卡片。
				box(widgets.Col(gxscene.Props{
					"width": 560, "height": 40, "bg": "#1e293b", "border-radius": 10,
					"box-shadow": "0 6px 16px 2px rgba(0,0,0,0.55)", "padding": 12,
					"children": func() any { return []any{label("box-shadow · 柔和阴影卡片", "#f8fafc", "15")} },
				}), nil),

				// 3) transition：宽度与颜色在 600ms 内缓动。
				widgets.Col(gxscene.Props{
					"width": 560, "gap": 6,
					"children": func() any {
						return []any{
							label("transition · 宽度/颜色 600ms ease-in-out", "#e2e8f0", "14"),
							gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
								{Code: gxscene.OpElem, Arg: "div"},
								{Code: gxscene.OpAttr, Key: "height", Arg: "18"},
								{Code: gxscene.OpAttr, Key: "border-radius", Arg: "9"},
								{Code: gxscene.OpAttr, Key: "transition", Arg: "width 600ms ease-in-out, bg 600ms ease-in-out"},
								{Code: gxscene.OpDynAttr, Key: "width", Slot: 0},
								{Code: gxscene.OpDynAttr, Key: "bg", Slot: 1},
								{Code: gxscene.OpClose},
							}, N: 2},
								func() any { return fmt.Sprintf("%g", barWidth()) },
								func() any { return barColor() },
							),
						}
					},
				}),

				// 4) overflow + VirtualList：1000 项只渲染可见窗口。
				label("VirtualList · 1000 项 / 仅渲染可见窗口", "#fbbf24", "14"),
				widgets.VirtualList(gxscene.Props{
					"count": 1000, "item-height": 34, "viewport-height": 170,
					"overscan": 2, "width": 560, "bg": "#111827",
					"scroll-top": func() any { return scrollTop() },
					"render-item": func(i int) *gxscene.Instance {
						bg := "#1e293b"
						if i%2 == 0 {
							bg = "#172033"
						}
						return widgets.Col(gxscene.Props{
							"width": 560, "height": 34, "bg": bg, "padding": 8,
							"children": func() any {
								return []any{label(fmt.Sprintf("  item #%03d", i), "#cbd5e1", "14")}
							},
						})
					},
				}),

				// ===== 企业级组件（Tooltip/Form/TimePicker/Table/FilePicker） =====
				label("企业级组件 · Tooltip / Form / TimePicker / Table / FilePicker", "#c084fc", "18"),

				// 5) Tooltip：state 初始打开，首帧即展示气泡（hover 触发在其它场景）。
				widgets.Tooltip(gxscene.Props{
					"state":     widgets.NewPopoverState(true),
					"placement": "bottom",
					"trigger":   func() any { return label("  ⓘ 悬停我 ", "#e2e8f0", "14") },
					"content":   func() any { return label("  这是一条提示气泡 Tooltip  ", "#f8fafc", "13") },
					"width":     "200",
				}),

				// 6) Form + FormItem：标题/必填/字段错误/提交按钮。
				widgets.Form(gxscene.Props{
					"title":    "账户设置",
					"width":    "560",
					"onSubmit": widgets.BindSubmit(func() {}),
					"children": func() any {
						return widgets.Col(gxscene.Props{
							"width": "540", "gap": "8",
							"children": func() any {
								return []any{
									widgets.FormItem(gxscene.Props{
										"label": "邮箱", "name": "email", "required": true,
										"hint":  "我们不会公开你的邮箱",
										"error": func() any { return "邮箱格式不正确" },
										"children": func() any {
											return widgets.Input(gxscene.Props{
												"value": func() any { return "demo@gox.dev" }, "width": "260",
											})
										},
									}),
									widgets.FormItem(gxscene.Props{
										"label": "昵称", "name": "nick",
										"children": func() any {
											return widgets.Input(gxscene.Props{
												"value": func() any { return "Gox 用户" }, "width": "260",
											})
										},
									}),
								}
							},
						})
					},
				}),

				// 7) TimePicker：带秒的 24 小时制。
				widgets.Row(gxscene.Props{
					"width": "560", "gap": "10", "align-items": "center",
					"children": func() any {
						return []any{
							label("TimePicker:", "#94a3b8", "14"),
							widgets.TimePicker(gxscene.Props{
								"value":        func() any { return "09:30:15" },
								"with-seconds": true, "width": "110",
							}),
						}
					},
				}),

				// 7b) Select：下拉选择，signal 驱动当前值 + 回写。
				widgets.Row(gxscene.Props{
					"width": "560", "gap": "10", "align-items": "center",
					"children": func() any {
						return []any{
							label("Select:", "#94a3b8", "14"),
							widgets.Select(gxscene.Props{
								"value":       func() any { return fruit() },
								"options":     []string{"苹果 Apple", "香蕉 Banana", "橙子 Orange", "葡萄 Grape"},
								"placeholder": "选择一种水果",
								"width":       "200",
								"onChange":    widgets.BindValue(func(v string) { fruit(v) }),
							}),
						}
					},
				}),

				// 8) Table：3 列 8 行，斑马纹 + 可排序列头。
				widgets.Table(gxscene.Props{
					"columns": []widgets.TableColumn{
						{Key: "name", Title: "姓名", Width: "120", Sortable: true},
						{Key: "role", Title: "角色", Width: "120"},
						{Key: "city", Title: "城市", Width: "120"},
					},
					"rows": func() any {
						names := []string{"张三", "李四", "王五", "赵六", "钱七", "孙八", "周九", "吴十"}
						rows := make([]any, len(names))
						for i, n := range names {
							rows[i] = []string{n, "工程师", []string{"北京", "上海", "深圳", "杭州"}[i%4]}
						}
						return rows
					},
					"scroll-top":      func() float64 { return 0 },
					"row-height":      30,
					"viewport-height": 250,
					"width":           "560",
				}),

				// 9) FilePicker：点击才弹原生对话框，截图不会触发，安全展示。
				widgets.FilePicker(gxscene.Props{
					"label": "选择文件…", "width": "200",
					"onPick": widgets.BindPick(func([]string) {}),
				}),

				// ===== Context / Theme：运行时主题切换 =====
				label("Context / Theme · 运行时切换主题", "#f472b6", "18"),
				widgets.Row(gxscene.Props{
					"width": "560", "gap": "10", "align-items": "center",
					"children": func() any {
						return []any{
							widgets.Button(gxscene.Props{
								"label": "深色", "variant": "secondary", "width": "80",
								"onClick": gxscene.Handler(func(gxscene.EventContext) { widgets.SetTheme(widgets.DarkTheme) }),
							}),
							widgets.Button(gxscene.Props{
								"label": "浅色", "variant": "secondary", "width": "80",
								"onClick": gxscene.Handler(func(gxscene.EventContext) { widgets.SetTheme(widgets.LightTheme) }),
							}),
							// 当前主题名：dynChild 读 Context，切换后自动重绘。
							gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
								{Code: gxscene.OpElem, Arg: "span"},
								{Code: gxscene.OpAttr, Key: "font-size", Arg: "14"},
								{Code: gxscene.OpAttr, Key: "color", Arg: "#cbd5e1"},
								{Code: gxscene.OpChildText, Arg: "当前主题: "},
								{Code: gxscene.OpClose},
							}}),
							// 主题名动态值单独一个 span（dynChild 插槽读 UseTheme）。
							gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
								{Code: gxscene.OpElem, Arg: "span"},
								{Code: gxscene.OpAttr, Key: "font-size", Arg: "14"},
								{Code: gxscene.OpAttr, Key: "weight", Arg: "bold"},
								{Code: gxscene.OpDynAttr, Key: "color", Slot: 0},
								{Code: gxscene.OpDynChild, Slot: 1},
								{Code: gxscene.OpClose},
							}, N: 2},
								func() any { return widgets.UseToken("accent")() },
								func() any { return widgets.UseTheme()().Name },
							),
						}
					},
				}),
				// 主题强调色块：dynAttr(bg) 读 UseToken("accent")。
				gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
					{Code: gxscene.OpElem, Arg: "div"},
					{Code: gxscene.OpAttr, Key: "width", Arg: "120"},
					{Code: gxscene.OpAttr, Key: "height", Arg: "28"},
					{Code: gxscene.OpAttr, Key: "border-radius", Arg: "8"},
					{Code: gxscene.OpDynAttr, Key: "bg", Slot: 0},
					{Code: gxscene.OpClose},
				}, N: 1}, func() any { return widgets.UseToken("accent")() }),
			}
		},
	})
}

// findNode 在当前帧场景树里按 NodeID 找节点（焦点事件定位用）。
func findNode(app *gxapp.App, id string) *gxscene.Node {
	return findNodeIn(app.Instance().Root(), id)
}

func findNodeIn(n *gxscene.Node, id string) *gxscene.Node {
	if n == nil {
		return nil
	}
	if n.ID.String() == id {
		return n
	}
	for _, c := range n.Children {
		if hit := findNodeIn(c, id); hit != nil {
			return hit
		}
	}
	return nil
}

// focusedInput 从当前帧取回聚焦的输入框节点。
func focusedInput(app *gxapp.App) *gxscene.Node {
	return widgets.FocusedInputIn(app.Instance().Root())
}

// box 把一个子树包进带自定义样式（如 absolute 定位）的 div。
func box(child *gxscene.Instance, styles map[string]any) *gxscene.Instance {
	ops := []gxscene.Op{{Code: gxscene.OpElem, Arg: "div"}}
	// 稳定顺序输出，保证模板结构一致。
	for _, k := range []string{"position", "top", "right", "bottom", "left", "width", "height", "bg", "border-radius", "color", "font-size"} {
		if v, ok := styles[k]; ok {
			ops = append(ops, gxscene.Op{Code: gxscene.OpAttr, Key: k, Arg: fmt.Sprintf("%v", v)})
		}
	}
	ops = append(ops, gxscene.Op{Code: gxscene.OpDynChild, Slot: 0}, gxscene.Op{Code: gxscene.OpClose})
	return gxscene.Render(gxscene.Template{Ops: ops, N: 1}, func() any { return child })
}
