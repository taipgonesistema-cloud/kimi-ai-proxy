#!/usr/bin/env python3
"""One-click installer for kimi-ai-proxy.

This script keeps secrets out of source files, installs local dependencies,
optionally captures a Kimi web session, and can start the proxy in the
background on Windows.
"""

from __future__ import annotations

import argparse
import os
import shutil
import subprocess
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parent
ENV_FILE = ROOT / ".env"
ENV_EXAMPLE = ROOT / ".env.example"


def run(command: list[str], *, check: bool = True) -> subprocess.CompletedProcess[str]:
    print("\n> " + " ".join(command))
    return subprocess.run(command, cwd=ROOT, text=True, check=check)


def need(command: str, install_hint: str) -> None:
    if shutil.which(command):
        return
    print(f"Missing dependency: {command}")
    print(install_hint)
    raise SystemExit(1)


def ensure_env() -> None:
    if ENV_FILE.exists():
        print(".env already exists; leaving it untouched.")
        return
    if not ENV_EXAMPLE.exists():
        raise SystemExit(".env.example not found")
    ENV_FILE.write_text(ENV_EXAMPLE.read_text(encoding="utf-8"), encoding="utf-8")
    print("Created .env from .env.example.")


def install_dependencies() -> None:
    need("go", "Install Go from https://go.dev/dl/ and reopen this terminal.")
    need("node", "Install Node.js LTS from https://nodejs.org/ and reopen this terminal.")
    need("npm", "Install Node.js LTS from https://nodejs.org/ and reopen this terminal.")
    run(["npm", "install"])
    run(["npx", "playwright", "install", "chromium"])


def capture_session() -> None:
    print("\nA Chromium window will open. Log in to Kimi and wait until the script captures the session.")
    run(["npm", "run", "session"])


def start_proxy(agent: bool) -> None:
    if os.name != "nt":
        env = os.environ.copy()
        if agent:
            env.update(
                {
                    "AUTO_TOOLS": "true",
                    "AUTO_TOOLS_AGENT_MODE": "pc",
                    "AUTO_TOOLS_FAST_RETURN": "true",
                    "AUTO_TOOLS_ALLOW_COMMANDS": "true",
                    "AUTO_TOOLS_WORKSPACE": str(ROOT),
                }
            )
        subprocess.Popen(["go", "run", "."], cwd=ROOT, env=env)
        print("Proxy started in background: http://localhost:3001/v1")
        return

    set_agent = ""
    if agent:
        set_agent = (
            "set AUTO_TOOLS=true&& "
            "set AUTO_TOOLS_AGENT_MODE=pc&& "
            "set AUTO_TOOLS_FAST_RETURN=true&& "
            "set AUTO_TOOLS_ALLOW_COMMANDS=true&& "
            f"set AUTO_TOOLS_WORKSPACE={ROOT}&& "
        )
    command = f'{set_agent}go run . > kimi-proxy.log 2>&1'
    run(["cmd", "/c", "start", "kimi-ai-proxy", "/MIN", "cmd", "/c", command])
    print("Proxy started in background: http://localhost:3001/v1")
    print("Log file: kimi-proxy.log")


def main() -> None:
    parser = argparse.ArgumentParser(description="Install and run kimi-ai-proxy.")
    parser.add_argument("--no-login", action="store_true", help="Skip Kimi browser login/session capture.")
    parser.add_argument("--start", action="store_true", help="Start the proxy after installing.")
    parser.add_argument("--agent", action="store_true", help="Start with local agent tools enabled.")
    args = parser.parse_args()

    os.chdir(ROOT)
    ensure_env()
    install_dependencies()
    if not args.no_login:
        capture_session()
    if args.start:
        start_proxy(args.agent)

    print("\nDone.")
    print("Base URL: http://localhost:3001/v1")
    print("API Key: local-dev-key")
    print("Model: kimi-k2.6")
    if not args.start:
        print("Start manually with: python install.py --no-login --start --agent")


if __name__ == "__main__":
    try:
        main()
    except subprocess.CalledProcessError as exc:
        print(f"Command failed with exit code {exc.returncode}")
        sys.exit(exc.returncode)
