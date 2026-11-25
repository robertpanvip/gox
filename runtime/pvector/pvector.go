// Package pvector 提供不可变持久化向量（persistent vector），采用 32 叉
// bitmapped vector trie + tail 结构（Clojure PersistentVector 的经典布局）。
//
// 与 GX 的关系：
//   - 它是 signal.Vec 在「大数据量」时的底层值载体，与 Rope 同源（不可变、
//     结构共享、零值可用、单线程），但服务的是「按下标的通用列表」而非文本。
//   - 结构共享让「改一个元素」只复制根→叶的一条路径（O(log32 N)，100 万元素
//     仅 4 层），其余节点与旧版本共享。因此旧版本指针可直接当撤销栈快照，
//     内存 O(1) 而非 O(N)。
//   - 每次修改必然产生新根指针，故 *PVector 的相等判断是 O(1) 指针比较——
//     契合 signal 的「新值必不同」语义。
//
// 复杂度：Len O(1)；Get/Set O(log32 N)；Append 均摊 O(1)；Values O(N)。
// 全部操作不可变（返回新值，输入不变），非线程安全（UI Runtime 单线程持有）。
package pvector

const (
	bits  = 5         // 每层分 5 bit
	width = 1 << bits // 分支因子 32
	mask  = width - 1 // 31
)

// PVector[T] 是不可变持久化向量。零值代表空向量，可直接用。
type PVector[T any] struct {
	cnt   int      // 元素总数
	shift int      // trie 根节点位移 = 层数 * bits
	root  *node[T] // trie 根；空向量为 nil
	tail  []T      // 末尾不足一整块的元素（长度 0..width）
}

type node[T any] struct {
	kids []*node[T] // 内部节点：子节点；叶子为 nil
	vals []T        // 叶子节点：元素值（长度 width）；内部为 nil
}

func (n *node[T]) leaf() bool { return n != nil && len(n.kids) == 0 }

// Of 从切片构造一个 PVector。O(N) 次均摊 Append。
func Of[T any](vals []T) PVector[T] {
	v := PVector[T]{}
	for _, x := range vals {
		v = v.Append(x)
	}
	return v
}

// Len 返回元素个数。O(1)。
func (v PVector[T]) Len() int { return v.cnt }

// tailOff 返回 tail 在逻辑下标空间中的起始位置。
func (v PVector[T]) tailOff() int {
	if v.cnt < width {
		return 0
	}
	return ((v.cnt - 1) >> bits) << bits
}

// Get 返回第 i 个元素。越界 panic。O(log32 N)。
func (v PVector[T]) Get(i int) T {
	if i < 0 || i >= v.cnt {
		panic("pvector: index out of range")
	}
	if i >= v.tailOff() {
		return v.tail[i-v.tailOff()]
	}
	n := v.root
	for s := v.shift; s > 0; s -= bits {
		n = n.kids[(i>>s)&mask]
	}
	return n.vals[i&mask]
}

// Set 返回「第 i 个元素改为 val」的新向量，原向量不变。越界 panic。O(log32 N)。
// 只复制根→叶路径上的节点，其余结构与原向量共享。
func (v PVector[T]) Set(i int, val T) PVector[T] {
	if i < 0 || i >= v.cnt {
		panic("pvector: index out of range")
	}
	if i >= v.tailOff() {
		nt := make([]T, len(v.tail))
		copy(nt, v.tail)
		nt[i-v.tailOff()] = val
		return PVector[T]{cnt: v.cnt, shift: v.shift, root: v.root, tail: nt}
	}
	return PVector[T]{cnt: v.cnt, shift: v.shift, root: assoc(v.shift, v.root, i, val), tail: v.tail}
}

// assoc 在 trie 的第 s 层替换下标 i 所在叶子中的值，返回新节点。
func assoc[T any](s int, n *node[T], i int, val T) *node[T] {
	if s == 0 {
		vals := make([]T, width)
		copy(vals, n.vals)
		vals[i&mask] = val
		return &node[T]{vals: vals}
	}
	sub := (i >> s) & mask
	kids := make([]*node[T], width)
	copy(kids, n.kids)
	kids[sub] = assoc(s-bits, n.kids[sub], i, val)
	return &node[T]{kids: kids}
}

// Append 返回末尾追加 val 的新向量，原向量不变。均摊 O(1)。
// tail 未满时只复制 tail；tail 满时把整块推入 trie（路径复制）。
func (v PVector[T]) Append(val T) PVector[T] {
	if v.cnt-v.tailOff() < width {
		nt := make([]T, len(v.tail)+1)
		copy(nt, v.tail)
		nt[len(v.tail)] = val
		return PVector[T]{cnt: v.cnt + 1, shift: v.shift, root: v.root, tail: nt}
	}
	// tail 已满：整块成为新的叶子节点推入 trie，val 成为新 tail 的首元素。
	tailNode := &node[T]{vals: make([]T, width)}
	copy(tailNode.vals, v.tail)
	cnt := v.cnt + 1
	root, shift := v.root, v.shift
	switch {
	case shift == 0:
		// 第一次 tail 满：建立根内部节点层（shift=5），tailNode 落在 kids[0]。
		// 零值 shift=0 时不能直接 pushTail——那会把叶子误当内部节点的槽位。
		kids := make([]*node[T], width)
		kids[0] = tailNode
		root = &node[T]{kids: kids}
		shift = bits
	case (v.cnt >> bits) > (1 << shift):
		// 根需要提升一层：旧根成 kids[0]，新叶路径成 kids[1]。
		kids := make([]*node[T], width)
		kids[0] = root
		kids[1] = newPath(shift, tailNode)
		root = &node[T]{kids: kids}
		shift += bits
	default:
		// 注意：pushTail 必须用追加前的旧 cnt 定位槽位。旧 cnt-1 正是刚
		// 被推出 tail 的最后一个元素的下标，它决定新叶子落在 trie 的哪一格。
		root = pushTail(shift, v.cnt, root, tailNode)
	}
	return PVector[T]{cnt: cnt, shift: shift, root: root, tail: []T{val}}
}

// pushTail 把 tailNode 放到 trie 中逻辑下标 (cnt-1) 对应的位置，返回新根节点。
func pushTail[T any](s, cnt int, parent, tailNode *node[T]) *node[T] {
	kids := make([]*node[T], width)
	if parent != nil {
		copy(kids, parent.kids)
	}
	sub := ((cnt - 1) >> s) & mask
	if s == bits {
		kids[sub] = tailNode
	} else {
		var child *node[T]
		if parent != nil {
			child = parent.kids[sub]
		}
		if child == nil {
			kids[sub] = newPath(s-bits, tailNode)
		} else {
			kids[sub] = pushTail(s-bits, cnt, child, tailNode)
		}
	}
	return &node[T]{kids: kids}
}

// newPath 从 s 层构造一条只含首槽的路径，末端是 tailNode。
func newPath[T any](s int, tailNode *node[T]) *node[T] {
	if s == 0 {
		return tailNode
	}
	n := &node[T]{kids: make([]*node[T], width)}
	n.kids[0] = newPath(s-bits, tailNode)
	return n
}

// Values 返回全部元素的切片快照。O(N)。
func (v PVector[T]) Values() []T {
	out := make([]T, v.cnt)
	for i := range out {
		out[i] = v.Get(i)
	}
	return out
}
