package compiler

import (
	"testing"
)

// TestNamedIconJSX 验证具名图标组件在 GX JSX 里可像 <IconHouse/> 这样引用，
// 编成 OpComp + IconHouse(Props{...})，size/tone/color 作为 props 插槽。
// 与 widgets 标准库其它组件（<Card/>）用法完全一致。
func TestNamedIconJSX(t *testing.T) {
	src := `package main

import gxscene "github.com/robertpanvip/gox/runtime/scene"

func view() any {
	return <div><IconHouse size="24" tone="blue"/><IconSearch size="20" color="#fff"/></div>
}
`
	got := compileOK(t, "iconjsx.gx", src)
	containsAll(t, got,
		"gxscene.OpComp",
		`Arg:"IconHouse"`,
		`Arg:"IconSearch"`,
		"IconHouse(gxscene.Props{",
		"IconSearch(gxscene.Props{",
		`"tone":"blue"`,
		`"color":"#fff"`,
	)
}
