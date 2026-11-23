package gxscene

import (
	"strings"
	"testing"
)

func TestStyleValidateWhitelist(t *testing.T) {
	ok := Style{"bg": "#fff", "&:hover": Style{"bg": "#000"}}
	if err := ok.Validate(); err != nil {
		t.Fatalf("合法样式表被拒: %v", err)
	}
	bad := Style{"colr": "#fff"} // 拼错
	if err := bad.Validate(); err == nil {
		t.Fatal("未知属性应被拒绝")
	}
	badPseudo := Style{"&:visitd": Style{"bg": "#000"}}
	if err := badPseudo.Validate(); err == nil {
		t.Fatal("未知伪状态应被拒绝")
	}
	badPseudoVal := Style{"&:hover": "#fff"} // 值非样式表
	if err := badPseudoVal.Validate(); err == nil {
		t.Fatal("伪状态值为非样式表应被拒绝")
	}
}

func TestStylePropsCanonicalizesPseudo(t *testing.T) {
	s := Style{
		"bg":       "#fff",
		"$:hover":  Style{"bg": "#eee"}, // 别名
		"&:active": Style{"bg": "#ddd"},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	p := s.Props()
	if p["bg"] != "#fff" {
		t.Errorf("普通属性直出: got %v", p["bg"])
	}
	if _, legacy := p["$:hover"]; legacy {
		t.Error("$:hover 应被规整，不应残留在产物")
	}
	hover, ok := p["&:hover"]
	if !ok {
		t.Fatal("缺少规整后的 &:hover")
	}
	if !strings.Contains(hover.(string), `"bg":"#eee"`) {
		t.Errorf("hover 子表编码错误: %v", hover)
	}
	active, ok := p["&:active"]
	if !ok || !strings.Contains(active.(string), `"bg":"#ddd"`) {
		t.Errorf("active 子表编码错误: %v", active)
	}
}

func TestStyleFlatten(t *testing.T) {
	s := Style{
		"bg":       "#fff",
		"color":    "#000",
		"&:hover":  Style{"bg": "#eee"},
		"&:active": Style{"bg": "#ddd", "opacity": "0.9"},
	}
	base := s.Flatten()
	if base["bg"] != "#fff" || len(base) != 2 {
		t.Errorf("无状态应为基础属性: %v", base)
	}
	hover := s.Flatten("hover")
	if hover["bg"] != "#eee" || hover["color"] != "#000" {
		t.Errorf("hover 覆盖错误: %v", hover)
	}
	active := s.Flatten("hover", "active")
	if active["bg"] != "#ddd" || active["opacity"] != "0.9" {
		t.Errorf("active（后来者）应覆盖: %v", active)
	}
	// $: 别名也能被 Flatten 识别
	alias := Style{"bg": "#fff", "$:hover": Style{"bg": "#111"}}
	if got := alias.Flatten("hover")["bg"]; got != "#111" {
		t.Errorf("别名 Flatten: got %v", got)
	}
}

func TestStyleParseStyle(t *testing.T) {
	src := `{"bg":"#fff","&:hover":{"bg":"#2563eb"},"&:active":{"bg":"#1d4ed8"}}`
	s, err := ParseStyle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 3 {
		t.Fatalf("应有 3 个 key: %v", s)
	}
	sub, ok := s["&:hover"].(Style)
	if !ok || sub["bg"] != "#2563eb" {
		t.Errorf("hover 子表解析错误: %v", s["&:hover"])
	}
	// 未知属性应报错
	if _, err := ParseStyle(`{"colr":"#fff"}`); err == nil {
		t.Error("ParseStyle 未知属性应报错")
	}
	// 非法 JSON
	if _, err := ParseStyle(`{`); err == nil {
		t.Error("ParseStyle 非法 JSON 应报错")
	}
}

func TestStyleFocusDisabled(t *testing.T) {
	s := Style{
		"bg":         "#3b82f6",
		"&:focus":    Style{"border": "#2563eb"},
		"&:disabled": Style{"bg": "#9ca3af", "opacity": "0.6"},
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("focus/disabled 应通过校验: %v", err)
	}
	p := s.Props()
	if _, ok := p[PseudoFocus]; !ok {
		t.Error("focus 子表应编码进 props")
	}
	if _, ok := p[PseudoDisabled]; !ok {
		t.Error("disabled 子表应编码进 props")
	}
	// Flatten 顺序：focus 先、disabled 后（后者覆盖同名 bg）。
	both := s.Flatten("focus", "disabled")
	if both["bg"] != "#9ca3af" || both["border"] != "#2563eb" || both["opacity"] != "0.6" {
		t.Errorf("focus+disabled 展平错误: %v", both)
	}
	// ParseStyle 也能解析。
	parsed, err := ParseStyle(`{"bg":"#fff","&:focus":{"border":"#00f"},"&:disabled":{"bg":"#999"}}`)
	if err != nil || len(parsed) != 3 {
		t.Errorf("ParseStyle focus/disabled 失败: %v %v", parsed, err)
	}
}

func TestPseudoStateFromProps(t *testing.T) {
	s := Style{"bg": "#fff", "&:hover": Style{"bg": "#eee"}}
	sub, ok := PseudoStateFromProps(map[string]any(s.Props()), "hover")
	if !ok || sub["bg"] != "#eee" {
		t.Errorf("从 props 读回 hover 子表失败: %v %v", sub, ok)
	}
	// $: 别名查询
	if _, ok := PseudoStateFromProps(map[string]any(s.Props()), "$:hover"); !ok {
		t.Error("$:hover 别名查询应成功")
	}
}
