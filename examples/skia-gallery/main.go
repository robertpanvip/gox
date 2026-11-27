// GX 组件库 Gallery（V2 rustbackend）：展示全部预制组件。
// 受控状态用 signal 持有，交互（勾选/切换/选择/滑块）通过 widgets.Bind* 回写。
package main

import (
	"os"

	rb "github.com/robertpanvip/gox-backend-rust"
	gxapp "github.com/robertpanvip/gox-core/runtime/app"
	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
	"github.com/robertpanvip/gox-core/runtime/signal"
	widgets "github.com/robertpanvip/gox-core/widgets"
)

// toAnySlice 把子树列表转成 host 约定的 []any（embedValue 只识别该切片类型）。
func toAnySlice(kids []*gxscene.Instance) []any {
	out := make([]any, len(kids))
	for i, k := range kids {
		out[i] = k
	}
	return out
}

// col 构造纵向容器，children getter 固定返回给定子树列表。
func col(width string, gap string, kids ...*gxscene.Instance) *gxscene.Instance {
	anyKids := toAnySlice(kids)
	return widgets.Col(gxscene.Props{
		"width": width, "gap": gap,
		"children": func() any { return anyKids },
	})
}

// row 构造横向容器。
func row(gap string, kids ...*gxscene.Instance) *gxscene.Instance {
	anyKids := toAnySlice(kids)
	return widgets.Row(gxscene.Props{
		"gap": gap,
		"children": func() any { return anyKids },
	})
}

// section 给内容加一个小标题，模拟组件分组。
func section(title string, child *gxscene.Instance) *gxscene.Instance {
	return col("380", "5",
		widgets.Text(gxscene.Props{"value": func() any { return title }, "tone": "cyan", "size": "12", "weight": "bold"}),
		child,
	)
}

func noopHandler() gxscene.Handler {
	return gxscene.Handler(func(gxscene.EventContext) {})
}

func main() {
	// ---- 受控状态 ----
	text := signal.New("你好，Gox")
	agree := signal.New(true)
	notify := signal.New(false)
	radio := signal.New("b")
	selected := signal.New("")
	selectState := widgets.NewPopoverState(false)
	segment := signal.New("day")
	tab := signal.New("overview")
	volume := signal.New(60.0)
	// GX_DEMO_MODAL=1 让模态框初始打开（截图/演示用）；GX_DEMO_PORTAL=1 显示传送门提示。
	modalState := widgets.NewModalState(os.Getenv("GX_DEMO_MODAL") == "1")
	portalShown := signal.New(os.Getenv("GX_DEMO_PORTAL") == "1")

	// ===== 左栏：表单与交互 =====
	leftCol := col("406", "10",
		section("按钮 Buttons", row("8",
			widgets.Button(gxscene.Props{"label": "主操作", "variant": "primary", "onClick": noopHandler()}),
			widgets.Button(gxscene.Props{"label": "次要", "variant": "secondary", "onClick": noopHandler()}),
			widgets.Button(gxscene.Props{"label": "危险", "variant": "danger", "onClick": noopHandler()}),
		)),
		section("输入框 Input", widgets.Input(gxscene.Props{
			"value":       func() any { return text() },
			"onInput":     func() any { return widgets.BindInput(func(s string) { text(s) }) },
			"placeholder": "点此输入…",
			"width":       "380",
		})),
		section("勾选 Checkbox / 开关 Switch", col("380", "10",
			widgets.Checkbox(gxscene.Props{
				"checked":  func() any { return agree() },
				"onChange": widgets.BindBool(func(v bool) { agree(v) }),
				"label":    func() any { return "我已阅读并同意条款" },
			}),
			widgets.Switch(gxscene.Props{
				"checked":  func() any { return notify() },
				"onChange": widgets.BindBool(func(v bool) { notify(v) }),
				"label":    func() any { return "开启消息通知" },
			}),
		)),
		section("单选 RadioGroup", widgets.RadioGroup(gxscene.Props{
			"value": func() any { return radio() },
			"options": []widgets.Option{
				{Label: "标准版", Value: "a"},
				{Label: "专业版", Value: "b"},
				{Label: "企业版（暂不可用）", Value: "c", Disabled: true},
			},
			"onChange": widgets.BindValue(func(v string) { radio(v) }),
		})),
		section("下拉 Select", widgets.Select(gxscene.Props{
			"value":    func() any { return selected() },
			"options":  []string{"苹果 Apple", "香蕉 Banana", "橙子 Orange"},
			"onChange": widgets.BindValue(func(v string) { selected(v) }),
			"state":    func() any { return selectState },
			"placeholder": "选择一种水果",
			"width":    "200",
		})),
		section("滑块 Slider", widgets.Slider(gxscene.Props{
			"value":    func() any { return volume() },
			"onChange": widgets.BindNumber(func(v float64) { volume(v) }),
			"min":      0, "max": 100, "step": 5, "width": "300", "tone": "cyan",
			"label": func() any { return "音量" },
		})),
	)

	// ===== 右栏：展示与反馈 =====
	rightCol := col("406", "10",
		section("分段 Segmented / 标签 Tabs", col("380", "10",
			widgets.Segmented(gxscene.Props{
				"value": func() any { return segment() },
				"segments": []widgets.Option{
					{Label: "日", Value: "day"}, {Label: "周", Value: "week"}, {Label: "月", Value: "month"},
				},
				"onChange": widgets.BindValue(func(v string) { segment(v) }),
			}),
			widgets.Tabs(gxscene.Props{
				"value": func() any { return tab() },
				"tabs": []widgets.Option{
					{Label: "概览", Value: "overview"}, {Label: "明细", Value: "detail"},
				},
				"onChange": widgets.BindValue(func(v string) { tab(v) }),
				"panels": []func() any{
					func() any { return widgets.Text(gxscene.Props{"value": func() any { return "这里是概览内容" }, "tone": "muted", "size": "13"}) },
					func() any { return widgets.Text(gxscene.Props{"value": func() any { return "这里是明细内容" }, "tone": "muted", "size": "13"}) },
				},
			}),
		)),
		section("统计 Stat", row("10",
			widgets.Stat(gxscene.Props{"label": func() any { return "今日访问" }, "value": func() any { return "12,345" }, "delta": func() any { return "+8.2%" }, "tone": "green", "width": "192"}),
			widgets.Stat(gxscene.Props{"label": func() any { return "转化率" }, "value": func() any { return "3.6%" }, "delta": func() any { return "-0.4%" }, "tone": "orange", "width": "192"}),
		)),
		section("标签 Tag / 头像 Avatar", row("8",
			widgets.Tag(gxscene.Props{"label": func() any { return "已发布" }, "tone": "green"}),
			widgets.Tag(gxscene.Props{"label": func() any { return "草稿" }, "tone": "orange", "outline": func() any { return true }}),
			widgets.Avatar(gxscene.Props{"name": func() any { return "Alice" }, "tone": "purple", "size": 36}),
			widgets.Avatar(gxscene.Props{"name": func() any { return "Bob" }, "tone": "info", "size": 36}),
		)),
		section("提示 Alert", widgets.Alert(gxscene.Props{
			"title":   func() any { return "系统提示" },
			"message": func() any { return "组件库已就绪，点击左侧控件体验交互。" },
			"tone":    "info", "width": "380",
		})),
		section("进度 Progress / 徽章 Badge", col("380", "10",
			widgets.Progress(gxscene.Props{"value": func() any { return int(volume()) }, "label": func() any { return "加载进度" }, "width": "300", "tone": "cyan"}),
			row("8",
				widgets.Badge(gxscene.Props{"label": func() any { return "12 条" }, "tone": "purple", "dot": func() any { return true }}),
				widgets.Badge(gxscene.Props{"label": func() any { return "在线" }, "tone": "green", "dot": func() any { return true }}),
			),
		)),
		section("空状态 Empty / 骨架 Skeleton", row("10",
			widgets.EmptyState(gxscene.Props{"title": func() any { return "暂无数据" }, "description": func() any { return "稍后再来看看" }, "width": "240"}),
			col("130", "8",
				widgets.Skeleton(gxscene.Props{"width": "130", "height": "14"}),
				widgets.Skeleton(gxscene.Props{"width": "100", "height": "14"}),
				widgets.Skeleton(gxscene.Props{"width": "120", "height": "14"}),
			),
		)),
		section("浮层 Modal / Portal", col("380", "8",
			widgets.Button(gxscene.Props{
				"label": "打开模态框", "variant": "primary",
				"onClick": func() any {
					return gxscene.Handler(func(gxscene.EventContext) { modalState.Open() })
				},
			}),
			widgets.Text(gxscene.Props{
				"value": func() any { return "Modal 视口居中 · Portal 脱离父级裁剪" },
				"tone":  "muted", "size": "12",
			}),
		)),
	)

	handle := gxapp.Use(rb.NewBackend()) // V2 接入层：gx.use(Backend) 接口值注入
	app, err := handle.Render(func() any {
		return widgets.Col(gxscene.Props{
			"width": "860", "height": "1040", "padding": "18", "gap": "12", "bg": widgets.ThemeBg,
			"children": func() any { return []any{
				col("824", "3",
					widgets.Heading(gxscene.Props{"value": func() any { return "GX 组件库 · Gallery" }, "level": 1, "tone": "cyan"}),
					widgets.Text(gxscene.Props{"value": func() any { return "预制控件 · signal 驱动 · 跨后端一致语义" }, "tone": "muted", "size": "13"}),
				),
				widgets.Divider(gxscene.Props{"width": "824"}),
				row("12", leftCol, rightCol),
				// 模态框：默认关闭，点"打开模态框"按钮弹出。
				widgets.Modal(gxscene.Props{
					"state":   func() any { return modalState },
					"title":   func() any { return "确认删除" },
					"width":   "400",
					"content": func() any {
						return col("352", "8",
							widgets.Text(gxscene.Props{
								"value": func() any { return "确定要删除这个项目吗？此操作不可撤销。" },
								"tone":  "text", "size": "14",
							}),
							widgets.Text(gxscene.Props{
								"value": func() any { return "删除后数据将无法恢复，请谨慎操作。" },
								"tone":  "muted", "size": "12",
							}),
						)
					},
					"actions": func() any {
						return row("8",
							widgets.Button(gxscene.Props{
								"label": "取消",
								"onClick": func() any {
									return gxscene.Handler(func(gxscene.EventContext) { modalState.Close() })
								},
							}),
							widgets.Button(gxscene.Props{
								"label": "删除", "variant": "danger",
								"onClick": func() any {
									return gxscene.Handler(func(gxscene.EventContext) { modalState.Close() })
								},
							}),
						)
					},
				}),
				// 传送门：右侧浮层提示（演示脱离父级裁剪，自定 absolute 定位）。
				widgets.Portal(gxscene.Props{
					"open": func() bool { return portalShown() },
					"children": func() any {
						return widgets.Col(gxscene.Props{
							"position": "absolute", "left": "480", "top": "955",
							"bg": widgets.AppleMaterialReg, "border-radius": widgets.RadiusCard,
							"border": widgets.AppleHairline, "border-width": "1",
							"box-shadow": widgets.ShadowFloat, "padding": "14", "gap": "4",
							"children": func() any { return []any{
								widgets.Text(gxscene.Props{"value": func() any { return "✓ 已保存到 iCloud" }, "tone": "green", "size": "13", "weight": "bold"}),
							} },
						})
					},
				}),
			} },
		})
	}, gxapp.WindowConfig{Title: "GX 组件库 Gallery", Width: 860, Height: 1040})
	if err != nil {
		panic(err)
	}
	defer handle.Close() // Handle.Close 级联：窗口 Close + 后端 Shutdown

	win := handle.WindowOf(app).(*rb.Window)
	win.BindApp(app) // GRX3 增量流：Render 后建基线，signal 更新走 delta

	// 输入接线（Rust 端命中/IME → 场景树）：click 冒泡派发、焦点与受控 Input 命令，
	// 与 desktop-demo / playground-desktop 同一翻译层模式。
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
		case "click":
			if n := findNode(app, ev.NodeID); n != nil {
				n.DispatchBubble("onClick", gxscene.EventContext{
					Raw: "rust-pointer",
					Synthetic: &gxscene.SyntheticEvent{
						Type:          gxscene.EvClick,
						NativeName:    "onClick",
						Target:        n,
						CurrentTarget: n,
						X:             float64(ev.X),
						Y:             float64(ev.Y),
					},
				})
			}
		}
	})

	handle.Run() // Host 唯一主循环（P4 目标态）
}

// findNode 在当前帧场景树里按 NodeID 找节点（click/焦点事件定位用）。
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
