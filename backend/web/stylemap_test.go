package gxweb

import (
	"strconv"
	"strings"
	"testing"
)

func TestSelectionVars(t *testing.T) {
	bg, fg := selectionVars(`{"bg":"#b4d5fe","color":"#fff"}`)
	if bg != "#b4d5fe" || fg != "#fff" {
		t.Errorf("selectionVars bg/fg = %q/%q", bg, fg)
	}
	// background 作为 bg 别名。
	bg, fg = selectionVars(`{"background":"#264f78"}`)
	if bg != "#264f78" || fg != "" {
		t.Errorf("background 别名 bg/fg = %q/%q", bg, fg)
	}
	// 空/非法输入不产生变量。
	if bg, fg = selectionVars(""); bg != "" || fg != "" {
		t.Errorf("空输入应无变量: %q/%q", bg, fg)
	}
	if bg, fg = selectionVars("{bad"); bg != "" || fg != "" {
		t.Errorf("非法 JSON 应无变量: %q/%q", bg, fg)
	}
}

func TestSelectionKeyClassification(t *testing.T) {
	if !isSelectionKey("&::selection") || !isSelectionKey("::selection") {
		t.Error("伪元素选区键应被识别")
	}
	if isSelectionKey("&:selection") {
		t.Error("单冒号是伪状态，不应识别为伪元素")
	}
	if !isPseudoElementKey("&::selection") || isPseudoElementKey("&:hover") {
		t.Error("伪元素与伪状态前缀应严格区分")
	}
	if isStyleKey("&::selection") {
		t.Error("伪元素键不应被当成内联样式键")
	}
	if !strings.Contains(selectionGlobalCSS, "var("+selectionVarBg+")") ||
		!strings.Contains(selectionGlobalCSS, "::selection") {
		t.Errorf("全局规则应读取选区自定义属性: %q", selectionGlobalCSS)
	}
}

func TestFontFamilyPassthrough(t *testing.T) {
	if !isStyleKey("font-family") {
		t.Fatal("font-family 应被识别为内联样式键")
	}
	stack := "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace"
	if got := cssValue("font-family", stack); got != stack {
		t.Errorf("字体栈应原样透传（不能加 px）: got %q", got)
	}
	if got := cssValue("font-family", "monospace"); got != "monospace" {
		t.Errorf("单个族名应原样透传: got %q", got)
	}
}

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

// TestGradientTextProp 验证三端统一的 gradient-text 语义开关识别：
// Web 后端据此把 bg 渐变裁到文字（background-clip:text + color:transparent），
// 与 Skia/Rust 的 shader 裁剪等价，编辑器因此不再依赖宿主 HTML 内联 CSS。
func TestGradientTextProp(t *testing.T) {
	for _, tc := range []struct {
		key    string
		isProp bool
	}{
		{"gradient-text", true},
		{"GRADIENT-TEXT", true},
		{"background", false},
		{"color", false},
	} {
		if got := isGradientTextProp(tc.key); got != tc.isProp {
			t.Errorf("isGradientTextProp(%q) = %v, want %v", tc.key, got, tc.isProp)
		}
	}
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"1", true}, {"true", true}, {"TRUE", true},
		{"0", false}, {"", false}, {"false", false},
	} {
		if got := gradientTextEnabled(tc.value); got != tc.want {
			t.Errorf("gradientTextEnabled(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

// TestStyleTargetPropGradientUsesImage 回归：bg=linear-gradient 必须映射到
// background-image，不能用 background 简写——后者会重置 background-clip，
// 导致 gradient-text 节点的渐变被画成背景块、文字被 color:transparent 藏掉。
func TestStyleTargetPropGradientUsesImage(t *testing.T) {
	grad := "linear-gradient(90deg, #c586c0 0ch 4ch, #d4d4d4 4ch 8ch)"
	if got := styleTargetProp("bg", grad); got != "background-image" {
		t.Errorf("bg 渐变应映射 background-image，got %q", got)
	}
	if got := styleTargetProp("bg", " #ff0000 "); got == "background-image" {
		t.Errorf("普通 bg 颜色不应映射 background-image，got %q", got)
	}
	if got := styleTargetProp("bg", grad); got == "background" {
		t.Error("bg 渐变不得用 background 简写（会重置 background-clip）")
	}
}

// TestAnimationCSS 验证声明式 animation prop 被翻译成原生 CSS animation，
// 且非法值返回空串（调用方据此移除属性）。
func TestAnimationCSS(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"blink 1.06s infinite", "gx-blink 1.06s steps(1) infinite"},
		{"blink 1060ms", "gx-blink 1.06s steps(1) infinite"},
		{"blink 500ms 3", "gx-blink 0.5s steps(1) 3"},
		{"", ""},
		{"none", ""},
		{"pulse 1s", ""},
	}
	for _, c := range cases {
		if got := animationCSS(c.in); got != c.want {
			t.Errorf("animationCSS(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestAnimationIsStyleKey 守护 animation / pointer-events 进入样式白名单，
// 确保两个新 prop 走 CSS 属性路径而非普通 DOM 属性。
func TestAnimationIsStyleKey(t *testing.T) {
	for _, k := range []string{"animation", "pointer-events", "ANIMATION"} {
		if !isStyleKey(k) {
			t.Errorf("isStyleKey(%q) 应为 true", k)
		}
	}
}
