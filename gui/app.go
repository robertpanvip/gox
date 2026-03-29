package gui

import (
	"github.com/hajimehoshi/ebiten/v2"
)

// App GUI 应用
type App struct {
	Width   int
	Height  int
	Title   string
	Root    *Container
	Counter int
}

// NewApp 创建新应用
func NewApp(title string, width, height int) *App {
	app := &App{
		Title:   title,
		Width:   width,
		Height:  height,
		Root:    NewContainer(),
		Counter: 0,
	}
	// 设置根容器的尺寸
	app.Root.SetRect(Rect{X: 0, Y: 0, Width: width, Height: height})
	app.Root.SetVisible(true)
	return app
}

// SetRootComponent 设置根组件
func (a *App) SetRootComponent(component Component) {
	if a.Root != nil {
		a.Root.Children = make([]Component, 0)
		a.Root.AddChild(component)
	}
}

// Update Ebiten update loop
func (a *App) Update() error {
	// 获取鼠标位置
	mx, my := ebiten.CursorPosition()

	// 触发鼠标移动事件
	a.Root.OnMouseMove(mx, my)

	// 处理鼠标点击
	if ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
		a.Root.OnClick(mx, my)
	}

	return nil
}

// Draw Ebiten draw loop
func (a *App) Draw(screen *ebiten.Image) {
	// 执行布局计算（在每次渲染前）
	a.Root.DoLayout()
	
	// 清空屏幕（白色背景）
	screen.Fill(ColorWhite.ToGoColor())

	// 渲染根容器及其子组件
	if a.Root.IsVisible() {
		a.Root.Render(screen)
	}
}

// Layout Ebiten layout
func (a *App) Layout(outsideWidth, outsideHeight int) (int, int) {
	return a.Width, a.Height
}

// Run 启动应用
func (a *App) Run() error {
	// 设置窗口
	ebiten.SetWindowSize(a.Width, a.Height)
	ebiten.SetWindowTitle(a.Title)

	// 运行 Ebiten
	return ebiten.RunGame(a)
}
