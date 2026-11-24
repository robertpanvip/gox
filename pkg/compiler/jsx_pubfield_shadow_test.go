package compiler

import (
	"strings"
	"testing"
)

// TestJSXLocalVarShadowingPubField 防回归：
// pub struct 字段名会进裸名 names 表（供 restoreRefs 还原自身 pub 符号）。
// 当 JSX 插值里出现一个与 pub 字段同名的局部变量时，插值 token 由
// lexExprScene 重新词法化、Pos 被重置为 JSX 根位置，ResolveAt 查不到函数体
// 内的局部绑定，曾导致局部变量 text 被误还原成 Text（恰好 Token.Text 字段
// 存在，Validate 也不报错——隐蔽语义错误）。
//
// 修复后：IsGen token 若名字是某函数/块作用域内的局部值（BkLocal/Var/Param/
// RangeVar），视为遮蔽、跳过还原；selector 右侧字段（t.text → t.Text）仍由
// transformExternal 正常还原。
func TestJSXLocalVarShadowingPubField(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
pub type token struct {
    pub text string
    pub color string
}
pub func highlight(s string) []token { return []token{} }
func view() any {
    text := "hello"
    return <div><For each={highlight(text)}>{(t token) => <span>{t.text}</span>}</For></div>
}
func main() { _ = view() }
`
	got := compileOK(t, "shadow.gx", src)
	// 局部变量 text 必须保持小写（不能被 pub 字段名字表误改成 Text）
	if !strings.Contains(got, "return Highlight(text)") {
		t.Errorf("JSX 插值里的局部变量 text 被误还原：\n%s", got)
	}
	if strings.Contains(got, "Highlight(Text)") {
		t.Errorf("局部变量 text 被误还原成 Text：\n%s", got)
	}
	// 真正的 pub 字段 selector 还原仍需生效
	if !strings.Contains(got, "t().Text") {
		t.Errorf("pub 字段 selector 还原失效（应为 t().Text）：\n%s", got)
	}
}
