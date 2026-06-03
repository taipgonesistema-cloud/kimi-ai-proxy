#!/usr/bin/env bash
set -e
cd "$(dirname "$0")"

echo ""
echo "  ======== DARTIK — Kimi AI Proxy ========"
echo ""

if ! command -v go &>/dev/null; then
  echo "[!] Go not found. Install from https://go.dev/dl/"
  exit 1
fi

if ! command -v node &>/dev/null; then
  echo "[!] Node.js not found. Install from https://nodejs.org/"
  exit 1
fi

if [ ! -f .env ]; then
  if [ -f .env.example ]; then
    cp .env.example .env
    echo "[*] .env created from .env.example"
  fi
else
  echo "[*] .env already exists"
fi

echo "[*] Installing Node dependencies..."
npm install --no-fund --no-audit 2>&1 | grep -v "added packages" || true

echo "[*] Ensuring Playwright Chromium..."
npx playwright install chromium 2>&1 | grep -v "already" || true

echo ""
read -r -p "Capture Kimi session now? (y/N): " resp
if [[ "$resp" =~ ^[Yy]$ ]]; then
  npm run session
  echo "[*] Session captured"
fi

echo ""
echo "  ======== Installation complete ========"
echo ""
echo "  Proxy:    go run ./cmd/kimi-ai-proxy"
echo "  TUI:      npm run darki"
echo "  Pipe:     npm run darki-pipe -- -p \"msg\""
echo "  URL:      http://localhost:3001"
echo ""
