param(
    [string]$InputFile = "simple_button.gox",
    [string]$OutputExe = ""
)

# 生成 Go 代码
Write-Host "🔍 Generating Go code..." -ForegroundColor Cyan
.\gox.exe -o "test\$([System.IO.Path]::GetFileNameWithoutExtension($InputFile)).go" "test\$InputFile"

if (-not $OutputExe) {
    $OutputExe = "test\$([System.IO.Path]::GetFileNameWithoutExtension($InputFile)).exe"
}

# 编译（使用缓存）
Write-Host "🔨 Building with cache..." -ForegroundColor Cyan
$env:GOFLAGS = "-mod=mod"
cd test
..\runtime\go\bin\go.exe build -o $OutputExe "$([System.IO.Path]::GetFileNameWithoutExtension($InputFile)).go"

if ($LASTEXITCODE -eq 0) {
    Write-Host "✓ Build successful: $OutputExe" -ForegroundColor Green
    Write-Host "🚀 Running..." -ForegroundColor Cyan
    & $OutputExe
} else {
    Write-Host "❌ Build failed" -ForegroundColor Red
}

cd ..
