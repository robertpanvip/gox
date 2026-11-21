# 响应式 UI 路线图落地说明（SolidJS 化四阶段）

本文档记录按架构评审建议，对 Gox 响应式 UI 栈所做的四个阶段改造。核心目标：
把 Gox 从「带 Signal 的 JSX 框架」推进为「具有原生细粒度响应式语义的语言」。
分层边界保持一致：**语言层识别 Signal，响应式层追踪依赖，UI 层更新节点**。

全部改动位于 `feat/scene-gxapp` 分支，工具链为 Go 1.27（泛型方法）。

---

## 阶段 1：响应式核心（`runtime/signal`）

评审反复强调「Owner + Reactive Computation 才是 SolidJS 的核心」。阶段 1 补齐了生命周期骨架。

| 能力 | 实现 |
| --- | --- |
| Signal 相等性裁剪 | `New` 默认用 `reflect.DeepEqual`，值未变不触发下游；`NewWithEquals` 自定义策略 |
| Effect 可销毁句柄 | `Effect` 返回 `Computation`（`Dispose()` 幂等：断依赖、停订阅、清清理） |
| Computed 句柄 | `ComputedWithHandle` 返回 `(Signal[T], Computation)` |
| Owner 树 | `CreateRoot` / `GetOwner` / `RunWithOwner`，父销毁级联子；嵌套 `CreateRoot` 自动挂为子 |
| 生命周期 | `OnCleanup`（重跑前 + 销毁时执行）、`OnMount`（首次挂载执行一次） |
| 批处理 | `Batch(f)` 合并同一轮 signal 写入，批次结束统一调度 Effect |

`scene.Computation` 以类型别名统一到 `signal.Computation`，使 `signal.Effect` 可直接作为
`BindReactive` 的实参（Go 函数类型要求返回类型 identical）。

---

## 阶段 2：条件分支响应式作用域（`runtime/scene` + 编译器）

**GX 语法**：

```gox
view := <div>
    if visible {
        <span>{count}</span>
    } else {
        <b>Hidden</b>
    }
</div>
```

**运行时**（`Branch` / `branch.go`）：

- `Branch.Resolve` 在当前 Effect 上下文求值 selector（`visible()` 被自动登记为依赖）；
- 命中索引变化时：销毁旧 Owner（级联停止 Effect、执行 Cleanup）→ 在新 Owner 下
  运行对应 builder 构建子树并接线；
- 命中索引不变时不重建——`count` 变化由子树自身的细粒度 Effect 处理，
  **绝不重跑根组件**；
- `BranchGetter` 工厂捕获稳定 `Branch`，避免 effect 重跑时重建状态。

**编译器**：JSX children 支持 `if / else if / else`，产出
`gxscene.OpBranch` + `gxscene.BranchGetter(selector, builders...)`。

---

## 阶段 3：Keyed For（`runtime/scene` + 编译器）

**GX 语法**（render 参数类型可省略，编译器从 `each` 自动推断元素类型）：

```gox
type User struct { id int; name string }
signal users = []User{{1, "Tom"}, {2, "Jack"}}

view := <ul>
    <For each={users} key={(u) => u.id}>
        {(u) => <li>{u.name}</li>}
    </For>
</ul>
```

> `(u)` 无类型注解时，编译器从 `each={users}` 的集合类型（`[]User`）推断元素类型
> `User`，生成 `ForGetterOfSignal[User]`。以下写法均受支持：
> - signal 变量：`signal users = []User{...}`（改写前采集声明类型）
> - 普通变量：`users := []User{...}`（查询绑定表）
> - 显式注解：`(u User) => ...` 始终优先于推断，可用于 `each` 类型无法静态确定的场景。
> `key` lambda 的参数类型同样可省略，复用 render 的元素类型。

**运行时**（`For` / `for.go`）：

- `each` 在当前 Effect 上下文求值（登记依赖）；
- keyed diff：`key(item)` 生成稳定键，相同键的节点被复用（移动而非重建）；
- 新增元素：新 Owner 下构建子树；删除元素：销毁 Owner（级联清理 Effect/Cleanup）；
- 重排：只调整节点顺序，不重跑 render；
- 每个元素独立 Owner，元素内 signal 变化只更新对应项；
- **item signal 根治**：每项持有一个 item signal，render 参数是它的 getter `func() T`。
  同 key 值变化时只写该 signal，子树 getter（编译成 `u().field`）登记的依赖精确重跑，
  只发 `SetText/SetAttr`——不销毁重建、不发 `ReplaceChildren`、项内 ID 天然稳定；
- 泛型工厂 `ForGetterOfSignal[T]` 编译期保留元素类型安全（旧 `ForGetterOf[T]` /
  `NewFor` 值快照模式仍保留兼容，同 key 值变化走销毁重建）。

**编译器**：`<For>` 产出 `gxscene.OpFor` + `gxscene.ForGetterOfSignal[T](each, key, render)`，
render 签名为 `func(u func() T) *gxscene.Instance`，body 内 `u.field` 改写为 `u().field`。

> 元素类型 `T` 的推断优先级：render 参数显式注解 > `each` 集合类型推断（`[]T` /
> `[N]T` / signal 声明字面量）> 降级 `any`。推断复用统一绑定表与 signal 声明类型
> 采集，在 signal 改写前完成，确保初值字面量类型不被 `Signal.New(...)` 包裹遮蔽。

---

## 阶段 4：GUI 调度与生命周期（`runtime/app`）

- **结构变化重绘**（修复集成缺陷）：此前 app 只建聚合 effect 调 `Update`，
  未为 Branch/For 结构插槽接线，导致分支/列表切换时场景树变了却不重绘。
  新增 `Instance.BindStructural`：仅为动态结构插槽接线，与叶子聚合 effect 并存；
  `app.render` 内部用「重绘包装」的 effect 接线，selector/each 变化重建子树后自动画一帧。
- **API 重构**：`Open(cfg)` 只收窗口配置、返回 `*App`（不收 JSX、不绘制）；
  `App.Render(build func() any)` 收「建树函数」（GX 写 `app.render(() => <App/>)`，
  SolidJS 风格）——在内部建立的**根 Owner** 下建树、画首帧，并把叶子聚合 effect 与
  结构重绘接线全部封装在内部（`bindReactive` 变为私有）。源码无需再手写 `app.BindReactive`。
- **组件生命周期**：组件在建树时创建的 `Signal.effect` / `computed` / `onMount` /
  `onCleanup` 归属本次 `Render` 的根 Owner；`App.Close` / 重复 `Render` 时随 Owner
  级联销毁并执行 `onCleanup`，不会残留永不停止的 Effect。组件内局部 `signal` 变化
  照常驱动 UI 更新（见 `examples/jsxapp-solid.gx`）。
- **关闭清理**：`App.Close` dispose 根 Owner（级联组件副作用）与内部聚合句柄，
  调用 `inst.Dispose()` 清理结构 effect 与元素 Owner；Render 之前调用也安全。
- **批处理**：`Batch` 合并同一轮多次 signal 写入为一帧重绘（已验证）。
- **压力测试**：Owner 500 轮创建/销毁、分支 200 轮切换，Effect/Cleanup 严格配对无泄漏。

---

## 验证

- `go build ./...` 与 `go test ./...` 全量通过（Go 1.27.4）。
- 各阶段均含单元测试 + 编译器测试；条件分支与 For 另有端到端集成测试
  （转译 → 真实 `go run` → 校验输出）。
- 关键测试：
  - `runtime/signal/owner_test.go`（Owner/Cleanup/批处理 + 压力）
  - `runtime/scene/branch_test.go`、`for_test.go`（结构重建 / 复用 / 清理 / 压力）
  - `runtime/app/app_test.go`（结构变化重绘 / 批处理合并帧）
  - `internal/integration/integration_test.go`（JSX 条件分支 / For 端到端）
