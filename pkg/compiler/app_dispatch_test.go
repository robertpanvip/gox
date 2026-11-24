package compiler

import (
	"strings"
	"testing"
)

// 回归：GX 的 app.dispatch(...) 应映射到 App.Dispatch(func(){...})。
// 这验证了文档/评审推荐的跨线程入口在语言层可用，无需新增编译器特殊规则。
func TestAppDispatchLowercaseMapping(t *testing.T) {
	got := compileOK(t, "app_dispatch.gx", `package main
import gxapp "github.com/robertpanvip/gox/runtime/app"

func load(app any) {
    app.dispatch(func() {
        println("done")
    })
}
`)
	if !strings.Contains(got, ".Dispatch(func()") {
		t.Fatalf("app.dispatch 应映射为 App.Dispatch(func...):\n%s", got)
	}
	if strings.Contains(got, ".dispatch(") {
		t.Fatalf("不应残留小写 dispatch:\n%s", got)
	}
}
