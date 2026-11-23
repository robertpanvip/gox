package widgets

import (
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

func newTimeInput() *gxscene.Instance {
	return Input(gxscene.Props{"value": func() any { return "" }})
}

func TestTimePickerStructure(t *testing.T) {
	tp := TimePicker(gxscene.Props{
		"value": func() any { return "09:30" },
	})
	root := tp.Root()
	if findByGxRole(root, "timepicker") == nil {
		t.Fatal("缺少 timepicker 容器")
	}
	segs := findAllByRole(root, "time-segment")
	if len(segs) != 2 {
		t.Fatalf("默认应有时/分 2 段, got %d", len(segs))
	}
	text := gxscene.TextBackend{}.Paint(root)
	if !containsStr(text, "09") || !containsStr(text, "30") {
		t.Errorf("应显示 09:30: %q", text)
	}
}

func TestTimePickerWithSeconds(t *testing.T) {
	tp := TimePicker(gxscene.Props{
		"value":        func() any { return "09:30:15" },
		"with-seconds": true,
	})
	segs := findAllByRole(tp.Root(), "time-segment")
	if len(segs) != 3 {
		t.Fatalf("with-seconds 应有 3 段, got %d", len(segs))
	}
	gotSec := gxscene.TextBackend{}.Paint(tp.Root())
	if !containsStr(gotSec, "15") {
		t.Errorf("应显示秒 15: %q", gotSec)
	}
}

func TestTimePicker12Hour(t *testing.T) {
	tp := TimePicker(gxscene.Props{
		"value":  func() any { return "13:05" },
		"hour12": true,
	})
	got12 := gxscene.TextBackend{}.Paint(tp.Root())
	if !containsStr(got12, "01") {
		t.Errorf("13 点在 12 小时制应显示 01: %q", got12)
	}
}

func TestTimePickerSelect(t *testing.T) {
	val := signal.New("09:30")
	var picked string
	tp := TimePicker(gxscene.Props{
		"value":    func() any { return val() },
		"onChange": BindValue(func(s string) { picked = s; val(s) }),
	})
	tp.BindReactive(signal.Effect)
	root := tp.Root()

	// 点小时段 → 打开菜单
	segs := findAllByRole(root, "time-segment")
	if len(segs) == 0 {
		t.Fatal("无时段触发器")
	}
	hourSeg := segs[0]
	hourSeg.Dispatch("onClick", gxscene.EventContext{})
	tp.BindReactive(signal.Effect)
	root = tp.Root()

	menu := findByGxRole(root, "timepicker-menu")
	if menu == nil {
		t.Fatal("点时段后应弹出选项菜单")
	}
	// 找到值为 "14" 的选项并点击
	options := findAllByRole(menu, "time-option")
	var target *gxscene.Node
	for _, o := range options {
		if containsStr(gxscene.TextBackend{}.Paint(o), "14") {
			target = o
			break
		}
	}
	if target == nil {
		t.Fatal("小时菜单应有 14 选项")
	}
	target.Dispatch("onClick", gxscene.EventContext{})
	if picked != "14:30" {
		t.Errorf("选小时后回传 = %q, want 14:30", picked)
	}
}

func TestSegmentValues(t *testing.T) {
	if got := segmentValues(segHour, false); len(got) != 24 || got[0] != 0 || got[23] != 23 {
		t.Errorf("24 小时段 = %v", got)
	}
	if got := segmentValues(segHour, true); len(got) != 12 || got[0] != 1 || got[11] != 12 {
		t.Errorf("12 小时段 = %v", got)
	}
	if got := segmentValues(segMinute, false); len(got) != 60 {
		t.Errorf("分钟段应有 60 个值, got %d", len(got))
	}
}
