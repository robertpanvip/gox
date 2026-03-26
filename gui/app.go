package gui

import (
    "github.com/fogleman/gg"
    "github.com/hajimehoshi/ebiten/v2"
)

// App GUI 应用
type App struct {
    Width     int
    Height    int
    Title     string
    Root      *Container
    Dc        *gg.Context
    EbitenImg *ebiten.Image
    Counter   int
}

func NewApp(title string, width, height int) *App {
    app := &App{
        Title:  title,
        Width:  width,
        Height: height,
        Root:   NewContainer(),
        Counter: 0,
    }
    // 设置根容器的尺寸和可见性
    app.Root.SetRect(Rect{X: 0, Y: 0, Width: width, Height: height})
    app.Root.Visible = true
    return app
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
    // 清空画布（白色背景）
    a.Dc.SetRGB(1, 1, 1)
    a.Dc.Clear()
    
    // 渲染根容器及其子组件
    if a.Root.Visible {
        a.Root.Render(a.Dc)
    }
    
    // 将 gg 图像转换为 Ebiten 图像
    img := a.Dc.Image()
    a.EbitenImg = ebiten.NewImageFromImage(img)
    
    // 绘制到屏幕
    screen.DrawImage(a.EbitenImg, nil)
}

// Layout Ebiten layout
func (a *App) Layout(outsideWidth, outsideHeight int) (int, int) {
    return a.Width, a.Height
}

// Run 启动应用
func (a *App) Run() error {
    // 初始化 gg 上下文
    a.Dc = gg.NewContext(a.Width, a.Height)
    
    // 设置窗口
    ebiten.SetWindowSize(a.Width, a.Height)
    ebiten.SetWindowTitle(a.Title)
    
    // 运行 Ebiten
    return ebiten.RunGame(a)
}
