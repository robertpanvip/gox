package interp

import (
	"os"
	"path/filepath"
	"testing"
	"strings"

	"github.com/robertpanvip/gox/runtime/signal"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// TestInterpretSamples 逐个解释 15 个 demo 源码，确保都能产出场景树。
// 这是解释器的核心回归：每个 demo 覆盖一类语言特性。
func TestInterpretSamples(t *testing.T) {
	dir := filepath.Join("..", "samples")
	files, err := filepath.Glob(filepath.Join(dir, "*.gx"))
	if err != nil || len(files) == 0 {
		t.Fatalf("找不到 demo 源文件: %v", err)
	}
	for _, f := range files {
		f := f
		t.Run(filepath.Base(f), func(t *testing.T) {
			srcBytes, err := os.ReadFile(f)
			if err != nil { t.Fatal(err) }
			res, err := Interpret(string(srcBytes))
			if err != nil { t.Fatalf("解释失败: %v", err) }
			if res == nil || res.Instance == nil { t.Fatal("view() 未产出场景树") }
			root := res.Instance.Root()
			if root == nil || len(root.Children) == 0 {
				t.Fatal("场景树为空")
			}
		})
	}
}

func containsKind(n *gxscene.Node, kind string) bool {
	if n == nil { return false }
	if n.Kind == kind { return true }
	for _, c := range n.Children { if containsKind(c, kind) { return true } }
	return false
}

// TestInterpretCounterRuns 验证 sig + 事件 demo 解释后能拿到根节点含 button。
func TestInterpretCounterRuns(t *testing.T) {
	src := `sig count = 0
func inc() { count = count + 1 }
func view() *gxscene.Instance {
    return <button onClick={inc}>Count: {count}</button>
}`
	res, err := Interpret(src)
	if err != nil { t.Fatal(err) }
	root := res.Instance.Root()
	if root == nil { t.Fatal("nil root") }
	if !containsKind(root, "button") { t.Fatalf("场景树里没有 button 节点: %#v", root) }
}

// TestInterpretNestedElements 验证多层嵌套元素。
func TestInterpretNestedElements(t *testing.T) {
	src := `func view() *gxscene.Instance {
    return <div><span>hello</span><b>world</b></div>
}`
	res, err := Interpret(src)
	if err != nil { t.Fatal(err) }
	root := res.Instance.Root()
	if !containsKind(root, "div") || !containsKind(root, "span") || !containsKind(root, "b") {
		t.Fatalf("嵌套元素缺失: %#v", root)
	}
}

func TestInterpretRequiresView(t *testing.T) {
	if _, err := Interpret("sig x = 1"); err == nil { t.Fatal("缺 view() 应报错") }
}

// TestInterpretReactive 验证点击事件真的改变 sig 并更新场景树文本——
// 这是 Run 能"真正运行"的关键：交互要像编译产物一样响应式。
func TestInterpretReactive(t *testing.T) {
	src := `sig count = 0
func inc() { count = count + 1 }
func view() *gxscene.Instance {
    return <button onClick={inc}>Count: {count}</button>
}`
	res, err := Interpret(src)
	if err != nil { t.Fatal(err) }
	res.Instance.BindLeafEffects(signal.Effect)
	res.Instance.BindStructural(signal.Effect)
	root := res.Instance.Root()
	// 找到 button 节点
	var btn *gxscene.Node
	var find func(*gxscene.Node)
	find = func(n *gxscene.Node) {
		if n == nil { return }
		if n.Kind == "button" { btn = n }
		for _, c := range n.Children { find(c) }
	}
	find(root)
	if btn == nil { t.Fatal("找不到 button") }
	// 派发点击
	if !btn.Dispatch("onClick", gxscene.EventContext{}) {
		t.Fatal("事件派发失败：handler 类型不对")
	}
	// 检查文本节点更新为 Count: 1
	var texts []string
	var collect func(*gxscene.Node)
	collect = func(n *gxscene.Node) {
		if n == nil { return }
		if n.Type == gxscene.NodeText && n.Text != "" { texts = append(texts, n.Text) }
		for _, c := range n.Children { collect(c) }
	}
	collect(root)
	joined := ""
	for _, tx := range texts { joined += tx }
	if !strings.Contains(joined, "1") {
		t.Fatalf("点击后文本应包含 1，实际: %q (texts=%v)", joined, texts)
	}
}
