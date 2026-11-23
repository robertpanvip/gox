package skia

import "testing"

// host 展平后的 props：普通属性 + 四种伪状态的 JSON 子表串。
func pseudoProps() map[string]any {
	return map[string]any{
		"bg":         "#ffffff",
		"color":      "#000000",
		"&:hover":    `{"bg":"#eeeeee","border-radius":"12"}`,
		"&:focus":    `{"border":"#0000ff"}`,
		"&:active":   `{"bg":"#dddddd"}`,
		"&:disabled": `{"bg":"#9ca3af","opacity":"0.6"}`,
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

func TestParseStyleWithStateFocus(t *testing.T) {
	base := parseStyle(pseudoProps())
	focus := parseStyleWithState(pseudoProps(), "focus")
	if focus.hasBorder == false || base.hasBorder == true {
		t.Error("focus 子表应新增 border，基础无 border")
	}
	if focus.bg != base.bg {
		t.Error("focus 未提及 bg，应保留基础值")
	}
}

func TestParseStyleWithStateDisabled(t *testing.T) {
	props := pseudoProps()
	props["disabled"] = true
	// 显式 state=hover 的同时节点禁用：disabled 子表应盖过 hover 的 bg。
	got := parseStyleWithState(props, "hover")
	if got.opacity != 0.6 {
		t.Errorf("disabled 子表 opacity 应生效, got %v", got.opacity)
	}
	// disabled 的 bg=#9ca3af 应盖过 hover 的 #eeeeee（两者都是子表覆盖，disabled 最后）。
	dis, _ := parseColor("#9ca3af")
	if got.bg != dis {
		t.Errorf("disabled 应盖过 hover 的 bg, got %08x want %08x", got.bg, dis)
	}
}

func TestDisabledPropStringTrue(t *testing.T) {
	props := pseudoProps()
	props["disabled"] = "1"
	got := parseStyleWithState(props, "")
	if got.opacity != 0.6 {
		t.Errorf("字符串 \"1\" 的 disabled 也应生效, got %v", got.opacity)
	}
	// "false" 不禁用。
	props["disabled"] = "false"
	got2 := parseStyleWithState(props, "hover")
	if got2.opacity != 1 {
		t.Errorf("disabled=false 不应套用禁用样式, got opacity %v", got2.opacity)
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
