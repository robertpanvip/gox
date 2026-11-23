package rustbackend

import (
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
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
