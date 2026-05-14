$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

if (-not (Get-Command node -ErrorAction SilentlyContinue)) {
  throw "Node.js nao encontrado no PATH. Instale Node.js LTS e rode novamente."
}

if (-not (Test-Path "node_modules/playwright")) {
  Write-Host "Instalando dependencias Node..."
  npm install
}

Write-Host "Garantindo Chromium do Playwright..."
npx playwright install chromium

Write-Host "Abrindo Kimi para login e captura de sessao..."
npm run session

Write-Host "Sessao capturada. Agora rode: go run ."
