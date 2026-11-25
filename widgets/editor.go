// editor.gx —— Ace 风格代码编辑器（公共组件，实例化 + Apple 主题）。
//
// 从 playground/ui/editor.gx 抽离为组件库公共组件：
//   - 状态封装在 EditorState 实例中（多实例/多 App 隔离，与 ModalState 范式一致）。
//   - 配色走 Apple Theme token（UseToken），明暗双主题随 ThemeProvider/SetTheme 切换。
//   - 命令是实例方法（st.Insert / st.Undo / st.SetCursor ...），装配层持有 state 句柄。
//   - 渲染 Editor(st) 用同一份 GX JSX，三端（Skia/Web/Rust）共用，零条件编译。
//
// EditorState 的响应式字段用 GX 的 sig 字段声明，由构造函数按编译器六档语义构造，
// 不手写底层大写 API。
package widgets

import (
    "strings"
    "strconv"
    Rope "github.com/robertpanvip/gox/runtime/rope"
    Signal "github.com/robertpanvip/gox/runtime/signal"
    gxscene "github.com/robertpanvip/gox/runtime/scene"
)

const editorDefaultSource = "// 用 GX 写的编辑器，试着输入 GX 代码\nsig count = 0\nfunc main() {\n    count = count + 1\n}"

// editorCodeFont 是代码行的等宽字体栈（三端统一）。
// Web 原生消费整条栈；Skia/Rust 各自解析出本机等宽族名（见 font.go / render.rs）。
// 与原先 editor.css 的 .gx-code-line 规则完全一致，使该 CSS 可整块移除。
const editorCodeFont = "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace"

// 虚拟滚动的几何常量：行高固定（等宽代码），视口与外层容器高度。
// 行高固定让 VirtualList 走显式 height 模式——零回流、纯窗口计算。
const (
    editorRowHeight      = 22
    editorViewportHeight = 470
    editorOverscan       = 3
)

// ---- 编辑器实例状态 ----

// EditorState 是一个编辑器实例的全部可变状态。每个实例互不共享，
// 可在同一 App 内并存多个，或被不同 App 分别持有（多 App 隔离）。
type EditorState struct {
    sourceCode Signal.Signal[string]
    doc           *Rope.Rope        // 文本权威：持久 Rope 根，编辑走 O(log N)；sourceCode 是它的字符串派生
    cursorLine Signal.Signal[int]
    cursorCol Signal.Signal[int]
    selAnchorLine Signal.Signal[int]
    selAnchorCol Signal.Signal[int]
    focused Signal.Signal[bool]
    lineCount Signal.Signal[int]
    scrollTop Signal.Signal[float64] // 受控滚动偏移（虚拟滚动的窗口起点）
    scrollTopVal  float64     // scrollTop 的非响应式镜像：供 effect 读而不订阅（避免跟随回环）
    charWidth Signal.Signal[float64]
    wideWidth Signal.Signal[float64]
    caretSettled Signal.Signal[bool]

    rowVers *Signal.Vec[int] // 每行的独立版本信号（细粒度订阅）
    undoStack []editSnapshot
    redoStack []editSnapshot

    tokenCache    [][]token
    lineTextCache []string
    textCache     []string
    gradientCache []string

    palette *editorPalette // 当前主题的语法色 getter（Editor 建树期建立）
}

// NewEditorState 创建一个编辑器实例。source 为空时用默认示例源码。
func NewEditorState(source string) *EditorState {
    if source == "" { source = editorDefaultSource }
    vers := make([]int, 24)
    lc := Rope.Of(source).LineCount()
    st := &EditorState{
        sourceCode:    Signal.LockedGetterShared("sf:source:3030",func() string { return source },
func(__v string) { source = __v },
),
        doc:           Rope.Of(source),
        cursorLine:    Signal.New(0),
        cursorCol:     Signal.New(0),
        selAnchorLine: Signal.New(0),
        selAnchorCol:  Signal.New(0),
        focused:       Signal.New(false),
        lineCount:     Signal.LockedGetterShared("sf:lc:3145",func() int { return lc },
func(__v int) { lc = __v },
),
        scrollTop:     Signal.New(0.0),
        charWidth:     Signal.New(7.83),
        wideWidth:     Signal.New(13.02),
        caretSettled:  Signal.New(true),
        rowVers:       Signal.NewVec(vers),
    }
    // 惰性解析：不再全文 tokenize。缓存切片按行数预留（nil 元素 = 该行未解析），
    // 首屏 O(1)；行进入可见窗口时由 lineText/lineCodeGradient 现场解析并缓存。
    st.ensureCacheLen(lc)
    st.rowVers.SetLen(lc) // 行数 >64 时 Vec 自动迁移到 pvector 大后端
    return st
}

// ensureCacheLen 把三个按行缓存的长度对齐到 n。新增行为 nil（惰性占位），
// 不触发任何 tokenize；缩容直接截断。
func (s *EditorState) ensureCacheLen(n int) {
    if n < 0 { n = 0 }
    s.lineTextCache = resizeStr(s.lineTextCache, n)
    s.textCache = resizeStr(s.textCache, n)
    s.tokenCache = resizeTokens(s.tokenCache, n)
}

func resizeStr(in []string, n int) []string {
    if len(in) == n { return in }
    out := make([]string, n)
    copy(out, in)
    return out
}
func resizeTokens(in [][]token, n int) [][]token {
    if len(in) == n { return in }
    out := make([][]token, n)
    copy(out, in)
    return out
}

// editSnapshot / token 是实例内部数据类型（值类型，不参与响应式）。
// Doc 保存编辑前的 Rope 根指针：持久结构共享让快照成本从 O(N) 全文拷贝
// 降为 O(1) 指针赋值；Text 保留兼容（供尚走 string 路径的调用方读取）。
type editSnapshot struct { Doc *Rope.Rope; Text string; Ln int; Col int }
type token struct { Text string; Color string } // Color 存语义 token 名，渲染时按主题解析

// ---- 语法色 token 解析 ----
// 语法色的 getter 必须在 Editor 建树期通过 UseToken 建立（Context 依赖 Owner）。
// palette 缓存这些 getter；渲染 getter 读 palette.color(name)() 时会登记主题依赖，
// 主题切换自动重绘。palette 未建立（如测试直接调命令）时用深色常量兜底。

type editorPalette struct {
    text, keyword, str, number, comment, tag, attr, punct func() string
    bg, gutter, active, caret, sel, selSolid func() string
}

func newEditorPalette() *editorPalette {
    return &editorPalette{
        text: UseToken("syntaxtext"), keyword: UseToken("syntaxkeyword"),
        str: UseToken("syntaxstring"), number: UseToken("syntaxnumber"),
        comment: UseToken("syntaxcomment"), tag: UseToken("syntaxtag"),
        attr: UseToken("syntaxattr"), punct: UseToken("syntaxpunct"),
        bg: UseToken("editorbg"), gutter: UseToken("editorgutter"),
        active: UseToken("editoractive"), caret: UseToken("editorcaret"),
        sel: UseToken("editorselect"),
        selSolid: UseToken("editorselectsolid"),
    }
}

func defaultSyntaxColor(name string) string {
    switch name {
    case "keyword": return "#c586c0"
    case "str": return "#ce9178"
    case "number": return "#b5cea8"
    case "comment": return "#6a9955"
    case "tag": return "#569cd6"
    case "attr": return "#9cdcfe"
    case "punct": return "#808080"
    case "gutter": return "#858585"
    case "active": return "#2a2d3a"
    case "caret": return "#aeafad"
    case "select": return "#264f7899"
    case "selectSolid": return "#264f78"
    case "bg": return "#1e1e1e"
    default: return "#d4d4d4"
    }
}

func (s *EditorState) color(name string) string {
    if s.palette != nil {
        var g func() string
        switch name {
        case "keyword": g = s.palette.keyword
        case "str": g = s.palette.str
        case "number": g = s.palette.number
        case "comment": g = s.palette.comment
        case "tag": g = s.palette.tag
        case "attr": g = s.palette.attr
        case "punct": g = s.palette.punct
        case "gutter": g = s.palette.gutter
        case "active": g = s.palette.active
        case "caret": g = s.palette.caret
        case "select": g = s.palette.sel
        case "selectSolid": g = s.palette.selSolid
        case "bg": g = s.palette.bg
        default: g = s.palette.text
        }
        if g != nil { c := g(); if c != "" { return c } }
    }
    return defaultSyntaxColor(name)
}

// ---- 字符度量（点击换算与光标渲染共用同一套）----

func runeCells(r rune) int {
    if r >= 0x1100 && (r <= 0x115f || r == 0x2329 || r == 0x232a ||
        (r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
        (r >= 0xac00 && r <= 0xd7a3) ||
        (r >= 0xf900 && r <= 0xfaff) ||
        (r >= 0xfe10 && r <= 0xfe19) ||
        (r >= 0xfe30 && r <= 0xfe6f) ||
        (r >= 0xff00 && r <= 0xff60) ||
        (r >= 0xffe0 && r <= 0xffe6)) {
        return 2
    }
    return 1
}

func (s *EditorState) cellPx(r rune) float64 {
    if runeCells(r) == 2 { return s.wideWidth() }
    return s.charWidth()
}

// PrefixWidth 返回一行前 col 个 rune 的像素宽度（caret 水平偏移来源）。
func (s *EditorState) PrefixWidth(str string, col int) float64 {
    rs := []rune(str)
    acc := 0.0
    if col > len(rs) { col = len(rs) }
    for i := 0; i < col; i = i + 1 { acc = acc + s.cellPx(rs[i]) }
    return acc
}

// PickColumn 是 PrefixWidth 的逆运算：给定行内 x 像素，返回最近 rune 边界的列。
func (s *EditorState) PickColumn(idx int, x float64) int {
    rs := []rune(s.lineText(idx))
    acc := 0.0
    for i := 0; i < len(rs); i = i + 1 {
        w := s.cellPx(rs[i])
        if x < acc + w / 2 { return i }
        acc = acc + w
    }
    return len(rs)
}

// ---- 纯文本编辑逻辑（纯函数，两端共用）----

func splitLines(v string) []string { return strings.Split(v, "\n") }
func joinLines(ls []string) string { return strings.Join(ls, "\n") }
func runeLen(v string) int { return len([]rune(v)) }
func indices(n int) []int {
    out := []int{}
    for i := 0; i < n; i = i + 1 { out = append(out, i) }
    return out
}

func insertText(v string, ln int, col int, ins string) (string, int) {
    lines := splitLines(v)
    if ln < 0 { ln = 0 }
    if ln >= len(lines) { return v, col }
    rs := []rune(lines[ln])
    if col < 0 { col = 0 }
    if col > len(rs) { col = len(rs) }
    lines[ln] = string(rs[:col]) + ins + string(rs[col:])
    return joinLines(lines), col + runeLen(ins)
}

func backspaceAt(v string, ln int, col int) (string, int, int) {
    lines := splitLines(v)
    if ln < 0 || ln >= len(lines) { return v, ln, col }
    rs := []rune(lines[ln])
    if col > len(rs) { col = len(rs) }
    if col > 0 {
        lines[ln] = string(rs[:col-1]) + string(rs[col:])
        return joinLines(lines), ln, col - 1
    }
    if ln > 0 {
        prev := []rune(lines[ln-1])
        merged := string(prev) + lines[ln]
        newLines := []string{}
        for i, l := range lines {
            if i == ln-1 { newLines = append(newLines, merged) } else if i != ln { newLines = append(newLines, l) }
        }
        return joinLines(newLines), ln - 1, len(prev)
    }
    return v, ln, col
}

func newlineAt(v string, ln int, col int) (string, int, int) {
    lines := splitLines(v)
    if ln < 0 || ln >= len(lines) { return v, ln, col }
    rs := []rune(lines[ln])
    if col > len(rs) { col = len(rs) }
    left := string(rs[:col]); right := string(rs[col:])
    newLines := []string{}
    for i, l := range lines {
        if i == ln { newLines = append(newLines, left, right) } else { newLines = append(newLines, l) }
    }
    return joinLines(newLines), ln + 1, 0
}

func removeRange(v string, fl int, fc int, tl int, tc int) string {
    lines := splitLines(v)
    if fl < 0 || fl >= len(lines) || tl >= len(lines) || fl > tl { return v }
    fr := []rune(lines[fl]); tr := []rune(lines[tl])
    if fc > len(fr) { fc = len(fr) }
    if tc > len(tr) { tc = len(tr) }
    if fl == tl {
        lines[fl] = string(fr[:fc]) + string(fr[tc:])
        return joinLines(lines)
    }
    merged := string(fr[:fc]) + string(tr[tc:])
    out := []string{}
    for i, l := range lines {
        if i < fl { out = append(out, l) } else if i == fl { out = append(out, merged) } else if i > tl { out = append(out, l) }
    }
    return joinLines(out)
}

// ---- Rope 编辑核心（O(log N)，与上方 string 版逐字节等价）----
//
// 上面的 string 版保留为公共 API / 测试 oracle（见 editor_export.go）。
// editor 实例命令改走下面这套 rope 实现：行列经 LineColToOffset 单次树下降
// 换算成 rune 偏移，插入/删除走持久结构的 split+join，避免每个按键 O(N)
// 的 splitLines + joinLines 全量重建。持久根同时用于 O(1) 撤销快照。

// ropeInsertText 在 (ln,col) 插入 ins，返回新根与新列。
func ropeInsertText(r *Rope.Rope, ln int, col int, ins string) (*Rope.Rope, int) {
    if r == nil { r = Rope.Of("") }
    if ln < 0 { ln = 0 }
    if ln >= r.LineCount() { return r, col }
    lineRunes := runeLen(r.Line(ln))
    if col < 0 { col = 0 }
    if col > lineRunes { col = lineRunes }
    off := r.LineColToOffset(ln, col)
    return r.Insert(off, ins), col + runeLen(ins)
}

// ropeDeleteRange 删除 [ (fl,fc), (tl,tc) ) 区间，返回新根。
func ropeDeleteRange(r *Rope.Rope, fl int, fc int, tl int, tc int) *Rope.Rope {
    if r == nil { return r }
    lc := r.LineCount()
    if fl < 0 || fl >= lc || tl >= lc || fl > tl { return r }
    start := r.LineColToOffset(fl, fc)
    end := r.LineColToOffset(tl, tc)
    if end < start { end = start }
    return r.Delete(start, end-start)
}

// ropeBackspaceAt 在 (ln,col) 退格：行内删一个 rune；行首则并入上一行。
// 返回新根、新行、新列。
func ropeBackspaceAt(r *Rope.Rope, ln int, col int) (*Rope.Rope, int, int) {
    if r == nil { r = Rope.Of("") }
    lc := r.LineCount()
    if ln < 0 || ln >= lc { return r, ln, col }
    lineRunes := runeLen(r.Line(ln))
    if col > lineRunes { col = lineRunes }
    if col > 0 {
        off := r.LineColToOffset(ln, col-1)
        return r.Delete(off, 1), ln, col - 1
    }
    if ln > 0 {
        // 行首退格 = 删除上一行末尾的 '\n'（其 rune 偏移 = 本行起点 - 1）。
        prevRunes := runeLen(r.Line(ln-1))
        off := r.LineStart(ln) - 1
        return r.Delete(off, 1), ln - 1, prevRunes
    }
    return r, ln, col
}

// ropeNewlineAt 在 (ln,col) 断行，返回新根、新行(=ln+1)、新列(0)。
func ropeNewlineAt(r *Rope.Rope, ln int, col int) (*Rope.Rope, int, int) {
    if r == nil { r = Rope.Of("") }
    lc := r.LineCount()
    if ln < 0 || ln >= lc { return r, ln, col }
    lineRunes := runeLen(r.Line(ln))
    if col > lineRunes { col = lineRunes }
    off := r.LineColToOffset(ln, col)
    return r.Insert(off, "\n"), ln + 1, 0
}

// ropeDeleteForward 删除光标后一个 rune：行尾则并入下一行。返回新根与新列。
func ropeDeleteForward(r *Rope.Rope, ln int, col int) (*Rope.Rope, int) {
    if r == nil { r = Rope.Of("") }
    lc := r.LineCount()
    if ln < 0 || ln >= lc { return r, col }
    lineRunes := runeLen(r.Line(ln))
    if col > lineRunes { col = lineRunes }
    if col < lineRunes {
        off := r.LineColToOffset(ln, col)
        return r.Delete(off, 1), col
    }
    // 行尾 Delete = 删除本行末尾 '\n'（= 下一行起点 - 1）。
    if ln < lc-1 {
        off := r.LineStart(ln+1) - 1
        return r.Delete(off, 1), col
    }
    return r, col
}

// ---- 选区引擎（Selection engine）----
// 选区 = anchor 到 head（光标）的闭左开右区间；锚点与光标重合即无选区。

func (s *EditorState) HasSelection() bool {
    return s.selAnchorLine() != s.cursorLine() || s.selAnchorCol() != s.cursorCol()
}

func (s *EditorState) selFrom() (int, int) {
    if s.selAnchorLine() < s.cursorLine() || (s.selAnchorLine() == s.cursorLine() && s.selAnchorCol() <= s.cursorCol()) {
        return s.selAnchorLine(), s.selAnchorCol()
    }
    return s.cursorLine(), s.cursorCol()
}

func (s *EditorState) selTo() (int, int) {
    if s.selAnchorLine() < s.cursorLine() || (s.selAnchorLine() == s.cursorLine() && s.selAnchorCol() <= s.cursorCol()) {
        return s.cursorLine(), s.cursorCol()
    }
    return s.selAnchorLine(), s.selAnchorCol()
}

func (s *EditorState) deleteSelectionInner() {
    fl, fc := s.selFrom()
    tl, tc := s.selTo()
    nr := ropeDeleteRange(s.doc, fl, fc, tl, tc)
    Signal.Batch(func() {
        s.applyRope(nr)
        s.cursorLine(fl); s.cursorCol(fc); s.selAnchorLine(fl); s.selAnchorCol(fc)
    })
}

// ---- 撤销 / 重做 ----

// snapshot 捕获当前编辑前状态：持久 Rope 根 O(1) 共享，Text 仍写供兼容读取。
func (s *EditorState) snapshot() editSnapshot {
    return editSnapshot{Doc: s.doc, Text: s.sourceCode(), Ln: s.cursorLine(), Col: s.cursorCol()}
}

func (s *EditorState) pushUndo() {
    s.undoStack = append(s.undoStack, s.snapshot())
    s.redoStack = []editSnapshot{}
}

// restore 从快照恢复：优先用持久 Doc 根（免 Rope.Of 重建），兼容无 Doc 的老快照。
func (s *EditorState) restore(st editSnapshot) {
    if st.Doc != nil {
        s.setSourceFromRope(st.Doc, st.Doc.String())
    } else {
        s.setSource(st.Text)
    }
    s.cursorLine(st.Ln); s.cursorCol(st.Col); s.selAnchorLine(st.Ln); s.selAnchorCol(st.Col)
    s.focused(true)
}

// Undo 撤销上一步编辑；无历史时什么也不做。
func (s *EditorState) Undo() {
    if len(s.undoStack) == 0 { return }
    n := len(s.undoStack); st := s.undoStack[n-1]; s.undoStack = s.undoStack[:n-1]
    s.redoStack = append(s.redoStack, s.snapshot())
    Signal.Batch(func() { s.restore(st) })
}

// Redo 重做一步；无重做历史时什么也不做。
func (s *EditorState) Redo() {
    if len(s.redoStack) == 0 { return }
    n := len(s.redoStack); st := s.redoStack[n-1]; s.redoStack = s.redoStack[:n-1]
    s.undoStack = append(s.undoStack, s.snapshot())
    Signal.Batch(func() { s.restore(st) })
}

// ---- 包级编辑命令（装配层收到键盘事件后调用）----

func (s *EditorState) Insert(v string) {
    s.pushUndo()
    r := s.doc; ln := s.cursorLine(); col := s.cursorCol()
    if s.HasSelection() {
        fl, fc := s.selFrom(); tl, tc := s.selTo()
        r = ropeDeleteRange(r, fl, fc, tl, tc); ln = fl; col = fc
    }
    nr, c := ropeInsertText(r, ln, col, v)
    Signal.Batch(func() {
        s.applyRope(nr)
        s.cursorLine(ln); s.cursorCol(c); s.selAnchorLine(ln); s.selAnchorCol(c); s.focused(true)
    })
}

func (s *EditorState) Backspace() {
    s.pushUndo()
    if s.HasSelection() { s.deleteSelectionInner(); return }
    nr, ln, c := ropeBackspaceAt(s.doc, s.cursorLine(), s.cursorCol())
    Signal.Batch(func() {
        s.applyRope(nr)
        s.cursorLine(ln); s.cursorCol(c); s.selAnchorLine(ln); s.selAnchorCol(c)
    })
}

func (s *EditorState) Newline() {
    s.pushUndo()
    r := s.doc; ln := s.cursorLine(); col := s.cursorCol()
    if s.HasSelection() {
        fl, fc := s.selFrom(); tl, tc := s.selTo()
        r = ropeDeleteRange(r, fl, fc, tl, tc); ln = fl; col = fc
    }
    nr, nl, c := ropeNewlineAt(r, ln, col)
    Signal.Batch(func() {
        s.applyRope(nr)
        s.cursorLine(nl); s.cursorCol(c); s.selAnchorLine(nl); s.selAnchorCol(c)
    })
}

func (s *EditorState) moveCursorRel(dln int, dcol int) {
    lc := s.doc.LineCount()
    ln := s.cursorLine() + dln
    if ln < 0 { ln = 0 }
    if ln >= lc { ln = lc - 1 }
    lineRunes := runeLen(s.doc.Line(ln)) // O(log N + 行长度)，非全文 split
    c := s.cursorCol() + dcol
    if c < 0 { c = 0 }
    if c > lineRunes { c = lineRunes }
    s.cursorLine(ln); s.cursorCol(c)
}

// Move 方向移动：有选区时先折叠再移动（左/上收到选区起点，右/下放到终点）。
func (s *EditorState) Move(dln int, dcol int) {
    Signal.Batch(func() {
        if s.HasSelection() {
            fl, fc := s.selFrom(); tl, tc := s.selTo()
            if dln < 0 || (dln == 0 && dcol < 0) { s.cursorLine(fl); s.cursorCol(fc) } else { s.cursorLine(tl); s.cursorCol(tc) }
            s.selAnchorLine(s.cursorLine()); s.selAnchorCol(s.cursorCol())
        }
        s.moveCursorRel(dln, dcol)
        s.selAnchorLine(s.cursorLine()); s.selAnchorCol(s.cursorCol())
    })
}

// MoveEx 带 Shift 的方向移动：锚点不动扩展选区。
func (s *EditorState) MoveEx(dln int, dcol int, shift bool) {
    if !shift { s.Move(dln, dcol); return }
    Signal.Batch(func() {
        if !s.HasSelection() { s.selAnchorLine(s.cursorLine()); s.selAnchorCol(s.cursorCol()) }
        s.moveCursorRel(dln, dcol)
    })
}

// SetText 整体替换文本（切换示例用）。
func (s *EditorState) SetText(v string) {
    s.pushUndo()
    Signal.Batch(func() {
        s.setSource(v)
        l, c := s.clampLineCol(s.cursorLine(), s.cursorCol())
        s.cursorLine(l); s.cursorCol(c); s.selAnchorLine(l); s.selAnchorCol(c)
    })
}

// Home 一次性把光标归位。
func (s *EditorState) Home() {
    Signal.Batch(func() {
        s.cursorLine(0); s.cursorCol(0); s.selAnchorLine(0); s.selAnchorCol(0)
    })
}

func (s *EditorState) clampLineCol(ln int, col int) (int, int) {
    lc := s.doc.LineCount()
    if ln < 0 { ln = 0 }
    if ln >= lc { ln = lc - 1 }
    lineRunes := runeLen(s.doc.Line(ln)) // O(log N + 行长度)
    if col < 0 { col = 0 }
    if col > lineRunes { col = lineRunes }
    return ln, col
}

// SetCursor 把光标定位到指定行/列，并折叠选区。
func (s *EditorState) SetCursor(ln int, col int) {
    Signal.Batch(func() {
        l, c := s.clampLineCol(ln, col)
        s.cursorLine(l); s.cursorCol(c); s.selAnchorLine(l); s.selAnchorCol(c); s.focused(true)
    })
}

func isWordRune(r rune) bool {
    if r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') { return true }
    return runeCells(r) == 2
}

func wordRunAt(line string, col int) (int, int) {
    rs := []rune(line)
    if len(rs) == 0 { return 0, 0 }
    at := col
    if at > len(rs) { at = len(rs) }
    pick := at
    if at > 0 && (at == len(rs) || !isWordRune(rs[at])) { pick = at - 1 }
    if at < len(rs) && isWordRune(rs[at]) { pick = at }
    word := isWordRune(rs[pick])
    start := pick
    for start > 0 && isWordRune(rs[start-1]) == word { start = start - 1 }
    end := pick + 1
    for end < len(rs) && isWordRune(rs[end]) == word { end = end + 1 }
    return start, end
}

func firstNonSpace(rs []rune) int {
    for i, r := range rs { if r != ' ' && r != '\t' { return i } }
    return len(rs)
}

// ClearSelection 折叠选区到光标处。
func (s *EditorState) ClearSelection() {
    Signal.Batch(func() { s.selAnchorLine(s.cursorLine()); s.selAnchorCol(s.cursorCol()) })
}

// SetSelect 拖拽/程序式定位：锚点与光标同时落到 (ln, col)。
func (s *EditorState) SetSelect(ln int, col int) {
    Signal.Batch(func() {
        l, c := s.clampLineCol(ln, col)
        s.cursorLine(l); s.cursorCol(c); s.selAnchorLine(l); s.selAnchorCol(c); s.focused(true)
    })
}

// ExtendTo 拖拽移动 / Shift+点击：只移动光标，锚点保持原位。
func (s *EditorState) ExtendTo(ln int, col int) {
    Signal.Batch(func() {
        l, c := s.clampLineCol(ln, col)
        s.cursorLine(l); s.cursorCol(c); s.focused(true)
    })
}

// MoveWord Ctrl/Alt+方向：按词移动光标；shift 时保留锚点扩展选区。
func (s *EditorState) MoveWord(dir int, shift bool) {
    Signal.Batch(func() {
        if !shift {
            if s.HasSelection() {
                fl, fc := s.selFrom(); tl, tc := s.selTo()
                if dir < 0 { s.cursorLine(fl); s.cursorCol(fc) } else { s.cursorLine(tl); s.cursorCol(tc) }
                s.selAnchorLine(s.cursorLine()); s.selAnchorCol(s.cursorCol())
            }
        } else if !s.HasSelection() {
            s.selAnchorLine(s.cursorLine()); s.selAnchorCol(s.cursorCol())
        }
        lc := s.doc.LineCount()
        ln := s.cursorLine()
        if ln >= lc { ln = lc - 1 }
        rs := []rune(s.doc.Line(ln))
        c := s.cursorCol()
        if c > len(rs) { c = len(rs) }
        if dir > 0 {
            for c < len(rs) && !isWordRune(rs[c]) { c = c + 1 }
            for c < len(rs) && isWordRune(rs[c]) { c = c + 1 }
        } else {
            for c > 0 && !isWordRune(rs[c-1]) { c = c - 1 }
            for c > 0 && isWordRune(rs[c-1]) { c = c - 1 }
        }
        s.cursorCol(c)
        if !shift { s.selAnchorLine(s.cursorLine()); s.selAnchorCol(s.cursorCol()) }
    })
}

// KeyHome Home 键：光标到首非空白、再到纯行首（两段式）；shift 扩展选区。
func (s *EditorState) KeyHome(shift bool) {
    Signal.Batch(func() {
        if shift && !s.HasSelection() { s.selAnchorLine(s.cursorLine()); s.selAnchorCol(s.cursorCol()) }
        rs := []rune(s.lineText(s.cursorLine()))
        target := 0
        fns := firstNonSpace(rs)
        if s.cursorCol() > fns { target = fns }
        s.cursorCol(target)
        if !shift { s.selAnchorLine(s.cursorLine()); s.selAnchorCol(s.cursorCol()) }
    })
}

// KeyEnd End 键：光标到行尾；shift 扩展选区。
func (s *EditorState) KeyEnd(shift bool) {
    Signal.Batch(func() {
        if shift && !s.HasSelection() { s.selAnchorLine(s.cursorLine()); s.selAnchorCol(s.cursorCol()) }
        s.cursorCol(len([]rune(s.lineText(s.cursorLine()))))
        if !shift { s.selAnchorLine(s.cursorLine()); s.selAnchorCol(s.cursorCol()) }
    })
}

// DeleteForward Delete 键：有选区删选区，否则删光标后字符（行尾则并入下行）。
func (s *EditorState) DeleteForward() {
    s.pushUndo()
    if s.HasSelection() { s.deleteSelectionInner(); return }
    ln := s.cursorLine(); col := s.cursorCol()
    nr, c := ropeDeleteForward(s.doc, ln, col)
    Signal.Batch(func() {
        s.applyRope(nr)
        s.cursorLine(ln); s.cursorCol(c); s.selAnchorLine(ln); s.selAnchorCol(c)
    })
}

// SelectAll 全选。
func (s *EditorState) SelectAll() {
    Signal.Batch(func() {
        lc := s.doc.LineCount()
        last := lc - 1
        s.selAnchorLine(0); s.selAnchorCol(0)
        s.cursorLine(last); s.cursorCol(runeLen(s.doc.Line(last)))
        s.focused(true)
    })
}

// SelectWord 双击选词：锚点落词首、光标落词尾。
func (s *EditorState) SelectWord(ln int, col int) {
    Signal.Batch(func() {
        l, c := s.clampLineCol(ln, col)
        s.cursorLine(l)
        start, end := wordRunAt(s.lineText(l), c)
        if start == end { s.selAnchorLine(l); s.selAnchorCol(c); s.cursorCol(c) } else { s.selAnchorLine(l); s.selAnchorCol(start); s.cursorCol(end) }
        s.focused(true)
    })
}

// SelectLine 三击选行：选中本行含行尾换行符（末行只选行内容）。
func (s *EditorState) SelectLine(ln int) {
    Signal.Batch(func() {
        lc := s.doc.LineCount()
        l := ln
        if l < 0 { l = 0 }
        if l >= lc { l = lc - 1 }
        s.selAnchorLine(l); s.selAnchorCol(0)
        if l < lc-1 { s.cursorLine(l + 1); s.cursorCol(0) } else { s.cursorLine(l); s.cursorCol(runeLen(s.doc.Line(l))) }
        s.focused(true)
    })
}

// SelectedText 返回选中文本（Ctrl+C / Ctrl+X 用）；无选区返回空串。
// 直接用 Rope 的 rune 偏移切片，避免按下标 splitLines 全文（O(log N + 选中长度)）。
func (s *EditorState) SelectedText() string {
    if !s.HasSelection() { return "" }
    fl, fc := s.selFrom(); tl, tc := s.selTo()
    lc := s.doc.LineCount()
    if fl >= lc || tl >= lc { return "" }
    start := s.doc.LineColToOffset(fl, fc)
    end := s.doc.LineColToOffset(tl, tc)
    if end < start { end = start }
    return s.doc.Slice(start, end)
}

// DeleteSelection 删除当前选区（Ctrl+X 的"剪"半边）。
func (s *EditorState) DeleteSelection() {
    if !s.HasSelection() { return }
    s.pushUndo(); s.deleteSelectionInner()
}

// Paste 粘贴（可含 \n 的多行文本）：有选区先删，再整体插入。
// Rope 的 Insert 原生接受含 '\n' 的文本，新光标 = 插入起点偏移 + 插入 rune 数，
// 经 OffsetToLineCol 换算成 (行,列)——与原 string 版逐字节等价，且 O(log N)。
func (s *EditorState) Paste(v string) {
    if v == "" { return }
    s.pushUndo()
    r := s.doc; ln := s.cursorLine(); col := s.cursorCol()
    if s.HasSelection() {
        fl, fc := s.selFrom(); tl, tc := s.selTo()
        r = ropeDeleteRange(r, fl, fc, tl, tc); ln = fl; col = fc
    }
    off := r.LineColToOffset(ln, col)
    nr := r.Insert(off, v)
    nl, nc := nr.OffsetToLineCol(off + runeLen(v))
    Signal.Batch(func() {
        s.applyRope(nr)
        s.cursorLine(nl); s.cursorCol(nc); s.selAnchorLine(nl); s.selAnchorCol(nc); s.focused(true)
    })
}

// Focus / Blur 焦点状态。
func (s *EditorState) Focus()  { Signal.Batch(func() { s.focused(true) }) }
func (s *EditorState) Blur()   { Signal.Batch(func() { s.focused(false) }) }
func (s *EditorState) Settle() { Signal.Batch(func() { s.caretSettled(true) }) }
func (s *EditorState) Idle()   { Signal.Batch(func() { s.caretSettled(false) }) }

// SetCharWidth / SetWideWidth 由装配层在启动时用 canvas 实测注入。
func (s *EditorState) SetCharWidth(w float64)  { Signal.Batch(func() { s.charWidth(w) }) }
func (s *EditorState) SetWideWidth(w float64) { Signal.Batch(func() { s.wideWidth(w) }) }

// Line / Col / Text 读取当前光标与源码（非响应式，供 handler 一次性读取）。
func (s *EditorState) Line() int    { return s.cursorLine() }
func (s *EditorState) Col() int     { return s.cursorCol() }
func (s *EditorState) Text() string { return s.sourceCode() }

// ---- caret / 选区几何（与行版本 getter 共用同一套坐标公式）----

func (s *EditorState) caretLeft() float64 { return 56 + s.PrefixWidth(s.lineText(s.cursorLine()), s.cursorCol()) }
func (s *EditorState) caretTop() float64  { return 6 + float64(s.cursorLine()*editorRowHeight) }

// caretTopVisible 把光标的文档坐标减去当前滚动偏移，得到视口内的屏幕坐标。
// 虚拟滚动下容器把子树上移了 scrollTop，绝对定位的 caret 必须同步补偿。
func (s *EditorState) caretTopVisible() float64 { return s.caretTop() - s.scrollTop() }

// SetScrollTop 写回受控滚动偏移（VirtualList on-scroll / 滚轮事件调用）。夹紧 >=0。
func (s *EditorState) SetScrollTop(v float64) {
    if v < 0 { v = 0 }
    s.scrollTopVal = v // 先更新镜像，effect 读到的是最新值
    Signal.Batch(func() { s.scrollTop(v) })
}

// ScrollBy 相对滚动（滚轮事件用：delta 为行数像素，向下为正）。
func (s *EditorState) ScrollBy(delta float64) {
    s.SetScrollTop(s.scrollTopVal + delta)
}

// ScrollLines 按行数滚动（滚轮一行一行的语义）。
func (s *EditorState) ScrollLines(lines float64) {
    s.ScrollBy(lines * editorRowHeight)
}

// ensureCursorVisible 若光标行落在视口外，滚动到让它可见。读 scrollTopVal
// （非响应式镜像）而非 scrollTop()——这样跟随 effect 只订阅 cursorLine，
// 用户主动滚动（on-scroll 写 scrollTop）不会被拉回，避免回环。
func (s *EditorState) ensureCursorVisible() {
    top := float64(s.cursorLine() * editorRowHeight)
    bottom := top + editorRowHeight
    st := s.scrollTopVal
    view := float64(editorViewportHeight)
    if top < st {
        s.SetScrollTop(top)
    } else if bottom > st+view {
        s.SetScrollTop(bottom - view)
    }
}

// installCursorFollow 在 Editor 建树期登记一个 effect：仅订阅 cursorLine/lineCount，
// 光标移出视口时自动滚动。它不读 scrollTop()（只读镜像 scrollTopVal），
// 故 on-scroll 的写回不会触发它，无回环。
func (s *EditorState) installCursorFollow() {
    Signal.Effect(func() {
        _ = s.cursorLine()
        _ = s.lineCount()
        s.ensureCursorVisible()
    })
}

func (s *EditorState) selRectStartCol(idx int) int {
    fl, fc := s.selFrom(); _ , tc := s.selTo(); _ = tc
    if !s.HasSelection() || idx < fl { return 0 }
    if idx == fl { return fc }
    return 0
}
func (s *EditorState) selRectEndCol(idx int) int {
    fl, _ := s.selFrom(); tl, tc := s.selTo(); _ = fl
    if !s.HasSelection() || idx > tl { return 0 }
    if idx == tl { return tc }
    return runeLen(s.lineText(idx))
}
// lineSelRange 返回该行文本内被选中的 rune 区间 "start:end"，供三端统一的
// selection 协议消费（Web 富文本 / Skia paintRichText / Rust 继承 span）。
// 本行无选区时返回空串。区间口径与 selRectStartCol/EndCol 一致，均为整行 rune 索引。
func (s *EditorState) lineSelRange(idx int) string {
    if !s.HasSelection() || idx < s.selFromLine() || idx > s.selToLine() { return "" }
    a := s.selRectStartCol(idx)
    b := s.selRectEndCol(idx)
    if b <= a { return "" }
    return strconv.Itoa(a) + ":" + strconv.Itoa(b)
}
func (s *EditorState) selRectLeft(idx int) float64 {
    return 56 + s.PrefixWidth(s.lineText(idx), s.selRectStartCol(idx))
}
func (s *EditorState) selRectWidth(idx int) float64 {
    t := s.lineText(idx)
    w := s.PrefixWidth(t, s.selRectEndCol(idx)) - s.PrefixWidth(t, s.selRectStartCol(idx))
    if w < 0 { w = 0 }
    return w
}
func (s *EditorState) selFromLine() int { fl, _ := s.selFrom(); return fl }
func (s *EditorState) selToLine() int   { tl, _ := s.selTo(); return tl }
func (s *EditorState) selRectBg(idx int) string {
    if !s.HasSelection() || idx < s.selFromLine() || idx > s.selToLine() { return "" }
    if s.selRectWidth(idx) <= 0 { return "" }
    return s.color("select")
}
func (s *EditorState) selRectTop(idx int) float64 { return 6 + float64(idx*22) }

// caretOpacity / caretAnimation 把光标三态表达为三端统一的声明式 prop，
// 取代原先 editor.css 里的 class + @keyframes caret-blink：
//   - off（未聚焦）：opacity=0、无动画 —— 隐藏且点击穿透；
//   - steady（交互后 1s 内）：opacity=1、无动画 —— 常亮；
//   - blink（静置）：animation="blink 1.06s infinite" —— 声明式方波闪烁。
// Web 后端翻译成原生 CSS animation（浏览器合成器驱动），Skia/Rust 按帧时钟采样
// opacity，语义严格一致；opacity=1 作为动画的基准值，动画期间由采样覆盖。
func (s *EditorState) caretOpacity() string {
    if !s.focused() { return "0" }
    return "1"
}
func (s *EditorState) caretAnimation() string {
    if !s.focused() || s.caretSettled() { return "" }
    return "blink 1.06s infinite"
}

// ---- 语法高亮（类 Ace 分词器，颜色用语义 token 名）----

func isAlpha(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_' || b >= 0x80 }
func isAlnum(b byte) bool { return isAlpha(b) || (b >= '0' && b <= '9') }

func isKeyword(w string) bool {
    switch w {
    case "func", "return", "if", "else", "for", "range", "var", "sig", "let",
         "pub", "type", "struct", "enum", "import", "package", "go", "defer",
         "nil", "true", "false", "any", "string", "int", "bool", "interface":
        return true
    }
    return false
}

// matchBrace 返回从 start 处的 '{' 开始、与之匹配的 '}' 下标；未找到返回 len。
func matchBrace(line string, start int) int {
    depth := 0; i := start
    for i < len(line) {
        if line[i] == '"' {
            i = i + 1
            for i < len(line) && line[i] != '"' {
                if line[i] == '\\' && i+1 < len(line) { i = i + 1 }
                i = i + 1
            }
        } else if line[i] == '{' {
            depth = depth + 1
        } else if line[i] == '}' {
            depth = depth - 1
            if depth == 0 { return i }
        }
        i = i + 1
    }
    return len(line)
}

// highlight 把一行源码切成带语义色的 token，拼回去严格等于原文。
// 颜色存语义名（keyword/str/number/comment/tag/attr/punct/text），渲染按主题解析。
func highlight(line string) []token {
    out := make([]token, 0, len(line)/2 + 1)
    i := 0
    inTag := false
    tagNamePending := false
    for i < len(line) {
        if inTag {
            if line[i] == '>' {
                out = append(out, token{Text: ">", Color: "punct"}); i = i + 1; inTag = false; continue
            }
            if line[i] == '/' && i+1 < len(line) && line[i+1] == '>' {
                out = append(out, token{Text: "/>", Color: "punct"}); i = i + 2; inTag = false; continue
            }
            if line[i] == '"' {
                j := i + 1
                for j < len(line) && line[j] != '"' {
                    if line[j] == '\\' && j+1 < len(line) { j = j + 1 }
                    j = j + 1
                }
                if j < len(line) { j = j + 1 }
                out = append(out, token{Text: line[i:j], Color: "str"}); i = j; continue
            }
            if line[i] == '{' {
                close := matchBrace(line, i)
                out = append(out, token{Text: "{", Color: "punct"})
                if close < len(line) {
                    for _, t := range highlight(line[i+1 : close]) { out = append(out, t) }
                    out = append(out, token{Text: "}", Color: "punct"})
                    i = close + 1
                } else { i = i + 1 }
                continue
            }
            if isAlpha(line[i]) {
                j := i + 1
                for j < len(line) && isAlnum(line[j]) { j = j + 1 }
                for j+1 < len(line) && line[j] == '-' && isAlnum(line[j+1]) {
                    j = j + 1
                    for j < len(line) && isAlnum(line[j]) { j = j + 1 }
                }
                c := "attr"
                if tagNamePending { c = "tag"; tagNamePending = false }
                out = append(out, token{Text: line[i:j], Color: c}); i = j; continue
            }
            c := "punct"
            if line[i] == ' ' || line[i] == '\t' { c = "text" }
            out = append(out, token{Text: line[i : i+1], Color: c}); i = i + 1; continue
        }

        if line[i] == '<' && i+1 < len(line) && (isAlpha(line[i+1]) || line[i+1] == '/') {
            out = append(out, token{Text: "<", Color: "punct"})
            i = i + 1; inTag = true; tagNamePending = true
            if line[i] == '/' { out = append(out, token{Text: "/", Color: "punct"}); i = i + 1 }
            continue
        }
        if i+1 < len(line) && line[i] == '/' && line[i+1] == '/' {
            out = append(out, token{Text: line[i:], Color: "comment"}); break
        }
        if line[i] == '"' {
            j := i + 1
            for j < len(line) && line[j] != '"' {
                if line[j] == '\\' && j+1 < len(line) { j = j + 1 }
                j = j + 1
            }
            if j < len(line) { j = j + 1 }
            out = append(out, token{Text: line[i:j], Color: "str"}); i = j; continue
        }
        if isAlpha(line[i]) {
            j := i + 1
            for j < len(line) && isAlnum(line[j]) { j = j + 1 }
            w := line[i:j]
            c := "text"
            if isKeyword(w) { c = "keyword" }
            out = append(out, token{Text: w, Color: c}); i = j; continue
        }
        if line[i] >= '0' && line[i] <= '9' {
            j := i + 1
            for j < len(line) && isAlnum(line[j]) { j = j + 1 }
            out = append(out, token{Text: line[i:j], Color: "number"}); i = j; continue
        }
        c := "punct"
        if line[i] == ' ' || line[i] == '\t' { c = "text" }
        out = append(out, token{Text: line[i : i+1], Color: c}); i = i + 1
    }
    return out
}

// ---- 行缓存（token 语义 + 派生纯文本；渐变按主题现场构建）----

func runeWidth(v string) int {
    n := 0
    for _, r := range v { if r >= 0x2e80 { n = n + 2 } else { n = n + 1 } }
    return n
}

func joinLineText(toks []token) string {
    var b strings.Builder
    for _, t := range toks { b.WriteString(t.Text) }
    return b.String()
}

// buildGradient 把一行的语义 token 编码成 CSS linear-gradient。
// resolve 把语义名解析成当前主题色（渲染 getter 现场调用，主题切换自动生效）。
func (s *EditorState) buildGradient(toks []token) string {
    var b strings.Builder
    b.WriteString("linear-gradient(90deg")
    pos := 0
    for _, t := range toks {
        w := runeWidth(t.Text)
        if w == 0 { continue }
        b.WriteString(", ")
        b.WriteString(s.color(t.Color))
        b.WriteString(" ")
        b.WriteString(strconv.Itoa(pos)); b.WriteString("ch ")
        b.WriteString(strconv.Itoa(pos+w)); b.WriteString("ch")
        pos = pos + w
    }
    b.WriteString(")")
    return b.String()
}

// rebuildTokenCache 已改为惰性：不再逐行 highlight。这里只保留行文本快照，
// 把变化行的 token/text 缓存置 nil 失效，并返回变化行索引供行版本 bump；
// 真正的 tokenize 推迟到该行被 lineText/lineCodeGradient/lineRichText 读取时。
func (s *EditorState) rebuildTokenCache(lines []string) []int {
    old := s.lineTextCache
    s.ensureCacheLen(len(lines))
    changed := []int{}
    for i := 0; i < len(lines); i = i + 1 {
        if i >= len(old) || old[i] != lines[i] {
            // 行内容变化或新增 → 文本快照更新 + token 缓存失效（nil = 待惰性解析）。
            s.lineTextCache[i] = lines[i]
            s.tokenCache[i] = nil
            s.textCache[i] = ""
            changed = append(changed, i)
        }
    }
    return changed
}

// ensureLineParsed 惰性解析单行：缓存命中直接返回，未解析则现场 highlight。
// 行内容以 lineTextCache 为准（setSource 已同步）。
func (s *EditorState) ensureLineParsed(idx int) {
    if idx < 0 || idx >= len(s.tokenCache) { return }
    if s.tokenCache[idx] != nil { return }
    line := ""
    if idx < len(s.lineTextCache) { line = s.lineTextCache[idx] }
    toks := highlight(line)
    s.tokenCache[idx] = toks
    s.textCache[idx] = joinLineText(toks)
}

func (s *EditorState) rowVersion(idx int) int {
    if idx < 0 || idx >= s.rowVers.Len() { return 0 }
    cell := s.rowVers.At(idx)
    if cell == nil { return 0 }
    return cell()
}
func (s *EditorState) bumpRow(idx int) {
    if idx < 0 || idx >= s.rowVers.Len() { return }
    cell := s.rowVers.At(idx)
    if cell == nil { return }
    cell(cell() + 1)
}

// setSource 是写 sourceCode 的唯一入口：同步 Rope 根、重算 token 缓存、只 bump 变化行。
func (s *EditorState) setSource(text string) {
    s.doc = Rope.Of(text)
    s.setSourceFromRope(s.doc, text)
}

// setSourceFromRope 以已构造好的 Rope 根 + 其字符串写状态，避免编辑命令里
// 先从 Rope.String() 拿到 text 后再次 Rope.Of(text) 的重复构造。
func (s *EditorState) setSourceFromRope(r *Rope.Rope, text string) {
    s.doc = r
    newLines := splitLines(text) // 仅为定位变化行；O(N) 但无 tokenize，行级 highlight 已惰性化
    changed := s.rebuildTokenCache(newLines)
    Signal.Batch(func() {
        // 先扩容 Vec（>64 自动切 pvector 大后端），再 bump——否则新增行的 bumpRow 越界被跳过。
        s.rowVers.SetLen(len(newLines))
        for _, i := range changed { s.bumpRow(i) }
        s.lineCount(len(newLines))
        s.sourceCode(text)
    })
}

// applyRope 把一次 Rope 编辑的结果（新根 + 光标）落地到状态：由新根取字符串，
// 复用同一份根做 token/行版本更新（不重复构造 Rope）。
func (s *EditorState) applyRope(r *Rope.Rope) {
    s.setSourceFromRope(r, r.String())
}

func (s *EditorState) lineText(idx int) string {
    _ = s.rowVersion(idx) // 登记行版本订阅：该行变化 → 视图重读
    if idx < 0 || idx >= len(s.textCache) { return "" }
    s.ensureLineParsed(idx) // 惰性：首次进入可见窗口才 tokenize
    return s.textCache[idx]
}

func (s *EditorState) lineCodeGradient(idx int) string {
    _ = s.rowVersion(idx)
    if idx < 0 || idx >= len(s.tokenCache) { return "" }
    s.ensureLineParsed(idx)
    return s.buildGradient(s.tokenCache[idx])
}

// lineRichText 把一行 token 编码成结构化富文本（每段带主题语法色），供 Web/Skia
// 后端走富文本分段着色通道。与 lineCodeGradient 并存：gradient 给只认标量 props
// 的 Rust 后端，rich-text 给能消费结构化分段的 Web/Skia，三端语法高亮同源。
func (s *EditorState) lineRichText(idx int) gxscene.RichText {
    _ = s.rowVersion(idx)
    if idx < 0 || idx >= len(s.tokenCache) { return gxscene.RichText{} }
    s.ensureLineParsed(idx)
    toks := s.tokenCache[idx]
    out := gxscene.RichText{}
    for _, t := range toks {
        if t.Text == "" { continue }
        out = append(out, gxscene.RichSpan{ Text: t.Text, Color: s.color(t.Color) })
    }
    return out
}

func (s *EditorState) lineRowBg(idx int) string {
    if idx == s.cursorLine() && s.focused() { return s.color("active") }
    return s.color("bg")
}
func (s *EditorState) lineGutterColor(idx int) string { return s.lineRowBg(idx) }
func (s *EditorState) lineNumber(idx int) string { return strconv.Itoa(idx + 1) }
func (s *EditorState) rowHeight(idx int) int {
    if idx < s.lineCount() { return 22 }
    return 0
}

// ---- 视图（GX JSX，三端共用）----

var editorRowIDs = indices(24)

func (s *EditorState) lineView(idx int) *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"gx-editor-row",Key:"class",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"bg",Slot:0,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"height",Slot:1,},gxscene.Op{Code:gxscene.OpAttr,Arg:"hidden",Key:"overflow",},gxscene.Op{Code:gxscene.OpAttr,Arg:"0",Key:"flex-shrink",},gxscene.Op{Code:gxscene.OpAttr,Arg:"row",Key:"flex-direction",},gxscene.Op{Code:gxscene.OpAttr,Arg:"center",Key:"align-items",},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"bg",Slot:2,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"color",Slot:3,},gxscene.Op{Code:gxscene.OpAttr,Arg:"42",Key:"width",},gxscene.Op{Code:gxscene.OpAttr,Arg:"12",Key:"font-size",},gxscene.Op{Code:gxscene.OpAttr,Arg:"22",Key:"height",},gxscene.Op{Code:gxscene.OpAttr,Arg:"22px",Key:"line-height",},gxscene.Op{Code:gxscene.OpAttr,Arg:"8",Key:"margin-right",},gxscene.Op{Code:gxscene.OpAttr,Arg:"0",Key:"flex-shrink",},gxscene.Op{Code:gxscene.OpAttr,Arg:"right",Key:"text-align",},gxscene.Op{Code:gxscene.OpDynChild,Slot:4,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpElem,Arg:"span",},gxscene.Op{Code:gxscene.OpAttr,Arg:"gx-code-line",Key:"class",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"data-code",Slot:5,},gxscene.Op{Code:gxscene.OpAttr,Arg:"1",Key:"gradient-text",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"font-family",Slot:6,},gxscene.Op{Code:gxscene.OpAttr,Arg:"pre",Key:"white-space",},gxscene.Op{Code:gxscene.OpAttr,Arg:"13",Key:"font-size",},gxscene.Op{Code:gxscene.OpAttr,Arg:"22",Key:"height",},gxscene.Op{Code:gxscene.OpAttr,Arg:"22px",Key:"line-height",},gxscene.Op{Code:gxscene.OpAttr,Arg:"0",Key:"flex-shrink",},gxscene.Op{Code:gxscene.OpAttr,Arg:"relative",Key:"position",},gxscene.Op{Code:gxscene.OpAttr,Arg:"2",Key:"z-index",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"bg",Slot:7,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"rich-text",Slot:8,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"selection-range",Slot:9,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"style",Slot:10,},gxscene.Op{Code:gxscene.OpDynChild,Slot:11,},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:12,}, func() any{ return s.lineRowBg(idx) }, func() any{ return s.rowHeight(idx) }, func() any{ return s.lineGutterColor(idx) }, func() any{ return s.color("gutter") }, func() any{ return s.lineNumber(idx) }, func() any{ return s.lineText(idx) }, func() any{ return editorCodeFont }, func() any{ return s.lineCodeGradient(idx) }, func() any{ return s.lineRichText(idx) }, func() any{ return s.lineSelRange(idx) }, func() any{ return gxscene.Style{Selection:&gxscene.Style{Bg:s.color("selectSolid"),},} }, func() any{ return s.lineText(idx) })
}

func (s *EditorState) selectionView(idx int) *gxscene.Instance {
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"gx-sel",Key:"class",},gxscene.Op{Code:gxscene.OpAttr,Arg:"absolute",Key:"position",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"top",Slot:0,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"left",Slot:1,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"width",Slot:2,},gxscene.Op{Code:gxscene.OpAttr,Arg:"22",Key:"height",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"bg",Slot:3,},gxscene.Op{Code:gxscene.OpAttr,Arg:"1",Key:"z-index",},gxscene.Op{Code:gxscene.OpClose,},},N:4,}, func() any{ return s.selRectTop(idx) }, func() any{ return s.selRectLeft(idx) }, func() any{ return s.selRectWidth(idx) }, func() any{ return s.selRectBg(idx) })
}

// Editor 构造整个编辑器视图。每次建树时建立主题色 palette（UseToken 依赖建树期
// Owner），使配色随明暗主题响应式切换。
//
// 虚拟滚动：用 VirtualList 取代原来写死的 indices(24) <For>。行高固定 22px，
// 走显式 height 模式（零回流）；场景树里只存在可见行 + overscan，节点数与文档
// 行数解耦——几千行文档也只建约 20 个行节点。scrollTop 受控于 EditorState，
// 光标移出视口时由 ensureCursorVisible 自动滚动跟随。
func Editor(st *EditorState) *gxscene.Instance {
    if st == nil { st = NewEditorState("") }
    st.palette = newEditorPalette()
    list := VirtualList(gxscene.Props{
        "count":           func() any { return st.lineCount() },
        "item-height":     editorRowHeight,
        "viewport-height": editorViewportHeight,
        "scroll-top":      func() any { return st.scrollTop() },
        "render-item":     func(i int) *gxscene.Instance { return st.lineView(i) },
        "on-scroll":       func(v any) { if f, ok := v.(float64); ok { st.SetScrollTop(f) } },
        "overscan":        editorOverscan,
        "bg":              st.color("bg"),
    })
    // 光标跟随：建树期订阅光标行/行数，移出视口时自动滚动（见 ensureCursorVisible）。
    st.installCursorFollow()
    return gxscene.Render(gxscene.Template{Ops:[]gxscene.Op{gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"bg",Slot:0,},gxscene.Op{Code:gxscene.OpAttr,Arg:"8",Key:"border-radius",},gxscene.Op{Code:gxscene.OpAttr,Arg:"6",Key:"padding",},gxscene.Op{Code:gxscene.OpAttr,Arg:"column",Key:"flex-direction",},gxscene.Op{Code:gxscene.OpAttr,Arg:"0",Key:"gap",},gxscene.Op{Code:gxscene.OpAttr,Arg:"482",Key:"height",},gxscene.Op{Code:gxscene.OpAttr,Arg:"relative",Key:"position",},gxscene.Op{Code:gxscene.OpDynChild,Slot:1,},gxscene.Op{Code:gxscene.OpElem,Arg:"div",},gxscene.Op{Code:gxscene.OpAttr,Arg:"gx-caret",Key:"class",},gxscene.Op{Code:gxscene.OpAttr,Arg:"none",Key:"pointer-events",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"opacity",Slot:2,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"animation",Slot:3,},gxscene.Op{Code:gxscene.OpAttr,Arg:"absolute",Key:"position",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"left",Slot:4,},gxscene.Op{Code:gxscene.OpDynAttr,Key:"top",Slot:5,},gxscene.Op{Code:gxscene.OpAttr,Arg:"2",Key:"width",},gxscene.Op{Code:gxscene.OpAttr,Arg:"22",Key:"height",},gxscene.Op{Code:gxscene.OpDynAttr,Key:"bg",Slot:6,},gxscene.Op{Code:gxscene.OpAttr,Arg:"6",Key:"z-index",},gxscene.Op{Code:gxscene.OpClose,},gxscene.Op{Code:gxscene.OpClose,},},N:7,}, func() any{ return st.color("bg") }, func() any{ return list }, func() any{ return st.caretOpacity() }, func() any{ return st.caretAnimation() }, func() any{ return st.caretLeft() }, func() any{ return st.caretTopVisible() }, func() any{ return st.color("caret") })
}

// Status 返回编辑器状态栏文本（行列 + 行数），供 playground 头部展示。
func (s *EditorState) Status() string {
    v := "Ln " + strconv.Itoa(s.cursorLine()+1) + ", Col " + strconv.Itoa(s.cursorCol()+1) +
         "  |  " + strconv.Itoa(s.lineCount()) + " lines"
    if s.HasSelection() { v = v + "  |  " + strconv.Itoa(runeLen(s.SelectedText())) + " selected" }
    return v
}

// LineText 导出给装配层诊断/集成用：读第 idx 行第 slot 个 token 文本。
func (s *EditorState) LineText(idx int, slot int) string {
    _ = s.rowVersion(idx)
    if idx < 0 || idx >= len(s.tokenCache) { return "" }
    s.ensureLineParsed(idx)
    toks := s.tokenCache[idx]
    if slot < 0 || slot >= len(toks) { return "" }
    return toks[slot].Text
}
