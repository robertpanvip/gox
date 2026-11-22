// PULSE Dashboard —— 用 GX 场景树 + Rust 后端渲染的端到端验证。
// 验证：首帧全量快照 + signal 更新走增量动作（SetText）+ IME 输入回传。
package main

import (
	"fmt"
	"time"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
	rb "github.com/robertpanvip/gox/examples/rust-backend/go"
)

func main() {
	fmt.Println("[demo] Rust backend version:", rb.Version())

	var count = signal.New(42)
	var name = signal.New("operator")

	// 每 1.2 秒自增，触发增量 SetText 动作
	go func() {
		for {
			time.Sleep(1200 * time.Millisecond)
			count(count() + 1)
		}
	}()

	win := rb.NewWindow()
	app, err := gxapp.OpenWith(gxapp.WindowConfig{
		Title: "PULSE Dashboard", Width: 760, Height: 560,
	}, win)
	if err != nil {
		panic(err)
	}
	defer app.Close()

	// 构造场景：根 div + 标题 + 三个卡片（数值为动态文本）+ 按钮 + 输入框。
	// 用紧凑的 Template DSL；动态数值放 OpDynChild 插槽（signal 更新 → SetText 增量）。
	app.Render(func() any {
		return gxscene.Render(
			gxscene.Template{
				Ops: []gxscene.Op{
					{Code: gxscene.OpElem, Arg: "div"},
					{Code: gxscene.OpAttr, Arg: "#0b1020", Key: "bg"},
					{Code: gxscene.OpAttr, Arg: "column", Key: "flex-direction"},
					{Code: gxscene.OpAttr, Arg: "20", Key: "padding"},
					{Code: gxscene.OpAttr, Arg: "14", Key: "gap"},

					// 标题
					{Code: gxscene.OpElem, Arg: "div"},
					{Code: gxscene.OpAttr, Arg: "#e2e8f0", Key: "color"},
					{Code: gxscene.OpAttr, Arg: "26", Key: "font-size"},
					{Code: gxscene.OpChildText, Arg: "PULSE · 脉冲看板"},
					{Code: gxscene.OpClose},

					// 卡片行
					{Code: gxscene.OpElem, Arg: "div"},
					{Code: gxscene.OpAttr, Arg: "row", Key: "flex-direction"},
					{Code: gxscene.OpAttr, Arg: "14", Key: "gap"},
					// 卡片1
					{Code: gxscene.OpElem, Arg: "div"},
					{Code: gxscene.OpAttr, Arg: "#141b33", Key: "bg"},
					{Code: gxscene.OpAttr, Arg: "208", Key: "width"},
					{Code: gxscene.OpAttr, Arg: "96", Key: "height"},
					{Code: gxscene.OpAttr, Arg: "12", Key: "border-radius"},
					{Code: gxscene.OpAttr, Arg: "14", Key: "padding"},
					{Code: gxscene.OpElem, Arg: "div"},
					{Code: gxscene.OpAttr, Arg: "#94a3b8", Key: "color"},
					{Code: gxscene.OpAttr, Arg: "14", Key: "font-size"},
					{Code: gxscene.OpChildText, Arg: "请求量"},
					{Code: gxscene.OpClose},
					{Code: gxscene.OpElem, Arg: "div"},
					{Code: gxscene.OpAttr, Arg: "#2dd4bf", Key: "color"},
					{Code: gxscene.OpAttr, Arg: "28", Key: "font-size"},
					{Code: gxscene.OpDynChild, Slot: 0}, // count 数值
					{Code: gxscene.OpClose},
					{Code: gxscene.OpClose},
					// 卡片2
					{Code: gxscene.OpElem, Arg: "div"},
					{Code: gxscene.OpAttr, Arg: "#141b33", Key: "bg"},
					{Code: gxscene.OpAttr, Arg: "208", Key: "width"},
					{Code: gxscene.OpAttr, Arg: "96", Key: "height"},
					{Code: gxscene.OpAttr, Arg: "12", Key: "border-radius"},
					{Code: gxscene.OpAttr, Arg: "14", Key: "padding"},
					{Code: gxscene.OpElem, Arg: "div"},
					{Code: gxscene.OpAttr, Arg: "#94a3b8", Key: "color"},
					{Code: gxscene.OpAttr, Arg: "14", Key: "font-size"},
					{Code: gxscene.OpChildText, Arg: "吞吐量"},
					{Code: gxscene.OpClose},
					{Code: gxscene.OpElem, Arg: "div"},
					{Code: gxscene.OpAttr, Arg: "#a78bfa", Key: "color"},
					{Code: gxscene.OpAttr, Arg: "28", Key: "font-size"},
					{Code: gxscene.OpDynChild, Slot: 1}, // doubled
					{Code: gxscene.OpClose},
					{Code: gxscene.OpClose},
					// 卡片3
					{Code: gxscene.OpElem, Arg: "div"},
					{Code: gxscene.OpAttr, Arg: "#141b33", Key: "bg"},
					{Code: gxscene.OpAttr, Arg: "208", Key: "width"},
					{Code: gxscene.OpAttr, Arg: "96", Key: "height"},
					{Code: gxscene.OpAttr, Arg: "12", Key: "border-radius"},
					{Code: gxscene.OpAttr, Arg: "14", Key: "padding"},
					{Code: gxscene.OpElem, Arg: "div"},
					{Code: gxscene.OpAttr, Arg: "#94a3b8", Key: "color"},
					{Code: gxscene.OpAttr, Arg: "14", Key: "font-size"},
					{Code: gxscene.OpChildText, Arg: "运行时长"},
					{Code: gxscene.OpClose},
					{Code: gxscene.OpElem, Arg: "div"},
					{Code: gxscene.OpAttr, Arg: "#fb923c", Key: "color"},
					{Code: gxscene.OpAttr, Arg: "28", Key: "font-size"},
					{Code: gxscene.OpChildText, Arg: "99.98%"},
					{Code: gxscene.OpClose},
					{Code: gxscene.OpClose},
					{Code: gxscene.OpClose}, // 卡片行

					// 提示
					{Code: gxscene.OpElem, Arg: "div"},
					{Code: gxscene.OpAttr, Arg: "#94a3b8", Key: "color"},
					{Code: gxscene.OpAttr, Arg: "15", Key: "font-size"},
					{Code: gxscene.OpChildText, Arg: "signal 数值每 1.2s 自增 → 增量动作 → Rust 镜像树打补丁"},
					{Code: gxscene.OpClose},

					// 输入框（带 gx-input-id 标记 → Rust 记录命中区）
					{Code: gxscene.OpElem, Arg: "div"},
					{Code: gxscene.OpAttr, Arg: "#0f172a", Key: "bg"},
					{Code: gxscene.OpAttr, Arg: "704", Key: "width"},
					{Code: gxscene.OpAttr, Arg: "44", Key: "height"},
					{Code: gxscene.OpAttr, Arg: "8", Key: "border-radius"},
					{Code: gxscene.OpAttr, Arg: "#2dd4bf", Key: "border"},
					{Code: gxscene.OpAttr, Arg: "2", Key: "border-width"},
					{Code: gxscene.OpAttr, Arg: "demo-input", Key: "gx-input-id"},
					{Code: gxscene.OpElem, Arg: "div"},
					{Code: gxscene.OpAttr, Arg: "#e2e8f0", Key: "color"},
					{Code: gxscene.OpAttr, Arg: "18", Key: "font-size"},
					{Code: gxscene.OpDynChild, Slot: 2}, // name
					{Code: gxscene.OpClose},
					{Code: gxscene.OpClose},

					// 底部说明
					{Code: gxscene.OpElem, Arg: "div"},
					{Code: gxscene.OpAttr, Arg: "#2dd4bf", Key: "color"},
					{Code: gxscene.OpAttr, Arg: "14", Key: "font-size"},
					{Code: gxscene.OpChildText, Arg: "IME: 点击输入框后，中文输入法的组合串/上屏字经回调写回 signal"},
					{Code: gxscene.OpClose},

					{Code: gxscene.OpClose}, // root
				},
				N: 3,
			},
			func() any { return fmt.Sprint(count()) },
			func() any { return fmt.Sprint(count() * 2) },
			func() any { return name() },
		)
	})

	// 注入 App 以启用增量动作流（首帧快照后走 TakeChanges）
	win.BindApp(app)

	// 事件回传：IME 上屏字写回 name signal
	win.SetDispatcher(func(ev rb.InputEvent) {
		switch ev.Kind {
		case "ime_commit":
			name(name() + ev.Text)
			fmt.Printf("[demo] ime_commit %q -> name=%q\n", ev.Text, name())
		case "ime_preedit":
			fmt.Printf("[demo] ime_preedit %q\n", ev.Text)
		case "click":
			fmt.Printf("[demo] click node=%s event=%s\n", ev.NodeID, ev.Event)
		case "focus":
			fmt.Printf("[demo] focus node=%s\n", ev.NodeID)
		}
	})

	if err := app.Run(); err != nil {
		panic(err)
	}
}
