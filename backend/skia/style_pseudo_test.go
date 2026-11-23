package skia

import "testing"

// host 展平后的 props：普通属性 + &:hover/&:active 的 JSON 子表串。
func pseudoProps() map[string]any {
	return map[string]any{
		"bg":       "#ffffff",
		"color":    "#000000",
		"&:hover":  `{"bg":"#eeeeee","border-radius":"12"}`,
		"&:active": `{"bg":"#dddddd"}`,
	}
}

func TestParseStyleWithStateHover(t *testing.T) {
	base := parseStyle(pseudoProps())
	hover := parseStyleWithState(pseudoProps(), "hover")
	if base.bg == hover.bg {
		t.Error("hover 应改变 bg")
	}
	if hover.radius != 12 {
		t.Errorf("hover 子表新增的 border-radius 应生效, got %v", hover.radius)
	}
	if hover.hasColor == false || hover.color != base.color {
		t.Error("hover 未提及的 color 应保留")
	}
}

func TestParseStyleWithStateActive(t *testing.T) {
	hover := parseStyleWithState(pseudoProps(), "active")
	if hover.bg == parseStyle(pseudoProps()).bg {
		t.Error("active 应改变 bg")
	}
	if hover.radius != 0 {
		t.Error("active 子表未提及 border-radius，应保持默认 0")
	}
}

func TestApplyPseudoStyleDoesNotMutateInput(t *testing.T) {
	p := pseudoProps()
	origBg := p["bg"]
	_ = applyPseudoStyle(p, "hover")
	if p["bg"] != origBg {
		t.Error("applyPseudoStyle 不应修改入参")
	}
}

func TestParseStyleIgnoresPseudoKeysWithoutState(t *testing.T) {
	// 无状态时 &:/$: key 应被 parseStyle 安全忽略，不影响基础解析。
	s := parseStyle(pseudoProps())
	if s.hasBg == false {
		t.Error("基础 bg 应被解析")
	}
}
