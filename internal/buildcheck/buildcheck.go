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
		cache:     map[string]*list.Element{},
		order:     list.New(),
		capacity:  capacity,
		toolchain: "go",
		replaces:  map[string]string{},
	}
}

// SetToolchain 注入 go 命令路径与额外环境变量（如 GOTOOLCHAIN=go1.27.3）。
func (c *Checker) SetToolchain(bin string, env ...string) {
	c.toolchain = bin
	c.extraEnv = env
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

// Check 对 goSrc 执行构建检查。结果被缓存；ctx 取消时返回 ctx.Err()。
func (c *Checker) Check(ctx context.Context, goSrc string) ([]Diagnostic, error) {
	sum := sha256.Sum256([]byte(goSrc))
	key := hex.EncodeToString(sum[:])

	c.mu.Lock()
	if ent, ok := c.cache[key]; ok {
		c.order.MoveToFront(ent)
		diags := append([]Diagnostic{}, ent.Value.(*cacheEntry).diags...)
		c.mu.Unlock()
		return diags, nil
	}
	c.mu.Unlock()

	diags, err := c.build(ctx, goSrc)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
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

func (c *Checker) build(ctx context.Context, goSrc string) ([]Diagnostic, error) {
	dir, err := os.MkdirTemp("", "gox-buildcheck-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	var gomod strings.Builder
	gomod.WriteString("module buildcheck\n\ngo 1.27\n")
	if len(c.replaces) > 0 {
		gomod.WriteString("\n")
		for mod := range c.replaces {
			fmt.Fprintf(&gomod, "require %s v0.0.0\n", mod)
		}
		gomod.WriteString("\n")
		for mod, local := range c.replaces {
			fmt.Fprintf(&gomod, "replace %s => %s\n", mod, local)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod.String()), 0644); err != nil {
		return nil, err
	}
	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte(goSrc), 0644); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, c.toolchain, "build", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), c.extraEnv...)
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
		diags = append(diags, Diagnostic{
			Line: ln, Col: col, Message: m[4], File: filepath.Base(m[1]),
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
