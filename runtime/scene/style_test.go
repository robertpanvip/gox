package gxscene

import (
	"strings"
	"testing"
)

func TestStyleValidateWhitelist(t *testing.T) {
	ok := Style{Bg: "#fff", Hover: &Style{Bg: "#000"}}
	if err := ok.Validate(); err != nil {
		t.Fatalf("合法样式表被拒: %v", err)
	}
	// 字段固定后拼错的 key 在 Go 编译期就报错；Validate 只校验动态构造的
	// 伪状态子表。这里验证动态路径（ParseStyle）仍拒绝未知属性/伪状态/错值。
	if _, err := ParseStyle(`{"colr":"#fff"}`); err == nil {
		t.Fatal("未知属性应被拒绝")
	}
	if _, err := ParseStyle(`{"visitd":{"bg":"#000"}}`); err == nil {
		t.Fatal("未知伪状态应被拒绝")
	}
	if _, err := ParseStyle(`{"&:hover":{"bg":"#000"}}`); err == nil {
		t.Fatal("JSON 输入不应接受 & 前缀，只认单词 key")
	}
}

func TestStylePropsCanonicalizesPseudo(t *testing.T) {
	s := Style{
		Bg:     "#fff",
		Hover:  &Style{Bg: "#eee"},
		Active: &Style{Bg: "#ddd"},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	p := s.Props()
	if p["bg"] != "#fff" {
		t.Errorf("普通属性直出: got %v", p["bg"])
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

func TestStyleSelectionPseudoElement(t *testing.T) {
	s := Style{
		Bg:        "#fff",
		Selection: &Style{Bg: "#b4d5fe", Color: "#fff"},
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("selection 伪元素子表应通过校验: %v", err)
	}
	p := s.Props()
	// 伪元素用双冒号独立协议键，不占用 &:selection 伪状态。
	sel, ok := p[PseudoSelection]
	if !ok || !strings.Contains(sel.(string), `"bg":"#b4d5fe"`) {
		t.Fatalf("selection 子表未按 &::selection 编码: %v", p)
	}
	if _, wrong := p["&:selection"]; wrong {
		t.Error("selection 不应编码成伪状态 &:selection")
	}
	// 伪元素不参与 Flatten 状态覆盖：传 "selection" 也不改变基础样式。
	flat := s.Flatten("selection")
	if flat["bg"] != "#fff" {
		t.Errorf("伪元素不应被当状态覆盖: got %v", flat["bg"])
	}
	// 动态构造路径接受单词 key。
	parsed, err := ParseStyle(`{"selection":{"bg":"#264f78","color":"#fff"}}`)
	if err != nil || parsed.Selection == nil || parsed.Selection.Bg != "#264f78" {
		t.Errorf("ParseStyle selection 失败: %+v %v", parsed, err)
	}
}

func TestStyleFontFamily(t *testing.T) {
	// font-family 是标量属性（含逗号的字体栈），反射编码应原样写入后端 key。
	stack := "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace"
	s := Style{FontFamily: stack, FontSize: "13"}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	p := s.Props()
	if p["font-family"] != stack {
		t.Errorf("font-family 编码错误: got %v", p["font-family"])
	}
	if p["font-size"] != "13" {
		t.Errorf("font-size 编码错误: got %v", p["font-size"])
	}
	// 动态构造路径（ParseStyle）也接受该键。
	parsed, err := ParseStyle(`{"font-family":"monospace"}`)
	if err != nil || parsed.FontFamily != "monospace" {
		t.Errorf("ParseStyle font-family 失败: %+v %v", parsed, err)
	}
}

func TestStyleFlatten(t *testing.T) {
	s := Style{
		Bg:     "#fff",
		Color:  "#000",
		Hover:  &Style{Bg: "#eee"},
		Active: &Style{Bg: "#ddd", Opacity: "0.9"},
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
	// &: 前缀与 $: 别名在 Flatten 入参侧都能识别。
	if got := s.Flatten("&:hover")["bg"]; got != "#eee" {
		t.Errorf("&:hover 前缀 Flatten: got %v", got)
	}
	if got := s.Flatten("$:hover")["bg"]; got != "#eee" {
		t.Errorf("$:hover 别名 Flatten: got %v", got)
	}
}

func TestStyleParseStyle(t *testing.T) {
	// JSON 输入同理：伪状态用单词 key，后端协议 "&:" 只在 Props 输出侧出现。
	src := `{"bg":"#fff","hover":{"bg":"#2563eb"},"active":{"bg":"#1d4ed8"}}`
	s, err := ParseStyle(src)
	if err != nil {
		t.Fatal(err)
	}
	if s.Bg != "#fff" || s.Hover == nil || s.Hover.Bg != "#2563eb" || s.Active == nil || s.Active.Bg != "#1d4ed8" {
		t.Fatalf("ParseStyle 解析错误: %+v", s)
	}
	// 未知属性应报错
	if _, err := ParseStyle(`{"colr":"#fff"}`); err == nil {
		t.Error("ParseStyle 未知属性应报错")
	}
	// 非法 JSON
	if _, err := ParseStyle(`{`); err == nil {
		t.Error("ParseStyle 非法 JSON 应报错")
	}
	// 驼峰 key 动态构造也应映射到字段。
	camel, err := ParseStyle(`{"fontSize":"12","hover":{"bg":"#eee"}}`)
	if err != nil || camel.FontSize != "12" || camel.Hover == nil {
		t.Errorf("ParseStyle 驼峰/伪状态单词失败: %+v %v", camel, err)
	}
}

func TestStyleFocusDisabled(t *testing.T) {
	s := Style{
		Bg:       "#3b82f6",
		Focus:    &Style{Border: "#2563eb"},
		Disabled: &Style{Bg: "#9ca3af", Opacity: "0.6"},
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
	// ParseStyle 也能解析（伪状态单词 key）。
	parsed, err := ParseStyle(`{"bg":"#fff","focus":{"border":"#00f"},"disabled":{"bg":"#999"}}`)
	if err != nil || parsed.Focus == nil || parsed.Disabled == nil {
		t.Errorf("ParseStyle focus/disabled 失败: %+v %v", parsed, err)
	}
}

func TestPseudoStateFromProps(t *testing.T) {
	s := Style{Bg: "#fff", Hover: &Style{Bg: "#eee"}}
	sub, ok := PseudoStateFromProps(map[string]any(s.Props()), "hover")
	if !ok || sub.Bg != "#eee" {
		t.Errorf("从 props 读回 hover 子表失败: %+v %v", sub, ok)
	}
	// $: 别名查询
	if _, ok := PseudoStateFromProps(map[string]any(s.Props()), "$:hover"); !ok {
		t.Error("$:hover 别名查询应成功")
	}
}

// TestStyleFieldsMatchNames 守护 struct 元信息与 PascalCase 字段名一致。
func TestStyleFieldsMatchNames(t *testing.T) {
	fields, _ := styleMeta()
	for _, f := range fields {
		if f.pseudo == "" {
			continue
		}
		want := strings.ToUpper(f.pseudo[:1]) + f.pseudo[1:]
		if f.name != want {
			t.Errorf("伪状态 %q 字段名 %q，期望 %q", f.key, f.name, want)
		}
	}
	// 普通属性：kebab 名应从字段名 camel→kebab 还原。
	for _, f := range fields {
		if f.pseudo != "" {
			continue
		}
		if got := camelToKebab(f.name); got != f.key {
			// 首字母缩略（Bg/ZIndex 等）不做严格还原，仅校验非空。
			if got == "" {
				t.Errorf("字段 %q 的 kebab 名为空", f.name)
			}
		}
	}
}
