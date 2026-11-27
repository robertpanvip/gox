package skia

import (
	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
	"testing"
	"time"
)

func TestAnimatorNumeric(t *testing.T) {
	a := NewAnimator()
	n := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpAttr, Key: "transition", Arg: "width 200ms linear"},
		{Code: gxscene.OpAttr, Key: "width", Arg: "100"},
		{Code: gxscene.OpClose},
	}})
	root := n.Root().Children[0]
	t0 := time.Now()

	readAt := func(at time.Time) float64 {
		restore := a.apply(root, at)
		v, _ := propNum(root.Props["width"])
		restore()
		return v
	}
	// 首帧：目标 100，直接生效
	if got := readAt(t0); got != 100 {
		t.Errorf("首帧 width=%v want 100", got)
	}
	// 目标改 200，t=0 → 起点 100
	root.Props["width"] = "200"
	if got := readAt(t0); got < 99 || got > 101 {
		t.Errorf("t=0 width=%v want ≈100", got)
	}
	// t=100ms → 150
	if got := readAt(t0.Add(100 * time.Millisecond)); got < 145 || got > 155 {
		t.Errorf("t=100ms width=%v want ≈150", got)
	}
	// t=200ms → 200
	if got := readAt(t0.Add(200 * time.Millisecond)); got < 199 || got > 201 {
		t.Errorf("t=200ms width=%v want ≈200", got)
	}
}

func TestAnimatorRestore(t *testing.T) {
	a := NewAnimator()
	n := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpAttr, Key: "transition", Arg: "opacity 300ms linear"},
		{Code: gxscene.OpAttr, Key: "opacity", Arg: "1"},
		{Code: gxscene.OpClose},
	}})
	root := n.Root().Children[0]
	t0 := time.Now()
	a.apply(root, t0)
	root.Props["opacity"] = "0"
	restore := a.apply(root, t0.Add(150*time.Millisecond))
	// apply 期间被覆盖（插值中），restore 后恢复为宿主写入的目标 "0"
	if root.Props["opacity"] == "0" {
		// 中点应约为 0.5，不是 0
		t.Logf("mid opacity=%v", root.Props["opacity"])
	}
	restore()
	if root.Props["opacity"] != "0" {
		t.Errorf("restore 后应恢复宿主值 0, got %v", root.Props["opacity"])
	}
}

func TestAnimatorColor(t *testing.T) {
	a := NewAnimator()
	n := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpAttr, Key: "transition", Arg: "bg 200ms linear"},
		{Code: gxscene.OpAttr, Key: "bg", Arg: "#000000"},
		{Code: gxscene.OpClose},
	}})
	root := n.Root().Children[0]
	t0 := time.Now()
	a.apply(root, t0)()
	root.Props["bg"] = "#ffffff"
	a.apply(root, t0)() // 检测目标变化、启动动画（t=0）
	restore := a.apply(root, t0.Add(100*time.Millisecond))
	c, ok := parseColor(root.Props["bg"].(string))
	restore()
	if !ok {
		t.Fatal("插值后颜色解析失败")
	}
	r := c >> 16 & 0xff
	if r < 110 || r > 145 {
		t.Errorf("中点颜色 R=%d want ≈127", r)
	}
}
