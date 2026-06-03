# DARTIK — Kimi AI Proxy

Proxy local compatível com a API OpenAI que usa a versão web do Kimi (`kimi-k2.6`) como backend, com suporte completo a **tool calling**, **streaming**, sessão persistente e uma **TUI Ink** interativa.

```text
Base URL: http://localhost:3001/v1
API Key: opcional
Modelo: kimi-k2.6
```

---

## Funcionalidades

### Proxy OpenAI

- `POST /v1/chat/completions` — streaming e não-streaming
- `GET /v1/models` — lista modelos disponíveis
- `POST /new` — limpa a sessão atual e cria uma nova conversa no Kimi
- Tool calling no formato OpenAI SDK (`tool_calls` com `type: "function"`)
- Retry automático quando o modelo ignora ferramentas
- Modo **thinking/reasoning** (`KIMI_THINKING=true`)
- Pesquisa web nativa do Kimi para pesquisas abertas
- Fallback XML `<tool_call>` (estilo Qwen)
- Validação de schema para argumentos de ferramentas

### Darki TUI (Interface Terminal)

- **TUI Ink** com logo DARKI em vermelho, bordas arredondadas e spinner animado
- Painel de mensagens com cores por papel (YOU, DARKI, CALL, TOOL, ERR)
- **Auto-complete Tab** para comandos
- **Lista de comandos** ao digitar `/` com filtro conforme digita
- **Confirmação de ferramentas** — cada tool call mostra nome, argumentos e aguarda `[Y]es [N]o [A]llow always (YOLO)`
- **Modo YOLO** (`/yolo`) — executa ferramentas automaticamente sem perguntar
- **Comandos:** `/new`, `/yolo`, `/session`, `/exit`

### Sessões Persistentes

- **Auto-save automático** — cada resposta do Darki salva o histórico em `storage/sessions/<sessionId>.json`
- **`/session`** — lista todas as sessões salvas com ID, data, quantidade de mensagens e prévia
- Sessões incluem o histórico completo de mensagens e metadados

### Modo Pipe (`-p`)

- `npm run darki-pipe -- -p "mensagem"` — envia uma mensagem e recebe a resposta no stdout
- `-y` / `--yolo` — executa ferramentas sem confirmação
- Perfeito para scripts, integrações e para a IA interagir com o Darki programaticamente
- Auto-save ativo também no modo pipe

### Ferramentas Locais (Agent Mode)

- `bash` — executa comandos no workspace
- `read_file` — lê arquivos
- `write_file` — escreve arquivos
- `list_files` — lista diretórios
- `grep` — busca conteúdo com regex
- `web_fetch` — baixa conteúdo de URLs
- `apply_patch` — substitui texto exato em arquivos
- Proteção de path safety (não permite sair do workspace)

### Prompts Condicionais

- `prompts/system.txt` — injetado em toda requisição (sempre)
- `prompts/darki.txt` — injetado apenas se a mensagem contiver `"darki"` (permite dois modos no mesmo chat)
- `prompts/` está no `.gitignore` — prompts sensíveis nunca versionados

---

## Requisitos

- Go
- Node.js LTS
- Python 3 (Windows via `py -3`)
- Chrome (para captura de sessão)

---

## Instalação Rápida

```cmd
py -3 install.py --start --agent
```

Para pular o login do Kimi (se já tiver sessão salva):

```cmd
py -3 install.py --no-login --start --agent
```

---

## Uso Manual

### 1. Capturar Sessão do Kimi

```cmd
login-kimi.cmd
```

Ou manualmente:

```cmd
npm run session
```

Isso abre o Chrome, limpa cookies/storage, e espera você fazer login. Pressione Enter no terminal após o login.

### 2. Iniciar o Proxy

```cmd
go run ./cmd/kimi-ai-proxy
```

O proxy sobe em `http://127.0.0.1:3001`.

### 3. Usar o Darki TUI

```cmd
npm run darki
```

Interface interativa com comandos:

| Comando | Descrição |
|---------|-----------|
| `/new` | Limpa o chat e cria nova sessão |
| `/yolo` | Ativa/desativa YOLO mode |
| `/session` | Lista sessões salvas (auto-save) |
| `/exit` | Sai do Darki TUI |

### 4. Usar o Modo Pipe

```cmd
npm run darki-pipe -- -p "fala darki, qual é o seu nome?"
npm run darki-pipe -- -y "cria um arquivo teste.txt"
echo "msg" | npm run darki-pipe
```

---

## Variáveis de Ambiente

Copie `.env.example` para `.env`:

```env
PORT=3001
API_KEY=
KIMI_STORAGE_STATE=storage/kimi-state.json
KIMI_MODEL=kimi-k2.6
KIMI_THINKING=false
KIMI_ENABLE_SEARCH_WITH_TOOLS=true
```

### Modo Agente (ferramentas locais)

```env
AUTO_TOOLS=true
AUTO_TOOLS_AGENT_MODE=pc
AUTO_TOOLS_WORKSPACE=.
AUTO_TOOLS_ALLOW_COMMANDS=false
```

### Personalização

```env
KIMI_PROXY_URL=http://127.0.0.1:3001   # URL do proxy (TUI/pipe)
KIMI_MODEL=kimi-k2.6                    # Modelo (TUI/pipe)
DARKI_WORKSPACE=.                       # Diretório de trabalho (TUI/pipe)
```

---

## Estrutura do Projeto

```text
├── cmd/kimi-ai-proxy/          # Entry point do proxy Go
├── internal/
│   ├── server/                 # Handlers HTTP, auth, CORS
│   ├── kimi/                   # Cliente Kimi API (chat, stream)
│   ├── tools/                  # Definição, execução e validação de ferramentas
│   ├── prompt/                 # Renderização de prompt, detecção de recusa
│   └── utils/                  # Tipos, parsers (JSON, XML)
├── scripts/
│   ├── darki-tui.mjs           # Darki TUI (Ink + React) — interface interativa
│   ├── darki-pipe.mjs          # Darki modo pipe — CLI programática
│   └── save-kimi-session.mjs   # Captura de sessão Playwright
├── prompts/                    # System prompts (gitignored)
│   ├── system.txt              # Prompt principal (sempre injetado)
│   └── darki.txt               # Prompt condicional (keyword "darki")
├── storage/                    # Sessões e estado (gitignored)
│   ├── kimi-state.json         # Sessão capturada do Kimi
│   └── sessions/               # Auto-save das conversas
├── install.py                  # Instalador one-click
├── login-kimi.cmd              # Atalho para captura de sessão
└── package.json                # Scripts npm (darki, darki-pipe, session)
```

---

## Integração com OpenCode

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

## Integração com Kilo Code / Roo Code

```text
Provider: OpenAI Compatible
Base URL: http://localhost:3001/v1
API Key: optional
Model ID: kimi-k2.6
```

---

## Observações

- Este proxy usa os **internos da web do Kimi**, não a API oficial da Moonshot.
- Pode quebrar quando o Kimi atualizar o site, a sessão expirar ou o Cloudflare bloquear.
- `prompts/` e `storage/` estão no `.gitignore` — dados sensíveis nunca vão para o repositório.
- O modo pipe (`-p`) é ideal para integrar o Darki em scripts, automações e interações com outras IAs.
