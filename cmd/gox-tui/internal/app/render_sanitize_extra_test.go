package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robertpanvip/gox-tui/internal/editor"
)

// GBK 文件打开后中文应正常显示（而非乱码），状态栏标记 GBK；
// 保存时应按 GBK 写回磁盘，不悄悄改编码。
func TestGBKFileOpenSaveRoundtrip(t *testing.T) {
	old := setTrueColor(t)
	defer old()

	dir := t.TempDir()
	src := filepath.Join(dir, "gbk.gx")
	// "中文注释" 的 GBK 字节（utf-8 写法会被判非法 → 走 GBK 解码）。
	gbk := editor.Encode("package main\n// 中文注释\n", editor.EncGBK)
	if _, enc := editor.Decode(gbk); enc != editor.EncGBK {
		t.Fatalf("GBK 字节应被识别为 GBK，实际 %q", enc)
	}
	os.WriteFile(src, gbk, 0o644)

	m := New(dir, true, []string{src})
	m.width, m.height = 80, 24
	m.focus = 1

	if enc := m.Current().Encoding; enc != editor.EncGBK {
		t.Errorf("打开 GBK 文件 Encoding=%q，应为 GBK", enc)
	}
	assertFrameFits(t, m)

	// 帧中应能找到正常中文（乱码形态的 U+FFFD 不允许出现）。
	if v := m.View(); strings.ContainsRune(v, '\ufffd') {
		t.Error("GBK 文件渲染出现替换符 U+FFFD，中文注释变成了乱码")
	}

	// 保存回写：磁盘字节应仍是 GBK，可原样解码回中文。
	m = sendKey(m, "end")
	m = sendKey(m, "ctrl+s")
	onDisk, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != string(gbk) {
		t.Errorf("GBK 文件保存后字节改变：\n原 %x\n新 %x", gbk, onDisk)
	}
}

// UTF-8 文件往返不变；BOM 打开时剥离。
func TestUTF8RoundtripAndBOM(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "utf8.gx")
	content := "package main\n// 中文\n"
	os.WriteFile(src, append([]byte{0xEF, 0xBB, 0xBF}, content...), 0o644)

	m := New(dir, true, []string{src})
	m.width, m.height = 80, 24
	if got := m.Current().Line(0); got != "package main" {
		t.Errorf("BOM 未剥离，第 0 行 = %q", got)
	}
	m = sendKey(m, "ctrl+s")
	onDisk, _ := os.ReadFile(src)
	if !strings.HasPrefix(string(onDisk), "package main") {
		t.Errorf("UTF-8 保存回写异常: %q", onDisk)
	}
}

// tab 与 CR 净化只影响显示，缓冲与保存保持原文。
func TestSanitizeDoesNotAlterBuffer(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "tabs.go")
	content := "func f() {\r\n\treturn 1\r\n}\r\n"
	os.WriteFile(src, []byte(content), 0o644)

	m := New(dir, true, []string{src})
	m.width, m.height = 80, 24
	m.focus = 1

	assertFrameFits(t, m)
	if v := m.View(); strings.ContainsAny(v, "\r\t") {
		t.Error("帧输出残留 \\r 或 \\t")
	}
	if got := m.Current().Text(); got != content {
		t.Errorf("净化不应改动缓冲内容，实际 %q", got)
	}
	m = sendKey(m, "ctrl+s")
	onDisk, _ := os.ReadFile(src)
	if string(onDisk) != content {
		t.Errorf("保存应原样保留 CRLF+tab，实际 %q", onDisk)
	}
}

// 含 tab 的行 hover 下划线区间映射后仍指向原单词。
func TestHoverSpanMappedAcrossTabs(t *testing.T) {
	old := setTrueColor(t)
	defer old()

	dir := t.TempDir()
	src := filepath.Join(dir, "hover.gx")
	// "\tfuncName"：单词 funcName 在原行 rune 列 [1,9)。
	os.WriteFile(src, []byte("\tfuncName = 1\n"), 0o644)

	m := New(dir, true, []string{src})
	m.width, m.height = 80, 24
	m.focus = 1
	// 直接注入 hover 区间（模拟鼠标悬停在 funcName 上）。
	m.navHover = &wordSpan{Line: 0, Start: 1, End: 9}

	v := m.View()
	// lipgloss 逐字符包裹下划线 ANSI，"funcName" 在流中不连续——
	// 先按含首字符样式定位行，再对去 ANSI 文本断言。
	line := ""
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, ";4mf") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatal("找不到带下划线的目标行")
	}
	plain := ansiRe.ReplaceAllString(line, "")
	if !strings.Contains(plain, "funcName") {
		t.Errorf("去 ANSI 后找不到 funcName: %q", plain)
	}
	// lipgloss 对 Underline(true) 逐字符包裹 ANSI（\x1b[4;…;4mX）。
	// funcName 8 个字符应全部带下划线；随后的空格不带。
	underlineCount := strings.Count(line, ";4m")
	if underlineCount != 8 {
		t.Errorf("tab 行下划线字符数 = %d，应为 8（funcName）——区间映射失效", underlineCount)
	}
	if strings.Contains(line, ";4m ") {
		t.Error("下划线越界：空格被加上了下划线")
	}
}
