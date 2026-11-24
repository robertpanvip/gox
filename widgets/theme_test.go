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
