package widgets

import "testing"

// TestWebstormHairlineSoftened 实拍基线：面板分隔近不可见（靠色差），
// dark hairline 6% 白、light 5% 黑。上轮按官方 light 营销图定 8%，
// 本轮用户实拍（深色主题）证据更强，dark 档降至 6%。
func TestWebstormHairlineSoftened(t *testing.T) {
	if AppleHairline != "#ffffff0f" {
		t.Errorf("dark hairline 应=#ffffff0f, got %s", AppleHairline)
	}
	if LightHairline != "#0000000d" {
		t.Errorf("light hairline 应=#0000000d, got %s", LightHairline)
	}
}
