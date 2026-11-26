package ui

import (
	"strconv"
	"strings"
	"testing"
)

// TestTerminalTabsReadableSize 底部终端 tab 标签与输出字号必须 >=12px：
// FontFootnote(10px) 加粗细笔画在任何缩放下都会糊成碎点，视觉上像
// "Terminal 每个字母中间有空格"。这是可读性回归的钉死断言。
func TestTerminalTabsReadableSize(t *testing.T) {
	v := TerminalView()
	var sizes []any
	collectProp(v.Root(), "font-size", &sizes)
	if len(sizes) == 0 {
		t.Fatal("TerminalView 未找到 font-size 属性")
	}
	for _, raw := range sizes {
		s, _ := raw.(string)
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			continue // 非数值（如 calc/百分比）不参与钉死
		}
		if n < 12 {
			t.Errorf("终端区字号过小: %q（<12px 会渲染成碎裂笔画，tab 文字显像字间有空格）", s)
		}
	}
}

// TestTerminalOutputMonospace 终端输出区必须声明等宽字体栈：
// 控制台输出混排对齐依赖等宽；sans 比例字体会让输出字符间距观感参差。
func TestTerminalOutputMonospace(t *testing.T) {
	v := TerminalView()
	var fams []any
	collectProp(v.Root(), "font-family", &fams)
	found := false
	for _, raw := range fams {
		s, _ := raw.(string)
		if strings.Contains(s, "monospace") || strings.Contains(s, "Mono") {
			found = true
		}
	}
	if !found {
		t.Errorf("终端输出区未声明等宽字体栈，实际 font-family: %v", fams)
	}
}
