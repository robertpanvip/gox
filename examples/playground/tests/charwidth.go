// 辅助程序：打印桌面编辑器等宽字体的实测窄字符 advance，供 gui_e2e.sh 计算
// 点击坐标用。与 desktop/main.go 的 ui.SetCharWidth 注入口径完全一致。
package main

import (
	"fmt"

	skia "github.com/robertpanvip/gox-backend-skia"
	"github.com/robertpanvip/gox/widgets"
)

func main() {
	narrow, wide := skia.MeasureEditorAdvances(widgets.EditorCodeFont(), widgets.EditorCodeFontSize)
	fmt.Printf("%.4f %.4f", narrow, wide)
}
