package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type message struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	ToolCalls  []toolCall  `json:"tool_calls,omitempty"`
}

type chatRequest struct {
	Model      string      `json:"model"`
	Messages   []message   `json:"messages"`
	Stream     bool        `json:"stream"`
	User       string      `json:"user"`
	Tools      []tool      `json:"tools"`
	ToolChoice interface{} `json:"tool_choice"`
}

type tool struct {
	Type     string       `json:"type"`
	Function functionTool `json:"function"`
}

type functionTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters,omitempty"`
}

type toolCall struct {
	Index    int          `json:"index,omitempty"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function toolFunction `json:"function"`
}

type toolFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments"`
}

type localToolResult struct {
	Name    string
	Content string
	OK      bool
	Path    string
	Summary string
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
	Usage   usage          `json:"usage"`
}

type openAIChoice struct {
	Index        int           `json:"index"`
	Message      openAIMessage `json:"message,omitempty"`
	Delta        openAIMessage `json:"delta,omitempty"`
	FinishReason *string       `json:"finish_reason"`
}

type openAIMessage struct {
	Role      string     `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []toolCall `json:"tool_calls,omitempty"`
}

func main() {
	loadDotEnv(".env")

	port := getenv("PORT", "3001")
	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/v1/models", withCORS(withAuth(handleModels)))
	mux.HandleFunc("/v1/chat/completions", withCORS(withAuth(handleChatCompletions)))
	mux.HandleFunc("/", withCORS(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"name": "kimi-ai-proxy", "status": "ok"})
	}))

	log.Printf("Kimi proxy listening on 0.0.0.0:%s", port)
	log.Fatal(http.ListenAndServe("0.0.0.0:"+port, mux))
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleModels(w http.ResponseWriter, r *http.Request) {
	model := getenv("KIMI_MODEL", "kimi-k2.6")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"object": "list",
		"data": []map[string]interface{}{{
			"id":       model,
			"object":   "model",
			"created":  time.Now().Unix(),
			"owned_by": "kimi-web",
		}},
	})
}

func handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var input chatRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if len(input.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages is required")
		return
	}
	if input.Model == "" {
		input.Model = getenv("KIMI_MODEL", "kimi-k2.6")
	}
	if autoToolsEnabled() {
		input.Tools = mergeTools(input.Tools, localTools())
	}

	prompt := renderPrompt(input.Messages, input.Tools)
	if autoToolsEnabled() && !input.Stream {
		response, err := runAutoToolLoop(input, prompt)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, response)
		return
	}

	resp, err := callKimi(prompt, input.User, len(input.Tools) == 0)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Close()

	id := "chatcmpl-" + randomID()
	if input.Stream && len(input.Tools) == 0 {
		streamOpenAI(w, resp, id, input.Model, prompt)
		return
	}

	content, err := collectKimiText(resp)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	finish := "stop"
	cleanContent, toolCalls := parseToolCalls(content)
	if len(toolCalls) > 0 {
		finish = "tool_calls"
		content = cleanContent
	}
	if input.Stream {
		streamCollectedOpenAI(w, id, input.Model, prompt, content, toolCalls, finish)
		return
	}
	writeJSON(w, http.StatusOK, openAIResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   input.Model,
		Choices: []openAIChoice{{
			Index: 0,
			Message: openAIMessage{
				Role:      "assistant",
				Content:   content,
				ToolCalls: toolCalls,
			},
			FinishReason: &finish,
		}},
		Usage: estimateUsage(prompt, content),
	})
}

func callKimi(prompt, chatID string, enableKimiSearch bool) (io.ReadCloser, error) {
	baseURL := strings.TrimRight(getenv("KIMI_BASE_URL", "https://www.kimi.com"), "/")
	endpoint := getenv("KIMI_CHAT_ENDPOINT", "/apiv2/kimi.gateway.chat.v1.ChatService/Chat")
	url := baseURL + endpoint

	client := &http.Client{Timeout: time.Duration(getenvInt("KIMI_REQUEST_TIMEOUT_MS", 300000)) * time.Millisecond}
	variants := buildKimiPayloadVariants(prompt, chatID, enableKimiSearch)
	var lastErr string
	for _, variant := range variants {
		body, err := json.Marshal(variant.Payload)
		if err != nil {
			return nil, err
		}
		body = encodeConnectEnvelope(body)

		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		setKimiHeaders(req)

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			lastErr = fmt.Sprintf("%s: HTTP %d: %s", variant.Name, resp.StatusCode, strings.TrimSpace(string(b)))
			continue
		}

		buffered, err := bufferKimiResponse(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Sprintf("%s: %s", variant.Name, err.Error())
			continue
		}
		if connectErr := kimiConnectError(buffered); connectErr != "" {
			lastErr = fmt.Sprintf("%s: %s", variant.Name, connectErr)
			if strings.Contains(connectErr, "invalid_argument") {
				continue
			}
		}
		return io.NopCloser(bytes.NewReader(buffered)), nil
	}
	return nil, fmt.Errorf("all kimi payload variants failed; last error: %s", lastErr)
}

type kimiPayloadVariant struct {
	Name    string
	Payload map[string]interface{}
}

func buildKimiPayloadVariants(prompt, chatID string, enableKimiSearch bool) []kimiPayloadVariant {
	baseScenario := getenvInt("KIMI_SCENARIO", 9)
	scenarios := []int{baseScenario}
	if baseScenario != 5 {
		scenarios = append(scenarios, 5)
	}
	variants := []kimiPayloadVariant{
		{Name: "browser-exact", Payload: buildBrowserExactPayload(prompt, chatID, enableKimiSearch)},
	}
	for _, scenario := range scenarios {
		variants = append(variants,
			kimiPayloadVariant{Name: fmt.Sprintf("minimal-snake-text-num-s%d", scenario), Payload: buildMinimalKimiPayload(prompt, chatID, scenario, true, false, false)},
			kimiPayloadVariant{Name: fmt.Sprintf("minimal-camel-text-num-s%d", scenario), Payload: buildMinimalKimiPayload(prompt, chatID, scenario, false, false, false)},
			kimiPayloadVariant{Name: fmt.Sprintf("minimal-snake-content-num-s%d", scenario), Payload: buildMinimalKimiPayload(prompt, chatID, scenario, true, true, false)},
			kimiPayloadVariant{Name: fmt.Sprintf("minimal-camel-content-num-s%d", scenario), Payload: buildMinimalKimiPayload(prompt, chatID, scenario, false, true, false)},
			kimiPayloadVariant{Name: fmt.Sprintf("minimal-snake-text-role-s%d", scenario), Payload: buildMinimalKimiPayload(prompt, chatID, scenario, true, false, true)},
			kimiPayloadVariant{Name: fmt.Sprintf("minimal-camel-text-role-s%d", scenario), Payload: buildMinimalKimiPayload(prompt, chatID, scenario, false, false, true)},
			kimiPayloadVariant{Name: fmt.Sprintf("snake-text-s%d", scenario), Payload: buildKimiPayload(prompt, chatID, scenario, true, false, false)},
			kimiPayloadVariant{Name: fmt.Sprintf("camel-text-s%d", scenario), Payload: buildKimiPayload(prompt, chatID, scenario, false, false, false)},
			kimiPayloadVariant{Name: fmt.Sprintf("camel-content-s%d", scenario), Payload: buildKimiPayload(prompt, chatID, scenario, false, true, false)},
			kimiPayloadVariant{Name: fmt.Sprintf("snake-content-s%d", scenario), Payload: buildKimiPayload(prompt, chatID, scenario, true, true, false)},
		)
	}
	if strings.EqualFold(getenv("KIMI_TYPE_NAME", "false"), "true") {
		variants = append(variants, kimiPayloadVariant{Name: "typename", Payload: buildKimiPayload(prompt, chatID, baseScenario, false, true, true)})
	}
	return variants
}

func buildBrowserExactPayload(prompt, chatID string, enableKimiSearch bool) map[string]interface{} {
	useExistingChat := strings.EqualFold(os.Getenv("KIMI_USE_EXISTING_CHAT"), "true")
	if chatID == "" && useExistingChat {
		chatID = os.Getenv("KIMI_CHAT_ID")
	}
	tools := []interface{}{}
	if enableKimiSearch {
		tools = append(tools, map[string]interface{}{
			"type":   getenv("KIMI_TOOL_TYPE", "TOOL_TYPE_SEARCH"),
			"search": map[string]interface{}{},
		})
	}
	payload := map[string]interface{}{
		"scenario": getenv("KIMI_SCENARIO_NAME", "SCENARIO_K2D5"),
		"tools":    tools,
		"message": map[string]interface{}{
			"parent_id": os.Getenv("KIMI_PARENT_ID"),
			"role":      "user",
			"blocks": []interface{}{
				map[string]interface{}{
					"message_id": "",
					"text": map[string]interface{}{
						"content": prompt,
					},
				},
			},
			"scenario": getenv("KIMI_SCENARIO_NAME", "SCENARIO_K2D5"),
		},
		"options": map[string]interface{}{
			"thinking": false,
		},
	}
	if chatID != "" {
		payload["chat_id"] = chatID
	}
	if !useExistingChat || os.Getenv("KIMI_PARENT_ID") == "" {
		message := payload["message"].(map[string]interface{})
		delete(message, "parent_id")
	}
	return payload
}

func buildKimiPayload(prompt, chatID string, scenario int, useProtoFieldName bool, useContentCase bool, withTypeName bool) map[string]interface{} {
	if chatID == "" {
		chatID = defaultKimiChatID()
	}
	role := interface{}(2)
	if strings.EqualFold(getenv("KIMI_ROLE_STRING", "false"), "true") {
		role = "user"
	}
	textBlock := map[string]interface{}{"content": prompt}
	block := map[string]interface{}{
		"id":        "",
		"messageId": "",
	}
	if useContentCase {
		block["content"] = map[string]interface{}{"case": "text", "value": textBlock}
	} else {
		block["text"] = textBlock
	}
	chatMessage := map[string]interface{}{
		"id":                 "",
		"parentId":           "",
		"childrenMessageIds": []interface{}{},
		"role":               role,
		"blocks":             []interface{}{block},
		"scenario":           scenario,
		"labels":             []interface{}{},
		"references":         []interface{}{},
	}
	options := map[string]interface{}{"thinking": false}
	payload := map[string]interface{}{
		"chatId":     chatID,
		"kimiplusId": "",
		"scenario":   scenario,
		"tools":      []interface{}{},
		"message":    chatMessage,
		"options":    options,
	}
	if useProtoFieldName {
		block["message_id"] = block["messageId"]
		delete(block, "messageId")
		chatMessage["parent_id"] = chatMessage["parentId"]
		chatMessage["children_message_ids"] = chatMessage["childrenMessageIds"]
		delete(chatMessage, "parentId")
		delete(chatMessage, "childrenMessageIds")
		payload["chat_id"] = payload["chatId"]
		payload["kimiplus_id"] = payload["kimiplusId"]
		delete(payload, "chatId")
		delete(payload, "kimiplusId")
	}
	if withTypeName {
		textBlock["$typeName"] = "kimi.chat.v1.TextBlock"
		block["$typeName"] = "kimi.chat.v1.Block"
		chatMessage["$typeName"] = "kimi.chat.v1.ChatMessage"
		options["$typeName"] = "kimi.gateway.chat.v1.ChatRequestOptions"
		payload["$typeName"] = "kimi.gateway.chat.v1.ChatRequest"
	}
	return payload
}

func buildMinimalKimiPayload(prompt, chatID string, scenario int, useProtoFieldName bool, useContentCase bool, roleString bool) map[string]interface{} {
	if chatID == "" {
		chatID = defaultKimiChatID()
	}
	role := interface{}(2)
	if roleString {
		role = "user"
	}
	block := map[string]interface{}{}
	if useContentCase {
		block["content"] = map[string]interface{}{"case": "text", "value": map[string]interface{}{"content": prompt}}
	} else {
		block["text"] = map[string]interface{}{"content": prompt}
	}
	message := map[string]interface{}{
		"role":     role,
		"blocks":   []interface{}{block},
		"scenario": scenario,
	}
	payload := map[string]interface{}{
		"chatId":   chatID,
		"scenario": scenario,
		"message":  message,
		"options":  map[string]interface{}{"thinking": false},
	}
	if useProtoFieldName {
		payload["chat_id"] = payload["chatId"]
		delete(payload, "chatId")
	}
	return payload
}

func bufferKimiResponse(r io.Reader) ([]byte, error) {
	return io.ReadAll(r)
}

func kimiConnectError(data []byte) string {
	if len(data) == 0 {
		return "empty response"
	}
	if data[0] == '{' {
		if strings.Contains(string(data), "\"error\"") {
			return string(data)
		}
		return ""
	}
	br := bytes.NewReader(data)
	for br.Len() > 0 {
		flag, err := br.ReadByte()
		if err != nil {
			return err.Error()
		}
		lenBytes := make([]byte, 4)
		if _, err := io.ReadFull(br, lenBytes); err != nil {
			return err.Error()
		}
		length := binary.BigEndian.Uint32(lenBytes)
		if length == 0 {
			continue
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(br, payload); err != nil {
			return err.Error()
		}
		if flag&0x02 != 0 && strings.Contains(string(payload), "error") {
			return string(payload)
		}
	}
	return ""
}

func setKimiHeaders(req *http.Request) {
	auth := os.Getenv("KIMI_AUTH")
	cookie := os.Getenv("KIMI_COOKIE")
	stateCookie, stateAuth := loadKimiStorageState()
	if auth == "" {
		auth = stateAuth
	}
	if cookie == "" {
		cookie = stateCookie
	}
	if cookie == "" && auth != "" {
		cookie = "kimi-auth=" + auth
	}

	req.Header.Set("Content-Type", "application/connect+json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("Origin", "https://www.kimi.com")
	req.Header.Set("Referer", getenv("KIMI_REFERER", "https://www.kimi.com/"))
	req.Header.Set("User-Agent", getenv("KIMI_USER_AGENT", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36"))
	req.Header.Set("x-msh-platform", "web")
	req.Header.Set("x-msh-version", "1.0.0")
	req.Header.Set("R-Timezone", getenv("KIMI_TIMEZONE", "America/Sao_Paulo"))
	req.Header.Set("X-Language", getenv("KIMI_LANGUAGE", "en-US"))
	jwtInfo := parseKimiJWT(auth)
	if v := os.Getenv("KIMI_DEVICE_ID"); v != "" {
		req.Header.Set("x-msh-device-id", v)
	} else if jwtInfo.DeviceID != "" {
		req.Header.Set("x-msh-device-id", jwtInfo.DeviceID)
	}
	if v := os.Getenv("KIMI_SESSION_ID"); v != "" {
		req.Header.Set("x-msh-session-id", v)
	}
	if v := os.Getenv("KIMI_TRAFFIC_ID"); v != "" {
		req.Header.Set("x-traffic-id", v)
	} else if jwtInfo.Sub != "" {
		req.Header.Set("x-traffic-id", jwtInfo.Sub)
	} else if jwtInfo.DeviceID != "" {
		req.Header.Set("x-traffic-id", jwtInfo.DeviceID)
	}
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
}

type kimiJWTInfo struct {
	Sub      string `json:"sub"`
	DeviceID string `json:"device_id"`
}

func parseKimiJWT(token string) kimiJWTInfo {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return kimiJWTInfo{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return kimiJWTInfo{}
	}
	var info kimiJWTInfo
	_ = json.Unmarshal(payload, &info)
	return info
}

type playwrightStorageState struct {
	Cookies []struct {
		Name   string `json:"name"`
		Value  string `json:"value"`
		Domain string `json:"domain"`
	} `json:"cookies"`
}

func loadKimiStorageState() (string, string) {
	path := os.Getenv("KIMI_STORAGE_STATE")
	if path == "" {
		path = filepath.Join("storage", "kimi-state.json")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()

	var state playwrightStorageState
	if err := json.NewDecoder(f).Decode(&state); err != nil {
		return "", ""
	}

	var parts []string
	var auth string
	for _, c := range state.Cookies {
		domain := strings.TrimPrefix(strings.ToLower(c.Domain), ".")
		if domain != "kimi.com" && domain != "www.kimi.com" {
			continue
		}
		if c.Name == "" {
			continue
		}
		parts = append(parts, c.Name+"="+c.Value)
		if c.Name == "kimi-auth" {
			auth = c.Value
		}
	}
	return strings.Join(parts, "; "), auth
}

func encodeConnectEnvelope(payload []byte) []byte {
	buf := bytes.NewBuffer(make([]byte, 0, len(payload)+5))
	buf.WriteByte(0)
	_ = binary.Write(buf, binary.BigEndian, uint32(len(payload)))
	buf.Write(payload)
	return buf.Bytes()
}

func collectKimiText(r io.Reader) (string, error) {
	var out strings.Builder
	err := readKimiEvents(r, func(text string) {
		out.WriteString(text)
	})
	return out.String(), err
}

func streamOpenAI(w http.ResponseWriter, r io.Reader, id, model, prompt string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	created := time.Now().Unix()
	writeSSE(w, map[string]interface{}{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []openAIChoice{{Index: 0, Delta: openAIMessage{Role: "assistant"}}},
	})

	var full strings.Builder
	err := readKimiEvents(r, func(text string) {
		full.WriteString(text)
		writeSSE(w, map[string]interface{}{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []openAIChoice{{Index: 0, Delta: openAIMessage{Content: text}}},
		})
	})
	finish := "stop"
	if err != nil {
		finish = "error"
		writeSSE(w, map[string]interface{}{"error": err.Error()})
	}
	writeSSE(w, map[string]interface{}{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []openAIChoice{{Index: 0, FinishReason: &finish}},
		"usage":   estimateUsage(prompt, full.String()),
	})
	fmt.Fprint(w, "data: [DONE]\n\n")
	flush(w)
}

func streamCollectedOpenAI(w http.ResponseWriter, id, model, prompt, content string, toolCalls []toolCall, finish string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	created := time.Now().Unix()
	writeSSE(w, map[string]interface{}{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []openAIChoice{{Index: 0, Delta: openAIMessage{Role: "assistant"}}},
	})
	if content != "" {
		writeSSE(w, map[string]interface{}{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []openAIChoice{{Index: 0, Delta: openAIMessage{Content: content}}},
		})
	}
	if len(toolCalls) > 0 {
		writeSSE(w, map[string]interface{}{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []openAIChoice{{Index: 0, Delta: openAIMessage{ToolCalls: toolCalls}}},
		})
	}
	writeSSE(w, map[string]interface{}{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []openAIChoice{{Index: 0, FinishReason: &finish}},
		"usage":   estimateUsage(prompt, content),
	})
	fmt.Fprint(w, "data: [DONE]\n\n")
	flush(w)
}

func readKimiEvents(r io.Reader, onText func(string)) error {
	br := bufio.NewReader(r)
	peek, _ := br.Peek(1)
	if len(peek) == 0 {
		return nil
	}
	if peek[0] == '{' || peek[0] == '[' {
		b, err := io.ReadAll(br)
		if err != nil {
			return err
		}
		text := extractTextFromJSONBytes(b)
		if text != "" {
			onText(text)
		}
		return nil
	}

	for {
		flag, err := br.ReadByte()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		lenBytes := make([]byte, 4)
		if _, err := io.ReadFull(br, lenBytes); err != nil {
			return err
		}
		length := binary.BigEndian.Uint32(lenBytes)
		if length == 0 {
			continue
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(br, payload); err != nil {
			return err
		}
		if flag&0x02 != 0 {
			if strings.Contains(string(payload), "error") {
				return fmt.Errorf("kimi trailer: %s", strings.TrimSpace(string(payload)))
			}
			continue
		}
		text := extractTextFromJSONBytes(payload)
		if text != "" {
			onText(text)
		}
	}
}

func extractTextFromJSONBytes(b []byte) string {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return ""
	}
	if m, ok := v.(map[string]interface{}); ok {
		mask, _ := m["mask"].(string)
		if !strings.HasPrefix(mask, "block.text") {
			return ""
		}
	}
	return extractText(v)
}

func extractText(v interface{}) string {
	switch x := v.(type) {
	case map[string]interface{}:
		if event, ok := x["event"].(map[string]interface{}); ok {
			return extractText(event)
		}
		if c, ok := x["content"].(string); ok {
			return c
		}
		if value, ok := x["value"]; ok {
			if text := extractText(value); text != "" {
				return text
			}
		}
		for _, key := range []string{"block", "message", "delta", "text"} {
			if val, ok := x[key]; ok {
				if text := extractText(val); text != "" {
					return text
				}
			}
		}
	case []interface{}:
		var parts []string
		for _, item := range x {
			if text := extractText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	case string:
		return x
	}
	return ""
}

func renderPrompt(messages []message, tools []tool) string {
	var system []string
	var turns []string
	toolInstructions := formatToolsAsInstructions(tools)
	if toolInstructions != "" {
		system = append(system, toolInstructions)
	}
	if requestMentionsFileCreation(messages) {
		system = append(system, "The user's request is a file creation/editing task. Your first action must be write_file or apply_patch. Do not draft the file in chat. Do not provide a download link. Save the file on the local PC.")
	}
	for _, m := range messages {
		text := contentToText(m.Content)
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
		case "tool":
			turns = append(turns, fmt.Sprintf("Tool result%s: %s", formatToolCallID(m.ToolCallID), text))
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

func formatToolCallID(id string) string {
	if id == "" {
		return ""
	}
	return " (" + id + ")"
}

func formatToolsAsInstructions(tools []tool) string {
	if len(tools) == 0 {
		return ""
	}
	var b strings.Builder
	if strings.EqualFold(getenv("AUTO_TOOLS_AGENT_MODE", "pc"), "pc") {
		b.WriteString("You are running as an autonomous coding agent on a real Windows PC, not as a web chat bot. You can inspect and modify files in the local workspace by calling tools.\n")
	}
	b.WriteString("Current local workspace root: " + workspaceRoot() + "\n")
	b.WriteString("When the user asks you to create, save, download, write, edit, generate, or update a file, you MUST call write_file or apply_patch. Do not claim a file was created unless a tool result confirms it. Do not answer with fake links such as 'Baixe aqui'.\n")
	b.WriteString("When the user asks you to run, test, install, build, list, search, or inspect the computer/project, you MUST use the matching local tool instead of describing what you would do.\n")
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

func parseToolCalls(text string) (string, []toolCall) {
	for _, jsonText := range toolJSONCandidates(text) {
		calls := parseToolJSON(jsonText)
		if len(calls) == 0 {
			continue
		}
		clean := strings.TrimSpace(strings.Replace(text, jsonText, "", 1))
		return clean, calls
	}
	return text, nil
}

func toolJSONCandidates(text string) []string {
	trimmed := strings.TrimSpace(text)
	var candidates []string
	if trimmed != "" {
		candidates = append(candidates, stripMarkdownFence(trimmed))
	}
	for _, fenced := range extractFencedBlocks(text) {
		candidates = append(candidates, stripMarkdownFence(fenced))
	}
	candidates = append(candidates, extractJSONValues(text)...)
	seen := map[string]bool{}
	var unique []string
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		unique = append(unique, candidate)
	}
	return unique
}

func stripMarkdownFence(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "```") {
		return text
	}
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```JSON")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}

func extractFencedBlocks(text string) []string {
	re := regexp.MustCompile("(?s)```(?:json|JSON)?\\s*(.*?)\\s*```")
	matches := re.FindAllStringSubmatch(text, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			out = append(out, match[1])
		}
	}
	return out
}

func extractJSONValues(text string) []string {
	var out []string
	for i, r := range text {
		if r != '{' && r != '[' {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(text[i:]))
		var v interface{}
		if err := dec.Decode(&v); err != nil {
			continue
		}
		out = append(out, strings.TrimSpace(text[i:i+int(dec.InputOffset())]))
	}
	return out
}

func parseToolJSON(jsonText string) []toolCall {
	if !(strings.HasPrefix(jsonText, "{") || strings.HasPrefix(jsonText, "[")) {
		return nil
	}
	var raw interface{}
	if err := json.Unmarshal([]byte(jsonText), &raw); err != nil {
		return nil
	}
	return parseToolValue(raw)
}

func parseToolValue(raw interface{}) []toolCall {
	switch v := raw.(type) {
	case []interface{}:
		var calls []toolCall
		for _, item := range v {
			calls = append(calls, parseToolValue(item)...)
		}
		for i := range calls {
			calls[i].Index = i
		}
		return calls
	case map[string]interface{}:
		if nested, ok := v["tool_calls"]; ok {
			return parseToolValue(nested)
		}
		name, args := parseToolObject(v)
		if name == "" {
			return nil
		}
		return []toolCall{{Index: 0, ID: "call_" + randomID(), Type: "function", Function: toolFunction{Name: name, Arguments: args}}}
	default:
		return nil
	}
}

func parseToolObject(v map[string]interface{}) (string, string) {
	name, _ := v["name"].(string)
	if name == "" {
		name, _ = v["tool"].(string)
	}
	argsValue := v["arguments"]
	if fn, ok := v["function"].(map[string]interface{}); ok {
		if name == "" {
			name, _ = fn["name"].(string)
		}
		if argsValue == nil {
			argsValue = fn["arguments"]
		}
	}
	if name == "" {
		return "", ""
	}
	args := "{}"
	if argsValue != nil {
		switch a := argsValue.(type) {
		case string:
			args = a
		default:
			b, _ := json.Marshal(a)
			args = string(b)
		}
	}
	return name, args
}

func autoToolsEnabled() bool {
	return strings.EqualFold(os.Getenv("AUTO_TOOLS"), "true")
}

func mergeTools(existing, extra []tool) []tool {
	seen := map[string]bool{}
	var merged []tool
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

func localTools() []tool {
	stringParam := func(desc string) map[string]interface{} {
		return map[string]interface{}{"type": "string", "description": desc}
	}
	return []tool{
		{Type: "function", Function: functionTool{Name: "read_file", Description: "Read a text file from the workspace", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"path": stringParam("Relative file path")}, "required": []string{"path"}}}},
		{Type: "function", Function: functionTool{Name: "write_file", Description: "Write a text file inside the workspace", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"path": stringParam("Relative file path"), "content": stringParam("Full file content")}, "required": []string{"path", "content"}}}},
		{Type: "function", Function: functionTool{Name: "list_files", Description: "List files by glob pattern inside the workspace", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"pattern": stringParam("Glob pattern, for example **/*.go")}, "required": []string{"pattern"}}}},
		{Type: "function", Function: functionTool{Name: "grep", Description: "Search file contents by regular expression", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"pattern": stringParam("Regular expression"), "path": stringParam("Relative directory or file path")}, "required": []string{"pattern"}}}},
		{Type: "function", Function: functionTool{Name: "apply_patch", Description: "Replace exact text inside a file. Arguments: path, old, new", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"path": stringParam("Relative file path"), "old": stringParam("Exact text to replace"), "new": stringParam("Replacement text")}, "required": []string{"path", "old", "new"}}}},
		{Type: "function", Function: functionTool{Name: "run_command", Description: "Run a non-interactive cmd.exe command in the workspace. Use only for build/test/install/status commands.", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"command": stringParam("Command to run"), "timeout_ms": map[string]interface{}{"type": "integer", "description": "Timeout in milliseconds, max 120000"}}, "required": []string{"command"}}}},
	}
}

func runAutoToolLoop(input chatRequest, firstPrompt string) (openAIResponse, error) {
	id := "chatcmpl-" + randomID()
	messages := append([]message(nil), input.Messages...)
	prompt := firstPrompt
	maxSteps := getenvInt("AUTO_TOOLS_MAX_STEPS", 6)
	var lastContent string
	for step := 0; step < maxSteps; step++ {
		resp, err := callKimi(prompt, input.User, false)
		if err != nil {
			return openAIResponse{}, err
		}
		content, err := collectKimiText(resp)
		resp.Close()
		if err != nil {
			return openAIResponse{}, err
		}
		lastContent = content
		clean, calls := parseToolCalls(content)
		if len(calls) == 0 {
			if shouldRetryWithFileTool(messages, content, step, maxSteps) {
				messages = append(messages, message{Role: "assistant", Content: content})
				messages = append(messages, message{Role: "user", Content: "You claimed or implied a file was available, but no local file tool was called. This is a real PC agent session. Call write_file now with the requested file path and full file contents. Respond only with the tool JSON."})
				prompt = renderPrompt(messages, input.Tools)
				continue
			}
			finish := "stop"
			return openAIResponse{ID: id, Object: "chat.completion", Created: time.Now().Unix(), Model: input.Model, Choices: []openAIChoice{{Index: 0, Message: openAIMessage{Role: "assistant", Content: content}, FinishReason: &finish}}, Usage: estimateUsage(prompt, content)}, nil
		}
		messages = append(messages, message{Role: "assistant", Content: clean, ToolCalls: calls})
		var results []localToolResult
		for _, call := range calls {
			result := executeLocalTool(call)
			results = append(results, result)
			messages = append(messages, message{Role: "tool", ToolCallID: call.ID, Content: result.Content})
		}
		if response, ok := fastToolResponse(input, id, prompt, results); ok {
			return response, nil
		}
		prompt = renderPrompt(messages, input.Tools) + "\n\nContinue after the tool result. If finished, answer normally without tool JSON."
	}
	finish := "stop"
	return openAIResponse{ID: id, Object: "chat.completion", Created: time.Now().Unix(), Model: input.Model, Choices: []openAIChoice{{Index: 0, Message: openAIMessage{Role: "assistant", Content: lastContent}, FinishReason: &finish}}, Usage: estimateUsage(prompt, lastContent)}, nil
}

func fastToolResponse(input chatRequest, id, prompt string, results []localToolResult) (openAIResponse, bool) {
	if !strings.EqualFold(getenv("AUTO_TOOLS_FAST_RETURN", "true"), "true") {
		return openAIResponse{}, false
	}
	if len(results) == 0 {
		return openAIResponse{}, false
	}
	var lines []string
	for _, result := range results {
		if !result.OK {
			return openAIResponse{}, false
		}
		switch result.Name {
		case "write_file", "apply_patch", "run_command":
			lines = append(lines, result.Summary)
		default:
			return openAIResponse{}, false
		}
	}
	content := strings.Join(lines, "\n")
	finish := "stop"
	return openAIResponse{ID: id, Object: "chat.completion", Created: time.Now().Unix(), Model: input.Model, Choices: []openAIChoice{{Index: 0, Message: openAIMessage{Role: "assistant", Content: content}, FinishReason: &finish}}, Usage: estimateUsage(prompt, content)}, true
}

func requestMentionsFileCreation(messages []message) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return mentionsFileCreation(strings.ToLower(contentToText(messages[i].Content)))
		}
	}
	return false
}

func shouldRetryWithFileTool(messages []message, content string, step, maxSteps int) bool {
	if step >= maxSteps-1 || !hasLocalToolName("write_file") {
		return false
	}
	lastUser := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lastUser = strings.ToLower(contentToText(messages[i].Content))
			break
		}
	}
	if !mentionsFileCreation(lastUser) {
		return false
	}
	lower := strings.ToLower(content)
	return strings.Contains(lower, "baixe aqui") || strings.Contains(lower, "download") || strings.Contains(lower, ".html") || strings.Contains(lower, "arquivo") || strings.Contains(lower, "salvo") || strings.Contains(lower, "criei")
}

func hasLocalToolName(name string) bool {
	for _, t := range localTools() {
		if t.Function.Name == name {
			return true
		}
	}
	return false
}

func mentionsFileCreation(text string) bool {
	keywords := []string{"crie", "criar", "cria", "gere", "gerar", "salve", "salvar", "write", "create", "save", "download", "arquivo", "file", ".html", ".js", ".css", ".md", ".txt", ".json"}
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func executeLocalTool(call toolCall) localToolResult {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return failTool(call.Function.Name, "invalid JSON arguments: "+err.Error())
	}
	log.Printf("tool=%s status=started", call.Function.Name)
	switch call.Function.Name {
	case "read_file":
		path, err := safeWorkspacePath(argString(args, "path"), false)
		if err != nil {
			return failTool(call.Function.Name, err.Error())
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return failTool(call.Function.Name, err.Error())
		}
		return okTool(call.Function.Name, string(b), "read "+argString(args, "path"), path)
	case "write_file":
		path, err := safeWorkspacePath(argString(args, "path"), true)
		if err != nil {
			return failTool(call.Function.Name, err.Error())
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return failTool(call.Function.Name, err.Error())
		}
		if err := os.WriteFile(path, []byte(argString(args, "content")), 0644); err != nil {
			return failTool(call.Function.Name, err.Error())
		}
		if _, err := os.Stat(path); err != nil {
			return failTool(call.Function.Name, "write verification failed: "+err.Error())
		}
		return okTool(call.Function.Name, "wrote "+argString(args, "path"), "Arquivo criado: "+path, path)
	case "list_files":
		return okTool(call.Function.Name, listWorkspaceFiles(argString(args, "pattern")), "listed files", "")
	case "grep":
		return okTool(call.Function.Name, grepWorkspace(argString(args, "pattern"), argString(args, "path")), "searched files", "")
	case "apply_patch":
		result := replaceInFile(argString(args, "path"), argString(args, "old"), argString(args, "new"))
		if strings.HasPrefix(result, "tool error:") {
			return failTool(call.Function.Name, strings.TrimPrefix(result, "tool error: "))
		}
		path, _ := safeWorkspacePath(argString(args, "path"), false)
		return okTool(call.Function.Name, result, "Arquivo atualizado: "+path, path)
	case "run_command":
		result := runWorkspaceCommand(argString(args, "command"), argInt(args, "timeout_ms", 30000))
		if strings.HasPrefix(result, "tool error:") || strings.HasPrefix(result, "command failed:") {
			return failTool(call.Function.Name, result)
		}
		summary := "Comando executado: " + argString(args, "command")
		if strings.TrimSpace(result) != "" {
			summary += "\n" + strings.TrimSpace(result)
		}
		return okTool(call.Function.Name, result, summary, "")
	default:
		return failTool(call.Function.Name, "unknown local tool "+call.Function.Name)
	}
}

func okTool(name, content, summary, path string) localToolResult {
	log.Printf("tool=%s status=ok path=%q", name, path)
	return localToolResult{Name: name, Content: content, OK: true, Path: path, Summary: summary}
}

func failTool(name, err string) localToolResult {
	content := "tool error: " + err
	log.Printf("tool=%s status=error error=%q", name, err)
	return localToolResult{Name: name, Content: content, OK: false, Summary: content}
}

func workspaceRoot() string {
	root := os.Getenv("AUTO_TOOLS_WORKSPACE")
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return root
	}
	return abs
}

func safeWorkspacePath(path string, writing bool) (string, error) {
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, "..") {
		return "", fmt.Errorf("unsafe path %q", path)
	}
	if writing && (filepath.Base(path) == ".env" || strings.Contains(strings.ToLower(path), "kimi-state")) {
		return "", fmt.Errorf("refusing to write sensitive file %q", path)
	}
	root := workspaceRoot()
	abs, err := filepath.Abs(filepath.Join(root, path))
	if err != nil {
		return "", err
	}
	if abs != root && !strings.HasPrefix(abs, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes workspace")
	}
	return abs, nil
}

func argString(args map[string]interface{}, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func argInt(args map[string]interface{}, key string, fallback int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return fallback
	}
}

func listWorkspaceFiles(pattern string) string {
	if pattern == "" {
		pattern = "*"
	}
	if filepath.IsAbs(pattern) || strings.Contains(pattern, "..") {
		return "tool error: unsafe pattern"
	}
	matches, err := filepath.Glob(filepath.Join(workspaceRoot(), filepath.FromSlash(pattern)))
	if err != nil {
		return "tool error: " + err.Error()
	}
	var out []string
	for _, match := range matches {
		rel, _ := filepath.Rel(workspaceRoot(), match)
		out = append(out, filepath.ToSlash(rel))
		if len(out) >= 200 {
			break
		}
	}
	return strings.Join(out, "\n")
}

func grepWorkspace(pattern, relPath string) string {
	if pattern == "" {
		return "tool error: pattern is required"
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "tool error: " + err.Error()
	}
	rootPath := relPath
	if rootPath == "" {
		rootPath = "."
	}
	path, err := safeWorkspacePath(rootPath, false)
	if err != nil {
		return "tool error: " + err.Error()
	}
	var lines []string
	_ = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || len(lines) >= 100 {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil || bytes.IndexByte(b, 0) >= 0 {
			return nil
		}
		rel, _ := filepath.Rel(workspaceRoot(), p)
		for i, line := range strings.Split(string(b), "\n") {
			if re.MatchString(line) {
				lines = append(lines, fmt.Sprintf("%s:%d:%s", filepath.ToSlash(rel), i+1, line))
				if len(lines) >= 100 {
					break
				}
			}
		}
		return nil
	})
	return strings.Join(lines, "\n")
}

func replaceInFile(relPath, oldText, newText string) string {
	path, err := safeWorkspacePath(relPath, true)
	if err != nil {
		return "tool error: " + err.Error()
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "tool error: " + err.Error()
	}
	content := string(b)
	if oldText == "" || !strings.Contains(content, oldText) {
		return "tool error: old text not found"
	}
	updated := strings.Replace(content, oldText, newText, 1)
	if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
		return "tool error: " + err.Error()
	}
	return "patched " + relPath
}

func runWorkspaceCommand(command string, timeoutMs int) string {
	if !strings.EqualFold(os.Getenv("AUTO_TOOLS_ALLOW_COMMANDS"), "true") {
		return "tool error: run_command disabled; set AUTO_TOOLS_ALLOW_COMMANDS=true"
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return "tool error: command is required"
	}
	if isDangerousCommand(command) {
		return "tool error: command blocked by safety policy"
	}
	if timeoutMs <= 0 || timeoutMs > 120000 {
		timeoutMs = 120000
	}
	cmd := exec.Command("cmd.exe", "/C", command)
	cmd.Dir = workspaceRoot()
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		return "tool error: " + err.Error()
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		result := out.String()
		if len(result) > 12000 {
			result = result[:12000] + "\n... output truncated ..."
		}
		if err != nil {
			return "command failed: " + err.Error() + "\n" + result
		}
		return result
	case <-time.After(time.Duration(timeoutMs) * time.Millisecond):
		_ = cmd.Process.Kill()
		return "tool error: command timed out"
	}
}

func isDangerousCommand(command string) bool {
	lower := strings.ToLower(command)
	blocked := []string{
		" del ", " erase ", " rmdir ", " rd /", " format ", " shutdown ", " reboot ", " reg delete",
		"git reset --hard", "git clean", "remove-item", "rm -rf", ":(){", "taskkill /f /im",
	}
	padded := " " + lower + " "
	for _, item := range blocked {
		if strings.Contains(padded, item) {
			return true
		}
	}
	return false
}

func contentToText(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, item := range v {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if m["type"] == "text" {
				if text, ok := m["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return fmt.Sprint(v)
	}
}

func estimateUsage(prompt, completion string) usage {
	p := len(prompt) / 4
	c := len(completion) / 4
	return usage{PromptTokens: p, CompletionTokens: c, TotalTokens: p + c}
}

func withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		apiKey := strings.TrimSpace(os.Getenv("API_KEY"))
		if apiKey != "" {
			expected := "Bearer " + apiKey
			if r.Header.Get("Authorization") != expected {
				writeError(w, http.StatusUnauthorized, "invalid or missing API key")
				return
			}
		}
		next(w, r)
	}
}

func withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", getenv("CORS_ORIGIN", "*"))
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    "proxy_error",
		},
	})
}

func writeSSE(w http.ResponseWriter, v interface{}) {
	b, _ := json.Marshal(v)
	fmt.Fprintf(w, "data: %s\n\n", b)
	flush(w)
}

func flush(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func randomChatID() string {
	id := randomID()
	return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
}

func defaultKimiChatID() string {
	if v := os.Getenv("KIMI_CHAT_ID"); v != "" {
		return v
	}
	ref := os.Getenv("KIMI_REFERER")
	marker := "/chat/"
	idx := strings.Index(ref, marker)
	if idx != -1 {
		id := ref[idx+len(marker):]
		if q := strings.Index(id, "?"); q != -1 {
			id = id[:q]
		}
		if id != "" {
			return id
		}
	}
	return randomChatID()
}

func loadDotEnv(path string) {
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
