package widgets

import (
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

func TestThemeDefaults(t *testing.T) {
	if DarkTheme.Name != "dark" || LightTheme.Name != "light" {
		t.Error("主题名错误")
	}
	if DarkTheme.Bg == LightTheme.Bg {
		t.Error("明暗主题底色应不同")
	}
	if DarkTheme.Accent != AppleBlue || LightTheme.Accent != "#007aff" {
		t.Error("强调色错误")
	}
}

func TestThemeToken(t *testing.T) {
	tests := []struct{ name, want string }{
		{"accent", AppleBlue},
		{"bg", AppleWindowBg},
		{"radius-card", RadiusCard},
		{"RadiusCard", RadiusCard},
		{"radius_card", RadiusCard},
		{"error", AppleRed},
		{"space-2", Space2},
		{"nonexistent", ""},
	}
	for _, tt := range tests {
		if got := themeToken(DarkTheme, tt.name); got != tt.want {
			t.Errorf("themeToken(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// TestEditorSelectFgToken 验证选中文字前景 token：深色主题浅字、浅色主题深字，
// 与选区背景（selectSolid）形成足够对比，解决"选中高亮看不清文字"。
func TestEditorSelectFgToken(t *testing.T) {
	dfg := themeToken(DarkTheme, "editorselectfg")
	dbg := themeToken(DarkTheme, "editorselectsolid")
	lfg := themeToken(LightTheme, "editorselectfg")
	lbg := themeToken(LightTheme, "editorselectsolid")
	if dfg != "#ffffff" || lfg != "#1d1d1f" {
		t.Errorf("选中前景色错误: dark=%q light=%q", dfg, lfg)
	}
	if dfg == dbg || lfg == lbg {
		t.Errorf("选中前景与背景不得相同: dark %q/%q light %q/%q", dfg, dbg, lfg, lbg)
	}
	if DarkTheme.EditorSelectFg != dfg || LightTheme.EditorSelectFg != lfg {
		t.Error("Theme 字段与 token 映射不一致")
	}
}

func TestUseTokenReactive(t *testing.T) {
	// 先切到确保的初始态，避免测试间 themeCtx 全局状态串扰。
	SetTheme(DarkTheme)
	accent := UseToken("accent")
	if accent() != AppleBlue {
		t.Fatalf("初始强调色应为 %q, got %q", AppleBlue, accent())
	}
	SetTheme(LightTheme)
	if accent() != "#007aff" {
		t.Errorf("切浅色后 accent getter 应为 #007aff, got %q", accent())
	}
	SetTheme(DarkTheme) // 复位，避免影响其它测试
}

func TestLegacyThemeTokensFollowSetTheme(t *testing.T) {
	SetTheme(DarkTheme)
	if ThemeBg != DarkTheme.Bg || ThemeText != DarkTheme.Text || ThemeAccent != DarkTheme.Accent {
		t.Fatalf("深色旧令牌未同步: bg=%q text=%q accent=%q", ThemeBg, ThemeText, ThemeAccent)
	}
	if tones["primary"] != DarkTheme.Accent || ShadowCard != DarkTheme.ShadowCard {
		t.Fatalf("深色 tone/阴影未同步: tone=%q shadow=%q", tones["primary"], ShadowCard)
	}

	SetTheme(LightTheme)
	defer SetTheme(DarkTheme)
	checks := []struct{ name, got, want string }{
		{"ThemeBg", ThemeBg, LightTheme.Bg},
		{"ThemePanel", ThemePanel, LightTheme.Panel},
		{"ThemeText", ThemeText, LightTheme.Text},
		{"ThemeMuted", ThemeMuted, LightTheme.Muted},
		{"ThemeAccent", ThemeAccent, LightTheme.Accent},
		{"tones[primary]", tones["primary"], LightTheme.Accent},
		{"ShadowCard", ShadowCard, LightTheme.ShadowCard},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestTextUsesCurrentThemeToken(t *testing.T) {
	SetTheme(LightTheme)
	defer SetTheme(DarkTheme)
	inst := Text(gxscene.Props{"value": "浅色文字", "tone": "muted"})
	if got := findAttrDeep(inst.Root(), "color"); got != LightTheme.Muted {
		t.Errorf("浅色主题 muted 文本颜色 = %q, want %q", got, LightTheme.Muted)
	}
}

func TestUseThemeDrivesComponentSlot(t *testing.T) {
	SetTheme(DarkTheme)
	use := UseTheme()
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		el("span"), dynChild(0), closeEl(),
	}, N: 1}, func() any { return use().Name })
	inst.BindReactive(signal.Effect)
	paint := func() string { return gxscene.TextBackend{}.Paint(inst.Root()) }
	if got := paint(); !containsStr(got, "dark") {
		t.Fatalf("初始应为 dark: %q", got)
	}
	SetTheme(LightTheme)
	if got := paint(); !containsStr(got, "light") {
		t.Errorf("切换后应重绘为 light: %q", got)
	}
	SetTheme(DarkTheme)
}

func TestThemeProvider(t *testing.T) {
	SetTheme(DarkTheme)
	var inside *gxscene.Instance
	prov := ThemeProvider(gxscene.Props{
		"value": LightTheme,
		"children": func() any {
			use := UseTheme()
			inside = gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
				el("span"), dynChild(0), closeEl(),
			}, N: 1}, func() any { return use().Name })
			return inside
		},
	})
	if got := (gxscene.TextBackend{}).Paint(inside.Root()); !containsStr(got, "light") {
		t.Errorf("子树内应读 light 主题, got %q", got)
	}
	if CurrentTheme().Name != "dark" {
		t.Errorf("ThemeProvider 不应改全局主题（子树隔离）, got %q", CurrentTheme().Name)
	}
	if findByGxRole(prov.Root(), "context-provider") == nil {
		t.Error("缺少 context-provider 容器")
	}
	SetTheme(DarkTheme)
}

// TestSpaceTokenScale 验证间距标度全量入 Theme（space1–space8），两套主题同值。
func TestSpaceTokenScale(t *testing.T) {
	for _, th := range []Theme{DarkTheme, LightTheme} {
		for _, tt := range []struct{ key, want string }{
			{"space-1", Space1}, {"space-2", Space2}, {"space-3", Space3}, {"space-4", Space4},
			{"space-5", Space5}, {"space-6", Space6}, {"space-8", Space8},
		} {
			if got := themeToken(th, tt.key); got != tt.want {
				t.Errorf("%s themeToken(%q) = %q want %q", th.Name, tt.key, got, tt.want)
			}
		}
	}
}

// TestShadowTokensPerTheme 验证阴影令牌按主题取值：浅色比深色更淡（HIG），
// 且 radius/font/height 令牌在两套主题都可读——非颜色令牌不再是静态常量专属。
func TestShadowTokensPerTheme(t *testing.T) {
	// 阴影 token 按 Apple HIG 重定（低 alpha / 大 blur / 近零 offset），具体值会
	// 随设计评审调整；断言跟随主题字段而非硬编码，避免 token 调参误报。
	if themeToken(LightTheme, "shadow-card") != LightTheme.ShadowCard {
		t.Errorf("浅色 shadow-card 未走 Theme 字段: %q", themeToken(LightTheme, "shadow-card"))
	}
	if themeToken(DarkTheme, "shadow-card") != DarkTheme.ShadowCard {
		t.Error("深色 shadow-card 未走 Theme 字段")
	}
	if themeToken(LightTheme, "shadow-card") == themeToken(DarkTheme, "shadow-card") {
		t.Error("明暗 shadow-card 不应同值")
	}
	for _, k := range []string{"radius-panel", "font-headline", "control-height", "row-height"} {
		if themeToken(DarkTheme, k) == "" || themeToken(LightTheme, k) == "" {
			t.Errorf("令牌 %q 未在主题中生效", k)
		}
	}
}

// TestUseTokenInsideProviderSubtree 验证非颜色令牌同样吃子树主题：
// Provider(浅色) 子树内 UseToken 读到浅色阴影；子树外全局仍为深色（隔离）。
func TestUseTokenInsideProviderSubtree(t *testing.T) {
	SetTheme(DarkTheme)
	var got string
	prov := ThemeProvider(gxscene.Props{
		"value": LightTheme,
		"children": func() any {
			got = UseToken("shadow-card")()
			return Text(gxscene.Props{"value": func() any { return "x" }, "size": FontBody})
		},
	})
	if prov == nil {
		t.Fatal("provider 为空")
	}
	if got != LightTheme.ShadowCard {
		t.Fatalf("子树内 shadow-card=%q want 浅色 %q", got, LightTheme.ShadowCard)
	}
	if UseToken("shadow-card")() != DarkTheme.ShadowCard {
		t.Error("子树外不应受 Provider 影响（子树隔离）")
	}
	SetTheme(DarkTheme)
}
