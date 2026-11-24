package compiler

import (
	"strings"
	"testing"
)

// 回归：sig 变量应像普通变量读写（count + 1 / count = 5）。用户手写显式调用
// count() 是把函数式 Signal 的实现细节当语法用，编译期必须报错并给出惯用写法。
func TestNoExplicitSigCall(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantErr bool
		wantMsg string
	}{
		{
			name: "显式调用读取报错",
			src: `package main
sig count = 0
func inc() { count = count() + 1 }
`,
			wantErr: true,
			wantMsg: "sig 变量 count 应像普通变量一样读写",
		},
		{
			name: "显式调用作为插值报错",
			src: `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
sig count = 0
func view() *gxscene.Instance { return <div>{count()}</div> }
`,
			wantErr: true,
			wantMsg: "sig 变量 count 应像普通变量一样读写",
		},
		{
			name: "computed 派生 sig 显式调用报错",
			src: `package main
sig n = 1
func main() { let doubled = Signal.computed(() => n * 2); _ = doubled() }
`,
			wantErr: true,
			wantMsg: "sig 变量 doubled 应像普通变量一样读写",
		},
		{
			name: "隐式读写通过",
			src: `package main
sig count = 0
func inc() { count = count + 1 }
`,
			wantErr: false,
		},
		{
			name: "JSX 插值隐式读通过",
			src: `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
sig count = 0
func view() *gxscene.Instance { return <div>{count}</div> }
`,
			wantErr: false,
		},
		{
			name: "context getter 的局部 getter 不误伤",
			src: `package main
import (
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	gxapp "github.com/robertpanvip/gox/runtime/app"
)
func view(ctx gxapp.Context) *gxscene.Instance {
	score := ctx.use()
	return <b>{score()}</b>
}
`,
			wantErr: false,
		},
		{
			name: "同名局部变量遮蔽 sig 不误伤",
			src: `package main
sig count = 0
func f() { count := func() int { return 1 }; _ = count() }
`,
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile("test.gx", tc.src)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望报错 %q，实际通过", tc.wantMsg)
				}
				if tc.wantMsg != "" && !strings.Contains(err.Error(), tc.wantMsg) {
					t.Fatalf("错误信息 %q 不含期望 %q", err.Error(), tc.wantMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("不应报错，实际: %v", err)
			}
		})
	}
}
