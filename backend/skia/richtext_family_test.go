package skia

import (
	"testing"

	sk "github.com/robertpanvip/gox-skia/compat"

	"github.com/robertpanvip/gox-core/widgets"
	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
)

// TestRichTextStyleKeepsMonospaceFamily 锁定富文本绘制的字体口径：
// 编辑器代码行带 font-family=等宽栈，richTextStyle 必须解析出与测量端
// MeasureEditorAdvances 一致的族名，使 splitRuns 用等宽字体绘制。
//
// 回归背景：paintElement 曾用 textStyle{color,fontSize,bold} 构造样式，
// 漏传 family → splitRuns 退回 sans-serif 用比例字体绘制（空格 4.13px），
// 而测量端用等宽字体测宽（7.83px）——两端字体不一致，导致点击 <div 的 v
// 后空格插到 div 前面（点击定位偏移）。
func TestRichTextStyleKeepsMonospaceFamily(t *testing.T) {
	stack := widgets.EditorCodeFont()
	n := &gxscene.Node{
		Type: gxscene.NodeDiv,
		Props: map[string]any{
			"font-family": stack,
			"font-size":   13,
		},
	}
	st := richTextStyle(n, 0xffffffff, 13, false)

	wantFamily := pickFontFamily(stack)
	if st.family != wantFamily {
		t.Fatalf("富文本 textStyle.family = %q, want %q（漏传 family 会退回 sans-serif）", st.family, wantFamily)
	}
	if st.family == "" {
		t.Fatal("富文本 textStyle.family 不应为空——空串会让绘制用比例字体，与测量端等宽字体不一致")
	}

	// 用解析出的 family 测空格宽度：必须等于等宽 advance（≈7.83），
	// 而不是比例字体的 4.13。
	spaceW := 0.0
	for _, r := range splitRuns("    ", st.fontSize, st.bold, st.family) {
		if !r.font.IsNil() {
			spaceW += float64(r.font.MeasureText([]byte(r.text), uint32(len(r.text)), sk.TextEncodingUTF8, nil))
		}
	}
	per := spaceW / 4
	narrow, _ := MeasureEditorAdvances(stack, 13)
	if diff := per - narrow; diff > 0.3 || diff < -0.3 {
		t.Fatalf("富文本空格宽 %.4f 与测量端等宽 %.4f 不符（差 %.4f）——绘制与命中会用不同字体", per, narrow, diff)
	}
}
