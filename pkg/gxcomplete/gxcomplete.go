// Package gxcomplete 提供面向编辑器的类型感知点号成员补全（如 `obj.` / `strings.`）。
//
// 它是 internal/typecheck 的公共门面：独立 module（例如 cmd/gox-tui）无法 import
// internal/*，但可以 import 本包。核心链路复用 typecheck.Importer.Complete ——
// 在点号后插入占位标识符使未完成表达式可类型检查，再从类型化 AST 枚举成员。
//
// 任何环节不适用（非点号上下文、无 GOROOT、编译失败、类型图拿不到）都返回 nil，
// 调用方回退到符号表 / 关键字路径，零行为回归。
package gxcomplete

import (
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/robertpanvip/gox/internal/typecheck"
	"github.com/robertpanvip/gox/pkg/compiler"
)

// Candidate 是一条成员候选。Kind ∈ package|function|method|field|variable|constant|type。
type Candidate struct {
	Name      string
	Signature string
	Kind      string
}

// 占位标识符：与 typecheck 内部一致，用于让 `obj.` 变成语法完整的 `obj.GxCompleteX_`。
const placeholder = "GxCompleteX_"

// Session 是持久补全会话。类型检查器缓存 std 包类型图，跨多次击键复用，
// 避免每次补全都重建依赖闭包。Importer 并发不安全，用 mu 串行化。
type Session struct {
	mu  sync.Mutex
	imp *typecheck.Importer
}

// New 创建补全会话。构造后再调 Warm 可异步预热类型图。
func New() *Session { return &Session{} }

// Warm 异步预热（避免首次补全因构建 std 包类型图而卡 UI）。可重复调用。
func (s *Session) Warm() {
	go func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.ensureImporter()
	}()
}

func (s *Session) ensureImporter() bool {
	if s.imp != nil {
		return true
	}
	gr := runtime.GOROOT()
	if gr == "" {
		return false
	}
	s.imp = typecheck.NewImporter(typecheck.DiskLookup(filepath.Join(gr, "src"), runtime.GOOS, runtime.GOARCH))
	return true
}

// MembersAt 在 gxSrc 第 line 行、光标位于第 col 个 rune 之后（0 基）做点号成员补全。
// col 指向"已敲前缀的末尾"——即点到点号之后。非点号上下文返回 nil。
func (s *Session) MembersAt(gxSrc string, line, col int) []Candidate {
	off := runeOffset(gxSrc, line, col)
	if off < 0 {
		return nil
	}
	return s.Members(gxSrc, off)
}

// Members 在 gxSrc 的字节偏移 cursor（指向点号后已敲前缀的末尾）做点号成员补全。
// 非点号上下文返回 nil。
func (s *Session) Members(gxSrc string, cursor int) []Candidate {
	if cursor < 0 || cursor > len(gxSrc) {
		return nil
	}
	// 从光标向左跳过标识符（含已敲前缀），定位点号。
	dot := cursor
	for dot > 0 && isIdentByte(gxSrc[dot-1]) {
		dot--
	}
	if dot == 0 || gxSrc[dot-1] != '.' {
		return nil
	}

	// 插入占位符，让 `obj.` / `obj.pr` 变成语法完整的 `obj.GxCompleteX_`。
	mods := gxSrc[:dot] + placeholder + gxSrc[cursor:]
	res, err := compiler.Compile("input.gx", mods)
	if err != nil || res == nil || res.GoSource == "" {
		return nil
	}
	// 占位符原样保留在产物里（探针验证：空/非空前缀、局部变量、GX sig 均可定位）。
	di := strings.Index(res.GoSource, placeholder)
	if di < 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ensureImporter() {
		return nil
	}
	cs := s.imp.Complete(res.GoSource, di+len(placeholder))
	if len(cs) == 0 {
		return nil
	}
	out := make([]Candidate, 0, len(cs))
	for _, c := range cs {
		out = append(out, Candidate{Name: c.Name, Signature: c.Signature, Kind: c.Kind})
	}
	return out
}

func isIdentByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b >= 0x80
}

// runeOffset 把 0 基 line 与行内第 col 个 rune 之后的字节位置转换为 gxSrc 字节偏移。
// col 等于该行 rune 数时返回行尾字节偏移；越界返回 -1。
func runeOffset(src string, line, col int) int {
	if line < 0 || col < 0 {
		return -1
	}
	off := 0
	for i := 0; i < line; i++ {
		j := strings.IndexByte(src[off:], '\n')
		if j < 0 {
			return -1
		}
		off += j + 1
	}
	end := strings.IndexByte(src[off:], '\n')
	if end < 0 {
		end = len(src) - off
	}
	lineBytes := src[off : off+end]
	if col > len([]rune(lineBytes)) {
		return -1
	}
	n := 0
	for i := range lineBytes {
		if n == col {
			return off + i
		}
		n++
	}
	return off + len(lineBytes)
}
