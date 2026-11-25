package gxscene

import "testing"

func TestParseAnimation(t *testing.T) {
	cases := []struct {
		in       string
		active   bool
		durMS    int64
		infinite bool
	}{
		{"blink 1.06s infinite", true, 1060, true},
		{"blink 1060ms", true, 1060, true},
		{"blink 1.06", true, 1060, true},
		{"blink 500ms 3", true, 500, false},
		{"", false, 0, false},
		{"none", false, 0, false},
		{"pulse 1s", false, 0, false}, // 未内置动画名 → 忽略
		{"blink", false, 0, false},    // 缺时长 → 忽略
	}
	for _, c := range cases {
		spec := ParseAnimation(c.in)
		if spec.Active() != c.active {
			t.Errorf("ParseAnimation(%q).Active=%v, want %v", c.in, spec.Active(), c.active)
			continue
		}
		if !c.active {
			continue
		}
		if spec.DurationMS != c.durMS {
			t.Errorf("ParseAnimation(%q).DurationMS=%d, want %d", c.in, spec.DurationMS, c.durMS)
		}
		if spec.Infinite() != c.infinite {
			t.Errorf("ParseAnimation(%q).Infinite=%v, want %v", c.in, spec.Infinite(), c.infinite)
		}
	}
}

func TestAnimationBlinkSample(t *testing.T) {
	spec := ParseAnimation("blink 1000ms infinite")
	// 半周期方波：相位 <500ms → 1，>=500ms → 0。
	cases := []struct {
		nowMS int64
		op    float32
		active bool
	}{
		{0, 1, true},
		{499, 1, true},
		{500, 0, true},
		{999, 0, true},
		{1000, 1, true}, // 下一周期起点
		{1500, 0, true},
	}
	for _, c := range cases {
		op, active := spec.Sample(c.nowMS)
		if op != c.op || active != c.active {
			t.Errorf("Sample(%d)=(%v,%v), want (%v,%v)", c.nowMS, op, active, c.op, c.active)
		}
	}
}

func TestAnimationFiniteIterations(t *testing.T) {
	spec := ParseAnimation("blink 1000ms 2")
	// 2 次共 2000ms；之后不覆盖基础 opacity。
	if _, active := spec.Sample(1999); !active {
		t.Error("有限次数未结束时应仍活跃")
	}
	if _, active := spec.Sample(2000); active {
		t.Error("有限次数结束后不应再覆盖基础样式")
	}
}

func TestAnimationFromProps(t *testing.T) {
	p := map[string]any{PropAnimation: "blink 1.06s infinite"}
	spec, ok := AnimationFromProps(p)
	if !ok || spec.DurationMS != 1060 {
		t.Fatalf("AnimationFromProps 失败: spec=%+v ok=%v", spec, ok)
	}
	if _, ok := AnimationFromProps(map[string]any{}); ok {
		t.Error("空 props 不应有动画")
	}
}
