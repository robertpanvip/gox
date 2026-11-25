package main

import (
	skia "github.com/robertpanvip/gox-backend-skia"
)

// inputRect 是窗口内容区坐标系下的物理像素矩形。
// 三端共用：Linux/XShape、Windows/WM_NCHITTEST、macOS/ignoresMouseEvents 判定
// 都基于同一份由 DrawLog 换算出的矩形集合，保证"哪些区域可点"语义一致。
type inputRect struct{ x, y, w, h int32 }

// contains 报告物理像素点 (px,py) 是否落在该矩形内（左闭右开）。
func (r inputRect) contains(px, py int32) bool {
	return px >= r.x && px < r.x+r.w && py >= r.y && py < r.y+r.h
}

// drawLogToInputRects 把本帧 DrawLog 转成输入形状矩形（物理像素）。
// 跳过完全透明的内容（alpha=0 的 fragment/隐藏节点），只保留真正画出的
// 不透明矩形/文字/图片；空结果表示整窗鼠标穿透。
//
// 口径用 DrawLog 而非 hitbox：hitbox 对所有节点（含 #root/#frag 透明容器、
// 无 bg 的纯透明 div）都记录，若据此设输入区会把整窗算作可点、失去穿透；
// DrawLog 只含真正画出的 rect/text/fragment，才是"视觉上有内容"的区域。
func drawLogToInputRects(log skia.DrawLog, dpr float32) []inputRect {
	rects := make([]inputRect, 0, len(log))
	for _, op := range log {
		if (op.ARGB >> 24) == 0 {
			continue
		}
		x, y := int32(op.X*dpr), int32(op.Y*dpr)
		w, hh := int32(op.W*dpr), int32(op.H*dpr)
		if w > 0 && hh > 0 {
			rects = append(rects, inputRect{x, y, w, hh})
		}
	}
	return rects
}
