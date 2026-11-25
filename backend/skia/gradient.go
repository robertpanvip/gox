package skia

import (
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	sk "github.com/pekim/go-skia"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// 本文件让 Skia 后端理解编辑器用 CSS linear-gradient 编码的语法高亮。
//
// 背景：编辑器每行代码的颜色由 buildGradient 编码成一条 CSS 渐变：
//
//	linear-gradient(90deg, #c586c0 0ch 7ch, #d4d4d4 7ch 8ch, ...)
//
// 每个色段是「颜色 起始格 结束格」，格(ch)是等宽字体下的字符宽度单位。
// Web 端由浏览器原生 background-clip:text 把渐变裁到文字上（GPU 合成，Go 侧零成本）。
// Skia 后端没有 CSS 引擎，go-skia 也没有渐变 shader，这里把同一条渐变解析回带颜色的
// 文本分段（gxscene.RichText），交给现成的 paintRichText 逐段绘制。
//
// 由此两端消费同一份 gradient 数据：编辑器源码只维护一个 getter（lineCodeGradient），
// 不再有「Web 走 gradient、桌面走 rich-text」的双轨分支，真正三端统一。

// gradStop 是 CSS 渐变里的一个着色段：[startCh, endCh) 个字符格用 color 着色。
type gradStop struct {
	color          string
	startCh, endCh int
}

// parseGradient 把 linear-gradient(...) 字符串解析成有序色段。
// 无法识别的格式返回 nil，调用方据此回退到继承色。
func parseGradient(s string) []gradStop {
	if !strings.HasPrefix(s, "linear-gradient(") || !strings.HasSuffix(s, ")") {
		return nil
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(s, "linear-gradient("), ")")
	parts := strings.Split(inner, ",")
	stops := make([]gradStop, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		// 跳过角度项（如 90deg）与空项。
		if p == "" || strings.HasSuffix(p, "deg") {
			continue
		}
		f := strings.Fields(p)
		if len(f) < 3 {
			continue
		}
		start, ok1 := parseCh(f[1])
		end, ok2 := parseCh(f[2])
		if !ok1 || !ok2 || end <= start {
			continue
		}
		stops = append(stops, gradStop{color: f[0], startCh: start, endCh: end})
	}
	return stops
}

// parseCh 解析 "7ch" → (7, true)。
func parseCh(s string) (int, bool) {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "ch"))
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// ---- 通用 CSS linear-gradient 背景渲染 ----
//
// go-skia 不暴露渐变 shader，这里用「逐像素条带」近似：把渐变轴切成若干条带，
// 每条带算出一个插值颜色画一个矩形。对水平/垂直渐变足够平滑，且是纯 CPU 绘制、
// 无 shader 依赖。所有 div 的 bg=linear-gradient(...) 都走这条路径。

// gradPos 是渐变色标的位置：数值 + 单位（%/ch/px/空）。
type gradPos struct {
	value float64
	unit  string
}

// colorStop 是通用渐变的一个色标：颜色 + 可选起止位置。
// 双位置（如 "#a 0ch 7ch"）表示硬色块，起止同色不插值；
// 单位置（如 "#a 10%"）表示渐变锚点，相邻色标间平滑插值。
type colorStop struct {
	color            string
	start, end       gradPos
	hasStart, hasEnd bool
}

// linearGradient 是解析后的通用线性渐变：方向角（度）+ 色标序列。
type linearGradient struct {
	angle float64 // CSS 角度：90deg 从左到右，180deg 从上到下；默认 180
	stops []colorStop
}

// parseLinearGradient 解析任意 CSS linear-gradient(...) 为通用结构。
// 支持角度（deg）、turn、to 关键字、颜色 + 0~2 个位置（%/ch/px/无单位）。
// 无法识别返回 nil。
func parseLinearGradient(s string) *linearGradient {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "linear-gradient(") || !strings.HasSuffix(s, ")") {
		return nil
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(s, "linear-gradient("), ")")
	parts := splitTopLevel(inner, ',')
	if len(parts) == 0 {
		return nil
	}
	g := &linearGradient{angle: 180}
	// 首段可能是方向：90deg / 0.25turn / to right 等。
	first := strings.TrimSpace(parts[0])
	if a, ok := parseGradientAngle(first); ok {
		g.angle = a
		parts = parts[1:]
	}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		f := strings.Fields(p)
		// 首字段必须是颜色。
		if _, ok := parseColor(f[0]); !ok {
			continue
		}
		cs := colorStop{color: f[0]}
		if len(f) >= 2 {
			if pos, ok := parseGradPos(f[1]); ok {
				cs.start, cs.hasStart = pos, true
			}
		}
		if len(f) >= 3 {
			if pos, ok := parseGradPos(f[2]); ok {
				cs.end, cs.hasEnd = pos, true
			}
		}
		g.stops = append(g.stops, cs)
	}
	if len(g.stops) == 0 {
		return nil
	}
	return g
}

// splitTopLevel 按 sep 切分，忽略括号内的分隔符（避免颜色函数 rgb(...) 被切开）。
func splitTopLevel(s string, sep byte) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case sep:
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}

// parseGradientAngle 解析渐变方向，返回 CSS 角度（度，顺时针，0=向上）。
// 支持 "90deg" / "0.25turn" / "to right|left|top|bottom" 及其组合。
func parseGradientAngle(s string) (float64, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0, false
	}
	if strings.HasSuffix(s, "deg") {
		v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(s, "deg")), 64)
		return v, err == nil
	}
	if strings.HasSuffix(s, "turn") {
		v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(s, "turn")), 64)
		return v * 360, err == nil
	}
	if strings.HasPrefix(s, "to ") {
		// CSS: to top=0, to right=90, to bottom=180, to left=270。
		dirs := strings.Fields(s[3:])
		angle := 0.0
		for _, d := range dirs {
			switch d {
			case "top":
				angle = 0
			case "right":
				angle = 90
			case "bottom":
				angle = 180
			case "left":
				angle = 270
			}
		}
		// 对角：to bottom right 等取两向平均。
		if len(dirs) == 2 {
			a, b := 0.0, 0.0
			for _, d := range dirs {
				switch d {
				case "top":
					a = 0
				case "right":
					a = 90
				case "bottom":
					a = 180
				case "left":
					a = 270
				}
				b += a
			}
			angle = b / 2
		}
		return angle, true
	}
	return 0, false
}

// parseGradPos 解析色标位置："50%" / "7ch" / "10px" / "0.5" → gradPos。
// 无单位数值按 CSS 视为归一化比例（0~1）。
func parseGradPos(s string) (gradPos, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return gradPos{}, false
	}
	for _, u := range []string{"%", "ch", "px", "em", "rem"} {
		if strings.HasSuffix(s, u) {
			num := strings.TrimSpace(strings.TrimSuffix(s, u))
			v, err := strconv.ParseFloat(num, 64)
			if err == nil {
				return gradPos{value: v, unit: u}, true
			}
			return gradPos{}, false
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err == nil {
		return gradPos{value: v, unit: ""}, true
	}
	return gradPos{}, false
}

// gradientCells 必须与编辑器 buildGradient 使用的 runeWidth 完全一致，
// 否则色段的 ch 边界会与文本 rune 错位、颜色整体偏移。
func gradientCells(r rune) int {
	if r >= 0x2e80 {
		return 2
	}
	return 1
}

// gradientToRich 按色段的 ch 边界把纯文本切成着色分段。
// 相邻同色 rune 合并成一段，减少 paintRichText 的绘制次数。
func gradientToRich(grad, text string) gxscene.RichText {
	stops := parseGradient(grad)
	if len(stops) == 0 {
		return nil
	}
	var out gxscene.RichText
	ch := 0
	si := 0
	for _, r := range text {
		for si < len(stops) && ch >= stops[si].endCh {
			si++
		}
		col := ""
		if si < len(stops) && ch >= stops[si].startCh && ch < stops[si].endCh {
			col = stops[si].color
		}
		if n := len(out); n > 0 && out[n-1].Color == col {
			out[n-1].Text += string(r)
		} else {
			out = append(out, gxscene.RichSpan{Text: string(r), Color: col})
		}
		ch += gradientCells(r)
	}
	return out
}

// 进程级缓存：gradient+text → 着色分段。语法高亮的行内容重复出现（同一示例反复渲染），
// 缓存让每帧只做一次 map 查找而非逐 rune 重解析。超过上限整体清空，防止长时间编辑无界增长。
var gradientRichCache sync.Map // map[string]gxscene.RichText
var gradientRichCount int64

const gradientRichMax = 2048

func cachedGradientRich(grad, text string) gxscene.RichText {
	key := grad + "\x00" + text
	if v, ok := gradientRichCache.Load(key); ok {
		return v.(gxscene.RichText)
	}
	rt := gradientToRich(grad, text)
	if atomic.LoadInt64(&gradientRichCount) >= gradientRichMax {
		gradientRichCache.Range(func(k, _ any) bool {
			gradientRichCache.Delete(k)
			return true
		})
		atomic.StoreInt64(&gradientRichCount, 0)
	}
	gradientRichCache.Store(key, rt)
	atomic.AddInt64(&gradientRichCount, 1)
	return rt
}

// gradientAxisLen 返回渐变轴在 CSS 角度下的像素长度（用于把 px/ch 位置归一化）。
// CSS 角度：0=向上、90=向右。水平分量 |sin|、垂直分量 |cos|。
func gradientAxisLen(angle float64, w, h float32) float64 {
	rad := angle * math.Pi / 180
	return math.Abs(math.Sin(rad))*float64(w) + math.Abs(math.Cos(rad))*float64(h)
}

// posFraction 把色标位置转成 0..1 比例。
// % → /100；px/ch/em → /axisLen；无单位 → 视为比例本身。
func posFraction(p gradPos, axisLen float64) float64 {
	switch p.unit {
	case "%":
		return p.value / 100
	case "px", "ch", "em", "rem":
		if axisLen > 0 {
			return p.value / axisLen
		}
		return 0
	default:
		return p.value
	}
}

// sampleGradient 按 t∈[0,1] 在渐变上取色，返回 ARGB。
// 双位置硬段（hasStart&&hasEnd）在同色内保持纯色；单位置色标之间线性插值 RGB。
func sampleGradient(g *linearGradient, t float64, axisLen float64) uint32 {
	type stopPos struct {
		pos float64
		col uint32
	}
	// 展开色标为 (位置, 颜色)：双位置展开成 [start,end] 两个同色锚点（硬段）。
	var expanded []stopPos
	for _, cs := range g.stops {
		c, ok := parseColor(cs.color)
		if !ok {
			continue
		}
		if cs.hasStart && cs.hasEnd {
			s := posFraction(cs.start, axisLen)
			e := posFraction(cs.end, axisLen)
			expanded = append(expanded, stopPos{s, c}, stopPos{e, c})
		} else if cs.hasStart {
			expanded = append(expanded, stopPos{posFraction(cs.start, axisLen), c})
		} else {
			expanded = append(expanded, stopPos{0, c})
		}
	}
	if len(expanded) == 0 {
		return argb(255, 255, 255, 255)
	}
	if len(expanded) == 1 {
		return expanded[0].col
	}
	if t <= expanded[0].pos {
		return expanded[0].col
	}
	if t >= expanded[len(expanded)-1].pos {
		return expanded[len(expanded)-1].col
	}
	for i := 0; i < len(expanded)-1; i++ {
		a, b := expanded[i], expanded[i+1]
		if t >= a.pos && t <= b.pos {
			span := b.pos - a.pos
			if span <= 0 {
				return b.col
			}
			k := (t - a.pos) / span
			return lerpARGB(a.col, b.col, k)
		}
	}
	return expanded[len(expanded)-1].col
}

// lerpARGB 在两个 0xAARRGGBB 颜色间线性插值。
func lerpARGB(a, b uint32, k float64) uint32 {
	ar, ag, ab, aa := float64(a>>16&0xff), float64(a>>8&0xff), float64(a&0xff), float64(a>>24&0xff)
	br, bg, bb, ba := float64(b>>16&0xff), float64(b>>8&0xff), float64(b&0xff), float64(b>>24&0xff)
	r := uint32(ar + (br-ar)*k + 0.5)
	g := uint32(ag + (bg-ag)*k + 0.5)
	bl := uint32(ab + (bb-ab)*k + 0.5)
	al := uint32(aa + (ba-aa)*k + 0.5)
	return argb(al, r, g, bl)
}

// gradientPoint 把归一化位置 t 映射到盒子内的 (x,y) 采样线起点。
// 返回渐变轴上该位置处、垂直于轴的线段两端，供条带绘制。
func gradientStrip(t float64, angle float64, box layoutBox) (x, y, w, h float32) {
	// 只支持水平（90/270）与垂直（0/180）主轴渐变，对角线退化为包围盒投影。
	rad := angle * math.Pi / 180
	dx, dy := math.Sin(rad), -math.Cos(rad) // CSS：0=上(-y)、90=右(+x)
	if math.Abs(dx) >= math.Abs(dy) {
		// 水平：条带是竖线。
		w = float32(math.Max(1, float64(box.W)/128))
		x = box.X + float32(t)*box.W - w/2
		if w > box.W {
			w = box.W
		}
		y, h = box.Y, box.H
	} else {
		// 垂直：条带是横线。
		h = float32(math.Max(1, float64(box.H)/128))
		y = box.Y + float32(t)*box.H - h/2
		if h > box.H {
			h = box.H
		}
		x, w = box.X, box.W
	}
	return
}

// paintGradientBackground 用条带近似绘制 CSS 线性渐变背景。
func (p *painter) paintGradientBackground(g *linearGradient, box layoutBox, radius float32) {
	axisLen := gradientAxisLen(g.angle, box.W, box.H)
	// 条带数量：沿主轴每 ~2px 一条，上下限 8..256，兼顾平滑与开销。
	major := box.W
	if math.Abs(math.Cos(g.angle*math.Pi/180)) >= math.Abs(math.Sin(g.angle*math.Pi/180)) {
		major = box.H
	}
	n := int(major / 2)
	if n < 8 {
		n = 8
	}
	if n > 256 {
		n = 256
	}
	paint := sk.NewPaint()
	defer paint.Delete()
	paint.SetAntiAlias(true)
	for i := 0; i < n; i++ {
		t := (float64(i) + 0.5) / float64(n)
		paint.SetColor(sk.Color(sampleGradient(g, t, axisLen)))
		x, y, w, h := gradientStrip(t, g.angle, box)
		p.canvas.DrawRect(sk.RectMakeXYWH(x, y, w+0.6, h+0.6), paint) // +0.6 消除条带缝隙
	}
}
