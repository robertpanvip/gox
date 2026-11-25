// Package rope 提供持久（不可变）Rope + SumTree 文本缓冲，面向编辑器场景。
//
// 设计目标（性能优先）：
//   - 插入 / 删除 / 索引：O(log N)，而非编辑器当前 []string + SplitLines/JoinLines 的 O(N) 全量重建。
//   - SumTree 聚合：每个内部节点缓存 runes / lines / bytes / tailLineRunes，使行列换算
//     （光标、选区、行首偏移）沿树下降一次 O(log N) 完成，无需扫描文本或二次下降。
//   - 叶内稀疏采样 byteAt：每 runeSampleStep 个 rune 记录一次字节偏移，把叶内
//     rune→byte 映射从 O(叶子长度) 降到 O(runeSampleStep)。
//   - 持久结构共享：所有修改返回新根，旧根及其未触碰子树完全复用。天然契合 GX 的
//     signal 值语义（相等判断是 O(1) 指针比较），撤销栈只需保存旧根指针。
//   - UTF-8 透明：索引单位统一为 rune（编辑器列语义），bytes 仅用于与字节流互操作。
//
// 平衡策略：AVL。叶子容量上限 leafMaxRunes，构造时二分归并；修改经 split+join 自底向上再平衡。
// 单线程使用（与 GX UI Runtime 一致），不做并发保护。
package rope

import (
	"strings"
	"unicode/utf8"
)

// leafMaxRunes 是单个叶子的 rune 容量上限。128 对编辑器典型的"行"粒度是合适的折中。
const leafMaxRunes = 128

// runeSampleStep 是叶内字节偏移采样步长。每 8 个 rune 存一个 uint16 字节偏移，
// 使 runeRangeToBytes 的定位从最坏 O(128) 降到最多扫 8 个 rune。
// 叶子最大 128 rune × 4 字节 = 512 字节，uint16 足够表示任何字节偏移。
const runeSampleStep = 8

// sampleSlots 是叶内采样数组槽位数。叶子最多 leafMaxRunes 个 rune，
// 采样点为第 0,8,16,...,(128-8) 个 rune，共 16 个槽足够。
const sampleSlots = leafMaxRunes / runeSampleStep

// node 是 Rope 的树节点。叶子（left==nil）存片段文本；内部节点只存聚合与子树。
// 所有字段不可变：一旦构造不再修改，修改走路径复制。
type node struct {
	left, right   *node
	text          string // 仅叶子有效
	runes         int    // 子树 rune 总数（weight）
	lines         int    // 子树中 '\n' 的个数（SumTree 聚合）
	bytes         int    // 子树 UTF-8 字节数（SumTree 聚合）
	tailLineRunes int    // 子树最后一个 '\n' 之后的 rune 数；无 '\n' 时等于 runes
	height        int    // AVL 高度
	// 仅叶子有效：byteAt[k] = 第 k*runeSampleStep 个 rune 的字节偏移。
	// 内嵌定长数组而非切片，避免每个叶子一次堆分配（写路径关键）。
	byteAt [sampleSlots]uint16
	nByte  uint8 // 实际采样点数（<= 16）
}

func leaf(s string) *node {
	rc := utf8.RuneCountInString(s)
	n := &node{
		text:          s,
		runes:         rc,
		lines:         strings.Count(s, "\n"),
		bytes:         len(s),
		tailLineRunes: tailRunes(s),
		height:        1,
	}
	// 稀疏采样：每 runeSampleStep 个 rune 记录一次字节偏移，写入内嵌数组。
	bi, ri := 0, 0
	for bi < len(s) {
		if ri%runeSampleStep == 0 {
			n.byteAt[n.nByte] = uint16(bi)
			n.nByte++
		}
		_, sz := utf8.DecodeRuneInString(s[bi:])
		bi += sz
		ri++
	}
	return n
}

// tailRunes 返回 s 中最后一个 '\n' 之后的 rune 数；无 '\n' 时返回全部 rune 数。
func tailRunes(s string) int {
	i := strings.LastIndexByte(s, '\n')
	if i < 0 {
		return utf8.RuneCountInString(s)
	}
	return utf8.RuneCountInString(s[i+1:])
}

// branch 以 l、r 为左右子树构造内部节点。nil 子树直接返回另一侧（恒等），
// 这既简化 split/join 的边界处理，也让单叶子树无需包裹空内部节点。
func branch(l, r *node) *node {
	if l == nil {
		return r
	}
	if r == nil {
		return l
	}
	// 尾行（最后一个 '\n' 之后）的长度：若右子树含换行，尾行完全在右子树；
	// 否则尾行从左子树延续到右子树，长度为 left.tail + right.runes。
	tail := r.tailLineRunes
	if r.lines == 0 {
		tail = l.tailLineRunes + r.runes
	}
	n := &node{
		left: l, right: r,
		runes:         l.runes + r.runes,
		lines:         l.lines + r.lines,
		bytes:         l.bytes + r.bytes,
		tailLineRunes: tail,
	}
	h := l.height
	if r.height > h {
		h = r.height
	}
	n.height = h + 1
	return n
}

func height(n *node) int {
	if n == nil {
		return 0
	}
	return n.height
}

// Rope 是文本缓冲的句柄。零值 Rope{} 是空串，可直接用。
// 唯一字段是根指针；所有操作返回新值，不修改原值。
type Rope struct {
	root *node
}

// Of 从字符串构造一棵平衡的 Rope。按 rune 边界切成叶子后二分归并。
func Of(s string) *Rope {
	if s == "" {
		return &Rope{}
	}
	var leaves []*node
	for i := 0; i < len(s); {
		j := i
		for k := 0; k < leafMaxRunes && j < len(s); k++ {
			_, sz := utf8.DecodeRuneInString(s[j:])
			j += sz
		}
		leaves = append(leaves, leaf(s[i:j]))
		i = j
	}
	return &Rope{root: buildBalanced(leaves)}
}

func buildBalanced(leaves []*node) *node {
	switch len(leaves) {
	case 0:
		return nil
	case 1:
		return leaves[0]
	}
	mid := len(leaves) / 2
	return branch(buildBalanced(leaves[:mid]), buildBalanced(leaves[mid:]))
}

// String 返回完整文本。O(N)，仅用于剪贴板/导出/序列化等低频路径。
func (r *Rope) String() string {
	if r == nil || r.root == nil {
		return ""
	}
	var b strings.Builder
	b.Grow(r.root.bytes)
	collect(r.root, &b)
	return b.String()
}

func collect(n *node, b *strings.Builder) {
	if n == nil {
		return
	}
	if n.left == nil {
		b.WriteString(n.text)
		return
	}
	collect(n.left, b)
	collect(n.right, b)
}

// RuneLen 返回文本 rune 数。O(1)——直接读 SumTree 根聚合。
func (r *Rope) RuneLen() int {
	if r == nil || r.root == nil {
		return 0
	}
	return r.root.runes
}

// ByteLen 返回 UTF-8 字节数。O(1)。
func (r *Rope) ByteLen() int {
	if r == nil || r.root == nil {
		return 0
	}
	return r.root.bytes
}

// LineCount 返回行数（换行符数 + 1）。O(1)——SumTree 核心收益之一。
func (r *Rope) LineCount() int {
	if r == nil || r.root == nil {
		return 1
	}
	return r.root.lines + 1
}

// Slice 返回 [start, end) rune 区间字符串。O(log N + 输出长度)。
// 越界按最近合法边界夹紧；start >= end 返回空串。
func (r *Rope) Slice(start, end int) string {
	if r == nil || r.root == nil || start >= end {
		return ""
	}
	n := r.root
	if start < 0 {
		start = 0
	}
	if end > n.runes {
		end = n.runes
	}
	if start >= end {
		return ""
	}
	var b strings.Builder
	// 精确预估字节数：用全文平均字节/rune 比避免 CJK 场景的多次扩容。
	// UTF-8 下 bytes >= runes，故 avg >= 1。
	avg := n.bytes / n.runes
	if avg < 1 {
		avg = 1
	}
	b.Grow((end - start) * avg)
	sliceInto(n, start, end, &b)
	return b.String()
}

func sliceInto(n *node, start, end int, b *strings.Builder) {
	if n == nil || start >= end {
		return
	}
	if n.left == nil {
		bs, be := runeRangeToBytes(n, start, end)
		b.WriteString(n.text[bs:be])
		return
	}
	lr := n.left.runes
	if start < lr {
		e := end
		if e > lr {
			e = lr
		}
		sliceInto(n.left, start, e, b)
	}
	if end > lr {
		s := start
		if s < lr {
			s = lr
		}
		sliceInto(n.right, s-lr, end-lr, b)
	}
}

// runeRangeToBytes 把叶子片段内的 [start,end) rune 区间映射到字节偏移。
// 借助 byteAt 稀疏采样，起点定位最多扫 runeSampleStep 个 rune，而非整个叶子。
func runeRangeToBytes(n *node, start, end int) (int, int) {
	s := n.text
	bs, bi := 0, 0
	if n.nByte > 0 {
		k := start / runeSampleStep
		if k >= int(n.nByte) {
			k = int(n.nByte) - 1
		}
		bs = int(n.byteAt[k])
		bi = k * runeSampleStep
	}
	for bi < start && bs < len(s) {
		_, sz := utf8.DecodeRuneInString(s[bs:])
		bs += sz
		bi++
	}
	be := bs
	for bi < end && be < len(s) {
		_, sz := utf8.DecodeRuneInString(s[be:])
		be += sz
		bi++
	}
	return bs, be
}

// RuneAt 返回第 i 个 rune；越界返回 0, false。O(log N)。
func (r *Rope) RuneAt(i int) (rune, bool) {
	if r == nil || r.root == nil || i < 0 || i >= r.root.runes {
		return 0, false
	}
	return runeAt(r.root, i), true
}

func runeAt(n *node, i int) rune {
	for n.left != nil {
		if i < n.left.runes {
			n = n.left
		} else {
			i -= n.left.runes
			n = n.right
		}
	}
	bs, _ := runeRangeToBytes(n, i, i+1)
	ru, _ := utf8.DecodeRuneInString(n.text[bs:])
	return ru
}

// LineStart 返回第 line 行首个 rune 的偏移，即"跨过 line 个 '\n' 之后"的位置。
// 越界（line >= LineCount）返回文本末尾。O(log N)。
func (r *Rope) LineStart(line int) int {
	if r == nil || r.root == nil || line <= 0 {
		return 0
	}
	if line >= r.root.lines+1 {
		return r.root.runes
	}
	return lineStart(r.root, line)
}

// lineStart 在子树 n 内返回跨过 k 个换行后的 rune 偏移。
func lineStart(n *node, k int) int {
	if k <= 0 || n == nil {
		return 0
	}
	if n.left == nil {
		return leafAfterNewlines(n.text, k)
	}
	if n.left.lines >= k {
		return lineStart(n.left, k)
	}
	return n.left.runes + lineStart(n.right, k-n.left.lines)
}

// leafAfterNewlines 返回片段内跨过 k 个 '\n' 后的 rune 偏移。换行不足时返回整片 rune 数。
func leafAfterNewlines(s string, k int) int {
	ru, seen := 0, 0
	for _, ch := range s {
		ru++
		if ch == '\n' {
			seen++
			if seen == k {
				return ru
			}
		}
	}
	return ru
}

// Line 返回第 line 行文本（不含换行符）。越界返回空串。O(log N + 行长度)。
func (r *Rope) Line(line int) string {
	if r == nil || r.root == nil || line < 0 || line >= r.LineCount() {
		return ""
	}
	start := r.LineStart(line)
	if line == r.LineCount()-1 {
		// 末行无换行符，直接取到文本末尾（不能用 LineStart(line+1)-1，会漏尾字符）。
		return r.Slice(start, r.root.runes)
	}
	end := r.LineStart(line+1) - 1 // 跳过本行末尾的 '\n'
	return r.Slice(start, end)
}

// Lines 返回所有行（不含换行符）。O(N)：一次中序遍历，复用一个字节缓冲，
// 不再逐行重复下降（原实现每行 2 次 LineStart + 1 次 Slice = O(L·log N)）。
// 缓冲只分配一次（strings.Builder.Reset 会丢弃 buffer，故用 []byte + string 转换）。
func (r *Rope) Lines() []string {
	if r == nil || r.root == nil {
		return []string{""}
	}
	out := make([]string, 0, r.root.lines+1)
	// 预分配：平均每行的字节长度，避免缓冲反复扩容。
	avgLine := r.root.bytes / (r.root.lines + 1)
	buf := make([]byte, 0, avgLine+16)
	buf = collectLines(r.root, buf, &out)
	// 末尾无论是否有 '\n'，都要收尾最后一行（以 '\n' 结尾时是空串）。
	out = append(out, string(buf))
	return out
}

func collectLines(n *node, buf []byte, out *[]string) []byte {
	if n == nil {
		return buf
	}
	if n.left == nil {
		for i := 0; i < len(n.text); {
			ch, sz := utf8.DecodeRuneInString(n.text[i:])
			if ch == '\n' {
				*out = append(*out, string(buf))
				buf = buf[:0]
			} else {
				buf = append(buf, n.text[i:i+sz]...)
			}
			i += sz
		}
		return buf
	}
	buf = collectLines(n.left, buf, out)
	return collectLines(n.right, buf, out)
}

// OffsetToLineCol 把 rune 偏移换算成 (行, 列)。O(log N)：单次树下降同时得出行列，
// 借助 tailLineRunes 聚合在进入右子树时直接得到列基线，无需再下降一次 LineStart。
// 越界偏移夹紧到 [0, RuneLen]。
func (r *Rope) OffsetToLineCol(off int) (int, int) {
	if r == nil || r.root == nil {
		return 0, 0
	}
	if off < 0 {
		off = 0
	}
	if off > r.root.runes {
		off = r.root.runes
	}
	st := lcState{}
	offsetToLineCol(r.root, off, &st)
	return st.line, st.col
}

type lcState struct{ line, col int }

func offsetToLineCol(n *node, off int, st *lcState) {
	if n == nil || off <= 0 {
		return
	}
	if n.left == nil {
		// 叶子内逐 rune：遇 '\n' 则行+1、列归零，否则列+1。
		// 从上层继承的 st.col 是进入本子树时当前行已有的列数。
		ru := 0
		for _, ch := range n.text {
			if ru >= off {
				break
			}
			if ch == '\n' {
				st.line++
				st.col = 0
			} else {
				st.col++
			}
			ru++
		}
		return
	}
	lr := n.left.runes
	if off <= lr {
		offsetToLineCol(n.left, off, st)
	} else {
		// 左子树整体在前：累加其换行数；列基线 = 左子树末尾行的长度。
		// 若左子树无换行，当前行延续，列加上其 rune 数；否则重置为末尾行长度。
		st.line += n.left.lines
		if n.left.lines > 0 {
			st.col = n.left.tailLineRunes
		} else {
			st.col += n.left.runes
		}
		offsetToLineCol(n.right, off-lr, st)
	}
}

// LineColToOffset 把 (行, 列) 换算成 rune 偏移。列按 rune 计。
// 行越界夹紧到最后一行；列越界夹紧到行尾。O(log N)。
func (r *Rope) LineColToOffset(line, col int) int {
	if r == nil || r.root == nil {
		return 0
	}
	lc := r.LineCount()
	if line < 0 {
		line = 0
	}
	if line >= lc {
		line = lc - 1
	}
	start := r.LineStart(line)
	lineEnd := r.root.runes
	if line < lc-1 {
		lineEnd = r.LineStart(line+1) - 1 // 下一行起点前一个是本行 '\n'
		if lineEnd < start {
			lineEnd = start
		}
	}
	off := start + col
	if off > lineEnd {
		off = lineEnd
	}
	if off < start {
		off = start
	}
	return off
}

// Insert 在 rune 偏移 off 处插入 ins，返回新 Rope（原值不变）。O(log N + |ins|)。
func (r *Rope) Insert(off int, ins string) *Rope {
	if ins == "" || r == nil {
		return r
	}
	if r.root == nil {
		return Of(ins)
	}
	n := r.root
	if off < 0 {
		off = 0
	}
	if off > n.runes {
		off = n.runes
	}
	left, right := split(n, off)
	// 快路径：单 rune 插入直接构造叶子，跳过 Of 的切片分配与二分归并。
	var mid *node
	if _, sz := utf8.DecodeRuneInString(ins); sz == len(ins) {
		mid = leaf(ins)
	} else {
		mid = Of(ins).root
	}
	return &Rope{root: join(join(left, mid), right)}
}

// Delete 删除 [off, off+count) rune 区间，返回新 Rope（原值不变）。O(log N)。
// 越界按最近合法边界夹紧；count<=0 返回原值。
func (r *Rope) Delete(off, count int) *Rope {
	if r == nil || r.root == nil || count <= 0 {
		return r
	}
	n := r.root
	if off < 0 {
		off = 0
	}
	if off >= n.runes {
		return r
	}
	if count > n.runes-off {
		count = n.runes - off
	}
	left, rest := split(n, off)
	_, right := split(rest, count)
	return &Rope{root: join(left, right)}
}

// split 在 rune 偏移 off 处把树切成左右两棵。O(log N)。
func split(n *node, off int) (*node, *node) {
	if n == nil {
		return nil, nil
	}
	if off <= 0 {
		return nil, n
	}
	if off >= n.runes {
		return n, nil
	}
	if n.left == nil {
		bs, _ := runeRangeToBytes(n, off, n.runes)
		return leaf(n.text[:bs]), leaf(n.text[bs:])
	}
	lr := n.left.runes
	if off < lr {
		a, b := split(n.left, off)
		// 用 join 而非单层 rebalance(branch(...))：b 与 n.right 高度差可能很大，
		// 单层旋转不足。join 会沿高度差递归下降并逐层再平衡。
		return a, join(b, n.right)
	}
	a, b := split(n.right, off-lr)
	return join(n.left, a), b
}

// join 把两棵子树拼成平衡树。O(|h1-h2|)。
func join(l, r *node) *node {
	if l == nil {
		return r
	}
	if r == nil {
		return l
	}
	if l.height > r.height+1 {
		return rebalance(branch(l.left, join(l.right, r)))
	}
	if r.height > l.height+1 {
		return rebalance(branch(join(l, r.left), r.right))
	}
	return branch(l, r)
}

// rebalance 对暂态失衡节点做 AVL 旋转，返回平衡根。
// 在 join 中，参与 rebalance 的两子树高度差至多为 2（join 的递归保证
// 高层返回的子树高度介于较低侧与其 +1 之间），故单层旋转足够。
func rebalance(n *node) *node {
	if n == nil || n.left == nil || n.right == nil {
		return n
	}
	hl, hr := n.left.height, n.right.height
	if hl > hr+1 {
		l := n.left
		if height(l.left) >= height(l.right) {
			return branch(l.left, branch(l.right, n.right)) // LL 右旋
		}
		return branch(branch(l.left, l.right.left), branch(l.right.right, n.right)) // LR
	}
	if hr > hl+1 {
		r := n.right
		if height(r.right) >= height(r.left) {
			return branch(branch(n.left, r.left), r.right) // RR 左旋
		}
		return branch(branch(n.left, r.left.left), branch(r.left.right, r.right)) // RL
	}
	return n
}
