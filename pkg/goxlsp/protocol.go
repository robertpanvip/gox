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
	Data   string `json:"data,omitempty"` // "pkg.Name"，lspResolveDoc 的入参
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

// DefinitionResult 跳转结果。Kind=local：Range 为 .gx 位置（0 基），宿主移动光标即可；
// Kind=std：Pkg/File/Line 指向内嵌 std 源，Snippet/Doc 为声明速览（弹窗），
// 需要全文再走 StdSourceJSON 开只读视图。
type DefinitionResult struct {
	Kind    string `json:"kind"`
	Range   *Range `json:"range,omitempty"`
	Pkg     string `json:"pkg,omitempty"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
	Snippet string `json:"snippet,omitempty"`
	Doc     string `json:"doc,omitempty"`
}

// Reference 浏览器面的一次引用。Pkg 空=用户文件内（Line/Col 为 .gx 0 基）；
// 外部声明点带 Pkg/File（内嵌源 1 基坐标），供速览卡展示。
type Reference struct {
	Pkg   string `json:"pkg,omitempty"`
	File  string `json:"file,omitempty"`
	Line  int    `json:"line"`
	Col   int    `json:"col"`
	Name  string `json:"name,omitempty"`
	IsDef bool   `json:"isDef,omitempty"`
}

// TextEdit 最小行编辑：Delete=true 删除 Line 行；否则在 Line/Col 前插入 Text。0 基。
type TextEdit struct {
	Line   int    `json:"line"`
	Col    int    `json:"col,omitempty"`
	Text   string `json:"text,omitempty"`
	Delete bool   `json:"delete,omitempty"`
}

// CodeAction QuickFix 动作（title + 单条编辑）。
type CodeAction struct {
	Title string    `json:"title"`
	Edit  *TextEdit `json:"edit,omitempty"`
}
