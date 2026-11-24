package compiler

import (
	"strings"
	"testing"
)

// 受限样式表：带引号 key 的 JSON 对象字面量（伪状态用单词 key hover/active）
// 同样编译为 gxscene.Style 结构体字面量（伪状态映射到 Hover/Active 字段），
// 不再报 attr-unsupported。源码不写 "&:" 前缀。
func TestStyleObjectLiteralJSONCompiles(t *testing.T) {
	src := "package main\nfunc main() {\n _ = <div style={{\"bg\":\"#fff\",\"hover\":{\"bg\":\"#eee\"},\"active\":{\"bg\":\"#ddd\"}}}>x</div>\n}\n"
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
	for _, want := range []string{`Bg:"#fff"`, `Hover:&gxscene.Style{`, `Active:&gxscene.Style{`} {
		if !strings.Contains(res.GoSource, want) {
			t.Errorf("JSON 形态应映射到字段 %q:\n%s", want, res.GoSource)
		}
	}
}

// key 无引号的驼峰对象字面量编译为 gxscene.Style{...} 结构体字面量，
// 伪状态用单词 key（hover）映射到 Hover 字段。
func TestStyleObjectLiteralNonJSONCompiles(t *testing.T) {
	res, err := CompileScene("style.gx", "package main\nfunc main() {\n _ = <div style={{ color: \"red\", fontSize: 12, hover: { bg: \"#eee\" } }}>x</div>\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range res.Analysis.Diagnostics() {
		if d.Code == "attr-unsupported" && strings.Contains(d.Message, "style") {
			t.Errorf("驼峰 style 对象字面量不应再报诊断: %v", d)
		}
	}
	for _, want := range []string{`Color:"red"`, `FontSize:"12"`, `Hover:&gxscene.Style{`} {
		if !strings.Contains(res.GoSource, want) {
			t.Errorf("应生成结构体字面量含 %q:\n%s", want, res.GoSource)
		}
	}
}
