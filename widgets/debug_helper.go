package widgets

import (
	"strconv"

	"github.com/robertpanvip/gox/runtime/signal"
)

// debugLockedSubsStr 是 GX 源码可见的小写诊断别名，返回 "total/disposed" 形式
// （GX 源码层禁止直调底层 Signal API，转发放 Go 侧）。
func debugLockedSubsStr(key string) string {
	t, d := signal.DebugLockedSubs(key)
	return strconv.Itoa(t) + "/" + strconv.Itoa(d)
}

// DebugLastWindow 返回最近一次 VirtualList 窗口计算的输入摘要。
func DebugLastWindow() string { return debugLastWindow }
