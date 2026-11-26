package lsp

// 桌面端类型化导航底座：光标映射（含编译产物回传）、持久 Importer 惰性单例、
// definition/hover 的类型层实现、GOROOT 包清单。references/highlight/codeAction
// 在 actions.go 复用这里。

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/robertpanvip/gox/internal/typecheck"
	"github.com/robertpanvip/gox/pkg/compiler"
)

func navIdentByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b >= 0x80
}

// navImporter 惰性创建持久 Importer（与类型化补全共用 s.timp，std 类型图只建一次）。
func (s *Server) navImporter() *typecheck.Importer {
	if s.timp == nil {
		gr := runtime.GOROOT()
		if gr == "" {
			return nil
		}
		s.timp = typecheck.NewImporter(typecheck.DiskLookup(filepath.Join(gr, "src"), runtime.GOOS, runtime.GOARCH))
	}
	return s.timp
}

// navCompile 把 .gx 光标坐标（0 基行；character 指向光标之后）映射到生成 Go 中
// "光标前一字符"的字节偏移，并回传编译产物（SourceMap 供回投）。失败即 false。
func navCompile(gxSrc string, line, character int) (int, *compiler.Result, bool) {
	if character <= 0 {
		return 0, nil, false
	}
	ls := strings.Split(gxSrc, "\n")
	if line < 0 || line >= len(ls) || character > len(ls[line]) || !navIdentByte(ls[line][character-1]) {
		return 0, nil, false
	}
	res, err := compiler.Compile("input.gx", gxSrc)
	if err != nil || res == nil || res.GoSource == "" || res.InvalidGo || res.SourceMap == nil {
		return 0, nil, false
	}
	gp, ok := res.SourceMap.GxToGo(line+1, character)
	if !ok {
		return 0, nil, false
	}
	o := typecheck.ByteOffset(res.GoSource, gp.Line, gp.Column)
	if o < 0 || o >= len(res.GoSource) || !navIdentByte(res.GoSource[o]) {
		return 0, nil, false
	}
	return o, res, true
}

// navCursor 只需 go 源码文本的调用方便捷形态。
func navCursor(gxSrc string, line, character int) (int, string, bool) {
	o, res, ok := navCompile(gxSrc, line, character)
	if !ok {
		return 0, "", false
	}
	return o, res.GoSource, true
}

// typedDefinition 外部（std）符号 → 本机 GOROOT 真实文件 file:// URI；
// 本地符号留给符号表（同文档 .gx 坐标回跳更准），磁盘不存在的路径不伪造。
func (s *Server) typedDefinition(gxSrc string, line, character int) (Location, bool) {
	off, goSrc, ok := navCursor(gxSrc, line, character)
	if !ok {
		return Location{}, false
	}
	in := s.navImporter()
	if in == nil {
		return Location{}, false
	}
	d, ok := in.Definition(goSrc, off+1)
	if !ok || d.Pkg == "" {
		return Location{}, false
	}
	abs := filepath.Join(runtime.GOROOT(), "src", filepath.FromSlash(d.Pkg), d.File)
	if _, err := os.Stat(abs); err != nil {
		return Location{}, false
	}
	return Location{
		URI: "file://" + abs,
		Range: Range{
			Start: Position{Line: d.Line - 1, Character: d.Col - 1},
			End:   Position{Line: d.Line - 1, Character: d.Col - 1},
		},
	}, true
}

// typedHover 符号表缺席时用真实签名兜底（std 符号 hover 的核心增量）。
func (s *Server) typedHover(gxSrc string, line, character int) (Hover, bool) {
	off, goSrc, ok := navCursor(gxSrc, line, character)
	if !ok {
		return Hover{}, false
	}
	in := s.navImporter()
	if in == nil {
		return Hover{}, false
	}
	sig, where, ok := in.TypeInfo(goSrc, off+1)
	if !ok || sig == "" {
		return Hover{}, false
	}
	start := off
	for start > 0 && navIdentByte(goSrc[start-1]) {
		start--
	}
	value := "**" + goSrc[start:off+1] + "** — `" + sig + "`"
	if where != "" {
		value += "\n\n_" + where + "_"
	}
	return Hover{Contents: MarkupContent{Kind: "markdown", Value: value}}, true
}

var (
	navPkgsOnce sync.Once
	navPkgs     []string
)

// diskPkgNames GOROOT/src 顶层可导入包清单（cmd/vendor/internal/点开头剔除）。
func diskPkgNames() []string {
	navPkgsOnce.Do(func() {
		gr := runtime.GOROOT()
		if gr == "" {
			return
		}
		entries, err := os.ReadDir(filepath.Join(gr, "src"))
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			n := e.Name()
			if n == "cmd" || n == "vendor" || n == "internal" || strings.HasPrefix(n, "_") || strings.HasPrefix(n, ".") {
				continue
			}
			navPkgs = append(navPkgs, n)
		}
	})
	return navPkgs
}

// navImportItems：GOROOT 包清单前缀过滤。
func navImportItems(prefix string) []CompletionItem {
	items := make([]CompletionItem, 0, 16)
	for _, p := range diskPkgNames() {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		items = append(items, CompletionItem{Label: p, Kind: 9, Detail: "std"})
		if len(items) >= 60 {
			break
		}
	}
	return items
}
