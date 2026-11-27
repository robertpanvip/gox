package ui

// 本文件提供编辑器符号兼容转发：Editor 抽离到 widgets 后，原 ui 包级符号
// （回归测试与装配层仍在使用）统一委托给 widgets.EditorState 实例 editorState。

import (
	"strings"

	"github.com/robertpanvip/gox-core/widgets"
)

func PrefixWidth(s string, col int) float64              { return editorState.PrefixWidth(s, col) }
func HasSelection() bool                                 { return editorState.HasSelection() }
func setSource(text string)                              { editorState.SetSource(text) }
func EditorSetSource(text string)                        { editorState.SetSource(text) }
func EditorLineSource(idx int) string                   { return splitNth(editorState.Text(), idx) }
func rowVersion(idx int) int                             { return editorState.RowVersion(idx) }
func clearHistory()                                      { editorState.ClearHistory() }
func InsertText(v string, ln, col int, ins string) (string, int) { return widgets.InsertText(v, ln, col, ins) }
func BackspaceAt(v string, ln, col int) (string, int, int)       { return widgets.BackspaceAt(v, ln, col) }
func NewlineAt(v string, ln, col int) (string, int, int)         { return widgets.NewlineAt(v, ln, col) }
func JoinLines(ls []string) string                        { return widgets.JoinLines(ls) }
func Highlight(line string) []widgets.Token               { return widgets.Highlight(line) }

// splitNth 返回 v 的第 idx 行（诊断日志用）。
func splitNth(v string, idx int) string {
	lines := strings.Split(v, "\n")
	if idx < 0 || idx >= len(lines) {
		return ""
	}
	return lines[idx]
}
