// gx:nocollections
// 本文件为原生 Go 组件源码，经 gox build 转译为 table.go。
// gx:nocollections 声明跳过集合/裸对象字面量 desugar，保留手写 opcode 的原生 Go 语义。
package widgets

// Table 是声明式表格组件。性能核心：行渲染复用 VirtualList —— 场景树里只存在
// 当前视口可见的行（+ overscan），节点数与数据总量解耦，杜绝"镜像树膨胀"。
// 后端只需支持 overflow 容器 + scroll-top（skia / rust 均已实现），无需任何
// 表格专用原生绘制。
//
// 结构：
//
//	div[gx-role=table]
//	├── div[gx-role=table-head]   ← For 列头，可点击排序
//	└── VirtualList               ← 行虚拟化，render-item = tableRow
//	    └── div[gx-role=table-row] ×可见行
//	        └── div[gx-role=table-cell] ×列数（For）
//
// Props:
//   - columns        []TableColumn                 列定义
//   - rows           func() any → []any / []string 行数据（响应式 getter）
//   - row-height     number/string                 行高（默认 40）
//   - viewport-height number/string                视口高（默认 400）
//   - scroll-top     func() any → float64           滚动偏移（响应式 getter）
//   - render-cell    func(row any, col int) string 单元格显示文本（可选；缺省按行类型推断）
//   - on-sort        Handler                       点击表头回传，Data["value"]=列 key（用 BindValue）
//   - on-select      Handler                       点击行回传，Data["value"]=行索引（用 BindValue）
//   - striped        bool                          斑马纹（默认 true）
//   - width          string                        表格宽度
import (
	"fmt"
	"strconv"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// TableColumn 定义一个表格列。
type TableColumn struct {
	Key      string // 业务键（排序回传此值）
	Title    string // 表头显示文本
	Width    string // 列宽（像素字符串，可空）
	Align    string // left/center/right（默认 left）
	Sortable bool   // 是否可点击排序
}

// Table 构造一个表格实例。
func Table(props gxscene.Props) *gxscene.Instance {
	cols := tableColumns(props)
	rowsGet := gxscene.Get[any](props, "rows")
	scrollGet := gxscene.Get[float64](props, "scroll-top")
	onSort := gxscene.Get[gxscene.Handler](props, "on-sort")
	onSelect := gxscene.Get[gxscene.Handler](props, "on-select")
	striped := boolGet(props, "striped", true)
	width := gxscene.Get[string](props, "width")

	rowHeight := propFloat(props, "row-height", 40)
	viewportHeight := propFloat(props, "viewport-height", 400)
	renderCellAny := gxscene.Get[any](props, "render-cell")

	// 单元格文本：优先 render-cell；否则行是 []string / []any 时按列取。
	cellText := func(row any, col int) string {
		if fn, ok := renderCellAny().(func(any, int) string); ok {
			return fn(row, col)
		}
		switch r := row.(type) {
		case []string:
			if col < len(r) {
				return r[col]
			}
		case []any:
			if col < len(r) {
				return valueTextOf(r[col])
			}
		case string:
			if col == 0 {
				return r
			}
		}
		return ""
	}

	rowCount := func() int {
		return tableRowCount(rowsGet())
	}
	rowAt := func(i int) any {
		return tableRowAt(rowsGet(), i)
	}

	// 行渲染：一个 table-row，内含每列一个单元格（For 控制器稳定挂一次）。
	renderRow := func(index int) *gxscene.Instance {
		row := rowAt(index)
		items := make([]any, len(cols))
		for i := range cols {
			items[i] = i
		}
		cellFor := gxscene.NewFor(
			func() []any { return items },
			func(v any) any { return v },
			func(v any) *gxscene.Instance {
				ci := v.(int)
				return tableCell(cellText(row, ci), cols[ci], ci)
			},
		)
		ops := []gxscene.Op{
			el("div"), attr("gx-role", "table-row"),
			attr("gx-row-index", strconv.Itoa(index)),
			attr("flex-direction", "row"), attr("align-items", "center"),
			attr("height", fmt.Sprintf("%g", rowHeight)),
			attr("border-bottom", ThemeBorder2), attr("border-width", "1"),
		}
		if striped() && index%2 == 1 {
			// 斑马纹：极淡分层（tertiarySystemBackground），不抢主文字层级。
			ops = append(ops, attr("bg", ThemeFill))
		}
		// 行点击 → on-select（列单元格点击会冒泡到此；事件名相同按命中就近派发）。
		ops = append(ops, gxscene.Op{Code: gxscene.OpEvent, Key: "onClick", Slot: 0})
		ops = append(ops, gxscene.Op{Code: gxscene.OpFor, Slot: 1}, closeEl())
		return gxscene.Render(gxscene.Template{Ops: ops, N: 2},
			func() any {
				h := onSelect()
				if h == nil {
					return gxscene.Handler(func(gxscene.EventContext) {})
				}
				idx := index
				return gxscene.Handler(func(ctx gxscene.EventContext) { emitValue(h, ctx, strconv.Itoa(idx)) })
			},
			func() any { return cellFor },
		)
	}

	// 表头：For 列，每列可点击排序。
	headItems := make([]any, len(cols))
	for i := range cols {
		headItems[i] = i
	}
	headFor := gxscene.NewFor(
		func() []any { return headItems },
		func(v any) any { return v },
		func(v any) *gxscene.Instance {
			return tableHeadCell(cols[v.(int)], onSort())
		},
	)

	// 外层容器：表头 + VirtualList。
	const (
		headSlot = iota
		listSlot
		nSlots
	)
	ops := []gxscene.Op{
		el("div"), attr("gx-role", "table"),
		attr("flex-direction", "column"),
		// macOS NSTableView：表格本身只是一张浅底平面，不额外套卡片阴影/描边
		// （外层 group 已提供卡片层次，避免"双层卡片"）。仅用一圈 hairline
		// 收边 + 圆角，贴近原生列表视图的克制观感。
		attr("bg", ThemePanel), attr("border-radius", RadiusSmall),
	}
	if w := width(); w != "" {
		ops = append(ops, attr("width", w))
	}
	ops = append(ops,
		// 表头：极淡底层 + 细分隔线，11px 次级色非粗体（macOS 列标题规范）。
		el("div"), attr("gx-role", "table-head"),
		attr("flex-direction", "row"), attr("align-items", "center"),
		attr("height", fmt.Sprintf("%g", rowHeight)),
		attr("bg", ThemeFill),
		attr("border-bottom", ThemeBorder2), attr("border-width", "1"),
		gxscene.Op{Code: gxscene.OpFor, Slot: headSlot},
		closeEl(),
		gxscene.Op{Code: gxscene.OpComp, Arg: "TableBody", Slot: listSlot},
		closeEl(),
	)

	list := VirtualList(gxscene.Props{
		"count":           func() any { return rowCount() },
		"item-height":     rowHeight,
		"viewport-height": viewportHeight,
		"scroll-top":      func() any { return scrollGet() },
		"render-item":     func(i int) *gxscene.Instance { return renderRow(i) },
		"width":           width(),
	})

	return gxscene.Render(gxscene.Template{Ops: ops, N: nSlots},
		func() any { return headFor },
		func() any { return list },
	)
}

// tableColumns 读取列定义，兼容 []TableColumn / func() []TableColumn / func() any。
func tableColumns(props gxscene.Props) []TableColumn {
	raw, ok := props["columns"]
	if !ok {
		return nil
	}
	v := evalProp(raw)
	switch x := v.(type) {
	case []TableColumn:
		return x
	case []any:
		out := make([]TableColumn, 0, len(x))
		for _, item := range x {
			if c, ok := item.(TableColumn); ok {
				out = append(out, c)
			}
		}
		return out
	}
	return nil
}

func tableRowCount(v any) int {
	switch x := v.(type) {
	case []any:
		return len(x)
	case []string:
		return len(x)
	case []TableColumn:
		return len(x)
	case int:
		return x
	}
	return 0
}

func tableRowAt(v any, i int) any {
	switch x := v.(type) {
	case []any:
		if i >= 0 && i < len(x) {
			return x[i]
		}
	case []string:
		if i >= 0 && i < len(x) {
			return x[i]
		}
	}
	return nil
}

func valueTextOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func tableHeadCell(col TableColumn, onSort gxscene.Handler) *gxscene.Instance {
	w := col.Width
	if w == "" {
		w = "120"
	}
	align := col.Align
	if align == "" {
		align = "left"
	}
	ops := []gxscene.Op{
		el("div"), attr("gx-role", "table-head-cell"),
		attr("width", w), attr("padding", "8"),
		attr("flex-direction", "row"), attr("align-items", "center"), attr("gap", "4"),
		attr("font-size", FontSubheadline), attr("color", ThemeMuted),
		attr("text-align", align),
		gxscene.Op{Code: gxscene.OpChildText, Arg: col.Title},
	}
	if col.Sortable {
		// 排序标记用预制矢量 chevron 图标（替代字符 ⇅），次级色跟随主题。
		// slot 0 = onClick Handler，slot 1 = 图标 canvas 子树。
		ops = append(ops,
			gxscene.Op{Code: gxscene.OpComp, Arg: "SortIcon", Slot: 1},
			gxscene.Op{Code: gxscene.OpEvent, Key: "onClick", Slot: 0},
		)
	}
	ops = append(ops, closeEl())
	n := 0
	getters := []func() any{}
	if col.Sortable {
		n = 2
		getters = append(getters,
			func() any {
				key := col.Key
				return gxscene.Handler(func(ctx gxscene.EventContext) {
					if onSort != nil {
						emitValue(onSort, ctx, key)
					}
				})
			},
			func() any { return iconNode("chevron-down", 12, "faint") },
		)
	}
	return gxscene.Render(gxscene.Template{Ops: ops, N: n}, getters...)
}

func tableCell(text string, col TableColumn, colIndex int) *gxscene.Instance {
	align := col.Align
	if align == "" {
		align = "left"
	}
	w := col.Width
	if w == "" {
		w = "120"
	}
	// 首列主文字高亮加粗，其余列次要色——建立阅读层级。
	textColor, weight := ThemeMuted, "normal"
	if colIndex == 0 {
		textColor, weight = ThemeText, "bold"
	}
	ops := []gxscene.Op{
		el("div"), attr("gx-role", "table-cell"),
		attr("width", w), attr("padding", "8"),
		attr("font-size", FontBody), attr("weight", weight), attr("color", textColor),
		attr("text-align", align),
		gxscene.Op{Code: gxscene.OpChildText, Arg: text}, closeEl(),
	}
	return gxscene.Render(gxscene.Template{Ops: ops, N: 0})
}
