package gxscene

import (
	"fmt"
	"strings"
	"sync"

	"github.com/robertpanvip/gox/runtime/signal"
)

// For 是 keyed 列表控制器，对应评审里建议的 <For each={xs} key={...}>{render}</For>。
//
// 语义（对齐 SolidJS <For>）：
//   - each 在「当前响应式 Effect 上下文」中求值，读到的 signal 被登记为依赖；
//   - key(item) 为每个元素生成稳定键；相同键的节点在列表变化时被复用（移动而非重建）；
//   - 新增元素：在新 Owner 下运行 render 构建子树并接线；
//   - 删除元素：销毁其 Owner（级联停止 Effect、执行 Cleanup），移除场景节点；
//   - 重排：只调整节点顺序，不重建、不重新运行 render。
//
// 每个元素拥有独立 Owner，这是条件分支/列表元素能精确释放副作用的基础。
type For struct {
	mu     sync.Mutex
	each   func() []any
	key    func(any) any
	render func(any) *Instance

	entries  map[any]*forEntry // key -> 元素条目
	order    []any             // 当前 key 顺序
	disposed bool

	// indexKey 为 true 时用元素下标作 key（未提供 key 函数时）。
	indexKey bool
	sink     *changeSink // 所属实例树的变更缓冲，传给各列表项子实例
	bctx     *bindCtx    // 统一接线上下文，传给新建的列表项子树
	idBase   string      // 本列表专属 ID 前缀；项节点 ID = idBase + "/[" + key + "]"，跨列表唯一
}

type forEntry struct {
	key   any
	value any
	inst  *Instance
	owner *signal.Owner
	node  *Node // 该元素在场景树中的包装节点（透明 #frag）
}

// NewFor 创建 keyed 列表控制器。
//   - each: 返回当前元素列表；在其中读 signal 会被自动追踪。
//   - key: 为元素生成稳定键；nil 时用下标（仅适用于纯追加/尾部删除场景）。
//   - render: 为单个元素构建子树；在新 Owner 下运行。
func NewFor(each func() []any, key func(any) any, render func(any) *Instance) *For {
	return &For{
		each:     each,
		key:      key,
		render:   render,
		entries:  make(map[any]*forEntry),
		indexKey: key == nil,
		idBase:   nextIDBase(),
	}
}

// ForGetter 是编译器生成 <For> 时使用的工厂：捕获稳定的 *For，返回 getter 闭包。
// getter 每次返回同一对象；列表的求值在 Resolve 内完成（在 effect 上下文登记依赖）。
func ForGetter(each func() []any, key func(any) any, render func(any) *Instance) func() any {
	f := NewFor(each, key, render)
	return func() any { return f }
}

// ForGetterOf 是带显式元素类型的泛型工厂，供编译器生成的带类型 lambda 使用。
//   - each: 返回 []T；
//   - key: func(T) any，可传 nil 用下标；
//   - render: func(T) *Instance。
//
// 泛型在编译期保留 T 的类型安全，内部擦除成 any 复用同一套 keyed 机制。
func ForGetterOf[T any](each func() []T, key func(T) any, render func(T) *Instance) func() any {
	f := NewFor(
		func() []any {
			src := each()
			out := make([]any, len(src))
			for i, x := range src {
				out[i] = x
			}
			return out
		},
		func(v any) any {
			if key == nil {
				return nil
			}
			t, ok := v.(T)
			if !ok {
				return nil
			}
			return key(t)
		},
		func(v any) *Instance {
			t, ok := v.(T)
			if !ok {
				return nil
			}
			return render(t)
		},
	)
	return func() any { return f }
}

// Resolve 在响应式 Effect 上下文中被调用：求值 each（登记依赖），做 keyed diff，
// 返回嫁接用的根子树（把所有元素节点挂在 #frag 容器下）。
func (f *For) Resolve() *Instance {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	if f.disposed {
		f.mu.Unlock()
		return nil
	}
	items := f.each()
	f.mu.Unlock()

	// 计算新 keys 与 values。
	type kv struct {
		key   any
		value any
	}
	newList := make([]kv, len(items))
	for i, item := range items {
		k := item
		if f.indexKey {
			k = i
		} else if f.key != nil {
			k = f.key(item)
		}
		newList[i] = kv{key: k, value: item}
	}

	// 标记新 keys 集合。
	keep := make(map[any]bool, len(newList))
	for _, e := range newList {
		keep[e.key] = true
	}

	// 删除不在新列表的条目：销毁 Owner + 移除节点。
	for k, entry := range f.entries {
		if !keep[k] {
			if entry.owner != nil {
				entry.owner.Dispose()
			}
			delete(f.entries, k)
		}
	}

	// 新增 / 复用 / 同 key 值变化重建条目。
	for _, e := range newList {
		if entry, ok := f.entries[e.key]; ok {
			if equalValue(entry.value, e.value) {
				continue // 同 key 同值：节点与子树原样复用（重排只调顺序）。
			}
			// 同 key 但值变了：按值捕获的 render 不会自己感知新值，必须销毁旧
			// Owner、重建该项子树，否则界面永远显示旧值（报告 P1）。
			if entry.owner != nil {
				entry.owner.Dispose()
			}
			entry.value = e.value
			f.buildEntry(entry)
			// entry.node 指针未变，父级 diff 用指针身份比较不会发指令（内容重建
			// 发生在该节点内部）。补发一条 ReplaceChildren，让按补丁更新的后端
			// 重读该节点的 children——否则只显示旧内容的镜像树会漏掉这一项。
			// sink 判空后记录：For 未挂载（无 sink）时手工 Resolve 也能安全运行。
			if f.sink != nil {
				f.sink.add(Change{Kind: ChangeReplaceChildren, NodeID: entry.node.ID})
			}
			continue
		}
		// 列表项 ID 按「本列表唯一前缀 + key」派生：同 key 重排/增删时 ID 不变，
		// 不同列表即使同 key 也不撞车（各自的 idBase 不同）。
		itemID := f.idBase + "/[" + fmt.Sprint(e.key) + "]"
		entry := &forEntry{key: e.key, value: e.value, node: &Node{Kind: "#frag", Props: map[string]any{}, ID: itemID, dirty: true}}
		f.entries[e.key] = entry
		f.buildEntry(entry)
	}

	// 按新顺序重排节点（复用 Node 指针，只调整顺序）。
	f.order = make([]any, len(newList))
	children := make([]*Node, 0, len(newList))
	for i, e := range newList {
		f.order[i] = e.key
		if entry := f.entries[e.key]; entry != nil {
			children = append(children, entry.node)
		}
	}

	// 组装根子树（透明容器），供 mountBranch 类似的嫁接待用。
	root := &Node{Kind: "#root", Props: map[string]any{}, ID: f.idBase + "/root", dirty: true}
	for _, c := range children {
		c.parent = root
		root.Children = append(root.Children, c)
	}
	return &Instance{root: root}
}

// buildEntry 在新 Owner 下为条目构建子树并接线，把子树 children 挂到 entry.node。
// 新条目与同 key 值变化重建都走这里；调用方负责先 Dispose 旧 Owner、清空 node.Children。
func (f *For) buildEntry(entry *forEntry) {
	// 重建（同 key 值变化）时复用旧实例的 idBase：render 每次 Render 会分配新前缀，
	// 导致项内所有节点 ID 变化，破坏"跨帧稳定"。把新树的 ID 前缀重写回旧 base，
	// 后端按 ID 缓存的 rect/焦点/滚动状态才不会因一次文本更新而全部失效。
	oldBase := ""
	if entry.inst != nil {
		oldBase = entry.inst.idBase
	}
	entry.node.Children = nil
	entry.owner = signal.CreateRoot(func(o *signal.Owner) {
		entry.inst = f.render(entry.value)
		if entry.inst == nil {
			return
		}
		if oldBase != "" && entry.inst.idBase != oldBase {
			rebaseTreeIDs(entry.inst.root, entry.inst.idBase, oldBase)
			entry.inst.idBase = oldBase
		}
		if f.sink != nil {
			entry.inst.changes = f.sink // 共享所属实例树的变更缓冲
		}
		// 叶子与结构接线互斥，避免项内 slotBranch 被双订阅（selector 调用两次）：
		// 有统一 ctx（app 场景）时，叶子用 bindLeaves、结构用 bindRec 继承 ctx；
		// 无 ctx（纯 scene）时，BindReactive 一并接叶子与结构。
		if f.bctx != nil {
			// 构建列表项子树期间静音：项内含 if 时，bindRec 建立的分支 effect 首次
			// 回调会调 onDirty；批量新增 N 项就是 N 帧。静音后由 each 的外层 effect
			// 统一画 1 帧。
			f.bctx.withSilent(func() {
				entry.inst.bindLeaves(signal.Effect)
				entry.inst.bindRec(f.bctx)
			})
		} else {
			entry.inst.BindReactive(signal.Effect)
		}
		for _, c := range entry.inst.root.Children {
			c.parent = entry.node
			entry.node.Children = append(entry.node.Children, c)
		}
	})
}

// rebaseTreeIDs 把子树里所有以 from 为前缀的 Node.ID 改写成以 to 开头。
// 只动本实例骨架派生的 ID（"iN"、"iN/0/1"）；项内若嵌了子 For/Branch，它们的
// idBase 是独立的 nextIDBase，不以 from 开头，故不受影响。
func rebaseTreeIDs(n *Node, from, to string) {
	if n == nil || from == "" {
		return
	}
	if n.ID == from {
		n.ID = to
	} else if strings.HasPrefix(n.ID, from+"/") {
		n.ID = to + strings.TrimPrefix(n.ID, from)
	}
	for _, c := range n.Children {
		rebaseTreeIDs(c, from, to)
	}
}

// adoptSink 实现 subtreeController：记录所属实例树的变更缓冲，传给各列表项子实例。
func (f *For) adoptSink(s *changeSink) { f.sink = s }

// adoptBind 实现 subtreeController：记录统一接线上下文，供 Resolve 构建新列表项时递归接线。
func (f *For) adoptBind(ctx *bindCtx) { f.bctx = ctx }

// Dispose 销毁所有元素 Owner（幂等）。
func (f *For) Dispose() {
	if f == nil {
		return
	}
	f.mu.Lock()
	if f.disposed {
		f.mu.Unlock()
		return
	}
	f.disposed = true
	entries := f.entries
	f.entries = nil
	f.order = nil
	f.mu.Unlock()
	for _, e := range entries {
		if e.owner != nil {
			e.owner.Dispose()
		}
	}
}

// Len 当前元素数（调试/测试用）。
func (f *For) Len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.entries)
}
