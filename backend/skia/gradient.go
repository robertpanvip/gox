package skia

import (
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

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
