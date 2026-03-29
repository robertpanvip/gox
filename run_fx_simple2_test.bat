@echo off
echo ========================================
echo 运行 test_fx_simple2.gox 测试
echo ========================================
echo.

cd /d "%~dp0.."

echo 正在编译并运行测试...
echo.

go run cmd\run_test_fx_simple2\main.go

echo.
echo ========================================
echo 测试完成
echo ========================================
pause
