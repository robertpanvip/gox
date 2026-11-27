package widgets

import "testing"

// TestWebstormHairlineSoftened 像素评审结论：参照图面板分隔几乎不可见，
// 两主题 hairline 各减淡一档（dark 12%->8% 白，light 8%->5% 黑）。
func TestWebstormHairlineSoftened(t *testing.T) {
	if AppleHairline != "#ffffff14" {
		t.Errorf("dark hairline 应=#ffffff14(8%%), got %s", AppleHairline)
	}
	if LightHairline != "#0000000d" {
		t.Errorf("light hairline 应=#0000000d(5%%), got %s", LightHairline)
	}
}
