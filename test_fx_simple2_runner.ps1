# test_fx_simple2_runner.ps1

Write-Host "========================================"
Write-Host "Running test_fx_simple2.gox test"
Write-Host "========================================"
Write-Host ""

$scriptPath = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $scriptPath

Write-Host "Test file:"
Write-Host "  test/test_fx_simple2.gox"
Write-Host ""

# Find gox executable
$goxPath = ""
if (Test-Path ".\test\gox.exe~") { $goxPath = ".\test\gox.exe~" }
if ($goxPath -eq "" -and (Test-Path ".\gox_new.exe~")) { $goxPath = ".\gox_new.exe~" }

if ($goxPath -ne "") {
    Write-Host "Using compiler:"
    Write-Host "  $goxPath"
    Write-Host ""
    
    Write-Host "Running compilation and test..."
    & $goxPath ".\test\test_fx_simple2.gox"
    
    Write-Host ""
    Write-Host "========================================"
    Write-Host "Test completed"
    Write-Host "========================================"
} else {
    Write-Host "gox compiler not found"
    Write-Host "Please use: go run cmd\run_test_fx_simple2\main.go"
}
