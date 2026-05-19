@echo off
setlocal
set "SCRIPT_DIR=%~dp0"
set "BIN=%SCRIPT_DIR%bin\tickle.exe"

if exist "%BIN%" (
  "%BIN%" %*
  exit /b %ERRORLEVEL%
)

where tickle >nul 2>nul
if %ERRORLEVEL% equ 0 (
  tickle %*
  exit /b %ERRORLEVEL%
)

echo tickle binary not found. Install a platform-specific skill bundle or put tickle on PATH. 1>&2
exit /b 127
