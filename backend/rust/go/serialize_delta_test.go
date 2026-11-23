package rustbackend

import (
	"encoding/binary"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

func leaf(id gxscene.NodeID, text string) *gxscene.Node {
	return &gxscene.Node{Type: gxscene.NodeText, ID: id, Text: text}
}

func TestSerializeDeltaAttrText(t *testing.T) {
	changes := []gxscene.Change{
		{Kind: gxscene.ChangeSetText, NodeID: "8", Value: "hello"},
		{Kind: gxscene.ChangeSetAttr, NodeID: "8", Key: "bg", Value: "#fff"},
		{Kind: gxscene.ChangeSetHandler, NodeID: "8", Key: "onClick"},
	}
	buf, ok := serializeDelta(changes, nil, 100, 100)
	if !ok {
		t.Fatal("属性动作不应回退全量")
	}
	if binary.LittleEndian.Uint32(buf[0:4]) != deltaMagic {
		t.Fatal("magic 错误")
	}
	if got := binary.LittleEndian.Uint32(buf[8:12]); got != uint32(len(changes)) {
		t.Fatalf("action_count=%d want %d", got, len(changes))
	}
	// kind 依次
	want := []byte{actSetText, actSetAttr, actSetHandle}
	for i, w := range want {
		if got := buf[deltaHeader+i*actionSize]; got != w {
			t.Fatalf("action[%d].kind=%d want %d", i, got, w)
		}
	}
}

func TestSerializeDeltaStructuralNoFallback(t *testing.T) {
	root := &gxscene.Node{
		Type: gxscene.NodeDiv, ID: 1,
		Children: []*gxscene.Node{
			leaf(2, "A"), leaf(3, "B"), leaf(4, "C"),
		},
	}
	changes := []gxscene.Change{
		{Kind: gxscene.ChangeRemoveChild, NodeID: "1", ChildID: "2"},
		{Kind: gxscene.ChangeMoveChild, NodeID: "1", ChildID: "3", BeforeID: "4"},
		{Kind: gxscene.ChangeInsertBefore, NodeID: "1", ChildID: "5",
			BeforeID: "4", Value: leaf(5, "D")},
		{Kind: gxscene.ChangeReplaceChildren, NodeID: "1"}, // Value=nil → 从 root 查
	}
	buf, ok := serializeDelta(changes, root, 100, 100)
	if !ok {
		t.Fatal("结构动作不应回退全量")
	}
	want := []byte{actRemoveChild, actMoveChild, actInsertBefore, actReplaceChildren}
	for i, w := range want {
		if got := buf[deltaHeader+i*actionSize]; got != w {
			t.Fatalf("action[%d].kind=%d want %d", i, got, w)
		}
	}
	// ReplaceChildren 的 blob length 应 > 0（内嵌 GRX1 帧，含 4 节点）
	vall := binary.LittleEndian.Uint32(buf[deltaHeader+3*actionSize+20 : deltaHeader+3*actionSize+24])
	if vall == 0 {
		t.Fatal("ReplaceChildren 未携带子树 blob")
	}
	// blob 区 magic 应为 GRX1
	poolLen := binary.LittleEndian.Uint32(buf[12:16])
	blobOff := deltaHeader + len(changes)*actionSize + int(poolLen)
	if got := binary.LittleEndian.Uint32(buf[blobOff : blobOff+4]); got != binMagic {
		t.Fatalf("blob magic=%x want GRX1(%x)", got, binMagic)
	}
}

// 回归守护：issue #1 在 host 端根治后，移除了"结构动作 >4 退化 GRX1"的阈值。
// 合法的大批量结构变化（如 <For> 一次增删多项）应全部编码为 GRX2 delta，
// 不再被误降级为全量快照——否则后端每帧都得重建整棵镜像树，失去增量意义。
func TestSerializeDeltaManyStructuralOpsStayDelta(t *testing.T) {
	root := &gxscene.Node{
		Type: gxscene.NodeDiv, ID: 1,
		Children: []*gxscene.Node{leaf(2, "A"), leaf(3, "B"), leaf(4, "C")},
	}
	// 8 个结构动作（远超旧阈值 4）：6 次删除 + 2 次插入。
	changes := []gxscene.Change{
		{Kind: gxscene.ChangeRemoveChild, NodeID: "1", ChildID: "2"},
		{Kind: gxscene.ChangeRemoveChild, NodeID: "1", ChildID: "3"},
		{Kind: gxscene.ChangeRemoveChild, NodeID: "1", ChildID: "4"},
		{Kind: gxscene.ChangeInsertBefore, NodeID: "1", ChildID: "5", Value: leaf(5, "D")},
		{Kind: gxscene.ChangeInsertBefore, NodeID: "1", ChildID: "6", Value: leaf(6, "E")},
		{Kind: gxscene.ChangeRemoveChild, NodeID: "1", ChildID: "5"},
		{Kind: gxscene.ChangeRemoveChild, NodeID: "1", ChildID: "6"},
		{Kind: gxscene.ChangeInsertBefore, NodeID: "1", ChildID: "7", Value: leaf(7, "F")},
	}
	buf, ok := serializeDelta(changes, root, 100, 100)
	if !ok {
		t.Fatal("大批量结构动作不应回退全量（阈值已移除）")
	}
	if binary.LittleEndian.Uint32(buf[0:4]) != deltaMagic {
		t.Fatal("应为 GRX2 帧")
	}
	if got := binary.LittleEndian.Uint32(buf[8:12]); got != uint32(len(changes)) {
		t.Fatalf("action_count=%d want %d", got, len(changes))
	}
}

func TestSerializeDeltaReplaceChildrenNilRootFallbacks(t *testing.T) {
	// ReplaceChildren 且 root 中查不到 → 应回退全量（无法构造子树）
	changes := []gxscene.Change{{Kind: gxscene.ChangeReplaceChildren, NodeID: "missing"}}
	_, ok := serializeDelta(changes, nil, 100, 100)
	if ok {
		t.Fatal("无法定位子树时应回退全量")
	}
}
