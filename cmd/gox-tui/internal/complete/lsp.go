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

// LSPProvider 把真实语言服务器（gopls/gox-lsp）的 workspace/symbol 纳入补全。
// 异步预热 + 内存缓存，Provide 永不阻塞 UI；没装 LSP 静默降级。
type LSPProvider struct {
	root    string
	mu      sync.RWMutex
	cache   []string
	stamp   int64
	warming bool
}

func NewLSPProvider(root string) *LSPProvider {
	if root == "" {
		root, _ = os.Getwd()
	}
	return &LSPProvider{root: root}
}

func (p *LSPProvider) Name() string { return "lsp" }

func (p *LSPProvider) Provide(q Query) []Candidate {
	if q.Token == "" {
		return nil
	}
	p.ensureWarm()
	p.mu.RLock()
	cache, stamp := p.cache, p.stamp
	p.mu.RUnlock()
	if len(cache) == 0 || nowNano()-stamp > 30*1e9 {
		return nil
	}
	tl := strings.ToLower(q.Token)
	out := make([]Candidate, 0, 24)
	for _, name := range cache {
		if strings.HasPrefix(strings.ToLower(name), tl) || strings.Contains(strings.ToLower(name), tl) {
			out = append(out, Candidate{Text: name, Label: name, Detail: "LSP", Kind: KindLSP})
		}
	}
	return out
}

func (p *LSPProvider) ensureWarm() {
	p.mu.Lock()
	// TTL 语义统一：stamp 无论成败都在抓取结束时刷新——30 秒内不重复抓，
	// 过期后允许重抓。此前用 sync.Once 只允许预热一次，缓存过期后候选
	// 永久消失；外层 2.5s 预算又小于内层 initialize 的 3s，慢启动的 gopls
	// 必被超时击杀且再无重试机会。失败退避与成功保鲜共用同一个 stamp。
	fresh := nowNano()-p.stamp < 30*int64(time.Second)
	if fresh || p.warming {
		p.mu.Unlock()
		return
	}
	p.warming = true
	p.mu.Unlock()
	go func() {
		syms, ok := fetchSymbols(p.root, 8*time.Second)
		p.mu.Lock()
		p.warming = false
		if ok {
			p.cache = syms
		}
		p.stamp = nowNano()
		p.mu.Unlock()
	}()
}

func fetchSymbols(root string, timeout time.Duration) ([]string, bool) {
	bin := ""
	for _, name := range []string{"gopls", "gox-lsp"} {
		if path, err := exec.LookPath(name); err == nil {
			bin = path
			break
		}
	}
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
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	if err := cmd.Start(); err != nil {
		return nil, false
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()

	type result struct {
		syms []string
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
			"processId":    os.Getpid(),
			"rootUri":      "file://" + filepath.ToSlash(root),
			"capabilities": map[string]any{"workspace": map[string]any{"symbol": map[string]any{}}},
		})
		if _, err := readUntilID(r, 1, 3*time.Second); err != nil {
			resCh <- result{ok: false}
			return
		}
		_ = writeNotify(stdin, "initialized", map[string]any{})
		time.Sleep(250 * time.Millisecond)
		_ = writeLSP(stdin, 3, "workspace/symbol", map[string]any{"query": ""})
		raw, err := readUntilID(r, 3, 3*time.Second)
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
		var syms []struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(resp.Result, &syms) != nil {
			var wrap struct {
				Data []struct {
					Name string `json:"name"`
				} `json:"data"`
			}
			if json.Unmarshal(resp.Result, &wrap) != nil {
				resCh <- result{ok: false}
				return
			}
			for _, s := range wrap.Data {
				if s.Name != "" {
					syms = append(syms, struct {
						Name string `json:"name"`
					}{s.Name})
				}
			}
		}
		names := make([]string, 0, len(syms))
		for _, s := range syms {
			if s.Name != "" {
				names = append(names, s.Name)
			}
		}
		resCh <- result{syms: names, ok: true}
	}()
	select {
	case r := <-resCh:
		return r.syms, r.ok
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		return nil, false
	}
}

func writeLSP(w io.Writer, id int, method string, params any) error {
	return writeFrame(w, map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
}

// writeNotify 发送通知：LSP 规范通知不得携带 id。
func writeNotify(w io.Writer, method string, params any) error {
	return writeFrame(w, map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func writeFrame(w io.Writer, body map[string]any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if _, err = io.WriteString(w, "Content-Length: "+strconv.Itoa(len(payload))+"\r\n\r\n"); err != nil {
		return err
	}
	_, err = w.Write(payload)
	return err
}

func readUntilID(r *bufio.Reader, id int, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
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
				if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))); err == nil {
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
		payload := bytes.TrimSpace(buf)
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

func nowNano() int64 { return time.Now().UnixNano() }
