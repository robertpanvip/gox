package gxapp_test

import (
	"fmt"
	"strings"
	"testing"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	"github.com/robertpanvip/gox/widgets"
)

// TestAppEditorWindowGrowsOnSetSource 用真实 app 接线（OpenWith→Render→bindReactive
// 全时序）复现 wasm 实测缺陷：编辑器 11 行 → SetText 60 行 → 虚拟窗口必须扩展。
// widgets 包的直连测试通过但 app 场景失败，说明断点在 app 的 effect 包装层。
func TestAppEditorWindowGrowsOnSetSource(t *testing.T) {
	var buf strings.Builder
	win := &gxapp.HeadlessWindow{Out: &buf}
	app := openApp(t, gxapp.DefaultConfig(), win)
	defer app.Close()

	st := widgets.NewEditorState("package main\n\nfunc view() {}\n")
	if err := app.Render(func() any { return widgets.Editor(st) }); err != nil {
		t.Fatal(err)
	}

	tree := app.DebugDumpStructural()
	t.Logf("接线后树:\n%s", tree)

	var sb strings.Builder
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	st.SetText(sb.String())

	tree2 := app.DebugDumpStructural()
	t.Logf("SetText 后树:\n%s", tree2)

	if strings.Contains(tree, "evals=1") && strings.Contains(tree2, "evals=1") {
		t.Fatalf("结构插槽 effect 从未重跑（evals=1）——app 接线缺陷复现")
	}
}
