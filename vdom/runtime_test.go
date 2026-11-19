package gxvdom

import "testing"

func TestRender(t *testing.T) {
	node := CreateElement("div", map[string]any{"class": "box", "disabled": true}, "hi ", "there")
	got := Render(node)
	want := `<div class="box" disabled>hi there</div>`
	if got != want {
		t.Fatalf("Render = %q, want %q", got, want)
	}
}

func TestDiff(t *testing.T) {
	a := CreateElement("div", map[string]any{"class": "box"}, "hello")
	b := CreateElement("div", map[string]any{"class": "card"}, "world", "!")
	patches := Diff(a, b)

	seen := map[PatchOp]int{}
	for _, p := range patches {
		seen[p.Op]++
		if p.Op == OpSetProp && p.Key != "class" {
			t.Errorf("set-prop 的 key 应为 class，实际 %q", p.Key)
		}
		if p.Op == OpSetText && (len(p.Path) != 1 || p.Path[0] != 0) {
			t.Errorf("set-text 路径应为 [0]，实际 %v", p.Path)
		}
	}
	if seen[OpSetProp] != 1 || seen[OpSetText] != 1 || seen[OpAppend] != 1 {
		t.Fatalf("期望 set-prop/set-text/append 各一条，实际 %+v", seen)
	}
}
