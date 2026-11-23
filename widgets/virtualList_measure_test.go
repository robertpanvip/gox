package widgets

import (
	"fmt"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// buildMeasureVL 构造一个自动测量模式的 VirtualList，返回每个可见项的根节点 ID。
// itemNodeIDs[index] = 该项内容根节点的 NodeID（后端回流的键）。
func buildMeasureVL(t *testing.T, count int, viewport float64, overscan int, scroll signal.Signal[float64], onScroll func(float64)) (*gxscene.Instance, map[int]gxscene.NodeID) {
	t.Helper()
	ids := make(map[int]gxscene.NodeID)
	vl := VirtualList(gxscene.Props{
		"count":           count,
		"viewport-height": viewport,
		"estimate-height": 40,
		"overscan":        overscan,
		"scroll-top":      func() any { return scroll() },
		"on-scroll":       onScroll,
		"render-item": func(i int) *gxscene.Instance {
			inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
				{Code: gxscene.OpElem, Arg: "div"},
				{Code: gxscene.OpChildText, Arg: fmt.Sprintf("item-%d", i)},
				{Code: gxscene.OpClose},
			}})
			ids[i] = inst.Root().ID
			return inst
		},
	})
	vl.BindReactive(signal.Effect)
	return vl, ids
}

func textsOf(n *gxscene.Node) []string {
	var out []string
	collectText(n, &out)
	return out
}

// TestAutoMeasureFillOffsets：不传 height 时，回流实测高度应改变可见窗口。
// 初始全部按 estimate=40 定位；把 item0..2 实测为 100 后，scrollTop=0 视口 120
// 里应只放得下 item0（100）+ item1 的一部分，窗口首项仍是 item0，但 item3 不再可见。
func TestAutoMeasureFillOffsets(t *testing.T) {
	gxscene.ResetMeasurements()
	scroll := signal.New(0.0)
	vl, ids := buildMeasureVL(t, 50, 120, 0, scroll, nil)

	before := textsOf(vl.Root())
	if len(before) == 0 || before[0] != "item-0" {
		t.Fatalf("初始窗口应从 item-0 开始: %v", before)
	}

	// 模拟后端回流：item0/1/2 真实高度都是 100（远大于 estimate 40）。
	gxscene.PublishMeasured([]gxscene.Measured{
		{ID: ids[0], W: 100, H: 100},
		{ID: ids[1], W: 100, H: 100},
		{ID: ids[2], W: 100, H: 100},
	})

	after := textsOf(vl.Root())
	// 高度变大后，同样 120px 视口能容纳的项变少，窗口尾项应前移。
	if len(after) >= len(before) {
		t.Errorf("实测高度变大后窗口项数应减少: before=%d after=%d", len(before), len(after))
	}
}

// TestAutoMeasureAnchorCompensation：VIRTUA 锚定——视口上方项变高时，
// 经 on-scroll 写回的 scrollTop 应增加同样的量，使可见内容视觉不动。
//
// 场景：scrollTop=200（原按 estimate=40 → 锚点是 item5），把 item4 实测从 40→140
// （+100，位于锚点上方）。offsets[5] 因此 +100，jump=+100，flush 后期望
// onScroll 收到 300。
func TestAutoMeasureAnchorCompensation(t *testing.T) {
	gxscene.ResetMeasurements()
	var flushed []float64
	scroll := signal.New(200.0)
	vl, ids := buildMeasureVL(t, 50, 120, 12, scroll, func(v float64) { flushed = append(flushed, v) })

	_ = vl
	// item4: 40 → 140。位于锚点 item5 正上方（overscan=12 保证其在窗口内已订阅），
	// offsets[5] 因此 +100，应触发 +100 补偿。
	gxscene.PublishMeasured([]gxscene.Measured{
		{ID: ids[4], W: 140, H: 140},
	})

	if len(flushed) == 0 {
		t.Fatal("锚点上方项变高应触发 on-scroll 回写")
	}
	want := 300.0
	if mabs(flushed[len(flushed)-1]-want) > 1 {
		t.Errorf("锚定补偿后 scrollTop 应为 %g，实际 %g (all=%v)", want, flushed[len(flushed)-1], flushed)
	}
}

// TestAutoMeasureBelowAnchorNoJump：锚点下方的项变高不应产生补偿。
func TestAutoMeasureBelowAnchorNoJump(t *testing.T) {
	gxscene.ResetMeasurements()
	var flushed []float64
	scroll := signal.New(0.0) // 锚点 item0
	vl, ids := buildMeasureVL(t, 50, 120, 12, scroll, func(v float64) { flushed = append(flushed, v) })
	_ = vl

	// item10 远在锚点下方，变高不应影响 offsets[0]，无补偿。
	gxscene.PublishMeasured([]gxscene.Measured{
		{ID: ids[10], W: 200, H: 200},
	})

	if len(flushed) != 0 {
		t.Errorf("锚点下方项变高不应触发补偿，实际收到 %v", flushed)
	}
}

// TestPublishMeasuredDedup：总线去重——相同高度重复发布不应重复通知。
func TestPublishMeasuredDedup(t *testing.T) {
	gxscene.ResetMeasurements()
	id := gxscene.NodeID(999999)
	calls := 0
	unsub := gxscene.SubscribeMeasured(id, func(gxscene.Measured) { calls++ })
	defer unsub()

	if !gxscene.PublishMeasured([]gxscene.Measured{{ID: id, W: 10, H: 10}}) {
		t.Error("首次发布应返回 changed=true")
	}
	if gxscene.PublishMeasured([]gxscene.Measured{{ID: id, W: 10, H: 10.2}}) {
		t.Error("0.5px 内的抖动不应视为变化")
	}
	if calls != 1 {
		t.Errorf("回调应只触发一次，实际 %d", calls)
	}
}

// TestSubscribeMeasuredReplay：订阅时若已有回流数据，应立即回放当前值。
func TestSubscribeMeasuredReplay(t *testing.T) {
	gxscene.ResetMeasurements()
	id := gxscene.NodeID(888888)
	gxscene.PublishMeasured([]gxscene.Measured{{ID: id, W: 30, H: 30}})
	got := 0.0
	unsub := gxscene.SubscribeMeasured(id, func(m gxscene.Measured) { got = m.H })
	defer unsub()
	if got != 30 {
		t.Errorf("订阅应回放已有实测值 30，实际 %g", got)
	}
}
