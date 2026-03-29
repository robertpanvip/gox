# 预编译 GUI 库以加速后续编译
Write-Host "📦 Pre-compiling GUI library..." -ForegroundColor Cyan

# 设置环境变量启用模块缓存
$env:GOFLAGS = "-mod=mod"

# 编译 gui 包（会缓存）
cd gui
..\runtime\go\bin\go.exe build -a .

if ($LASTEXITCODE -eq 0) {
    Write-Host "✓ GUI library pre-compiled" -ForegroundColor Green
} else {
    Write-Host "❌ Failed to compile GUI library" -ForegroundColor Red
    exit 1
}

cd ..

# 编译 gox 工具
Write-Host "🔨 Building gox tool..." -ForegroundColor Cyan
.\runtime\go\bin\go.exe build -o gox.exe cmd/gox/main.go

if ($LASTEXITCODE -eq 0) {
    Write-Host "✓ gox tool built" -ForegroundColor Green
} else {
    Write-Host "❌ Failed to build gox" -ForegroundColor Red
    exit 1
}

Write-Host "`n🎉 Pre-compilation complete!" -ForegroundColor Green
Write-Host "Now you can use:" -ForegroundColor Yellow
Write-Host "  .\build_and_run.ps1 simple_button.gox" -ForegroundColor White
