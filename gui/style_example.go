package gui

// 使用示例

// 示例 1: 使用 style 字符串
func ExampleDivWithStyleString() {
	// CSS 字符串方式
	div := NewDiv(DivProps{
		Style: ParseStyle("display:flex; flex-wrap:wrap; justify-content:center"),
		Children: []Component{
			NewText("Child 1"),
			NewText("Child 2"),
		},
	})
	
	_ = div
}

// 示例 2: 使用 Style 对象
func ExampleDivWithStyleObject() {
	// 创建 Style 对象
	style := &Style{
		Display:  "flex",
		FlexDir:  "row",
		FlexWrap: "wrap",
		Justify:  "center",
		Align:    "center",
		Width:    "100%",
		Height:   "auto",
		Padding:  "10px",
	}
	
	div := NewDiv(DivProps{
		Style: style,
		Children: []Component{
			NewText("Content"),
		},
	})
	
	_ = div
}

// 示例 3: 链式调用（可选）
func ExampleDivWithChainedStyle() {
	style := NewStyle().
		Set("display", "flex").
		Set("flex-direction", "row").
		Set("justify-content", "space-between")
	
	div := NewDiv(DivProps{
		Style: style,
	})
	
	_ = div
}

// 示例 4: 通过 SetAttribute 设置 style
func ExampleDivSetAttribute() {
	div := NewDiv(DivProps{})
	
	// 通过 SetAttribute 设置 style
	div.SetAttribute("style", "display:flex; flex-wrap:wrap")
	
	// 或者设置其他属性
	div.SetAttribute("text", "Hello World")
	
	_ = div
}
