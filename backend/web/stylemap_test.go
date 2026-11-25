package gxweb

import (
	"strconv"
	"strings"
	"testing"
)

func TestCSSValueBoxShorthand(t *testing.T) {
	tests := []struct {
		key  string
		in   string
		want string
	}{
		{"padding", "8", "8px"},
		{"padding", "7 14", "7px 14px"},
		{"padding", "0 16 12 16", "0px 16px 12px 16px"},
		{"margin", "4 8", "4px 8px"},
		{"border-radius", "6 10", "6px 10px"},
		{"gap", "2 6", "2px 6px"},
		{"padding", "auto", "auto"}, // 非数值原样透传
		{"opacity", "0.5", "0.5"},   // 无量纲
		{"width", "100", "100px"},   // 普通长度
		{"color", "#fff", "#fff"},   // 颜色
	}
	for _, tc := range tests {
		if got := cssValue(tc.key, tc.in); got != tc.want {
			t.Errorf("cssValue(%q, %q) = %q, want %q", tc.key, tc.in, got, tc.want)
		}
	}
}

// TestExpandBoxShadowMatchesSkia 验证 Web 端 6 层展开与 Skia/Rust 端
// paintShadow 算法逐层一致：同一 token 两端算出相同的 expand 与 alpha 序列。
func TestExpandBoxShadowMatchesSkia(t *testing.T) {
	// "0 4px 16px 0 #00000040"：ox=0 oy=4 blur=16 spread=0 baseA=0x40=64
	got := expandBoxShadow("0 4px 16px 0 #00000040")
	layers := strings.Split(got, ", ")
	if len(layers) != 6 {
		t.Fatalf("应展开 6 层，got %d: %q", len(layers), got)
	}

	// 复刻 Skia paintShadow 公式，逐层比对。
	const baseA = 64.0
	const blur = 16.0
	const spread = 0.0
	for i := 1; i <= 6; i++ {
		tt := float64(i) / 6
		wantExpand := spread + blur*tt
		wantA := baseA * (1 - tt) / 6 * 1.8
		if wantA < 0 {
			wantA = 0
		} else if wantA > 255 {
			wantA = 255
		}

		// 期望形如 "0px 4px 0px 2.6667px rgba(0,0,0,0.0627)"
		// 只需确认该层包含正确的 expand（第 4 段）与 alpha（rgba 末段）。
		fields := strings.Fields(layers[i-1])
		if len(fields) < 5 {
			t.Fatalf("第 %d 层字段不足: %q", i, layers[i-1])
		}
		gotExpand := strings.TrimSuffix(fields[3], "px")
		if !approx(gotExpand, wantExpand) {
			t.Errorf("第 %d 层 expand: got %s, want %g", i, gotExpand, wantExpand)
		}
		wantAlpha := wantA / 255
		// fields[4] 形如 "rgba(0,0,0,0.0627)"，取最后一个逗号后的数字。
		rgbaParts := strings.Split(fields[4], ",")
		if len(rgbaParts) < 4 {
			t.Fatalf("第 %d 层 rgba 字段异常: %q", i, fields[4])
		}
		gotAlphaStr := strings.TrimSuffix(rgbaParts[3], ")")
		if !approx(gotAlphaStr, wantAlpha) {
			t.Errorf("第 %d 层 alpha: got %s, want %.4f", i, gotAlphaStr, wantAlpha)
		}
	}
}

// TestExpandBoxShadowPassthrough 多层串与非法串原样透传，避免二次展开。
func TestExpandBoxShadowPassthrough(t *testing.T) {
	multi := "0 1px 2px rgba(0,0,0,0.06), 0 4px 16px rgba(0,0,0,0.1)"
	if got := cssValue("box-shadow", multi); got != multi {
		t.Errorf("多层阴影应原样透传: got %q", got)
	}
	if got := cssValue("box-shadow", "garbage"); got != "garbage" {
		t.Errorf("非法阴影应原样透传: got %q", got)
	}
}

// approx 解析浮点字符串并比较（容差 1e-3）。
func approx(s string, want float64) bool {
	got, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return false
	}
	d := got - want
	if d < 0 {
		d = -d
	}
	return d <= 1e-3
}
