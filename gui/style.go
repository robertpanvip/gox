package gui

// FlexDirection 弹性盒子方向（保留向后兼容）
type FlexDirection int

const (
	FlexRow FlexDirection = iota
	FlexColumn
)

// JustifyContent 主轴对齐方式（保留向后兼容）
type JustifyContent int

const (
	JustifyFlexStart JustifyContent = iota
	JustifyCenter
	JustifyFlexEnd
	JustifySpaceBetween
	JustifySpaceAround
)

// AlignItems 交叉轴对齐方式（保留向后兼容）
type AlignItems int

const (
	AlignStretch AlignItems = iota
	AlignCenter
	AlignFlexStart
	AlignFlexEnd
)

// Style 样式结构体（CSS 风格）
type Style struct {
	// 布局
	Display         string  // "flex" (默认) | "none"
	FlexDirection   string  // "row" | "column" | "row-reverse" | "column-reverse"
	JustifyContent  string  // "flex-start" | "flex-end" | "center" | "space-between" | "space-around" | "space-evenly"
	AlignItems      string  // "stretch" | "flex-start" | "flex-end" | "center" | "baseline"
	AlignSelf       string  // "auto" | "stretch" | "flex-start" | "flex-end" | "center"
	FlexWrap        string  // "nowrap" | "wrap" | "wrap-reverse"
	FlexGrow        float32
	FlexShrink      float32
	FlexBasis       string  // e.g., "100px", "auto"
	
	// 间距（gap）
	Gap           string  // e.g., "10px", "1rem"
	RowGap        string  // 行间距
	ColumnGap     string  // 列间距
	
	// 尺寸
	Width           string  // e.g., "100px", "50%", "auto"
	Height          string
	MinWidth        string
	MinHeight       string
	MaxWidth        string
	MaxHeight       string
	
	// 间距（支持 "10px", "10px 20px", "10px 20px 30px 40px"）
	Margin          string
	MarginTop       string
	MarginRight     string
	MarginBottom    string
	MarginLeft      string
	
	Padding         string
	PaddingTop      string
	PaddingRight    string
	PaddingBottom   string
	PaddingLeft     string
	
	// 定位
	Position        string  // "relative" | "absolute"
	Left            string
	Right           string
	Top             string
	Bottom          string
	
	// 边框
	Border          string
	BorderWidth     string
	BorderStyle     string  // "solid" | "dashed" | "dotted"
	BorderColor     string
	BorderRadius    string
	
	// 背景
	BackgroundColor string
	
	// 文字
	FontSize        string
	FontWeight      string  // "normal" | "bold" | "100"-"900"
	Color           string
	TextAlign       string  // "left" | "center" | "right"
}

// CSS 创建样式（CSS 风格）
func CSS(properties map[string]interface{}) *Style {
	style := &Style{
		Display: "flex", // 默认启用 flex 布局
	}
	
	for key, value := range properties {
		switch key {
		// 布局
		case "display":
			if v, ok := value.(string); ok {
				style.Display = v
			}
		case "flexDirection":
			if v, ok := value.(string); ok {
				style.FlexDirection = v
			}
		case "justifyContent":
			if v, ok := value.(string); ok {
				style.JustifyContent = v
			}
		case "alignItems":
			if v, ok := value.(string); ok {
				style.AlignItems = v
			}
		case "alignSelf":
			if v, ok := value.(string); ok {
				style.AlignSelf = v
			}
		case "flexWrap":
			if v, ok := value.(string); ok {
				style.FlexWrap = v
			}
		case "flexGrow":
			switch v := value.(type) {
			case float32:
				style.FlexGrow = v
			case int:
				style.FlexGrow = float32(v)
			case float64:
				style.FlexGrow = float32(v)
			}
		case "flexShrink":
			switch v := value.(type) {
			case float32:
				style.FlexShrink = v
			case int:
				style.FlexShrink = float32(v)
			case float64:
				style.FlexShrink = float32(v)
			}
		case "flexBasis":
			if v, ok := value.(string); ok {
				style.FlexBasis = v
			}
			
		// 间距（gap）
		case "gap":
			if v, ok := value.(string); ok {
				style.Gap = v
			}
		case "rowGap":
			if v, ok := value.(string); ok {
				style.RowGap = v
			}
		case "columnGap":
			if v, ok := value.(string); ok {
				style.ColumnGap = v
			}
			
		// 尺寸
		case "width":
			if v, ok := value.(string); ok {
				style.Width = v
			}
		case "height":
			if v, ok := value.(string); ok {
				style.Height = v
			}
		case "minWidth":
			if v, ok := value.(string); ok {
				style.MinWidth = v
			}
		case "minHeight":
			if v, ok := value.(string); ok {
				style.MinHeight = v
			}
		case "maxWidth":
			if v, ok := value.(string); ok {
				style.MaxWidth = v
			}
		case "maxHeight":
			if v, ok := value.(string); ok {
				style.MaxHeight = v
			}
			
		// 间距
		case "padding":
			if v, ok := value.(string); ok {
				style.Padding = v
			}
		case "paddingTop":
			if v, ok := value.(string); ok {
				style.PaddingTop = v
			}
		case "paddingRight":
			if v, ok := value.(string); ok {
				style.PaddingRight = v
			}
		case "paddingBottom":
			if v, ok := value.(string); ok {
				style.PaddingBottom = v
			}
		case "paddingLeft":
			if v, ok := value.(string); ok {
				style.PaddingLeft = v
			}
		case "margin":
			if v, ok := value.(string); ok {
				style.Margin = v
			}
		case "marginTop":
			if v, ok := value.(string); ok {
				style.MarginTop = v
			}
		case "marginRight":
			if v, ok := value.(string); ok {
				style.MarginRight = v
			}
		case "marginBottom":
			if v, ok := value.(string); ok {
				style.MarginBottom = v
			}
		case "marginLeft":
			if v, ok := value.(string); ok {
				style.MarginLeft = v
			}
			
		// 定位
		case "position":
			if v, ok := value.(string); ok {
				style.Position = v
			}
		case "left":
			if v, ok := value.(string); ok {
				style.Left = v
			}
		case "right":
			if v, ok := value.(string); ok {
				style.Right = v
			}
		case "top":
			if v, ok := value.(string); ok {
				style.Top = v
			}
		case "bottom":
			if v, ok := value.(string); ok {
				style.Bottom = v
			}
			
		// 边框
		case "borderWidth":
			if v, ok := value.(string); ok {
				style.BorderWidth = v
			}
		case "borderColor":
			if v, ok := value.(string); ok {
				style.BorderColor = v
			}
		case "borderStyle":
			if v, ok := value.(string); ok {
				style.BorderStyle = v
			}
		case "borderRadius":
			if v, ok := value.(string); ok {
				style.BorderRadius = v
			}
			
		// 背景
		case "backgroundColor":
			if v, ok := value.(string); ok {
				style.BackgroundColor = v
			}
			
		// 文字
		case "fontSize":
			if v, ok := value.(string); ok {
				style.FontSize = v
			}
		case "color":
			if v, ok := value.(string); ok {
				style.Color = v
			}
		}
	}
	
	return style
}

// Merge 合并样式
func (s *Style) Merge(other *Style) *Style {
	if other == nil {
		return s
	}
	
	newStyle := &Style{}
	
	// 复制当前样式
	*newStyle = *s
	
	// 用 other 的值覆盖（如果 other 有值）
	if other.Display != "" {
		newStyle.Display = other.Display
	}
	if other.FlexDirection != "" {
		newStyle.FlexDirection = other.FlexDirection
	}
	if other.JustifyContent != "" {
		newStyle.JustifyContent = other.JustifyContent
	}
	if other.AlignItems != "" {
		newStyle.AlignItems = other.AlignItems
	}
	if other.FlexGrow != 0 {
		newStyle.FlexGrow = other.FlexGrow
	}
	if other.Width != "" {
		newStyle.Width = other.Width
	}
	if other.Height != "" {
		newStyle.Height = other.Height
	}
	if other.Padding != "" {
		newStyle.Padding = other.Padding
	}
	if other.BackgroundColor != "" {
		newStyle.BackgroundColor = other.BackgroundColor
	}
	if other.BorderWidth != "" {
		newStyle.BorderWidth = other.BorderWidth
	}
	if other.BorderColor != "" {
		newStyle.BorderColor = other.BorderColor
	}
	
	return newStyle
}
