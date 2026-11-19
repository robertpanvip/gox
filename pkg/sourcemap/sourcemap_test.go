package sourcemap

import "testing"

func TestBidirectionalMapping(t *testing.T) {
	raw := []RawMapping{
		{OutLine: 1, OutCol: 1, InLine: 1, InCol: 1},
		{OutLine: 3, OutCol: 5, InLine: 2, InCol: 3},
		{OutLine: 8, OutCol: 1, InLine: 6, InCol: 1},
	}
	sm := Build("x.gx", raw)

	cases := []struct {
		goLine, goCol int
		wantLine      int
	}{
		{1, 1, 1},
		{3, 5, 2},
		{4, 1, 2}, // 落在 3:5 与 8:1 之间，取最近前一个
		{8, 1, 6},
	}
	for _, c := range cases {
		pos, ok := sm.GoToGx(c.goLine, c.goCol)
		if !ok {
			t.Fatalf("GoToGx(%d,%d) 无映射", c.goLine, c.goCol)
		}
		if pos.Line != c.wantLine {
			t.Errorf("GoToGx(%d,%d).Line = %d, want %d", c.goLine, c.goCol, pos.Line, c.wantLine)
		}
	}

	// 反向
	pos, ok := sm.GxToGo(2, 3)
	if !ok || pos.Line != 3 || pos.Column != 5 {
		t.Errorf("GxToGo(2,3) = %+v ok=%v", pos, ok)
	}
}

func TestEmptyMap(t *testing.T) {
	sm := Build("x.gx", nil)
	if _, ok := sm.GoToGx(1, 1); ok {
		t.Error("空映射不应返回结果")
	}
	if sm.Len() != 0 {
		t.Error("空映射 Len 应为 0")
	}
}

func TestJSONRoundTrip(t *testing.T) {
	raw := []RawMapping{{OutLine: 2, OutCol: 4, InLine: 1, InCol: 2}}
	sm := Build("a.gx", raw)
	data, err := sm.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var got SourceMap
	if err := got.UnmarshalJSON(data); err != nil {
		t.Fatal(err)
	}
	if got.File != "a.gx" || got.Len() != 1 {
		t.Fatalf("往返失败: %+v", got)
	}
	pos, ok := got.GoToGx(2, 4)
	if !ok || pos.Line != 1 || pos.Column != 2 {
		t.Errorf("往返后查询失败: %+v ok=%v", pos, ok)
	}
}

func TestColumnOffsetAdjustment(t *testing.T) {
	// 生成的第 5 行第 10 列对应源第 2 行第 3 列；查询同行更大列时线性修正偏移
	raw := []RawMapping{
		{OutLine: 5, OutCol: 10, InLine: 2, InCol: 3},
		{OutLine: 6, OutCol: 1, InLine: 3, InCol: 1},
	}
	sm := Build("x.gx", raw)
	pos, ok := sm.GoToGx(5, 13)
	if !ok || pos.Line != 2 || pos.Column != 6 {
		t.Errorf("GoToGx(5,13) = %+v ok=%v, want 2:6 (3 + 3)", pos, ok)
	}
	// 反向：源 2:5 → 生成 5:12
	pos, ok = sm.GxToGo(2, 5)
	if !ok || pos.Line != 5 || pos.Column != 12 {
		t.Errorf("GxToGo(2,5) = %+v ok=%v, want 5:12", pos, ok)
	}
}

func TestUnsortedRawMappings(t *testing.T) {
	// 输入乱序，Build 应排序后二分查询仍正确
	raw := []RawMapping{
		{OutLine: 8, OutCol: 1, InLine: 6, InCol: 1},
		{OutLine: 1, OutCol: 1, InLine: 1, InCol: 1},
		{OutLine: 3, OutCol: 5, InLine: 2, InCol: 3},
	}
	sm := Build("x.gx", raw)
	cases := []struct {
		line, col, wantLine int
	}{
		{1, 1, 1},
		{3, 5, 2},
		{4, 9, 2},
		{8, 1, 6},
	}
	for _, c := range cases {
		pos, ok := sm.GoToGx(c.line, c.col)
		if !ok || pos.Line != c.wantLine {
			t.Errorf("GoToGx(%d,%d) = %+v ok=%v, want line %d", c.line, c.col, pos, ok, c.wantLine)
		}
	}
}

func TestNilSourceMap(t *testing.T) {
	var sm *SourceMap
	if _, ok := sm.GoToGx(1, 1); ok {
		t.Error("nil map GoToGx 应返回 ok=false")
	}
	if _, ok := sm.GxToGo(1, 1); ok {
		t.Error("nil map GxToGo 应返回 ok=false")
	}
	if sm.Len() != 0 {
		t.Error("nil map Len 应为 0")
	}
	if sm.Mappings() != nil {
		t.Error("nil map Mappings 应为 nil")
	}
}

func TestBeforeFirstMapping(t *testing.T) {
	// 查询早于第一条映射的位置，应回退到第一条（ok=true）
	raw := []RawMapping{{OutLine: 5, OutCol: 10, InLine: 2, InCol: 3}}
	sm := Build("x.gx", raw)
	pos, ok := sm.GoToGx(1, 1)
	if !ok || pos.Line != 2 || pos.Column != 3 {
		t.Errorf("早于首映射应回退到首条: %+v ok=%v", pos, ok)
	}
}
