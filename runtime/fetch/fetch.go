// Package fetch 提供基于 UIPromise 的 UI 感知 HTTP 客户端（GX 设计提案 §7 的延续）。
//
// 定位：它**不是**通用 net/http 客户端，而是"把响应安全地交回 UI 上下文"的编排层。
// 与 Web Fetch API 对齐的是数据模型（Request / Response / headers / json()/text()），
// 与 loop.UIPromise 对齐的是线程语义：
//
//	后台 goroutine 发请求、读 body  →  结果经 Executor 投回 UI 上下文  →  Then/Catch 在 UI 线程消费
//
// 因此调用方永远不需要自己写 go func + Dispatch：UIFetch 返回一个已绑定 UI Executor
// 的 *loop.UIPromise[*Response]，Then/Catch/Finally 的回调保证在 UI 上下文执行。
//
// 与 Web Fetch 的差异（有意为之，见 README）：
//   - Response.json()/text()/arrayBuffer() 是同步方法（body 已在后台读完），
//     返回 (v, error) 而非 Promise——避免无谓的二次微任务投递，且 JSON 解析很快；
//   - 请求取消用 context.Context（Web 用 AbortController），超时是 Request.Timeout；
//   - 错误通过 Promise 的 Rejected 通道传播，不抛异常。
package fetch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/robertpanvip/gox/runtime/loop"
)

// Method 常量（对标 Web 的常用方法，避免调用方拼裸字符串）。
const (
	MethodGet    = http.MethodGet
	MethodHead   = http.MethodHead
	MethodPost   = http.MethodPost
	MethodPut    = http.MethodPut
	MethodPatch  = http.MethodPatch
	MethodDelete = http.MethodDelete
)

// Request 描述一次 HTTP 请求。零值可用（等价于 GET 空 URL，会在调用时 reject）。
// 刻意做成值类型而非 *http.Request：GX 侧用结构体字面量即可构造，无需暴露 net/http。
type Request struct {
	// URL 请求地址，必填。
	URL string
	// Method 为空时按 GET 处理。
	Method string
	// Headers 请求头；nil 表示无自定义头。
	Headers map[string]string
	// Body 请求体；nil 表示无请求体。发送时按字节原样写入。
	Body []byte
	// Timeout 总超时（含连接、读 body）。<=0 表示不设超时（仍受 ctx 约束）。
	Timeout time.Duration
	// Context 取消信号。nil 时用 context.Background()。取消会导致 Promise reject。
	Context context.Context
}

// Response 是对标 Web Fetch 的响应句柄。字段只读，通过方法访问，避免调用方改写。
//
// Body 在后台 goroutine 中已完整读入内存（io.ReadAll），因此所有解析方法是同步的；
// 这契合"网络 IO 在后台、UI 线程只做轻量解析"的分工。大文件场景请用流式 API
// （未来可加 Stream()），当前版本有意只支持内存响应——这覆盖绝大多数 UI 数据加载。
type Response struct {
	status     int
	statusText string
	headers    http.Header
	body       []byte
	url        string
	redirected bool
}

// Status 返回 HTTP 状态码（如 200）。
func (r *Response) Status() int { return r.status }

// StatusText 返回状态文本（如 "OK"）。
func (r *Response) StatusText() string { return r.statusText }

// Ok 等价于 Web Fetch 的 Response.ok：状态码在 200-299 之间为 true。
func (r *Response) Ok() bool { return r.status >= 200 && r.status < 300 }

// Headers 返回响应头。返回底层 map 的副本语义由调用方约定：不要修改。
// 提供 Get 便捷方法以避免调用方直接改 map。
func (r *Response) Headers() http.Header { return r.headers }

// Header 返回单个响应头的值（大小写不敏感，同 http.Header.Get）。
func (r *Response) Header(key string) string { return r.headers.Get(key) }

// URL 返回最终响应 URL（跟随重定向后）。
func (r *Response) URL() string { return r.url }

// Redirected 报告响应是否来自重定向。
func (r *Response) Redirected() bool { return r.redirected }

// BodyBytes 返回响应体的字节切片（底层数组，勿修改）。
func (r *Response) BodyBytes() []byte { return r.body }

// Text 把响应体按 UTF-8 解码为字符串（对标 Web Response.text()）。
func (r *Response) Text() (string, error) {
	if !utf8.Valid(r.body) {
		return "", fmt.Errorf("gx/fetch: 响应体不是合法 UTF-8")
	}
	return string(r.body), nil
}

// JSON 把响应体解析到 v（对标 Web Response.json()，但同步返回 error）。
// v 必须是指针；解析失败（含非法 JSON、类型不匹配）返回错误。
func (r *Response) JSON(v any) error {
	if v == nil {
		return fmt.Errorf("gx/fetch: JSON 目标不能为 nil")
	}
	return json.Unmarshal(r.body, v)
}

// ArrayBuffer 返回响应体字节的副本（对标 Web Response.arrayBuffer()）。
func (r *Response) ArrayBuffer() []byte {
	out := make([]byte, len(r.body))
	copy(out, r.body)
	return out
}

// Blob 是 ArrayBuffer 的别名（对标 Web Response.blob() 的字节语义）。
func (r *Response) Blob() []byte { return r.ArrayBuffer() }

// UIFetch 发起一次 HTTP 请求，返回绑定到 rt 的 *UIPromise[*Response]。
//
// 请求在后台 goroutine 执行（经 loop.Go），响应体在后台读完；Promise 的
// Then/Catch/Finally 回调经 rt 投回 UI 上下文执行。rt 为 nil 或 req.URL 为空时
// 返回已 Rejected 的 Promise（不 panic）。
//
// 用法（注意 UIPromise 的 Then/Catch 是包级函数，Go 泛型不支持方法带新类型参数）：
//
//	p1 := loop.Then(
//	    fetch.UIFetch(rt, fetch.Request{URL: "https://api.example.com/user"}),
//	    func(r *fetch.Response) (string, error) {
//	        var u User
//	        if err := r.JSON(&u); err != nil { return "", err }
//	        return u.Name, nil
//	    },
//	)
//	p2 := loop.Then(p1, func(name string) (int, error) {
//	    label.Set(name)              // 在 UI 上下文写 Signal
//	    return len(name), nil
//	})
//	_ = p2
func UIFetch(rt loop.Executor, req Request) *loop.UIPromise[*Response] {
	if rt == nil {
		return loop.Rejected[*Response](nil, fmt.Errorf("gx/fetch: UI Executor 为 nil"))
	}
	if strings.TrimSpace(req.URL) == "" {
		return loop.Rejected[*Response](rt, fmt.Errorf("gx/fetch: URL 为空"))
	}
	return loop.Go(rt, func() (*Response, error) { return doRequest(req) })
}

// Get 是 UIFetch 的 GET 快捷方式。
func Get(rt loop.Executor, url string) *loop.UIPromise[*Response] {
	return UIFetch(rt, Request{URL: url, Method: MethodGet})
}

// Post 是 UIFetch 的 POST 快捷方式（body 为字节）。
func Post(rt loop.Executor, url string, body []byte, contentType string) *loop.UIPromise[*Response] {
	h := map[string]string{}
	if contentType != "" {
		h["Content-Type"] = contentType
	}
	return UIFetch(rt, Request{URL: url, Method: MethodPost, Headers: h, Body: body})
}

// doRequest 在后台 goroutine 执行真实 HTTP 请求。所有错误以 error 返回，
// 由 loop.Go 统一转为 Promise 的 Rejected。
func doRequest(req Request) (*Response, error) {
	ctx := req.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	method := req.Method
	if method == "" {
		method = MethodGet
	}
	var bodyReader io.Reader
	if len(req.Body) > 0 {
		bodyReader = bytes.NewReader(req.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, req.URL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("gx/fetch: 构造请求失败: %w", err)
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := defaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gx/fetch: 请求失败: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gx/fetch: 读取响应体失败: %w", err)
	}
	return &Response{
		status:     resp.StatusCode,
		statusText: http.StatusText(resp.StatusCode),
		headers:    resp.Header.Clone(),
		body:       data,
		url:        resp.Request.URL.String(),
		redirected: resp.Request.Response != nil,
	}, nil
}

// defaultClient 复用连接（避免每次请求重建 Transport）。不设全局超时——
// 超时由 Request.Timeout / Context 控制，更细粒度且不会误伤长连接场景。
var defaultClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	},
}
