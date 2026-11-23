package compiler

import (
	"strings"
	"testing"
)

// 受限样式表：带引号 key 的 JSON 对象字面量（含 &:hover/$:active 伪状态）
// 应被改写成 gxscene.parseStyle(...)，不再报 attr-unsupported。
func TestStyleObjectLiteralJSONCompiles(t *testing.T) {
	src := "package main\nfunc main() {\n _ = <div style={{\"bg\":\"#fff\",\"&:hover\":{\"bg\":\"#eee\"},\"$:active\":{\"bg\":\"#ddd\"}}}>x</div>\n}\n"
	res, err := Compile("style.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range res.Analysis.Diagnostics() {
		if d.Code == "attr-unsupported" {
			t.Fatalf("合法 JSON style 不应报 attr-unsupported: %s", d.Message)
		}
	}
	if err := res.Validate(); err != nil {
		t.Fatalf("生成的 Go 无效: %v\n%s", err, res.GoSource)
	}
	if !strings.Contains(strings.ToLower(res.GoSource), "parsestyle") {
		t.Errorf("应生成 parseStyle 调用:\n%s", res.GoSource)
	}
}

// key 无引号的非 JSON 对象字面量仍报诊断（历史行为保持，避免匿名 struct 静默错值）。
func TestStyleObjectLiteralNonJSONStillDiagnosed(t *testing.T) {
	res, err := CompileScene("style.gx", "package main\nfunc main() {\n _ = <div style={{ color: \"red\" }}>x</div>\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range res.Analysis.Diagnostics() {
		if d.Code == "attr-unsupported" && strings.Contains(d.Message, "style") {
			found = true
		}
	}
	if !found {
		t.Errorf("非 JSON 的 style 对象字面量仍应报诊断: %v", res.Analysis.Diagnostics())
	}
}
