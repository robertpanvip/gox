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
	TextDocumentSync           int                    `json:"textDocumentSync"` // 2 = incremental, 1 = full
	DefinitionProvider         bool                   `json:"definitionProvider"`
	HoverProvider              bool                   `json:"hoverProvider"`
	CompletionProvider         *CompletionOptions     `json:"completionProvider,omitempty"`
	SignatureHelpProvider      *SignatureHelpOptions  `json:"signatureHelpProvider,omitempty"`
	DocumentSymbolProvider     bool                   `json:"documentSymbolProvider"`
	RenameProvider             bool                   `json:"renameProvider"`
	DocumentFormattingProvider bool                   `json:"documentFormattingProvider"`
	SemanticTokensProvider     *SemanticTokensOptions `json:"semanticTokensProvider,omitempty"`
	InlayHintProvider          bool                   `json:"inlayHintProvider,omitempty"`
	ReferencesProvider         bool                   `json:"referencesProvider,omitempty"`
	DocumentHighlightProvider  bool                   `json:"documentHighlightProvider,omitempty"`
	CodeActionProvider         *CodeActionOptions     `json:"codeActionProvider,omitempty"`
	DiagnosticProvider         *DiagnosticOptions     `json:"diagnosticProvider,omitempty"`
	WorkspaceSymbolProvider    bool                   `json:"workspaceSymbolProvider,omitempty"`
	TypeDefinitionProvider     bool                   `json:"typeDefinitionProvider,omitempty"`
	ImplementationProvider     bool                   `json:"implementationProvider,omitempty"`
	Workspace                  *WorkspaceOptions      `json:"workspace,omitempty"`
}

type CompletionOptions struct {
	TriggerCharacters []string `json:"triggerCharacters,omitempty"`
	ResolveProvider   bool     `json:"resolveProvider,omitempty"`
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
	Label         string `json:"label"`
	Kind          int    `json:"kind,omitempty"` // 3 = Function, 5 = Field, 6 = Variable
	Detail        string `json:"detail,omitempty"`
	Documentation string `json:"documentation,omitempty"`
	Data          string `json:"data,omitempty"` // "pkg.Name"，resolve 依据
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

// SignatureInformation 一条候选签名（LSP textDocument/signatureHelp 兼容子集）。
type SignatureInformation struct {
	Label      string                 `json:"label"`
	Parameters []ParameterInformation `json:"parameters,omitempty"`
}

// ParameterInformation 参数在 Label 内的字节区间。
type ParameterInformation struct {
	Label [2]int `json:"label"`
}

// SignatureHelp 签名帮助返回体；空 signatures 表示不适用。
type SignatureHelp struct {
	Signatures      []SignatureInformation `json:"signatures"`
	ActiveSignature int                    `json:"activeSignature"`
	ActiveParameter int                    `json:"activeParameter"`
}

// SignatureHelpOptions 签名帮助触发字符声明。
type SignatureHelpOptions struct {
	TriggerCharacters []string `json:"triggerCharacters,omitempty"`
}

// ---- semanticTokens / inlayHints（LSP 子集）----

// SemanticTokensParams 请求整篇文档的语义 token。
type SemanticTokensParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// SemanticTokensResult 编码数组：每 5 元一组 [deltaLine, deltaStart, length, typeIdx, modIdx]。
type SemanticTokensResult struct {
	Data []int `json:"data"`
}

// InlayHintParams 请求整篇文档的参数提示。
type InlayHintParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
}

// InlayHint 单条提示；v1 仅 kind=2（参数名），label 为纯文本。
type InlayHint struct {
	Position Position `json:"position"`
	Label    string   `json:"label"`
	Kind     int      `json:"kind,omitempty"`
}

// ---- organize / pull diagnostics（3.17）----

// CodeActionOptions 声明支持的 codeAction 种类。
type CodeActionOptions struct {
	CodeActionKinds []string `json:"codeActionKinds,omitempty"`
}

// DiagnosticOptions 拉取式诊断能力（v1 仅文档级，两项依赖声明 false）。
type DiagnosticOptions struct {
	InterFileDependencies bool `json:"interFileDependencies"`
	WorkspaceDiagnostics  bool `json:"workspaceDiagnostics"`
}

// DiagnosticParams textDocument/diagnostic 入参。
type DiagnosticParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	PreviousID   string                 `json:"previousResultId,omitempty"`
}

// DiagnosticReport 拉取式诊断报告。
type DiagnosticReport struct {
	Kind     string       `json:"kind"` // full | unchanged
	ResultID string       `json:"resultId,omitempty"`
	Items    []Diagnostic `json:"items"`
}

// ---- workspace 文件重命名（LSP 文件操作）----

// FileRename 一条 old→new 重命名。
type FileRename struct {
	OldURI string `json:"oldUri"`
	NewURI string `json:"newUri"`
}

// WorkspaceRenameFilesParams will/didRenameFiles 入参。
type WorkspaceRenameFilesParams struct {
	Files []FileRename `json:"files"`
}

// WorkspaceOptions 顶层 workspace 能力。
type WorkspaceOptions struct {
	FileOperations *FileOperationOptions `json:"fileOperations,omitempty"`
}

// FileOperationOptions 文件操作能力（v1 只做 rename 两拍）。
type FileOperationOptions struct {
	WillRenameFiles *FileOperationRegistrationOptions `json:"willRename,omitempty"`
	DidRenameFiles  *FileOperationRegistrationOptions `json:"didRename,omitempty"`
}

// FileOperationRegistrationOptions 过滤器声明。
type FileOperationRegistrationOptions struct {
	Filters []DocumentFilter `json:"filters,omitempty"`
}

// DocumentFilter v1 仅 glob。
type DocumentFilter struct {
	Pattern string `json:"pattern,omitempty"`
}
