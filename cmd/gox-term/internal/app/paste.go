package app

import (
	"strings"

	"github.com/robertpanvip/gox-term/internal/clipboard"
)

const (
	pasteBegin = "\x1b[200~" // bracketed paste mode start
	pasteEnd   = "\x1b[201~" // bracketed paste mode end
)

// wrapBracketedPaste 把 text 包成 bracketed-paste 序列。为了让 shell 的
// readline/zle 把它当一次粘贴（而非逐字符输入），必须成对包裹。
// 若文本里已含结束标记，先剔除，避免提前关闭粘贴模式。
func wrapBracketedPaste(text string) string {
	text = strings.ReplaceAll(text, pasteEnd, "")
	return pasteBegin + text + pasteEnd
}

// pasteToShell 把一段文本整体写入当前 shell：优先 bracketed paste；
// 若文本里含换行（多行粘贴），依然整体写入，由 shell 决定如何执行。
func (m *Model) pasteToShell(text string) {
	if text == "" {
		return
	}
	s := m.current()
	if s == nil {
		return
	}
	m.closeCompletion()
	m.clearSelection()
	_, _ = s.Write([]byte(wrapBracketedPaste(text)))
	m.scrollBack = 0
	m.autoScroll = true
	m.statusMsg = "已粘贴 " + itoa(len([]rune(text))) + " 字符"
}

// pasteFromClipboard 从本地剪贴板读取并粘贴（Ctrl+V 无 bracketed-paste 标记时的兜底）。
func (m *Model) pasteFromClipboard() {
	text := clipboard.ReadLocal()
	if text != "" {
		m.pasteToShell(text)
	} else {
		m.statusMsg = "剪贴板为空或不可读（可重试或检查 xclip/wl-paste）"
	}
}
