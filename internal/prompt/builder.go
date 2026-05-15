package prompt

import (
	"encoding/json"
	"fmt"
	"strings"

	"kimi-ai-proxy/internal/utils"
)

func RenderPrompt(messages []utils.Message, tools []utils.Tool) string {
	var system []string
	var turns []string
	alreadyHasToolResult := HasToolResult(messages)
	toolInstructions := FormatToolsAsInstructions(tools, alreadyHasToolResult)
	if toolInstructions != "" {
		system = append(system, toolInstructions)
	}
	if alreadyHasToolResult && len(tools) == 0 {
		system = append(system, "A tool has already run and its output is included below as Tool result. Use that output to answer the user directly. Do not say you lack access to tools. Do not request or emit another tool call.")
	}
	if HasEisdirResult(messages) {
		system = append(system, "The last tool result shows EISDIR: you tried to read a directory with read. The read tool cannot list directories. If you need to list a directory, call ls instead. Do not ask the user what to do next -- call ls now.")
	}
	if RequestMentionsFileCreation(messages) && !alreadyHasToolResult {
		system = append(system, FileTaskInstruction(tools))
	}
	for _, m := range messages {
		text := utils.ContentToText(m.Content)
		switch m.Role {
		case "system":
			if text == "" {
				continue
			}
			system = append(system, text)
		case "assistant":
			if len(m.ToolCalls) > 0 {
				for _, tc := range m.ToolCalls {
					turns = append(turns, fmt.Sprintf("Assistant requested tool %s with arguments: %s", tc.Function.Name, tc.Function.Arguments))
				}
				continue
			}
			if text == "" {
				continue
			}
			turns = append(turns, "Assistant: "+text)
		case "tool", "toolResult":
			turns = append(turns, fmt.Sprintf("Tool result%s: %s", FormatToolCallID(m.ToolCallID), text))
		default:
			if text == "" {
				continue
			}
			turns = append(turns, "User: "+text)
		}
	}
	if len(system) > 0 {
		return strings.Join(system, "\n") + "\n\n" + strings.Join(turns, "\n\n")
	}
	return strings.Join(turns, "\n\n")
}

func FormatToolsAsInstructions(tools []utils.Tool, alreadyHasToolResult bool) string {
	if len(tools) == 0 {
		return ""
	}
	var b strings.Builder
	localToolSet := HasTool(tools, "write_file") || HasTool(tools, "read_file") || HasTool(tools, "run_command")
	if strings.EqualFold(utils.GetEnv("AUTO_TOOLS_AGENT_MODE", "pc"), "pc") {
		b.WriteString("You are running as an autonomous coding agent on a real Windows PC, not as a web chat bot. You can inspect and modify files in the local workspace by calling tools. You are NOT in a Linux container or /mnt/agents. You are NOT in a cloud environment. The local Windows filesystem at C:/Users/Desktop/Desktop/mimo is fully accessible through the listed tools.\n")
		b.WriteString("When the user asks to access, list, enter, or see a directory/folder, use list_files, not read_file. The read_file tool is for files only and will fail on directories.\n")
	}
	if localToolSet {
		b.WriteString("Current local workspace root: " + utils.WorkspaceRoot() + "\n")
		if alreadyHasToolResult {
			b.WriteString("A tool result is already present in this conversation. If it completed the user's request, answer normally and do not call the same tool again.\n")
		} else {
			b.WriteString("When the user asks you to create, save, download, write, edit, generate, or update a file, you MUST call write_file or apply_patch. Do not claim a file was created unless a tool result confirms it. Do not answer with fake links such as 'Baixe aqui'.\n")
		}
		b.WriteString("When the user asks you to run, test, install, build, list, search, or inspect the local computer/project, you MUST use the matching local tool instead of describing what you would do.\n")
	} else {
		if alreadyHasToolResult {
			b.WriteString("A client tool result is already present in this conversation. If it completed the user's request, answer normally and do not call any more tools. Do not repeat the same write or edit call.\n")
		} else {
			b.WriteString("Use only the tools listed below. The client controls the workspace root; do not use the proxy server directory as the project directory. Prefer relative paths unless the user provides an absolute path. Do not invent unavailable tools such as write_file. You have real local file access through the listed client tools; never say you cannot access local Windows files or claim to be in a Linux container. You are NOT in /mnt/agents. When the user asks you to create, save, write, edit, generate, or update a file and gives enough detail, you MUST call an available client file tool such as write or edit. If the user asks to edit a file but does not say what to change, ask what exact change they want. After a successful tool result, answer normally without calling the same tool again. Do not claim a file was created unless a tool result confirms it. Do not answer with fake links such as 'Baixe aqui'.\n")
		}
		b.WriteString("For client tool calls, output strict valid JSON only: escape newlines as \\n inside string values, never put literal line breaks inside JSON strings, and use forward slashes in Windows paths such as C:/Users/Desktop/file.html. For edit tools, use the smallest exact oldString/newString needed instead of huge repeated blocks.\n")
		b.WriteString("When the user asks to access, list, enter, or see a directory/folder, use ls, not read. The read tool is for files only and will fail on directories.\n")
	}
	b.WriteString("Do not invent or call a JSON tool named web_search, search, browser_search, or internet_search. Open-ended web research is handled by Kimi's native search, not by local tools.\n")
	if HasTool(tools, "web_fetch") {
		b.WriteString("When the user provides a specific URL to read, use web_fetch. For current information, rankings, news, benchmarks, prices, or broad web research, rely on Kimi's native web search. Do not say internet is disabled unless the upstream Kimi request actually fails.\n")
	} else {
		b.WriteString("For current information, rankings, news, benchmarks, prices, or broad web research, rely on Kimi's native web search. Do not say internet is disabled unless the upstream Kimi request actually fails.\n")
	}
	b.WriteString("If a tool is needed, respond ONLY with one JSON object and no markdown. Format: {\"name\":\"tool_name\",\"arguments\":{...}}.\n")
	b.WriteString("Available tools:\n")
	for _, t := range tools {
		if t.Type != "function" || t.Function.Name == "" {
			continue
		}
		params, _ := json.Marshal(t.Function.Parameters)
		b.WriteString(fmt.Sprintf("- %s: %s Parameters: %s\n", t.Function.Name, t.Function.Description, string(params)))
	}
	return b.String()
}

func FileTaskInstruction(tools []utils.Tool) string {
	if HasTool(tools, "write_file") || HasTool(tools, "apply_patch") {
		return "The user's request is a file creation/editing task. If the target directory is unclear, ask which directory to use before calling write_file or apply_patch. If the target directory is clear, your first action must be write_file or apply_patch. Do not draft the file in chat. Do not provide a download link. Save the file on the local PC."
	}
	if HasTool(tools, "write") || HasTool(tools, "edit") {
		return "The user's request is a file creation/editing task. If the target directory is unclear, ask which directory to use before calling a file-writing tool. If the target directory is clear, use the available client file tool, such as write or edit. Do not invent write_file. Do not draft the file in chat. Do not provide a download link."
	}
	return "The user's request is a file creation/editing task. Use only the file-writing tools listed below. Do not invent unavailable tools such as write_file. Do not provide a download link."
}

func HasTool(tools []utils.Tool, name string) bool {
	for _, t := range tools {
		if t.Function.Name == name {
			return true
		}
	}
	return false
}

func HasToolResult(messages []utils.Message) bool {
	for _, m := range messages {
		if m.Role == "tool" || m.Role == "toolResult" {
			return true
		}
	}
	return false
}

func HasEisdirResult(messages []utils.Message) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.Role != "tool" && m.Role != "toolResult" {
			continue
		}
		return strings.Contains(strings.ToLower(utils.ContentToText(m.Content)), "eisdir")
	}
	return false
}

func EisdirPath(messages []utils.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.Role != "tool" && m.Role != "toolResult" {
			continue
		}
		if !strings.Contains(strings.ToLower(utils.ContentToText(m.Content)), "eisdir") {
			return ""
		}
		toolID := m.ToolCallID
		for j := i - 1; j >= 0; j-- {
			if len(messages[j].ToolCalls) > 0 {
				for _, call := range messages[j].ToolCalls {
					if call.ID == toolID {
						var args map[string]interface{}
						if json.Unmarshal([]byte(call.Function.Arguments), &args) != nil {
							continue
						}
						path, _ := args["path"].(string)
						path = strings.ReplaceAll(path, "\\", "/")
						return path
					}
				}
			}
		}
		return ""
	}
	return ""
}

func ClientToolResultFinalResponse(messages []utils.Message) (string, bool) {
	text, toolName, ok := LatestClientToolResult(messages)
	if !ok {
		return "", false
	}
	lower := strings.ToLower(text)
	if (toolName == "write" || toolName == "edit") && (strings.Contains(lower, "successfully") || strings.Contains(lower, "sucesso") || strings.Contains(lower, "wrote") || strings.Contains(lower, "updated") || strings.Contains(lower, "criado") || strings.Contains(lower, "atualizado")) {
		return "Concluido. " + text, true
	}
	return "", false
}

func ClientToolResultClarification(messages []utils.Message) (string, bool) {
	_, toolName, ok := LatestClientToolResult(messages)
	if !ok || toolName != "read" {
		return "", false
	}
	latest := LatestUserText(messages)
	if IsVagueEditRequest(latest) {
		return "Li o README.md. O que voce quer alterar nele?", true
	}
	return "", false
}

func LatestClientToolResult(messages []utils.Message) (string, string, bool) {
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.Role == "user" {
			return "", "", false
		}
		if m.Role == "assistant" && len(m.ToolCalls) == 0 && strings.TrimSpace(utils.ContentToText(m.Content)) != "" {
			return "", "", false
		}
		if m.Role != "tool" && m.Role != "toolResult" {
			continue
		}
		text := strings.TrimSpace(utils.ContentToText(m.Content))
		if text == "" {
			return "", "", false
		}
		return text, ToolNameForCallID(messages[:i], m.ToolCallID), true
	}
	return "", "", false
}

func ClientToolResultFallback(messages []utils.Message) (string, bool) {
	_, toolName, ok := LatestClientToolResult(messages)
	if !ok {
		return "", false
	}
	latest := LatestUserText(messages)
	if toolName == "read" && MentionsFileCreation(latest) {
		return "Li o arquivo. O que voce quer alterar nele?", true
	}
	if toolName == "ls" {
		return "Consegui listar a pasta. O que voce quer fazer com esses arquivos?", true
	}
	return "", false
}

func RepeatedClientToolCallFallback(messages []utils.Message, calls []utils.ToolCall) (string, bool) {
	if len(calls) != 1 {
		return "", false
	}
	call := calls[0]
	result, ok := CompletedToolCallResult(messages, call.Function.Name, call.Function.Arguments)
	if !ok {
		return "", false
	}
	path := ToolArgPath(call.Function.Arguments)
	if path != "" {
		return "Ja executei " + call.Function.Name + " em `" + path + "`. Resultado:\n" + result, true
	}
	return "Ja executei " + call.Function.Name + " com esses argumentos. Resultado:\n" + result, true
}

func CompletedToolCallResult(messages []utils.Message, name, args string) (string, bool) {
	sig := ToolSignature(name, args)
	if sig == "" {
		return "", false
	}
	completed := map[string]string{}
	for _, m := range messages {
		if m.Role == "tool" || m.Role == "toolResult" {
			completed[m.ToolCallID] = strings.TrimSpace(utils.ContentToText(m.Content))
		}
	}
	for _, m := range messages {
		if m.Role != "assistant" {
			continue
		}
		for _, prior := range m.ToolCalls {
			result := completed[prior.ID]
			if result == "" {
				continue
			}
			if ToolSignature(prior.Function.Name, prior.Function.Arguments) == sig {
				return result, true
			}
		}
	}
	return "", false
}

func ToolSignature(name, args string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return name + ":" + CanonicalJSON(args)
}

func CanonicalJSON(text string) string {
	var v interface{}
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		return strings.TrimSpace(text)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return strings.TrimSpace(text)
	}
	return string(b)
}

func ToolArgPath(args string) string {
	var v map[string]interface{}
	if err := json.Unmarshal([]byte(args), &v); err != nil {
		return ""
	}
	path, _ := v["path"].(string)
	return path
}

func ToolNameForCallID(messages []utils.Message, id string) string {
	if id == "" {
		return ""
	}
	for i := len(messages) - 1; i >= 0; i-- {
		for _, call := range messages[i].ToolCalls {
			if call.ID == id {
				return call.Function.Name
			}
		}
	}
	return ""
}

func FormatToolCallID(id string) string {
	if id == "" {
		return ""
	}
	return " (" + id + ")"
}

func RequestMentionsFileCreation(messages []utils.Message) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return MentionsFileCreation(strings.ToLower(utils.ContentToText(messages[i].Content)))
		}
	}
	return false
}

func NeedsDirectoryConfirmation(messages []utils.Message) bool {
	if !strings.EqualFold(utils.GetEnv("AUTO_TOOLS_REQUIRE_DIRECTORY_CONFIRM", "true"), "true") {
		return false
	}
	latest := LatestUserText(messages)
	if latest == "" || !MentionsFileCreation(latest) {
		return false
	}
	return !MentionsDirectoryChoice(latest)
}

func MentionsDirectoryChoice(text string) bool {
	phrases := []string{
		"pasta atual", "diretorio atual", "diretório atual", "workspace atual", "diretorio corrente", "diretório corrente",
		"current directory", "current folder", "current workspace", "nesta pasta", "nessa pasta", "aqui", "here",
		"path:", "pasta:", "folder:", "diretorio:", "diretório:", "em ./", "em .\\", " no ./", " na ./",
	}
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return strings.Contains(text, ":\\") || strings.Contains(text, "./") || strings.Contains(text, ".\\") || strings.Contains(text, "/") || strings.Contains(text, "\\")
}

func ShouldRetryWithClientTool(messages []utils.Message, tools []utils.Tool, content string) bool {
	if !(HasTool(tools, "write") || HasTool(tools, "edit") || HasTool(tools, "read") || HasTool(tools, "ls")) {
		return false
	}
	lower := strings.ToLower(content)
	if RequestMentionsFileCreation(messages) {
		return strings.Contains(lower, "baixe aqui") || strings.Contains(lower, "download") || strings.Contains(lower, ".html") || strings.Contains(lower, "arquivo") || strings.Contains(lower, "salvo") || strings.Contains(lower, "criei") || strings.Contains(lower, "criado") || strings.Contains(lower, "pronto") || RefusesLocalFileAccess(lower)
	}
	if RequestMentionsDirectoryAccess(messages) {
		return strings.Contains(lower, "não existe") || strings.Contains(lower, "nao existe") || strings.Contains(lower, "does not exist") || strings.Contains(lower, "doesn't exist") || RefusesLocalFileAccess(lower)
	}
	return false
}

func ClientToolRetryInstruction(tools []utils.Tool, messages []utils.Message) string {
	lastUser := GoLastUserText(messages)
	if HasTool(tools, "ls") && !HasTool(tools, "edit") {
		if strings.Contains(strings.ToLower(lastUser), "acess") || strings.Contains(strings.ToLower(lastUser), "entr") || strings.Contains(strings.ToLower(lastUser), "list") || strings.Contains(strings.ToLower(lastUser), "mostr") || strings.Contains(strings.ToLower(lastUser), "ach") || strings.Contains(strings.ToLower(lastUser), "encontr") || strings.Contains(strings.ToLower(lastUser), "ver") {
			return "Voce afirmou que um diretorio nao existe ou que esta em um container. Isso e falso. Voce esta rodando diretamente no Windows do usuario. Use ls agora para listar o diretorio solicitado. Responda apenas com o JSON da tool call."
		}
	}
	if HasTool(tools, "edit") {
		return "You responded without using the available client file tools. This is a coding agent, not a browser chat. You do have local file access through the listed tools. If the user requested a specific edit and gave enough detail, call the available edit tool now and respond only with the JSON tool call. If the user only said to edit a file but did not say what to change, ask one short clarification question. Never say you cannot access local Windows files."
	}
	return "You responded without using the available client file tools. This is a coding agent, not a browser chat. You do have local file access through the listed tools. If the user requested a specific file write and gave enough detail, call the available write tool now and respond only with the JSON tool call. If the user did not say what content/change they want, ask one short clarification question. Never say you cannot access local Windows files."
}

func RequestMentionsDirectoryAccess(messages []utils.Message) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			text := strings.ToLower(utils.ContentToText(messages[i].Content))
			keywords := []string{"acessa", "acessar", "entra", "entrar", "lista", "listar", "mostra", "mostrar", "ve", "ver", "ache", "achar", "encontre", "encontrar"}
			for _, kw := range keywords {
				if strings.Contains(text, kw) {
					return true
				}
			}
			return false
		}
	}
	return false
}

func GoLastUserText(messages []utils.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return utils.ContentToText(messages[i].Content)
		}
	}
	return ""
}

func RefusesLocalFileAccess(text string) bool {
	phrases := []string{
		"não consigo acessar diretamente arquivos",
		"nao consigo acessar diretamente arquivos",
		"não consigo acessar arquivos",
		"nao consigo acessar arquivos",
		"não tenho acesso",
		"nao tenho acesso",
		"cannot access",
		"can't access",
		"do not have access",
		"don't have access",
		"não permitem leitura/escrita",
		"nao permitem leitura/escrita",
		"copiar e colar o conteúdo",
		"copiar e colar o conteudo",
		"upload do arquivo",
		"/mnt/agents",
		"containerizado",
		"container",
	}
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func IsVagueEditRequest(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" || !MentionsEditIntent(text) {
		return false
	}
	changeHints := []string{"adicione", "adicionar", "inclua", "incluir", "remova", "remover", "troque", "trocar", "substitua", "substituir", "mude", "mudar", "corrija", "corrigir", "para", "por", "com", "linha", "secao", "seção", "texto", "conteudo", "conteúdo", "english", "portugues", "português"}
	for _, hint := range changeHints {
		if strings.Contains(text, hint) {
			return false
		}
	}
	return true
}

func MentionsEditIntent(text string) bool {
	keywords := []string{"edite", "editar", "edita", "altere", "alterar", "modifique", "modificar", "atualize", "atualizar", "edit", "modify", "update"}
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func MentionsFileCreation(text string) bool {
	keywords := []string{"crie", "criar", "cria", "gere", "gerar", "salve", "salvar", "edite", "editar", "edita", "altere", "alterar", "modifique", "modificar", "atualize", "atualizar", "write", "create", "save", "edit", "modify", "update", "download", "arquivo", "file", "readme", ".html", ".js", ".css", ".md", ".txt", ".json"}
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func LatestUserText(messages []utils.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return strings.ToLower(utils.ContentToText(messages[i].Content))
		}
	}
	return ""
}
