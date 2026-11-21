// Package module 提供 GX 项目的跨文件模块解析：把一组 .gx 文件按 package
// 分组为 GX 包，解析 import 来源（Go / GX），建立依赖图并给出拓扑序与循环诊断。
//
// 本包只负责「项目视图 + 来源解析 + 转译调度」，实际的符号还原与大小写处理
// 交给 pkg/compiler（保持纯转译职责）。compiler 不 import 本包，避免环依赖：
// 本包调用 compiler.CompileWithProject 并把 import 来源表喂给它。
package module

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/robertpanvip/gox/pkg/compiler"
	"github.com/robertpanvip/gox/pkg/sourcemap"
)

// Prefer 定义本地 GX 包与 Go 包同名时的歧义裁决。
type Prefer string

const (
	PreferGX Prefer = "gx" // 默认：本地 .gx 优先
	PreferGo Prefer = "go" // 逃生开关：Go 包优先
)

// GXModule 一个 GX 包（同一目录下、同一 package 的一组 .gx 文件）解析后的产物。
type GXModule struct {
	ImportPath string   // 逻辑 import 路径，如 example.com/proj/util
	Dir        string   // 源文件目录（绝对路径）
	Package    string   // package 名
	Files      []string // .gx 文件绝对路径（按文件名排序）

	// Exports 汇总该包所有 pub 声明的「小写原名 → 导出名」。
	// 来自各文件 compiler.ScanExports 的并集，严格保真被导入方声明。
	Exports map[string]string

	// imports 是该包所有文件的 import 路径并集（含 Go 包与 GX 包），用于建依赖图。
	imports []string
}

// Project 是项目视图：以根目录 + module 路径建立 importPath → GXModule 索引。
type Project struct {
	Root       string               // 项目根目录（绝对路径）
	ModulePath string               // Go module 路径，如 example.com/proj
	Prefer     Prefer               // 歧义裁决
	packages   map[string]*GXModule // importPath → 包
	order      []*GXModule          // 拓扑序（被依赖方在前），Build 时填充
}

// CompiledFile 是一份 .gx 的转译产物。
type CompiledFile struct {
	SourcePath string // .gx 绝对路径
	GoPath     string // 建议落盘的 .go 路径（与源文件同目录，后缀 .go）
	Package    string // 所属 GX 包 importPath
	GoSource   string
	SourceMap  *sourcemap.SourceMap
	Analysis   *compiler.Analysis // GX 语义层产物（含大小写诊断），供 check.CheckAll 消费
}

// LoadProject 从根目录扫描所有 .gx，按 package 分组建立项目视图。
// modulePath 是 Go module 路径前缀，用于把目录推导为逻辑 importPath。
func LoadProject(root, modulePath string, prefer Prefer) (*Project, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("项目根目录 %s: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("项目根 %s 不是目录", abs)
	}
	if prefer == "" {
		prefer = PreferGX
	}
	p := &Project{
		Root: abs, ModulePath: strings.TrimSuffix(modulePath, "/"),
		Prefer: prefer, packages: map[string]*GXModule{},
	}

	// 按 (目录, package) 分组。
	type groupKey struct{ dir, pkg string }
	groups := map[groupKey]*GXModule{}
	err = filepath.Walk(abs, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fi.IsDir() {
			base := fi.Name()
			if base == ".git" || base == "vendor" || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(fi.Name(), ".gx") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		pkgName := compiler.ScanPackageName(string(src))
		if pkgName == "" {
			return fmt.Errorf("%s: 无法识别 package 声明", path)
		}
		dir := filepath.Dir(path)
		key := groupKey{dir: dir, pkg: pkgName}
		g, ok := groups[key]
		if !ok {
			rel, err := filepath.Rel(abs, dir)
			if err != nil {
				return err
			}
			imp := p.ModulePath
			if rel != "." {
				imp = p.ModulePath + "/" + filepath.ToSlash(rel)
			}
			g = &GXModule{
				ImportPath: imp, Dir: dir, Package: pkgName,
				Exports: map[string]string{},
			}
			groups[key] = g
		}
		g.Files = append(g.Files, path)
		// 汇总导出表与 import 列表。
		for k, v := range compiler.ScanExports(string(src)) {
			if _, exists := g.Exports[k]; !exists {
				g.Exports[k] = v
			}
		}
		g.imports = append(g.imports, compiler.ScanImports(string(src))...)
		return nil
	})
	if err != nil {
		return nil, err
	}

	for _, g := range groups {
		sort.Strings(g.Files)
		sort.Strings(g.imports)
		p.packages[g.ImportPath] = g
	}
	return p, nil
}

// Resolve 解析一条 import 路径，返回其来源信息。
// 第二个返回值为 true 表示命中本地 GX 包（调用方应据此走 ImportGX 分支）。
// 仅当 PreferGo 或路径不在本地包索引时，才按 Go 来源处理。
func (p *Project) Resolve(importPath string) (compiler.ImportInfo, bool) {
	if p.Prefer != PreferGo {
		if g, ok := p.packages[importPath]; ok {
			return compiler.ImportInfo{
				Path: importPath, Origin: compiler.ImportGX, Exports: g.Exports,
			}, true
		}
	}
	return compiler.ImportInfo{Path: importPath, Origin: compiler.ImportGo}, false
}

// packagesForImport 返回给定 import 路径指向的 GX 包（仅本地 GX 来源）。
func (p *Project) packagesForImport(importPath string) (*GXModule, bool) {
	if p.Prefer == PreferGo {
		return nil, false
	}
	g, ok := p.packages[importPath]
	return g, ok
}

// importMapFor 为一个文件的所有 import 构造来源表，喂给 compiler.CompileWithProject。
func (p *Project) importMapFor(src string) map[string]compiler.ImportInfo {
	out := map[string]compiler.ImportInfo{}
	for _, path := range compiler.ScanImports(src) {
		info, _ := p.Resolve(path)
		out[path] = info
	}
	return out
}

// Order 返回拓扑序（被依赖方在前）。若存在环，返回带路径链的错误。
// 结果会被缓存，Build 复用。
func (p *Project) Order() ([]*GXModule, error) {
	if p.order != nil {
		return p.order, nil
	}
	nodes := make([]*GXModule, 0, len(p.packages))
	for _, g := range p.packages {
		nodes = append(nodes, g)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ImportPath < nodes[j].ImportPath })

	// 邻接表：g → 它依赖的本地 GX 包。
	adj := map[string][]string{}
	index := map[string]*GXModule{}
	for _, g := range nodes {
		index[g.ImportPath] = g
		var deps []string
		for _, imp := range g.imports {
			if dep, ok := p.packagesForImport(imp); ok && dep.ImportPath != g.ImportPath {
				deps = append(deps, dep.ImportPath)
			}
		}
		sort.Strings(deps)
		adj[g.ImportPath] = deps
	}

	const (
		white = 0 // 未访问
		gray  = 1 // 在栈上
		black = 2 // 完成
	)
	color := map[string]int{}
	var order []*GXModule
	var stack []string
	var cycErr error

	var visit func(path string)
	visit = func(path string) {
		if cycErr != nil {
			return
		}
		color[path] = gray
		stack = append(stack, path)
		for _, dep := range adj[path] {
			switch color[dep] {
			case white:
				visit(dep)
			case gray:
				// 找到回边：截取栈上从 dep 到当前的部分，形成路径链。
				start := 0
				for i, s := range stack {
					if s == dep {
						start = i
						break
					}
				}
				chain := append([]string{}, stack[start:]...)
				chain = append(chain, dep)
				cycErr = fmt.Errorf("GX 模块循环依赖: %s", strings.Join(chain, " → "))
				return
			}
		}
		stack = stack[:len(stack)-1]
		color[path] = black
		order = append(order, index[path])
	}
	for _, g := range nodes {
		if color[g.ImportPath] == white {
			visit(g.ImportPath)
		}
	}
	if cycErr != nil {
		return nil, cycErr
	}
	p.order = order
	return order, nil
}

// Build 按拓扑序转译项目中所有 GX 包的所有文件。
// entry 仅用于确认入口在项目内并给出友好诊断；产物覆盖整个项目依赖闭包。
func (p *Project) Build(entry string) ([]*CompiledFile, error) {
	abs, err := filepath.Abs(entry)
	if err != nil {
		return nil, err
	}
	found := false
	for _, g := range p.packages {
		for _, f := range g.Files {
			if f == abs {
				found = true
				break
			}
		}
	}
	if !found {
		return nil, fmt.Errorf("入口 %s 不在项目 %s 内", entry, p.Root)
	}

	order, err := p.Order()
	if err != nil {
		return nil, err
	}
	var out []*CompiledFile
	for _, g := range order {
		for _, f := range g.Files {
			src, err := os.ReadFile(f)
			if err != nil {
				return nil, err
			}
			imports := p.importMapFor(string(src))
			res, err := compiler.CompileWithProject(f, string(src), p.ModulePath, imports)
			if err != nil {
				return nil, fmt.Errorf("转译 %s: %w", f, err)
			}
			if err := res.Validate(); err != nil {
				return nil, fmt.Errorf("生成 Go 语法无效 %s: %w", f, err)
			}
			goPath := strings.TrimSuffix(f, ".gx") + ".go"
			out = append(out, &CompiledFile{
				SourcePath: f, GoPath: goPath, Package: g.ImportPath,
				GoSource: res.GoSource, SourceMap: res.SourceMap, Analysis: res.Analysis,
			})
		}
	}
	return out, nil
}
