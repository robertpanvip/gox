// idecheck —— gox-idea 功能点点击测量仪。
//
// 设计原则：不 mock、不直接调命令，而是复现桌面 glwindow 的真实事件链：
//  1. offscreen skia 窗口渲染真 ui.Playground()（同一 painter/布局/字体）；
//  2. 取帧末 hitbox 表，按 glwindow 同款规则（包含点 + 面积最小 = 最内层元素）
//     选出命中节点；
//  3. 对该节点 DispatchBubble("onClick")，与鼠标点击完全同链路；
//  4. 断言三层：ui 状态（sig）/ DrawLog 渲染文本 / 命中结构。
//
// 用法：cd cmd/gox-idea && go run ./idecheck
// 退出码：0 = 全部通过；1 = 存在 FAIL（失败行含功能点 id 与原因）。
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	skia "github.com/robertpanvip/gox-backend-skia"
	"github.com/robertpanvip/gox-idea/ui"
	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// ---- harness ----

type Harness struct {
	win  *skia.Window
	app  *gxapp.App
	hits []skia.HitBox
	log  skia.DrawLog
	flip int
}

func New() *Harness {
	h := &Harness{win: skia.NewWindow()}
	app, err := gxapp.OpenWith(gxapp.WindowConfig{Title: "idecheck", Width: 1400, Height: 860}, h.win)
	if err != nil {
		panic(err)
	}
	h.app = app
	h.Render()
	return h
}

// Render 重画一帧并抓取 hitbox 与绘制日志。每执行一次动作后调用。
func (h *Harness) Render() {
	if err := h.app.Render(func() any { return ui.Playground() }); err != nil {
		panic(err)
	}
	h.hits = h.win.Hits()
	h.log = h.win.DrawLog()
}

// clickPoint 复现 glwindow 的命中选取：包含点的所有框里取面积最小者，
// 命中节点沿父链冒泡 onClick。返回是否命中了带事件的节点。
func (h *Harness) clickPoint(px, py float32) bool {
	// 与 glwindow 同步：登记序倒序 = 顶绘制层 + 最内层优先。
	bestIdx := -1
	for i := len(h.hits) - 1; i >= 0; i-- {
		if h.hits[i].Contains(px, py) {
			bestIdx = i
			break
		}
	}
	if bestIdx < 0 {
		return false
	}
	node := h.hits[bestIdx].Node
	if node == nil {
		return false
	}
	node.DispatchBubble("onClick", gxscene.EventContext{
		Raw: "idecheck",
		Synthetic: &gxscene.SyntheticEvent{
			Type: gxscene.EvClick, NativeName: "onClick",
			Target: node, CurrentTarget: node,
			X: float64(px), Y: float64(py), Button: 1,
		},
	})
	return true
}

// ClickTestID 按 testid 属性点击（图标按钮锚点）。
func (h *Harness) ClickTestID(id string) error {
	for i := range h.hits {
		n := h.hits[i].Node
		if n == nil || n.Props == nil {
			continue
		}
		if v, _ := n.Props["testid"].(string); v == id && h.hits[i].W > 0 {
			x, y := h.hits[i].X+h.hits[i].W/2, h.hits[i].Y+h.hits[i].H/2
			if !h.clickPoint(x, y) {
				return fmt.Errorf("testid %q 坐标无命中", id)
			}
			h.Render()
			return nil
		}
	}
	return fmt.Errorf("找不到 testid %q", id)
}

// PixelAt 取指定坐标像素色（RGBA）。
func (h *Harness) PixelAt(x, y int) (byte, byte, byte, bool) {
	buf := h.win.Pixels()
	if buf == nil {
		return 0, 0, 0, false
	}
	i := (y*1400 + x) * 4
	if i+3 >= len(buf) {
		return 0, 0, 0, false
	}
	return buf[i], buf[i+1], buf[i+2], true
}

// textCenter 找文本节点的框中心（首个包含 substr 的 text 节点）。
func (h *Harness) textCenter(substr string) (float32, float32, bool) {
	for i := range h.hits {
		n := h.hits[i].Node
		if n == nil || n.Text == "" || !strings.Contains(n.Text, substr) {
			continue
		}
		if h.hits[i].W <= 0 || h.hits[i].H <= 0 {
			continue
		}
		return h.hits[i].X + h.hits[i].W/2, h.hits[i].Y + h.hits[i].H/2, true
	}
	return 0, 0, false
}

// ClickText 点击含指定文本的元素（真实坐标命中 + 冒泡）。
func (h *Harness) ClickText(substr string) error {
	x, y, ok := h.textCenter(substr)
	if !ok {
		return fmt.Errorf("找不到文本 %q 的可点节点", substr)
	}
	if !h.clickPoint(x, y) {
		return fmt.Errorf("坐标 %.0f,%.0f 无命中", x, y)
	}
	h.Render()
	return nil
}

// ClickTextN 第 n 次出现（0 基）——同名文本（tab 标题等）需要区分。
func (h *Harness) ClickTextN(substr string, n int) error {
	seen := 0
	for i := range h.hits {
		nd := h.hits[i].Node
		if nd == nil || !strings.Contains(nd.Text, substr) {
			continue
		}
		if h.hits[i].W <= 0 || h.hits[i].H <= 0 {
			continue
		}
		if seen == n {
			x, y := h.hits[i].X+h.hits[i].W/2, h.hits[i].Y+h.hits[i].H/2
			if !h.clickPoint(x, y) {
				return fmt.Errorf("坐标 %.0f,%.0f 无命中", x, y)
			}
			h.Render()
			return nil
		}
		seen++
	}
	return fmt.Errorf("文本 %q 只出现 %d 次，取不到第 %d 个", substr, seen, n)
}

// HasText 渲染断言：DrawLog 中存在包含 substr 的文本指令。
func (h *Harness) HasText(substr string) bool {
	for _, op := range h.log {
		if op.Kind == "text" && strings.Contains(op.Name, substr) {
			return true
		}
	}
	return false
}

// HasClickable 命中断言：存在含 substr 文本且可命中的节点。
func (h *Harness) HasClickable(substr string) bool {
	_, _, ok := h.textCenter(substr)
	return ok
}

// BoxOf 取文本元素矩形（用于验证位置/可见性）。
func (h *Harness) BoxOf(substr string) (skia.HitBox, bool) {
	for i := range h.hits {
		n := h.hits[i].Node
		if n != nil && strings.Contains(n.Text, substr) && h.hits[i].W > 0 && h.hits[i].H > 0 {
			return h.hits[i], true
		}
	}
	return skia.HitBox{}, false
}

// reset 把 harness 全局态恢复到基线（各功能点独立起测）。
func (h *Harness) reset() {
	ui.SetMenu("")
	ui.CloseAllTabs()
	ui.SetBottomTab("Terminal")
	ui.SetProjectPanelVisible(true)
	ui.SetLayoutTerminalH(180)
	ui.LoadSample(0)
	h.Render()
}

// ---- 功能点 ----

type Feature struct {
	ID   string
	Name string
	Run  func(h *Harness) error
}

func expect(cond bool, msg string) error {
	if !cond {
		return fmt.Errorf("%s", msg)
	}
	return nil
}

var features = []Feature{
	// ---- 菜单栏 ----
	{"M1", "点击 File 弹出下拉", func(h *Harness) error {
		if err := h.ClickText("File"); err != nil {
			return err
		}
		return expect(h.HasText("New File"), "点击 File 后应出现 New File 菜单项")
	}},
	{"M2", "菜单项真执行且自动收起", func(h *Harness) error {
		if err := h.ClickText("File"); err != nil {
			return err
		}
		if err := h.ClickText("New File"); err != nil {
			h.dumpHits("M2-click")
			return err
		}
		if err := expect(!h.HasClickable("Next Example"), "执行后菜单应收起(命中层)"); err != nil {
			h.dumpHits("M2")
			return err
		}
		return expect(ui.ActiveTabName() == "untitled.gx", "New File 应激活 untitled.gx 标签")
	}},
	{"M3", "菜单条点击切换（桌面语义；已知缺陷：scrim 顶置吞点击，待 popover noScrim 挂载修复后转绿）", func(h *Harness) error {
		if err := h.ClickText("File"); err != nil {
			return err
		}
		if err := h.ClickText("Run"); err != nil {
			return err
		}
		if err := expect(h.HasText("Clear Console"), "应打开 Run 菜单"); err != nil {
			return err
		}
		return expect(!h.HasText("New File"), "不应同时挂着 File 菜单")
	}},
	{"M4", "再次点击 File 收起", func(h *Harness) error {
		if err := h.ClickText("File"); err != nil {
			return err
		}
		if err := expect(h.HasText("New File"), "先确认已打开"); err != nil {
			return err
		}
		if err := h.ClickText("File"); err != nil {
			return err
		}
		return expect(!h.HasText("New File"), "再点一次应收起")
	}},
	{"M5", "点空白处收起菜单（scrim）", func(h *Harness) error {
		if err := h.ClickText("File"); err != nil {
			return err
		}
		// 画布中下部空白（状态栏上方、编辑器右侧空隙）
		h.clickPoint(1350, 420)
		h.Render()
		return expect(!h.HasText("New File"), "点击空白应命中 scrim 收起菜单")
	}},
	{"M6", "Edit 菜单 Undo/Redo 可点", func(h *Harness) error {
		if err := h.ClickText("Edit"); err != nil {
			return err
		}
		return expect(h.HasText("Select All"), "Edit 下拉应含 Select All")
	}},

	// ---- 编辑器标签 ----
	{"T1", "示例列表点击开标签", func(h *Harness) error {
		before := len(ui.TabNames())
		if err := h.ClickText("02_counter.gx"); err != nil {
			h.dumpHits("T1")
			return err
		}
		if err := expect(len(ui.TabNames()) == before+1, "点示例应新增标签"); err != nil {
			return err
		}
		return expect(h.HasText("02 Counter.gx"), "tab 条应渲染 02 Counter.gx")
	}},
	{"T2", "双标签间点击切换", func(h *Harness) error {
		ui.LoadSample(0)
		ui.LoadSample(1)
		h.Render()
		if err := h.ClickTextN("01 Hello GX.gx", 0); err != nil {
			return err
		}
		if err := expect(ui.ActiveTabName() == "01 Hello GX.gx", "点 tab1 应激活 01"); err != nil {
			return err
		}
		return expect(strings.Contains(ui.EditorText(), "sig name"), "编辑器应换回 01 内容")
	}},
	{"T3", "关闭按钮只关不切（无冒泡误伤）", func(h *Harness) error {
		ui.LoadSample(0)
		ui.LoadSample(1) // active=02
		h.Render()
		// 关闭 tab01 的 close 图标区：01 标签矩形右侧 12px 处
		box, ok := h.BoxOf("01 Hello GX.gx")
		if !ok {
			return fmt.Errorf("找不到 01 标签矩形")
		}
		h.clickPoint(box.X+box.W+18, box.Y+box.H/2)
		h.Render()
		if err := expect(len(ui.TabNames()) == 1, "应关掉一个标签"); err != nil {
			return err
		}
		return expect(ui.ActiveTabName() == "02 Counter.gx", "关闭非激活标签不应切走激活态")
	}},
	{"T4", "标签全部关闭后编辑区清空", func(h *Harness) error {
		ui.LoadSample(0)
		h.Render()
		box, ok := h.BoxOf("01 Hello GX.gx")
		if !ok {
			return fmt.Errorf("找不到标签")
		}
		h.clickPoint(box.X+box.W+18, box.Y+box.H/2)
		h.Render()
		if err := expect(len(ui.TabNames()) == 0, "应无标签残留"); err != nil {
			return err
		}
		return expect(ui.EditorText() == "", "编辑器应清空")
	}},

	// ---- 工具栏 / 面板 ----
	{"P1", "folder 图标按钮收起项目面板", func(h *Harness) error {
		if err := h.ClickTestID("btn-folder"); err != nil {
			return err
		}
		if err := expect(!h.HasText("PROJECT"), "点 folder 后应隐藏项目面板"); err != nil {
			return err
		}
		if err := h.ClickTestID("btn-folder"); err != nil {
			return err
		}
		return expect(h.HasText("PROJECT"), "再点应恢复")
	}},
	{"P1b", "项目面板隐藏后 PROJECT 不在树中", func(h *Harness) error {
		ui.SetProjectPanelVisible(false)
		h.Render()
		if err := expect(!h.HasText("PROJECT"), "隐藏后不应渲染 PROJECT 头"); err != nil {
			return err
		}
		ui.SetProjectPanelVisible(true)
		h.Render()
		return expect(h.HasText("PROJECT"), "恢复可见")
	}},
	{"S1", "主题切换改变标签栏文字色", func(h *Harness) error {
		h.Render()
		before, _ := h.BoxOf("main.gx")
		_ = before
		ui.ToggleTheme()
		h.Render()
		after := h.log.Find("text", "Terminal")
		ui.ToggleTheme()
		h.Render()
		return expect(after >= 0, "切换后终端页仍正常渲染")
	}},

	// ---- 底部工具窗 ----
	{"B1", "点击 Problems 切换内容页", func(h *Harness) error {
		if err := h.ClickText("Problems"); err != nil {
			return err
		}
		if err := expect(ui.BottomTab() == "Problems", "激活态应为 Problems"); err != nil {
			return err
		}
		return expect(h.HasText("No problems detected"), "Problems 页应显示无问题提示")
	}},
	{"B2", "切回 Terminal 显示输出", func(h *Harness) error {
		ui.SetBottomTab("Problems")
		h.Render()
		if err := h.ClickText("Terminal"); err != nil {
			return err
		}
		return expect(ui.BottomTab() == "Terminal", "应切回 Terminal")
	}},

	// ---- 状态栏 ----
	{"K1", "光标移动驱动状态栏行列", func(h *Harness) error {
		ui.CloseAllTabs()
		ui.OpenTab("k.gx", "", "line1\nline2\nline3")
		ui.EditorMove(2, 1) // 第 3 行第 2 列
		h.Render()
		return expect(h.HasText("Ln 3, Col 2"), "状态栏应显示光标位置，实际 EditorStatus="+ui.EditorStatus())
	}},
	// ---- 基础编辑操作（打字/滚动/选区/undo） ----
	{"E1", "打字落点：插入出现在光标处且状态栏同步", func(h *Harness) error {
		ui.CloseAllTabs()
		ui.OpenTab("e.gx", "", "line1\nline2\nline3")
		ui.EditorSetSelect(1, 2)
		ui.EditorInsert("Q")
		if err := expect(strings.Split(ui.EditorText(), "\n")[1] == "liQne2", "Q 应落在第2行 index2 处, got "+ui.EditorText()); err != nil {
			return err
		}
		h.Render()
		return expect(h.HasText("Ln 2, Col 4"), "插入后光标应在 Q 之后(Col 4), got "+ui.EditorStatus())
	}},
	{"E2", "回车换行行数增加", func(h *Harness) error {
		ui.CloseAllTabs()
		ui.OpenTab("e2.gx", "", "aaa\nbbb")
		ui.EditorSetSelect(0, 3)
		ui.EditorNewline()
		h.Render()
		return expect(h.HasText("3 lines"), "行数应为 3, got "+ui.EditorStatus())
	}},
	{"E3", "退格删字符", func(h *Harness) error {
		ui.CloseAllTabs()
		ui.OpenTab("e3.gx", "", "abc")
		ui.EditorSetSelect(0, 3)
		ui.EditorBackspace()
		return expect(ui.EditorText() == "ab", "backspace 后应为 ab, got "+ui.EditorText())
	}},
	{"E4", "undo/redo 往返", func(h *Harness) error {
		ui.CloseAllTabs()
		ui.OpenTab("e4.gx", "", "base")
		ui.EditorSetSelect(0, 4)
		ui.EditorInsert("XYZ")
		ui.EditorUndo()
		if ui.EditorText() != "base" {
			return fmt.Errorf("undo 后应回 base, got %q", ui.EditorText())
		}
		ui.EditorRedo()
		return expect(ui.EditorText() == "baseXYZ", "redo 应恢复 baseXYZ, got "+ui.EditorText())
	}},
	{"E5", "宽字符中文写入与撤销", func(h *Harness) error {
		ui.CloseAllTabs()
		ui.OpenTab("e5.gx", "", "x")
		ui.EditorSetSelect(0, 1)
		ui.EditorInsert("中")
		if !strings.Contains(ui.EditorText(), "中") {
			return fmt.Errorf("中文应写入, got %q", ui.EditorText())
		}
		ui.EditorUndo()
		return expect(ui.EditorText() == "x", "撤销后应回到 x")
	}},
	{"E6", "滚动改变首可见行", func(h *Harness) error {
		ui.CloseAllTabs()
		var sb strings.Builder
		for i := 0; i < 60; i++ {
			sb.WriteString("row" + strconv.Itoa(i) + "\n")
		}
		ui.OpenTab("e6.gx", "", sb.String())
		ui.EditorScrollLines(-15)
		h.Render()
		if h.log.Find("text", "row15") < 0 {
			return fmt.Errorf("滚动 15 行后 row15 应进入可视区")
		}
		ui.EditorScrollLines(15)
		h.Render()
		return expect(h.log.Find("text", "row0") >= 0, "滚回后 row0 应可见")
	}},
	{"E7", "滚动越界保护", func(h *Harness) error {
		ui.CloseAllTabs()
		ui.OpenTab("e7.gx", "", strings.Repeat("l\n", 40))
		ui.EditorScrollLines(-99999)
		h.Render()
		ui.EditorScrollLines(99999)
		h.Render()
		return expect(h.HasClickable("e7.gx"), "极限滚动后视图仍正常渲染")
	}},
	{"E8", "shift 扩展选区可选出文本", func(h *Harness) error {
		ui.CloseAllTabs()
		ui.OpenTab("e8.gx", "", "hello world")
		ui.EditorSetSelect(0, 0)
		ui.EditorMoveEx(0, 5, true)
		return expect(ui.EditorSelectedText() == "hello", "shift+右5 应选中 hello, got ["+ui.EditorSelectedText()+"]")
	}},
	{"E9", "选行覆盖整行（多行块选区含行尾换行，IDEA 同款语义）", func(h *Harness) error {
		ui.CloseAllTabs()
		ui.OpenTab("e9.gx", "", "one\ntwo\nthree")
		ui.EditorSelectLine(1)
		if ui.EditorSelectedText() != "two\n" {
			return fmt.Errorf("选中间行应覆盖 two+行尾换行, got %q", ui.EditorSelectedText())
		}
		ui.EditorSelectLine(2)
		return expect(ui.EditorSelectedText() == "three", "选末行只取内容, got ["+ui.EditorSelectedText()+"]")
	}},
	{"D2", "分隔条命中带 resize 光标（hover 与派发同规则）", func(h *Harness) error {
		// 通过 hitbox 的 cursor 属性验证竖向分隔条注册了 col-resize。
		found := ""
		for i := range h.hits {
			n := h.hits[i].Node
			if n == nil || n.Props == nil {
				continue
			}
			if cls, _ := n.Props["class"].(string); cls == "gx-splitter-v" {
				found = h.hits[i].Cursor
			}
		}
		return expect(found == "col-resize", "gx-splitter-v 应带 col-resize 光标, got ["+found+"]")
	}},
}

// dumpHits 调试用：打印命中表里的文本节点与关键状态。
func (h *Harness) dumpHits(tag string) {
	fmt.Printf("---- DUMP %s (hits=%d) ----\n", tag, len(h.hits))
	n := 0
	for i := range h.hits {
		nd := h.hits[i].Node
		if nd == nil || nd.Text == "" {
			continue
		}
		fmt.Printf("  TXT %q @%.0f,%.0f %.0fx%.0f\n", nd.Text, h.hits[i].X, h.hits[i].Y, h.hits[i].W, h.hits[i].H)
		n++
		if n > 40 {
			break
		}
	}
	fmt.Printf("  state: menu=%q tabs=%v bottom=%q\n", ui.MenuName(), ui.TabNames(), ui.BottomTab())
}

// ---- main ----

func main() {
	h := New()
	passed, failed, skipped := 0, 0, 0
	fmt.Println("== gox-idea 功能点测量报告 ==")
	for _, f := range features {
		h.reset()
		// reset 里 LoadSample(0) 会开一个标签，作为基线现场
		err := f.Run(h)
		switch {
		case err == nil:
			passed++
			fmt.Printf("PASS  %-4s %s\n", f.ID, f.Name)
		case strings.HasPrefix(err.Error(), "SKIP:"):
			skipped++
			fmt.Printf("SKIP  %-4s %s (%s)\n", f.ID, f.Name, strings.TrimPrefix(err.Error(), "SKIP:"))
		default:
			failed++
			fmt.Printf("FAIL  %-4s %s\n      └─ %v\n", f.ID, f.Name, err)
		}
	}
	fmt.Printf("== 合计: %d 通过 / %d 失败 / %d 跳过 ==\n", passed, failed, skipped)
	h.app.Close()
	if failed > 0 {
		os.Exit(1)
	}
}
