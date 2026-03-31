package main

import (
	"fmt"
	"github.com/gox-lang/gox/gui"
)

// Counter 高阶组件函数
func Counter() func() gui.TemplateResult {
	count := 0
	return func() gui.TemplateResult {
		return gui.TemplateResult{
			StaticCode: `<button>`,
			Static: []func() gui.Component{
				func() gui.Component {
					return gui.NewButton(ButtonProps{OnClick: func() {}, Children: []gui.Component{gui.NewText(gui.TextProps{Text: fmt.Sprintf("Count: %v", count)})}})
				},
			},
		}
	}
}



// App 高阶组件函数
func App() func() gui.TemplateResult {
	title := "My App"
	return func() gui.TemplateResult {
		return gui.TemplateResult{
			StaticCode: `<div>`,
			Static: []func() gui.Component{
				func() gui.Component {
					return gui.NewDiv(DivProps{Children: []gui.Component{gui.NewDiv(DivProps{Children: []gui.Component{gui.NewText(gui.TextProps{Text: title})}}), gui.WrapTemplateResult(Counter(Counter{})), gui.WrapTemplateResult(Counter(Counter{}))}})
				},
			},
		}
	}
}

func main() {
	app := gui.NewApp()
	app.SetRootComponentFuncHO(App())
	app.Run()
}



