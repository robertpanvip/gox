// gx:nocollections
// 本文件为原生 Go 组件源码，经 gox build 转译为 tokens.go。
// gx:nocollections 声明跳过集合/裸对象字面量 desugar，保留手写 opcode 的原生 Go 语义。
package widgets

// 主题令牌 getter 快捷方式。组件把这些 getter 接到 dynAttr 插槽上，
// 主题切换时自动重绘——这是明暗双主题真正生效的关键（旧代码用编译期常量，
// 浅色主题下大量控件仍是深色）。
//
// 用法：
//
//	bgSlot := 0
//	ops := []gxscene.Op{ el("div"), dynAttr("bg", bgSlot), closeEl() }
//	getter := tkBg()  // func() string，随主题变化
//	inst := gxscene.render(template, func() any { return getter() })
//
// 注意：这些函数返回的是 getter；在组件构建期调用一次即可，getter 内部
// 通过 UseTheme() 建立响应式依赖。

// tkColor 返回一个随主题变化的颜色 getter（语义令牌名见 themeToken）。
func tkColor(name string) func() string {
	return UseToken(name)
}

// tkTone 返回一个随主题变化的 tone 色 getter（语义色 primary/success/...）。
func tkTone(name string) func() string {
	return UseTone(name)
}

// tkBg/tkGrouped/tkPanel/tkPanelAlt/tkInput 背景类 getter。
func tkBg() func() string      { return UseToken("bg") }
func tkGrouped() func() string { return UseToken("grouped") }
func tkPanel() func() string   { return UseToken("panel") }
func tkPanelAlt() func() string { return UseToken("panelalt") }
func tkInput() func() string   { return UseToken("input") }

// tkBorder/tkBorder2/tkHairline 描边类 getter。
func tkBorder() func() string  { return UseToken("border") }
func tkBorder2() func() string { return UseToken("border2") }
func tkHairline() func() string { return UseToken("hairline") }

// 文字类 getter。
func tkText() func() string     { return UseToken("text") }
func tkMuted() func() string    { return UseToken("muted") }
func tkFaint() func() string    { return UseToken("faint") }
func tkOnAccent() func() string { return UseToken("onaccent") }

// 圆角 getter。
func tkRadiusSmall() func() string  { return UseToken("radius-small") }
func tkRadiusControl() func() string { return UseToken("radius-control") }
func tkRadiusCard() func() string   { return UseToken("radius-card") }
func tkRadiusPanel() func() string  { return UseToken("radius-panel") }
func tkRadiusPill() func() string   { return UseToken("radius-pill") }

// 控件高度 getter。
func tkControlHeight() func() string { return UseToken("control-height") }

// 阴影 getter。
func tkShadowCard() func() string { return UseToken("shadow-card") }
