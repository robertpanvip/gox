package gxscene

import (
	"strconv"
	"strings"
)

// animation.go —— 三端统一的「声明式动画」协议（受限内置动画，非任意 @keyframes）。
//
// 背景：组件样式绝大多数已由强类型 style prop 表达，唯一残留在宿主 CSS 里的能力
// 缺口是「周期闪烁」（Editor 光标）。为此引入一个受限、声明式、三端一致的动画
// prop，使宿主无需再内联 @keyframes。
//
// 语法（标量 StyleValue，兼容 CSS animation 简写的常用子集）：
//
//	animation = "<name> <duration> [<iterations>]"
//	name       : 内置动画名，当前支持 "blink"（opacity 方波闪烁）
//	duration   : "1.06s" / "1060ms" / "1.06" （缺省单位视为秒）
//	iterations : "infinite"（默认）或正整数；有限次数结束后不覆盖基础样式
//
// 例：animation="blink 1.06s infinite"
//
// 三端语义（严格统一）：
//   - Web：翻译成原生 CSS animation + 注入一次全局 @keyframes gx-blink，
//     由浏览器合成器驱动，不占用 JS/RAF。
//   - Skia/Rust：渲染帧按 now 采样 Spec.Sample 得到 opacity，临时覆盖到节点
//     props（绘制后恢复，不污染常驻树）；绘制层把 opacity 应用到背景/文字 alpha。
//
// 采样相位使用进程纪元毫秒（UnixMilli）取模，三端同机运行时相位一致；不同进程
// 仅相位偏移不同，视觉同为 1.06s 方波，不影响语义。

const (
	// AnimationBlink 是内置的闪烁动画：前半周期 opacity=1，后半周期 opacity=0，
	// 对齐原生 CSS 的 @keyframes gx-blink {0%,49%{opacity:1} 50%,100%{opacity:0}}。
	AnimationBlink = "blink"

	// PropAnimation 是动画 prop 在后端的协议键。
	PropAnimation = "animation"
)

// AnimationSpec 是 animation prop 的解析结果。
type AnimationSpec struct {
	Name       string // 内置动画名（当前仅 blink）
	DurationMS int64  // 周期毫秒，>0
	Iterations int    // -1 表示 infinite；>0 为有限次数
}

// Active 报告该 spec 是否表达一个有效的、周期大于零的动画。
func (a AnimationSpec) Active() bool {
	return a.Name != "" && a.DurationMS > 0
}

// Infinite 报告是否无限循环。
func (a AnimationSpec) Infinite() bool { return a.Iterations < 0 }

// Sample 计算动画在时刻 nowMS 的 opacity 覆盖值。
// active=false 表示不应覆盖基础 opacity（动画名未知 / 有限次数已结束）。
//
// blink：以 DurationMS 为周期取模，相位 < 50% 返回 1，否则返回 0。
// 有限次数结束后 active=false，让节点回到原始 opacity（对齐 CSS fill-mode:none）。
func (a AnimationSpec) Sample(nowMS int64) (opacity float32, active bool) {
	if !a.Active() {
		return 1, false
	}
	elapsed := nowMS
	if a.Iterations > 0 && elapsed >= a.DurationMS*int64(a.Iterations) {
		return 1, false
	}
	if a.Name != AnimationBlink {
		return 1, false
	}
	phase := elapsed % a.DurationMS
	if phase < 0 {
		phase += a.DurationMS
	}
	if phase*2 < a.DurationMS {
		return 1, true
	}
	return 0, true
}

// ParseAnimation 解析 animation 标量值。非法值返回零值 spec（Active=false），
// 后端据此忽略动画——与「解析失败静默回退」的既有风格一致。
func ParseAnimation(s string) AnimationSpec {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(s)))
	if len(fields) == 0 {
		return AnimationSpec{}
	}
	spec := AnimationSpec{Name: fields[0], Iterations: -1}
	if spec.Name != AnimationBlink {
		return AnimationSpec{}
	}
	for _, tok := range fields[1:] {
		if tok == "infinite" {
			spec.Iterations = -1
			continue
		}
		if n, err := strconv.Atoi(tok); err == nil && n > 0 {
			spec.Iterations = n
			continue
		}
		if d, ok := parseDurationMS(tok); ok && d > 0 {
			spec.DurationMS = d
		}
	}
	if spec.DurationMS <= 0 {
		return AnimationSpec{}
	}
	return spec
}

// parseDurationMS 解析 "1.06s" / "1060ms" / "1.06"（缺省单位秒）为毫秒。
func parseDurationMS(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "ms") {
		v, err := strconv.ParseFloat(strings.TrimSuffix(s, "ms"), 64)
		if err != nil {
			return 0, false
		}
		return int64(v), true
	}
	if strings.HasSuffix(s, "s") {
		v, err := strconv.ParseFloat(strings.TrimSuffix(s, "s"), 64)
		if err != nil {
			return 0, false
		}
		return int64(v * 1000), true
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return int64(v * 1000), true
}

// AnimationFromProps 从后端 props 读取 animation 标量并解析。
func AnimationFromProps(props map[string]any) (AnimationSpec, bool) {
	if props == nil {
		return AnimationSpec{}, false
	}
	v, ok := props[PropAnimation]
	if !ok {
		return AnimationSpec{}, false
	}
	s, ok := v.(string)
	if !ok {
		return AnimationSpec{}, false
	}
	spec := ParseAnimation(s)
	return spec, spec.Active()
}
