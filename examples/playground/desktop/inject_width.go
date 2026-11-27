package main

// 本文件与 Web 端的 injectCharWidth 对齐：桌面端启动时用渲染端实测编辑器
// 等宽字体的单格宽度并注入编辑器，使 caret 渲染（PrefixWidth）与点击命中
// （PickColumn）用的字符宽度与文本绘制端一致。
//
// 不注入时编辑器沿用默认 7.83px/13.02px，而 Skia 用 DejaVu Sans Mono
// 13px 实测单格约 7.2px——偏差累积后，点击会落到错误的列、拖选到
// 视觉行尾只能复制到前面一部分文本。

import (
	rb "github.com/robertpanvip/gox-backend-rust"
	"github.com/robertpanvip/gox-playground/ui"
)

const editorCodeFontStack = "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace"

// editorFontWidths 是实测出的编辑器字符宽度：Narrow=ASCII 单格、Wide=CJK 单格。
type editorFontWidths struct {
	Narrow float64
	Wide   float64
}

// measureEditorFontWidths 用 V2 渲染端实测编辑器等宽字体的单格宽度。
// fontSize 必须与 widgets/editor.gx 里代码行的 font-size=13 一致。
// 测量走 glyphon 真实 shaping（与绘制端字形同源），语义与旧 Skia 路径对齐：
// 窄字符取样本均值抵消测量舍入误差，宽字符（CJK）单独实测——
// 很多 monospace 字体里 CJK 并非恰好 2×ASCII（与 Web 端 injectCharWidth 同口径）。
func measureEditorFontWidths() editorFontWidths {
	const fontSize = 13
	narrow, wide := rb.MeasureEditorAdvances(editorCodeFontStack, fontSize)
	return editorFontWidths{Narrow: float64(narrow), Wide: float64(wide)}
}

// injectEditorCharWidth 实测并注入编辑器字符宽度，使 caret 渲染与点击命中
// 的列↔像素换算与绘制端一致。必须在首帧渲染前调用。
func injectEditorCharWidth() {
	w := measureEditorFontWidths()
	if w.Narrow > 0 {
		ui.SetCharWidth(w.Narrow)
	}
	if w.Wide > 0 {
		ui.SetWideWidth(w.Wide)
	}
}
