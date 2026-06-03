#!/usr/bin/env python3
"""Instalador minimo para DARTIK.

Delega para setup.cmd no Windows. Em Linux/macOS, faz o basico.
"""

from __future__ import annotations

import os
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent

if os.name == "nt":
    print("Use setup.cmd no Windows:")
    print("  setup.cmd")
    sys.exit(0)

def run(cmd: str) -> None:
    print(f"> {cmd}")
    subprocess.run(cmd, cwd=ROOT, shell=True, check=True)

def need(exe: str, hint: str) -> None:
    if not shutil.which(exe):
        print(f"Falta: {exe}. {hint}")
        sys.exit(1)

def main() -> None:
    need("go", "Instale Go: https://go.dev/dl/")
    need("node", "Instale Node.js: https://nodejs.org/")
    need("npm", "Instale Node.js: https://nodejs.org/")

    if not (ROOT / ".env").exists():
        shutil.copy2(ROOT / ".env.example", ROOT / ".env")
        print("Criado .env")

    run("npm install --no-fund --no-audit")
    run("npx playwright install chromium")

    resp = input("\nCapturar sessao do Kimi? (s/N): ").strip().lower()
    if resp == "s":
        run("npm run session")

    print("\nInstalacao completa.")
    print("  Proxy:    go run ./cmd/kimi-ai-proxy")
    print("  TUI:      npm run darki")

if __name__ == "__main__":
    main()
