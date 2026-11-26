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
	once    sync.Once
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
	p.mu.RLock()
	fresh := len(p.cache) > 0 && nowNano()-p.stamp < 30*1e9
	warming := p.warming
	p.mu.RUnlock()
	if fresh || warming {
		return
	}
	p.once.Do(func() {
		p.mu.Lock()
		p.warming = true
		p.mu.Unlock()
		go func() {
			defer func() {
				p.mu.Lock()
				p.warming = false
				p.mu.Unlock()
			}()
			syms, ok := fetchSymbols(p.root, 2500*time.Millisecond)
			if !ok {
				return
			}
			p.mu.Lock()
			p.cache, p.stamp = syms, nowNano()
			p.mu.Unlock()
		}()
	})
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
		_ = writeLSP(stdin, 2, "initialized", map[string]any{})
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
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, "Content-Length: "+strconv.Itoa(len(payload))+"\r\n\r\n")
	if err != nil {
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
