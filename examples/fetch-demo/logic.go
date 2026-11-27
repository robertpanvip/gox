// Package main 演示 UIFetch 在三端的统一用法。
//
// 核心业务逻辑 LoadUser 与宿主无关：它只依赖 loop.Executor（由桌面/Web/Rust
// 各自的宿主提供），用 UIFetch 发请求、loop.Then 在 UI 上下文解析并写 Signal。
// 三端差异仅在"如何构造 Executor、何时 drain"，业务逻辑零条件编译。
package main

import (
	"fmt"

	"github.com/robertpanvip/gox-core/runtime/fetch"
	"github.com/robertpanvip/gox-core/runtime/loop"
)

// User 是示例数据结构。
type User struct {
	Name string `json:"name"`
	Bio  string `json:"bio"`
}

// LoadUser 演示完整链路：后台 GET → UI 上下文解析 JSON → 写 Signal。
// onResult / onError 都在 UI 上下文被调用（由 Promise 的 Executor 保证），
// 宿主可直接在其内部写 Signal，无需再 app.Dispatch。
func LoadUser(rt loop.Executor, url string, onResult func(User), onError func(error)) {
	p1 := loop.Then(
		fetch.UIFetch(rt, fetch.Request{URL: url, Timeout: 5e9 /* 5s */}),
		func(r *fetch.Response) (User, error) {
			if !r.Ok() {
				return User{}, fmt.Errorf("HTTP %d %s", r.Status(), r.StatusText())
			}
			var u User
			return u, r.JSON(&u)
		},
	)
	// 成功：写 UI；失败：透传给 onError（Then 的错误会原样流到下游）。
	loop.Then(p1, func(u User) (int, error) {
		onResult(u)
		return 0, nil
	})
	loop.Catch(p1, func(err error) (User, error) {
		onError(err)
		return User{}, nil
	})
}
