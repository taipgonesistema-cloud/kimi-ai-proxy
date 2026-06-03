package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"kimi-ai-proxy/internal/kimi"
	"kimi-ai-proxy/internal/prompt"
	"kimi-ai-proxy/internal/tools"
	"kimi-ai-proxy/internal/utils"
)

var chatState = struct {
	sync.Mutex
	ID string
}{}

func newChatID() string {
	id := utils.RandomID()
	id = id[:12] + "4" + id[13:16] + "8" + id[17:]
	return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
}

func CurrentChatID() string {
	chatState.Lock()
	id := chatState.ID
	chatState.Unlock()
	return id
}

func HandleNewChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	chatState.Lock()
	chatState.ID = ""
	id := chatState.ID
	chatState.Unlock()
	WriteJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "chat_id": id, "message": "next request will let Kimi create a new chat"})
}

func HandleHealth(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeClearChatsResponse(w http.ResponseWriter, model string) {
	id := "chatcmpl-" + utils.RandomID()
	finish := "tool_calls"
	args := "{}"
	tc := []utils.ToolCall{{
		ID:       "call_" + utils.RandomID(),
		Type:     "function",
		Function: utils.ToolFunction{Name: "clear_chats", Arguments: args},
	}}
	WriteJSON(w, http.StatusOK, utils.OpenAIResponse{
		ID: id, Object: "chat.completion", Created: time.Now().Unix(), Model: model,
		Choices: []utils.OpenAIChoice{{
			Index: 0,
			Message: utils.OpenAIMessage{
				Role:      "assistant",
				Content:   "Kimi chat limit reached. Call clear_chats to delete all old chats and retry.",
				ToolCalls: tc,
			},
			FinishReason: &finish,
		}},
		Usage: utils.EstimateUsage("", ""),
	})
}

func HandleClearChats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	cmd := exec.Command("node", "scripts/clear-kimi-chats.mjs")
	cmd.Dir = utils.WorkspaceRoot()
	output, err := cmd.CombinedOutput()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to clear chats: "+err.Error()+": "+string(output))
		return
	}
	WriteJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "chats cleared"})
}

func HandleModels(w http.ResponseWriter, r *http.Request) {
	model := utils.GetEnv("KIMI_MODEL", "kimi-k2.6")
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"object": "list",
		"data": []map[string]interface{}{{
			"id":       model,
			"object":   "model",
			"created":  time.Now().Unix(),
			"owned_by": "kimi-web",
		}},
	})
}

func HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var input utils.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if len(input.Messages) == 0 {
		WriteError(w, http.StatusBadRequest, "messages is required")
		return
	}
	if input.Model == "" {
		input.Model = utils.GetEnv("KIMI_MODEL", "kimi-k2.6")
	}
	if input.User == "" {
		input.User = CurrentChatID()
	}

	clientHasTools := len(input.Tools) > 0
	useLocalTools := AutoToolsEnabledForRequest(r) && !clientHasTools
	if useLocalTools {
		input.Tools = MergeTools(input.Tools, tools.LocalTools())
	}
	if clientHasTools {
		if content, ok := prompt.ClientToolResultFinalResponse(input.Messages); ok {
			id := "chatcmpl-" + utils.RandomID()
			if input.Stream {
				kimi.StreamCollectedOpenAI(w, id, input.Model, "", content, nil, "stop")
				return
			}
			WriteAssistantText(w, id, input.Model, content)
			return
		}
		if content, ok := prompt.ClientToolResultClarification(input.Messages); ok {
			id := "chatcmpl-" + utils.RandomID()
			if input.Stream {
				kimi.StreamCollectedOpenAI(w, id, input.Model, "", content, nil, "stop")
				return
			}
			WriteAssistantText(w, id, input.Model, content)
			return
		}
		if _, toolName, ok := prompt.LatestClientToolResult(input.Messages); ok {
			input.Tools = RemoveToolByName(input.Tools, toolName)
		}
	}
	if useLocalTools && prompt.NeedsDirectoryConfirmation(input.Messages) {
		id := "chatcmpl-" + utils.RandomID()
		content := "Qual diretorio devo usar para criar/editar arquivos? Responda `atual` para usar `" + utils.WorkspaceRoot() + "`, ou envie um caminho/subdiretorio especifico."
		if input.Stream {
			kimi.StreamCollectedOpenAI(w, id, input.Model, content, content, nil, "stop")
			return
		}
		WriteAssistantText(w, id, input.Model, content)
		return
	}

	p := prompt.RenderPrompt(input.Messages, input.Tools)
	if useLocalTools && !input.Stream {
		response, err := tools.RunAutoToolLoop(input, p)
		if err != nil {
			if strings.Contains(err.Error(), "concurrency limit") {
				writeClearChatsResponse(w, input.Model)
				return
			}
			WriteError(w, http.StatusBadGateway, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, response)
		return
	}

	resp, err := kimi.CallKimi(p, input.User, utils.ShouldEnableKimiSearch(input.Messages, len(input.Tools) > 0))
	if err != nil {
		if strings.Contains(err.Error(), "concurrency limit") {
			writeClearChatsResponse(w, input.Model)
			return
		}
		WriteError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Close()

	id := "chatcmpl-" + utils.RandomID()

	if input.Stream && len(input.Tools) == 0 {
		kimi.StreamOpenAI(w, resp, id, input.Model, p)
		return
	}

	content, err := kimi.CollectKimiText(resp)
	if err != nil {
		WriteError(w, http.StatusBadGateway, err.Error())
		return
	}
	cleanContent, toolCalls := utils.ParseToolCalls(content)
	content, cleanContent, toolCalls, p = normalizeOrRetryToolCalls(input, content, p)

	if useLocalTools && len(toolCalls) > 0 {
		messages := append([]utils.Message(nil), input.Messages...)
		for step := 0; step < 10 && len(toolCalls) > 0; step++ {
			messages = append(messages, utils.Message{Role: "assistant", Content: cleanContent, ToolCalls: toolCalls})
			for _, call := range toolCalls {
				result := tools.ExecuteLocalTool(call)
				messages = append(messages, utils.Message{Role: "tool", ToolCallID: call.ID, Content: result.Content})
			}
			loopPrompt := prompt.RenderPrompt(messages, input.Tools) + "\n\nContinue after the tool result."
			loopResp, loopErr := kimi.CallKimi(loopPrompt, input.User, utils.ShouldEnableKimiSearch(messages, true))
			if loopErr != nil {
				break
			}
			loopContent, loopCollectErr := kimi.CollectKimiText(loopResp)
			loopResp.Close()
			if loopCollectErr != nil {
				break
			}
			content = loopContent
			cleanContent, toolCalls = utils.ParseToolCalls(content)
			if len(toolCalls) > 0 {
				toolCalls, _ = tools.NormalizeToolCalls(toolCalls, input.Tools)
			}
		}
		if input.Stream {
			kimi.StreamCollectedOpenAI(w, id, input.Model, p, content, nil, "stop")
			return
		}
		finish := "stop"
		WriteJSON(w, http.StatusOK, utils.OpenAIResponse{
			ID: id, Object: "chat.completion", Created: time.Now().Unix(), Model: input.Model,
			Choices: []utils.OpenAIChoice{{Index: 0, Message: utils.OpenAIMessage{Role: "assistant", Content: content}, FinishReason: &finish}},
			Usage:   utils.EstimateUsage(p, content),
		})
		return
	}
	if clientHasTools && len(toolCalls) > 0 {
		if fallback, ok := prompt.RepeatedClientToolCallFallback(input.Messages, toolCalls); ok {
			content = fallback
			cleanContent = fallback
			toolCalls = nil
		}
	}
	if clientHasTools && len(toolCalls) == 0 && prompt.RefusesLocalFileAccess(strings.ToLower(content)) {
		if fallback, ok := prompt.ClientToolResultFallback(input.Messages); ok {
			content = fallback
			cleanContent = fallback
		}
	}
	if len(toolCalls) == 0 && prompt.HasEisdirResult(input.Messages) {
		id := "chatcmpl-" + utils.RandomID()
		path := prompt.EisdirPath(input.Messages)
		if path == "" {
			path = "."
		}
		tc := directoryListingToolCall(input.Tools, path)
		if len(tc) == 0 {
			WriteAssistantText(w, id, input.Model, "O read falhou porque o caminho e um diretorio, mas nenhuma ferramenta de listagem esta disponivel.")
			return
		}
		finish := "tool_calls"
		if input.Stream {
			kimi.StreamCollectedOpenAI(w, id, input.Model, "", "", tc, "tool_calls")
			return
		}
		WriteJSON(w, http.StatusOK, utils.OpenAIResponse{
			ID: id, Object: "chat.completion", Created: time.Now().Unix(), Model: input.Model,
			Choices: []utils.OpenAIChoice{{Index: 0, Message: utils.OpenAIMessage{Role: "assistant", Content: "", ToolCalls: tc}, FinishReason: &finish}},
			Usage:   utils.EstimateUsage("", ""),
		})
		return
	}
	if len(toolCalls) == 0 && clientHasTools && prompt.HasEisdirResult(input.Messages) && !prompt.RefusesLocalFileAccess(strings.ToLower(content)) {
		messages := append([]utils.Message(nil), input.Messages...)
		messages = append(messages, utils.Message{Role: "assistant", Content: content})
		messages = append(messages, utils.Message{Role: "user", Content: "O read falhou porque o caminho e um diretorio, nao um arquivo. Use ls agora para listar o diretorio. Responda apenas com o JSON da tool call ls."})
		retryPrompt := prompt.RenderPrompt(messages, input.Tools)
		retryResp, err := kimi.CallKimi(retryPrompt, input.User, utils.ShouldEnableKimiSearch(messages, len(input.Tools) > 0))
		if err == nil {
			retryContent, collectErr := kimi.CollectKimiText(retryResp)
			retryResp.Close()
			if collectErr == nil {
				content = retryContent
				cleanContent, toolCalls = utils.ParseToolCalls(content)
				if len(toolCalls) > 0 {
					toolCalls, _ = tools.NormalizeToolCalls(toolCalls, input.Tools)
				}
				p = retryPrompt
			}
		}
	}
	if len(toolCalls) == 0 && clientHasTools && prompt.ShouldRetryWithClientTool(input.Messages, input.Tools, content) {
		messages := append([]utils.Message(nil), input.Messages...)
		messages = append(messages, utils.Message{Role: "assistant", Content: content})
		messages = append(messages, utils.Message{Role: "user", Content: prompt.ClientToolRetryInstruction(input.Tools, input.Messages)})
		retryPrompt := prompt.RenderPrompt(messages, input.Tools)
		retryResp, err := kimi.CallKimi(retryPrompt, input.User, utils.ShouldEnableKimiSearch(messages, true))
		if err == nil {
			retryContent, collectErr := kimi.CollectKimiText(retryResp)
			retryResp.Close()
			if collectErr == nil {
				content = retryContent
				cleanContent, toolCalls = utils.ParseToolCalls(content)
				if len(toolCalls) > 0 {
					toolCalls, _ = tools.NormalizeToolCalls(toolCalls, input.Tools)
				}
				p = retryPrompt
			}
		}
	}
	finish := "stop"
	if len(toolCalls) > 0 {
		finish = "tool_calls"
		content = cleanContent
	}
	if input.Stream {
		kimi.StreamCollectedOpenAI(w, id, input.Model, p, content, toolCalls, finish)
		return
	}
	WriteJSON(w, http.StatusOK, utils.OpenAIResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   input.Model,
		Choices: []utils.OpenAIChoice{{
			Index: 0,
			Message: utils.OpenAIMessage{
				Role:      "assistant",
				Content:   content,
				ToolCalls: toolCalls,
			},
			FinishReason: &finish,
		}},
		Usage: utils.EstimateUsage(p, content),
	})
}

func normalizeOrRetryToolCalls(input utils.ChatRequest, content, currentPrompt string) (string, string, []utils.ToolCall, string) {
	cleanContent, toolCalls := utils.ParseToolCalls(content)
	if len(toolCalls) == 0 {
		return content, cleanContent, nil, currentPrompt
	}
	normalized, validationErrors := tools.NormalizeToolCalls(toolCalls, input.Tools)
	if len(validationErrors) == 0 {
		return content, cleanContent, normalized, currentPrompt
	}

	messages := append([]utils.Message(nil), input.Messages...)
	messages = append(messages, utils.Message{Role: "assistant", Content: content})
	messages = append(messages, utils.Message{Role: "user", Content: tools.ToolValidationRetryInstruction(validationErrors, input.Tools)})
	retryPrompt := prompt.RenderPrompt(messages, input.Tools)
	retryResp, err := kimi.CallKimi(retryPrompt, input.User, utils.ShouldEnableKimiSearch(messages, len(input.Tools) > 0))
	if err != nil {
		message := "Tool call failed validation: " + strings.Join(validationErrors, "; ")
		return message, message, nil, currentPrompt
	}
	retryContent, collectErr := kimi.CollectKimiText(retryResp)
	retryResp.Close()
	if collectErr != nil {
		message := "Tool call failed validation: " + strings.Join(validationErrors, "; ")
		return message, message, nil, currentPrompt
	}
	retryClean, retryCalls := utils.ParseToolCalls(retryContent)
	normalized, validationErrors = tools.NormalizeToolCalls(retryCalls, input.Tools)
	if len(validationErrors) > 0 {
		message := "Tool call failed validation: " + strings.Join(validationErrors, "; ")
		return message, message, nil, retryPrompt
	}
	return retryContent, retryClean, normalized, retryPrompt
}

func directoryListingToolCall(toolDefs []utils.Tool, path string) []utils.ToolCall {
	name := ""
	args := map[string]interface{}{}
	switch {
	case requestHasTool(toolDefs, "list_files"):
		name = "list_files"
		args["path"] = path
	case requestHasTool(toolDefs, "ls"):
		name = "ls"
		args["path"] = path
	case requestHasTool(toolDefs, "bash"):
		name = "bash"
		args["command"] = "dir /s /b \"" + strings.ReplaceAll(path, "/", "\\") + "\""
	case requestHasTool(toolDefs, "run_command"):
		name = "run_command"
		args["command"] = "dir /s /b \"" + strings.ReplaceAll(path, "/", "\\") + "\""
	default:
		return nil
	}
	encoded, _ := json.Marshal(args)
	return []utils.ToolCall{{ID: "call_" + utils.RandomID(), Type: "function", Function: utils.ToolFunction{Name: name, Arguments: string(encoded)}}}
}

func requestHasTool(toolDefs []utils.Tool, name string) bool {
	for _, t := range toolDefs {
		if t.Function.Name == name {
			return true
		}
	}
	return false
}

func WriteAssistantText(w http.ResponseWriter, id, model, content string) {
	finish := "stop"
	WriteJSON(w, http.StatusOK, utils.OpenAIResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []utils.OpenAIChoice{{
			Index:        0,
			Message:      utils.OpenAIMessage{Role: "assistant", Content: content},
			FinishReason: &finish,
		}},
		Usage: utils.EstimateUsage("", content),
	})
}

func WithAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		apiKey := strings.TrimSpace(os.Getenv("API_KEY"))
		if apiKey != "" {
			expected := "Bearer " + apiKey
			if r.Header.Get("Authorization") != expected {
				WriteError(w, http.StatusUnauthorized, "invalid or missing API key")
				return
			}
		}
		next(w, r)
	}
}

func WithCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", utils.GetEnv("CORS_ORIGIN", "*"))
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    "proxy_error",
		},
	})
}

func AutoToolsEnabled() bool {
	return strings.EqualFold(os.Getenv("AUTO_TOOLS"), "true")
}

func AutoToolsEnabledForRequest(r *http.Request) bool {
	if strings.EqualFold(r.Header.Get("X-Kimi-Auto-Tools"), "false") {
		return false
	}
	return AutoToolsEnabled()
}

func MergeTools(existing, extra []utils.Tool) []utils.Tool {
	seen := map[string]bool{}
	var merged []utils.Tool
	for _, t := range existing {
		if t.Function.Name == "" || seen[t.Function.Name] {
			continue
		}
		seen[t.Function.Name] = true
		merged = append(merged, t)
	}
	for _, t := range extra {
		if t.Function.Name == "" || seen[t.Function.Name] {
			continue
		}
		seen[t.Function.Name] = true
		merged = append(merged, t)
	}
	return merged
}

func RemoveToolByName(tools []utils.Tool, name string) []utils.Tool {
	if name == "" {
		return tools
	}
	out := make([]utils.Tool, 0, len(tools))
	for _, t := range tools {
		if t.Function.Name == name {
			continue
		}
		out = append(out, t)
	}
	return out
}

func LoadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), "\"'")
		if key != "" && os.Getenv(key) == "" {
			_ = os.Setenv(key, value)
		}
	}
}
