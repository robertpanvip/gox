package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// FuzzUpdateAndView：随机按键/鼠标/缩放事件流轰炸 Model。
// 不变式（任何输入序列都必须成立）：
//  1. 不 panic；
//  2. active/treeSel/光标/scrollLn 全部处于合法范围；
//  3. View() 帧不超终端宽高（超宽→折行，超高→滚动，即闪动 bug 家族）。
func FuzzUpdateAndView(f *testing.F) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)

	keys := []string{
		"enter", "esc", "tab", "backspace", "delete", "up", "down", "left",
		"right", "home", "end", "pgup", "pgdown", "space", "f12",
		"ctrl+c", "ctrl+s", "ctrl+t", "ctrl+f", "ctrl+p", "ctrl+w",
		"ctrl+k", "ctrl+a", "ctrl+d", "ctrl+e", "ctrl+tab",
	}

	// 种子：覆盖历史 bug 场景（长文件名、CJK、缩放、浮层组合）。
	f.Add([]byte{0, 2, 5, 9, 14, 20, 30, 1, 1, 1})
	f.Add([]byte{3, 3, 3, 3, 30, 30, 30})
	f.Add([]byte{26, 26, 26, 0, 0, 31, 31, 12})

	f.Fuzz(func(t *testing.T, data []byte) {
		m := treeTestModel(t)
		m.width, m.height = 80, 24
		m.focus = 1

		for _, b := range data {
			var msg tea.Msg
			switch b % 8 {
			case 0, 1, 2: // 按键
				k := keys[int(b)%len(keys)]
				if t2, ok := specialKeys[k]; ok {
					msg = tea.KeyMsg{Type: t2}
				} else {
					msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
				}
			case 3: // 随机可见字符（含 CJK）
				runes := []rune{'a', '中', '文', '9', '_', '🐹', 'X'}
				msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{runes[int(b)/8%len(runes)]}}
			case 4, 5: // 鼠标
				msg = tea.MouseMsg{
					X:      int(b)*37%120 - 10,
					Y:      int(b)*13%40 - 5,
					Action: tea.MouseAction(int(b) % 3),
					Button: tea.MouseButton(int(b) % 6),
					Ctrl:   b%2 == 0,
				}
			case 6: // 缩放
				w := int(b)%140 + 8
				h := int(b)%50 + 4
				msg = tea.WindowSizeMsg{Width: w, Height: h}
				next, _ := m.Update(msg)
				m = next.(*Model)
				continue
			default: // 未知消息类型
				msg = completionTickMsg{seq: int(b)}
			}
			next, _ := m.Update(msg)
			m = next.(*Model)

			// 不变式 2：状态合法
			if m.active < 0 || m.active >= len(m.buffers) {
				t.Fatalf("输入字节 %d：active=%d 越界", b, m.active)
			}
			nodes := m.tree.nodes()
			if len(nodes) > 0 && (m.treeSel < 0 || m.treeSel >= len(nodes)) {
				t.Fatalf("输入字节 %d：treeSel=%d 越界", b, m.treeSel)
			}
			bf := m.Current()
			ln, col := bf.Cursor()
			if ln < 0 || ln >= bf.LineCount() {
				t.Fatalf("输入字节 %d：光标行 %d 越界", b, ln)
			}
			if col < 0 || col > len([]rune(bf.Line(ln))) {
				t.Fatalf("输入字节 %d：光标列 %d 越界", b, col)
			}
			if m.scrollLn < 0 {
				t.Fatalf("输入字节 %d：scrollLn=%d < 0", b, m.scrollLn)
			}
		}

		// 不变式 3：最终帧合规
		v := m.View()
		lines := strings.Split(v, "\n")
		if len(lines) > m.height {
			t.Fatalf("整帧 %d 行 > 高 %d", len(lines), m.height)
		}
		for i, line := range lines {
			if w := lipgloss.Width(line); w > m.width {
				t.Fatalf("第 %d 行宽 %d > 终端宽 %d", i, w, m.width)
			}
		}
	})
}
