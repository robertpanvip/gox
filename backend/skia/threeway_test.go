package skia

// 三端一致性矩阵测试：读取 testdata/threeway/cases.json 的共享用例，
// 用无头 Window 渲染每个场景，把 DrawLog / 命中框转成与 Rust 端同构的
// 观测面（rect / text / order / hit / cursor），按用例的 expect 断言。
//
// 运行方式：
//
//	go test ./backend/skia -run TestThreeWayMatrix
//
// 环境变量（可选）：GX_THREEWAY_CASES 覆盖用例路径；
// GX_THREEWAY_OUT 指定观测结果 JSON 输出路径（scripts/threeway-parity.sh 汇总用）。
//
// 断言语义与 docs/STYLE.md 第 14 节「三端消费矩阵」一致：这里任何一条
// rect/hit/text 失败都意味着 Skia 端该属性未实现或传值解析出错。

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	gxapp "github.com/robertpanvip/gox-core/runtime/app"
	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
)

type twNode struct {
	Kind     string            `json:"kind"` // div | text
	Text     string            `json:"text"`
	Props    map[string]string `json:"props"`
	Name     string            `json:"name"` // → gx-name 属性
	Children []twNode          `json:"children"`
}

type twExpect struct {
	Kind   string  `json:"kind"` // rect | text | text-dx | text-dy | order | hit | style | css
	Bg     string  `json:"bg"`
	Text   string  `json:"text"`
	Anchor string  `json:"anchor"`
	X      float64 `json:"x"`
	Y      float64
	W      float64 `json:"w"`
	H      float64
	Dx     float64  `json:"dx"`
	Dy     float64  `json:"dy"`
	Tol    float64  `json:"tol"`
	Bgs    []string `json:"bgs"`
	Target string   `json:"target"`
	Cursor string   `json:"cursor"`
}

type twCase struct {
	Name   string     `json:"name"`
	Scene  twNode     `json:"scene"`
	Expect []twExpect `json:"expect"`
}

type twSuite struct {
	Viewport []int    `json:"viewport"`
	Cases    []twCase `json:"cases"`
}

func twLoadCases(t *testing.T) twSuite {
	t.Helper()
	path := os.Getenv("GX_THREEWAY_CASES")
	if path == "" {
		path = "../../testdata/threeway/cases.json"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取用例 %s: %v", path, err)
	}
	var suite twSuite
	if err := json.Unmarshal(raw, &suite); err != nil {
		t.Fatalf("解析用例 %s: %v", path, err)
	}
	return suite
}

// twBuildNode 把 JSON 节点转成场景树。props 原样字符串透传（与 GRX 协议一致）。
func twBuildNode(n twNode) *gxscene.Node {
	node := &gxscene.Node{}
	if n.Kind == "text" {
		node.Type = gxscene.NodeText
		node.Text = n.Text
	} else {
		node.Type = gxscene.NodeDiv
		node.Kind = "div"
	}
	if len(n.Props) > 0 || n.Name != "" {
		node.Props = map[string]any{}
		for k, v := range n.Props {
			node.Props[k] = v
		}
		if n.Name != "" {
			node.Props["gx-name"] = n.Name
		}
	}
	for _, c := range n.Children {
		node.Children = append(node.Children, twBuildNode(c))
	}
	return node
}

func twHex(argb uint32) string {
	return fmt.Sprintf("#%06x", argb&0xffffff)
}

// twObservations 抽取一帧的可观测面。
type twObservations struct {
	Rects []twRect `json:"rects"`
	Texts []twText `json:"texts"`
	Hits  []twHit  `json:"hits"`
}

type twRect struct {
	Bg      string  `json:"bg"`
	X       float32 `json:"x"`
	Y, W, H float32
}

type twText struct {
	Text string  `json:"text"`
	X    float32 `json:"x"`
	Y    float32
}

type twHit struct {
	Bg      string  `json:"bg"`
	Cursor  string  `json:"cursor"`
	X       float32 `json:"x"`
	Y, W, H float32
}

func twObserve(w *Window) twObservations {
	obs := twObservations{}
	for _, op := range w.log {
		switch op.Kind {
		case "rect":
			obs.Rects = append(obs.Rects, twRect{Bg: twHex(op.ARGB), X: op.X, Y: op.Y, W: op.W, H: op.H})
		case "text":
			obs.Texts = append(obs.Texts, twText{Text: op.Name, X: op.X, Y: op.Y})
		}
	}
	hits := w.Hits()
	for i := len(hits) - 1; i >= 0; i-- { // 逆序 = 自顶向下命中序
		h := hits[i]
		bg := ""
		if h.Node != nil && h.Node.Props != nil {
			bg, _ = h.Node.Props["bg"].(string)
		}
		obs.Hits = append(obs.Hits, twHit{Bg: bg, Cursor: h.Cursor, X: h.X, Y: h.Y, W: h.W, H: h.H})
	}
	return obs
}

func (o *twObservations) firstRect(bg string) (twRect, bool) {
	for _, r := range o.Rects {
		if r.Bg == bg {
			return r, true
		}
	}
	return twRect{}, false
}

func (o *twObservations) firstText(s string) (twText, bool) {
	for _, tx := range o.Texts {
		if tx.Text == s {
			return tx, true
		}
	}
	return twText{}, false
}

// hitAt 按 twObserve 的逆序（自顶向下）找第一个包含该点的命中框。
func (o *twObservations) hitAt(x, y float32) (twHit, bool) {
	for _, h := range o.Hits {
		if x >= h.X && x < h.X+h.W && y >= h.Y && y < h.Y+h.H {
			return h, true
		}
	}
	return twHit{}, false
}

func twEvalSkia(c twCase, o *twObservations) []string {
	var fails []string
	for _, e := range c.Expect {
		switch e.Kind {
		case "rect":
			r, ok := o.firstRect(e.Bg)
			if !ok {
				fails = append(fails, fmt.Sprintf("rect %s 未绘制（属性未消费或颜色未透传）", e.Bg))
				continue
			}
			if d := abs(r.X - float32(e.X)); d > float32(e.Tol) {
				fails = append(fails, fmt.Sprintf("rect %s x=%.1f want %.1f", e.Bg, r.X, e.X))
			}
			if d := abs(r.Y - float32(e.Y)); d > float32(e.Tol) {
				fails = append(fails, fmt.Sprintf("rect %s y=%.1f want %.1f", e.Bg, r.Y, e.Y))
			}
			if d := abs(r.W - float32(e.W)); d > float32(e.Tol) {
				fails = append(fails, fmt.Sprintf("rect %s w=%.1f want %.1f", e.Bg, r.W, e.W))
			}
			if d := abs(r.H - float32(e.H)); d > float32(e.Tol) {
				fails = append(fails, fmt.Sprintf("rect %s h=%.1f want %.1f", e.Bg, r.H, e.H))
			}
		case "text", "text-dx", "text-dy":
			tx, ok := o.firstText(e.Text)
			if !ok {
				fails = append(fails, fmt.Sprintf("text %q 未绘制（文本属性未消费或内容被错误改写）", e.Text))
				continue
			}
			switch e.Kind {
			case "text-dx":
				if d := abs(tx.X - float32(e.X)); d > float32(e.Tol) {
					fails = append(fails, fmt.Sprintf("text %q x=%.1f want %.1f(±%.1f)", e.Text, tx.X, e.X, e.Tol))
				}
			case "text-dy":
				anc, ok := o.firstText(e.Anchor)
				if !ok {
					fails = append(fails, fmt.Sprintf("anchor text %q 未绘制", e.Anchor))
					continue
				}
				if d := abs((tx.Y - anc.Y) - float32(e.Dy)); d > float32(e.Tol) {
					fails = append(fails, fmt.Sprintf("text %q dy=%.1f want %.1f(±%.1f)", e.Text, tx.Y-anc.Y, e.Dy, e.Tol))
				}
			}
		case "order":
			pos := map[string]int{}
			for i, r := range o.Rects {
				if _, seen := pos[r.Bg]; !seen {
					pos[r.Bg] = i
				}
			}
			for i := 1; i < len(e.Bgs); i++ {
				a, okA := pos[e.Bgs[i-1]]
				b, okB := pos[e.Bgs[i]]
				if !okA || !okB {
					fails = append(fails, fmt.Sprintf("order %v: 有 bg 未绘制", e.Bgs))
					break
				}
				if a >= b {
					fails = append(fails, fmt.Sprintf("order: %s 应先于 %s 绘制（z-index 未消费）", e.Bgs[i-1], e.Bgs[i]))
				}
			}
		case "hit":
			h, ok := o.hitAt(float32(e.X), float32(e.Y))
			if e.Target == "" {
				if ok && h.Bg != "" {
					fails = append(fails, fmt.Sprintf("hit(%.0f,%.0f) 命中了 %s，期望空白区", e.X, e.Y, h.Bg))
				}
			} else if !ok || h.Bg != e.Target {
				got := "空白"
				if ok {
					got = h.Bg
				}
				fails = append(fails, fmt.Sprintf("hit(%.0f,%.0f) = %s, want %s（pointer-events 命中豁免或 z 序不符）", e.X, e.Y, got, e.Target))
			}
			if e.Cursor != "" && (!ok || h.Cursor != e.Cursor) {
				got := "无"
				if ok {
					got = h.Cursor
				}
				fails = append(fails, fmt.Sprintf("hit(%.0f,%.0f) cursor = %s, want %s", e.X, e.Y, got, e.Cursor))
			}
		}
	}
	return fails
}

func abs(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}

func TestThreeWayMatrix(t *testing.T) {
	suite := twLoadCases(t)
	vw, vh := 300, 200
	if len(suite.Viewport) == 2 {
		vw, vh = suite.Viewport[0], suite.Viewport[1]
	}

	type caseResult struct {
		Name  string   `json:"name"`
		Pass  bool     `json:"pass"`
		Fails []string `json:"fails,omitempty"`
	}
	report := struct {
		Backend string                    `json:"backend"`
		Cases   []caseResult              `json:"cases"`
		Obs     map[string]twObservations `json:"obs"`
	}{Backend: "skia", Obs: map[string]twObservations{}}

	for _, c := range suite.Cases {
		root := twBuildNode(c.Scene)
		w := NewWindow()
		if err := w.Open(gxapp.WindowConfig{Width: vw, Height: vh}); err != nil {
			t.Fatalf("case %s: 打开窗口: %v", c.Name, err)
		}
		if err := w.Draw(root); err != nil {
			t.Fatalf("case %s: Draw: %v", c.Name, err)
		}
		obs := twObserve(w)
		w.Close()
		report.Obs[c.Name] = obs
		fails := twEvalSkia(c, &obs)
		report.Cases = append(report.Cases, caseResult{Name: c.Name, Pass: len(fails) == 0, Fails: fails})
		for _, f := range fails {
			t.Errorf("[skia] %s: %s", c.Name, f)
		}
	}

	if out := os.Getenv("GX_THREEWAY_OUT"); out != "" {
		raw, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(out, raw, 0o644); err != nil {
			t.Logf("写出 %s 失败: %v（不影响断言）", out, err)
		}
	}
}
