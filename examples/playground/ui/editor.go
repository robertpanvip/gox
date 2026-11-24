// editor.gx —— 用 GX 语言编写的 Ace 风格代码编辑器。
//
// 这是 playground 的核心组件，全部逻辑用 GX 表达（sig 源、pub 函数、
// 字符串处理、For 列表、动态属性），转译成标准 Go 后由任意 gxscene 后端渲染：
// 桌面（skia + GLFW）与网页（web + wasm）共用这一份源码，零条件编译。
//
// 编辑器数据模型：
//   - Source      sig string  当前代码文本
//   - CursorLine  sig int     光标所在行（0 基）
//   - CursorCol   sig int     光标所在列（rune 计，0 基）
//   - Focused     sig bool    是否有焦点（驱动光标行高亮）
//
// 命令式编辑操作（insert/backspace/newline/move）都是 pub 函数，装配层在
// 收到平台键盘事件后直接调用——不暴露 NodeID，不区分桌面还是网页。
package ui

import Signal "github.com/robertpanvip/gox/runtime/signal"

import (
    "strings"
    "strconv"
    gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// ---- 编辑器状态（包级 sig，单编辑器 playground 足够） ----

 var SourceCode = Signal.New("// 用 GX 写的编辑器，试着输入 GX 代码\nsig count = 0\nfunc main() {\n    count = count() + 1\n}")
 var CursorLine = Signal.New(0)
 var CursorCol = Signal.New(0)
 var EditorFocused = Signal.New(false)

// ---- 纯文本编辑逻辑（GX 编写，两端共用） ----

 func SplitLines(s string) []string {
    return strings.Split(s, "\n")
}

 func JoinLines(ls []string) string {
    return strings.Join(ls, "\n")
}

 func RuneLen(s string) int {
    return len([]rune(s))
}

// indices 生成 [0, n) 的索引切片，<For> 逐行渲染时用。
 func Indices(n int) []int {
    out := []int{}
    for i := 0; i < n; i = i + 1 {
        out = append(out, i)
    }
    return out
}

// insertText 在当前光标处插入一个字符串（可为多字符/中文），返回新光标列。
 func InsertText(s string, ln int, col int, ins string) (string, int) {
    lines := SplitLines(s)
    if ln < 0 { ln = 0 }
    if ln >= len(lines) { return s, col }
    rs := []rune(lines[ln])
    if col < 0 { col = 0 }
    if col > len(rs) { col = len(rs) }
    next := string(rs[:col]) + ins + string(rs[col:])
    lines[ln] = next
    return JoinLines(lines), col + RuneLen(ins)
}

// backspaceAt 删除光标前一个字符；行首退格则与上一行合并。返回新文本与新行列。
 func BackspaceAt(s string, ln int, col int) (string, int, int) {
    lines := SplitLines(s)
    if ln < 0 || ln >= len(lines) { return s, ln, col }
    rs := []rune(lines[ln])
    if col > len(rs) { col = len(rs) }
    if col > 0 {
        next := string(rs[:col-1]) + string(rs[col:])
        lines[ln] = next
        return JoinLines(lines), ln, col - 1
    }
    if ln > 0 {
        prev := []rune(lines[ln-1])
        merged := string(prev) + lines[ln]
        newLines := []string{}
        for i, l := range lines {
            if i == ln-1 {
                newLines = append(newLines, merged)
            } else if i != ln {
                newLines = append(newLines, l)
            }
        }
        return JoinLines(newLines), ln - 1, len(prev)
    }
    return s, ln, col
}

// newlineAt 在当前光标处断行，返回新文本与新行列。
 func NewlineAt(s string, ln int, col int) (string, int, int) {
    lines := SplitLines(s)
    if ln < 0 || ln >= len(lines) { return s, ln, col }
    rs := []rune(lines[ln])
    if col > len(rs) { col = len(rs) }
    left := string(rs[:col])
    right := string(rs[col:])
    newLines := []string{}
    for i, l := range lines {
        if i == ln {
            newLines = append(newLines, left, right)
        } else {
            newLines = append(newLines, l)
        }
    }
    return JoinLines(newLines), ln + 1, 0
}

// ---- 包级编辑命令：装配层收到键盘事件后调用 ----

 func EditorInsert(s string) {
    text, c := InsertText(SourceCode(), CursorLine(), CursorCol(), s)
    SourceCode(text)
    CursorCol(c)
    EditorFocused(true)
}

 func EditorBackspace() {
    text, ln, c := BackspaceAt(SourceCode(), CursorLine(), CursorCol())
    SourceCode(text)
    CursorLine(ln)
    CursorCol(c)
}

 func EditorNewline() {
    text, ln, c := NewlineAt(SourceCode(), CursorLine(), CursorCol())
    SourceCode(text)
    CursorLine(ln)
    CursorCol(c)
}

 func EditorMove(dln int, dcol int) {
    lines := SplitLines(SourceCode())
    ln := CursorLine()
    c := CursorCol()
    ln = ln + dln
    if ln < 0 { ln = 0 }
    if ln >= len(lines) { ln = len(lines) - 1 }
    rs := []rune(lines[ln])
    c = c + dcol
    if c < 0 { c = 0 }
    if c > len(rs) { c = len(rs) }
    CursorLine(ln)
    CursorCol(c)
}

 func EditorSetText(s string) {
    SourceCode(s)
    lines := SplitLines(s)
    if CursorLine() >= len(lines) { CursorLine(len(lines) - 1 )}
}

 func EditorFocus() { EditorFocused(true )}
 func EditorBlur()  { EditorFocused(false )}
 func EditorLine() int { return CursorLine() }
 func EditorCol() int { return CursorCol() }
 func EditorText() string { return SourceCode() }

// ---- 语法高亮（GX 编写的类 Ace 分词器） ----

 type Token struct {
     Text  string
     Color string
}

func isAlpha(b byte) bool {
    return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_' || b >= 0x80
}

func isAlnum(b byte) bool {
    return isAlpha(b) || (b >= '0' && b <= '9')
}

func isKeyword(w string) bool {
    switch w {
    case "func", "return", "if", "else", "for", "range", "var", "sig", "let",
         "pub", "type", "struct", "enum", "import", "package", "go", "defer",
         "nil", "true", "false", "any", "string", "int", "bool", "interface":
        return true
    }
    return false
}

// highlight 把一行 GX 源码切成带颜色的 token，拼回去等于原文。
 func Highlight(line string) []Token {
    var out []Token
    b := []byte(line)
    i := 0
    for i < len(b) {
        // 注释：行尾全绿
        if i+1 < len(b) && b[i] == '/' && b[i+1] == '/' {
            out = append(out, Token{Text: line[i:], Color: "#6a9955"})
            break
        }
        // 字符串：到下一个未转义引号
        if b[i] == '"' {
            j := i + 1
            for j < len(b) && b[j] != '"' {
                if b[j] == '\\' && j+1 < len(b) { j = j + 1 }
                j = j + 1
            }
            if j < len(b) { j = j + 1 }
            out = append(out, Token{Text: line[i:j], Color: "#ce9178"})
            i = j
            continue
        }
        // 标识符 / 关键字
        if isAlpha(b[i]) {
            j := i + 1
            for j < len(b) && isAlnum(b[j]) { j = j + 1 }
            w := line[i:j]
            c := "#d4d4d4"
            if isKeyword(w) { c = "#c586c0" }
            out = append(out, Token{Text: w, Color: c})
            i = j
            continue
        }
        // 数字
        if b[i] >= '0' && b[i] <= '9' {
            j := i + 1
            for j < len(b) && isAlnum(b[j]) { j = j + 1 }
            out = append(out, Token{Text: line[i:j], Color: "#b5cea8"})
            i = j
            continue
        }
        // 标点 / 空白：逐字节
        c := "#808080"
        if b[i] == ' ' || b[i] == '\t' { c = "#d4d4d4" }
        out = append(out, Token{Text: line[i : i+1], Color: c})
        i = i + 1
    }
    return out
}

// ---- 编辑器视图（GX JSX，两端共用） ----

// 每行固定 token 槽位数。GX 的 OpDynChild 会把首次产出的 *Instance 升级为稳定
// 子树，因此"每次重建整棵子树"不会刷新；正确做法是让每个可变值都是标量 getter。
// 这里每行固定 N 个 span 槽位，槽位文本/颜色都是读 sig 的 getter——既保留逐 token
// 高亮，又让 sourceCode 变化精确刷新各槽位。
 const MaxTokens = 14

// EditorLineText 导出给装配层诊断/集成用：读第 idx 行第 slot 个 token 文本。
 func EditorLineText(idx int, slot int) string { return lineText(idx, slot) }

func lineText(idx int, slot int) string {
    lines := SplitLines(SourceCode())
    if idx < 0 || idx >= len(lines) { return "" }
    toks := Highlight(lines[idx])
    if slot < 0 || slot >= len(toks) { return "" }
    return toks[slot].Text
}

 func LineTokenColor(idx int, slot int) string {
    lines := SplitLines(SourceCode())
    if idx < 0 || idx >= len(lines) { return "#d4d4d4" }
    toks := Highlight(lines[idx])
    if slot < 0 || slot >= len(toks) { return "#d4d4d4" }
    return toks[slot].Color
}

func lineGutterColor(idx int) string {
    if idx == CursorLine() && EditorFocused() { return "#0e639c" }
    return "#3a3d45"
}

func lineRowBg(idx int) string {
    if idx == CursorLine() && EditorFocused() { return "#2a2d3a" }
    return "#1e1e1e"
}

func lineNumber(idx int) string { return strconv.Itoa(idx + 1) }

// tokenSlots 渲染一行里的固定槽位 token span。slot 索引是编译期常量展开的
// 标量 getter，文本/颜色都读 sig。
func tokenSlots(idx int, slot int) *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"span",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"color",Slot:0,},gxscene.Op{Code:gxscene.OpAttr,Arg:"13",Key:"font-size",},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpClose,},},N:2,}, func() any{ return LineTokenColor(idx, slot) }, func() any{ return lineText(idx, slot) })
}

func slot0(idx int) *gxscene.Instance { return tokenSlots(idx, 0) }
func slot1(idx int) *gxscene.Instance { return tokenSlots(idx, 1) }
func slot2(idx int) *gxscene.Instance { return tokenSlots(idx, 2) }
func slot3(idx int) *gxscene.Instance { return tokenSlots(idx, 3) }
func slot4(idx int) *gxscene.Instance { return tokenSlots(idx, 4) }
func slot5(idx int) *gxscene.Instance { return tokenSlots(idx, 5) }
func slot6(idx int) *gxscene.Instance { return tokenSlots(idx, 6) }
func slot7(idx int) *gxscene.Instance { return tokenSlots(idx, 7) }
func slot8(idx int) *gxscene.Instance { return tokenSlots(idx, 8) }
func slot9(idx int) *gxscene.Instance { return tokenSlots(idx, 9) }
func slot10(idx int) *gxscene.Instance { return tokenSlots(idx, 10) }
func slot11(idx int) *gxscene.Instance { return tokenSlots(idx, 11) }
func slot12(idx int) *gxscene.Instance { return tokenSlots(idx, 12) }
func slot13(idx int) *gxscene.Instance { return tokenSlots(idx, 13) }

// lineView 渲染一行：行号 + 固定 token 槽位。所有可变属性都是 getter。
func lineView(idx int) *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"bg",Slot:0,},gxscene.Op{Code:gxscene.OpAttr,Arg:"22",Key:"height",},gxscene.Op{Code:gxscene.OpAttr,Arg:"row",Key:"flex-direction",},gxscene.Op{Code:gxscene.OpAttr,Arg:"center",Key:"align-items",},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"bg",Slot:1,},gxscene.Op{Code:gxscene.OpAttr,Arg:"#858585",Key:"color",},gxscene.Op{Code:gxscene.OpAttr,Arg:"42",Key:"width",},gxscene.Op{Code:gxscene.OpAttr,Arg:"12",Key:"font-size",},gxscene.Op{Code:gxscene.OpAttr,Arg:"right",Key:"text-align",},gxscene.Op{Code:gxscene.OpAttr,Arg:"4",Key:"padding",},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"row",Key:"flex-direction",},gxscene.Op{Code:gxscene.OpAttr,Arg:"6",Key:"padding",},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpDynChild,Slot:4,},gxscene.Op{Code:gxscene.OpDynChild,Slot:5,},gxscene.Op{Code:gxscene.OpDynChild,Slot:6,},gxscene.Op{Code:gxscene.OpDynChild,Slot:7,},gxscene.Op{Code:gxscene.OpDynChild,Slot:8,},gxscene.Op{Code:gxscene.OpDynChild,Slot:9,},gxscene.Op{Code:gxscene.OpDynChild,Slot:10,},gxscene.Op{Code:gxscene.OpDynChild,Slot:11,},gxscene.Op{Code:gxscene.OpDynChild,Slot:12,},gxscene.Op{Code:gxscene.OpDynChild,Slot:13,},gxscene.Op{Code:gxscene.OpDynChild,Slot:14,},gxscene.Op{Code:gxscene.OpDynChild,Slot:15,},gxscene.Op{Code:gxscene.OpDynChild,Slot:16,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:17,}, func() any{ return lineRowBg(idx) }, func() any{ return lineGutterColor(idx) }, func() any{ return lineNumber(idx) }, func() any{ return slot0(idx) }, func() any{ return slot1(idx) }, func() any{ return slot2(idx) }, func() any{ return slot3(idx) }, func() any{ return slot4(idx) }, func() any{ return slot5(idx) }, func() any{ return slot6(idx) }, func() any{ return slot7(idx) }, func() any{ return slot8(idx) }, func() any{ return slot9(idx) }, func() any{ return slot10(idx) }, func() any{ return slot11(idx) }, func() any{ return slot12(idx) }, func() any{ return slot13(idx) })
}

// Editor 渲染整个编辑器。each 直接读 sourceCode（行数变化即增删行），
// 每行的文本/颜色/高亮都是标量 getter（内容变化即精确刷新）。
 func Editor() *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"#1e1e1e",Key:"bg",},gxscene.Op{Code:gxscene.OpAttr,Arg:"8",Key:"border-radius",},gxscene.Op{Code:gxscene.OpAttr,Arg:"6",Key:"padding",},gxscene.Op{Code:gxscene.OpAttr,Arg:"column",Key:"flex-direction",},gxscene.Op{Code:gxscene.OpAttr,Arg:"0",Key:"gap",},gxscene.Op{Code:gxscene.OpAttr,Arg:"320",Key:"height",},gxscene.Op{Code:gxscene.OpFor,Slot:0,},gxscene.Op{Code:gxscene.OpClose,},},N:1,}, gxscene.ForGetterOfSignal[int](func() []int { return Indices(len(SplitLines(SourceCode()))) },nil,func(i func() int) *gxscene.Instance { return lineView(i()) }))
}

// EditorStatus 返回编辑器状态栏文本（行列 + 行数），供 playground 头部展示。
 func EditorStatus() string {
    return "Ln " + strconv.Itoa(CursorLine()+1) + ", Col " + strconv.Itoa(CursorCol()+1) +
           "  |  " + strconv.Itoa(len(SplitLines(SourceCode()))) + " lines"
}
