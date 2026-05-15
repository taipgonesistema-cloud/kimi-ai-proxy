package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Message struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
}

type ChatRequest struct {
	Model      string      `json:"model"`
	Messages   []Message   `json:"messages"`
	Stream     bool        `json:"stream"`
	User       string      `json:"user"`
	Tools      []Tool      `json:"tools"`
	ToolChoice interface{} `json:"tool_choice"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function FunctionTool `json:"function"`
}

type FunctionTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters,omitempty"`
}

type ToolCall struct {
	Index    int          `json:"index,omitempty"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments"`
}

type LocalToolResult struct {
	Name    string
	Content string
	OK      bool
	Path    string
	Summary string
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type OpenAIResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   Usage          `json:"usage"`
}

type OpenAIChoice struct {
	Index        int           `json:"index"`
	Message      OpenAIMessage `json:"message,omitempty"`
	Delta        OpenAIMessage `json:"delta,omitempty"`
	FinishReason *string       `json:"finish_reason"`
}

type OpenAIMessage struct {
	Role      string     `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type KimiPayloadVariant struct {
	Name    string
	Payload map[string]interface{}
}

type KimiJWTInfo struct {
	Sub      string `json:"sub"`
	DeviceID string `json:"device_id"`
}

type PlaywrightStorageState struct {
	Cookies []struct {
		Name   string `json:"name"`
		Value  string `json:"value"`
		Domain string `json:"domain"`
	} `json:"cookies"`
}

func ContentToText(content interface{}) string {
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

func EstimateUsage(prompt, completion string) Usage {
	p := len(prompt) / 4
	c := len(completion) / 4
	return Usage{PromptTokens: p, CompletionTokens: c, TotalTokens: p + c}
}

func RandomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func GetEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func GetEnvInt(key string, fallback int) int {
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

func ShouldEnableKimiSearch(messages []Message, hasTools bool) bool {
	if strings.EqualFold(GetEnv("KIMI_ENABLE_SEARCH_WITH_TOOLS", "true"), "false") && hasTools {
		return false
	}
	if !hasTools {
		return true
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		text := strings.ToLower(ContentToText(messages[i].Content))
		keywords := []string{"web", "internet", "pesquisa", "pesquise", "buscar", "busca", "noticia", "noticias", "ranking", "benchmark", "preco", "preço", "atual", "hoje", "2026", "latest", "current", "news", "search"}
		for _, keyword := range keywords {
			if strings.Contains(text, keyword) {
				return true
			}
		}
		return false
	}
	return false
}

func WorkspaceRoot() string {
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
