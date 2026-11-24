package compiler

import (
	"strings"
	"testing"
)

// compileStyleJSX 编译一个含 style={{...}} 的最小 GX 片段，返回产物与诊断。
func compileStyleJSX(t *testing.T, styleExpr string) *Result {
	t.Helper()
	src := "package main\n" +
		"import gxscene \"github.com/robertpanvip/gox/runtime/scene\"\n" +
		"func main() {\n" +
		"    idx := 0\n" +
		"    lineGutterColor := func(i int) string { return \"#fff\" }\n" +
		"    _ = <div " + styleExpr + ">x</div>\n" +
		"}\n"
	res, err := CompileScene("style.gx", src)
	if err != nil {
		t.Fatalf("编译失败: %v\n源码:\n%s", err, src)
	}
	return res
}

func assertNoDiag(t *testing.T, res *Result) {
	t.Helper()
	if res.InvalidGo {
		t.Fatalf("产物被抑制（InvalidGo）")
	}
	if ds := res.Analysis.Diagnostics(); len(ds) > 0 {
		t.Fatalf("不应有诊断，得到: %v", ds)
	}
	if err := res.Validate(); err != nil {
		t.Fatalf("产物不是合法 Go: %v\n%s", err, res.GoSource)
	}
}

func TestStyleObjCamelKeysAndValues(t *testing.T) {
	res := compileStyleJSX(t, `style={{background: lineGutterColor(idx), color: "#858585", width: 42, fontSize: 12, textAlign: "right", padding: 4}}`)
	assertNoDiag(t, res)
	for _, want := range []string{
		`Background:lineGutterColor(idx)`,
		`Color:"#858585"`,
		`Width:"42"`,
		`FontSize:"12"`,
		`TextAlign:"right"`,
		`Padding:"4"`,
		"gxscene.Style{",
	} {
		if !strings.Contains(res.GoSource, want) {
			t.Errorf("产物缺少 %q\n%s", want, res.GoSource)
		}
	}
	// 旧 map 形态的 kebab 字符串 key 不应再出现。
	for _, bad := range []string{`"font-size"`, `"text-align"`, `"background":`} {
		if strings.Contains(res.GoSource, bad) {
			t.Errorf("产物残留 map 形态 %q\n%s", bad, res.GoSource)
		}
	}
}

func TestStyleObjPseudoNested(t *testing.T) {
	// 伪状态用单词 key（hover），值对象递归成 Hover: &gxscene.Style{...}。
	res := compileStyleJSX(t, `style={{hover: {bg: "#eee", fontSize: 14}, bg: "#fff"}}`)
	assertNoDiag(t, res)
	for _, want := range []string{
		`Hover:&gxscene.Style{`,
		`Bg:"#eee"`,
		`FontSize:"14"`,
		`Bg:"#fff"`,
	} {
		if !strings.Contains(res.GoSource, want) {
			t.Errorf("产物缺少 %q\n%s", want, res.GoSource)
		}
	}
}

func TestStyleObjEmpty(t *testing.T) {
	res := compileStyleJSX(t, `style={{}}`)
	assertNoDiag(t, res)
	if !strings.Contains(res.GoSource, "gxscene.Style{}") {
		t.Errorf("空对象应生成 gxscene.Style{}\n%s", res.GoSource)
	}
}

func TestStyleObjJSONCompatible(t *testing.T) {
	// 带引号 key 的 JSON 写法同样映射到强类型 struct 字段；伪状态用单词 key。
	res := compileStyleJSX(t, `style={{"bg":"#fff","hover":{"bg":"#eee"}}}`)
	assertNoDiag(t, res)
	for _, want := range []string{`Bg:"#fff"`, `Hover:&gxscene.Style{`, `Bg:"#eee"`} {
		if !strings.Contains(res.GoSource, want) {
			t.Errorf("JSON 写法应映射到字段 %q:\n%s", want, res.GoSource)
		}
	}
}

func TestStyleObjAmpPrefixRejected(t *testing.T) {
	// 第三种写法：源码只认单词 key，写 "&:hover" 报 style-unknown-prop，不做前缀转译。
	src := "package main\nfunc main() {\n _ = <div style={{\"&:hover\":{\"bg\":\"#eee\"}}}>x</div>\n}\n"
	res, err := CompileScene("style.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range res.Analysis.Diagnostics() {
		if d.Code == "style-unknown-prop" {
			found = true
		}
	}
	if !found {
		t.Errorf("源码写 & 前缀应报 style-unknown-prop，得到 %v", res.Analysis.Diagnostics())
	}
}

func TestStyleKeyCamelToKebab(t *testing.T) {
	cases := map[string]string{
		"fontSize":     "font-size",
		"textAlign":    "text-align",
		"borderRadius": "border-radius",
		"background":   "background",
		"bg":           "bg",
		"color":        "color",
		"font-size":    "font-size",
		"lineHeight":   "line-height",
		"zIndex":       "z-index",
	}
	for in, want := range cases {
		if got := styleKeyCamelToKebab(in); got != want {
			t.Errorf("styleKeyCamelToKebab(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStyleObjUnknownProp(t *testing.T) {
	// 未知属性应报诊断，不静默生成非法字段。
	src := "package main\nfunc main() {\n _ = <div style={{ colr: \"red\" }}>x</div>\n}\n"
	res, err := CompileScene("style.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range res.Analysis.Diagnostics() {
		if d.Code == "style-unknown-prop" {
			found = true
		}
	}
	if !found {
		t.Errorf("未知属性应报 style-unknown-prop，得到 %v", res.Analysis.Diagnostics())
	}
}

func TestStyleObjClassStillRejected(t *testing.T) {
	// class 的对象字面量仍应报错（style 才支持对象字面量）。
	src := "package main\nfunc main() {\n _ = <div class={{ active: true }}>x</div>\n}\n"
	res, err := CompileScene("class.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range res.Analysis.Diagnostics() {
		if d.Code == "attr-unsupported" && strings.Contains(d.Message, "class") {
			found = true
		}
	}
	if !found {
		t.Errorf("class 对象字面量应报 attr-unsupported，得到 %v", res.Analysis.Diagnostics())
	}
}
