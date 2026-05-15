@echo off
setlocal
cd /d "%~dp0"

for /f "tokens=5" %%a in ('netstat -ano ^| findstr ":3001" ^| findstr "LISTENING"') do (
  taskkill /PID %%a /F >nul 2>nul
)

set AUTO_TOOLS=true
set AUTO_TOOLS_AGENT_MODE=pc
set AUTO_TOOLS_FAST_RETURN=true
set AUTO_TOOLS_ALLOW_COMMANDS=true
set AUTO_TOOLS_WORKSPACE=%CD%

start "kimi-ai-proxy" /MIN cmd /c "go run ./cmd/kimi-ai-proxy > kimi-proxy.log 2>&1"
echo Kimi proxy iniciado em background. Log: kimi-proxy.log
