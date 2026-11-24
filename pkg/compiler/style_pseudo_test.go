package compiler

import (
	"strings"
	"testing"
)

// 受限样式表：带引号 key 的 JSON 对象字面量（含 &:hover/$:active 伪状态）
// 编译为 gxscene.Style{...} 复合字面量（含嵌套伪状态子表），不再报 attr-unsupported。
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
	// JSON 形态与驼峰形态统一编译为 gxscene.Style{...}，伪状态递归为子 Style。
	if !strings.Contains(res.GoSource, `"bg":"#fff"`) {
		t.Errorf("应生成 gxscene.Style 复合字面量:\n%s", res.GoSource)
	}
	for _, want := range []string{`"&:hover":gxscene.Style{`, `"$:active":gxscene.Style{`} {
		if !strings.Contains(res.GoSource, want) {
			t.Errorf("伪状态 %q 应编译为嵌套 Style:\n%s", want, res.GoSource)
		}
	}
}

// key 无引号的驼峰对象字面量现在编译为 gxscene.Style{...}（react-native 风格），
// 不再报诊断——历史行为（报 attr-unsupported）已被新特性取代。
func TestStyleObjectLiteralNonJSONCompiles(t *testing.T) {
	res, err := CompileScene("style.gx", "package main\nfunc main() {\n _ = <div style={{ color: \"red\", fontSize: 12 }}>x</div>\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range res.Analysis.Diagnostics() {
		if d.Code == "attr-unsupported" && strings.Contains(d.Message, "style") {
			t.Errorf("驼峰 style 对象字面量不应再报诊断: %v", d)
		}
	}
	if !strings.Contains(res.GoSource, `gxscene.Style{"color":"red","font-size":12`) {
		t.Errorf("应生成 gxscene.Style 复合字面量:\n%s", res.GoSource)
	}
}
