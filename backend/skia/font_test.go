package skia

import (
	"testing"

	sk "github.com/robertpanvip/gox-skia/compat"
)

// TestResolveFontCJK：主字体 sans-serif 缺中文/日文字形，resolveFont 必须
// 选出能覆盖它们的 CJK fallback 字体；纯 ASCII 仍用主字体。
func TestResolveFontCJK(t *testing.T) {
	// CI runner 等环境可能没装任何 CJK 字体：届时 resolveFont 无字可选，
	// 跳过而不是误报回归（ HasCJKFont 测的是环境，不是代码）。
	if !HasCJKFont() {
		t.Skip("系统未安装 CJK 字体，无法验证 fallback 选择逻辑")
	}
	primary := resolveFont('a', 16, false, "")
	if primary.IsNil() {
		t.Fatal("主字体不应为 nil")
	}
	if !covers(primary, 'a') {
		t.Fatal("主字体应覆盖 ASCII 'a'")
	}
	if covers(primary, '中') {
		t.Fatal("测试前提：sans-serif 不应覆盖 '中'（否则本用例失去意义）")
	}

	zh := resolveFont('中', 16, false, "")
	if zh.IsNil() || !covers(zh, '中') {
		t.Fatal("resolveFont('中') 未选出覆盖中文字形的字体")
	}
	if zh == primary {
		t.Error("中文字体不应与主字体相同")
	}
	jp := resolveFont('日', 16, false, "")
	if jp.IsNil() || !covers(jp, '日') {
		t.Error("resolveFont('日') 未选出覆盖日文字形的字体")
	}
}

// TestSplitRunsMixed：中英混排切成多段，每段各自字体覆盖其文本，且拼接后
// 与原文一致（不丢字、不重字）。
func TestSplitRunsMixed(t *testing.T) {
	if !HasCJKFont() {
		t.Skip("系统未安装 CJK 字体，无法验证混排分Run逻辑")
	}
	text := "Hello中文World"
	runs := splitRuns(text, 16, false, "")
	if len(runs) < 3 {
		t.Fatalf("中英混排应至少 3 段（英/中/英），实际 %d 段", len(runs))
	}
	var rebuilt string
	for i, r := range runs {
		if r.font.IsNil() {
			t.Errorf("第 %d 段字体为 nil", i)
		}
		for _, ch := range r.text {
			if !covers(r.font, ch) {
				t.Errorf("第 %d 段字体不覆盖 %q", i, ch)
			}
		}
		rebuilt += r.text
	}
	if rebuilt != text {
		t.Errorf("分段拼接 = %q, want %q", rebuilt, text)
	}
}

// TestSplitRunsPureASCII：纯 ASCII 只有一段，且用主字体（零回退开销）。
func TestSplitRunsPureASCII(t *testing.T) {
	runs := splitRuns("PULSE 2026", 14, true, "")
	if len(runs) != 1 {
		t.Fatalf("纯 ASCII 应 1 段，实际 %d", len(runs))
	}
	if runs[0].text != "PULSE 2026" {
		t.Errorf("段落文本 = %q", runs[0].text)
	}
}

// TestMeasureMixedWidth：混排测量宽度 = 各段宽度之和。CJK 字符是全角，
// 两汉字宽度应接近 2×字号，证明 CJK 字体被真实用于测宽（而非 tofu 或漏测）。
func TestMeasureMixedWidth(t *testing.T) {
	m := skiaFontMeasurer()
	wMixed := m("中文", 16, false, "")
	if wMixed <= 0 {
		t.Fatalf("混排宽度应 > 0, got %v", wMixed)
	}
	// 全角汉字约等于字号宽度；允许 0.6~1.4 倍字号/字的合理区间。
	per := wMixed / 2
	if per < 16*0.6 || per > 16*1.4 {
		t.Errorf("单汉字宽度 = %.1f，超出全角合理区间 [%.1f, %.1f]", per, 16*0.6, 16*1.4)
	}
	// 混排总宽应等于各段宽度之和（绘制端按此口径累加落笔 x）。
	runs := splitRuns("中A文", 16, false, "")
	var sum float32
	for _, r := range runs {
		sum += r.font.MeasureText([]byte(r.text), uint32(len(r.text)), sk.TextEncodingUTF8, nil)
	}
	if got := m("中A文", 16, false, ""); got != sum {
		t.Errorf("混排总宽(%v) != 各段宽度和(%v)", got, sum)
	}
}

// TestPickFontFamily：字体栈按优先级挑出本机可用族名；Editor 的等宽栈
// 在没有 Menlo/Consolas 的 Linux 上应落到 DejaVu Sans Mono 等真实等宽字体，
// 而不是走 compat 默认族（那样会退化成非等宽）。
func TestPickFontFamily(t *testing.T) {
	if got := pickFontFamily(""); got != "" {
		t.Errorf("空栈应返回空串: %q", got)
	}
	if got := pickFontFamily("ui-monospace, SFMono-Regular, Menlo, Consolas, monospace"); got == "" {
		t.Error("等宽栈应解析出本机一个可用族名")
	} else if !sk.FontMgrDefault().FamilyExists(got) {
		t.Errorf("解析出的族名 %q 应真实存在", got)
	}
	// 纯通用 sans-serif 栈保持默认（空串）。
	if got := pickFontFamily("system-ui, -apple-system, sans-serif"); got != "" {
		t.Errorf("无具体族名的 sans 栈应走默认: %q", got)
	}
}
