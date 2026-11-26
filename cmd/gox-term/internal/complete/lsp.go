package complete

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LSPSymbolProvider 把真实语言服务器（gopls / gox-lsp）的 workspace/symbol
// 结果纳入补全。设计成「异步预热 + 内存缓存」：
//   - Provide 永不阻塞 UI：只从已完成的缓存里读；
//   - 第一次命中时后台启动一次 LSP 会话取符号，失败静默降级（机器上没装 gopls 也无妨）。
//
// 这样既兑现了「装了全局 gopls，敲符号首字母就弹」的体验，又不会让终端卡顿。
type LSPSymbolProvider struct {
	root string

	mu       sync.RWMutex
	cache    []Candidate
	stamp    int64
	ttl      int64
	warming  bool
	warmOnce sync.Once
}

func NewLSPSymbolProvider(root string) *LSPSymbolProvider {
	if root == "" {
		root, _ = os.Getwd()
	}
	return &LSPSymbolProvider{root: root, ttl: int64(30 * 1e9)}
}

func (p *LSPSymbolProvider) Name() string { return "lsp" }

func (p *LSPSymbolProvider) Provide(q Query) []Candidate {
	if q.Token == "" {
		return nil
	}
	cwd := q.CWD
	if cwd == "" {
		cwd = p.root
	}
	p.ensureWarm(cwd)

	p.mu.RLock()
	cache, stamp := p.cache, p.stamp
	p.mu.RUnlock()
	if len(cache) == 0 || nowNano()-stamp > p.ttl {
		return nil
	}

	tl := strings.ToLower(q.Token)
	out := make([]Candidate, 0, 24)
	for _, c := range cache {
		if strings.HasPrefix(strings.ToLower(c.Label), tl) || strings.Contains(strings.ToLower(c.Label), tl) {
			out = append(out, c)
		}
	}
	return out
}

// ensureWarm 触发一次后台预热（仅一次，非阻塞）。
func (p *LSPSymbolProvider) ensureWarm(root string) {
	p.mu.RLock()
	fresh := len(p.cache) > 0 && nowNano()-p.stamp < p.ttl
	warming := p.warming
	p.mu.RUnlock()
	if fresh || warming {
		return
	}
	p.warmOnce.Do(func() {
		p.mu.Lock()
		p.warming = true
		p.mu.Unlock()
		go func() {
			defer func() {
				p.mu.Lock()
				p.warming = false
				p.mu.Unlock()
			}()
			syms, ok := fetchWorkspaceSymbols(root, 1500*time.Millisecond)
			if !ok {
				return
			}
			cands := make([]Candidate, 0, len(syms))
			for _, s := range syms {
				if s.Name == "" {
					continue
				}
				cands = append(cands, Candidate{Text: s.Name, Label: s.Name, Detail: "LSP 符号", Kind: KindSymbol})
			}
			p.mu.Lock()
			p.cache, p.stamp = cands, nowNano()
			p.mu.Unlock()
		}()
	})
}

// ---- 最小 LSP 客户端（JSON-RPC over stdio）----

type lspSymbol struct {
	Name string `json:"name"`
	Kind int    `json:"kind"`
}

// fetchWorkspaceSymbols 启动一个 LSP 服务器，initialize → initialized →
// workspace/symbol → shutdown，超时或任何错误都静默降级。
func fetchWorkspaceSymbols(root string, timeout time.Duration) ([]lspSymbol, bool) {
	bin := pickLSPServer()
	if bin == "" {
		return nil, false
	}
	cmd := exec.Command(bin)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, false
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false
	}
	cmd.Stderr = io.Discard
	// gopls 需要 module 根目录；用 uri 形式。
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	rootURI := "file://" + filepath.ToSlash(root)

	if err := cmd.Start(); err != nil {
		return nil, false
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()

	type result struct {
		syms []lspSymbol
		ok   bool
	}
	resCh := make(chan result, 1)
	go func() {
		defer func() {
			_ = stdin.Close()
			select {
			case <-done:
			case <-time.After(200 * time.Millisecond):
				_ = cmd.Process.Kill()
			}
		}()

		r := bufio.NewReader(stdout)
		_ = writeLSP(stdin, 1, "initialize", map[string]any{
			"processId": os.Getpid(),
			"rootUri":   rootURI,
			"capabilities": map[string]any{
				"workspace": map[string]any{"symbol": map[string]any{}},
			},
		})
		// 读完 initialize 响应（期间可能夹杂 server 的 window/logMessage 通知）。
		if _, err := readLSPUntilID(r, 1, 3*time.Second); err != nil {
			resCh <- result{ok: false}
			return
		}
		_ = writeNotify(stdin, "initialized", map[string]any{})
		// 等 workspace/symbol 索引就绪的简短宽限（gopls 首次索引需要时间）。
		time.Sleep(250 * time.Millisecond)
		_ = writeLSP(stdin, 2, "workspace/symbol", map[string]any{"query": ""})
		raw, err := readLSPUntilID(r, 2, 3*time.Second)
		if err != nil {
			resCh <- result{ok: false}
			return
		}
		var resp struct {
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal(raw, &resp) != nil {
			resCh <- result{ok: false}
			return
		}
		var syms []lspSymbol
		// result 可能是数组，也可能是 {data:[...]}。
		if json.Unmarshal(resp.Result, &syms) != nil {
			var wrap struct {
				Data []lspSymbol `json:"data"`
			}
			if json.Unmarshal(resp.Result, &wrap) != nil {
				resCh <- result{ok: false}
				return
			}
			syms = wrap.Data
		}
		resCh <- result{syms: syms, ok: true}
	}()

	select {
	case r := <-resCh:
		return r.syms, r.ok
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		return nil, false
	}
}

func pickLSPServer() string {
	for _, name := range []string{"gopls", "gox-lsp"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

func writeLSP(w io.Writer, id int, method string, params any) error {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": method, "params": params,
	})
	if err != nil {
		return err
	}
	return writeFrame(w, payload)
}

func writeNotify(w io.Writer, method string, params any) error {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "method": method, "params": params,
	})
	if err != nil {
		return err
	}
	return writeFrame(w, payload)
}

func writeFrame(w io.Writer, payload []byte) error {
	_, err := io.WriteString(w, "Content-Length: "+strconv.Itoa(len(payload))+"\r\n\r\n")
	if err != nil {
		return err
	}
	_, err = w.Write(payload)
	return err
}

// readLSPUntilID 读消息直到找到指定 id 的响应，忽略通知与查询。
func readLSPUntilID(r *bufio.Reader, id int, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		payload, err := readLSPFrame(r, timeout)
		if err != nil {
			return nil, err
		}
		var head struct {
			ID    json.RawMessage `json:"id"`
			Error json.RawMessage `json:"error"`
		}
		if json.Unmarshal(payload, &head) != nil {
			continue
		}
		var got int
		if json.Unmarshal(head.ID, &got) == nil && got == id {
			if len(head.Error) > 0 && string(head.Error) != "null" {
				return nil, io.ErrUnexpectedEOF
			}
			return payload, nil
		}
	}
	return nil, io.ErrUnexpectedEOF
}

func readLSPFrame(r *bufio.Reader, timeout time.Duration) ([]byte, error) {
	_ = timeout // 由上层 readLSPUntilID 的 deadline 统一控制
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length:") {
			n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:")))
			if err == nil {
				length = n
			}
		}
	}
	if length < 0 || length > 16<<20 {
		return nil, io.ErrUnexpectedEOF
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(buf), nil
}
