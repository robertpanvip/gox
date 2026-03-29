package gui

// Counter fx 组件（lit-html 风格）
type Counter struct {
	BaseFxComponent
	
	// 状态变量（类似 lit 的属性）
	count int
	name  string
	
	// 静态组件（创建一次）
	rootDiv    *Div
	nameLabel  *Label
	countLabel *Label
	button     *Button
	
	// 动态部分（可更新）
	namePart  *TextPart
	countPart *TextPart
}

// NewCounter 创建 Counter 组件
func NewCounter() *Counter {
	c := &Counter{
		count: 0,
		name:  "World",
	}
	
	// 创建静态组件（只执行一次）
	c.rootDiv = NewDiv(&Style{
		Padding: "20px",
		FlexDirection: "column",
	})
	
	c.nameLabel = NewLabel(LabelProps{
		FontSize: 16,
	})
	
	c.countLabel = NewLabel(LabelProps{
		FontSize: 16,
	})
	
	c.button = NewButton(ButtonProps{
		Text: "Increment",
	})
	
	// 初始绑定（创建动态部分）
	c.namePart = NewTextPart(c.nameLabel, func() string {
		return "Hello " + c.name + "!"
	})
	
	c.countPart = NewTextPart(c.countLabel, func() string {
		return "Count: " + itoa(c.count)
	})
	
	// 设置初始值
	c.namePart.Update()
	c.countPart.Update()
	
	// 添加事件处理器
	c.button.SetOnClick(func() {
		c.count++
		c.RequestUpdate() // 触发更新
	})
	
	// 添加子组件到根 div
	c.rootDiv.AddChild(c.nameLabel)
	c.rootDiv.AddChild(c.countLabel)
	c.rootDiv.AddChild(c.button)
	
	// 创建模板结果
	c.SetTemplateResult(&TemplateResult{
		StaticParts: []Component{c.rootDiv},
		DynamicParts: []TemplatePart{c.namePart, c.countPart},
	})
	
	return c
}

// itoa 整数转字符串（简化版本）
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	
	negative := false
	if n < 0 {
		negative = true
		n = -n
	}
	
	digits := make([]byte, 0)
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}
	
	if negative {
		digits = append(digits, '-')
	}
	
	// Reverse
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	
	return string(digits)
}
