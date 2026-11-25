# fetch-demo — UIFetch 三端统一示例

演示 `runtime/fetch` 的 `UIFetch` 如何跨三端复用同一份业务逻辑。**核心 `LoadUser(rt, ...)` 零条件编译**：三端差异只在"如何构造 Executor、何时 drain"。

## 共用业务逻辑（logic.go）

```go
loop.Then(
    fetch.UIFetch(rt, fetch.Request{URL: url, Timeout: 5 * time.Second}),
    func(r *fetch.Response) (User, error) { /* UI 上下文解析 */ },
)
// 成功回调 / Catch 都在 UI 上下文执行，可直接写 Signal
```

## 三端 Executor 与驱动

| 宿主 | Executor | 驱动方式 |
|---|---|---|
| 桌面 GLFW + Skia | `glWindow.uiLoop`（`*loop.Queue`） | `Run()` 主循环每帧 `DrainTurn()` |
| Rust 渲染后端 | Go 宿主侧同样 `*loop.Queue` | winit 事件循环每帧 drain；Rust 只收序列化场景树，不碰网络 |
| Web / WASM | `loop.NewWakeExecutor(win.Post)` 补出 `PostMicro` | wake 里 `win.Post(func(){ exec.DrainTurn() })`，下一帧 drain |

> Rust 是纯渲染后端（C ABI 收场景树，`image.rs` 明确"Rust 端无 HTTP 栈"），故网络与业务逻辑都在 Go 宿主侧，与桌面共用同一份驱动。

## 运行

```bash
# 桌面/Rust 宿主驱动（linux，本地 httptest 服务，成功路径）
go run .

# Web/WASM 驱动（node 宿主，reject 路径演示；浏览器需 gxweb.Window 提供 RAF 投递）
GOOS=js GOARCH=wasm go build -o /tmp/fetch-demo.wasm .
node ../../scripts/runwasm_test.js /tmp/fetch-demo.wasm
```

预期输出：

```
[UI] 用户加载成功: Pan — GX UI Engineer
fetch-demo: OK
```
