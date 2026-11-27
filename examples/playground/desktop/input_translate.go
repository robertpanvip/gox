// 桌面输入翻译层（V2 rustbackend）：把 Rust 侧原始输入事件映射成 GX 编辑
// 命令。装配层只做「平台事件 → 语义命令」的翻译，不碰编辑逻辑——这正是
// 「一份 GX 代码两端共用」的关键：网页端也调用同一组 GX 命令。
//
// 与旧 GLFW 路径（glwindow.go）的对应关系：
//
//	KeyCallback        → InputEvent{Kind:"key"}（键名 + mods 修饰键串）
//	CharCallback       → InputEvent{Kind:"ime_commit"}（Rust 端字符/IME 统一回传）
//	MouseButtonCallback → InputEvent{Kind:"pointer_down"/"pointer_up"}（多击计数保留）
//	ScrollCallback     → InputEvent{Kind:"wheel"}（仅编辑器滚动；容器滚动由渲染端自治）
//	CursorPosCallback  → InputEvent{Kind:"pointer_move"}（拖拽选区扩展）
//	系统光标形状       → 渲染端直接消费 CSS cursor（Rust 侧 set_cursor，不再经 Go）
package main

import (
	"strconv"
	"strings"
	"sync"
	"time"

	rb "github.com/robertpanvip/gox-backend-rust"
	"github.com/robertpanvip/gox-playground/ui"
	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"

	"math"
)

// inputBridge 持有翻译层状态：命中框缓存（按下/滚轮时刷新）、多击计数、拖拽态。
// Rust 事件回调在事件泵线程串行执行，无需额外加锁保护翻译状态本身；
// 命中框缓存有独立 mu（DebugAllBoxes 走 cgo，与 Run 泵串行即可）。
type inputBridge struct {
	win   *rb.Window
	app   *gxapp.App
	mu    sync.Mutex
	boxes []map[string]any // 最近一次拉取的命中框快照（editorHit 用）
	// 多击计数（双击选词/三击选行）：400ms 内同点位连续按下。
	clickTime  time.Time
	clickX     float64
	clickY     float64
	clickCount int
	// 拖拽选区：pointer_down 命中编辑器行后置位，pointer_up 复位。
	dragging bool
}

// refreshBoxes 拉取渲染端最近一帧命中框（按下/滚轮时调用，低频；布局在
// 两次拉取间被视为静止——拖拽选区期间布局不变的假设与旧 skia 路径一致）。
func (b *inputBridge) refreshBoxes() {
	if b.win == nil {
		return
	}
	b.mu.Lock()
	b.boxes = b.win.DebugAllBoxes()
	b.mu.Unlock()
}

// editorHit 把屏幕点换算成编辑器 (行, 列)。行号取行节点上的 data-line 属性
// （真实文档行号，0 基）——虚拟滚动下可见行只是文档的一个窗口，DOM 顺序索引
// 不等于行号，滚动后必须按 data-line 换算，否则点击会把光标送到文档顶部。
// 列换算交给 ui.PickColumn，与 caret 渲染共用同一套字符度量。行内前 50 逻辑点是
// 行号槽（42）+ 间距（8），点击行号区等价列 0（PickColumn 对负数返回 0）。
func (b *inputBridge) editorHit(px, py float32) (int, int, bool) {
	b.mu.Lock()
	boxes := b.boxes
	b.mu.Unlock()
	for _, h := range boxes {
		cls, _ := h["class"].(string)
		if !strings.Contains(cls, "gx-editor-row") {
			continue
		}
		x, _ := h["x"].(float64)
		y, _ := h["y"].(float64)
		hh, _ := h["h"].(float64)
		if hh <= 0 || py < float32(y) || py >= float32(y)+float32(hh) || px < float32(x) {
			continue
		}
		line := 0
		if s, ok := h["data-line"].(string); ok {
			if n, err := strconv.Atoi(s); err == nil {
				line = n
			}
		}
		relX := float64(px) - (float64(x) + 50)
		return line, ui.PickColumn(line, relX), true
	}
	return 0, 0, false
}

// dispatch 是 SetDispatcher 的入口：按 kind 分发到对应翻译路径。
func (b *inputBridge) dispatch(ev rb.InputEvent) {
	switch ev.Kind {
	case "key":
		b.onKey(ev)
	case "ime_commit":
		b.onChar(ev.Text)
	case "ime_preedit":
		// 组合串预编辑：预制 Input 暂不渲染 preedit，只记录不消费。
	case "pointer_down":
		b.onPointerDown(ev)
	case "pointer_move":
		b.onPointerMove(ev)
	case "pointer_up":
		b.dragging = false
	case "wheel":
		b.onWheel(ev)
	case "click":
		b.onClick(ev)
	default:
		// backspace / focus / ime_enabled：编辑器命令走 key 翻译，忽略。
	}
}

// onKey：平台按键 → GX 编辑器命令。键名来自 Rust 端 key_name
// （Named 键变体名 / 字符键字符），mods 是固定顺序修饰键串。
func (b *inputBridge) onKey(ev rb.InputEvent) {
	ctrl := strings.Contains(ev.Mods, "ctrl")
	shift := strings.Contains(ev.Mods, "shift")
	key := ev.Text
	if key == "Escape" {
		// Escape 已由 Rust 端关窗（el.exit），翻译层无操作。
		return
	}
	switch key {
	case "Enter", "NumpadEnter":
		if ctrl {
			ui.TriggerRun()
			return
		}
		ui.EditorNewline()
	case "KeyZ", "z":
		if ctrl {
			if shift {
				ui.EditorRedo()
			} else {
				ui.EditorUndo()
			}
			return
		}
	case "KeyY", "y":
		if ctrl {
			ui.EditorRedo()
			return
		}
	case "KeyA", "a":
		if ctrl {
			ui.EditorSelectAll()
			return
		}
	case "KeyC", "c":
		if ctrl {
			if sel := ui.EditorSelectedText(); sel != "" {
				rb.ClipboardSet(sel)
			}
			return
		}
	case "KeyX", "x":
		if ctrl {
			if sel := ui.EditorSelectedText(); sel != "" {
				rb.ClipboardSet(sel)
				ui.EditorDeleteSelection()
			}
			return
		}
	case "KeyV", "v":
		if ctrl {
			if s := rb.ClipboardGet(); s != "" {
				ui.EditorPaste(s)
			}
			return
		}
	case "Backspace":
		ui.EditorBackspace()
	case "ArrowUp":
		ui.EditorMoveEx(-1, 0, shift)
	case "ArrowDown":
		ui.EditorMoveEx(1, 0, shift)
	case "ArrowLeft":
		if ctrl {
			ui.EditorMoveWord(-1, shift)
		} else {
			ui.EditorMoveEx(0, -1, shift)
		}
	case "ArrowRight":
		if ctrl {
			ui.EditorMoveWord(1, shift)
		} else {
			ui.EditorMoveEx(0, 1, shift)
		}
	case "Home":
		ui.EditorKeyHome(shift)
	case "End":
		ui.EditorKeyEnd(shift)
	case "Delete":
		ui.EditorDeleteForward()
	}
}

// onChar：可打印字符（含中文，Rust 端字符/IME 统一回传）→ GX 编辑器。
// Rust 端已过滤控制字符，这里不再重复过滤。
func (b *inputBridge) onChar(text string) {
	ui.EditorInsert(text)
}

// onPointerDown：编辑器选区交互——定点 / Shift 扩展 / 双击选词 / 三击选行 /
// 拖拽起点；点击任意处聚焦编辑器（焦点是 editor.gx 里的 sig）。
func (b *inputBridge) onPointerDown(ev rb.InputEvent) {
	b.refreshBoxes()
	px, py := ev.X, ev.Y
	shift := strings.Contains(ev.Mods, "shift")
	// 多击计数：400ms 内同点位连续按下（Rust/winit 无 dblclick 事件，计数模拟）。
	now := time.Now()
	if !shift && now.Sub(b.clickTime) < 400*time.Millisecond &&
		math.Abs(float64(px)-b.clickX) < 6 && math.Abs(float64(py)-b.clickY) < 6 {
		b.clickCount++
	} else {
		b.clickCount = 1
	}
	b.clickTime = now
	b.clickX = float64(px)
	b.clickY = float64(py)
	if line, col, ok := b.editorHit(px, py); ok {
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
	ui.EditorFocus()
}

// onPointerMove：左键按住拖拽时持续扩展选区（与 web 端 pointermove 同语义）。
// 悬停光标形状由渲染端消费 CSS cursor（Rust 侧），翻译层不处理。
func (b *inputBridge) onPointerMove(ev rb.InputEvent) {
	if !b.dragging {
		return
	}
	if line, col, ok := b.editorHit(ev.X, ev.Y); ok {
		ui.EditorExtendTo(line, col)
	}
}

// onWheel：编辑器虚拟滚动（容器滚动已由渲染端自治，不会到达这里）。
// Rust 端 y 正为向上滚，取负号转成"向下为正行数"。
func (b *inputBridge) onWheel(ev rb.InputEvent) {
	ui.EditorScrollLines(-float64(ev.Y) * 3)
}

// onClick：Rust 端命中派发（带 NodeID 的 onClick 命中）→ 冒泡派发到场景树。
// 编辑器内的点击已由 pointer_down 路径处理；这里覆盖普通按钮/组件。
func (b *inputBridge) onClick(ev rb.InputEvent) {
	if ev.NodeID == "" || b.app == nil {
		return
	}
	root := b.app.Instance().Root()
	if n := findNodeIn(root, ev.NodeID); n != nil {
		n.DispatchBubble("onClick", gxscene.EventContext{
			Raw: "rust-pointer",
			Synthetic: &gxscene.SyntheticEvent{
				Type:          gxscene.EvClick,
				NativeName:    "onClick",
				Target:        n,
				CurrentTarget: n,
				X:             float64(ev.X),
				Y:             float64(ev.Y),
			},
		})
	}
}

// findNodeIn 在场景树按 NodeID 找节点（Rust 端 click 事件定位用）。
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
