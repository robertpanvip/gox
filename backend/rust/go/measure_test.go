package rustbackend

import (
	"testing"

	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
)

// TestPublishMeasuredWire：Rust 回流的 [[id,w,h],...] 应被正确解析并发布到
// scene 回流总线，订阅者收到对应 NodeID 的实测高度。
func TestPublishMeasuredWire(t *testing.T) {
	gxscene.ResetMeasurements()
	id := gxscene.NodeID(12345)
	got := 0.0
	unsub := gxscene.SubscribeMeasured(id, func(m gxscene.Measured) { got = m.H })
	defer unsub()

	// id 用十进制字符串（NodeID.String 的 wire 形式）。
	wire := `[["12345",80,156.5],["",10,10]]` // 第二条空 id 应被忽略
	publishMeasuredWire(wire)

	if mabs(got-156.5) > 0.01 {
		t.Errorf("回流实测高度应为 156.5，实际 %g", got)
	}
}

func mabs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// MeasureEditorAdvances 走真实 glyphon shaping（与绘制端同源字形），装配层
// 注入语义与 skia 后端对齐：narrow/wide 必须为正，CJK advance 不小于 ASCII。
func TestMeasureEditorAdvances(t *testing.T) {
	narrow, wide := MeasureEditorAdvances("Consolas, 'JetBrains Mono', monospace", 13)
	if narrow <= 0 {
		t.Fatalf("narrow advance 应为正, got %v", narrow)
	}
	if wide <= 0 {
		t.Fatalf("wide advance 应为正, got %v", wide)
	}
	if wide < narrow {
		t.Fatalf("CJK advance(%v) 不应小于 ASCII advance(%v)", wide, narrow)
	}
}

// 空栈（默认 SansSerif）也能测出正 advance；同参数重复调用结果稳定（共享
// FontSystem 缓存），保证装配层多次注入不会漂移。
func TestMeasureEditorAdvancesDefaultFamilyStable(t *testing.T) {
	n1, w1 := MeasureEditorAdvances("", 13)
	if n1 <= 0 || w1 <= 0 {
		t.Fatalf("默认族 advance 应为正, got narrow=%v wide=%v", n1, w1)
	}
	n2, w2 := MeasureEditorAdvances("", 13)
	if n1 != n2 || w1 != w2 {
		t.Fatalf("重复调用应稳定: first=(%v,%v) second=(%v,%v)", n1, w1, n2, w2)
	}
}
