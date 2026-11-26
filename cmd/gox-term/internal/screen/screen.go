// Package screen 提供一个轻量的 VT/ANSI 终端屏幕模拟器。
//
// 它与简单的「逐行追加文本」解析器的本质区别：维护一块二维字符网格
// （每格有字符 + 颜色 + 属性），并正确处理光标寻址、局部擦除、滚动、
// 交替屏幕等控制序列。因此 vim/htop 这类全屏 TUI 发出的「跳到第 N 行
// 第 M 列、擦掉、重写」指令能被正确还原，而不是把内容往后追加导致乱套。
//
// 本包是纯逻辑，不依赖 bubbletea / pty，便于单测。
package screen

import (
	"strings"
	"unicode/utf8"
)

// ColorKind 区分颜色表达方式。
type ColorKind uint8

const (
	ColorDefault ColorKind = iota // 终端默认色（渲染时用主题默认值）
	ColorIndex                    // 256 色索引：V1 = 0..255
	ColorRGB                      // 真彩：V1/V2/V3 = R/G/B
)

// Color 是一个前景或背景色。
type Color struct {
	Kind       ColorKind
	V1, V2, V3 uint8
}

// Cell 是屏幕上的一个字符格。
type Cell struct {
	Ch   rune
	Fg   Color
	Bg   Color
	Attr uint8
	Cont bool // 是前一个宽字符（CJK）的占位格，渲染时跳过
}

// 文本属性位。
const (
	attrBold uint8 = 1 << iota
	attrDim
	attrItalic
	attrUnderline
	attrBlink
	attrReverse
	attrHidden
	attrStrike
)

// 解析状态。
type pstate uint8

const (
	pGround pstate = iota
	pEscape
	pEscIntermediate
	pCSIEntry
	pCSIParam
	pCSIIntermediate
	pOSC
	pOSCEsc
	pUTF8
)

// Screen 是一块终端屏幕。所有方法都在单个 goroutine（UI 线程）调用。
type Screen struct {
	Cols, Rows int

	normal [][]Cell // 常规屏幕缓冲
	alt    [][]Cell // 交替屏幕缓冲（vim/less 使用）
	buf    [][]Cell // 当前指向 normal 或 alt
	inAlt  bool

	x, y             int
	savedX, savedY   int
	fg, bg           Color
	attr             uint8
	savedFg, savedBg Color
	savedAttr        uint8

	origin      bool // 相对寻址（光标限制在滚动区内）
	autowrap    bool
	insert      bool
	top, bottom int // 滚动区（0 基，含端点）
	showCursor  bool
	lastChar    rune // REP（ESC[b）重复用
	pendingWrap bool // 光标停在最后一列，下个字符需先换行

	state    pstate
	csi      []byte
	osc      []byte
	utf8Buf  [4]byte
	utf8Need int
	utf8Have int

	history    [][]Cell // 从顶部滚出的行（常规屏幕）
	maxHistory int

	// 默认前景/背景（主题色），用于 ColorDefault 的渲染。
	defFg, defBg Color
}

// New 创建一块 cols×rows 的屏幕。
func New(cols, rows int) *Screen {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	s := &Screen{
		Cols: cols, Rows: rows,
		autowrap:   true,
		showCursor: true,
		maxHistory: 2000,
	}
	s.normal = newGrid(cols, rows)
	s.alt = newGrid(cols, rows)
	s.buf = s.normal
	s.top, s.bottom = 0, rows-1
	return s
}

func newGrid(cols, rows int) [][]Cell {
	g := make([][]Cell, rows)
	for i := range g {
		g[i] = make([]Cell, cols)
	}
	return g
}

// SetDefaultColors 设置主题默认前景/背景（hex 或 ANSI 色号），用于
// ColorDefault 格的渲染。传入空串表示沿用终端自身默认色（不发射颜色序列）。
func (s *Screen) SetDefaultColors(fg, bg string) {
	s.defFg = parseColorSpec(fg)
	s.defBg = parseColorSpec(bg)
}

// InAlt 报告当前是否处于交替屏幕。
func (s *Screen) InAlt() bool { return s.inAlt }

// Cursor 返回当前光标位置（0 基）。
func (s *Screen) Cursor() (int, int) { return s.x, s.y }

// Resize 调整屏幕尺寸：保留左上角重叠区域内容，钳制光标与滚动区。
func (s *Screen) Resize(cols, rows int) {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	if cols == s.Cols && rows == s.Rows {
		return
	}
	s.normal = reflow(s.normal, cols, rows)
	s.alt = reflow(s.alt, cols, rows)
	if s.inAlt {
		s.buf = s.alt
	} else {
		s.buf = s.normal
	}
	s.Cols, s.Rows = cols, rows
	s.top, s.bottom = 0, rows-1
	if s.x >= cols {
		s.x = cols - 1
	}
	if s.y >= rows {
		s.y = rows - 1
	}
}

func reflow(old [][]Cell, cols, rows int) [][]Cell {
	g := newGrid(cols, rows)
	for y := 0; y < rows && y < len(old); y++ {
		for x := 0; x < cols && x < len(old[y]); x++ {
			g[y][x] = old[y][x]
		}
	}
	return g
}

// Write 把一段 PTY 输出喂给解析器。
func (s *Screen) Write(data []byte) {
	for _, b := range data {
		s.step(b)
	}
}

func (s *Screen) step(b byte) {
	switch s.state {
	case pGround:
		s.ground(b)
	case pEscape:
		if b >= 0x20 && b <= 0x2f {
			s.state = pEscIntermediate
		} else {
			s.state = pGround
			s.escape(b)
		}
	case pEscIntermediate:
		// 忽略 ESC ( B 之类的字符集指定。
		s.state = pGround
	case pCSIEntry:
		if b >= 0x30 && b <= 0x3f { // 参数字节（含 ? < = >）
			s.csi = append(s.csi, b)
			s.state = pCSIParam
		} else if b >= 0x20 && b <= 0x2f {
			s.state = pCSIIntermediate
		} else {
			s.state = pGround
			s.csiFinal(b)
		}
	case pCSIParam:
		if b >= 0x30 && b <= 0x3f {
			s.csi = append(s.csi, b)
		} else if b >= 0x20 && b <= 0x2f {
			s.state = pCSIIntermediate
		} else {
			s.state = pGround
			s.csiFinal(b)
		}
	case pCSIIntermediate:
		if b >= 0x20 && b <= 0x2f {
			// 继续吞中间字节
		} else {
			s.state = pGround
			s.csiFinal(b)
		}
	case pOSC:
		if b == 0x07 { // BEL 结束
			s.state = pGround
			s.osc = s.osc[:0]
		} else if b == 0x1b {
			s.state = pOSCEsc
		} else {
			s.osc = append(s.osc, b)
			if len(s.osc) > 4096 {
				s.osc = s.osc[:0]
			}
		}
	case pOSCEsc:
		if b == '\\' { // ST = ESC \
			s.state = pGround
		} else {
			s.state = pOSC
		}
		s.osc = s.osc[:0]
	case pUTF8:
		s.utf8Buf[s.utf8Have] = b
		s.utf8Have++
		if s.utf8Have >= s.utf8Need {
			r, _ := utf8.DecodeRune(s.utf8Buf[:s.utf8Have])
			s.state = pGround
			if r == utf8.RuneError || r < 0x20 {
				r = '\uFFFD'
			}
			s.putRune(r)
		}
	}
}

func (s *Screen) ground(b byte) {
	switch {
	case b == 0x1b:
		s.state = pEscape
	case b == '\r':
		s.x = 0
		s.pendingWrap = false
	case b == '\n' || b == 0x0b || b == 0x0c:
		s.lineFeed()
		s.pendingWrap = false
	case b == '\b':
		if s.x > 0 {
			s.x--
		}
	case b == '\t':
		s.x = (s.x/8 + 1) * 8
		if s.x >= s.Cols {
			s.x = s.Cols - 1
		}
	case b < 0x20 || b == 0x7f:
		// 其它 C0 / DEL 忽略。
	case b < 0x80:
		s.putRune(rune(b))
	default:
		// UTF-8 多字节。
		need := utf8LenLead(b)
		s.utf8Need = need
		s.utf8Have = 0
		if need <= 1 {
			s.putRune('\uFFFD')
			return
		}
		s.utf8Buf[0] = b
		s.utf8Have = 1
		s.state = pUTF8
	}
}

func utf8LenLead(b byte) int {
	switch {
	case b&0xe0 == 0xc0:
		return 2
	case b&0xf0 == 0xe0:
		return 3
	case b&0xf8 == 0xf0:
		return 4
	default:
		return 1
	}
}

func (s *Screen) escape(b byte) {
	switch b {
	case '[':
		s.csi = s.csi[:0]
		s.state = pCSIEntry
	case ']':
		s.osc = s.osc[:0]
		s.state = pOSC
	case '7':
		s.saveCursor()
	case '8':
		s.restoreCursor()
	case 'D':
		s.lineFeed() // IND
	case 'M':
		s.reverseIndex() // RI
	case 'c':
		s.reset()
	}
}

// csiFinal 解析并分发一条 CSI 指令。
func (s *Screen) csiFinal(final byte) {
	seq := string(s.csi)
	s.csi = s.csi[:0]
	if final < 0x40 || final > 0x7e {
		return
	}
	private := strings.HasPrefix(seq, "?")
	body := seq
	if private {
		body = seq[1:]
	}
	ps := parseParams(body)

	switch final {
	case 'H', 'f': // CUP / HVP
		row, col := 1, 1
		if len(ps) > 0 && ps[0] > 0 {
			row = ps[0]
		}
		if len(ps) > 1 && ps[1] > 0 {
			col = ps[1]
		}
		s.y = row - 1
		s.x = col - 1
		if s.origin {
			s.y += s.top
		}
		s.clampCursor()
	case 'A': // CUU
		s.y -= param(ps, 0, 1)
		s.clampCursor()
	case 'B': // CUD
		s.y += param(ps, 0, 1)
		s.clampCursor()
	case 'C': // CUF
		s.x += param(ps, 0, 1)
		s.clampCursor()
	case 'D': // CUB
		s.x -= param(ps, 0, 1)
		s.clampCursor()
	case 'E': // CNL
		s.y += param(ps, 0, 1)
		s.x = 0
		s.clampCursor()
	case 'F': // CPL
		s.y -= param(ps, 0, 1)
		s.x = 0
		s.clampCursor()
	case 'G': // CHA
		s.x = param(ps, 0, 1) - 1
		s.clampCursor()
	case 'd': // VPA
		s.y = param(ps, 0, 1) - 1
		if s.origin {
			s.y += s.top
		}
		s.clampCursor()
	case 'K': // EL
		s.eraseLine(param(ps, 0, 0))
	case 'J': // ED
		mode := param(ps, 0, 0)
		s.eraseDisplay(mode)
		if mode == 3 {
			s.history = s.history[:0]
		}
	case 'm': // SGR
		s.sgr(ps)
	case 'r': // DECSTBM
		top := param(ps, 0, 1) - 1
		bot := s.Rows - 1
		if len(ps) > 1 && ps[1] > 0 {
			bot = ps[1] - 1
		}
		if top < 0 || top >= s.Rows || bot < 0 || bot >= s.Rows || top >= bot {
			s.top, s.bottom = 0, s.Rows-1
		} else {
			s.top, s.bottom = top, bot
		}
		if s.origin {
			s.y = s.top
		} else {
			s.y = 0
		}
		s.x = 0
	case 's':
		s.saveCursor()
	case 'u':
		s.restoreCursor()
	case 'L': // IL
		s.insertLines(param(ps, 0, 1))
	case 'M': // DL
		s.deleteLines(param(ps, 0, 1))
	case '@': // ICH
		s.insertCells(param(ps, 0, 1))
	case 'P': // DCH
		s.deleteCells(param(ps, 0, 1))
	case 'X': // ECH
		s.eraseCells(param(ps, 0, 1))
	case 'S': // SU
		s.scrollUp(param(ps, 0, 1))
	case 'T': // SD
		s.scrollDown(param(ps, 0, 1))
	case 'b': // REP
		n := param(ps, 0, 1)
		for i := 0; i < n && i < 1000; i++ {
			s.putRune(s.lastChar)
		}
	case 'h', 'l':
		set := final == 'h'
		s.mode(private, ps, set)
	}
	// DA(ESC[c)、DSR、TBC 等查询类指令忽略。
}

func param(ps []int, idx, def int) int {
	if idx < len(ps) && ps[idx] > 0 {
		return ps[idx]
	}
	return def
}

func parseParams(body string) []int {
	if body == "" {
		return nil
	}
	parts := strings.Split(body, ":")
	if len(parts) == 1 {
		parts = strings.Split(body, ";")
	}
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			out = append(out, 0)
			continue
		}
		n := 0
		for i := 0; i < len(p); i++ {
			if p[i] < '0' || p[i] > '9' {
				break
			}
			n = n*10 + int(p[i]-'0')
			if n > 1<<24 {
				n = 1 << 24
			}
		}
		out = append(out, n)
	}
	return out
}

func (s *Screen) mode(private bool, ps []int, set bool) {
	for _, p := range ps {
		if private {
			switch p {
			case 1049: // 交替屏幕（保存/恢复光标）
				s.setAltScreen(set)
			case 1047, 1048:
				s.setAltScreen(set)
			case 25:
				s.showCursor = set
			case 7:
				s.autowrap = set
			case 6:
				s.origin = set
				// 1000/1002/1004/1015 鼠标、2004 粘贴括号、1 应用光标键：解析但忽略。
			}
		} else {
			switch p {
			case 4:
				s.insert = set
			}
		}
	}
}

func (s *Screen) setAltScreen(on bool) {
	if on == s.inAlt {
		return
	}
	if on {
		s.savedX, s.savedY = s.x, s.y
		for i := range s.alt {
			for j := range s.alt[i] {
				s.alt[i][j] = Cell{}
			}
		}
		s.buf = s.alt
		s.inAlt = true
	} else {
		s.buf = s.normal
		s.inAlt = false
		s.x, s.y = s.savedX, s.savedY
	}
	s.top, s.bottom = 0, s.Rows-1
	s.x, s.y = 0, 0 // 进入/退出通常伴随清屏+归位
}

func (s *Screen) saveCursor() {
	s.savedX, s.savedY = s.x, s.y
	s.savedFg, s.savedBg, s.savedAttr = s.fg, s.bg, s.attr
}

func (s *Screen) restoreCursor() {
	s.x, s.y = s.savedX, s.savedY
	s.fg, s.bg, s.attr = s.savedFg, s.savedBg, s.savedAttr
	s.clampCursor()
}

func (s *Screen) reset() {
	for i := range s.normal {
		for j := range s.normal[i] {
			s.normal[i][j] = Cell{}
		}
	}
	for i := range s.alt {
		for j := range s.alt[i] {
			s.alt[i][j] = Cell{}
		}
	}
	s.buf = s.normal
	s.inAlt = false
	s.x, s.y = 0, 0
	s.fg, s.bg, s.attr = Color{}, Color{}, 0
	s.origin, s.insert, s.autowrap, s.showCursor = false, false, true, true
	s.top, s.bottom = 0, s.Rows-1
	s.history = s.history[:0]
}

func (s *Screen) clampCursor() {
	s.pendingWrap = false // 任何显式光标定位都清除延迟换行
	if s.x < 0 {
		s.x = 0
	}
	if s.x >= s.Cols {
		s.x = s.Cols - 1
	}
	lo, hi := 0, s.Rows-1
	if s.origin {
		lo, hi = s.top, s.bottom
	}
	if s.y < lo {
		s.y = lo
	}
	if s.y > hi {
		s.y = hi
	}
}

func (s *Screen) putRune(r rune) {
	w := runeWidth(r)
	// 上一个字符填满最后一列后的延迟换行：写下一个字符前先换行。
	if s.pendingWrap && s.autowrap {
		s.x = 0
		s.lineFeed()
		s.pendingWrap = false
	}
	if s.x+w > s.Cols {
		if s.autowrap {
			s.x = 0
			s.lineFeed()
		} else {
			s.x = s.Cols - w
		}
	}
	if s.insert {
		s.shiftCellsRight(s.y, s.x, w)
	}
	c := Cell{Ch: r, Fg: s.fg, Bg: s.bg, Attr: s.attr}
	s.buf[s.y][s.x] = c
	s.lastChar = r
	s.x++
	if w == 2 && s.x < s.Cols {
		s.buf[s.y][s.x] = Cell{Cont: true, Fg: s.fg, Bg: s.bg, Attr: s.attr}
		s.x++
	}
	if s.x >= s.Cols {
		s.x = s.Cols - 1
		if s.autowrap {
			s.pendingWrap = true // 延迟换行：等下一个字符真正触发
		}
	}
}

func (s *Screen) lineFeed() {
	if s.y == s.bottom {
		s.scrollUp(1)
	} else if s.y < s.Rows-1 {
		s.y++
	}
}

func (s *Screen) reverseIndex() {
	if s.y == s.top {
		s.scrollDown(1)
	} else if s.y > 0 {
		s.y--
	}
}

func (s *Screen) scrollUp(n int) {
	if n <= 0 || s.top >= s.bottom {
		return
	}
	if n > s.bottom-s.top+1 {
		n = s.bottom - s.top + 1
	}
	for k := 0; k < n; k++ {
		if !s.inAlt && s.top == 0 {
			row := make([]Cell, s.Cols)
			copy(row, s.buf[0])
			s.history = append(s.history, row)
			if len(s.history) > s.maxHistory {
				s.history = s.history[len(s.history)-s.maxHistory:]
			}
		}
		for y := s.top; y < s.bottom; y++ {
			copy(s.buf[y], s.buf[y+1])
		}
		for x := range s.buf[s.bottom] {
			s.buf[s.bottom][x] = Cell{}
		}
	}
}

func (s *Screen) scrollDown(n int) {
	if n <= 0 || s.top >= s.bottom {
		return
	}
	if n > s.bottom-s.top+1 {
		n = s.bottom - s.top + 1
	}
	for k := 0; k < n; k++ {
		for y := s.bottom; y > s.top; y-- {
			copy(s.buf[y], s.buf[y-1])
		}
		for x := range s.buf[s.top] {
			s.buf[s.top][x] = Cell{}
		}
	}
}

func (s *Screen) eraseLine(mode int) {
	row := s.buf[s.y]
	switch mode {
	case 0: // 光标到行尾
		for x := s.x; x < s.Cols; x++ {
			row[x] = Cell{}
		}
	case 1: // 行首到光标（含）
		for x := 0; x <= s.x && x < s.Cols; x++ {
			row[x] = Cell{}
		}
	case 2: // 整行
		for x := range row {
			row[x] = Cell{}
		}
	}
}

func (s *Screen) eraseDisplay(mode int) {
	switch mode {
	case 0: // 光标到屏幕末尾
		s.eraseLine(0)
		for y := s.y + 1; y < s.Rows; y++ {
			for x := range s.buf[y] {
				s.buf[y][x] = Cell{}
			}
		}
	case 1: // 屏幕开头到光标
		for y := 0; y < s.y; y++ {
			for x := range s.buf[y] {
				s.buf[y][x] = Cell{}
			}
		}
		s.eraseLine(1)
	case 2: // 整屏
		for y := range s.buf {
			for x := range s.buf[y] {
				s.buf[y][x] = Cell{}
			}
		}
	}
}

func (s *Screen) insertLines(n int) {
	if s.y < s.top || s.y > s.bottom {
		return
	}
	for k := 0; k < n; k++ {
		for y := s.bottom; y > s.y; y-- {
			copy(s.buf[y], s.buf[y-1])
		}
		for x := range s.buf[s.y] {
			s.buf[s.y][x] = Cell{}
		}
	}
}

func (s *Screen) deleteLines(n int) {
	if s.y < s.top || s.y > s.bottom {
		return
	}
	for k := 0; k < n; k++ {
		for y := s.y; y < s.bottom; y++ {
			copy(s.buf[y], s.buf[y+1])
		}
		for x := range s.buf[s.bottom] {
			s.buf[s.bottom][x] = Cell{}
		}
	}
}

func (s *Screen) shiftCellsRight(y, x, n int) {
	if y < 0 || y >= s.Rows {
		return
	}
	row := s.buf[y]
	for i := s.Cols - 1; i >= x+n; i-- {
		row[i] = row[i-n]
	}
	for i := x; i < s.Cols && i < x+n; i++ {
		row[i] = Cell{}
	}
}

func (s *Screen) insertCells(n int) {
	s.shiftCellsRight(s.y, s.x, n)
}

func (s *Screen) deleteCells(n int) {
	if s.y < 0 || s.y >= s.Rows {
		return
	}
	row := s.buf[s.y]
	for x := s.x; x+n < s.Cols; x++ {
		row[x] = row[x+n]
	}
	for x := s.Cols - n; x < s.Cols; x++ {
		if x >= 0 {
			row[x] = Cell{}
		}
	}
}

func (s *Screen) eraseCells(n int) {
	if s.y < 0 || s.y >= s.Rows {
		return
	}
	row := s.buf[s.y]
	for x := s.x; x < s.Cols && x < s.x+n; x++ {
		row[x] = Cell{}
	}
}

func (s *Screen) sgr(ps []int) {
	if len(ps) == 0 {
		ps = []int{0}
	}
	for i := 0; i < len(ps); i++ {
		p := ps[i]
		switch {
		case p == 0:
			s.fg, s.bg, s.attr = Color{}, Color{}, 0
		case p == 1:
			s.attr |= attrBold
		case p == 2:
			s.attr |= attrDim
		case p == 3:
			s.attr |= attrItalic
		case p == 4:
			s.attr |= attrUnderline
		case p == 5, p == 6:
			s.attr |= attrBlink
		case p == 7:
			s.attr |= attrReverse
		case p == 8:
			s.attr |= attrHidden
		case p == 9:
			s.attr |= attrStrike
		case p == 21 || p == 22:
			s.attr &^= attrBold | attrDim
		case p == 23:
			s.attr &^= attrItalic
		case p == 24:
			s.attr &^= attrUnderline
		case p == 25:
			s.attr &^= attrBlink
		case p == 27:
			s.attr &^= attrReverse
		case p == 28:
			s.attr &^= attrHidden
		case p == 29:
			s.attr &^= attrStrike
		case p >= 30 && p <= 37:
			s.fg = Color{Kind: ColorIndex, V1: uint8(p - 30)}
		case p == 38:
			c, next := extendedColor(ps, i, 38)
			s.fg = c
			i = next
		case p == 39:
			s.fg = Color{}
		case p >= 40 && p <= 47:
			s.bg = Color{Kind: ColorIndex, V1: uint8(p - 40)}
		case p == 48:
			c, next := extendedColor(ps, i, 48)
			s.bg = c
			i = next
		case p == 49:
			s.bg = Color{}
		case p >= 90 && p <= 97:
			s.fg = Color{Kind: ColorIndex, V1: uint8(p - 90 + 8)}
		case p >= 100 && p <= 107:
			s.bg = Color{Kind: ColorIndex, V1: uint8(p - 100 + 8)}
		}
	}
}

func extendedColor(ps []int, i, base int) (Color, int) {
	// ps[i] == 38/48；后面是 5;n 或 2;r;g;b。
	if i+1 >= len(ps) {
		return Color{}, i
	}
	if ps[i+1] == 5 && i+2 < len(ps) {
		return Color{Kind: ColorIndex, V1: uint8(ps[i+2])}, i + 2
	}
	if ps[i+1] == 2 && i+4 < len(ps) {
		return Color{Kind: ColorRGB, V1: uint8(ps[i+2]), V2: uint8(ps[i+3]), V3: uint8(ps[i+4])}, i + 4
	}
	return Color{}, i
}

// ---- 渲染 ----

// HistoryCount 返回滚动历史行数。
func (s *Screen) HistoryCount() int { return len(s.history) }

// RenderWindow 返回 height 行可见内容；scrollBack > 0 时向上回溯历史行。
// 每行是带 SGR  ANSI 的字符串，显示宽度恰为 Cols。
func (s *Screen) RenderWindow(scrollBack, height int) []string {
	if height < 1 {
		height = 1
	}
	total := len(s.history) + s.Rows
	end := total - scrollBack
	if end < 0 {
		end = 0
	}
	if end > total {
		end = total
	}
	start := end - height
	if start < 0 {
		start = 0
	}
	lines := make([]string, 0, height)
	atBottom := end == total
	for i := start; i < end && len(lines) < height; i++ {
		if i < len(s.history) {
			lines = append(lines, s.renderCells(s.history[i], -1, false))
		} else {
			cx := -1
			if atBottom && i == total-1 && s.showCursor {
				cx = s.x
			}
			lines = append(lines, s.renderCells(s.buf[i-len(s.history)], cx, cx >= 0))
		}
	}
	for len(lines) < height {
		lines = append(lines, s.renderCells(make([]Cell, s.Cols), -1, false))
	}
	return lines
}

func (s *Screen) renderCells(row []Cell, cursorX int, showCursor bool) string {
	var b strings.Builder
	// 初始状态：主题默认前景/背景。
	base := s.stateSeq(Color{}, Color{}, 0)
	b.WriteString(base)
	cur := base
	for x := 0; x < s.Cols; x++ {
		var c Cell
		if x < len(row) {
			c = row[x]
		}
		if c.Cont {
			continue // 宽字符的占位格不额外输出
		}
		at := c.Attr
		if showCursor && x == cursorX {
			at ^= attrReverse // 反显模拟光标块
		}
		seq := s.stateSeq(c.Fg, c.Bg, at)
		if seq != cur {
			b.WriteString(seq)
			cur = seq
		}
		if c.Ch == 0 || c.Ch < 0x20 {
			b.WriteByte(' ')
		} else {
			b.WriteRune(c.Ch)
		}
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

// stateSeq 生成「从默认态设置到 fg/bg/attr」的完整 SGR 序列。
func (s *Screen) stateSeq(fg, bg Color, attr uint8) string {
	var nums []string
	if attr&attrBold != 0 {
		nums = append(nums, "1")
	}
	if attr&attrDim != 0 {
		nums = append(nums, "2")
	}
	if attr&attrItalic != 0 {
		nums = append(nums, "3")
	}
	if attr&attrUnderline != 0 {
		nums = append(nums, "4")
	}
	if attr&attrBlink != 0 {
		nums = append(nums, "5")
	}
	if attr&attrReverse != 0 {
		nums = append(nums, "7")
	}
	if attr&attrHidden != 0 {
		nums = append(nums, "8")
	}
	if attr&attrStrike != 0 {
		nums = append(nums, "9")
	}
	if seq := s.colorSeq(38, fg, s.defFg); seq != "" {
		nums = append(nums, seq)
	}
	if seq := s.colorSeq(48, bg, s.defBg); seq != "" {
		nums = append(nums, seq)
	}
	if len(nums) == 0 {
		return "\x1b[0m"
	}
	return "\x1b[0m\x1b[" + strings.Join(nums, ";") + "m"
}

func (s *Screen) colorSeq(base int, c, def Color) string {
	switch c.Kind {
	case ColorDefault:
		return colorSeqRaw(base, def)
	case ColorIndex:
		return itoa(base) + ";5;" + itoa(int(c.V1))
	case ColorRGB:
		return itoa(base) + ";2;" + itoa(int(c.V1)) + ";" + itoa(int(c.V2)) + ";" + itoa(int(c.V3))
	}
	return ""
}

func colorSeqRaw(base int, c Color) string {
	switch c.Kind {
	case ColorIndex:
		return itoa(base) + ";5;" + itoa(int(c.V1))
	case ColorRGB:
		return itoa(base) + ";2;" + itoa(int(c.V1)) + ";" + itoa(int(c.V2)) + ";" + itoa(int(c.V3))
	}
	return ""
}

// parseColorSpec 解析 "#rrggbb" 或纯 ANSI 色号（"n"）为 Color。
func parseColorSpec(s string) Color {
	if s == "" {
		return Color{}
	}
	if len(s) == 7 && s[0] == '#' {
		r := hexNibble(s[1])<<4 | hexNibble(s[2])
		g := hexNibble(s[3])<<4 | hexNibble(s[4])
		b := hexNibble(s[5])<<4 | hexNibble(s[6])
		return Color{Kind: ColorRGB, V1: r, V2: g, V3: b}
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return Color{}
		}
		n = n*10 + int(s[i]-'0')
	}
	return Color{Kind: ColorIndex, V1: uint8(n)}
}

func hexNibble(b byte) uint8 {
	switch {
	case b >= '0' && b <= '9':
		return b - '0'
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10
	}
	return 0
}

func runeWidth(r rune) int {
	if r < 0x20 || (r >= 0x7f && r < 0xa0) {
		return 0
	}
	if r < 0x2ff || (r >= 0x370 && r < 0x1100) {
		return 1
	}
	return 2
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + itoa(-n)
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// DumpText 返回网格的纯文本（每行一条，用于调试诊断）。
func (s *Screen) DumpText() []string {
	out := make([]string, s.Rows)
	for r := 0; r < s.Rows; r++ {
		var b strings.Builder
		for _, c := range s.buf[r] {
			if c.Cont {
				continue
			}
			if c.Ch == 0 || c.Ch < 0x20 {
				b.WriteByte(' ')
			} else {
				b.WriteRune(c.Ch)
			}
		}
		out[r] = strings.TrimRight(b.String(), " ")
	}
	return out
}
