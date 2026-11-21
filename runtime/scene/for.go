package gxscene

import (
	"fmt"
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

	// 新增或复用条目。
	for _, e := range newList {
		if entry, ok := f.entries[e.key]; ok {
			entry.value = e.value // 复用：更新值（子树内部 signal 自己更新，不重建）
			continue
		}
		// 列表项 ID 按 key 派生（非数组下标），重排/增删时同 key 项 ID 不变。
		itemID := "r/[" + fmt.Sprint(e.key) + "]"
		entry := &forEntry{key: e.key, value: e.value, node: &Node{Kind: "#frag", Props: map[string]any{}, ID: itemID, dirty: true}}
		f.entries[e.key] = entry
		// 新 Owner 下构建子树并接线；子树内的 Effect 归属该 Owner，删除时级联清理。
		entry.owner = signal.CreateRoot(func(o *signal.Owner) {
			entry.inst = f.render(e.value)
			if entry.inst != nil {
				if f.sink != nil {
					entry.inst.changes = f.sink // 共享所属实例树的变更缓冲
				}
				entry.inst.BindReactive(signal.Effect)
				for _, c := range entry.inst.root.Children {
					c.parent = entry.node
					entry.node.Children = append(entry.node.Children, c)
				}
			}
		})
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
	root := &Node{Kind: "#root", Props: map[string]any{}, ID: "r", dirty: true}
	for _, c := range children {
		c.parent = root
		root.Children = append(root.Children, c)
	}
	return &Instance{root: root}
}

// adoptSink 实现 subtreeController：记录所属实例树的变更缓冲，传给各列表项子实例。
func (f *For) adoptSink(s *changeSink) { f.sink = s }

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
