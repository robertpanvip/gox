// Package lsp 实现一个最小可用的 GX 语言服务器（JSON-RPC over stdio）。
// 覆盖：initialize / didOpen / didChange / didClose / definition / hover /
// completion / documentSymbol / shutdown / exit，以及基于 go build 的实时诊断。
package lsp

import "encoding/json"

// ---- JSON-RPC 基础 ----

type RequestMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type ResponseMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type NotificationMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ---- 位置 ----

type Position struct {
	Line      int `json:"line"`      // 0 基
	Character int `json:"character"` // 0 基
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// ---- initialize ----

type InitializeParams struct {
	RootURI      string `json:"rootUri"`
	Capabilities struct {
	} `json:"capabilities"`
}

type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   ServerInfo         `json:"serverInfo"`
}

type ServerCapabilities struct {
	TextDocumentSync       int                    `json:"textDocumentSync"` // 2 = incremental, 1 = full
	DefinitionProvider     bool                   `json:"definitionProvider"`
	HoverProvider          bool                   `json:"hoverProvider"`
	CompletionProvider     *CompletionOptions     `json:"completionProvider,omitempty"`
	DocumentSymbolProvider      bool                   `json:"documentSymbolProvider"`
	RenameProvider              bool                   `json:"renameProvider"`
	DocumentFormattingProvider  bool                   `json:"documentFormattingProvider"`
	SemanticTokensProvider      *SemanticTokensOptions `json:"semanticTokensProvider,omitempty"`
}

type CompletionOptions struct {
	TriggerCharacters []string `json:"triggerCharacters,omitempty"`
}

type SemanticTokensOptions struct {
	Legend SemanticTokensLegend `json:"legend"`
	Full   bool                 `json:"full"`
	Range  bool                 `json:"range"`
}

type SemanticTokensLegend struct {
	TokenTypes     []string `json:"tokenTypes"`
	TokenModifiers []string `json:"tokenModifiers"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ---- 文本文档 ----

type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

type DidOpenParams struct {
	TextDocument struct {
		URI        string `json:"uri"`
		LanguageID string `json:"languageId"`
		Version    int    `json:"version"`
		Text       string `json:"text"`
	} `json:"textDocument"`
}

type DidChangeParams struct {
	TextDocument   VersionedTextDocumentIdentifier `json:"textDocument"`
	ContentChanges []struct {
		Text string `json:"text"`
	} `json:"contentChanges"`
}

type DidCloseParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// ---- definition / hover / completion ----

type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type Hover struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

type MarkupContent struct {
	Kind  string `json:"kind"` // "markdown" | "plaintext"
	Value string `json:"value"`
}

type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

type CompletionItem struct {
	Label  string `json:"label"`
	Kind   int    `json:"kind,omitempty"` // 3 = Function, 5 = Field, 6 = Variable
	Detail string `json:"detail,omitempty"`
}

type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"` // 12 = Function, 5 = Class/Struct
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// ---- rename ----

type RenameParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	NewName      string                 `json:"newName"`
}

type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// DocumentFormattingParams 是 textDocument/formatting 的请求参数。
// FormattingOptions 对 GX 目前无 tunable 项，忽略即可。
type DocumentFormattingParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type WorkspaceEdit struct {
	Changes map[string][]TextEdit `json:"changes"`
}

// ---- 诊断通知 ----

type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type Diagnostic struct {
	Range    Range   `json:"range"`
	Severity int     `json:"severity"` // 1 = Error
	Source   string  `json:"source"`
	Message  string  `json:"message"`
	Code     *string `json:"code,omitempty"`
}
