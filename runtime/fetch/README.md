# runtime/fetch — UI 感知的 HTTP 客户端（UIFetch）

基于 `runtime/loop.UIPromise` 封装的 HTTP 客户端。**它不是通用 `net/http` 客户端**，而是把"网络请求"这件事纳入 GX 的 UI 线程契约：

```
后台 goroutine 发请求、读 body
        │  loop.Go → 结果经 Executor 投递
        ▼
UI 上下文：Then/Catch/Finally 回调执行，可安全读写 Signal
```

调用方无需自己写 `go func` + `app.Dispatch`——`UIFetch` 返回的 `*UIPromise[*Response]` 已绑定 UI Executor。

## 为什么叫 UIFetch 而不是 Fetch

命名与 `UIPromise` 一脉相承，强调其职责边界：核心不是"发请求"，而是"把响应安全交回 UI 上下文供 Then 消费"。裸网络需求请直接用 `net/http`。

## API

| 入口 | 说明 |
|---|---|
| `UIFetch(rt, req) *UIPromise[*Response]` | 通用入口，`rt` 为 UI Executor |
| `Get(rt, url)` / `Post(rt, url, body, contentType)` | 快捷方式 |
| `Request{URL, Method, Headers, Body, Timeout, Context}` | 请求描述（值类型，零值可用） |

`Response` 对标 Web Fetch：

| 方法 | 语义 |
|---|---|
| `Status()` / `StatusText()` / `Ok()` | 状态码 / 文本 / 200-299 为 true |
| `Header(k)` / `Headers()` | 响应头（大小写不敏感） |
| `URL()` / `Redirected()` | 最终 URL / 是否重定向 |
| `Text() (string, error)` | UTF-8 解码 |
| `JSON(v any) error` | 解析到指针 v |
| `ArrayBuffer()` / `Blob()` / `BodyBytes()` | 字节（前两个返回副本） |

## 与 Web Fetch 的有意差异

| 项 | Web Fetch | UIFetch | 原因 |
|---|---|---|---|
| `json()`/`text()` | 返回 Promise | 同步 `(v, error)` | body 已在后台读完，解析是纯 CPU、很快；避免无谓的二次微任务投递 |
| 组合子 | `p.then(...)` 方法链 | `loop.Then(p, fn)` 包级函数 | Go 泛型不支持方法带新类型参数（与 `UIPromise` 一致） |
| 取消 | `AbortController` | `context.Context` | Go 惯例 |
| 超时 | 无内建 | `Request.Timeout` | 显式、细粒度 |
| 错误 | reject 异常 | Promise Rejected + `loop.Catch` | 无异常模型 |

**4xx/5xx 是 Fulfilled**（`Ok()==false`），只有网络错误/超时/取消才 Rejected——与 Web Fetch 一致。

## 三端语义

fetch 逻辑统一在 Go runtime 层，三个宿主共用同一份 `runtime/fetch`：

| 宿主 | Executor 来源 | 网络栈 |
|---|---|---|
| 桌面（GLFW + Skia） | `glWindow.uiLoop`（`*loop.Queue`） | Go `net/http` |
| Web（WASM） | `gxweb.Window` 需适配成 `loop.Executor`（`Post` 走 RAF，`PostMicro` 同轮次） | Go `net/http` 在 js/wasm 下走浏览器 fetch（受 CORS 约束） |
| Rust 渲染后端 | Go 宿主侧同样用 `loop.Queue`；Rust 只接收序列化的场景树 | Go `net/http`（Rust 端不执行业务逻辑/网络） |

> Rust 是纯渲染后端（C ABI 收场景树，`image.rs` 明确"Rust 端无 HTTP 栈，Go adapter 应转 data URI"），故 fetch 不在 Rust 里重复实现。

## 用法示例

```go
p1 := loop.Then(
    fetch.UIFetch(rt, fetch.Request{URL: "https://api.example.com/user"}),
    func(r *fetch.Response) (string, error) {
        if !r.Ok() { return "", fmt.Errorf("HTTP %d", r.Status()) }
        var u struct{ Name string `json:"name"` }
        if err := r.JSON(&u); err != nil { return "", err }
        return u.Name, nil
    },
)
loop.Catch(p1, func(err error) (*string, error) {
    fmt.Println("请求失败:", err)
    return nil, nil
})
// 在宿主的 UI 轮次里 drain：桌面 Run() 循环 / Web rAF 回调
```

## 测试

```bash
go test ./runtime/fetch/          # httptest 本地服务：成功/链/异步/解析/404/网络错误/取消/超时/POST
```
