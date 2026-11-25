package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/robertpanvip/gox/runtime/loop"
)

// syncLoop 持一个真实 loop.Queue，由测试显式 drain（模拟 UI 上下文轮次）。
// Post/PostMicro 只入队、不在调用栈同步执行——满足 UIPromise 的线性化前提。
type syncLoop struct{ *loop.Queue }

func newSyncLoop() *syncLoop { return &syncLoop{loop.New(loop.Config{})} }

// awaitTurn 等到后台 settle 产生的微任务入队，再跑一个 UI 轮次。
// loop.RunMicrotasks 会在同一次 drain 里把链式 Then 新投递的 continuation 一并跑完，
// 所以一次 awaitTurn 即可覆盖整条 Then/Catch 链。
func awaitTurn(t *testing.T, rt *syncLoop) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, micro := rt.HasPending()
		if micro > 0 {
			for rt.RunOneMacro() {
			}
			rep := rt.RunMicrotasks()
			if !rep.Complete() {
				t.Fatalf("微任务未跑完: %+v", rep)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("等待 UI continuation 超时（后台请求未完成？）")
}

type payload struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

// TestUIFetchSuccessAndChain 验证：后台请求 → Then 在 UI 轮次执行 → 链式解析。
func TestUIFetchSuccessAndChain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"Pan","age":7}`))
	}))
	defer srv.Close()

	rt := newSyncLoop()
	var gotName string
	var gotAge int

	p1 := loop.Then(UIFetch(rt, GetRequest(srv.URL)), func(r *Response) (payload, error) {
		if !r.Ok() || r.Status() != 200 {
			return payload{}, errors.New("bad status")
		}
		if ct := r.Header("Content-Type"); ct != "application/json" {
			return payload{}, errors.New("bad content-type: " + ct)
		}
		var u payload
		return u, r.JSON(&u)
	})
	p2 := loop.Then(p1, func(u payload) (int, error) {
		gotName, gotAge = u.Name, u.Age // 模拟在 UI 上下文写 Signal
		return u.Age, nil
	})

	awaitTurn(t, rt)
	if gotName != "Pan" || gotAge != 7 {
		t.Fatalf("链式结果 = (%q,%d)，期望 (Pan,7)", gotName, gotAge)
	}
	_ = p2
}

// TestUIFetchIsAsync 验证请求不阻塞调用方：handler 故意慢，UIFetch 应立即返回。
func TestUIFetchIsAsync(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte("done"))
	}))
	defer srv.Close()

	rt := newSyncLoop()
	start := time.Now()
	p := UIFetch(rt, GetRequest(srv.URL))
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("UIFetch 阻塞了 %v，应异步返回", elapsed)
	}

	var txt string
	loop.Then(p, func(r *Response) (string, error) {
		var err error
		txt, err = r.Text()
		return txt, err
	})
	awaitTurn(t, rt)
	if txt != "done" {
		t.Fatalf("Text() = %q", txt)
	}
}

// TestResponseParsers 验证 text/json/arrayBuffer/blob 与各属性。
func TestResponseParsers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("hello 世界"))
	}))
	defer srv.Close()

	rt := newSyncLoop()
	var resp *Response
	loop.Then(UIFetch(rt, GetRequest(srv.URL)), func(r *Response) (*Response, error) {
		resp = r
		return r, nil
	})
	awaitTurn(t, rt)
	if resp == nil {
		t.Fatal("未拿到 Response")
	}
	if resp.Status() != http.StatusCreated || resp.StatusText() != "Created" || !resp.Ok() {
		t.Fatalf("status=%d text=%q ok=%v", resp.Status(), resp.StatusText(), resp.Ok())
	}
	if txt, err := resp.Text(); err != nil || txt != "hello 世界" {
		t.Fatalf("Text()=%q err=%v", txt, err)
	}
	if buf := resp.ArrayBuffer(); len(buf) != len("hello 世界") {
		t.Fatalf("ArrayBuffer len=%d", len(buf))
	}
	if blob := resp.Blob(); len(blob) != len("hello 世界") {
		t.Fatalf("Blob len=%d", len(blob))
	}
	var v any
	if err := resp.JSON(&v); err == nil {
		t.Fatal("非 JSON 响应体 JSON() 应报错")
	}
	// ArrayBuffer 是副本：改副本不影响原 body。
	buf := resp.ArrayBuffer()
	buf[0] = 'X'
	if resp.BodyBytes()[0] == 'X' {
		t.Fatal("ArrayBuffer 应返回副本")
	}
}

// TestUIFetchNotFoundIsFulfilled 验证 404 是 Fulfilled（Web Fetch：网络错误才 reject）。
func TestUIFetchNotFoundIsFulfilled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	rt := newSyncLoop()
	var ok bool
	loop.Then(UIFetch(rt, GetRequest(srv.URL)), func(r *Response) (bool, error) {
		ok = r.Ok()
		return r.Ok(), nil
	})
	awaitTurn(t, rt)
	if ok {
		t.Fatal("404 应 Ok()==false")
	}
}

// TestUIFetchNetworkErrorRejects 验证网络错误走 Rejected + Catch。
func TestUIFetchNetworkErrorRejects(t *testing.T) {
	rt := newSyncLoop()
	var caught error
	loop.Catch(UIFetch(rt, Request{URL: "http://127.0.0.1:0/unreachable"}), func(err error) (*Response, error) {
		caught = err
		return nil, nil
	})
	awaitTurn(t, rt)
	if caught == nil {
		t.Fatal("网络错误应被 Catch 捕获")
	}
	if !strings.Contains(caught.Error(), "gx/fetch") {
		t.Fatalf("错误应带 gx/fetch 前缀，实际: %v", caught)
	}
}

// TestUIFetchCancelRejects 验证 Context 取消导致 reject。
func TestUIFetchCancelRejects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second) // 等取消
		_, _ = w.Write([]byte("late"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	rt := newSyncLoop()
	p := UIFetch(rt, Request{URL: srv.URL, Context: ctx})
	cancel()
	var caught error
	loop.Catch(p, func(err error) (*Response, error) { caught = err; return nil, nil })
	awaitTurn(t, rt)
	if !errors.Is(caught, context.Canceled) {
		t.Fatalf("取消应返回 context.Canceled，实际: %v", caught)
	}
}

// TestUIFetchTimeoutRejects 验证 Request.Timeout 生效。
func TestUIFetchTimeoutRejects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		_, _ = w.Write([]byte("late"))
	}))
	defer srv.Close()

	rt := newSyncLoop()
	p := UIFetch(rt, Request{URL: srv.URL, Timeout: 100 * time.Millisecond})
	var caught error
	loop.Catch(p, func(err error) (*Response, error) { caught = err; return nil, nil })
	awaitTurn(t, rt)
	if !errors.Is(caught, context.DeadlineExceeded) {
		t.Fatalf("超时应返回 context.DeadlineExceeded，实际: %v", caught)
	}
}

// TestUIFetchInvalidInput 验证 nil Executor / 空 URL 返回 Rejected 而非 panic。
func TestUIFetchInvalidInput(t *testing.T) {
	if p := UIFetch(nil, Request{URL: "http://example.com"}); p == nil {
		t.Fatal("nil rt 应返回 Promise 而非 nil")
	}
	rt := newSyncLoop()
	var caught error
	loop.Catch(UIFetch(rt, Request{URL: "  "}), func(err error) (*Response, error) {
		caught = err
		return nil, nil
	})
	awaitTurn(t, rt)
	if caught == nil || !strings.Contains(caught.Error(), "URL 为空") {
		t.Fatalf("空 URL 应 Rejected，实际: %v", caught)
	}
}

// TestPostSendsBody 验证 POST 的 method/header/body 正确送达。
func TestPostSendsBody(t *testing.T) {
	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 64)
		n, _ := r.Body.Read(buf)
		got <- r.Method + "|" + r.Header.Get("Content-Type") + "|" + string(buf[:n])
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt := newSyncLoop()
	loop.Then(Post(rt, srv.URL, []byte(`{"k":1}`), "application/json"), func(r *Response) (int, error) {
		return r.Status(), nil
	})
	awaitTurn(t, rt)
	select {
	case s := <-got:
		want := `POST|application/json|{"k":1}`
		if s != want {
			t.Fatalf("送达 = %q，期望 %q", s, want)
		}
	default:
		t.Fatal("handler 未收到请求")
	}
}

// GetRequest 构造 GET Request（测试可读性）。
func GetRequest(url string) Request { return Request{URL: url, Method: MethodGet} }
