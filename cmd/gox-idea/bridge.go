// bridge.go —— V2 输入翻译层（ideBridge）。
//
// phase4 迁移把 gox-idea 从模块内 skia/GLFW 装配切到 rustbackend（Host 唯一
// UI Loop），原 glwindow.go 承担的"原始输入 → GX 编辑命令/节点事件"翻译职责
// 由本文件接手：消费 rb.Window.SetDispatcher 派发的 InputEvent，语义与旧
// GLFW 装配层逐条对齐（点击定位、双击选词、三击选行、shift 扩选、拖拽选区、
// 分隔条拖动、快捷键、IME、滚轮），编辑逻辑仍全部在 ui/editor 层，本文件
// 零业务逻辑。
package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	rb "github.com/robertpanvip/gox-backend-rust"
	"github.com/robertpanvip/gox-idea/ui"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// gxAppIface 是 bridge 依赖的应用窄接口（*gxapp.App 满足）。
type gxAppIface interface {
	Instance() *gxscene.Instance
}

type ideBridge struct {
	app      gxAppIface
	win      *rb.Window
	rerender func() // 分隔条拖拽后的完整重绘回调（main 注入）

	// 分隔条拖拽状态
	splitDrag   bool
	splitAxis   string // "v" | "h"
	splitStartX float32
	splitStartY float32
	splitStartW int
	splitStartH int

	// 编辑器拖拽选区状态
	dragging bool

	// 双击/三击模拟（V2 无 dblclick 事件：400ms 内同点位连续按下计数）
	clickTime  time.Time
	clickX     float32
	clickY     float32
	clickCount int
}

// dispatch 实现 rb.Window.SetDispatcher 的回调签名，在 Host 事件泵内同步调用。
func (b *ideBridge) dispatch(ev rb.InputEvent) {
	switch ev.Kind {
	case "pointer_down":
		b.onPointerDown(ev)
	case "pointer_move":
		b.onPointerMove(ev)
	case "pointer_up":
		b.onPointerUp(ev)
	case "click":
		b.onClick(ev)
	case "wheel":
		// winit 约定 y 正=向上滚（Rust 侧原样透传），取负转"向下为正行数"，
		// 与旧 GLFW 装配层 -yoff*3 同源。容器滚动已由渲染端自治路由，
		// 到这里的是编辑器区/无容器兜底。
		ui.EditorScrollLines(-float64(ev.Y) * 3)
	case "key":
		b.onKey(ev)
	case "ime_commit":
		b.onIMECommit(ev)
	case "backspace":
		ui.EditorBackspace()
	}
}

// ---- 鼠标 ----

func (b *ideBridge) onPointerDown(ev rb.InputEvent) {
	if !isLeft(ev) {
		return
	}
	px, py := ev.X, ev.Y
	inputLogf("pointer_down px=%.1f py=%.1f node=%s", px, py, ev.NodeID)

	// 分隔条拖拽优先于编辑器选区（与旧装配层顺序一致）。
	if axis, ok := b.splitterAt(px, py); ok {
		b.splitDrag = true
		b.splitAxis = axis
		b.splitStartX, b.splitStartY = px, py
		if axis == "v" {
			b.splitStartW = ui.LayoutProjectW()
		} else {
			b.splitStartH = ui.LayoutTerminalH()
		}
		ui.SetSplitDragging(true)
		return
	}

	// 编辑器：定点 / Shift 扩展 / 双击选词 / 三击选行 / 拖拽起点。
	if line, col, ok := b.editorHit(px, py); ok {
		shift := hasMod(ev, "shift")
		now := time.Now()
		if !shift && now.Sub(b.clickTime) < 400*time.Millisecond &&
			math.Abs(float64(px-b.clickX)) < 6 && math.Abs(float64(py-b.clickY)) < 6 {
			b.clickCount++
		} else {
			b.clickCount = 1
		}
		b.clickTime, b.clickX, b.clickY = now, px, py
		switch {
		case shift:
			ui.EditorExtendTo(line, col)
		case b.clickCount == 2:
			ui.EditorSelectWord(line, col)
		case b.clickCount >= 3:
			ui.EditorSelectLine(line)
			b.clickCount = 1 // 四击不再重复选行
		default:
			ui.EditorSetSelect(line, col)
			b.dragging = true
		}
	}
}

func (b *ideBridge) onPointerMove(ev rb.InputEvent) {
	if b.splitDrag {
		if b.splitAxis == "v" {
			ui.SetLayoutProjectW(b.splitStartW + int(ev.X-b.splitStartX))
		} else {
			// 终端在下方：向上拖(y 减小)增大高度，故取负号。
			ui.SetLayoutTerminalH(b.splitStartH - int(ev.Y-b.splitStartY))
		}
		// 布局尺寸写在动态子树内部，增量帧不刷新，走完整重绘。
		if b.rerender != nil {
			b.rerender()
		}
		return
	}
	if !b.dragging {
		return
	}
	if line, col, ok := b.editorHit(ev.X, ev.Y); ok {
		ui.EditorExtendTo(line, col)
	}
}

func (b *ideBridge) onPointerUp(ev rb.InputEvent) {
	b.dragging = false
	if b.splitDrag {
		b.splitDrag = false
		ui.SetSplitDragging(false)
		if b.rerender != nil {
			b.rerender()
		}
	}
}

func (b *ideBridge) onClick(ev rb.InputEvent) {
	// 点击任意处即聚焦编辑器（焦点是 editor.gx 里的 sig，由 GX 命令管理）。
	ui.EditorFocus()
	node := b.findNode(ev.NodeID)
	if node == nil {
		return
	}
	node.DispatchBubble("onClick", gxscene.EventContext{
		Raw: "rb-" + ev.Kind,
		Synthetic: &gxscene.SyntheticEvent{
			Type:          gxscene.EvClick,
			NativeName:    "onClick",
			Target:        node,
			CurrentTarget: node,
			X:             float64(ev.X),
			Y:             float64(ev.Y),
			Button:        1,
		},
	})
}

// splitterAt 按 class 精确匹配 WebStorm 三区分隔条（gx-splitter-v/h），
// 数据源是 V2 的 DebugAllBoxes 命中框表（与旧 skia hits 同语义）。
func (b *ideBridge) splitterAt(px, py float32) (string, bool) {
	for _, h := range b.win.DebugAllBoxes() {
		cls, _ := h["class"].(string)
		if cls != "gx-splitter-v" && cls != "gx-splitter-h" {
			continue
		}
		x, y := boxF(h, "x"), boxF(h, "y")
		w, hh := boxF(h, "w"), boxF(h, "h")
		if w <= 0 || hh <= 0 {
			continue
		}
		if px >= x && px < x+w && py >= y && py < y+hh {
			if cls == "gx-splitter-v" {
				return "v", true
			}
			return "h", true
		}
	}
	return "", false
}

// editorHit 把屏幕点换算成编辑器 (行, 列)：行取 .gx-editor-row 命中框
// （可见行按 y 排序后序号即文档行号），列换算交给 ui.PickColumn（与 caret
// 渲染同一套字符度量）。行内前 50 逻辑点是行号槽(42)+间距(8)，点击行号区
// 等价列 0（PickColumn 对负数返回 0）。
func (b *ideBridge) editorHit(px, py float32) (int, int, bool) {
	type rowBox struct{ x, y float32 }
	var rows []rowBox
	for _, h := range b.win.DebugAllBoxes() {
		cls, _ := h["class"].(string)
		if !strings.Contains(cls, "gx-editor-row") {
			continue
		}
		hh := boxF(h, "h")
		if hh <= 0 {
			continue
		}
		rows = append(rows, rowBox{boxF(h, "x"), boxF(h, "y")})
	}
	if len(rows) == 0 {
		return 0, 0, false
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].y < rows[j].y })
	for idx, r := range rows {
		if py >= r.y && py < r.y+22 {
			relX := float64(px) - (float64(r.x) + 50)
			col := ui.PickColumn(idx, relX)
			inputLogf("editorHit px=%.1f py=%.1f r.x=%.1f idx=%d relX=%.1f col=%d",
				px, py, r.x, idx, relX, col)
			return idx, col, true
		}
	}
	return 0, 0, false
}

// findNode 在当前帧场景树里按 NodeID 找节点（与 rustbackend demo 同款）。
func (b *ideBridge) findNode(id string) *gxscene.Node {
	if id == "" || b.app == nil {
		return nil
	}
	inst := b.app.Instance()
	if inst == nil {
		return nil
	}
	return findNodeIn(inst.Root(), id)
}

func findNodeIn(n *gxscene.Node, id string) *gxscene.Node {
	if n == nil {
		return nil
	}
	if n.ID.String() == id {
		return n
	}
	for _, c := range n.Children {
		if hit := findNodeIn(c, id); hit != nil {
			return hit
		}
	}
	return nil
}

// ---- 键盘 ----

// onKey 快捷键与导航键翻译，映射表与旧 GLFW 装配层逐条对齐。
func (b *ideBridge) onKey(ev rb.InputEvent) {
	if ev.Event != "down" && ev.Event != "repeat" {
		return
	}
	ctrl := hasMod(ev, "ctrl")
	shift := hasMod(ev, "shift")
	key := strings.ToLower(ev.Text)

	if ctrl {
		switch key {
		case "enter":
			ui.TriggerRun()
			return
		case "z":
			if shift {
				ui.EditorRedo()
			} else {
				ui.EditorUndo()
			}
			return
		case "y":
			ui.EditorRedo()
			return
		case "a":
			ui.EditorSelectAll()
			return
		case "c":
			if sel := ui.EditorSelectedText(); sel != "" {
				rb.ClipboardSet(sel)
			}
			return
		case "x":
			if sel := ui.EditorSelectedText(); sel != "" {
				rb.ClipboardSet(sel)
				ui.EditorDeleteSelection()
			}
			return
		case "v":
			if s := rb.ClipboardGet(); s != "" {
				ui.EditorPaste(s)
			}
			return
		}
	}

	switch ev.Text {
	case "Up":
		ui.EditorMoveEx(-1, 0, shift)
	case "Down":
		ui.EditorMoveEx(1, 0, shift)
	case "Left":
		if ctrl {
			ui.EditorMoveWord(-1, shift)
		} else {
			ui.EditorMoveEx(0, -1, shift)
		}
	case "Right":
		if ctrl {
			ui.EditorMoveWord(1, shift)
		} else {
			ui.EditorMoveEx(0, 1, shift)
		}
	case "Home":
		ui.EditorKeyHome(shift)
	case "End":
		ui.EditorKeyEnd(shift)
	case "Enter":
		ui.EditorNewline()
	case "Backspace":
		ui.EditorBackspace()
	case "Delete":
		ui.EditorDeleteForward()
	case "Tab":
		ui.EditorInsert("\t")
	}
}

// onIMECommit 可打印字符（含 CJK）插入编辑器；过滤控制字符（\t 保留，
// Tab 缩进是既有行为），与旧 CharCallback 同规则。
func (b *ideBridge) onIMECommit(ev rb.InputEvent) {
	for _, ch := range ev.Text {
		if (ch < 0x20 && ch != '\t') || ch == 0x7f {
			continue
		}
		inputLogf("ime %q before: %s", ch, ui.EditorStatus())
		ui.EditorInsert(string(ch))
		inputLogf("ime %q after:  %s", ch, ui.EditorStatus())
	}
}

// ---- 工具 ----

func isLeft(ev rb.InputEvent) bool {
	// V2 pointer 事件的按键在 Event 字段（"left"）；缺省视为左键。
	return ev.Event == "" || ev.Event == "left"
}

func hasMod(ev rb.InputEvent, m string) bool {
	return strings.Contains(ev.Mods, m)
}

// boxF 从 DebugAllBoxes 的 map 里取浮点字段（Rust JSON 过来的数值可能是
// float64/int/json.Number 反序列化产物，统一收敛）。
func boxF(h map[string]any, key string) float32 {
	switch v := h[key].(type) {
	case float64:
		return float32(v)
	case float32:
		return v
	case int:
		return float32(v)
	case int64:
		return float32(v)
	}
	return 0
}

// inputLogf 输入诊断日志（GOX_INPUT_DEBUG=1 启用，写 %TEMP%/gox-ide-input.log）。
// 用于定位「点击定位偏移 / 空格插错位置」类报障：记录每次点击换算与字符输入
// 前后的光标状态，区分「逻辑层错」与「显示层错」。
func inputLogf(format string, args ...any) {
	if os.Getenv("GOX_INPUT_DEBUG") == "" {
		return
	}
	p := filepath.Join(os.TempDir(), "gox-ide-input.log")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	fmt.Fprintf(f, time.Now().Format("15:04:05.000 ")+format+"\n", args...)
	f.Close()
}
