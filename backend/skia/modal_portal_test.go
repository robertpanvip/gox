package skia

import (
	"strings"
	"testing"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

func TestPlaceCentered(t *testing.T) {
	cases := []struct {
		pw, ph, aw, ah float32
		x, y           float32
	}{
		{200, 100, 800, 600, 300, 250}, // 常规居中
		{100, 100, 300, 300, 100, 100},
		{900, 500, 800, 600, 0, 50}, // 宽度超画布：clamp 到画布后贴左
	}
	for _, c := range cases {
		x, y := placeCentered(c.pw, c.ph, c.aw, c.ah)
		if abs32(x-c.x) > 0.01 || abs32(y-c.y) > 0.01 {
			t.Errorf("placeCentered(%v,%v,%v,%v) = (%v,%v), want (%v,%v)",
				c.pw, c.ph, c.aw, c.ah, x, y, c.x, c.y)
		}
	}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// 构造：scrim + modal（居中）+ portal（自身 absolute）。
func modalPortalTree() *gxscene.Node {
	return gxscene.Render(gxscene.Template{
		Ops: []gxscene.Op{
			{Code: gxscene.OpElem, Arg: "div"}, // root
			{Code: gxscene.OpAttr, Key: "width", Arg: "800"},
			{Code: gxscene.OpAttr, Key: "height", Arg: "600"},
			// scrim
			{Code: gxscene.OpElem, Arg: "div"},
			{Code: gxscene.OpAttr, Key: "gx-role", Arg: "scrim"},
			{Code: gxscene.OpClose},
			// modal
			{Code: gxscene.OpElem, Arg: "div"},
			{Code: gxscene.OpAttr, Key: "gx-role", Arg: "modal"},
			{Code: gxscene.OpAttr, Key: "width", Arg: "200"},
			{Code: gxscene.OpAttr, Key: "height", Arg: "100"},
			{Code: gxscene.OpChildText, Arg: "Dialog"},
			{Code: gxscene.OpClose},
			// portal
			{Code: gxscene.OpElem, Arg: "div"},
			{Code: gxscene.OpAttr, Key: "gx-role", Arg: "portal"},
			{Code: gxscene.OpClose},
			{Code: gxscene.OpClose},
		},
	}).Root()
}

func TestModalPortalRoles(t *testing.T) {
	root := modalPortalTree()
	for _, tc := range []struct {
		role string
		fn   func(*gxscene.Node) bool
	}{
		{"scrim", isScrim}, {"modal", isModal}, {"portal", isPortal},
	} {
		n := findRole(root, tc.role)
		if n == nil {
			t.Fatalf("未找到角色 %s", tc.role)
		}
		if !tc.fn(n) || !isOverlay(n) {
			t.Errorf("角色 %s 判定失败: fn=%v overlay=%v", tc.role, tc.fn(n), isOverlay(n))
		}
		if tc.role != "scrim" && !isContentOverlay(n) {
			t.Errorf("内容浮层 %s 应被 isContentOverlay 识别", tc.role)
		}
	}
}

func TestModalCenteredLayout(t *testing.T) {
	root := modalPortalTree()
	layout := computeLayout(root, 800, 600, fixedMeasure2)
	modal := findRole(root, "modal")
	box, ok := layout[modal]
	if !ok {
		t.Fatal("modal 未进入布局缓存")
	}
	// 200x100 在 800x600 中居中 → (300,250)
	if abs32(box.X-300) > 0.01 || abs32(box.Y-250) > 0.01 {
		t.Errorf("modal 位置 = (%v,%v), want (300,250)", box.X, box.Y)
	}
	// scrim 铺满
	scrim := findRole(root, "scrim")
	sb := layout[scrim]
	if sb.W != 800 || sb.H != 600 {
		t.Errorf("scrim 尺寸 = (%v,%v), want (800,600)", sb.W, sb.H)
	}
}

func TestModalPortalPaintOrder(t *testing.T) {
	root := gxscene.Render(gxscene.Template{
		Ops: []gxscene.Op{
			{Code: gxscene.OpElem, Arg: "div"},
			{Code: gxscene.OpAttr, Key: "width", Arg: "800"},
			// 普通内容标记
			{Code: gxscene.OpElem, Arg: "div"},
			{Code: gxscene.OpChildText, Arg: "NormalContent"},
			{Code: gxscene.OpClose},
			// scrim
			{Code: gxscene.OpElem, Arg: "div"},
			{Code: gxscene.OpAttr, Key: "gx-role", Arg: "scrim"},
			{Code: gxscene.OpChildText, Arg: "Scrim"},
			{Code: gxscene.OpClose},
			// modal
			{Code: gxscene.OpElem, Arg: "div"},
			{Code: gxscene.OpAttr, Key: "gx-role", Arg: "modal"},
			{Code: gxscene.OpAttr, Key: "width", Arg: "200"},
			{Code: gxscene.OpAttr, Key: "height", Arg: "100"},
			{Code: gxscene.OpChildText, Arg: "Dialog"},
			{Code: gxscene.OpClose},
			// portal（树末尾 → 最上层）
			{Code: gxscene.OpElem, Arg: "div"},
			{Code: gxscene.OpAttr, Key: "gx-role", Arg: "portal"},
			{Code: gxscene.OpChildText, Arg: "PortalToast"},
			{Code: gxscene.OpClose},
			{Code: gxscene.OpClose},
		},
	}).Root()

	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Title: "mp", Width: 800, Height: 600, Backend: "skia"}); err != nil {
		t.Skipf("无图形环境，跳过绘制测试: %v", err)
	}
	defer w.Close()
	if err := w.Draw(root); err != nil {
		t.Fatal(err)
	}
	log := w.DrawLog()
	idx := func(substr string) int {
		for i, op := range log {
			if op.Kind == "text" && strings.Contains(op.Name, substr) {
				return i
			}
		}
		return -1
	}
	iNormal, iScrim, iModal, iPortal := idx("NormalContent"), idx("Scrim"), idx("Dialog"), idx("PortalToast")
	if iNormal < 0 || iScrim < 0 || iModal < 0 || iPortal < 0 {
		t.Fatalf("绘制缺失: normal=%d scrim=%d modal=%d portal=%d", iNormal, iScrim, iModal, iPortal)
	}
	if !(iNormal < iScrim && iScrim < iModal && iModal < iPortal) {
		t.Errorf("绘制顺序应为 普通<%d < 遮罩<%d < modal<%d < portal<%d>", iNormal, iScrim, iModal, iPortal)
	}
}
