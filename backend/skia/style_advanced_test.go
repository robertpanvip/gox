package skia

import (
	"math"
	"testing"
)

func TestParsePositionInset(t *testing.T) {
	s := parseStyle(map[string]any{
		"position": "absolute", "top": 10, "left": "20px", "right": "auto", "bottom": 30,
	})
	if s.position != positionAbsolute {
		t.Fatalf("position = %v, want absolute", s.position)
	}
	if s.inset[0] != 10 || s.inset[3] != 20 || s.inset[2] != 30 {
		t.Errorf("inset(top,left,bottom) = %v,%v,%v", s.inset[0], s.inset[3], s.inset[2])
	}
	if !math.IsNaN(float64(s.inset[1])) {
		t.Errorf("inset[right] = %v, want auto(NaN)", s.inset[1])
	}
	// 默认 relative + 全 auto
	d := parseStyle(nil)
	if d.position != positionRelative {
		t.Error("默认 position 应为 relative")
	}
}

func TestParseOverflowScroll(t *testing.T) {
	for in, want := range map[string]overflowMode{
		"hidden": overflowHidden, "scroll": overflowScroll, "auto": overflowAuto,
		"visible": overflowVisible, "nonsense": overflowVisible,
	} {
		got := parseStyle(map[string]any{"overflow": in, "scroll-top": "42"}).overflow
		if got != want {
			t.Errorf("overflow=%q got %v want %v", in, got, want)
		}
	}
	s := parseStyle(map[string]any{"scroll-top": 42, "scroll-left": "7px"})
	if s.scrollTop != 42 || s.scrollLeft != 7 {
		t.Errorf("scroll = %v,%v want 42,7", s.scrollTop, s.scrollLeft)
	}
}

func TestParseTransform(t *testing.T) {
	tr := parseTransform("translate(10px, -4px) scale(1.5)")
	if tr.translateX != 10 || tr.translateY != -4 || tr.scale != 1.5 {
		t.Errorf("transform = %+v", tr)
	}
	tr2 := parseTransform("translateX(8) translateY(2)")
	if tr2.translateX != 8 || tr2.translateY != 2 || tr2.scale != 1 {
		t.Errorf("transform2 = %+v", tr2)
	}
}

func TestParseBoxShadow(t *testing.T) {
	sh, ok := parseBoxShadow("0 4px 12px 2px rgba(0,0,0,0.35)")
	if !ok {
		t.Fatal("解析失败")
	}
	if sh.offsetY != 4 || sh.blur != 12 || sh.spread != 2 {
		t.Errorf("shadow = %+v", sh)
	}
	a := sh.color >> 24 & 0xff
	if a < 80 || a > 95 {
		t.Errorf("alpha = %d want ~89", a)
	}
	// 简写：仅 offset + 颜色
	sh2, ok := parseBoxShadow("2 3 #000000")
	if !ok || sh2.offsetX != 2 || sh2.offsetY != 3 || sh2.blur != 0 || sh2.spread != 0 {
		t.Errorf("shadow2 = %+v ok=%v", sh2, ok)
	}
}

func TestParseTransition(t *testing.T) {
	trs := parseTransition("opacity 300ms ease, transform 200ms linear, width .4s ease-in-out")
	if len(trs) != 3 {
		t.Fatalf("len = %d want 3: %+v", len(trs), trs)
	}
	if trs[0].property != "opacity" || trs[0].duration != 300 || trs[0].easing != easeDefault {
		t.Errorf("trs[0] = %+v", trs[0])
	}
	if trs[1].property != "transform" || trs[1].duration != 200 || trs[1].easing != easeLinear {
		t.Errorf("trs[1] = %+v", trs[1])
	}
	if trs[2].duration != 400 || trs[2].easing != easeInOut {
		t.Errorf("trs[2] = %+v", trs[2])
	}
}

func TestEasing(t *testing.T) {
	if easingValue(easeLinear, 0.5) != 0.5 {
		t.Error("linear 0.5")
	}
	if v := easingValue(easeIn, 0.5); v > 0.2 {
		t.Errorf("ease-in 0.5 = %v too high", v)
	}
	if v := easingValue(easeOut, 0.5); v < 0.8 {
		t.Errorf("ease-out 0.5 = %v too low", v)
	}
	if v := easingValue(easeInOut, 0.5); math.Abs(float64(v-0.5)) > 0.01 {
		t.Errorf("ease-in-out 0.5 = %v", v)
	}
}

func TestParseColorAlpha(t *testing.T) {
	// rgba 带 alpha
	c, ok := parseColor("rgba(10, 20, 30, 0.5)")
	if !ok {
		t.Fatal("rgba 解析失败")
	}
	if c>>24&0xff < 125 || c>>24&0xff > 130 {
		t.Errorf("rgba alpha = %d", c>>24&0xff)
	}
}
