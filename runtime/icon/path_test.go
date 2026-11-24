package icon

import (
	"math"
	"testing"
)

func approx(a, b float32) bool { return math.Abs(float64(a-b)) < 0.05 }

func TestParsePathAbsolute(t *testing.T) {
	cmds, err := parsePath("M10 10 L20 10 L20 20 Z")
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 4 {
		t.Fatalf("期望 4 条命令，got %d", len(cmds))
	}
	if cmds[0].op != 'M' || cmds[0].pts[0].x != 10 {
		t.Fatalf("M 解析错误: %+v", cmds[0])
	}
	if cmds[3].op != 'Z' {
		t.Fatalf("Z 解析错误")
	}
}

func TestParsePathRelative(t *testing.T) {
	cmds, err := parsePath("m10 10 l10 0 l0 10 z")
	if err != nil {
		t.Fatal(err)
	}
	// m10 10 → (10,10); l10 0 → (20,10); l0 10 → (20,20)
	if !approx(cmds[1].pts[0].x, 20) || !approx(cmds[2].pts[0].y, 20) {
		t.Fatalf("相对坐标解析错误: %+v", cmds)
	}
}

func TestParsePathImplicitRepeat(t *testing.T) {
	// M 后的隐式坐标组按 L 处理；L 后重复。
	cmds, err := parsePath("M0 0 10 0 10 10 0 10 Z")
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 5 {
		t.Fatalf("期望 M+3L+Z=5，got %d: %+v", len(cmds), cmds)
	}
	if cmds[1].op != 'L' || cmds[2].op != 'L' || cmds[3].op != 'L' {
		t.Fatalf("隐式重复应为 L: %+v", cmds)
	}
}

func TestParsePathHVLowers(t *testing.T) {
	cmds, err := parsePath("M5 5 H20 V20 h-10 v-10")
	if err != nil {
		t.Fatal(err)
	}
	// H20→L(20,5); V20→L(20,20); h-10→L(10,20); v-10→L(10,10)
	want := []pt{{20, 5}, {20, 20}, {10, 20}, {10, 10}}
	for i, w := range want {
		got := cmds[i+1].pts[0]
		if !approx(got.x, w.x) || !approx(got.y, w.y) {
			t.Fatalf("第 %d 段: got %+v want %+v", i, got, w)
		}
	}
}

func TestParsePathQToC(t *testing.T) {
	cmds, err := parsePath("M0 0 Q10 10 20 0")
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 2 || cmds[1].op != 'C' || len(cmds[1].pts) != 3 {
		t.Fatalf("Q 应转为 C: %+v", cmds)
	}
	if !approx(cmds[1].pts[2].x, 20) {
		t.Fatalf("Q 终点错误: %+v", cmds[1].pts[2])
	}
}

func TestParsePathSmoothCurve(t *testing.T) {
	// S/T 需要前序 C/Q 才有反射点；单独出现应退化为自身控制点而不报错。
	if _, err := parsePath("M0 0 S10 10 20 0 T30 10"); err != nil {
		t.Fatal(err)
	}
	// C 之后的 S、Q 之后的 T
	if _, err := parsePath("M0 0 C5 5 10 5 15 0 S25 -5 30 0 Q35 5 40 0 T50 0"); err != nil {
		t.Fatal(err)
	}
}

func TestParsePathArc(t *testing.T) {
	// 半圆弧：从 (0,0) 到 (20,0)，rx=ry=10 → 应转成若干 C。
	cmds, err := parsePath("M0 0 A10 10 0 0 1 20 0")
	if err != nil {
	 t.Fatal(err)
	}
	if len(cmds) < 2 || cmds[1].op != 'C' {
		t.Fatalf("A 应转为 C: %+v", cmds)
	}
	last := cmds[len(cmds)-1].pts[2]
	if !approx(last.x, 20) || !approx(last.y, 0) {
		t.Fatalf("A 终点错误: %+v", last)
	}
}

func TestParsePathScientific(t *testing.T) {
	cmds, err := parsePath("M1e1 2.5e0 L-3.5 +4")
	if err != nil {
		t.Fatal(err)
	}
	if !approx(cmds[0].pts[0].x, 10) || !approx(cmds[0].pts[0].y, 2.5) {
		t.Fatalf("科学计数法解析错误: %+v", cmds[0])
	}
}

func TestEmitPathBakeTransform(t *testing.T) {
	cmds, _ := parsePath("M0 0 L10 10 Z")
	// viewBox 0 0 10 10 烘焙到 size=20：scale=2
	got := emitPath(cmds, transform{s: 2, tx: 0, ty: 0})
	if got != "M0 0 L20 20 Z" {
		t.Fatalf("变换烘焙错误: %q", got)
	}
}
