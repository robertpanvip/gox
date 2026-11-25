package typecheck

// Package typecheck 提供「虚拟文件系统 + go/types」的轻量类型检查原型（P0）。
//
// 结论依据：go/types 本体是纯 Go 库，可编译进 js/wasm；它对外部的真实需求
// 只有「解析 import」——即 types.Importer。本包把文件系统抽象成
// Lookup(importPath) -> 源文件列表：数据可来自磁盘（CLI/桌面）、内存 map
// （测试）、或将来的 go:embed（playground wasm），类型检查全程无感。
//
// 诊断坐标为生成 Go 源码的 1 基行列；回投 .gx 由调用方经 sourcemap 完成
// （internal/check 已建立该管线）。

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// File 是虚拟文件系统中的一个源文件。
type File struct{ Name, Src string }

// Lookup 是虚拟文件系统入口：按 import path 返回源文件；false 表示白名单外。
type Lookup func(path string) ([]File, bool)

// Diagnostic 是一条类型检查诊断（生成 Go 的 1 基坐标）。
type Diagnostic struct {
	Line    int
	Col     int
	Message string
}

func (d Diagnostic) String() string { return fmt.Sprintf("%d:%d: %s", d.Line, d.Col, d.Message) }

// maxDiags 防止极端源码刷屏。
const maxDiags = 16

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// Check 对 src 执行 go/types 完整检查（语法合法性由调用方先行保证，
// 语法诊断归 internal/check）。类型错误经 Config.Error 逐条收集。
func Check(src string, lookup Lookup) ([]Diagnostic, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "input.go", src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	im := &srcImporter{fset: fset, lookup: lookup, pkgs: map[string]*types.Package{}}
	diags := []Diagnostic{}
	collect := func(e error) {
		if len(diags) >= maxDiags {
			return
		}
		if te, ok := e.(types.Error); ok {
			pos := te.Fset.Position(te.Pos)
			diags = append(diags, Diagnostic{Line: pos.Line, Col: pos.Column, Message: te.Msg})
		}
	}
	conf := types.Config{Importer: im, Error: collect}
	_, cerr := conf.Check("input", fset, []*ast.File{f}, nil)
	if cerr != nil {
		collect(cerr)
	}
	sort.SliceStable(diags, func(i, j int) bool {
		if diags[i].Line != diags[j].Line {
			return diags[i].Line < diags[j].Line
		}
		return diags[i].Col < diags[j].Col
	})
	return diags, nil
}

// srcImporter 用 Lookup 的源码做 source 模式类型检查，递归解析依赖并缓存。
type srcImporter struct {
	fset   *token.FileSet
	lookup Lookup
	pkgs   map[string]*types.Package
}

func (i *srcImporter) Import(path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if p, ok := i.pkgs[path]; ok {
		return p, nil
	}
	files, ok := i.lookup(path)
	if !ok {
		return nil, fmt.Errorf("could not import %s: not found in virtual filesystem", path)
	}
	var asts []*ast.File
	pkgName := ""
	for _, fd := range files {
		af, perr := parser.ParseFile(i.fset, path+"/"+fd.Name, fd.Src, parser.SkipObjectResolution)
		if perr != nil || af.Name == nil {
			continue
		}
		if pkgName == "" {
			pkgName = af.Name.Name
		} else if af.Name.Name != pkgName {
			continue
		}
		asts = append(asts, af)
	}
	if len(asts) == 0 {
		return nil, fmt.Errorf("could not import %s: no parsable Go files", path)
	}
	pkg := types.NewPackage(path, pkgName)
	i.pkgs[path] = pkg // 先占位，切断循环导入
	conf := types.Config{Importer: i, Error: func(error) {}}
	done, _ := conf.Check(path, i.fset, asts, nil)
	if done != nil {
		i.pkgs[path] = done
		return done, nil
	}
	return pkg, nil
}

// ---- DiskLookup：目录树虚拟 FS，带目标平台过滤 ----

var goosSet = map[string]bool{"aix": true, "android": true, "darwin": true, "dragonfly": true,
	"freebsd": true, "hurd": true, "illumos": true, "ios": true, "js": true, "linux": true,
	"nacl": true, "netbsd": true, "openbsd": true, "plan9": true, "solaris": true,
	"wasip1": true, "windows": true, "zos": true}

var goarchSet = map[string]bool{"386": true, "amd64": true, "amd64p32": true, "arm": true,
	"arm64": true, "armbe": true, "loong64": true, "m68k": true, "mips": true, "mips64": true,
	"mips64le": true, "mips64n32": true, "mips64n32el": true, "mipsle": true, "ppc64": true,
	"ppc64le": true, "riscv64": true, "s390": true, "s390x": true, "sparc64": true, "wasm": true}

var unixLike = map[string]bool{"linux": true, "android": true, "darwin": true, "ios": true,
	"freebsd": true, "netbsd": true, "openbsd": true, "dragonfly": true, "solaris": true,
	"illumos": true, "aix": true}

var littleEndian = map[string]bool{"386": true, "amd64": true, "arm": true, "arm64": true,
	"loong64": true, "mipsle": true, "mips64le": true, "ppc64le": true, "riscv64": true, "wasm": true}

// DiskLookup 以 root（如 $GOROOT/src）为虚拟文件系统根，按 goos/goarch 过滤：
// 文件名平台后缀、//go:build 约束、_test.go、含 import "C" 的文件全部排除。
func DiskLookup(root, goos, goarch string) Lookup {
	return func(path string) ([]File, bool) {
		dir := filepath.Join(root, filepath.FromSlash(path))
		if !dirExists(dir) {
			return nil, false
		}
		ents, err := os.ReadDir(dir)
		if err != nil {
			return nil, false
		}
		out := []File{}
		for _, e := range ents {
			n := e.Name()
			if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") ||
				strings.HasPrefix(n, "_") {
				continue
			}
			if !nameOK(n, goos, goarch) {
				continue
			}
			b, rerr := os.ReadFile(filepath.Join(dir, n))
			if rerr != nil {
				continue
			}
			src := string(b)
			if strings.Contains(src, `import "C"`) {
				continue
			}
			if expr, has := buildConstraint(src); has && !satisfies(expr, goos, goarch) {
				continue
			}
			out = append(out, File{Name: n, Src: src})
		}
		if len(out) == 0 {
			return nil, false
		}
		return out, true
	}
}

// nameOK 按 Go 文件名约定过滤 x_<os>.go / x_<os>_<arch>.go / x_<arch>.go。
func nameOK(name, goos, goarch string) bool {
	stem := strings.TrimSuffix(name, ".go")
	parts := strings.Split(stem, "_")
	if len(parts) < 2 {
		return true
	}
	last := parts[len(parts)-1]
	if goarchSet[last] {
		if last != goarch {
			return false
		}
		if len(parts) >= 3 && goosSet[parts[len(parts)-2]] {
			return parts[len(parts)-2] == goos
		}
		return true
	}
	if goosSet[last] {
		if last != goos {
			return false
		}
		if len(parts) >= 3 && goarchSet[parts[len(parts)-2]] {
			return parts[len(parts)-2] == goarch
		}
		return true
	}
	return true
}

// buildConstraint 取包声明前的 //go:build 表达式（多条按 && 合并）。
func buildConstraint(src string) (string, bool) {
	exprs := []string{}
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "package ") {
			break
		}
		if strings.HasPrefix(t, "//go:build ") {
			exprs = append(exprs, strings.TrimSpace(strings.TrimPrefix(t, "//go:build ")))
		}
	}
	if len(exprs) == 0 {
		return "", false
	}
	return strings.Join(exprs, " && "), true
}

// satisfies 实现 //go:build 布尔代数（||、&&、!、括号）的最小求值器。
func satisfies(expr, goos, goarch string) bool {
	return evalOr(strings.TrimSpace(expr), goos, goarch)
}

func splitTopLevel(s, sep string) []string {
	out := []string{}
	depth := 0
	cur := strings.Builder{}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '(' {
			depth++
		}
		if c == ')' {
			depth--
		}
		if depth == 0 && strings.HasPrefix(s[i:], sep) {
			out = append(out, cur.String())
			cur.Reset()
			i += len(sep) - 1
			continue
		}
		cur.WriteByte(c)
	}
	out = append(out, cur.String())
	return out
}

func evalOr(s, goos, goarch string) bool {
	for _, p := range splitTopLevel(s, "||") {
		if evalAnd(strings.TrimSpace(p), goos, goarch) {
			return true
		}
	}
	return false
}

func evalAnd(s, goos, goarch string) bool {
	for _, p := range splitTopLevel(s, "&&") {
		if !evalAtom(strings.TrimSpace(p), goos, goarch) {
			return false
		}
	}
	return true
}

func evalAtom(s, goos, goarch string) bool {
	if s == "" {
		return true
	}
	if strings.HasPrefix(s, "!") {
		return !evalAtom(strings.TrimSpace(s[1:]), goos, goarch)
	}
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		return evalOr(strings.TrimSpace(s[1:len(s)-1]), goos, goarch)
	}
	if strings.HasPrefix(s, "go1.") {
		return true // 工具链版本门槛由构建期保证
	}
	if s == "unix" || s == "posix" {
		return unixLike[goos]
	}
	if s == "littleendian" {
		return littleEndian[goarch]
	}
	if s == "bigendian" {
		return !littleEndian[goarch]
	}
	if s == "gc" || s == "std" || s == "cmdline" || s == "purego" || s == "portability" {
		return true
	}
	if s == "cgo" {
		return false
	}
	return s == goos || s == goarch
}
