package compiler

import (
	"strings"
	"testing"
)

// A4：<For empty={...}> 被静默丢弃 → 应报 attr-unsupported 并提示条件分支写法。
func TestForEmptyAttrDiagnostic(t *testing.T) {
	src := "package main\nfunc main(){ rows := []int{1}; _ = <ul><For each={rows} empty={<li>none</li>}>{(r)=><li>{r}</li>}</For></ul> }\n"
	res, err := CompileScene("a4.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range res.Analysis.Diagnostics() {
		if d.Code == "attr-unsupported" && strings.Contains(d.Message, "empty") && strings.Contains(d.Message, "if") {
			found = true
			if d.Pos.Line < 1 || d.Pos.Col < 1 {
				t.Errorf("诊断须带 1 基坐标: %+v", d.Pos)
			}
		}
	}
	if !found {
		t.Errorf("应报 <For empty> 不支持诊断, got %v", res.Analysis.Diagnostics())
	}
}

// A5：高置信拼错属性应报 attr-unknown——bind:value 冒号前缀、onClik 事件手误。
func TestUnknownAttrDiagnostic(t *testing.T) {
	cases := []struct {
		name, jsx, want string
	}{
		{"bind前缀", `<input bind:value={n}/>`, "冒号前缀"},
		{"事件手误", `<button onClik={f}>x</button>`, "onclick"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := "package main\nfunc f(){}\nfunc main(){ n := \"x\"; _ = " + c.jsx + " }\n"
			res, err := CompileScene("a5.gx", src)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, d := range res.Analysis.Diagnostics() {
				if d.Code == "attr-unknown" && strings.Contains(strings.ToLower(d.Message), strings.ToLower(c.want)) {
					found = true
				}
			}
			if !found {
				t.Errorf("应报 attr-unknown 含 %q, got %v", c.want, res.Analysis.Diagnostics())
			}
		})
	}
}

// 守护：合法属性（含后端自定义属性名、on* 表外事件、data-*）不报 attr-unknown。
func TestUnknownAttrNoFalsePositive(t *testing.T) {
	src := "package main\nfunc f(){}\nfunc main(){ _ = <div tone=\"cyan\" data-id=\"x\" onCustomThing={f} class=\"a\">x</div> }\n"
	res, err := CompileScene("a5ok.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range res.Analysis.Diagnostics() {
		if d.Code == "attr-unknown" || d.Code == "attr-unsupported" {
			t.Errorf("合法/自定义属性不应报诊断: %s", d.Message)
		}
	}
}
