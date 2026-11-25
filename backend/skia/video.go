package skia

// video 原语（ffmpeg 子进程解码）：后端读取 src，spawn ffmpeg 解码成 RGBA 帧，
// 画在 video 节点盒子里。新帧到达只请求重绘、不触发布局重算。
//
// 与 canvas/img 一样，video 在场景树里是普通 NodeDiv（Kind="video"），参与
// 布局/定位/裁剪/伪状态。poster 在首帧到达前占位。无 ffmpeg 时画 poster 降级。

import (
	"image"
	"strconv"
	"sync"

	sk "github.com/robertpanvip/gox-skia/compat"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	gxvideo "github.com/robertpanvip/gox/runtime/video"
)

// videoManager 进程级管理器：key(nodeID|playbackID|seek|src) → 解码器。
// 相同 key 复用解码进程；key 变化重建。响应式重绘不会反复 spawn ffmpeg。
type videoManager struct {
	mu      sync.Mutex
	streams map[string]*videoDecoder
	byNode  map[gxscene.NodeID]string
	onEvent func(videoEvent)
}

var videos = &videoManager{
	streams: make(map[string]*videoDecoder),
	byNode:  make(map[gxscene.NodeID]string),
}

// SetVideoEventHandler 注册视频事件回调（state/meta/time → host），传 nil 清空。
func SetVideoEventHandler(fn func(nodeID, kind, text string)) {
	videos.mu.Lock()
	videos.onEvent = nil
	if fn != nil {
		videos.onEvent = func(e videoEvent) { fn(e.NodeID, e.Kind, e.Text) }
	}
	videos.mu.Unlock()
}

// syncFromTree 遍历场景树，为每个 video 节点确保有对应解码器；清理已消失节点。
// 由 Window.Draw 在绘制前调用。
func (m *videoManager) syncFromTree(root *gxscene.Node) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := make(map[gxscene.NodeID]bool)
	var walk func(*gxscene.Node)
	walk = func(n *gxscene.Node) {
		if n == nil {
			return
		}
		if n.Kind == gxvideo.Kind && n.ID != gxscene.NoNode {
			seen[n.ID] = true
			src := videoProp(n.Props, gxvideo.PropSrc)
			pbid := videoProp(n.Props, gxvideo.PropPlaybackID)
			seek := videoProp(n.Props, gxvideo.PropSeekTo)
			key := n.ID.String() + "|" + pbid + "|" + seek + "|" + src
			if m.byNode[n.ID] != key {
				if oldKey, ok := m.byNode[n.ID]; ok {
					if old := m.streams[oldKey]; old != nil {
						old.stop()
						delete(m.streams, oldKey)
					}
				}
				m.byNode[n.ID] = key
				playing := videoBool(n.Props, gxvideo.PropPlaying)
				autoplay := videoBool(n.Props, gxvideo.PropAutoplay) || playing
				if v, ok := n.Props[gxvideo.PropPlaying]; ok {
					playing = videoBool(map[string]any{"v": v}, "v")
				}
				seekSec, _ := strconv.ParseFloat(seek, 64)
				dec := newVideoDecoder(n.ID.String(), src, pbid, autoplay,
					videoBool(n.Props, gxvideo.PropLoop),
					videoBool(n.Props, gxvideo.PropMuted), seekSec, m.onEvent)
				// autoplay=false 且无 playing 意图时初始暂停。
				if !autoplay {
					dec.setPaused(true)
				}
				m.streams[key] = dec
			} else if dec := m.streams[key]; dec != nil {
				if v, ok := n.Props[gxvideo.PropPlaying]; ok {
					dec.setPaused(!videoBool(map[string]any{"v": v}, "v"))
				}
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	for id, key := range m.byNode {
		if !seen[id] {
			if dec := m.streams[key]; dec != nil {
				dec.stop()
			}
			delete(m.streams, key)
			delete(m.byNode, id)
		}
	}
}

// frameFor 返回节点当前帧。
func (m *videoManager) frameFor(id gxscene.NodeID) (videoFrame, bool) {
	m.mu.Lock()
	key, ok := m.byNode[id]
	var dec *videoDecoder
	if ok {
		dec = m.streams[key]
	}
	m.mu.Unlock()
	if dec == nil {
		return videoFrame{}, false
	}
	return dec.currentFrame()
}

func videoProp(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	s, _ := m[k].(string)
	return s
}

func videoBool(m map[string]any, k string) bool {
	if m == nil {
		return false
	}
	switch v := m[k].(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1"
	default:
		return false
	}
}

// paintVideo 画当前帧到节点盒子（object-fit + 圆角裁剪）；无帧时画 poster。
func paintVideo(p *painter, n *gxscene.Node, box layoutBox, s style) {
	frame, ok := videos.frameFor(n.ID)
	if ok {
		img := skImageFromRGBA(frame.data, frame.w, frame.h)
		if !img.IsNil() {
			paintVideoImage(p, n, box, s, img, frame.w, frame.h)
			return
		}
	}
	paintVideoPoster(p, n, box, s)
}

// skImageFromRGBA 把解码帧（RGBA8888 非预乘）转成 sk.Image（预乘）。
func skImageFromRGBA(px []byte, w, h int) sk.Image {
	if w <= 0 || h <= 0 || len(px) < w*h*4 {
		return sk.Image{}
	}
	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	copy(rgba.Pix, px[:w*h*4])
	premulRGBA(rgba.Pix)
	info := sk.NewImageInfo().MakeWH(int32(w), int32(h)).
		MakeColorType(sk.ColorTypeRGBA_8888).MakeAlphaType(sk.AlphaTypePremul)
	data := sk.DataMakeWithCopy(rgba.Pix, uint32(len(rgba.Pix)))
	return sk.ImagesRasterFromData(info, data, uint32(rgba.Stride))
}

// paintVideoImage 用 object-fit 与圆角裁剪画一帧（与 paintImage 同构）。
func paintVideoImage(p *painter, n *gxscene.Node, box layoutBox, s style, img sk.Image, iw, ih int) {
	fit := fitFill
	if v := videoProp(n.Props, gxvideo.PropObjectFit); v != "" {
		fit = parseObjectFit(v)
	}
	radius := s.radius
	if v := videoProp(n.Props, "border-radius"); v != "" {
		if r := radiusFromProp(v); r > 0 {
			radius = r
		}
	}
	dst := imageDstRect(box, iw, ih, fit)
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
	srcRect := sk.RectMakeWH(float32(iw), float32(ih))
	p.canvas.DrawImageRect(img, srcRect, dst, sk.NewSamplingOptions(), paint, sk.CanvasSrcRectConstraintStrict)
	paint.Delete()
	if needClip {
		p.canvas.Restore()
	}
	*p.log = append(*p.log, DrawOp{Kind: "video", Name: n.Kind, X: box.X, Y: box.Y, W: box.W, H: box.H})
}

// paintVideoPoster 画封面占位（复用 imgutil 解码）。
func paintVideoPoster(p *painter, n *gxscene.Node, box layoutBox, s style) {
	poster := videoProp(n.Props, gxvideo.PropPoster)
	if poster == "" {
		return
	}
	img, ok := loadSkImage(poster)
	if !ok {
		return
	}
	paintVideoImage(p, n, box, s, img, int(img.Width()), int(img.Height()))
}
