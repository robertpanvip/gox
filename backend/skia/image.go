package skia

// img 原语：把 <img src> 解码为 Skia Image 并按 object-fit 绘制到布局盒。
// src 解析复用核心 runtime/imgutil（data URI / 本地路径 / http），因此两端
// 图片来源语义完全一致。解码结果按 src 缓存，避免每帧重复解码——响应式重绘
// 时同一张图只解码一次。

import (
	"image"
	"image/draw"
	"strconv"
	"strings"
	"sync"

	sk "github.com/pekim/go-skia"
	"github.com/robertpanvip/gox/runtime/imgutil"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// imageCache 进程级缓存：src → *sk.Image。Skia Image 是 immutable 可共享的。
// 用 Unref 需要谨慎，这里让缓存常驻（后端生命周期内图片集合有限）。
var imageCache sync.Map // map[string]sk.Image

func loadSkImage(src string) (sk.Image, bool) {
	if v, ok := imageCache.Load(src); ok {
		return v.(sk.Image), true
	}
	img, err := imgutil.Resolve(src)
	if err != nil {
		return sk.Image{}, false
	}
	si := skImageFromGo(img)
	if si.IsNil() {
		return sk.Image{}, false
	}
	imageCache.Store(src, si)
	return si, true
}

// skImageFromGo 把 image.Image 转成 premultiplied RGBA8888 的 sk.Image。
func skImageFromGo(img image.Image) sk.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return sk.Image{}
	}
	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	// Skia RGBA_8888 期望 premultiplied alpha。
	premulRGBA(rgba.Pix)
	rowBytes := rgba.Stride
	info := sk.NewImageInfo().
		MakeWH(int32(w), int32(h)).
		MakeColorType(sk.ColorTypeRGBA_8888).
		MakeAlphaType(sk.AlphaTypePremul)
	data := sk.DataMakeWithCopy(rgba.Pix, uint32(len(rgba.Pix)))
	return sk.ImagesRasterFromData(info, data, uint32(rowBytes))
}

// premulRGBA 就地做 alpha 预乘：c' = c * a / 255。
func premulRGBA(px []byte) {
	for i := 0; i+3 < len(px); i += 4 {
		a := uint16(px[i+3])
		px[i] = uint8((uint16(px[i]) * a) / 255)
		px[i+1] = uint8((uint16(px[i+1]) * a) / 255)
		px[i+2] = uint8((uint16(px[i+2]) * a) / 255)
	}
}

// objectFit 决定图片如何映射进布局盒；对应 CSS object-fit 的常用值。
type objectFit int

const (
	fitFill    objectFit = iota // 拉伸铺满（默认，等同无 object-fit）
	fitContain                  // 等比缩放完整放入，留白
	fitCover                    // 等比缩放铺满，溢出裁剪
)

func parseObjectFit(s string) objectFit {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "contain":
		return fitContain
	case "cover":
		return fitCover
	default:
		return fitFill
	}
}

// imageDstRect 按 object-fit 计算绘制目标矩形。
func imageDstRect(box layoutBox, iw, ih int, fit objectFit) sk.Rect {
	bx, by, bw, bh := box.X, box.Y, box.W, maxF(box.H, 1)
	if iw <= 0 || ih <= 0 {
		return sk.RectMakeXYWH(bx, by, bw, bh)
	}
	if fit == fitFill {
		return sk.RectMakeXYWH(bx, by, bw, bh)
	}
	ar := float32(iw) / float32(ih)
	bar := bw / bh
	switch fit {
	case fitContain:
		if ar > bar { // 图更宽：按宽适配
			h := bw / ar
			return sk.RectMakeXYWH(bx, by+(bh-h)/2, bw, h)
		}
		w := bh * ar
		return sk.RectMakeXYWH(bx+(bw-w)/2, by, w, bh)
	case fitCover:
		if ar > bar { // 图更宽：按高铺满，裁剪左右
			w := bh * ar
			return sk.RectMakeXYWH(bx+(bw-w)/2, by, w, bh)
		}
		h := bw / ar
		return sk.RectMakeXYWH(bx, by+(bh-h)/2, bw, h)
	}
	return sk.RectMakeXYWH(bx, by, bw, bh)
}

// radiusFromProp 读取 border-radius（数字或 "8px"）。
func radiusFromProp(s string) float32 {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "px"))
	v, err := strconv.ParseFloat(s, 32)
	if err != nil || v < 0 {
		return 0
	}
	return float32(v)
}

// paintImage 画一张图到布局盒，支持 object-fit 与圆角裁剪。
func paintImage(p *painter, n *gxscene.Node, box layoutBox, src string, s style) {
	img, ok := loadSkImage(src)
	if !ok {
		return
	}
	fit := fitFill
	if v, _ := n.Props["object-fit"].(string); v != "" {
		fit = parseObjectFit(v)
	}
	radius := s.radius
	if v, _ := n.Props["border-radius"].(string); v != "" {
		if r := radiusFromProp(v); r > 0 {
			radius = r
		}
	}
	dst := imageDstRect(box, int(img.Width()), int(img.Height()), fit)
	needClip := radius > 0
	if needClip {
		p.canvas.Save()
		r := radiusClamp(radius, box.W, maxF(box.H, 1))
		path := sk.NewPath()
		path.AddRoundRect1(sk.RectMakeXYWH(box.X, box.Y, box.W, maxF(box.H, 1)), r, r, sk.PathDirectionCW)
		p.canvas.ClipPath(path, sk.ClipOpIntersect, true)
	}
	paint := sk.NewPaint()
	paint.SetAntiAlias(true)
	alpha := s.opacity
	if alpha <= 0 || alpha > 1 {
		alpha = 1
	}
	paint.SetAlpha(uint32(alpha * 255))
	srcRect := sk.RectMakeWH(float32(img.Width()), float32(img.Height()))
	sampling := sk.NewSamplingOptions()
	p.canvas.DrawImageRect(img, srcRect, dst, sampling, paint, sk.CanvasSrcRectConstraintStrict)
	paint.Delete()
	if needClip {
		p.canvas.Restore()
	}
	*p.log = append(*p.log, DrawOp{Kind: "image", Name: n.Kind, X: box.X, Y: box.Y, W: box.W, H: box.H})
}
