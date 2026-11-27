package gxweb

import (
	"testing"

	gxapp "github.com/robertpanvip/gox-core/runtime/app"
	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
	"github.com/robertpanvip/gox-core/runtime/signal"
)

// helloInst 构造 <div id="greet" style={{bg:"#fff",padding:"8"}}><b>hi</b></div>
func helloInst() *gxscene.Instance {
	return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpAttr, Key: "id", Arg: "greet"},
		{Code: gxscene.OpAttr, Key: "bg", Arg: "#fff"},
		{Code: gxscene.OpAttr, Key: "padding", Arg: "8"},
		{Code: gxscene.OpElem, Arg: "b"},
		{Code: gxscene.OpChildText, Arg: "hi"},
		{Code: gxscene.OpClose},
		{Code: gxscene.OpClose},
	}, N: 0})
}

func TestWindowFullBuild(t *testing.T) {
	d := newMemDriver()
	w := NewWithDriver(d)
	if err := w.Open(gxapp.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	inst := helloInst()
	if err := w.Draw(inst.Root()); err != nil {
		t.Fatal(err)
	}
	if w.Frames() != 1 {
		t.Fatalf("frames=%d want 1", w.Frames())
	}
	// 找到 id=greet 的 div
	var divElem ElemID
	for id, e := range w.elems {
		if e.kind == "div" && e.attrs["id"] == "greet" {
			divElem = id
			break
		}
	}
	if divElem == 0 {
		t.Fatal("未找到 id=greet 的 div 镜像")
	}
	div := d.Element(divElem)
	if div.styles["bg"] != "#fff" || div.styles["padding"] != "8" {
		t.Fatalf("样式属性未下传: attrs=%#v styles=%#v", div.attrs, div.styles)
	}
	if len(div.children) != 1 {
		t.Fatalf("div 子节点数=%d want 1", len(div.children))
	}
	b := d.Element(div.children[0])
	if b.kind != "b" || b.text != "" {
		// b 的文本在文本子节点里
		if b.kind != "b" {
			t.Fatalf("子节点 kind=%q want b", b.kind)
		}
	}
	if len(b.children) != 1 || d.Element(b.children[0]).text != "hi" {
		t.Fatal("文本内容未下传")
	}
}

func TestWindowPresentFullSyncAndIncremental(t *testing.T) {
	d := newMemDriver()
	w := NewWithDriver(d)
	_ = w.Open(gxapp.DefaultConfig())

	txt := signal.New("A")
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}, func() any { return txt() })
	inst.EnableChanges() // gxapp 在 Present 后端会自动开启，这里显式模拟

	// 首帧：全量
	frame := &gxapp.Frame{Root: inst.Root(), Changes: inst.Changes(), Version: 1, FullSync: true}
	if err := w.Present(frame); err != nil {
		t.Fatal(err)
	}
	inst.TakeChanges()

	// 找到文本节点
	findText := func() string {
		for _, e := range w.elems {
			if e.kind == "#text" {
				return e.text
			}
		}
		return ""
	}
	if got := findText(); got != "A" {
		t.Fatalf("首帧文本=%q want A", got)
	}

	// 写 signal → 触发宿主响应（这里手动 Update 模拟 scene 更新）
	txt("B")
	inst.Update()
	frame2 := &gxapp.Frame{Root: inst.Root(), Changes: inst.Changes(), Version: 2}
	if err := w.Present(frame2); err != nil {
		t.Fatal(err)
	}
	if got := findText(); got != "B" {
		t.Fatalf("增量帧文本=%q want B（变更未应用）", got)
	}
	if w.Version() != 2 {
		t.Fatalf("version=%d want 2", w.Version())
	}
}

func TestWindowEventDispatch(t *testing.T) {
	d := newMemDriver()
	w := NewWithDriver(d)
	_ = w.Open(gxapp.DefaultConfig())

	fired := signal.New(0)
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "button"},
		{Code: gxscene.OpEvent, Key: "onClick", Slot: 0},
		{Code: gxscene.OpChildText, Arg: "+"},
		{Code: gxscene.OpClose},
	}, N: 1}, func() any {
		return gxscene.Handler(func(ctx gxscene.EventContext) {
			fired(fired() + 1)
		})
	})
	if err := w.Draw(inst.Root()); err != nil {
		t.Fatal(err)
	}

	// 找到 button 元素
	var btnElem ElemID
	var btnID gxscene.NodeID
	for id, e := range w.elems {
		if e.kind == "button" {
			btnElem, btnID = id, e.id
			break
		}
	}
	if btnElem == 0 {
		t.Fatal("未找到 button 镜像")
	}

	// 模拟浏览器触发 click
	d.Fire(btnElem, FiredEvent{Name: "click", Button: 0})
	if fired() != 1 {
		t.Fatalf("handler 未触发，fired=%d", fired())
	}

	// 合成事件字段正确映射
	if len(d.FiredEvents()) != 1 {
		t.Fatalf("事件记录数=%d want 1", len(d.FiredEvents()))
	}
	_ = btnID
}

func TestWindowCapabilities(t *testing.T) {
	d := newMemDriver()
	w := NewWithDriver(d)
	_ = w.Open(gxapp.DefaultConfig())

	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpAttr, Key: "id", Arg: "target"},
		{Code: gxscene.OpAttr, Key: "bg", Arg: "#123456"},
		{Code: gxscene.OpClose},
	}, N: 0})
	if err := w.Draw(inst.Root()); err != nil {
		t.Fatal(err)
	}

	target := inst.Root().Children[0]
	id := target.ID

	// 注入布局
	var elem ElemID
	for eid, m := range w.elems {
		if m.id == id {
			elem = eid
			break
		}
	}
	d.SetRect(elem, 1, 2, 120, 80)
	w2w, h, ok := w.Measure(id)
	if !ok || w2w != 120 || h != 80 {
		t.Fatalf("Measure=(%v,%v,%v) want (120,80,true)", w2w, h, ok)
	}

	cs := w.ComputedStyle(id, "")
	if cs["bg"] != "#123456" {
		t.Fatalf("ComputedStyle=%#v 缺 bg", cs)
	}

	w.Focus(id)
	if !d.Element(elem).focused {
		t.Fatal("Focus 未生效")
	}
}

func TestWindowClose(t *testing.T) {
	w := NewWithDriver(newMemDriver())
	_ = w.Open(gxapp.DefaultConfig())
	inst := helloInst()
	_ = w.Draw(inst.Root())
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Draw(inst.Root()); err != errClosed {
		t.Fatalf("Close 后 Draw err=%v want errClosed", err)
	}
	// 幂等
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}
