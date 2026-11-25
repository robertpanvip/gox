// Package showcase 是 Apple macOS 风格组件库的可视化场景树，桌面（Skia）与
// Web（wasm/DOM）两端共享同一棵树——保证「1:1 对照」不需要维护两份组件排布。
//
// 本包只依赖 gxscene / signal / widgets，不依赖任何后端绘制代码（skia/web），
// 因此可被任意后端 import；Apple 主题 token、预制 SVG 图标、明暗双主题全部
// 由 widgets 提供，两端呈现同一套 HIG 设计。
package showcase

import (
	"strconv"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
	widgets "github.com/robertpanvip/gox/widgets"
)

// DefaultWidth / DefaultHeight 是桌面 showcase 的逻辑点尺寸（Apple pt 体系）。
const (
	DefaultWidth  = 900
	DefaultHeight = 4400
)

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

func str(n int) string { return strconv.Itoa(n) }

// Build 构造整棵 showcase 场景树。W/H 为逻辑点尺寸；Web 端可用 DefaultWidth
// 并让容器自然滚动（height 仅作为外层盒高）。
func Build(W, H int) *gxscene.Instance {
	text := signal.New("你好，Gox")
	agree := signal.New(true)
	notify := signal.New(true)
	radio := signal.New("b")
	seg := signal.New("day")
	tab := signal.New("overview")
	volume := signal.New(60.0)
	sel := signal.New("")

	btnRow := row("12",
		widgets.Button(gxscene.Props{"label": "主操作", "variant": "primary", "icon": "plus", "onClick": noop()}),
		widgets.Button(gxscene.Props{"label": "次要", "variant": "secondary", "icon": "download", "onClick": noop()}),
		widgets.Button(gxscene.Props{"label": "危险", "variant": "danger", "icon": "trash", "onClick": noop()}),
	)

	inputField := widgets.Input(gxscene.Props{
		"value": func() any { return text() }, "placeholder": "搜索…", "width": "360",
		"icon":    "search",
		"onInput": func() any { return widgets.BindInput(func(s string) { text(s) }) },
	})

	checks := col(str(W-48-40), "12",
		widgets.Checkbox(gxscene.Props{
			"checked": func() any { return agree() }, "label": func() any { return "我已阅读并同意条款" },
			"onChange": widgets.BindBool(func(v bool) { agree(v) }),
		}),
		widgets.Checkbox(gxscene.Props{
			"checked": func() any { return false }, "label": func() any { return "未选中的复选框" },
			"onChange": widgets.BindBool(func(bool) {}),
		}),
		widgets.Switch(gxscene.Props{
			"checked": func() any { return notify() }, "label": func() any { return "开启消息通知" },
			"onChange": widgets.BindBool(func(v bool) { notify(v) }),
		}),
	)

	radioGrp := widgets.RadioGroup(gxscene.Props{
		"value": func() any { return radio() },
		"options": []widgets.Option{
			{Label: "标准版", Value: "a"}, {Label: "专业版", Value: "b"},
			{Label: "企业版（暂不可用）", Value: "c", Disabled: true},
		},
		"onChange": widgets.BindValue(func(v string) { radio(v) }),
	})

	segmented := widgets.Segmented(gxscene.Props{
		"value": func() any { return seg() },
		"segments": []widgets.Option{
			{Label: "日", Value: "day"}, {Label: "周", Value: "week"}, {Label: "月", Value: "month"},
		},
		"onChange": widgets.BindValue(func(v string) { seg(v) }),
	})

	slider := widgets.Slider(gxscene.Props{
		"value": func() any { return volume() }, "min": 0.0, "max": 100.0, "step": 5.0,
		"width": "320", "label": func() any { return "音量" },
		"onChange": widgets.BindNumber(func(v float64) { volume(v) }),
	})

	selectBox := widgets.Select(gxscene.Props{
		"value":       func() any { return sel() },
		"options":     []string{"苹果 Apple", "香蕉 Banana", "橙子 Orange"},
		"placeholder": "选择一种水果", "width": "220",
		"state":    func() any { return widgets.NewPopoverState(false) },
		"onChange": widgets.BindValue(func(v string) { sel(v) }),
	})

	progress := widgets.Progress(gxscene.Props{
		"value": func() any { return int(volume()) }, "label": func() any { return "加载进度" },
		"width": "300", "tone": "primary",
	})

	tabs := widgets.Tabs(gxscene.Props{
		"value":    func() any { return tab() },
		"tabs":     []widgets.Option{{Label: "概览", Value: "overview"}, {Label: "明细", Value: "detail"}},
		"onChange": widgets.BindValue(func(v string) { tab(v) }),
		"panels": []func() any{
			func() any { return label("这里是概览内容", widgets.FontBody, "muted") },
			func() any { return label("这里是明细内容", widgets.FontBody, "muted") },
		},
	})

	alert := widgets.Alert(gxscene.Props{
		"title":   func() any { return "系统提示" },
		"message": func() any { return "组件库已就绪，点击左侧控件体验交互。" },
		"tone":    "info", "width": "360",
	})

	statRow := row("12",
		widgets.Stat(gxscene.Props{"label": func() any { return "今日访问" }, "value": func() any { return "12,345" }, "delta": func() any { return "+8.2%" }, "tone": "green", "width": "170"}),
		widgets.Stat(gxscene.Props{"label": func() any { return "转化率" }, "value": func() any { return "3.6%" }, "delta": func() any { return "-0.4%" }, "tone": "orange", "width": "170"}),
	)

	tagRow := row("8",
		widgets.Tag(gxscene.Props{"label": "已发布", "tone": "green"}),
		widgets.Tag(gxscene.Props{"label": "草稿", "tone": "orange", "outline": func() any { return true }}),
		widgets.Badge(gxscene.Props{"label": "12 条", "tone": "purple", "dot": func() any { return true }}),
	)

	avatarRow := row("8",
		widgets.Avatar(gxscene.Props{"name": func() any { return "Alice" }, "tone": "purple", "size": 36}),
		widgets.Avatar(gxscene.Props{"name": func() any { return "Bob" }, "tone": "info", "size": 36}),
		widgets.Avatar(gxscene.Props{"name": func() any { return "陈" }, "tone": "green", "size": 36}),
	)

	card := widgets.Card(gxscene.Props{
		"title": func() any { return "项目概览" }, "width": str(W - 88),
		"children": func() any { return label("卡片内容 · 当前主题面板", widgets.FontBody, "text") },
	})

	table := widgets.Table(gxscene.Props{
		"columns": []widgets.TableColumn{
			{Key: "name", Title: "名称", Width: "220", Sortable: true},
			{Key: "status", Title: "状态", Width: "140"},
			{Key: "owner", Title: "负责人", Width: "140"},
		},
		"rows": func() any {
			return []any{
				[]any{"组件库主题重塑", "进行中", "陈"},
				[]any{"深色控件精修", "已完成", "林"},
				[]any{"浮层样式回归", "待处理", "王"},
			}
		},
		"row-height": "32", "viewport-height": "120", "width": str(W - 88),
	})

	formInput := widgets.Input(gxscene.Props{
		"value": func() any { return text() }, "placeholder": "项目名称", "width": str(W - 120),
	})
	form := widgets.Form(gxscene.Props{
		"title": func() any { return "新建项目" }, "width": str(W - 88),
		"children": func() any {
			return col(str(W-120), "12",
				widgets.FormItem(gxscene.Props{
					"label": "名称", "name": "name", "required": true,
					"children": func() any { return formInput },
				}),
				widgets.FormItem(gxscene.Props{
					"label": "说明", "name": "desc", "hint": "简短描述便于团队识别",
					"children": func() any { return label("表单控件占位", widgets.FontBody, "muted") },
				}),
			)
		},
		"onSubmit": widgets.BindSubmit(func() {}),
	})

	emptyState := widgets.EmptyState(gxscene.Props{
		"icon": "folder", "title": func() any { return "暂无内容" },
		"description": func() any { return "空状态也遵循当前主题的层级与留白" },
		"width":       str(W - 88),
	})
	skeletonRow := row("16",
		widgets.Skeleton(gxscene.Props{"width": "180", "height": "14"}),
		widgets.Skeleton(gxscene.Props{"width": "120", "height": "14"}),
		widgets.Skeleton(gxscene.Props{"width": "80", "height": "28", "radius": widgets.RadiusControl}),
	)
	spinnerRow := row("20",
		label("加载中", widgets.FontBody, "muted"),
		widgets.Spinner(gxscene.Props{"size": 18}),
	)

	timePick := widgets.TimePicker(gxscene.Props{
		"value":    func() any { return "09:30" },
		"onChange": widgets.BindValue(func(string) {}),
	})

	tooltipState := widgets.NewPopoverState(true)
	tooltip := widgets.Tooltip(gxscene.Props{
		"state": tooltipState, "placement": "top",
		"trigger": func() any { return widgets.Button(gxscene.Props{"label": "悬停我", "variant": "secondary"}) },
		"content": func() any { return label("轻量的 macOS 提示气泡", widgets.FontCallout, "text") },
	})

	return widgets.Col(gxscene.Props{
		"width": W, "height": H, "padding": "24", "gap": "20", "bg": widgets.UseToken("groupedBg")(),
		"children": func() any {
			return toAny(
				col(str(W), "4",
					widgets.Heading(gxscene.Props{"value": func() any { return "组件库 · Apple macOS" }, "level": 1}),
					label("预制控件 · 明暗双主题 · HIG 对齐", widgets.FontBody, "muted"),
				),
				group(str(W-48), str(W-48), sectionTitleIcon("按钮 Buttons", "cursor"), btnRow),
				group(str(W-48), str(W-48), sectionTitleIcon("输入框 Text Field", "text-field"), inputField),
				group(str(W-48), str(W-48), sectionTitleIcon("复选框 / 开关 Checkbox / Switch", "toggle"), checks),
				group(str(W-48), str(W-48), sectionTitleIcon("单选 Radio", "radio"), radioGrp),
				group(str(W-48), str(W-48), sectionTitleIcon("分段 / 标签 Segmented / Tabs", "tab"), segmented, tabs),
				group(str(W-48), str(W-48), sectionTitleIcon("滑块 / 进度 Slider / Progress", "slider"), slider, progress),
				group(str(W-48), str(W-48), sectionTitleIcon("下拉 Select", "select"), selectBox),
				group(str(W-48), str(W-48), sectionTitleIcon("统计 / 标签 / 头像", "chart"), statRow, tagRow, avatarRow),
				group(str(W-48), str(W-48), sectionTitleIcon("提示 Alert", "info"), alert),
				group(str(W-48), str(W-48), sectionTitleIcon("卡片 Card", "card"), card),
				group(str(W-48), str(W-48), sectionTitleIcon("表格 Table", "grid"), table),
				group(str(W-48), str(W-48), sectionTitleIcon("表单 Form", "doc"), form),
				group(str(W-48), str(W-48), sectionTitleIcon("空状态 EmptyState", "doc"), emptyState),
				group(str(W-48), str(W-48), sectionTitleIcon("骨架屏 / 加载 Skeleton / Spinner", "reload"), skeletonRow, spinnerRow),
				group(str(W-48), str(W-48), sectionTitleIcon("时间 / 浮层提示", "clock"), timePick, tooltip),
				iconGallery(str(W-48)),
			)
		},
	})
}
