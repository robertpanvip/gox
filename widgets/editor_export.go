package widgets

// 本文件提供 Editor 组件的公共导出 API：把 editor.gx 内部的纯编辑逻辑与
// 实例方法以稳定名称暴露出去，供装配层与回归测试复用。
// 这些是普通 Go 文件（不参与 gx 转译），包装 editor.gx 生成的内部符号。

// Token 是语法高亮分词结果。Color 为语义 token 名（keyword/str/comment/
// number/tag/attr/punct/text），渲染时由当前主题解析为具体色值。
type Token struct {
	Text  string
	Color string
}

// Highlight 把单行源码切成语法 token（语义名着色，主题无关，可缓存）。
func Highlight(line string) []Token {
	raw := highlight(line)
	out := make([]Token, len(raw))
	for i, t := range raw {
		out[i] = Token{Text: t.Text, Color: t.Color}
	}
	return out
}

// JoinLines 用 \n 拼接若干行。
func JoinLines(ls []string) string { return joinLines(ls) }

// EditorCodeFont 返回代码行的等宽字体栈（三端统一）。
// 装配层用它做本端字符宽度实测后注入 SetCharWidth/SetWideWidth：
// Web 端用 JS canvas 实测（playground/web/main.go），桌面端用
// skia.MeasureEditorAdvances 实测（cmd/gox-idea、playground/desktop）。
func EditorCodeFont() string { return editorCodeFont }

// EditorCodeFontSize 是代码行的字号（px），与 editor.gx 里 .gx-code-line
// 的 font-size 一致——字符宽度实测必须用同一字号才有意义。
const EditorCodeFontSize = 13

// InsertText 在 (ln,col) 插入 ins，返回新文本与新列（纯函数，rune 计列）。
func InsertText(v string, ln int, col int, ins string) (string, int) {
	return insertText(v, ln, col, ins)
}

// BackspaceAt 在 (ln,col) 退格，返回新文本、新行、新列。
func BackspaceAt(v string, ln int, col int) (string, int, int) {
	return backspaceAt(v, ln, col)
}

// NewlineAt 在 (ln,col) 断行，返回新文本、新行、新列。
func NewlineAt(v string, ln int, col int) (string, int, int) {
	return newlineAt(v, ln, col)
}

// SetSource 一次性替换整篇源码（只 bump 变化行版本，供示例切换/测试用）。
func (s *EditorState) SetSource(text string) { s.setSource(text) }

// RowVersion 返回第 idx 行的缓存版本号。
func (s *EditorState) RowVersion(idx int) int { return s.rowVersion(idx) }

// ClearHistory 清空撤销/重做栈（重置编辑器到已知状态）。
func (s *EditorState) ClearHistory() {
	s.undoStack = nil
	s.redoStack = nil
}
