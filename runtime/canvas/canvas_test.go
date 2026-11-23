package canvas

import "testing"

func TestEncodeDecodeRoundTrip(t *testing.T) {
	ops := []Op{
		{Kind: OpRect, X: 10, Y: 20, W: 100, H: 50, Fill: "#ff0000", Radius: 8},
		{Kind: OpCircle, CX: 50, CY: 50, R: 25, Stroke: "blue", StrokeWidth: 2},
		{Kind: OpLine, X1: 0, Y1: 0, X2: 100, Y2: 100, Stroke: "#000"},
		{Kind: OpPath, D: "M0 0 L10 10 Z", Fill: "rgba(0,0,0,0.5)"},
	}
	s := Encode(ops)
	got := Decode(s)
	if len(got) != len(ops) {
		t.Fatalf("round-trip 数量 %d, want %d", len(got), len(ops))
	}
	for i := range ops {
		if got[i].Kind != ops[i].Kind {
			t.Errorf("[%d] Kind=%q want %q", i, got[i].Kind, ops[i].Kind)
		}
	}
}

func TestDecodeInvalid(t *testing.T) {
	for _, s := range []string{"", "not json", "{}", "null"} {
		if got := Decode(s); len(got) != 0 {
			t.Errorf("Decode(%q) 应为空, got %v", s, got)
		}
	}
}

func TestEncodeEmpty(t *testing.T) {
	if Encode(nil) != "[]" {
		t.Fatal("空指令应编码为 []")
	}
}
