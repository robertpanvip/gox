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

// 回归：SVG 1.1 F.6.5.5 的平方根符号选择（laf≠sf→+1，laf=sf→−1）。
// 写反会把 90° 短弧画成 270° 长弧，refresh 等图标渲染成乱圈。
func TestParsePathArcSweepSign(t *testing.T) {
	// 短弧（laf=0, sf=1）：(21,12) → (12,21)，r=9，圆心 (12,12)。
	// 中间控制点必须贴着圆走，不得绕到弦另一侧（错误圆心 (21,21)）。
	cmds, err := parsePath("M21 12 A9 9 0 0 1 12 21")
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 2 || cmds[1].op != 'C' {
		t.Fatalf("90° 弧应转成 1 段 C: %+v", cmds)
	}
	// 所有生成控制点到正确圆心 (12,12) 的距离 = r·√(1+k²)（k≈0.5523 切线系数），
	// 上界取 r·√2：错误圆心方案（270° 长弧绕弦另一侧）的控制点必然超过此值。
	for _, p := range cmds[1].pts[:2] {
		d := math.Hypot(float64(p.x)-12, float64(p.y)-12)
		if d > 9*math.Sqrt2*1.01 {
			t.Fatalf("控制点 (%g,%g) 到圆心距离 %g 超出短弧切线范围", p.x, p.y, d)
		}
	}
	// 终点必须等于弧参数终点。
	end := cmds[1].pts[2]
	if !approx(end.x, 12) || !approx(end.y, 21) {
		t.Fatalf("弧终点错误: %+v", end)
	}

	// 大弧（laf=1, sf=1）应转成多段并落在同一圆上。
	cmds2, err := parsePath("M21 12 A9 9 0 1 1 12 3")
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds2) < 2 {
		t.Fatalf("270° 弧应转成 ≥2 段 C: %+v", cmds2)
	}
	for _, c := range cmds2 {
		if c.op != 'C' {
			continue
		}
		e := c.pts[2]
		d := math.Hypot(float64(e.x)-12, float64(e.y)-12)
		if !approx(float32(d), 9) {
			t.Fatalf("大弧子段终点 (%g,%g) 不在圆上: d=%g", e.x, e.y, d)
		}
	}
}
