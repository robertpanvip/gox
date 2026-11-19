package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/robertpanvip/gox/internal/buildcheck"
)

// Server 是 GX 语言服务器。
type Server struct {
	in     io.Reader
	out    io.Writer
	docs   map[string]*document // uri -> 文档状态
	checker *buildcheck.Checker
}

type document struct {
	Text   string
	Symbols *SymbolMap
	SymbolsDirty bool
}

// NewServer 创建 server；checker 可为 nil（将内部创建默认检查器）。
func NewServer(in io.Reader, out io.Writer, checker *buildcheck.Checker) *Server {
	if checker == nil {
		checker = buildcheck.New(64)
		// 生成的 Go 代码声明 go 1.27；固定 toolchain 避免默认 go 联网下载卡住。
		checker.SetToolchain("go", "GOTOOLCHAIN=go1.27.3")
	}
	return &Server{
		in:      in,
		out:     out,
		docs:    map[string]*document{},
		checker: checker,
	}
}

// Checker 返回内部检查器（供注入 toolchain）。
func (s *Server) Checker() *buildcheck.Checker { return s.checker }

// Run 启动主循环，直到 stdin 关闭或收到 exit。
func (s *Server) Run() error {
	r := bufio.NewReader(s.in)
	w := bufio.NewWriter(s.out)
	defer w.Flush()

	for {
		req, body, err := readMessage(r)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if req == nil { // 通知解析失败，跳过
			continue
		}
		if req.Method == "exit" {
			return nil
		}

		if req.ID == nil { // 通知
			s.handleNotification(req, body, w)
			continue
		}

		result, rpcErr := s.handleRequest(req, body)
		resp := ResponseMessage{JSONRPC: "2.0", ID: req.ID}
		if rpcErr != nil {
			resp.Error = rpcErr
		} else {
			resp.Result = result
		}
		if err := writeMessage(w, resp); err != nil {
			return err
		}
		w.Flush()
	}
}

func (s *Server) handleRequest(req *RequestMessage, body []byte) (any, *RPCError) {
	switch req.Method {
	case "initialize":
		return InitializeResult{
			Capabilities: ServerCapabilities{
				TextDocumentSync:       1, // full
				DefinitionProvider:     true,
				HoverProvider:          true,
				CompletionProvider:     &CompletionOptions{TriggerCharacters: []string{"."}},
				DocumentSymbolProvider: true,
				RenameProvider:         true,
			},
			ServerInfo: ServerInfo{Name: "gox-lsp", Version: "0.1.0"},
		}, nil
	case "shutdown":
		return nil, nil
	case "textDocument/definition":
		return s.definition(body)
	case "textDocument/hover":
		return s.hover(body)
	case "textDocument/completion":
		return s.completion(body)
	case "textDocument/documentSymbol":
		return s.documentSymbol(body)
	case "textDocument/rename":
		return s.rename(body)
	default:
		return nil, &RPCError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)}
	}
}

func (s *Server) handleNotification(req *RequestMessage, body []byte, w *bufio.Writer) {
	switch req.Method {
	case "initialized":
		// no-op
	case "textDocument/didOpen":
		var p DidOpenParams
		if json.Unmarshal(body, &p) == nil {
			uri := p.TextDocument.URI
			s.docs[uri] = &document{Text: p.TextDocument.Text, SymbolsDirty: true}
			s.publishDiagnostics(uri, w)
		}
	case "textDocument/didChange":
		var p DidChangeParams
		if json.Unmarshal(body, &p) == nil {
			if d := s.docs[p.TextDocument.URI]; d != nil && len(p.ContentChanges) > 0 {
				d.Text = p.ContentChanges[len(p.ContentChanges)-1].Text
				d.SymbolsDirty = true
				s.publishDiagnostics(p.TextDocument.URI, w)
			}
		}
	case "textDocument/didClose":
		var p DidCloseParams
		if json.Unmarshal(body, &p) == nil {
			delete(s.docs, p.TextDocument.URI)
			// 清空诊断
			if err := writeMessage(w, NotificationMessage{
				JSONRPC: "2.0", Method: "textDocument/publishDiagnostics",
				Params: mustJSON(PublishDiagnosticsParams{URI: p.TextDocument.URI, Diagnostics: []Diagnostic{}}),
			}); err != nil {
				_ = err
			}
		}
	}
}

func (s *Server) publishDiagnostics(uri string, w *bufio.Writer) {
	d := s.docs[uri]
	if d == nil {
		return
	}
	diags, _, err := compileAndCheck(d.Text, s.checker)
	if err != nil && len(diags) == 0 {
		diags = []Diagnostic{{
			Range: Range{Start: Position{}, End: Position{}}, Severity: 1, Source: "gox", Message: err.Error(),
		}}
	}
	notif := NotificationMessage{
		JSONRPC: "2.0", Method: "textDocument/publishDiagnostics",
		Params: mustJSON(PublishDiagnosticsParams{URI: uri, Diagnostics: diags}),
	}
	_ = writeMessage(w, notif)
}

func (s *Server) docForURI(uri string) (*document, bool) {
	d, ok := s.docs[uri]
	return d, ok
}

func (s *Server) symbolMap(d *document) *SymbolMap {
	if d.Symbols == nil || d.SymbolsDirty {
		d.Symbols = BuildSymbolMap(d.Text)
		d.SymbolsDirty = false
	}
	return d.Symbols
}

func (s *Server) definition(body []byte) (any, *RPCError) {
	var p TextDocumentPositionParams
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, &RPCError{Code: -32602, Message: err.Error()}
	}
	d, ok := s.docForURI(p.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	sm := s.symbolMap(d)
	lines := strings.Split(d.Text, "\n")
	sym := sm.At(lines, p.Position.Line, p.Position.Character)
	if sym == nil {
		return nil, nil
	}
	loc := Location{
		URI: p.TextDocument.URI,
		Range: Range{
			Start: Position{Line: sym.Line, Character: sym.Col},
			End:   Position{Line: sym.Line, Character: sym.Col + len(sym.Name)},
		},
	}
	return loc, nil
}

func (s *Server) hover(body []byte) (any, *RPCError) {
	var p TextDocumentPositionParams
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, &RPCError{Code: -32602, Message: err.Error()}
	}
	d, ok := s.docForURI(p.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	sm := s.symbolMap(d)
	lines := strings.Split(d.Text, "\n")
	sym := sm.At(lines, p.Position.Line, p.Position.Character)
	if sym == nil {
		return nil, nil
	}
	value := fmt.Sprintf("**%s** — %s\n\n`%s`", sym.Name, sym.Doc, sym.Detail)
	return Hover{Contents: MarkupContent{Kind: "markdown", Value: value}}, nil
}

func (s *Server) completion(body []byte) (any, *RPCError) {
	var p TextDocumentPositionParams
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, &RPCError{Code: -32602, Message: err.Error()}
	}
	d, ok := s.docForURI(p.TextDocument.URI)
	if !ok {
		return CompletionList{}, nil
	}
	sm := s.symbolMap(d)
	items := make([]CompletionItem, 0, len(sm.Symbols))
	seen := map[string]bool{}
	for _, sym := range sm.Symbols {
		if seen[sym.Name] {
			continue
		}
		seen[sym.Name] = true
		items = append(items, CompletionItem{Label: sym.Name, Kind: sym.Kind, Detail: sym.Detail})
	}
	// Go/gx 关键字与常用内置
	for _, kw := range []string{"func", "pub", "type", "enum", "var", "const", "import", "return", "map", "filter", "reduce"} {
		items = append(items, CompletionItem{Label: kw, Kind: 14, Detail: "keyword"})
	}
	return CompletionList{IsIncomplete: false, Items: items}, nil
}

func (s *Server) documentSymbol(body []byte) (any, *RPCError) {
	var p struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, &RPCError{Code: -32602, Message: err.Error()}
	}
	d, ok := s.docForURI(p.TextDocument.URI)
	if !ok {
		return []DocumentSymbol{}, nil
	}
	sm := s.symbolMap(d)
	out := make([]DocumentSymbol, 0, len(sm.Symbols))
	for _, sym := range sm.Symbols {
		r := Range{
			Start: Position{Line: sym.Line, Character: sym.Col},
			End:   Position{Line: sym.Line, Character: sym.Col + len(sym.Name)},
		}
		out = append(out, DocumentSymbol{
			Name: sym.Name, Kind: sym.Kind, Range: r, SelectionRange: r, Detail: sym.Detail,
		})
	}
	return out, nil
}

// ---- JSON-RPC 帧读写 ----

func readMessage(r *bufio.Reader) (*RequestMessage, []byte, error) {
	var length int
	seen := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if err == io.EOF && seen {
				break
			}
			return nil, nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if seen {
				break
			}
			continue // 跳过帧前空行
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			v := strings.TrimSpace(line[len("content-length:"):])
			length, _ = strconv.Atoi(v)
			seen = true
		}
		// 忽略其它头（Content-Type 等）
	}
	if !seen {
		return nil, nil, nil
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, nil, err
	}
	var req RequestMessage
	if err := json.Unmarshal(buf, &req); err != nil {
		return nil, buf, nil
	}
	// 各 handler 期望拿到 params 对象本身（顶层 textDocument/position 等字段），
	// 而不是包含 jsonrpc/method/params 的完整消息。
	return &req, req.Params, nil
}

func writeMessage(w *bufio.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(data))
	_, err = w.Write(data)
	return err
}

// rename 实现作用域感知重命名：
// 普通符号按名字精确替换所有出现处；枚举成员按三种拼写派生替换——
// 简名 green -> 新简名、全名 ColorGreen -> Color<新简名>、跨包 colorGreen -> color<新简名>。
func (s *Server) rename(body []byte) (any, *RPCError) {
	var p RenameParams
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, &RPCError{Code: -32602, Message: err.Error()}
	}
	d, ok := s.docForURI(p.TextDocument.URI)
	if !ok {
		return nil, &RPCError{Code: -32602, Message: "document not opened"}
	}
	lines := splitLines(d.Text)
	sm := s.symbolMap(d)
	sym := sm.At(lines, p.Position.Line, p.Position.Character)
	if sym == nil {
		return nil, &RPCError{Code: -32602, Message: "no symbol at position"}
	}
	newName := strings.TrimSpace(p.NewName)
	if newName == "" {
		return nil, &RPCError{Code: -32602, Message: "empty newName"}
	}

	// 构造「旧拼写 -> 新拼写」映射
	replacements := map[string]string{}
	if sym.IsEnumMember() {
		short := sym.Name // 声明处简名
		newShort := newName
		// 若用户传的是全名 ColorYellow 或跨包 colorYellow，提取成员简名 yellow
		prefix := sym.EnumType
		fullPrefix := prefix
		crossPrefix := lowerFirstLocal(prefix)
		if strings.HasPrefix(newName, fullPrefix) && len(newName) > len(fullPrefix) {
			newShort = lowerFirstLocal(newName[len(fullPrefix):])
		} else if strings.HasPrefix(newName, crossPrefix) && len(newName) > len(crossPrefix) {
			rest := newName[len(crossPrefix):]
			if rest != "" && rest[0] >= 'A' && rest[0] <= 'Z' {
				newShort = lowerFirstLocal(rest)
			}
		}
		replacements[short] = newShort
		replacements[prefix+upperFirstLocal(short)] = prefix + upperFirstLocal(newShort)
		replacements[lowerFirstLocal(prefix+upperFirstLocal(short))] = lowerFirstLocal(prefix + upperFirstLocal(newShort))
	} else {
		replacements[sym.Name] = newName
	}

	edits := findIdentifierEdits(lines, replacements)
	return WorkspaceEdit{Changes: map[string][]TextEdit{p.TextDocument.URI: edits}}, nil
}

// findIdentifierEdits 扫描所有行的标识符，按 replacements 精确替换，编辑范围按实际拼写宽度。
func findIdentifierEdits(lines []string, replacements map[string]string) []TextEdit {
	var edits []TextEdit
	for li, line := range lines {
		i := 0
		for i < len(line) {
			if !isIdentByte(line[i]) {
				i++
				continue
			}
			start := i
			for i < len(line) && isIdentByte(line[i]) {
				i++
			}
			word := line[start:i]
			if repl, ok := replacements[word]; ok && repl != word {
				edits = append(edits, TextEdit{
					Range: Range{
						Start: Position{Line: li, Character: start},
						End:   Position{Line: li, Character: i},
					},
					NewText: repl,
				})
			}
		}
	}
	return edits
}

func mustJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
