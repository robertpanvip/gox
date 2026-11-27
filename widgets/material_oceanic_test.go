package widgets

import "testing"

// TestDarkThemeMatchesUserWebStormShot 对齐基准 = 用户真实环境截图
// （WebStorm + Material Oceanic，1728x1080，逐区像素众数）：
// 编辑器主底 #233239、侧栏/工具窗更沉 #1c272d、分组底 #18252b、
// 树选中块 #2c454a、青色 accent（tab 下划线实测 #1f8985~#26a69a 族）、
// hairline 近无界（实拍面板分隔靠色差非亮线）。
func TestDarkThemeMatchesUserWebStormShot(t *testing.T) {
	if DarkTheme.Bg != "#233239" {
		t.Errorf("编辑器底应=#233239, got %s", DarkTheme.Bg)
	}
	if DarkTheme.Panel != "#233239" {
		t.Errorf("chrome 应=#233239(与编辑器同带), got %s", DarkTheme.Panel)
	}
	if DarkTheme.GroupedBg != "#1c272d" {
		t.Errorf("侧栏底应=#1c272d, got %s", DarkTheme.GroupedBg)
	}
	if DarkTheme.PanelAlt != "#2c454a" {
		t.Errorf("选中块应=#2c454a, got %s", DarkTheme.PanelAlt)
	}
	if !isTealish(DarkTheme.Accent) {
		t.Errorf("accent 应为青色系, got %s", DarkTheme.Accent)
	}
	if DarkTheme.EditorBg != "#233239" {
		t.Errorf("编辑器 editorBg 应=#233239, got %s", DarkTheme.EditorBg)
	}
}

func isTealish(hex string) bool {
	if len(hex) != 7 || hex[0] != '#' {
		return false
	}
	dig := func(b byte) int {
		switch {
		case b >= '0' && b <= '9':
			return int(b - '0')
		case b >= 'a' && b <= 'f':
			return int(b-'a') + 10
		case b >= 'A' && b <= 'F':
			return int(b-'A') + 10
		}
		return -1
	}
	r := dig(hex[1])*16 + dig(hex[2])
	g := dig(hex[3])*16 + dig(hex[4])
	b := dig(hex[5])*16 + dig(hex[6])
	return g > r+40 && b > r+20
}
