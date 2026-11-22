package rustbackend

import (
	"encoding/binary"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

func leaf(id, text string) *gxscene.Node {
	return &gxscene.Node{Type: gxscene.NodeText, ID: id, Text: text}
}

func TestSerializeDeltaAttrText(t *testing.T) {
	changes := []gxscene.Change{
		{Kind: gxscene.ChangeSetText, NodeID: "n1", Value: "hello"},
		{Kind: gxscene.ChangeSetAttr, NodeID: "n1", Key: "bg", Value: "#fff"},
		{Kind: gxscene.ChangeSetHandler, NodeID: "n1", Key: "onClick"},
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
		Type: gxscene.NodeDiv, ID: "r",
		Children: []*gxscene.Node{
			leaf("a", "A"), leaf("b", "B"), leaf("c", "C"),
		},
	}
	changes := []gxscene.Change{
		{Kind: gxscene.ChangeRemoveChild, NodeID: "r", ChildID: "a"},
		{Kind: gxscene.ChangeMoveChild, NodeID: "r", ChildID: "b", BeforeID: "c"},
		{Kind: gxscene.ChangeInsertBefore, NodeID: "r", ChildID: "d",
			BeforeID: "c", Value: leaf("d", "D")},
		{Kind: gxscene.ChangeReplaceChildren, NodeID: "r"}, // Value=nil → 从 root 查
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

func TestSerializeDeltaReplaceChildrenNilRootFallbacks(t *testing.T) {
	// ReplaceChildren 且 root 中查不到 → 应回退全量（无法构造子树）
	changes := []gxscene.Change{{Kind: gxscene.ChangeReplaceChildren, NodeID: "missing"}}
	_, ok := serializeDelta(changes, nil, 100, 100)
	if ok {
		t.Fatal("无法定位子树时应回退全量")
	}
}
