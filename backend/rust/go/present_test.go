package rustbackend

import (
	"encoding/binary"
	"testing"
	"unsafe"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// presentProbe 记录每次 draw 的字节帧，并可指定返回码。
type presentProbe struct {
	frames []byte // 最近一次发送的帧（magic 判定全量/增量）
	calls  int
	rc     int // draw 返回码，0=成功
}

func (p *presentProbe) draw(data []byte) int {
	p.calls++
	p.frames = append(p.frames[:0], data...)
	return p.rc
}

func newProbeWindow(p *presentProbe) *Window {
	return &Window{
		handle: unsafeHandle(), // 非 nil 即可，drawFn 已接管真实 cgo 调用
		cfg:    gxapp.WindowConfig{Width: 100, Height: 100},
		drawFn: p.draw,
	}
}

// unsafeHandle 返回一个非 nil 的哨兵指针；测试路径下 cWindowDraw 不会被调用。
func unsafeHandle() unsafe.Pointer { return unsafe.Pointer(&struct{}{}) }

func magicOf(b []byte) uint32 {
	if len(b) < 4 {
		return 0
	}
	return binary.LittleEndian.Uint32(b[:4])
}

func rootWith(id gxscene.NodeID) *gxscene.Node {
	return &gxscene.Node{Type: gxscene.NodeDiv, ID: id}
}

// 无变化帧不得发送任何数据：避免空帧触发整棵树重序列化（评审 P1）。
func TestPresentNoChangesSendsNothing(t *testing.T) {
	p := &presentProbe{}
	w := newProbeWindow(p)
	root := rootWith(1)

	// 首帧全量建基线。
	if err := w.Present(&gxapp.Frame{Root: root, Version: 1, FullSync: true}); err != nil {
		t.Fatal(err)
	}
	if p.calls != 1 || magicOf(p.frames) != binMagic {
		t.Fatalf("首帧应全量, calls=%d magic=%x", p.calls, magicOf(p.frames))
	}
	// 第二帧无变化：不应发送。
	if err := w.Present(&gxapp.Frame{Root: root, Version: 2}); err != nil {
		t.Fatal(err)
	}
	if p.calls != 1 {
		t.Fatalf("无变化不应 send, calls=%d", p.calls)
	}
	// 无变化帧仍推进版本，否则下一帧会被误判丢帧。
	if w.lastVersion != 2 {
		t.Fatalf("无变化帧应推进 lastVersion=2, got=%d", w.lastVersion)
	}
	// 紧接着的正常增量帧必须是 GRX2 而非被迫全量。
	changes := []gxscene.Change{{Kind: gxscene.ChangeSetText, NodeID: "1", Value: "x"}}
	if err := w.Present(&gxapp.Frame{Root: root, Version: 3, Changes: changes}); err != nil {
		t.Fatal(err)
	}
	if magicOf(p.frames) != deltaMagic {
		t.Fatalf("版本连续应发增量 GRX2, magic=%x", magicOf(p.frames))
	}
}

// Present 失败必须回退 hasBase，下一帧用全量重建（评审 P0）。
func TestPresentFailureFallsBackToFullSync(t *testing.T) {
	p := &presentProbe{}
	w := newProbeWindow(p)
	root := rootWith(1)

	if err := w.Present(&gxapp.Frame{Root: root, Version: 1, FullSync: true}); err != nil {
		t.Fatal(err)
	}
	if !w.hasBase {
		t.Fatal("首帧成功后应有基线")
	}

	// 第二帧 draw 失败。
	p.rc = 1
	changes := []gxscene.Change{{Kind: gxscene.ChangeSetText, NodeID: "1", Value: "y"}}
	if err := w.Present(&gxapp.Frame{Root: root, Version: 2, Changes: changes}); err == nil {
		t.Fatal("draw 失败应返回错误")
	}
	if w.hasBase {
		t.Fatal("失败后必须回退 hasBase")
	}

	// 恢复帧：即使带 Changes 也必须全量重建。
	p.rc = 0
	if err := w.Present(&gxapp.Frame{Root: root, Version: 3, Changes: changes, FullSync: true}); err != nil {
		t.Fatal(err)
	}
	if magicOf(p.frames) != binMagic {
		t.Fatalf("恢复帧应全量 GRX1, magic=%x", magicOf(p.frames))
	}
	if !w.hasBase {
		t.Fatal("恢复成功后应重建基线")
	}
}

// 版本跳号/重复视为丢帧，强制全量（评审 P1）。
func TestPresentVersionGapForcesFullSync(t *testing.T) {
	p := &presentProbe{}
	w := newProbeWindow(p)
	root := rootWith(1)

	if err := w.Present(&gxapp.Frame{Root: root, Version: 10, FullSync: true}); err != nil {
		t.Fatal(err)
	}
	changes := []gxscene.Change{{Kind: gxscene.ChangeSetText, NodeID: "1", Value: "z"}}

	// 版本从 10 跳到 12（漏了 11）：强制全量。
	if err := w.Present(&gxapp.Frame{Root: root, Version: 12, Changes: changes}); err != nil {
		t.Fatal(err)
	}
	if magicOf(p.frames) != binMagic {
		t.Fatalf("版本跳号应全量, magic=%x", magicOf(p.frames))
	}
	if w.lastVersion != 12 {
		t.Fatalf("全量后应更新版本=12, got=%d", w.lastVersion)
	}

	// 版本回退到 11：同样强制全量。
	if err := w.Present(&gxapp.Frame{Root: root, Version: 11, Changes: changes}); err != nil {
		t.Fatal(err)
	}
	if magicOf(p.frames) != binMagic {
		t.Fatalf("版本回退应全量, magic=%x", magicOf(p.frames))
	}
}

// 有基线且版本连续时用增量，验证不误降级全量（守护评审改动不破坏正常路径）。
func TestPresentNormalDelta(t *testing.T) {
	p := &presentProbe{}
	w := newProbeWindow(p)
	root := rootWith(1)

	if err := w.Present(&gxapp.Frame{Root: root, Version: 1, FullSync: true}); err != nil {
		t.Fatal(err)
	}
	changes := []gxscene.Change{{Kind: gxscene.ChangeSetText, NodeID: "1", Value: "ok"}}
	if err := w.Present(&gxapp.Frame{Root: root, Version: 2, Changes: changes}); err != nil {
		t.Fatal(err)
	}
	if magicOf(p.frames) != deltaMagic {
		t.Fatalf("正常更新应发增量, magic=%x", magicOf(p.frames))
	}
}
