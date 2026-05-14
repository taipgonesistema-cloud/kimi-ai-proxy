@echo off
setlocal
cd /d "%~dp0"

where node >nul 2>nul
if errorlevel 1 (
  echo Node.js nao encontrado no PATH. Instale Node.js LTS e rode novamente.
  exit /b 1
)

if not exist "node_modules\playwright" (
  echo Instalando dependencias Node...
  call npm install
  if errorlevel 1 exit /b 1
)

echo Garantindo Chromium do Playwright...
call npx playwright install chromium
if errorlevel 1 exit /b 1

echo Abrindo Kimi para login e captura de sessao...
call npm run session
if errorlevel 1 exit /b 1

echo.
echo Sessao capturada. Agora rode: go run .
