package widgets

import (
	"fmt"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

func tableFixture() (gxscene.Props, *signal.Signal[float64]) {
	scroll := signal.New(0.0)
	return gxscene.Props{
		"columns": []TableColumn{
			{Key: "name", Title: "姓名", Width: "100", Sortable: true},
			{Key: "age", Title: "年龄", Width: "80"},
		},
		"rows": func() any {
			rows := make([]any, 1000)
			for i := range rows {
				rows[i] = []string{fmt.Sprintf("用户%d", i), fmt.Sprint(20 + i%30)}
			}
			return rows
		},
		"scroll-top":      func() any { return scroll() },
		"row-height":      40,
		"viewport-height": 200,
	}, &scroll
}

func TestTableStructure(t *testing.T) {
	props, _ := tableFixture()
	tbl := Table(props)
	root := tbl.Root()
	if findByGxRole(root, "table") == nil {
		t.Fatal("缺少 table 容器")
	}
	if findByGxRole(root, "table-head") == nil {
		t.Fatal("缺少表头")
	}
	heads := findAllByRole(root, "table-head-cell")
	if len(heads) != 2 {
		t.Fatalf("应有 2 个表头单元格, got %d", len(heads))
	}
	if heads[0].Handler("onClick") == nil {
		t.Error("可排序列头应有 onClick")
	}
	text := gxscene.TextBackend{}.Paint(root)
	if !containsStr(text, "姓名") || !containsStr(text, "年龄") {
		t.Errorf("表头文本缺失: %q", text)
	}
}

// TestTableVirtualization 核心：1000 行数据下，场景树中的 table-row 数量应远小于
// 1000（只渲染视口可见 + overscan），验证节点数与数据量解耦。
func TestTableVirtualization(t *testing.T) {
	props, _ := tableFixture()
	tbl := Table(props)
	tbl.BindReactive(signal.Effect)
	rows := findAllByRole(tbl.Root(), "table-row")
	if len(rows) == 0 {
		t.Fatal("应渲染可见行")
	}
	if len(rows) >= 100 {
		t.Errorf("1000 行数据只应渲染视口附近行, got %d", len(rows))
	}
	t.Logf("可见行数 = %d (数据 1000 行)", len(rows))
}

func TestTableScrollWindowShifts(t *testing.T) {
	props, scroll := tableFixture()
	tbl := Table(props)
	tbl.BindReactive(signal.Effect)
	first := gxscene.TextBackend{}.Paint(tbl.Root())
	// 滚动到 500 行处（写信号）
	(*scroll)(500.0)
	second := gxscene.TextBackend{}.Paint(tbl.Root())
	if first == second {
		t.Error("滚动后可见行内容应变化")
	}
}

func TestTableSortEmit(t *testing.T) {
	props, _ := tableFixture()
	var sorted string
	props["on-sort"] = BindValue(func(s string) { sorted = s })
	tbl := Table(props)
	tbl.BindReactive(signal.Effect)
	heads := findAllByRole(tbl.Root(), "table-head-cell")
	if len(heads) == 0 || heads[0].Handler("onClick") == nil {
		t.Fatal("首个表头应可排序")
	}
	heads[0].Dispatch("onClick", gxscene.EventContext{})
	if sorted != "name" {
		t.Errorf("排序回传 = %q, want name", sorted)
	}
}

func TestTableSelectEmit(t *testing.T) {
	props, _ := tableFixture()
	var selected string
	props["on-select"] = BindValue(func(s string) { selected = s })
	tbl := Table(props)
	tbl.BindReactive(signal.Effect)
	rows := findAllByRole(tbl.Root(), "table-row")
	if len(rows) == 0 {
		t.Fatal("无可见行")
	}
	rows[0].Dispatch("onClick", gxscene.EventContext{})
	if selected == "" {
		t.Error("点击行应回传索引")
	}
}
