# Kimi AI Proxy

Local OpenAI-compatible proxy for Kimi web.

```text
Base URL: http://localhost:3001/v1
API Key: optional
Model: kimi-k2.6
```

## Features

- `POST /v1/chat/completions`
- `GET /v1/models`
- OpenAI-compatible streaming
- Captured Kimi browser session support
- Optional local agent tools: `read_file`, `write_file`, `web_fetch`, `list_files`, `grep`, `apply_patch`, `run_command`
- Native Kimi search for open-ended web research

## Requirements

- Go
- Node.js LTS
- Python 3 on Windows via `py -3`

## Install And Start

```cmd
py -3 install.py --start --agent
```

Without opening the Kimi login flow again:

```cmd
py -3 install.py --no-login --start --agent
```

The installer creates `.env` if needed, installs Node/Playwright dependencies, captures the Kimi session, and can start the proxy in the background.

## Manual Flow

Capture or refresh the Kimi session:

```cmd
login-kimi.cmd
```

Start in agent mode:

```cmd
start-proxy.cmd
```

Start directly:

```cmd
go run ./cmd/kimi-ai-proxy
```

Health check:

```cmd
curl http://localhost:3001/health
```

## Environment

Create `.env` from `.env.example`.

Core settings:

```env
PORT=3001
API_KEY=
KIMI_STORAGE_STATE=storage/kimi-state.json
KIMI_MODEL=kimi-k2.6
KIMI_REQUEST_TIMEOUT_MS=300000
KIMI_ENABLE_SEARCH_WITH_TOOLS=true
```

Agent mode:

```env
AUTO_TOOLS=true
AUTO_TOOLS_AGENT_MODE=pc
AUTO_TOOLS_WORKSPACE=.
AUTO_TOOLS_MAX_STEPS=6
AUTO_TOOLS_FAST_RETURN=true
AUTO_TOOLS_REQUIRE_DIRECTORY_CONFIRM=true
AUTO_TOOLS_ALLOW_COMMANDS=true
```

Use `AUTO_TOOLS_ALLOW_COMMANDS=true` only in a trusted workspace.

When `AUTO_TOOLS_REQUIRE_DIRECTORY_CONFIRM=true`, file creation/editing requests ask which directory to use unless the user already specified the current directory or a target path.

`web_fetch` is only for specific URLs. Open-ended web research should use Kimi's native search, not a local `web_search` tool.

## OpenCode

Add this provider to `~/.config/opencode/opencode.json`:

```json
{
  "provider": {
    "kimi": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "Kimi Local Proxy",
      "options": {
        "baseURL": "http://localhost:3001/v1",
        "apiKey": "optional",
        "timeout": 300000,
        "chunkTimeout": 120000
      },
      "models": {
        "kimi-k2.6": {
          "name": "Kimi K2.6",
          "tool_call": true,
          "temperature": true,
          "limit": {
            "context": 128000,
            "output": 8192
          }
        }
      }
    }
  },
  "model": "kimi/kimi-k2.6",
  "small_model": "kimi/kimi-k2.6"
}
```

## Kilo Code

Use an OpenAI-compatible provider:

```text
Provider: OpenAI Compatible
Base URL: http://localhost:3001/v1
API Key: optional
Model ID: kimi-k2.6
```


## Notes

This proxy uses Kimi web internals, not the official Moonshot API. It can break when Kimi changes the website, the session expires, or Cloudflare blocks the browser session.
