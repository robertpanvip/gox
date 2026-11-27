// 辅助程序：打印编辑器等宽字体的实测窄字符 advance，供 gui_e2e.sh 计算
// 点击坐标用。与装配层 ui.SetCharWidth 的注入口径完全一致。
package main

import (
	"fmt"

	skia "github.com/robertpanvip/gox-backend-rust"
	"github.com/robertpanvip/gox/widgets"
)

func main() {
	narrow, _ := skia.MeasureEditorAdvances(widgets.EditorCodeFont(), widgets.EditorCodeFontSize)
	fmt.Printf("%.4f", narrow)
}
