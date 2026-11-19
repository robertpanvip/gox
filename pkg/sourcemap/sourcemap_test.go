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
