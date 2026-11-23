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
	prov := ThemeProvider(gxscene.Props{
		"value": LightTheme,
		"children": func() any {
			return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
				el("span"), gxscene.Op{Code: gxscene.OpChildText, Arg: "content"}, closeEl(),
			}})
		},
	})
	if CurrentTheme().Name != "light" {
		t.Errorf("ThemeProvider 应写入 light, got %q", CurrentTheme().Name)
	}
	if findByGxRole(prov.Root(), "context-provider") == nil {
		t.Error("缺少 context-provider 容器")
	}
	SetTheme(DarkTheme)
}
