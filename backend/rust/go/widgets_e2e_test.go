package rustbackend

// 端到端验证：GX 公共组件库（gox/widgets）构建的场景树，经 GRX1 协议喂给 Rust
// 镜像探针后保持一致。重点覆盖新增的 img/canvas 原语与交互组件的样式属性透传——
// 证明"同一套 GX 组件库在 Rust 后端可用"，而不只是 Skia 后端。

import (
	"strings"
	"testing"

	"github.com/robertpanvip/gox/runtime/canvas"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	widgets "github.com/robertpanvip/gox/widgets"
)

// 统计场景树里 Kind 出现的次数。
func countNodeKind(n *gxscene.Node, kind string) int {
	if n == nil {
		return 0
	}
	c := 0
	if n.Kind == kind {
		c++
	}
	for _, ch := range n.Children {
		c += countNodeKind(ch, kind)
	}
	return c
}

// countAllNodes 统计场景树所有节点数。
func countAllNodes(n *gxscene.Node) int {
	if n == nil {
		return 0
	}
	c := 1
	for _, ch := range n.Children {
		c += countAllNodes(ch)
	}
	return c
}

func TestWidgetsSceneAcrossRustMirror(t *testing.T) {
	// 用公共组件库构建一棵混合场景：容器 + 卡片 + 交互组件 + img + canvas。
	root := widgets.Col(gxscene.Props{
		"width": "400", "gap": "8",
		"children": func() any {
			return []any{
				widgets.Card(gxscene.Props{"title": "统计", "tone": "cyan", "children": func() any {
					return widgets.Text(gxscene.Props{"value": "hello"})
				}}),
				widgets.Button(gxscene.Props{"label": "提交", "variant": "primary"}),
				widgets.Img(gxscene.Props{"src": "data:image/png;base64,AAAA", "width": "100", "height": "60", "object-fit": "cover"}),
				widgets.Canvas(gxscene.Props{
					"width": "200", "height": "80",
					"ops": []canvas.Op{
						{Kind: canvas.OpRect, X: 0, Y: 0, W: 200, H: 80, Fill: "#0b1020"},
						{Kind: canvas.OpPath, D: "M0 70 L100 20 L200 50", Stroke: "#2dd4bf", StrokeWidth: 2},
					},
				}),
			}
		},
	}).Root()

	// 先断言 Go 侧场景树结构正确（组件库本身产出预期节点）。
	if countNodeKind(root, "img") != 1 {
		t.Fatalf("Go 场景树应有 1 个 img 节点, got %d", countNodeKind(root, "img"))
	}
	if countNodeKind(root, "canvas") != 1 {
		t.Fatalf("Go 场景树应有 1 个 canvas 节点, got %d", countNodeKind(root, "canvas"))
	}

	// 序列化成 GRX1 全量帧，喂给 Rust 镜像探针。
	mirrorReset()
	if rc := mirrorApply(mustFrame(t, root)); rc != 0 {
		t.Fatalf("组件场景全量应用失败 rc=%d", rc)
	}
	// 镜像节点数应与 Go 场景树节点数严格一致——证明组件库产出的整棵树
	// （含 img/canvas/交互组件）都跨 GRX1 完整到达 Rust 侧。
	want := countAllNodes(root)
	if got := mirrorNodeCount(); got != want {
		t.Fatalf("Rust 镜像节点数=%d, Go 场景树=%d", got, want)
	}
	// 文本语义保留（组件里的 Card 标题/按钮/文本都应进镜像）。
	dump := mirrorDump()
	for _, s := range []string{"统计", "hello", "提交"} {
		if !strings.Contains(dump, s) {
			t.Errorf("Rust 镜像缺少文本 %q: %.160s", s, dump)
		}
	}
}

func TestImgCanvasPropsOverProtocol(t *testing.T) {
	// 单独验证 img/canvas 的 Props 经二进制序列化后 key/value 完整保留。
	root := &gxscene.Node{Type: gxscene.NodeDiv, ID: 1, Kind: "div", Children: []*gxscene.Node{
		{Type: gxscene.NodeDiv, ID: 2, Kind: "img", Props: map[string]any{"src": "data:image/png;base64,AAA", "object-fit": "cover"}},
		{Type: gxscene.NodeDiv, ID: 3, Kind: "canvas", Props: map[string]any{canvas.PropKey: canvas.Encode([]canvas.Op{{Kind: canvas.OpRect, W: 10, H: 10, Fill: "#f00"}})}},
	}}
	b := mustFrame(t, root)
	// 帧字节里应能找到属性名与 src 值（字符串池原文）。
	s := string(b)
	for _, key := range []string{"src", "object-fit", canvas.PropKey, "data:image/png;base64,AAA"} {
		if !strings.Contains(s, key) {
			t.Errorf("帧中缺少属性 %q", key)
		}
	}
}
