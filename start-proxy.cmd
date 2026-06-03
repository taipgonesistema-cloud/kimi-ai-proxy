@echo off
cd /d "%~dp0"
echo [*] Iniciando DARTIK proxy em http://localhost:3001
echo [*] Pressione Ctrl+C para parar
echo.
go run ./cmd/kimi-ai-proxy
pause
