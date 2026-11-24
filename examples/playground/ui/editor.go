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

var rowVer0 = Signal.New(0)
var rowVer1 = Signal.New(0)
var rowVer2 = Signal.New(0)
var rowVer3 = Signal.New(0)
var rowVer4 = Signal.New(0)
var rowVer5 = Signal.New(0)
var rowVer6 = Signal.New(0)
var rowVer7 = Signal.New(0)
var rowVer8 = Signal.New(0)
var rowVer9 = Signal.New(0)
var rowVer10 = Signal.New(0)
var rowVer11 = Signal.New(0)
var rowVer12 = Signal.New(0)
var rowVer13 = Signal.New(0)
var rowVer14 = Signal.New(0)
var rowVer15 = Signal.New(0)
var rowVer16 = Signal.New(0)
var rowVer17 = Signal.New(0)
var rowVer18 = Signal.New(0)
var rowVer19 = Signal.New(0)
var rowVer20 = Signal.New(0)
var rowVer21 = Signal.New(0)
var rowVer22 = Signal.New(0)
var rowVer23 = Signal.New(0)

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
    setSource(text)
    CursorCol(c)
    EditorFocused(true)
}

 func EditorBackspace() {
    text, ln, c := BackspaceAt(SourceCode(), CursorLine(), CursorCol())
    setSource(text)
    CursorLine(ln)
    CursorCol(c)
}

 func EditorNewline() {
    text, ln, c := NewlineAt(SourceCode(), CursorLine(), CursorCol())
    setSource(text)
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
    setSource(s)
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
// 高亮，又让 sourceCode 变化精确刷新各槽位。N 取 24 覆盖常见代码行（含缩进）。
 const MaxTokens = 24

// 按键延迟优化：行级 token 缓存 + 每行独立版本信号。
//
// 旧实现里，每个 token span 的 text/color getter 都直接读 sourceCode()，于是
// 8 行 × 24 槽 × 2 = 384 个 effect 全都订阅 sourceCode。插入一个字符写 sourceCode，
// 384 个 effect 全部重跑（wasm 里实测 ~88ms/键），这是延迟的真正主因。
//
// 新实现：
//   - tokenCache[i] 缓存第 i 行的分词结果，全文只在 setSource 里分词一次；
//   - rowVers[i] 每行一个版本信号，token getter 只读自己行的版本（订阅粒度从
//     "整篇源码" 细化到 "单行"）；
//   - setSource 比对新旧每行，只 bump 内容变化行的版本 → 插入字符只重跑该行
//     24×2=48 个 effect（行数不变时 For 也短路），实测降到 ~11ms。
//
// 所有写 sourceCode 的编辑命令都改走 setSource，保证缓存与版本同步更新。
var tokenCache [][]Token

func rowVersion(idx int) int {
    if idx < 0 { return 0 }
    if idx >= 24 { return rowVer23() }
    if idx == 0 { return rowVer0() }
    if idx == 1 { return rowVer1() }
    if idx == 2 { return rowVer2() }
    if idx == 3 { return rowVer3() }
    if idx == 4 { return rowVer4() }
    if idx == 5 { return rowVer5() }
    if idx == 6 { return rowVer6() }
    if idx == 7 { return rowVer7() }
    if idx == 8 { return rowVer8() }
    if idx == 9 { return rowVer9() }
    if idx == 10 { return rowVer10() }
    if idx == 11 { return rowVer11() }
    if idx == 12 { return rowVer12() }
    if idx == 13 { return rowVer13() }
    if idx == 14 { return rowVer14() }
    if idx == 15 { return rowVer15() }
    if idx == 16 { return rowVer16() }
    if idx == 17 { return rowVer17() }
    if idx == 18 { return rowVer18() }
    if idx == 19 { return rowVer19() }
    if idx == 20 { return rowVer20() }
    if idx == 21 { return rowVer21() }
    if idx == 22 { return rowVer22() }
    if idx == 23 { return rowVer23() }
    return 0
}

func bumpRow(idx int) {
    if idx < 0 { return }
    if idx >= 24 { idx = 23 }
    if idx == 0 { rowVer0(rowVer0() + 1); return }
    if idx == 1 { rowVer1(rowVer1() + 1); return }
    if idx == 2 { rowVer2(rowVer2() + 1); return }
    if idx == 3 { rowVer3(rowVer3() + 1); return }
    if idx == 4 { rowVer4(rowVer4() + 1); return }
    if idx == 5 { rowVer5(rowVer5() + 1); return }
    if idx == 6 { rowVer6(rowVer6() + 1); return }
    if idx == 7 { rowVer7(rowVer7() + 1); return }
    if idx == 8 { rowVer8(rowVer8() + 1); return }
    if idx == 9 { rowVer9(rowVer9() + 1); return }
    if idx == 10 { rowVer10(rowVer10() + 1); return }
    if idx == 11 { rowVer11(rowVer11() + 1); return }
    if idx == 12 { rowVer12(rowVer12() + 1); return }
    if idx == 13 { rowVer13(rowVer13() + 1); return }
    if idx == 14 { rowVer14(rowVer14() + 1); return }
    if idx == 15 { rowVer15(rowVer15() + 1); return }
    if idx == 16 { rowVer16(rowVer16() + 1); return }
    if idx == 17 { rowVer17(rowVer17() + 1); return }
    if idx == 18 { rowVer18(rowVer18() + 1); return }
    if idx == 19 { rowVer19(rowVer19() + 1); return }
    if idx == 20 { rowVer20(rowVer20() + 1); return }
    if idx == 21 { rowVer21(rowVer21() + 1); return }
    if idx == 22 { rowVer22(rowVer22() + 1); return }
    if idx == 23 { rowVer23(rowVer23() + 1); return }
}

// setSource 是写 sourceCode 的唯一入口：重算 token 缓存、只 bump 变化行版本、
// 并在一个 Batch 内写回 sourceCode（行数变化时 For 据此增删行）。
func setSource(text string) {
    oldLines := SplitLines(SourceCode())
    newLines := SplitLines(text)
    // 一次分词，整表刷新。
    cache := [][]Token{}
    for i := 0; i < len(newLines); i = i + 1 {
        cache = append(cache, Highlight(newLines[i]))
    }
    tokenCache = cache
    Signal.Batch(func() {
        for i := 0; i < len(newLines); i = i + 1 {
            // 新行或内容变化的行才 bump；未变行的 token getter 不重跑。
            if i >= len(oldLines) || oldLines[i] != newLines[i] {
                bumpRow(i)
            }
        }
        SourceCode(text)
    })
}

// EditorLineText 导出给装配层诊断/集成用：读第 idx 行第 slot 个 token 文本。
 func EditorLineText(idx int, slot int) string { return lineText(idx, slot) }

// 懒初始化：首次渲染时 tokenCache 尚未由 setSource 建立（初始 sourceCode
// 是包级 sig 字面量，未经编辑命令），此时读一次 sourceCode 全量搭表，保证首帧有内容。
func ensureTokenCache() {
    if tokenCache != nil { return }
    s := SourceCode()
    lines := SplitLines(s)
    cache := [][]Token{}
    for i := 0; i < len(lines); i = i + 1 {
        cache = append(cache, Highlight(lines[i]))
    }
    tokenCache = cache
}

func lineText(idx int, slot int) string {
    _ = rowVersion(idx) // 订阅本行版本；本行变化才重跑此 getter
    ensureTokenCache()
    if idx < 0 || idx >= len(tokenCache) { return "" }
    toks := tokenCache[idx]
    if slot < 0 || slot >= len(toks) { return "" }
    return toks[slot].Text
}

 func LineTokenColor(idx int, slot int) string {
    _ = rowVersion(idx)
    ensureTokenCache()
    if idx < 0 || idx >= len(tokenCache) { return "#d4d4d4" }
    toks := tokenCache[idx]
    if slot < 0 || slot >= len(toks) { return "#d4d4d4" }
    return toks[slot].Color
}

// lineGutterColor 与整行同色：行号列不再用独立背景块，避免视觉上把编辑器割成两条。
// 光标行用左侧强调条（border-left）表达，背景仍与整行一致。
func lineGutterColor(idx int) string {
    return lineRowBg(idx)
}

func lineRowBg(idx int) string {
    if idx == CursorLine() && EditorFocused() { return "#2a2d3a" }
    return "#1e1e1e"
}

func lineNumber(idx int) string { return strconv.Itoa(idx + 1) }

// tokenSlots 渲染一行里的固定槽位 token span。slot 索引是编译期常量展开的
// 标量 getter，文本/颜色都读 sig。
// white-space="pre" 让每个 token span 内部的空格/制表符原样保留；否则这些 span
// 处于 flex 行容器中，纯空白 flex item 会被浏览器折叠掉（代码会挤成 "signame"）。
func tokenSlots(idx int, slot int) *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"span",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"color",Slot:0,},gxscene.Op{Code:gxscene.OpAttr,Arg:"pre",Key:"white-space",},gxscene.Op{Code:gxscene.OpAttr,Arg:"13",Key:"font-size",},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpClose,},},N:2,}, func() any{ return LineTokenColor(idx, slot) }, func() any{ return lineText(idx, slot) })
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
func slot14(idx int) *gxscene.Instance { return tokenSlots(idx, 14) }
func slot15(idx int) *gxscene.Instance { return tokenSlots(idx, 15) }
func slot16(idx int) *gxscene.Instance { return tokenSlots(idx, 16) }
func slot17(idx int) *gxscene.Instance { return tokenSlots(idx, 17) }
func slot18(idx int) *gxscene.Instance { return tokenSlots(idx, 18) }
func slot19(idx int) *gxscene.Instance { return tokenSlots(idx, 19) }
func slot20(idx int) *gxscene.Instance { return tokenSlots(idx, 20) }
func slot21(idx int) *gxscene.Instance { return tokenSlots(idx, 21) }
func slot22(idx int) *gxscene.Instance { return tokenSlots(idx, 22) }
func slot23(idx int) *gxscene.Instance { return tokenSlots(idx, 23) }

// lineView 渲染一行：行号 + 固定 token 槽位。所有可变属性都是 getter。
// 行号列与代码区同背景色，整行连续；代码容器 white-space=pre 保留 token 内空白。
func lineView(idx int) *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"bg",Slot:0,},gxscene.Op{Code:gxscene.OpAttr,Arg:"22",Key:"height",},gxscene.Op{Code:gxscene.OpAttr,Arg:"row",Key:"flex-direction",},gxscene.Op{Code:gxscene.OpAttr,Arg:"center",Key:"align-items",},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"bg",Slot:1,},gxscene.Op{Code:gxscene.OpAttr,Arg:"#858585",Key:"color",},gxscene.Op{Code:gxscene.OpAttr,Arg:"42",Key:"width",},gxscene.Op{Code:gxscene.OpAttr,Arg:"12",Key:"font-size",},gxscene.Op{Code:gxscene.OpAttr,Arg:"right",Key:"text-align",},gxscene.Op{Code:gxscene.OpAttr,Arg:"4",Key:"padding",},gxscene.Op{Code:gxscene.OpDynChild,Slot:2,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"row",Key:"flex-direction",},gxscene.Op{Code:gxscene.OpAttr,Arg:"6",Key:"padding",},gxscene.Op{Code:gxscene.OpAttr,Arg:"pre",Key:"white-space",},gxscene.Op{Code:gxscene.OpDynChild,Slot:3,},gxscene.Op{Code:gxscene.OpDynChild,Slot:4,},gxscene.Op{Code:gxscene.OpDynChild,Slot:5,},gxscene.Op{Code:gxscene.OpDynChild,Slot:6,},gxscene.Op{Code:gxscene.OpDynChild,Slot:7,},gxscene.Op{Code:gxscene.OpDynChild,Slot:8,},gxscene.Op{Code:gxscene.OpDynChild,Slot:9,},gxscene.Op{Code:gxscene.OpDynChild,Slot:10,},gxscene.Op{Code:gxscene.OpDynChild,Slot:11,},gxscene.Op{Code:gxscene.OpDynChild,Slot:12,},gxscene.Op{Code:gxscene.OpDynChild,Slot:13,},gxscene.Op{Code:gxscene.OpDynChild,Slot:14,},gxscene.Op{Code:gxscene.OpDynChild,Slot:15,},gxscene.Op{Code:gxscene.OpDynChild,Slot:16,},gxscene.Op{Code:gxscene.OpDynChild,Slot:17,},gxscene.Op{Code:gxscene.OpDynChild,Slot:18,},gxscene.Op{Code:gxscene.OpDynChild,Slot:19,},gxscene.Op{Code:gxscene.OpDynChild,Slot:20,},gxscene.Op{Code:gxscene.OpDynChild,Slot:21,},gxscene.Op{Code:gxscene.OpDynChild,Slot:22,},gxscene.Op{Code:gxscene.OpDynChild,Slot:23,},gxscene.Op{Code:gxscene.OpDynChild,Slot:24,},gxscene.Op{Code:gxscene.OpDynChild,Slot:25,},gxscene.Op{Code:gxscene.OpDynChild,Slot:26,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:27,}, func() any{ return lineRowBg(idx) }, func() any{ return lineGutterColor(idx) }, func() any{ return lineNumber(idx) }, func() any{ return slot0(idx) }, func() any{ return slot1(idx) }, func() any{ return slot2(idx) }, func() any{ return slot3(idx) }, func() any{ return slot4(idx) }, func() any{ return slot5(idx) }, func() any{ return slot6(idx) }, func() any{ return slot7(idx) }, func() any{ return slot8(idx) }, func() any{ return slot9(idx) }, func() any{ return slot10(idx) }, func() any{ return slot11(idx) }, func() any{ return slot12(idx) }, func() any{ return slot13(idx) }, func() any{ return slot14(idx) }, func() any{ return slot15(idx) }, func() any{ return slot16(idx) }, func() any{ return slot17(idx) }, func() any{ return slot18(idx) }, func() any{ return slot19(idx) }, func() any{ return slot20(idx) }, func() any{ return slot21(idx) }, func() any{ return slot22(idx) }, func() any{ return slot23(idx) })
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
