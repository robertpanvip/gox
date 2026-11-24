// Package buildcheck 用真实 go build 对转译后的 Go 源码做语法/类型检查，
// 并解析诊断。支持 context 取消与 LRU 缓存（LSP 场景反复触发）。
package buildcheck

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Diagnostic 是一条 go build 诊断（位置为生成 Go 源码的行列）。
type Diagnostic struct {
	Line    int
	Col     int
	Message string
	File    string
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("%d:%d: %s", d.Line, d.Col, d.Message)
}

// Checker 执行构建检查并缓存结果。并发安全。
type Checker struct {
	mu       sync.Mutex
	cache    map[string]*list.Element // key -> order 中的元素
	order    *list.List               // 最近使用在前，Element.Value = *cacheEntry
	capacity int

	// toolchain 用于 go 命令（默认 "go"；测试可注入）。
	toolchain string
	extraEnv  []string
	// replaces 是 go.mod 的 replace 指令（module 路径 -> 本地目录），
	// 用于让生成的 Go 代码能解析到本地运行时（vdom/list）。
	replaces map[string]string
	// modulePath 用于临时 go.mod 的 module 声明。默认 "buildcheck"；
	// GX 项目模式下设为真实项目 module 路径，使本地包 import 可解析。
	modulePath string
}

type cacheEntry struct {
	key   string
	diags []Diagnostic
}

// New 创建 Checker，capacity 为 LRU 容量。
func New(capacity int) *Checker {
	if capacity <= 0 {
		capacity = 64
	}
	return &Checker{
		cache:      map[string]*list.Element{},
		order:      list.New(),
		capacity:   capacity,
		toolchain:  "go",
		replaces:   map[string]string{},
		modulePath: "buildcheck",
	}
}

// SetToolchain 注入 go 命令路径与额外环境变量（如 GOTOOLCHAIN=go1.27.4）。
// 工具链变更会使缓存失效，因此在锁内清空缓存。
func (c *Checker) SetToolchain(bin string, env ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.toolchain = bin
	c.extraEnv = append([]string(nil), env...)
	c.cache = map[string]*list.Element{}
	c.order.Init()
}

// AddReplace 添加一条 go.mod replace 指令（module 路径 -> 本地目录），
// 并在替换变更时清空缓存（缓存键未包含 replace 内容）。
func (c *Checker) AddReplace(modulePath, localDir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.replaces[modulePath] = localDir
	c.cache = map[string]*list.Element{}
	c.order.Init()
}

// SetModulePath 设置临时 go.mod 的 module 声明（GX 项目模式下为项目 module 路径），
// 使本地包 import（如 example.com/proj/util）可被解析。变更会使缓存失效。
func (c *Checker) SetModulePath(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if path == "" {
		path = "buildcheck"
	}
	c.modulePath = path
	c.cache = map[string]*list.Element{}
	c.order.Init()
}

// buildConfig 是一次构建所用配置的一致快照。build 只读取快照，
// 绝不在锁外访问 Checker 的共享字段，从而消除并发数据竞争。
type buildConfig struct {
	toolchain  string
	extraEnv   []string
	replaces   map[string]string
	modulePath string
}

// snapshot 在锁内复制当前配置。
func (c *Checker) snapshot() buildConfig {
	c.mu.Lock()
	defer c.mu.Unlock()
	env := append([]string(nil), c.extraEnv...)
	rep := make(map[string]string, len(c.replaces))
	for k, v := range c.replaces {
		rep[k] = v
	}
	return buildConfig{toolchain: c.toolchain, extraEnv: env, replaces: rep, modulePath: c.modulePath}
}

// cacheKey 把源码与构建配置一起哈希：不同 toolchain/replace/env 的检查结果
// 不会互相命中旧缓存。
func cacheKey(goSrc string, cfg buildConfig) string {
	h := sha256.New()
	h.Write([]byte(goSrc))
	h.Write([]byte{0})
	h.Write([]byte(cfg.toolchain))
	h.Write([]byte{0})
	h.Write([]byte(cfg.modulePath))
	for _, e := range cfg.extraEnv {
		h.Write([]byte{0})
		h.Write([]byte(e))
	}
	// map 迭代无序，按 key 排序保证 key 稳定。
	keys := make([]string, 0, len(cfg.replaces))
	for k := range cfg.replaces {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h.Write([]byte{0})
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write([]byte(cfg.replaces[k]))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// cacheKeyFiles 把多文件源码与构建配置一起哈希。文件按 Name 排序保证稳定。
func cacheKeyFiles(files []SourceFile, cfg buildConfig) string {
	h := sha256.New()
	sorted := append([]SourceFile(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for _, f := range sorted {
		h.Write([]byte{0})
		h.Write([]byte(f.Name))
		h.Write([]byte{0})
		h.Write([]byte(f.Source))
	}
	h.Write([]byte{0})
	h.Write([]byte(cfg.toolchain))
	h.Write([]byte{0})
	h.Write([]byte(cfg.modulePath))
	for _, e := range cfg.extraEnv {
		h.Write([]byte{0})
		h.Write([]byte(e))
	}
	keys := make([]string, 0, len(cfg.replaces))
	for k := range cfg.replaces {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h.Write([]byte{0})
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write([]byte(cfg.replaces[k]))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Check 对单文件 goSrc 执行构建检查。结果被缓存；ctx 取消时返回 ctx.Err()。
func (c *Checker) Check(ctx context.Context, goSrc string) ([]Diagnostic, error) {
	return c.CheckFiles(ctx, []SourceFile{{Name: "main.go", Source: goSrc}})
}

// SourceFile 是多文件构建中的一份 Go 源文件；Name 为相对构建根的路径
// （如 "main.go"、"util/math.go"），Source 为其内容。
type SourceFile struct {
	Name   string
	Source string
}

// CheckFiles 对一组 Go 源文件执行构建检查，诊断的 File 字段是 SourceFile.Name。
// 供 GX 项目模式使用：多份 .gx 转译产物需一起 go build 才能解析跨包引用。
func (c *Checker) CheckFiles(ctx context.Context, files []SourceFile) ([]Diagnostic, error) {
	cfg := c.snapshot()
	key := cacheKeyFiles(files, cfg)

	c.mu.Lock()
	if ent, ok := c.cache[key]; ok {
		c.order.MoveToFront(ent)
		diags := append([]Diagnostic{}, ent.Value.(*cacheEntry).diags...)
		c.mu.Unlock()
		return diags, nil
	}
	c.mu.Unlock()

	diags, err := c.buildFiles(ctx, files, cfg)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	// 双检：并发期间可能已有相同 key 的构建完成，避免插入重复 LRU 节点。
	if ent, ok := c.cache[key]; ok {
		c.order.MoveToFront(ent)
		c.mu.Unlock()
		return append([]Diagnostic{}, ent.Value.(*cacheEntry).diags...), nil
	}
	ent := c.order.PushFront(&cacheEntry{key: key, diags: diags})
	c.cache[key] = ent
	for c.order.Len() > c.capacity {
		back := c.order.Back()
		if back == nil {
			break
		}
		c.order.Remove(back)
		delete(c.cache, back.Value.(*cacheEntry).key)
	}
	c.mu.Unlock()

	return append([]Diagnostic{}, diags...), nil
}

var diagRe = regexp.MustCompile(`^(.*?):(\d+):(\d+):\s*(.+)$`)

func (c *Checker) buildFiles(ctx context.Context, files []SourceFile, cfg buildConfig) ([]Diagnostic, error) {
	dir, err := os.MkdirTemp("", "gox-buildcheck-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	var gomod strings.Builder
	fmt.Fprintf(&gomod, "module %s\n\ngo 1.27\n", cfg.modulePath)
	if len(cfg.replaces) > 0 {
		gomod.WriteString("\n")
		// 排序使 go.mod 内容稳定（确定性构建）
		mods := make([]string, 0, len(cfg.replaces))
		for mod := range cfg.replaces {
			mods = append(mods, mod)
		}
		sort.Strings(mods)
		for _, mod := range mods {
			fmt.Fprintf(&gomod, "require %s v0.0.0\n", mod)
		}
		gomod.WriteString("\n")
		for _, mod := range mods {
			fmt.Fprintf(&gomod, "replace %s => %s\n", mod, cfg.replaces[mod])
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod.String()), 0644); err != nil {
		return nil, err
	}
	// 逐份写入源码，保留相对路径（含子目录），使跨包引用可解析、
	// 诊断 File 字段能回投到对应的 .gx sourcemap。
	for _, f := range files {
		name := filepath.Clean(f.Name)
		if name == "." || filepath.IsAbs(name) || strings.HasPrefix(name, "..") {
			name = "main.go" // 防御：非法相对路径回退
		}
		dst := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dst, []byte(f.Source), 0644); err != nil {
			return nil, err
		}
	}

	cmd := exec.CommandContext(ctx, cfg.toolchain, "build", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), cfg.extraEnv...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err == nil {
		return nil, nil
	}

	var diags []Diagnostic
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "# ") {
			continue
		}
		m := diagRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		ln, _ := strconv.Atoi(m[2])
		col, _ := strconv.Atoi(m[3])
		file := filepath.ToSlash(filepath.Clean(m[1]))
		diags = append(diags, Diagnostic{
			Line: ln, Col: col, Message: m[4], File: file,
		})
	}
	if len(diags) == 0 && len(out) > 0 {
		// 没有结构化位置信息时，兜底一条 1:1 诊断
		diags = append(diags, Diagnostic{Line: 1, Col: 1, Message: strings.TrimSpace(string(out))})
	}
	return diags, nil
}

// Stats 返回缓存使用统计（供测试/观测）。
func (c *Checker) Stats() (entries int, capacity int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cacheLen(), c.capacity
}

func (c *Checker) cacheLen() int { return c.order.Len() }

// Clear 清空缓存。
func (c *Checker) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = map[string]*list.Element{}
	c.order.Init()
}
