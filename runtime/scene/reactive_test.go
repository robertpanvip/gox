package gxscene_test

import (
	"strings"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// 这一组测试模拟编译器为 signal 语法 + JSX scene 后端生成的产物，验证：
// signal 经 Props getter 从根一路传到叶子组件，叶子在其动态插槽里读取；
// BindReactive(signal.Effect) 接线后，signal 写入自动精确命中对应插槽，
// 既不手调 Update()、也不重建组件子树。

// labelTpl 是叶子组件 <span>{text}</span> 的模板（一个动态子节点插槽）。
func labelTpl() gxscene.Template {
	return gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "span"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
}

// panelTpl 是中间组件 <section><Label/></section> 的模板（一个 OpComp 插槽）。
func panelTpl() gxscene.Template {
	return gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "section"},
		{Code: gxscene.OpComp, Arg: "Label", Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
}

// rootTpl 是 <div><Panel/></div> 的模板（一个 OpComp 插槽）。
func rootTpl() gxscene.Template {
	return gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpComp, Arg: "Panel", Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
}

// Label 是叶子组件：从 Props 取出 signal（静态属性，signal 对象本身不变），
// 在其动态子节点 getter 里读 signal 的值。这是"消费信号"的位置。
func labelOf(props gxscene.Props, calls *int) *gxscene.Instance {
	*calls++
	text := gxscene.Get[signal.Signal[string]](props, "text")
	return gxscene.Render(labelTpl(), func() any { return text()() })
}

// Panel 是中间组件：从 Props 取出 signal，原样转发给 Label。
// 注意组件 slotComp 的 getter 只"转发 signal 对象"，并不读它的值——
// 所以 signal 变化不会触发 Panel 重挂载，只会被子组件叶子的 effect 精确接住。
func panelOf(props gxscene.Props, calls *int, labelCalls *int) *gxscene.Instance {
	*calls++
	text := gxscene.Get[signal.Signal[string]](props, "text")
	return gxscene.Render(panelTpl(), func() any {
		return labelOf(gxscene.Props{"text": text()}, labelCalls)
	})
}

func TestSignalFlowsThroughComponentsAndAutoUpdates(t *testing.T) {
	title := signal.New("GX")
	labelCalls, panelCalls := 0, 0

	// 根组件 getter 把 signal 作为 Props 往下传。
	view := gxscene.Render(rootTpl(), func() any {
		return panelOf(gxscene.Props{"text": func() any { return title }}, &panelCalls, &labelCalls)
	})

	if labelCalls != 1 || panelCalls != 1 {
		t.Fatalf("组件应各执行一次, panel=%d label=%d", panelCalls, labelCalls)
	}
	if out := view.String(); !strings.Contains(out, `"GX"`) {
		t.Fatalf("初始渲染应有 GX:\n%s", out)
	}

	// 一行接线：把 signal.Effect 注入 scene，自动为叶子插槽建立精确 effect。
	view.BindReactive(signal.Effect)

	// signal 写入：不用 view.Update()，叶子自动刷新。
	title("Gox")
	if out := view.String(); !strings.Contains(out, `"Gox"`) || strings.Contains(out, `"GX"`) {
		t.Fatalf("叶子未自动更新:\n%s", out)
	}
	if labelCalls != 1 || panelCalls != 1 {
		t.Fatalf("signal 变化不应重建组件, panel=%d label=%d", panelCalls, labelCalls)
	}
	if view.Updates != 0 {
		t.Fatalf("不应调用 Update(), got %d", view.Updates)
	}

	// 写相同值：signal effect 会重跑，但 SetPart 的 equalValue 裁剪应零写入，
	// 渲染结果保持不变。（Writes 记在各 Instance 上，不跨子树累加。）
	before := view.String()
	title("Gox")
	if view.String() != before {
		t.Fatal("写相同值不应改变渲染结果")
	}
}

func TestBindReactivePreciseSlotTargeting(t *testing.T) {
	a := signal.New("A")
	b := signal.New("B")

	// <div>{a()}{b()}</div> —— 两个独立的动态子节点插槽。
	tpl := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpDynChild, Slot: 1},
		{Code: gxscene.OpClose},
	}, N: 2}
	view := gxscene.Render(tpl,
		func() any { return a() },
		func() any { return b() },
	)
	view.BindReactive(signal.Effect)

	// 只改 b：应只命中 b 的插槽，a 不受影响。
	b("B2")
	if view.Writes != 1 {
		t.Fatalf("只改 b 应写 1 节点, got %d", view.Writes)
	}
	out := view.String()
	if !strings.Contains(out, `"A"`) || !strings.Contains(out, `"B2"`) {
		t.Fatalf("精确更新失败:\n%s", out)
	}

	// a 改、b 写同值：只 a 写一个节点，b 的 effect 重跑但被裁剪。
	a("A2")
	b("B2")
	if view.Writes != 2 {
		t.Fatalf("a 改+b 同值应只写 a 一处, got %d", view.Writes)
	}
	if out := view.String(); !strings.Contains(out, `"A2"`) || !strings.Contains(out, `"B2"`) {
		t.Fatalf("更新结果错误:\n%s", out)
	}
}

func TestBindReactiveComputedDriver(t *testing.T) {
	count := signal.New(1)
	doubled := signal.Computed(func() int { return count() * 2 })

	tpl := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "p"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
	view := gxscene.Render(tpl, func() any { return doubled() })
	view.BindReactive(signal.Effect)

	count(5) // doubled 惰性重算 → effect 命中
	if view.Writes != 1 {
		t.Fatalf("computed 变化应写 1 节点, got %d", view.Writes)
	}
	if out := view.String(); !strings.Contains(out, `"10"`) {
		t.Fatalf("computed 值未渲染:\n%s", out)
	}
}
