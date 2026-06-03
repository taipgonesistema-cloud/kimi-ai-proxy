@echo off
setlocal enabledelayedexpansion
cd /d "%~dp0"

echo.
echo  ======== DARTIK — Kimi AI Proxy ========
echo.

:check_go
where go >nul 2>nul
if errorlevel 1 (
  echo [!] Go nao encontrado. Instale Go em https://go.dev/dl/
  pause
  exit /b 1
)

:check_node
where node >nul 2>nul
if errorlevel 1 (
  echo [!] Node.js nao encontrado. Instale Node.js LTS em https://nodejs.org/
  pause
  exit /b 1
)

:env
if not exist .env (
  if exist .env.example (
    copy .env.example .env >nul
    echo [*] .env criado a partir de .env.example
  )
) else (
  echo [*] .env ja existe
)

:npm
echo [*] Instalando dependencias Node...
call npm install --no-fund --no-audit 2>&1 | findstr /V "added packages"
if errorlevel 1 (
  echo [!] npm install falhou
  pause
  exit /b 1
)

:playwright
echo [*] Garantindo Chromium do Playwright...
call npx playwright install chromium 2>&1 | findstr /V "already"
if errorlevel 1 (
  echo [!] playwright install falhou (pode ignorar se ja instalado)
)

:session
echo.
set /p CAPTURE=Deseja capturar sessao do Kimi agora? (S/n):
if /i "!CAPTURE!"=="" set CAPTURE=s
if /i "!CAPTURE!"=="s" (
  call npm run session
  if errorlevel 1 (
    echo [!] Captura de sessao falhou
    pause
    exit /b 1
  )
  echo [*] Sessao capturada com sucesso
)

:done
echo.
echo  ======== Instalacao concluida ========
echo.
echo  Iniciar proxy:    go run ./cmd/kimi-ai-proxy
echo  Darki TUI:        npm run darki
echo  Modo pipe:        npm run darki-pipe -- -p "msg"
echo  Proxy + TUI:      start-proxy.cmd  +  npm run darki
echo.
echo  Proxy em: http://localhost:3001
echo.
pause
