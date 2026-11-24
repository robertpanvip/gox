package video

import "testing"

func TestMetaRoundTrip(t *testing.T) {
	m := Meta{Duration: 12.5, W: 1280, H: 720}
	s := EncodeMeta(m)
	got, ok := DecodeMeta(s)
	if !ok {
		t.Fatalf("DecodeMeta 失败: %s", s)
	}
	if got != m {
		t.Errorf("Meta 往返不一致: %+v != %+v", got, m)
	}
	if _, ok := DecodeMeta("not json"); ok {
		t.Error("非法 JSON 应返回 ok=false")
	}
}

func TestNumString(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{2, "2"},
		{int64(3), "3"},
		{3.5, "3.5"},
		{"4", "4"},
		{func() float64 { return 1.25 }, "1.25"},
		{[]int{1}, ""},
	}
	for _, c := range cases {
		if got := NumString(c.in); got != c.want {
			t.Errorf("NumString(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
