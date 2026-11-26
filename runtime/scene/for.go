package gxscene

import (
	"sync"
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
//
// 两种 render 模式：
//   - 值快照模式（render）：render 收到元素值，子树 getter 捕获该值。同 key 值变化
//     必须销毁 Owner、重建整棵子树（并补发 ReplaceChildren）——兼容旧编译器与手写调用。
//   - 信号化模式（sigRender，item signal 根治）：render 收到项的 getter（读 item signal），
//     子树 getter 通过它读值并登记依赖。同 key 值变化只写 item signal，依赖它的叶子
//     effect 精确重跑 → 只发 SetText/SetAttr，不重建、不换 ID、不发 ReplaceChildren。
type For struct {
	mu        sync.Mutex
	each      func() []any
	key       func(any) any
	render    func(any) *Instance        // 值快照模式（旧）
	sigRender func(func() any) *Instance // 信号化模式（新）；非 nil 时优先于 render

	entries  map[any]*forEntry // key -> 元素条目
	order    []any             // 当前 key 顺序
	disposed bool

	// lastRoot 缓存上次 Resolve 构造的根实例。列表长度/顺序/每项值都未变时直接复用，
	// 避免每次 each 依赖 signal 写入都重建 order/children 切片与 root Instance。
	lastRoot *Instance

	// indexKey 为 true 时用元素下标作 key（未提供 key 函数时）。
	indexKey bool
	sink     *changeSink // 所属实例树的变更缓冲，传给各列表项子实例
	bctx     *bindCtx    // 统一接线上下文，传给新建的列表项子树
	scope    Owner       // 建树期捕获的 Owner；列表项重建时恢复它以继承 Context
}

type forEntry struct {
	key   any
	value any
	inst  *Instance
	owner Owner  // 该项的响应式作用域（抽象，默认由 signal 实现）
	node  *Node  // 该元素在场景树中的包装节点（透明 #frag）
	item  Signal // 信号化模式：该项的 item signal，子树 getter 经它读值并登记依赖
}

// dupKey 是重复 key 的唯一化包装：保留原 key 与出现序号，使不同元素即使业务 key
// 相同也有独立身份。Go 的 map[any] 按动态类型+值区分，dupKey 结构体与任何原始 key
// 类型都不相等，故不会与正常 key 冲突。
type dupKey struct {
	key any
	seq int
}

// keysForItems 计算每个元素的最终 key（含重复 key 唯一化），并返回是否存在重复。
// 短路检测与完整 diff 共用它，保证两条路径对 key 的判定一致。
func (f *For) keysForItems(items []any) (keys []any, hasDup bool) {
	keys = make([]any, len(items))
	seen := make(map[any]int, len(items))
	for i, item := range items {
		k := item
		if f.indexKey {
			k = i
		} else if f.key != nil {
			k = f.key(item)
		}
		if n, dup := seen[k]; dup {
			seen[k] = n + 1
			keys[i] = dupKey{key: k, seq: n + 1}
			hasDup = true
		} else {
			seen[k] = 1
			keys[i] = k
		}
	}
	return
}

// NewFor 创建 keyed 列表控制器（值快照模式）。
//   - each: 返回当前元素列表；在其中读 signal 会被自动追踪。
//   - key: 为元素生成稳定键；nil 时用下标（仅适用于纯追加/尾部删除场景）。
//   - render: 为单个元素构建子树；在新 Owner 下运行。
//
// Deprecated: 同 key 值变化时此模式需销毁并重建整项子树（补发 ReplaceChildren、
// 用 rebaseTreeIDs 兜底项内 ID），开销高且 rebase 不覆盖嵌套控制器。新代码请用
// NewForSignal（item signal：值变化只更新叶子、ID 天然稳定）。此 API 保留以兼容
// 手写调用与编译器逃逸回退，将在后续 breaking 版本移除。
func NewFor(each func() []any, key func(any) any, render func(any) *Instance) *For {
	return &For{
		each:     each,
		key:      key,
		render:   render,
		entries:  make(map[any]*forEntry),
		indexKey: key == nil,
		scope:    Reactive().CurrentOwner(),
	}
}

// NewForSignal 创建信号化 keyed 列表控制器（item signal 根治模式）。
//   - each: 返回当前元素列表；在其中读 signal 会被自动追踪。
//   - key: 为元素生成稳定键；nil 时用下标。
//   - render: 为单个元素构建子树；参数是该元素的值 getter（读 item signal）。
//     子树里的动态 getter 应通过它读值，从而登记依赖——值变化时只更新受影响的叶子。
//
// 与 NewFor 的区别：同 key 值变化时只写 item signal，不销毁重建、不发 ReplaceChildren。
func NewForSignal(each func() []any, key func(any) any, render func(func() any) *Instance) *For {
	return &For{
		each:      each,
		key:       key,
		sigRender: render,
		entries:   make(map[any]*forEntry),
		indexKey:  key == nil,
		scope:     Reactive().CurrentOwner(),
	}
}

// ForGetterSignal 是信号化模式的工厂：捕获稳定的 *For，返回 getter 闭包。
func ForGetterSignal(each func() []any, key func(any) any, render func(func() any) *Instance) func() any {
	f := NewForSignal(each, key, render)
	return func() any { return f }
}

// keyAny 把强类型 key func(T) any 适配成 For 需要的 func(any) any。
// 关键：key == nil（省略 key）时必须返回 nil 而不是一个恒返回 nil 的闭包——后者
// 非 nil，会让 NewFor / NewForSignal 的 indexKey 判定失效，导致所有元素 key 坍缩
// 成 nil，列表塌成一项；配合 BindReactive 时每次 Resolve 的短路比对永远失败 →
// 每帧全量重建 → effect 重跑 → 死循环（第十轮 P1-1）。保持 nil 才能让下标 key 生效。
func keyAny[T any](key func(T) any) func(any) any {
	if key == nil {
		return nil
	}
	return func(v any) any {
		t, ok := v.(T)
		if !ok {
			return nil
		}
		return key(t)
	}
}

// ForGetterOfSignal 是带显式元素类型的信号化工广，供编译器生成的带类型 lambda 使用。
// render 的签名是 func(func(T) *Instance)：参数是元素值的 getter，子树 getter 经它读值。
// 泛型在编译期保留 T 的类型安全，内部擦除成 any 复用同一套 keyed 机制。
func ForGetterOfSignal[T any](each func() []T, key func(T) any, render func(func() T) *Instance) func() any {
	f := NewForSignal(
		func() []any {
			src := each()
			out := make([]any, len(src))
			for i, x := range src {
				out[i] = x
			}
			return out
		},
		keyAny(key),
		func(get func() any) *Instance {
			return render(func() T {
				v := get()
				t, ok := v.(T)
				if !ok {
					var zero T
					return zero
				}
				return t
			})
		},
	)
	return func() any { return f }
}

// ForGetter 是值快照模式的工厂：捕获稳定的 *For，返回 getter 闭包。
// getter 每次返回同一对象；列表的求值在 Resolve 内完成（在 effect 上下文登记依赖）。
//
// Deprecated: 请用 ForGetterSignal（item signal 根治模式）。
func ForGetter(each func() []any, key func(any) any, render func(any) *Instance) func() any {
	f := NewFor(each, key, render)
	return func() any { return f }
}

// ForGetterOf 是带显式元素类型的泛型工厂（值快照模式），供编译器逃逸回退使用。
//   - each: 返回 []T；
//   - key: func(T) any，可传 nil 用下标；
//   - render: func(T) *Instance。
//
// 泛型在编译期保留 T 的类型安全，内部擦除成 any 复用同一套 keyed 机制。
//
// Deprecated: 值快照模式同 key 值变化需重建整项子树。编译器仅在 item 逃逸到组件
// props 等无法用 item signal 的场景回退到此工厂；常规场景请用 ForGetterOfSignal。
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
		keyAny(key),
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

	type kv struct {
		key   any
		value any
	}

	// 计算最终 keys（含重复 key 唯一化）；短路与完整 diff 共用，避免判定不一致。
	keys, _ := f.keysForItems(items)

	// 短路：先比长度，再直接遍历 items 比对 key 顺序与每项值，全程不构造 newList。
	// 常见场景是"别的 signal 变了触发 each 重算，但列表本身没变"——命中即复用
	// 上次 root，省掉 make([]kv)、order/children 重建与 root Instance 分配。
	// 只有检测失败（列表确实变了）才构造 newList 走完整 diff 路径。
	if f.lastRoot != nil && len(items) == len(f.order) {
		unchanged := true
		for i, item := range items {
			k := keys[i]
			if !equalValue(f.order[i], k) {
				unchanged = false
				break
			}
			entry, ok := f.entries[k]
			if !ok || !equalValue(entry.value, item) {
				unchanged = false
				break
			}
		}
		if unchanged {
			return f.lastRoot
		}
	}

	// 复用上面算好的 keys 装配 newList（重复 key 已唯一化，每项独立身份）。
	newList := make([]kv, len(items))
	for i, item := range items {
		newList[i] = kv{key: keys[i], value: item}
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

	// 新增 / 复用 / 同 key 值变化。
	for _, e := range newList {
		if entry, ok := f.entries[e.key]; ok {
			if equalValue(entry.value, e.value) {
				continue // 同 key 同值：节点与子树原样复用（重排只调顺序）。
			}
			if f.sigRender != nil {
				// 信号化根治：同 key 值变化只写 item signal。子树 getter 在读 item()
				// 时已登记依赖，写入会精确重跑那些叶子的 effect → 只发 SetText/SetAttr。
				// 不销毁 Owner、不重建子树、ID 天然稳定、无需补发 ReplaceChildren。
				entry.value = e.value
				if entry.item != nil {
					entry.item(e.value)
				}
				continue
			}
			// 值快照模式：同 key 但值变了，按值捕获的 render 不会自己感知新值，
			// 必须销毁旧 Owner、重建该项子树，否则界面永远显示旧值（报告 P1）。
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
				f.sink.add(Change{Kind: ChangeReplaceChildren, NodeID: nodeID(entry.node)})
			}
			continue
		}
		// 列表项包装节点分配独立稳定 ID：同 key 的 entry 复用时 node 不变、ID 也不变，
		// 因此重排/增删只动顺序、身份保持；不同列表各节点由全局分配器给号，天然不撞。
		itemID := nextNodeID()
		// Type 必须为 NodeFragment：零值是 NodeText，会让后端把透明包装当空文本跳过，
		// 导致整棵列表项子树不被绘制（Kind="#frag" 只标 Kind，不决定渲染语义）。
		entry := &forEntry{key: e.key, value: e.value, node: &Node{Kind: "#frag", Type: NodeFragment, Props: map[string]any{}, ID: itemID, dirty: true}}
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
	root := &Node{Kind: "#root", Type: NodeFragment, Props: map[string]any{}, ID: nextNodeID(), dirty: true}
	for _, c := range children {
		c.parent = root
		root.Children = append(root.Children, c)
	}
	inst := &Instance{root: root}
	f.lastRoot = inst
	return inst
}

// buildEntry 在新 Owner 下为条目构建子树并接线，把子树 children 挂到 entry.node。
// 新条目与同 key 值变化重建都走这里；调用方负责先 Dispose 旧 Owner、清空 node.Children。
func (f *For) buildEntry(entry *forEntry) {
	// 重建（值快照模式同 key 值变化）时保留旧子树的 ID：render 每次都分配全新整数，
	// 若直接采用会让项内所有节点 ID 变化，破坏"跨帧稳定"，后端按 ID 缓存的 rect/
	// 焦点/滚动状态会因一次文本更新而全部失效。buildEntry 结束后按结构位置把旧 ID
	// 复制回新树（见下方 rebaseTreeIDs），使项内身份与重建前一致。
	var oldRoot *Node
	if entry.inst != nil {
		oldRoot = entry.inst.root
	}
	entry.node.Children = nil
	// 信号化模式：item signal 在 Owner 外创建（纯信号，不挂 Effect），首次构建时
	// 初始化一次；同 key 值变化走 Resolve 里写 signal 的路径，不再进 buildEntry。
	if f.sigRender != nil && entry.item == nil {
		entry.item = Reactive().NewSignal(entry.value)
	}
	entry.owner = Reactive().Root(func(o Owner) {
		build := func() {
			if f.sigRender != nil {
				entry.inst = f.sigRender(func() any { return entry.item() })
			} else {
				entry.inst = f.render(entry.value)
			}
		}
		// 恢复建树期捕获的 Owner：列表项 render 里的组件函数 / Context.Use
		// 借此沿正确 parent 链找到 Provider（含列表整体增删项后的重建）。
		if f.scope != nil {
			Reactive().RunWithOwner(f.scope, build)
		} else {
			build()
		}
		if entry.inst == nil {
			return
		}
		if oldRoot != nil {
			rebaseTreeIDs(entry.inst.root, oldRoot)
		}
		if f.sink != nil {
			// 递归共享所属实例树的变更缓冲：子树 Render 时内部组件/嵌套控制器
			// 已 adopt 到创建期临时 sink，这里必须下探覆盖，否则项内组件变更丢失。
			adoptSinkDeep(entry.inst, f.sink)
		}
		// 叶子与结构接线互斥，避免项内 slotBranch 被双订阅（selector 调用两次）：
		// 有统一 ctx（app 场景）时，叶子用 bindLeaves、结构用 bindRec 继承 ctx；
		// 无 ctx（纯 scene）时，BindReactive 一并接叶子与结构。
		if f.bctx != nil {
			// 构建列表项子树期间静音：项内含 if 时，bindRec 建立的分支 effect 首次
			// 回调会调 onDirty；批量新增 N 项就是 N 帧。静音后由 each 的外层 effect
			// 统一画 1 帧。
			f.bctx.withSilent(func() {
				// 用 ctx 注入的 effect（app 场景为带 UI-dirty 标记的包装 effect），
				// 使列表项内叶子的变化也能被 app 的帧边界统一重绘；纯 scene 场景
				// 该 effect 就是当前适配器的 Effect。
				entry.inst.bindLeaves(f.bctx.effect)
				entry.inst.bindRec(f.bctx)
			})
		} else {
			entry.inst.BindReactive(Reactive().Effect)
		}
		for _, c := range entry.inst.root.Children {
			c.parent = entry.node
			entry.node.Children = append(entry.node.Children, c)
		}
	})
}

// rebaseTreeIDs 按前序遍历把旧树 old 的节点身份复制回新树 n 的对应位置。
//
// 用于列表项「值快照模式」的同 key 重建：render 产出的新树每个节点都是新分配的
// 整数 ID，但逻辑上它是旧树同一位置的延续（同一个列表项、同一段文本）。把旧 ID
// 复制回去，后端按 ID 维护的 rect/焦点/滚动缓存才不会因一次值更新而整体失效。
//
// 按结构位置配对对嵌套控制器同样成立：子 For/Branch 的包装节点也在此树中，复制
// ID 后这些控制器持有的 entry.node.ID 已是旧值，后续 emit 的 Change 自然带旧 ID。
// 结构不同的部分（新增节点）保留新分配的 ID——本就是新身份，符合评审预期。
func rebaseTreeIDs(n, old *Node) {
	if n == nil || old == nil {
		return
	}
	n.ID = old.ID
	common := len(n.Children)
	if len(old.Children) < common {
		common = len(old.Children)
	}
	for i := 0; i < common; i++ {
		rebaseTreeIDs(n.Children[i], old.Children[i])
	}
}

// adoptSink 实现 subtreeController：记录所属实例树的变更缓冲，传给各列表项子实例。
func (f *For) adoptSink(s *changeSink) { f.sink = s }

// adoptSinkDeep 实现 deepSinkAdopter：换 sink 时递归覆盖所有已建列表项子树
// （含项内组件与嵌套控制器），保证整棵实时树的变更汇入同一 sink。
func (f *For) adoptSinkDeep(s *changeSink) {
	if f == nil {
		return
	}
	f.mu.Lock()
	f.sink = s
	entries := f.entries
	f.mu.Unlock()
	for _, e := range entries {
		adoptSinkDeep(e.inst, s)
	}
}

// adoptBind 实现 subtreeController：记录统一接线上下文，供 Resolve 构建新列表项时递归接线。
// 同时对首帧已建好的列表项补接：它们建树时还没有 ctx，用的是独立 effect（无 app
// onDirty 帧边界），不补接会导致项内叶子变化时增量后端收不到重绘请求。
func (f *For) adoptBind(ctx *bindCtx) {
	f.bctx = ctx
	f.mu.Lock()
	entries := make([]*forEntry, 0, len(f.entries))
	for _, e := range f.entries {
		entries = append(entries, e)
	}
	f.mu.Unlock()
	for _, e := range entries {
		if e.inst != nil {
			// 首帧建项时 sink 已 adopt，但项内后来挂载的嵌套组件可能漏接；
			// 补接前再深灌一次 sink，确保项内所有叶子的 Change 汇入同一缓冲。
			if f.sink != nil {
				adoptSinkDeep(e.inst, f.sink)
			}
			e.inst.rebindWith(ctx)
		}
	}
}

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
	// 销毁期异常隔离：单个 entry 的 Owner cleanup panic 不阻断其余 entry 释放
	// （entries 已摘除，中断即永久泄漏），全部释放后统一上报。
	var caught []any
	for _, e := range entries {
		if e.owner != nil {
			caught = append(caught, runFuncSafely(e.owner.Dispose)...)
		}
	}
	repanicDisposeErrors(caught)
}

// Len 当前元素数（调试/测试用）。
func (f *For) Len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.entries)
}
