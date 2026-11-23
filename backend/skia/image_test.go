package skia

// img 原语回归：验证 <img src> 走原生绘制管线，DrawLog 出现 image 记录，
// object-fit 与圆角不影响结构，且 src 解码失败时安全降级（记录无 image，不 panic）。

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"testing"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

func testDataURI(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 20), uint8(y * 20), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func imgTemplate(src, fit string) gxscene.Template {
	ops := []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "img"},
		{Code: gxscene.OpAttr, Key: "src", Arg: src},
		{Code: gxscene.OpAttr, Key: "width", Arg: "120"},
		{Code: gxscene.OpAttr, Key: "height", Arg: "80"},
	}
	if fit != "" {
		ops = append(ops, gxscene.Op{Code: gxscene.OpAttr, Key: "object-fit", Arg: fit})
	}
	ops = append(ops, gxscene.Op{Code: gxscene.OpClose})
	return gxscene.Template{Ops: ops}
}

func TestImgDrawsImageOp(t *testing.T) {
	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Width: 300, Height: 200}); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	src := testDataURI(t, 40, 30)
	if err := w.Draw(gxscene.Render(imgTemplate(src, "cover")).Root()); err != nil {
		t.Fatal(err)
	}
	log := w.DrawLog()
	if log.Find("image", "img") != 1 {
		t.Fatalf("应有 1 条 image 绘制记录, log=%v", log)
	}
}

func TestImgObjectFitVariants(t *testing.T) {
	src := testDataURI(t, 40, 30)
	for _, fit := range []string{"fill", "contain", "cover"} {
		w := NewWindow()
		if err := w.Open(gxapp.WindowConfig{Width: 200, Height: 200}); err != nil {
			t.Fatal(err)
		}
		if err := w.Draw(gxscene.Render(imgTemplate(src, fit)).Root()); err != nil {
			t.Fatalf("fit=%s: %v", fit, err)
		}
		if w.DrawLog().Find("image", "img") != 1 {
			t.Fatalf("fit=%s 应画 image", fit)
		}
		w.Close()
	}
}

func TestImgBadSrcDegradesGracefully(t *testing.T) {
	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Width: 200, Height: 200}); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	// 解码失败的图片：不应 panic，也不产生 image 记录。
	if err := w.Draw(gxscene.Render(imgTemplate("data:image/png;base64,bm90LWFuLWltYWdl", "")).Root()); err != nil {
		t.Fatal(err)
	}
	if w.DrawLog().Find("image", "img") != 0 {
		t.Fatalf("坏 src 不应画 image, log=%v", w.DrawLog())
	}
}
