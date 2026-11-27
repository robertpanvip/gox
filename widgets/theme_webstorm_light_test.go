package widgets

import "testing"

// TestLightThemeWebstormBaselines 像素评审结论落地：官方 WebStorm light 截图
// 顶栏/状态栏/工具窗/编辑区留白实测众数色 #eeede9（62~100% 占比），
// 深色一档用于输入槽底色。锁死这些基线，防止回退成纯白 Apple 底。
func TestLightThemeWebstormBaselines(t *testing.T) {
	if LightTheme.Bg != "#eeede9" {
		t.Errorf("窗口底应=参照实测 #eeede9, got %s", LightTheme.Bg)
	}
	if LightTheme.Panel != "#eeede9" {
		t.Errorf("工具窗/顶栏底应=参照实测 #eeede9, got %s", LightTheme.Panel)
	}
	if LightTheme.GroupedBg != "#e3e2de" {
		t.Errorf("次级底(输入槽/斑马)应比 chrome 深一档 #e3e2de, got %s", LightTheme.GroupedBg)
	}
	if LightTheme.PanelAlt != "#e3e2de" {
		t.Errorf("PanelAlt 应与 GroupedBg 同档, got %s", LightTheme.PanelAlt)
	}
	if LightTheme.EditorBg != "#ffffff" {
		t.Errorf("代码面板保持白底(参照图编辑区为白色块), got %s", LightTheme.EditorBg)
	}
	if LightTheme.InputBg != "#ffffff" {
		t.Errorf("输入槽内底应为白(与 #eeede9 chrome 区分), got %s", LightTheme.InputBg)
	}
}
