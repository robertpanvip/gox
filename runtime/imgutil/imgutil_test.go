package imgutil

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func solidPNG(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestResolveDataURI(t *testing.T) {
	raw := solidPNG(t, 2, 3, color.RGBA{255, 0, 0, 255})
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
	img, err := Resolve(uri)
	if err != nil {
		t.Fatalf("Resolve data URI: %v", err)
	}
	if img.Bounds().Dx() != 2 || img.Bounds().Dy() != 3 {
		t.Fatalf("尺寸 %dx%d, want 2x3", img.Bounds().Dx(), img.Bounds().Dy())
	}
}

func TestResolveLocalFile(t *testing.T) {
	raw := solidPNG(t, 4, 4, color.RGBA{0, 255, 0, 255})
	p := filepath.Join(t.TempDir(), "a.png")
	if err := os.WriteFile(p, raw, 0644); err != nil {
		t.Fatal(err)
	}
	img, err := Resolve(p)
	if err != nil {
		t.Fatalf("Resolve local: %v", err)
	}
	if img.Bounds().Dx() != 4 {
		t.Fatalf("尺寸 %d, want 4", img.Bounds().Dx())
	}
}

func TestResolveErrors(t *testing.T) {
	for name, src := range map[string]string{
		"empty":    "",
		"bad uri":  "data:image/png;base64,!!!not-base64!!!",
		"missing":  filepath.Join(t.TempDir(), "missing.png"),
		"bad data": "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("not an image")),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Resolve(src); err == nil {
				t.Fatal("应返回错误")
			}
		})
	}
}
