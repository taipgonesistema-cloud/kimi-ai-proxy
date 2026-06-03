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
- `POST /v1/clear-chats` — deleta todas as conversas do Kimi via Playwright
- `POST /new` — limpa a sessão atual e cria uma nova conversa no Kimi
- Tool calling no formato OpenAI SDK (`tool_calls` com `type: "function"`)
- Separação estrita de tools: se o cliente envia `tools`, o proxy usa só as tools do cliente; se a requisição não envia `tools` e `AUTO_TOOLS=true`, o proxy injeta/executa suas tools locais
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
| `run_command` (`bash`) | Executa comandos no workspace |
| `read_file` | Lê arquivos |
| `write_file` | Escreve arquivos |
| `list_files` | Lista diretórios |
| `glob` | Busca arquivos por padrão (ex: `**/*.ts`) |
| `grep` | Busca conteúdo com regex |
| `apply_patch` (`edit`) | Substitui texto exato em arquivos |
| `web_fetch` | Baixa conteúdo de URLs |
| `clear_chats` | Deleta todas as conversas do Kimi via Playwright (útil quando bate o limite de concorrência) |

- Proteção de path safety (não permite sair do workspace)
- Confirmação opcional por tool (exceto em modo YOLO)

### Prompts (sempre injetados)

- `prompts/system.txt` — identidade e personalidade do Dartik (injetado em toda requisição)
- `prompts/darki.txt` — protocolo de tool calling (sempre injetado — a lista real de tools vem do schema dinâmico de cada request)
- `prompts/` é versionado no git — essencial para o funcionamento das tools

### Tool Calling Reforçado

- O `system.txt` foi preservado e continua sendo o prompt principal do Kimi.
- O `darki.txt` agora é apenas o protocolo de tool calling, sem lista fixa de ferramentas.
- A lista real de tools vem do schema dinâmico de cada request.
- O proxy normaliza aliases comuns: `bash -> run_command`, `edit -> apply_patch`, `read -> read_file`, `write -> write_file`, `ls -> list_files`.
- O proxy normaliza argumentos comuns: `oldString/newString`, `cmd`, `directory`, `folder`, `glob`, `query`.
- Tool calls são validadas contra o schema antes de executar ou retornar ao cliente.
- Se o Kimi gerar uma tool inválida, o proxy faz retry com erro de schema direcionado.
- Client tools e proxy tools ficam separados por padrão para não quebrar Kilo/pi.dev/OpenCode.
- Tarefas multi-etapa com client tools não são finalizadas cedo após a primeira escrita; o proxy deixa o cliente continuar o loop.

---

## Requisitos

- Go
- Node.js LTS
- Chrome (para captura de sessão)
- `rg` (ripgrep) e `fd` para as tools `grep`/`find` do pi.dev funcionarem sem fallback

No Windows desta maquina, `rg.exe` e `fd.exe` foram instalados em:

```text
C:\Users\Desktop\AppData\Roaming\npm\rg.exe
C:\Users\Desktop\AppData\Roaming\npm\fd.exe
```

Os binarios portaveis baixados tambem ficam em:

```text
C:\Users\Desktop\Desktop\yk\tools\bin\
```

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
KIMI_REQUEST_TIMEOUT_MS=45000
KIMI_ENABLE_SEARCH_WITH_TOOLS=true
```

### Modo Agente (ferramentas locais)

```env
AUTO_TOOLS=true
AUTO_TOOLS_AGENT_MODE=pc
AUTO_TOOLS_WORKSPACE=.
AUTO_TOOLS_ALLOW_COMMANDS=false
AUTO_TOOLS_REQUIRE_DIRECTORY_CONFIRM=false
AUTO_TOOLS_MAX_STEPS=6
AUTO_TOOLS_FAST_RETURN=true
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
│   ├── clear-kimi-chats.mjs    # Limpeza de chats via Playwright
│   └── save-kimi-session.mjs   # Captura de sessão Playwright
├── prompts/                    # System prompts (versionados)
│   ├── system.txt              # Prompt principal (sempre injetado)
│   └── darki.txt               # Protocolo de tools (sempre injetado)
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

## Integração com pi.dev

O provider `kimi-local` em `~/.pi/agent/models.json` aponta para o proxy:

```json
{
  "providers": {
    "kimi-local": {
      "baseUrl": "http://localhost:3001/v1",
      "api": "openai-completions",
      "apiKey": "dummy",
      "compat": {
        "supportsDeveloperRole": false,
        "supportsReasoningEffort": false,
        "supportsUsageInStreaming": false
      },
      "models": [
        {
          "id": "kimi-k2.6",
          "name": "Kimi K2.6 Local Proxy (Darki)",
          "reasoning": false,
          "contextWindow": 128000,
          "maxTokens": 8192
        }
      ]
    }
  }
}
```

Exemplo de uso:

```cmd
pi --model kimi-local/kimi-k2.6 --tools read,bash,edit,write,grep,find,ls -p "crie um arquivo nesta pasta"
```

Com `tools` enviadas pelo pi.dev, o proxy nao executa tools locais; ele apenas devolve `tool_calls` OpenAI para o pi.dev executar no `cwd` da CLI.

As tools `grep` e `find` do pi.dev dependem de `rg` e `fd`. Sem eles, o pi.dev pode retornar erros como:

```text
ripgrep (rg) is not available and could not be downloaded
fd is not available and could not be downloaded
```

### Testes Realizados

- Proxy local sem `tools`: criou `resultado.txt` dentro de `yk\sandbox` usando `write_file` do proxy.
- pi.dev com client tools: criou `pi_client_tool_result.txt` dentro do `cwd` do pi.dev, sem executar `write_file` do proxy.
- pi.dev multi-etapa: usou listagem, leitura, escrita, edição e terminal sem o prompt citar nomes de tools.
- Apos instalar `rg` e `fd`, pi.dev usou `find`, `grep`, `read` e `write` corretamente para gerar `installed_tools_report.txt`.

---

## Observações

- Este proxy usa os **internos da web do Kimi**, não a API oficial da Moonshot. Pode quebrar quando o Kimi atualizar o site, a sessão expirar ou o Cloudflare bloquear.
- `prompts/` é versionado; `storage/` é gitignored para manter sessão/cookies fora do repositório.
- O modo pipe (`-p`) é ideal para integrar o Darki em scripts, automações e interações com outras IAs.
- O proxy mantém **client tools** e **proxy tools** separados. Kilo/pi.dev/OpenCode com `tools` próprios recebem somente tool calls para as tools deles; requisições sem `tools` usam as tools locais do proxy quando `AUTO_TOOLS=true`.
- **Importante:** O Kimi com prompt injection (system.txt + darki.txt) funciona apenas com `KIMI_THINKING=false` atualmente. O modo thinking (`KIMI_THINKING=true`) ignora os prompts de sistema injetados.
- O Kimi tem um **limite de concorrência** (muitos chats abertos). Quando detectado (`REASON_CHAT_CONCURRENCY_EXCEEDED`), o proxy retorna automaticamente uma `tool_calls` com `clear_chats` em vez de erro HTTP. A TUI/pipe executa a limpeza via Playwright e você pode tentar novamente.
