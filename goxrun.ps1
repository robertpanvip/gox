param(
    [string]$SourceFile,
    [Parameter(ValueFromRemainingArguments=$true)]
    [string[]]$Args
)

if (-not $SourceFile) {
    Write-Host "Usage: .\goxrun.ps1 <source.gox> [args...]" -ForegroundColor Red
    Write-Host ""
    Write-Host "Directly run .gox files without manual compilation steps."
    Write-Host ""
    Write-Host "Example:" -ForegroundColor Green
    Write-Host "  .\goxrun.ps1 test/fx_component.gox"
    Write-Host "  .\goxrun.ps1 test/demo_counter.gox"
    exit 1
}

if (-not (Test-Path $SourceFile)) {
    Write-Host "Error: File not found: $SourceFile" -ForegroundColor Red
    exit 1
}

Write-Host "🔍 Parsing $(Split-Path $SourceFile -Leaf)..." -ForegroundColor Cyan

# Build goxrun if not exists
$goxrunExe = ".\cmd\goxrun\goxrun.exe"
if (-not (Test-Path $goxrunExe)) {
    Write-Host "🔨 Building goxrun..." -ForegroundColor Yellow
    & ".\runtime\go\bin\go.exe" build -o $goxrunExe cmd/goxrun/main.go
    if ($LASTEXITCODE -ne 0) {
        Write-Host "Error building goxrun" -ForegroundColor Red
        exit 1
    }
    Write-Host "✓ goxrun built successfully" -ForegroundColor Green
}

# Run goxrun
& $goxrunExe $SourceFile $Args
