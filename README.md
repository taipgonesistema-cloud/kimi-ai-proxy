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
- `POST /v1/clear-chats` — deleta todas as conversas do Kimi via Browser Bridge
- `POST /new` — limpa a sessão atual e cria uma nova conversa no Kimi
- Tool calling no formato OpenAI SDK (`tool_calls` com `type: "function"`)
- Retry automático quando o modelo ignora ferramentas
- Modo **thinking/reasoning** (`KIMI_THINKING=true`)
- Pesquisa web nativa do Kimi para pesquisas abertas

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

| Ferramenta | Descrição |
|------------|-----------|
| `bash` | Executa comandos no workspace |
| `read_file` | Lê arquivos |
| `write_file` | Escreve arquivos |
| `list_files` | Lista diretórios |
| `glob` | Busca arquivos por padrão (ex: `**/*.ts`) |
| `grep` | Busca conteúdo com regex |
| `edit` / `apply_patch` | Substitui texto exato em arquivos |
| `web_fetch` | Baixa conteúdo de URLs |
| `web_search` | Pesquisa web por informações atuais |
| `question` | Pergunta ao usuário e aguarda resposta |
| `clear_chats` | Deleta todas as conversas do Kimi via Playwright (útil quando bate o limite de concorrência) |

- Proteção de path safety (não permite sair do workspace)
- Confirmação opcional por tool (exceto em modo YOLO)

### Prompts (sempre injetados)

- `prompts/system.txt` — identidade e personalidade do Dartik (injetado em toda requisição)
- `prompts/darki.txt` — contrato de tool calling com todas as 12 ferramentas, regras e exemplos (sempre injetado — não depende da mensagem conter `"darki"`)
- `prompts/` é versionado no git — essencial para o funcionamento das tools

---

## Requisitos

- Go
- Node.js LTS
- Chrome (para captura de sessão)

---

## Instalação Rápida

Windows:
```cmd
setup.cmd
```

Linux / macOS:
```bash
chmod +x setup.sh && ./setup.sh
```

Isso instala dependências, configura o `.env` e pergunta se quer capturar a sessão do Kimi.

Após a instalação:

```cmd
start-proxy.cmd        # inicia o proxy (Windows)
npm run darki          # abre o Darki TUI
```

---

## Uso

### Iniciar o Proxy

```cmd
start-proxy.cmd
```

Ou manualmente:

```cmd
go run ./cmd/kimi-ai-proxy
```

O proxy sobe em `http://127.0.0.1:3001`.

### Capturar Sessão do Kimi (primeira vez)

```cmd
login-kimi.cmd
```

Abre o Chrome, limpa cookies/storage, e espera você fazer login no Kimi. Pressione Enter no terminal após o login.

### Darki TUI (Interface Interativa)

```cmd
npm run darki
```

| Comando | Descrição |
|---------|-----------|
| `/new` | Limpa o chat e cria nova sessão |
| `/yolo` | Ativa/desativa YOLO mode (tools sem confirmação) |
| `/session` | Lista sessões salvas (auto-save automático) |
| `/exit` | Sai do Darki TUI |

### Modo Pipe (para Scripts / IAs)

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
│   ├── clear-kimi-chats.mjs    # Limpeza de chats via Browser Bridge (relay)
│   └── save-kimi-session.mjs   # Captura de sessão Playwright
├── prompts/                    # System prompts (gitignored)
│   ├── system.txt              # Prompt principal (sempre injetado)
│   └── darki.txt               # Prompt condicional (keyword "darki")
├── storage/                    # Sessões e estado (gitignored)
│   ├── kimi-state.json         # Sessão capturada do Kimi
│   └── sessions/               # Auto-save das conversas
├── setup.cmd                   # Instalação (Windows)
├── setup.sh                    # Instalação (Linux/macOS)
├── start-proxy.cmd             # Inicia o proxy
├── login-kimi.cmd              # Captura de sessão Kimi
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

- Este proxy usa os **internos da web do Kimi**, não a API oficial da Moonshot. Pode quebrar quando o Kimi atualizar o site, a sessão expirar ou o Cloudflare bloquear.
- `prompts/` e `storage/` estão versionados — dados sensíveis nunca vão para o repositório.
- O modo pipe (`-p`) é ideal para integrar o Darki em scripts, automações e interações com outras IAs.
- **Importante:** O Kimi com prompt injection (system.txt + darki.txt) funciona apenas com `KIMI_THINKING=false` atualmente. O modo thinking (`KIMI_THINKING=true`) ignora os prompts de sistema injetados.
- O Kimi tem um **limite de concorrência** (muitos chats abertos). Quando detectado (`REASON_CHAT_CONCURRENCY_EXCEEDED`), o proxy retorna automaticamente uma `tool_calls` com `clear_chats` em vez de erro HTTP. A TUI/pipe executa a limpeza via Playwright e você pode tentar novamente.
