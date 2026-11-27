package skia

import (
	"testing"

	gxapp "github.com/robertpanvip/gox-core/runtime/app"
	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
)

// TestDrawPublishesMeasured：Skia 在 Draw（锁外）把布局结果回流给 Host。
// 订阅某节点的稳定 ID，Draw 后应收到其实测宽高（这里显式 height=64/width=120）。
func TestDrawPublishesMeasured(t *testing.T) {
	gxscene.ResetMeasurements()
	tpl := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"}, // 根：column 容器
		{Code: gxscene.OpElem, Arg: "div"}, // 子：目标节点，固定高度
		{Code: gxscene.OpAttr, Key: "width", Arg: "120"},
		{Code: gxscene.OpAttr, Key: "height", Arg: "64"},
		{Code: gxscene.OpClose},
		{Code: gxscene.OpClose},
	}}
	inst := gxscene.Render(tpl)
	defer inst.Dispose()
	id := inst.Root().Children[0].ID

	got := 0.0
	unsub := gxscene.SubscribeMeasured(id, func(m gxscene.Measured) { got = m.H })
	defer unsub()

	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Width: 300, Height: 200}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()
	if err := w.Draw(inst.Root()); err != nil {
		t.Fatalf("Draw: %v", err)
	}

	// 契约：回流值 == 布局器为该节点算出的真实高度（不强依赖具体 padding 规则）。
	node := inst.Root().Children[0]
	want := float64(w.cache.layout[node].H)
	if mabs(got-want) > 0.01 {
		t.Errorf("回流实测高度应等于布局高度 %g，实际 %g", want, got)
	}
	if got <= 0 {
		t.Errorf("回流高度应为正，实际 %g", got)
	}
}

// TestMeasuredFromLayoutSkipsNoNode：无 ID 的骨架节点不进回流。
func TestMeasuredFromLayoutSkipsNoNode(t *testing.T) {
	withID := &gxscene.Node{Type: gxscene.NodeDiv, ID: 42}
	noID := &gxscene.Node{Type: gxscene.NodeDiv, ID: gxscene.NoNode}
	cache := layoutCache{
		withID: layoutBox{W: 10, H: 20},
		noID:   layoutBox{W: 30, H: 40},
	}
	ms := measuredFromLayout(cache)
	if len(ms) != 1 {
		t.Fatalf("应只回流 1 个有 ID 的节点，实际 %d", len(ms))
	}
	if ms[0].ID != 42 || ms[0].H != 20 {
		t.Errorf("回流值错误: %+v", ms[0])
	}
}

func mabs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
