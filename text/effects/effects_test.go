package effects

import "testing"

func TestParseTextShadow(t *testing.T) {
	t.Run("none", func(t *testing.T) {
		if got := parseTextShadow("none"); got != nil {
			t.Fatalf("none 应返回 nil，得到 %v", got)
		}
		if got := parseTextShadow("  "); got != nil {
			t.Fatalf("空白应返回 nil，得到 %v", got)
		}
	})

	t.Run("single", func(t *testing.T) {
		got := parseTextShadow("2px 3px 4px rgba(0,0,0,0.3)")
		if len(got) != 1 {
			t.Fatalf("期望 1 条阴影，得到 %d", len(got))
		}
		sh := got[0]
		if sh.OffsetX != 2 || sh.OffsetY != 3 || sh.Blur != 4 {
			t.Fatalf("偏移/模糊错误: %+v", sh)
		}
		if sh.Color != "rgba(0,0,0,0.3)" {
			t.Fatalf("颜色错误: %q", sh.Color)
		}
	})

	t.Run("multiple-with-rgba-commas", func(t *testing.T) {
		got := parseTextShadow("2px 2px 4px rgba(0, 0, 0, 0.3), 0 0 10px rgba(0, 150, 255, 0.5)")
		if len(got) != 2 {
			t.Fatalf("期望 2 条阴影（rgba 内逗号不应切分），得到 %d: %+v", len(got), got)
		}
		if got[0].Color != "rgba(0,0,0,0.3)" || got[1].Color != "rgba(0,150,255,0.5)" {
			t.Fatalf("多阴影颜色错误: %+v", got)
		}
		if got[1].OffsetX != 0 || got[1].Blur != 10 {
			t.Fatalf("第二条阴影参数错误: %+v", got[1])
		}
	})

	t.Run("color-first", func(t *testing.T) {
		got := parseTextShadow("red 1px 2px")
		if len(got) != 1 || got[0].Color != "red" || got[0].OffsetX != 1 || got[0].OffsetY != 2 {
			t.Fatalf("颜色在前解析错误: %+v", got)
		}
	})
}

func TestParseTextStroke(t *testing.T) {
	t.Run("spec", func(t *testing.T) {
		got := parseTextStroke(map[string]string{"-webkit-text-stroke": "2px red"})
		if got == nil || got.Width != 2 || got.Color != "red" {
			t.Fatalf("描边解析错误: %+v", got)
		}
		if got.Join != StrokeJoinMiter || got.MiterLimit != 4 {
			t.Fatalf("默认值错误: %+v", got)
		}
	})

	t.Run("separate-props", func(t *testing.T) {
		got := parseTextStroke(map[string]string{
			"-webkit-text-stroke-width": "3px",
			"-webkit-text-stroke-color": "#fff",
		})
		if got == nil || got.Width != 3 || got.Color != "#fff" {
			t.Fatalf("独立属性解析错误: %+v", got)
		}
	})

	t.Run("absent", func(t *testing.T) {
		if got := parseTextStroke(map[string]string{}); got != nil {
			t.Fatalf("无描边应返回 nil，得到 %+v", got)
		}
	})
}

func TestParseTextFillRequiresBackgroundClipText(t *testing.T) {
	grad := "linear-gradient(90deg, #f00 0%, #00f 100%)"

	t.Run("with-clip", func(t *testing.T) {
		got := parseTextFill(map[string]string{
			"background-clip":  "text",
			"background-image": grad,
		})
		if got == nil || got.Kind != TextFillLinearGradient || got.Gradient == nil {
			t.Fatalf("background-clip:text 应激活渐变填充: %+v", got)
		}
		if got.Gradient.Angle != 90 || len(got.Gradient.Stops) != 2 {
			t.Fatalf("渐变解析错误: %+v", got.Gradient)
		}
		if got.Gradient.Stops[0].Offset != 0 || got.Gradient.Stops[1].Offset != 1 {
			t.Fatalf("色标位置错误: %+v", got.Gradient.Stops)
		}
	})

	t.Run("webkit-clip", func(t *testing.T) {
		got := parseTextFill(map[string]string{
			"-webkit-background-clip": "text",
			"bg":                      grad,
		})
		if got == nil || got.Kind != TextFillLinearGradient {
			t.Fatalf("-webkit-background-clip:text 应生效: %+v", got)
		}
	})

	// 关键语义：没有 background-clip:text，渐变只是背景，不能当渐变文字。
	t.Run("gradient-without-clip", func(t *testing.T) {
		got := parseTextFill(map[string]string{"bg": grad, "color": "#fff"})
		if got != nil {
			t.Fatalf("无 background-clip:text 时渐变不应作用于文字，得到 %+v", got)
		}
	})

	t.Run("text-fill-color", func(t *testing.T) {
		got := parseTextFill(map[string]string{"-webkit-text-fill-color": "#0f0"})
		if got == nil || got.Kind != TextFillSolid || got.Color != "#0f0" {
			t.Fatalf("text-fill-color 解析错误: %+v", got)
		}
	})
}

func TestParseGradient(t *testing.T) {
	t.Run("radial", func(t *testing.T) {
		g := parseGradient("radial-gradient(circle at center, red, blue)")
		if g == nil || g.Kind != TextFillRadialGradient || len(g.Stops) != 2 {
			t.Fatalf("radial 解析错误: %+v", g)
		}
	})

	t.Run("angle-keywords", func(t *testing.T) {
		for _, tc := range []struct {
			in   string
			want float32
		}{
			{"to right", 90},
			{"to bottom", 180},
			{"0.25turn", 90},
			{"45deg", 45},
		} {
			g := parseGradient("linear-gradient(" + tc.in + ", red, blue)")
			if g == nil || g.Angle != tc.want {
				t.Fatalf("%q 角度错误: 期望 %v，得到 %+v", tc.in, tc.want, g)
			}
		}
	})

	t.Run("invalid", func(t *testing.T) {
		if g := parseGradient("url(a.png)"); g != nil {
			t.Fatalf("非渐变应返回 nil: %+v", g)
		}
	})
}

func TestParseImageFill(t *testing.T) {
	got := parseTextFill(map[string]string{
		"background-clip":     "text",
		"background-image":    `url("pic.png")`,
		"background-size":     "cover",
		"background-position": "center",
	})
	if got == nil || got.Kind != TextFillImage || got.Image == nil {
		t.Fatalf("图片填充解析错误: %+v", got)
	}
	if got.Image.URL != "pic.png" || got.Image.Size != "cover" || got.Image.Position != "center" {
		t.Fatalf("图片参数错误: %+v", got.Image)
	}
}

func TestParseCombineAllEffects(t *testing.T) {
	e := Parse(map[string]string{
		"text-shadow":         "1px 2px 3px rgba(0,0,0,.5)",
		"-webkit-text-stroke": "1px #000",
		"background-clip":     "text",
		"background-image":    "linear-gradient(red, blue)",
	})
	if !e.Active() || !e.HasFill() {
		t.Fatalf("应识别为有效果: %+v", e)
	}
	if len(e.Shadows) != 1 || e.Stroke == nil || e.Fill == nil {
		t.Fatalf("组合效果解析不完整: %+v", e)
	}

	empty := Parse(map[string]string{"color": "#fff", "font-size": "12"})
	if empty.Active() {
		t.Fatalf("普通样式不应有文本效果: %+v", empty)
	}
}

func TestCacheKeysAreComparable(t *testing.T) {
	// 三层缓存键必须可作 map key，否则无法用作缓存索引。
	layout := map[LayoutKey]int{{Text: "hi", FontFamily: "mono", FontSize: 12}: 1}
	geom := map[GeometryKey]int{{FontFamily: "mono", GlyphID: 7, FontSize: 12, Scale: 2}: 1}
	effect := map[EffectKey]int{{Geometry: GeometryKey{GlyphID: 7}, Effect: "blur:4", DPR: 2}: 1}
	if layout[LayoutKey{Text: "hi", FontFamily: "mono", FontSize: 12}] != 1 ||
		geom[GeometryKey{FontFamily: "mono", GlyphID: 7, FontSize: 12, Scale: 2}] != 1 ||
		effect[EffectKey{Geometry: GeometryKey{GlyphID: 7}, Effect: "blur:4", DPR: 2}] != 1 {
		t.Fatal("缓存键命中失败")
	}
}

func TestCapabilities(t *testing.T) {
	skia := SkiaCapabilities()
	if !skia.NativeStroke || !skia.AlphaMask || skia.ShaderFill {
		t.Fatalf("Skia 能力矩阵与 go-skia 现状不符: %+v", skia)
	}
	glyphon := GlyphonCapabilities()
	if !glyphon.ShaderFill || !glyphon.ImageFill || !glyphon.AlphaMask {
		t.Fatalf("Glyphon 能力矩阵错误: %+v", glyphon)
	}
	web := WebCapabilities()
	if !web.NativeShadow || !web.NativeStroke || !web.ShaderFill || !web.ImageFill {
		t.Fatalf("Web 能力矩阵错误: %+v", web)
	}
}
