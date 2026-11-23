// Skia 后端高级样式验证 demo：position:absolute / box-shadow / overflow 虚拟滚动 / transition。
// 首帧截图验证 absolute/shadow/虚拟滚动；1 秒后定时器触发 transition，验证动画插值。
package main

import (
	"fmt"
	"time"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
	widgets "github.com/robertpanvip/gox/widgets"
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

func main() {
	scrollTop := signal.New(0.0)
	barWidth := signal.New(60.0)   // transition 目标宽度
	barColor := signal.New("#4ade80")

	app, err := gxapp.OpenWith(gxapp.WindowConfig{
		Title: "Skia Advanced Styles", Width: 900, Height: 1500,
	}, openGL())
	if err != nil {
		panic(err)
	}
	defer app.Close()

	app.Render(func() any {
		return widgets.Col(gxscene.Props{
			"width": "900", "height": "1500", "padding": "20", "gap": "14", "bg": "#0b1020",
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
											"hint": "我们不会公开你的邮箱",
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
				}
			},
		})
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

	app.Run()
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
