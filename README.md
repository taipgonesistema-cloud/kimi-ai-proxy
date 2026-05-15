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
- Thinking/reasoning mode (`KIMI_THINKING=true`)
- Optional local agent tools: `read_file`, `write_file`, `web_fetch`, `list_files`, `grep`, `apply_patch`, `run_command`
- Native Kimi search for open-ended web research
- XML `<tool_call>` parsing (Qwen-style fallback)
- Schema validation for tool arguments

## Requirements

- Go
- Node.js LTS
- Python 3 on Windows via `py -3`

## Quick Start

```cmd
py -3 install.py --start --agent
```

Without opening the Kimi login flow again:

```cmd
py -3 install.py --no-login --start --agent
```

## Manual

```cmd
login-kimi.cmd                          # capture browser session
go run ./cmd/kimi-ai-proxy              # start proxy
curl http://localhost:3001/health       # health check
```

## Environment

Copy `.env.example` to `.env`:

```env
PORT=3001
API_KEY=
KIMI_STORAGE_STATE=storage/kimi-state.json
KIMI_MODEL=kimi-k2.6
KIMI_THINKING=false
KIMI_ENABLE_SEARCH_WITH_TOOLS=true
```

Enable agent mode:

```env
AUTO_TOOLS=true
AUTO_TOOLS_AGENT_MODE=pc
AUTO_TOOLS_WORKSPACE=.
AUTO_TOOLS_ALLOW_COMMANDS=false
```

## Project Structure

```text
├── cmd/kimi-ai-proxy/    # Entry point
├── internal/
│   ├── server/           # HTTP handlers, auth, CORS
│   ├── kimi/             # Kimi API client (Connect, stream)
│   ├── tools/            # Tool definitions, execution, validation
│   ├── prompt/           # Prompt rendering, refusal detection
│   └── utils/            # Types, parsers (JSON, XML)
├── install.py            # One-click installer
├── login-kimi.cmd        # Browser session capture
└── scripts/              # Playwright helpers
```

## OpenCode

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
          "limit": { "context": 128000, "output": 8192 }
        }
      }
    }
  },
  "model": "kimi/kimi-k2.6"
}
```

## Kilo Code

```text
Provider: OpenAI Compatible
Base URL: http://localhost:3001/v1
API Key: optional
Model ID: kimi-k2.6
```

## Notes

This proxy uses Kimi web internals, not the official Moonshot API. It can break when Kimi changes the website, the session expires, or Cloudflare blocks the browser session.
