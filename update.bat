@echo off
setlocal
cd /d "%~dp0"
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\publish-update.ps1" %*
set "result=%errorlevel%"
echo.
if not "%result%"=="0" echo UPDATE FAILED. Read the error above. Existing files were retained.
pause
exit /b %result%
