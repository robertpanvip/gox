package goxlsp

// 浏览器 LSP 门面用到的最小协议类型。刻意不复用 internal/lsp 的完整协议包，
// 避免把 buildcheck（依赖 go build 外部进程）拖进 wasm。
type Position struct {
	Line      int `json:"line"`      // 0 基
	Character int `json:"character"` // 0 基
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Location struct {
	Range Range `json:"range"`
}

type Diagnostic struct {
	Range    Range   `json:"range"`
	Severity int     `json:"severity"` // 1 = Error
	Source   string  `json:"source"`
	Message  string  `json:"message"`
	Code     *string `json:"code,omitempty"`
}

type CompletionItem struct {
	Label  string `json:"label"`
	Kind   int    `json:"kind,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

type Hover struct {
	Contents struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	} `json:"contents"`
}

type DocumentSymbol struct {
	Name           string `json:"name"`
	Detail         string `json:"detail,omitempty"`
	Kind           int    `json:"kind"`
	Range          Range  `json:"range"`
	SelectionRange Range  `json:"selectionRange"`
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
