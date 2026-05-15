package kimi

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kimi-ai-proxy/internal/utils"
)

func CallKimi(prompt, chatID string, enableKimiSearch bool) (io.ReadCloser, error) {
	baseURL := strings.TrimRight(utils.GetEnv("KIMI_BASE_URL", "https://www.kimi.com"), "/")
	endpoint := utils.GetEnv("KIMI_CHAT_ENDPOINT", "/apiv2/kimi.gateway.chat.v1.ChatService/Chat")
	url := baseURL + endpoint

	client := &http.Client{Timeout: time.Duration(utils.GetEnvInt("KIMI_REQUEST_TIMEOUT_MS", 300000)) * time.Millisecond}
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

func buildKimiPayloadVariants(prompt, chatID string, enableKimiSearch bool) []utils.KimiPayloadVariant {
	baseScenario := utils.GetEnvInt("KIMI_SCENARIO", 9)
	scenarios := []int{baseScenario}
	if baseScenario != 5 {
		scenarios = append(scenarios, 5)
	}
	variants := []utils.KimiPayloadVariant{
		{Name: "browser-exact", Payload: buildBrowserExactPayload(prompt, chatID, enableKimiSearch)},
	}
	isThinking := strings.EqualFold(utils.GetEnv("KIMI_THINKING", "false"), "true")
	for _, scenario := range scenarios {
		variants = append(variants,
			utils.KimiPayloadVariant{Name: fmt.Sprintf("minimal-snake-text-num-s%d", scenario), Payload: buildMinimalKimiPayload(prompt, chatID, scenario, true, false, false, isThinking)},
			utils.KimiPayloadVariant{Name: fmt.Sprintf("minimal-camel-text-num-s%d", scenario), Payload: buildMinimalKimiPayload(prompt, chatID, scenario, false, false, false, isThinking)},
			utils.KimiPayloadVariant{Name: fmt.Sprintf("minimal-snake-content-num-s%d", scenario), Payload: buildMinimalKimiPayload(prompt, chatID, scenario, true, true, false, isThinking)},
			utils.KimiPayloadVariant{Name: fmt.Sprintf("minimal-camel-content-num-s%d", scenario), Payload: buildMinimalKimiPayload(prompt, chatID, scenario, false, true, false, isThinking)},
			utils.KimiPayloadVariant{Name: fmt.Sprintf("minimal-snake-text-role-s%d", scenario), Payload: buildMinimalKimiPayload(prompt, chatID, scenario, true, false, true, isThinking)},
			utils.KimiPayloadVariant{Name: fmt.Sprintf("minimal-camel-text-role-s%d", scenario), Payload: buildMinimalKimiPayload(prompt, chatID, scenario, false, false, true, isThinking)},
			utils.KimiPayloadVariant{Name: fmt.Sprintf("snake-text-s%d", scenario), Payload: buildKimiPayload(prompt, chatID, scenario, true, false, false, isThinking)},
			utils.KimiPayloadVariant{Name: fmt.Sprintf("camel-text-s%d", scenario), Payload: buildKimiPayload(prompt, chatID, scenario, false, false, false, isThinking)},
			utils.KimiPayloadVariant{Name: fmt.Sprintf("camel-content-s%d", scenario), Payload: buildKimiPayload(prompt, chatID, scenario, false, true, false, isThinking)},
			utils.KimiPayloadVariant{Name: fmt.Sprintf("snake-content-s%d", scenario), Payload: buildKimiPayload(prompt, chatID, scenario, true, false, false, isThinking)},
		)
	}
	if strings.EqualFold(utils.GetEnv("KIMI_TYPE_NAME", "false"), "true") {
		variants = append(variants, utils.KimiPayloadVariant{Name: "typename", Payload: buildKimiPayload(prompt, chatID, baseScenario, false, true, true, isThinking)})
	}
	return variants
}

func buildBrowserExactPayload(prompt, chatID string, enableKimiSearch bool) map[string]interface{} {
	useExistingChat := strings.EqualFold(os.Getenv("KIMI_USE_EXISTING_CHAT"), "true")
	if chatID == "" && useExistingChat {
		chatID = os.Getenv("KIMI_CHAT_ID")
	}
	isThinking := strings.EqualFold(utils.GetEnv("KIMI_THINKING", "false"), "true")
	tools := []interface{}{}
	if enableKimiSearch {
		tools = append(tools, map[string]interface{}{
			"type":   utils.GetEnv("KIMI_TOOL_TYPE", "TOOL_TYPE_SEARCH"),
			"search": map[string]interface{}{},
		})
	}
	payload := map[string]interface{}{
		"scenario": utils.GetEnv("KIMI_SCENARIO_NAME", "SCENARIO_K2D5"),
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
			"scenario": utils.GetEnv("KIMI_SCENARIO_NAME", "SCENARIO_K2D5"),
		},
		"options": map[string]interface{}{
			"thinking": isThinking,
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

func buildKimiPayload(prompt, chatID string, scenario int, useProtoFieldName bool, useContentCase bool, withTypeName bool, thinking bool) map[string]interface{} {
	if chatID == "" {
		chatID = defaultKimiChatID()
	}
	role := interface{}(2)
	if strings.EqualFold(utils.GetEnv("KIMI_ROLE_STRING", "false"), "true") {
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
	options := map[string]interface{}{"thinking": thinking}
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

func buildMinimalKimiPayload(prompt, chatID string, scenario int, useProtoFieldName bool, useContentCase bool, roleString bool, thinking bool) map[string]interface{} {
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
		"options":  map[string]interface{}{"thinking": thinking},
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
	req.Header.Set("Referer", utils.GetEnv("KIMI_REFERER", "https://www.kimi.com/"))
	req.Header.Set("User-Agent", utils.GetEnv("KIMI_USER_AGENT", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36"))
	req.Header.Set("x-msh-platform", "web")
	req.Header.Set("x-msh-version", "1.0.0")
	req.Header.Set("R-Timezone", utils.GetEnv("KIMI_TIMEZONE", "America/Sao_Paulo"))
	req.Header.Set("X-Language", utils.GetEnv("KIMI_LANGUAGE", "en-US"))
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

func parseKimiJWT(token string) utils.KimiJWTInfo {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return utils.KimiJWTInfo{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return utils.KimiJWTInfo{}
	}
	var info utils.KimiJWTInfo
	_ = json.Unmarshal(payload, &info)
	return info
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

	var state utils.PlaywrightStorageState
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

func CollectKimiText(r io.Reader) (string, error) {
	var out strings.Builder
	err := ReadKimiEvents(r, func(text string) {
		out.WriteString(text)
	})
	return out.String(), err
}

func StreamOpenAI(w http.ResponseWriter, r io.Reader, id, model, prompt string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	created := time.Now().Unix()
	writeSSE(w, map[string]interface{}{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []utils.OpenAIChoice{{Index: 0, Delta: utils.OpenAIMessage{Role: "assistant"}}},
	})

	var full strings.Builder
	err := ReadKimiEvents(r, func(text string) {
		full.WriteString(text)
		writeSSE(w, map[string]interface{}{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []utils.OpenAIChoice{{Index: 0, Delta: utils.OpenAIMessage{Content: text}}},
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
		"choices": []utils.OpenAIChoice{{Index: 0, FinishReason: &finish}},
		"usage":   utils.EstimateUsage(prompt, full.String()),
	})
	fmt.Fprint(w, "data: [DONE]\n\n")
	flush(w)
}

func StreamCollectedOpenAI(w http.ResponseWriter, id, model, prompt, content string, toolCalls []utils.ToolCall, finish string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	created := time.Now().Unix()
	writeSSE(w, map[string]interface{}{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []utils.OpenAIChoice{{Index: 0, Delta: utils.OpenAIMessage{Role: "assistant"}}},
	})
	if content != "" {
		writeSSE(w, map[string]interface{}{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []utils.OpenAIChoice{{Index: 0, Delta: utils.OpenAIMessage{Content: content}}},
		})
	}
	if len(toolCalls) > 0 {
		writeSSE(w, map[string]interface{}{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []utils.OpenAIChoice{{Index: 0, Delta: utils.OpenAIMessage{ToolCalls: toolCalls}}},
		})
	}
	writeSSE(w, map[string]interface{}{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []utils.OpenAIChoice{{Index: 0, FinishReason: &finish}},
		"usage":   utils.EstimateUsage(prompt, content),
	})
	fmt.Fprint(w, "data: [DONE]\n\n")
	flush(w)
}

func ReadKimiEvents(r io.Reader, onText func(string)) error {
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

func randomChatID() string {
	id := utils.RandomID()
	return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
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
