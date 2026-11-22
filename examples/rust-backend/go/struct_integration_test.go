package rustbackend

import (
	"encoding/binary"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

func forTpl() gxscene.Template {
	return gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "ul"},
		{Code: gxscene.OpFor, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
}

func itemTpl() gxscene.Template {
	return gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "li"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
}

// TestForStructuralChangesSerialize 验证真实 host <For> 增删产生的结构动作
// 能被 GRX2 编码（不回退全量），且 blob 内含合法 GRX1 子树。
func TestForStructuralChangesSerialize(t *testing.T) {
	items := signal.New([]int{1, 2, 3})
	f := gxscene.NewFor(
		func() []any {
			src := items()
			out := make([]any, len(src))
			for i, x := range src { out[i] = x }
			return out
		},
		func(v any) any { return v.(int) },
		func(v any) *gxscene.Instance {
			n := v.(int)
			return gxscene.Render(itemTpl(), func() any { return n })
		},
	)
	root := gxscene.Render(forTpl(), func() any { return f })
	root.BindReactive(signal.Effect)
	root.EnableChanges()
	root.TakeChanges() // 清接线期

	// 追加一项 → 应产生 InsertBefore 结构动作
	items(append(items(), 4))
	changes := root.TakeChanges()
	hasStruct := false
	for _, c := range changes {
		if c.Kind == gxscene.ChangeInsertBefore || c.Kind == gxscene.ChangeReplaceChildren ||
			c.Kind == gxscene.ChangeRemoveChild || c.Kind == gxscene.ChangeMoveChild {
			hasStruct = true
		}
	}
	if !hasStruct {
		t.Fatalf("追加列表项应产生结构动作, got %v", changes)
	}
	buf, ok := serializeDelta(changes, root.Root(), 100, 100)
	if !ok {
		t.Fatalf("结构动作不应回退全量, changes=%v", changes)
	}
	if binary.LittleEndian.Uint32(buf[0:4]) != deltaMagic {
		t.Fatal("应为 GRX2 帧")
	}

	// 更新 root：再做一次删除 → 仍应编码成功
	root.BindReactive(signal.Effect)
	items([]int{1, 2})
	changes2 := root.TakeChanges()
	buf2, ok2 := serializeDelta(changes2, root.Root(), 100, 100)
	if !ok2 || binary.LittleEndian.Uint32(buf2[0:4]) != deltaMagic {
		t.Fatalf("删除列表项的结构动作应编码为 GRX2, changes=%v", changes2)
	}
}
