package interp

import (
	"fmt"

	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
)

// Result 是一次解释执行的结果。
type Result struct {
	Instance *gxscene.Instance
	Env      *Env
	Program  *Program
}

// Interpret 解释 GX 源码，调用 view() 返回场景树。
//
// 返回的 Instance 不含响应式绑定——由宿主（gxapp.Render 的 bindReactive）
// 统一接线，避免与外层树重复绑定。若单独使用，调用方需自行
// BindLeafEffects / BindStructural。
func Interpret(src string) (res *Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			res = nil
			err = fmt.Errorf("解释执行失败: %v", r)
		}
	}()

	p := parseProgram(src)
	if p == nil { return nil, fmt.Errorf("解析失败") }
	fn := p.env.lookupFunc("view")
	if fn == nil { return nil, fmt.Errorf("源码缺少 view() 函数") }
	v := callValue(fn, nil, p.env)
	inst, ok := v.(*gxscene.Instance)
	if !ok { return nil, fmt.Errorf("view() 未返回场景树") }
	return &Result{Instance: inst, Env: p.env, Program: p}, nil
}
