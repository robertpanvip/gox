package rustbackend

import (
	"strings"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// ---------- GRX3：NodeID uint64 直传端到端验证 ----------
//
// GRX3 帧（节点 40B + u64 node_id）与 GRX3 delta（动作 48B，id 全 u64）喂给
// Rust 镜像探针，断言整棵树与增量补丁跨协议完整到达。GRX1/GRX2 由
// mirror_consistency_test.go / serialize_delta_test.go 覆盖，双版本并行存在。

func mustFrameV3(t *testing.T, root *gxscene.Node) []byte {
	t.Helper()
	b, err := serializeFrameBinV3(root, 800, 600)
	if err != nil {
		t.Fatalf("serializeFrameBinV3: %v", err)
	}
	return b
}

func mustDeltaV3(t *testing.T, changes []gxscene.Change, root *gxscene.Node) []byte {
	t.Helper()
	b, ok := serializeDeltaV3(changes, root, 800, 600)
	if !ok {
		t.Fatal("serializeDeltaV3 回退全量，测试前提失败")
	}
	return b
}

func TestGRX3FrameMirrorRoundtrip(t *testing.T) {
	mirrorReset()
	root := buildTree(100)
	if rc := mirrorApply(mustFrameV3(t, root)); rc != 0 {
		t.Fatalf("GRX3 全量应用失败 rc=%d", rc)
	}
	if got := mirrorNodeCount(); got != 100 {
		t.Fatalf("GRX3 镜像节点数=%d, want 100", got)
	}
	dump := mirrorDump()
	// GRX3 帧不携带 id 字符串 → 镜像摘要按 nid 十进制回显。
	if !strings.Contains(dump, "50:n50") || !strings.Contains(dump, "99:n99") {
		t.Fatalf("GRX3 镜像缺文本/身份: %.200s", dump)
	}
}

func TestGRX3FrameOmitsIDStrings(t *testing.T) {
	// GRX3 的 id 池槽为空串：帧字节不应包含旧协议的十进制 id 原串条目。
	root := &gxscene.Node{Type: gxscene.NodeDiv, ID: 777, Kind: "div", Text: ""}
	b := mustFrameV3(t, root)
	s := string(b)
	if strings.Contains(s, "777") {
		t.Fatalf("GRX3 帧不应携带 id 字符串 777（应走 u64 直传）")
	}
}

func TestGRX3DeltaMirrorPatch(t *testing.T) {
	mirrorReset()
	// 树：root(ID=1) + 三个文本子节点 ID=2,3,4。
	root := &gxscene.Node{Type: gxscene.NodeDiv, ID: 1, Kind: "div", Children: []*gxscene.Node{
		{Type: gxscene.NodeText, ID: 2, Text: "alpha"},
		{Type: gxscene.NodeText, ID: 3, Text: "beta"},
		{Type: gxscene.NodeText, ID: 4, Text: "gamma"},
	}}
	if rc := mirrorApply(mustFrameV3(t, root)); rc != 0 {
		t.Fatalf("GRX3 全量建树失败 rc=%d", rc)
	}
	if got := mirrorNodeCount(); got != 4 {
		t.Fatalf("建树节点数=%d, want 4", got)
	}

	// 增量：改 beta 文本、删 alpha、加 delta（插到 beta 前）。
	deltaNode := &gxscene.Node{Type: gxscene.NodeText, ID: 5, Text: "delta"}
	changes := []gxscene.Change{
		{Kind: gxscene.ChangeSetText, NodeID: "3", Value: "beta2"},
		{Kind: gxscene.ChangeRemoveChild, NodeID: "1", ChildID: "2"},
		{Kind: gxscene.ChangeInsertBefore, NodeID: "1", ChildID: "5", BeforeID: "3", Value: deltaNode},
	}
	if rc := mirrorApply(mustDeltaV3(t, changes, root)); rc != 0 {
		t.Fatalf("GRX3 delta 应用失败 rc=%d", rc)
	}
	if got := mirrorNodeCount(); got != 4 {
		t.Fatalf("增量后节点数=%d, want 4（-1 +1）", got)
	}
	dump := mirrorDump()
	for _, want := range []string{"beta2", "delta", "gamma"} {
		if !strings.Contains(dump, want) {
			t.Errorf("增量后镜像缺 %q: %.200s", want, dump)
		}
	}
	if strings.Contains(dump, "alpha") {
		t.Errorf("被删除的 alpha 不应残留: %.200s", dump)
	}
	// 结构顺序：delta 在 beta2 前，gamma 末尾。
	if i := strings.Index(dump, "delta"); i >= 0 {
		if j := strings.Index(dump, "beta2"); j >= 0 && j < i {
			t.Errorf("delta 应插在 beta2 之前: %.200s", dump)
		}
	}
}

func TestGRX3DeltaFallsBackOnNonNumericID(t *testing.T) {
	// 非数字 id 无法 GRX3 直传 → ok=false（调用方回退全量），不能静默丢动作。
	root := &gxscene.Node{Type: gxscene.NodeDiv, ID: 1, Kind: "div"}
	changes := []gxscene.Change{
		{Kind: gxscene.ChangeSetText, NodeID: "not-a-number", Value: "x"},
	}
	if _, ok := serializeDeltaV3(changes, root, 800, 600); ok {
		t.Fatal("非数字 NodeID 应导致 GRX3 delta 编码失败（回退全量）")
	}
}

func TestGRX3EmptyIDMeansNoNode(t *testing.T) {
	// BeforeID="" = NoNode = 0 = 追加末尾（与 GRX2 语义对齐）。
	mirrorReset()
	root := &gxscene.Node{Type: gxscene.NodeDiv, ID: 1, Kind: "div", Children: []*gxscene.Node{
		{Type: gxscene.NodeText, ID: 2, Text: "a"},
	}}
	if rc := mirrorApply(mustFrameV3(t, root)); rc != 0 {
		t.Fatalf("建树失败 rc=%d", rc)
	}
	added := &gxscene.Node{Type: gxscene.NodeText, ID: 3, Text: "b"}
	changes := []gxscene.Change{
		{Kind: gxscene.ChangeInsertBefore, NodeID: "1", ChildID: "3", BeforeID: "", Value: added},
	}
	if rc := mirrorApply(mustDeltaV3(t, changes, root)); rc != 0 {
		t.Fatalf("delta 应用失败 rc=%d", rc)
	}
	dump := strings.TrimRight(mirrorDump(), "\n")
	if !strings.HasSuffix(dump, "3:b") {
		t.Fatalf("空锚点（NoNode=0）应把节点追加到末尾: %.200s", dump)
	}
}
