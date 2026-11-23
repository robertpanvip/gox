package rustbackend

import (
	"fmt"
	"strings"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// 本文件验证评审要求的核心命题：
//   GX 动态创建 1000 节点、更新 1 个、删除 100 个、切换条件分支后，
//   Rust 镜像必须始终与 Go Scene 一致。
//
// 做法：Go 侧用真实序列化器产出 GRX1/GRX2 帧，喂给 Rust 导出的纯 CPU 镜像探针
// （与 Renderer::apply_frame 同源的 GRX1/GRX2 消费逻辑），再断言镜像节点数与
// 规范化摘要。这是真正的跨语言端到端验证，而非只测 Go 侧序列化。

func buildTree(n int) *gxscene.Node {
	root := &gxscene.Node{Type: gxscene.NodeDiv, ID: 1, Kind: "div"}
	for i := 2; i < n+1; i++ {
		root.Children = append(root.Children, &gxscene.Node{
			Type: gxscene.NodeText, ID: gxscene.NodeID(i), Text: fmt.Sprintf("n%d", i),
		})
	}
	return root
}

func mustFrame(t *testing.T, root *gxscene.Node) []byte {
	t.Helper()
	b, err := serializeFrameBin(root, 800, 600)
	if err != nil {
		t.Fatalf("serializeFrameBin: %v", err)
	}
	return b
}

func mustDelta(t *testing.T, changes []gxscene.Change, root *gxscene.Node) []byte {
	t.Helper()
	b, ok := serializeDelta(changes, root, 800, 600)
	if !ok {
		t.Fatal("serializeDelta 回退全量，测试前提失败")
	}
	return b
}

// TestMirrorConsistencyDynamicScene 覆盖评审的四个动态场景。
func TestMirrorConsistencyDynamicScene(t *testing.T) {
	mirrorReset()

	// 1) 动态创建 1000 节点 → 全量快照建基线。
	root := buildTree(1000)
	if rc := mirrorApply(mustFrame(t, root)); rc != 0 {
		t.Fatalf("首帧全量应用失败 rc=%d", rc)
	}
	if got := mirrorNodeCount(); got != 1000 {
		t.Fatalf("创建 1000 节点后镜像节点数=%d, want 1000", got)
	}
	dump := mirrorDump()
	if !strings.Contains(dump, "500:n500") || !strings.Contains(dump, "999:n999") {
		t.Fatalf("镜像摘要缺少预期节点文本: %.80s", dump)
	}

	// 2) 更新 1 个节点（SetText）→ 增量，节点数不变、文本更新。
	changes := []gxscene.Change{
		{Kind: gxscene.ChangeSetText, NodeID: "500", Value: "UPDATED"},
	}
	if rc := mirrorApply(mustDelta(t, changes, root)); rc != 0 {
		t.Fatalf("增量应用失败 rc=%d", rc)
	}
	if got := mirrorNodeCount(); got != 1000 {
		t.Fatalf("更新后节点数应不变, got=%d", got)
	}
	if !strings.Contains(mirrorDump(), "500:UPDATED") {
		t.Fatal("增量 SetText 未反映到 Rust 镜像")
	}

	// 3) 删除 100 个节点（RemoveChild）→ 镜像节点数减 100。
	removes := make([]gxscene.Change, 0, 100)
	for i := 900; i < 1000; i++ {
		removes = append(removes, gxscene.Change{
			Kind: gxscene.ChangeRemoveChild, NodeID: "1",
			ChildID: gxscene.NodeID(i).String(),
		})
	}
	if rc := mirrorApply(mustDelta(t, removes, root)); rc != 0 {
		t.Fatalf("批量删除应用失败 rc=%d", rc)
	}
	if got := mirrorNodeCount(); got != 900 {
		t.Fatalf("删除 100 节点后镜像节点数=%d, want 900", got)
	}
	if strings.Contains(mirrorDump(), "999:") {
		t.Fatal("被删除节点仍残留在镜像中")
	}

	// 4) 条件分支切换（ReplaceChildren）→ 子树整体替换，镜像与 Go 树一致。
	branchRoot := &gxscene.Node{Type: gxscene.NodeDiv, ID: 1, Kind: "div"}
	branchRoot.Children = []*gxscene.Node{
		{Type: gxscene.NodeText, ID: 2000, Text: "branch-A"},
		{Type: gxscene.NodeText, ID: 2001, Text: "branch-B"},
	}
	replace := []gxscene.Change{{Kind: gxscene.ChangeReplaceChildren, NodeID: "1"}}
	if rc := mirrorApply(mustDelta(t, replace, branchRoot)); rc != 0 {
		t.Fatalf("分支切换应用失败 rc=%d", rc)
	}
	if got := mirrorNodeCount(); got != 3 { // root + 2 新子节点
		t.Fatalf("分支切换后节点数=%d, want 3", got)
	}
	dump = mirrorDump()
	if !strings.Contains(dump, "branch-A") || strings.Contains(dump, "n500") {
		t.Fatalf("分支切换后镜像内容错误: %s", dump)
	}

	applied, failed := mirrorCounts()
	if failed != 0 {
		t.Fatalf("镜像应用失败次数=%d", failed)
	}
	if applied != 4 {
		t.Fatalf("应应用 4 帧, got=%d", applied)
	}
}

// 无基线时收到增量必须被拒绝（对应 Present 失败后的 FullSync 恢复语义）。
func TestMirrorRejectsDeltaWithoutBaseline(t *testing.T) {
	mirrorReset()
	changes := []gxscene.Change{{Kind: gxscene.ChangeSetText, NodeID: "1", Value: "x"}}
	rc := mirrorApply(mustDelta(t, changes, nil))
	if rc != 1 {
		t.Fatalf("无基线收到增量应返回 1, got=%d", rc)
	}
	// 随后全量恢复应成功。
	if rc := mirrorApply(mustFrame(t, buildTree(3))); rc != 0 {
		t.Fatalf("FullSync 全量恢复应成功, got=%d", rc)
	}
	if got := mirrorNodeCount(); got != 3 {
		t.Fatalf("恢复后节点数=%d, want 3", got)
	}
}
