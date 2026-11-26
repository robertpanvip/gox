package gxcomplete

// 类型感知导航：跳转定义与 hover 签名。与成员补全共用同一个持久 Importer
// 类型图（Session.imp），因此 std 包类型只构建一次。所有环节不适用返回 ok=false，
// 调用方优雅降级（例如回退到符号表跳转 / 不显示 hover）——零行为回归。

import (
	"path/filepath"
	"runtime"
	"strings"

	"github.com/robertpanvip/gox/internal/typecheck"
	"github.com/robertpanvip/gox/pkg/compiler"
	"github.com/robertpanvip/gox/pkg/sourcemap"
)

// Def 是一次跳转定义的结果。
type Def struct {
	Local   bool   // true=声明在用户当前 .gx 文件（GxLine/GxCol 为 0 基 .gx 坐标）
	GxLine  int    // 本地：.gx 0 基行
	GxCol   int    // 本地：.gx 0 基列
	Pkg     string // 外部：包路径（如 "strings"）
	File    string // 外部：包内文件名（如 "strings.go"）
	Line    int    // 外部：1 基声明行
	Snippet string // 外部：声明签名片段
	Doc     string // 外部：声明上方文档注释
}

// HoverInfo 是一次 hover 的结果。
type HoverInfo struct {
	Expr  string // 光标下的标识符文本
	Sig   string // 类型 / 函数签名
	Where string // 来源位置（如 "strings/strings.go:123"），本地为空
}

// navProbe 把 GX 光标坐标映射到 Go 源码字节偏移。col 为 0 基 rune 列，指向
// "最后输入字符之后"；前一 rune 必须是标识符字符（否则光标不在符号上）。
func (s *Session) navProbe(gxSrc string, line, col int) (off int, goSrc string, sm *sourcemap.SourceMap, ok bool) {
	if col <= 0 || line < 0 {
		return 0, "", nil, false
	}
	ls := strings.Split(gxSrc, "\n")
	if line >= len(ls) {
		return 0, "", nil, false
	}
	runes := []rune(ls[line])
	if col > len(runes) || !isIdentRune(runes[col-1]) {
		return 0, "", nil, false
	}
	res, err := compiler.Compile("input.gx", gxSrc)
	if err != nil || res == nil || res.GoSource == "" || res.InvalidGo || res.SourceMap == nil {
		return 0, "", nil, false
	}
	// GX 列用 rune 计数，SourceMap 的列按字节（gox 源码 UTF-8）；先换算。
	gxByteCol := len(string(runes[:col]))
	gp, ok := res.SourceMap.GxToGo(line+1, gxByteCol)
	if !ok {
		return 0, "", nil, false
	}
	o := typecheck.ByteOffset(res.GoSource, gp.Line, gp.Column)
	if o < 0 || o >= len(res.GoSource) || !isIdentByte(res.GoSource[o]) {
		return 0, "", nil, false
	}
	return o, res.GoSource, res.SourceMap, true
}

func isIdentRune(r rune) bool {
	return r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r >= 0x80
}

// Definition 求 GX 光标（0 基行、0 基 rune 列，指向符号末尾之后）处符号的声明。
func (s *Session) Definition(gxSrc string, line, col int) (Def, bool) {
	off, goSrc, sm, ok := s.navProbe(gxSrc, line, col)
	if !ok {
		return Def{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ensureImporter() {
		return Def{}, false
	}
	d, ok := s.imp.Definition(goSrc, off+1)
	if !ok {
		return Def{}, false
	}
	if d.Pkg == "" { // 本地符号：Go 坐标回投到 .gx
		gp, ok := sm.GoToGx(d.Line, d.Col)
		if !ok {
			return Def{}, false
		}
		return Def{Local: true, GxLine: gp.Line - 1, GxCol: gp.Column - 1}, true
	}
	return Def{
		Pkg: d.Pkg, File: d.File, Line: d.Line, Snippet: d.Snippet, Doc: d.Doc,
	}, true
}

// Hover 求 GX 光标处符号的签名提示。
func (s *Session) Hover(gxSrc string, line, col int) (HoverInfo, bool) {
	off, goSrc, _, ok := s.navProbe(gxSrc, line, col)
	if !ok {
		return HoverInfo{}, false
	}
	// 取光标下标识符文本（Go 源码字节区间）。
	start := off
	for start > 0 && isIdentByte(goSrc[start-1]) {
		start--
	}
	expr := goSrc[start : off+1]
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ensureImporter() {
		return HoverInfo{}, false
	}
	sig, where, ok := s.imp.TypeInfo(goSrc, off+1)
	if !ok || sig == "" {
		return HoverInfo{}, false
	}
	return HoverInfo{Expr: expr, Sig: sig, Where: where}, true
}

// StdSource 返回外部包源文件全文（peek 打开全文标签用）。
func (s *Session) StdSource(pkg, file string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ensureImporter() {
		return "", false
	}
	return s.imp.StdSource(pkg, file)
}

// StdAbsPath 返回 std 源文件在本机 GOROOT 的绝对路径（供展示来源）。
func StdAbsPath(pkg, file string) string {
	gr := runtime.GOROOT()
	if gr == "" {
		return ""
	}
	return filepath.Join(gr, "src", filepath.FromSlash(pkg), file)
}
