package compiler

import (
	"strings"
	"testing"
)

// TestJSXSceneRefOpcode 验证 ref={fn} 在 jsxscene 阶段编成 OpRef 插槽，
// 而不是普通 OpDynAttr（ref 不是节点属性，是命令式句柄回调）。
func TestJSXSceneRefOpcode(t *testing.T) {
	out, err := CompileStage(`package main
func onInput(el *gxscene.Element) { el.Focus() }
func view() any {
	return <div ref={onInput} class="box">hi</div>
}
`, "jsxscene")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"OpRef", "Slot:0",
		"OpAttr", `Key:"class"`, // class 仍是普通静态属性
		"N:1", // 只有 ref 占一个插槽，class 不占
		"func() any{ return onInput }",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物应包含 %q, got:\n%s", want, out)
		}
	}
	// ref 不能漏成动态属性。
	if strings.Contains(out, `Key:"ref"`) {
		t.Errorf("ref 不应编成 OpDynAttr，got:\n%s", out)
	}
}

// TestRefLowercaseMethodRestored 验证 GX 侧按规范用小写写 Element 方法，
// 完整管线经 transformExternal 还原为首字母大写的 Go 方法；类型名保持大写。
func TestRefLowercaseMethodRestored(t *testing.T) {
	res, err := Compile(`ref_lower.gx`, `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"

func onInput(el *gxscene.Element) {
	el.focus()
	r := el.boundingClientRect()
	_ = r
	el.measure(func(w, h float32) {})
}

func view() any {
	return <input ref={onInput} />
}
`)
	if err != nil {
		t.Fatal(err)
	}
	out := res.GoSource
	for _, want := range []string{
		"el.Focus()",
		"el.BoundingClientRect()",
		"el.Measure(func(w, h float32) {})",
		"gxscene.OpRef",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物应包含 %q, got:\n%s", want, out)
		}
	}
	// 小写形态不应残留。
	for _, bad := range []string{"el.focus()", "el.boundingClientRect()", "el.measure("} {
		if strings.Contains(out, bad) {
			t.Errorf("小写方法名应被还原，却残留 %q, got:\n%s", bad, out)
		}
	}
}

// TestRefAnySignature 验证未注解的 func(any) 形态回调同样被接受（兼容 GX 侧
// 不写参数类型的高级用法），且 ref 插槽编号与其它动态插槽连续。
func TestRefAnySignature(t *testing.T) {
	out, err := CompileStage(`package main
func grab(e any) {}
func view(title string) any {
	return <div>{title}<span ref={grab}>x</span></div>
}
`, "jsxscene")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"gxscene.OpDynChild", "Slot:0",
		"gxscene.OpRef", "Slot:1",
		"N:2",
		"func() any{ return grab }",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物应包含 %q, got:\n%s", want, out)
		}
	}
}
