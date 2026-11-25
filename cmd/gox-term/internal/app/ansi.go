package app

import (
	"strconv"
	"strings"
)

// ansiLine 是一行"可显示内容"：已剥离光标/清屏等控制序列，仅保留 SGR 颜色。
// 这样拼进 bubbletea 的 View 不会干扰它的光标定位。
type ansiLine struct {
	text string // 含 SGR 转义（颜色），不含其它控制序列
}

// ansiToLines 把 PTY 原始字节流转成可显示的行。
//
// 处理规则（务实取舍，非完整终端模拟）：
//   - \r\n 与 \n 都换行；单独 \r 视为回到行首（这里用空格覆盖式简化：保留最后一段）
//   - CSI 序列：保留 SGR（m 结尾，颜色）；其余（光标移动 H/A/B/C/D、擦除 K/J、清屏 2J 等）丢弃
//   - OSC 序列（标题栏设置，\x1b]...\x07）丢弃
//   - 退格 \b 删除上一个可见字符
//   - 其它控制字符丢弃
func ansiToLines(data []byte) []ansiLine {
	s := string(data)
	var lines []ansiLine
	var cur strings.Builder
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '\r':
			// 回车：若下一个是 \n，交给 \n 处理；否则清空当前行（覆盖式简化）。
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
				continue
			}
			cur.Reset()
			i++
		case c == '\n':
			lines = append(lines, ansiLine{text: cur.String()})
			cur.Reset()
			i++
		case c == '\b':
			// 退格：删掉上一个字节（对中文可能不完整，够用）。
			v := cur.String()
			if len(v) > 0 {
				r := []rune(v)
				if len(r) > 0 {
					cur.Reset()
					cur.WriteString(string(r[:len(r)-1]))
				}
			}
			i++
		case c == 0x1b && i+1 < len(s) && s[i+1] == '[':
			// CSI 序列。
			j := i + 2
			for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
				j++
			}
			if j < len(s) {
				seq := s[i : j+1]
				final := s[j]
				if final == 'm' {
					// SGR：保留（颜色），但跳过空序列。
					cur.WriteString(seq)
				}
				// 其它 CSI（光标/擦除/清屏）统一丢弃；清屏 2J/H 时清空当前行。
				if (final == 'J' || final == 'H') && strings.Contains(seq, "2") {
					cur.Reset()
				}
				i = j + 1
			} else {
				i++
			}
		case c == 0x1b && i+1 < len(s) && s[i+1] == ']':
			// OSC 序列，吞到 BEL 或 ST。
			j := i + 2
			for j < len(s) && s[j] != 0x07 && !(s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\') {
				j++
			}
			if j < len(s) {
				if s[j] == 0x07 {
					i = j + 1
				} else {
					i = j + 2
				}
			} else {
				i = len(s)
			}
		case c == 0x1b:
			// 孤立 ESC：跳过后面的一个字符。
			i += 2
		case c < 0x20 && c != '\t':
			// 其它控制字符丢弃。
			i++
		default:
			cur.WriteByte(c)
			i++
		}
	}
	lines = append(lines, ansiLine{text: cur.String()})
	return lines
}

// visibleWidth 返回去掉 SGR 后的可见宽度（rune 数近似）。
func (l ansiLine) visibleText() string {
	return stripSGR(l.text)
}

func stripSGR(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// parseSGRColor 从 SGR 序列里提取一个近似的前景 ANSI 色号（用于反显等场景占位）。
// 这里仅作诊断用，渲染由 lipgloss 样式负责。
func parseSGRColor(seq string) (int, bool) {
	if len(seq) < 3 || seq[len(seq)-1] != 'm' {
		return 0, false
	}
	parts := strings.Split(seq[2:len(seq)-1], ";")
	for _, p := range parts {
		if n, err := strconv.Atoi(p); err == nil && n >= 30 && n <= 37 {
			return n - 30, true
		}
	}
	return 0, false
}
