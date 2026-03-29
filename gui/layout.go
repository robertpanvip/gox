package gui

// LayoutEngine 简单的布局引擎
type LayoutEngine struct {
	FlexDirection   FlexDirection
	JustifyContent  JustifyContent
	AlignItems      AlignItems
	PaddingTop      int
	PaddingRight    int
	PaddingBottom   int
	PaddingLeft     int
	Gap             int
	RowGap          int
	ColumnGap       int
	Children        []*LayoutEngine
	Parent          *LayoutEngine
	X               int
	Y               int
	Width           int
	Height          int
	ComputedX       int
	ComputedY       int
	ComputedWidth   int
	ComputedHeight  int
}

// NewLayoutEngine 创建新的布局引擎
func NewLayoutEngine() *LayoutEngine {
	return &LayoutEngine{
		FlexDirection:  FlexRow,
		JustifyContent: JustifyFlexStart,
		AlignItems:     AlignStretch,
		Children:       make([]*LayoutEngine, 0),
	}
}

// SetFlexDirection 设置弹性盒子方向
func (l *LayoutEngine) SetFlexDirection(dir FlexDirection) {
	l.FlexDirection = dir
}

// SetJustifyContent 设置主轴对齐方式
func (l *LayoutEngine) SetJustifyContent(justify JustifyContent) {
	l.JustifyContent = justify
}

// SetAlignItems 设置交叉轴对齐方式
func (l *LayoutEngine) SetAlignItems(align AlignItems) {
	l.AlignItems = align
}

// SetPadding 设置内边距
func (l *LayoutEngine) SetPadding(top, right, bottom, left int) {
	l.PaddingTop = top
	l.PaddingRight = right
	l.PaddingBottom = bottom
	l.PaddingLeft = left
}

// SetGap 设置间距
func (l *LayoutEngine) SetGap(gap int) {
	l.Gap = gap
	l.RowGap = gap
	l.ColumnGap = gap
}

// SetRowGap 设置行间距
func (l *LayoutEngine) SetRowGap(rowGap int) {
	l.RowGap = rowGap
}

// SetColumnGap 设置列间距
func (l *LayoutEngine) SetColumnGap(columnGap int) {
	l.ColumnGap = columnGap
}

// SetWidth 设置宽度
func (l *LayoutEngine) SetWidth(width int) {
	l.Width = width
}

// SetHeight 设置高度
func (l *LayoutEngine) SetHeight(height int) {
	l.Height = height
}

// AddChild 添加子节点
func (l *LayoutEngine) AddChild(child *LayoutEngine) {
	l.Children = append(l.Children, child)
	child.Parent = l
}

// CalculateLayout 计算布局（简单的 Flexbox 实现）
func (l *LayoutEngine) CalculateLayout(x, y, width, height int) {
	l.X = x
	l.Y = y
	l.Width = width
	l.Height = height

	// 应用内边距
	contentX := x + l.PaddingLeft
	contentY := y + l.PaddingTop
	contentWidth := width - l.PaddingLeft - l.PaddingRight
	contentHeight := height - l.PaddingTop - l.PaddingBottom

	// 计算子组件布局
	if l.FlexDirection == FlexRow {
		l.calculateRowLayout(contentX, contentY, contentWidth, contentHeight)
	} else {
		l.calculateColumnLayout(contentX, contentY, contentWidth, contentHeight)
	}
}

// calculateRowLayout 计算行布局
func (l *LayoutEngine) calculateRowLayout(x, y, width, height int) {
	numChildren := len(l.Children)
	if numChildren == 0 {
		return
	}

	// 计算总 gap 宽度
	totalGap := 0
	if numChildren > 1 {
		totalGap = l.ColumnGap * (numChildren - 1)
	}

	// 计算每个子组件的宽度（简单平均分配）
	availableWidth := width - totalGap
	childWidth := availableWidth / numChildren
	childHeight := height

	startX := x
	switch l.JustifyContent {
	case JustifyCenter:
		startX = x + width/4
	case JustifyFlexEnd:
		startX = x + width/2
	case JustifySpaceAround:
		startX = x + childWidth/2
	case JustifySpaceBetween:
		startX = x
	}

	for i, child := range l.Children {
		childY := y
		switch l.AlignItems {
		case AlignCenter:
			childY = y + (height-childHeight)/2
		case AlignFlexEnd:
			childY = y + height - childHeight
		}

		child.ComputedX = startX + i*(childWidth+l.ColumnGap)
		child.ComputedY = childY
		child.ComputedWidth = childWidth
		child.ComputedHeight = childHeight
	}
}

// calculateColumnLayout 计算列布局
func (l *LayoutEngine) calculateColumnLayout(x, y, width, height int) {
	numChildren := len(l.Children)
	if numChildren == 0 {
		return
	}

	// 计算总 gap 高度
	totalGap := 0
	if numChildren > 1 {
		totalGap = l.RowGap * (numChildren - 1)
	}

	// 计算每个子组件的高度（简单平均分配）
	availableHeight := height - totalGap
	childHeight := availableHeight / numChildren
	childWidth := width

	startY := y
	switch l.JustifyContent {
	case JustifyCenter:
		startY = y + height/4
	case JustifyFlexEnd:
		startY = y + height/2
	case JustifySpaceAround:
		startY = y + childHeight/2
	case JustifySpaceBetween:
		startY = y
	}

	for i, child := range l.Children {
		childX := x
		switch l.AlignItems {
		case AlignCenter:
			childX = x + (width-childWidth)/2
		case AlignFlexEnd:
			childX = x + width - childWidth
		}

		child.ComputedX = childX
		child.ComputedY = startY + i*(childHeight+l.RowGap)
		child.ComputedWidth = childWidth
		child.ComputedHeight = childHeight
	}
}

// GetComputedRect 获取计算后的布局矩形
func (l *LayoutEngine) GetComputedRect() Rect {
	return Rect{
		X:      l.ComputedX,
		Y:      l.ComputedY,
		Width:  l.ComputedWidth,
		Height: l.ComputedHeight,
	}
}
