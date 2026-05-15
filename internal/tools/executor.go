package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"kimi-ai-proxy/internal/kimi"
	"kimi-ai-proxy/internal/prompt"
	"kimi-ai-proxy/internal/utils"
)

func LocalTools() []utils.Tool {
	stringParam := func(desc string) map[string]interface{} {
		return map[string]interface{}{"type": "string", "description": desc}
	}
	return []utils.Tool{
		{Type: "function", Function: utils.FunctionTool{Name: "read_file", Description: "Read a text file from the workspace", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"path": stringParam("Relative file path")}, "required": []string{"path"}}}},
		{Type: "function", Function: utils.FunctionTool{Name: "write_file", Description: "Write a text file inside the workspace", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"path": stringParam("Relative file path"), "content": stringParam("Full file content")}, "required": []string{"path", "content"}}}},
		{Type: "function", Function: utils.FunctionTool{Name: "web_fetch", Description: "Fetch text content from a specific http or https URL provided by the user. Not for open-ended web search.", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"url": stringParam("Specific http or https URL to fetch")}, "required": []string{"url"}}}},
		{Type: "function", Function: utils.FunctionTool{Name: "list_files", Description: "List files by glob pattern inside the workspace", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"pattern": stringParam("Glob pattern, for example **/*.go")}, "required": []string{"pattern"}}}},
		{Type: "function", Function: utils.FunctionTool{Name: "grep", Description: "Search file contents by regular expression", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"pattern": stringParam("Regular expression"), "path": stringParam("Relative directory or file path")}, "required": []string{"pattern"}}}},
		{Type: "function", Function: utils.FunctionTool{Name: "apply_patch", Description: "Replace exact text inside a file. Arguments: path, old, new", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"path": stringParam("Relative file path"), "old": stringParam("Exact text to replace"), "new": stringParam("Replacement text")}, "required": []string{"path", "old", "new"}}}},
		{Type: "function", Function: utils.FunctionTool{Name: "run_command", Description: "Run a non-interactive cmd.exe command in the workspace. Use only for build/test/install/status commands.", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"command": stringParam("Command to run"), "timeout_ms": map[string]interface{}{"type": "integer", "description": "Timeout in milliseconds, max 120000"}}, "required": []string{"command"}}}},
	}
}

func ExecuteLocalTool(call utils.ToolCall) utils.LocalToolResult {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return failTool(call.Function.Name, "invalid JSON arguments: "+err.Error())
	}
	validated, err := validateToolArgs(call.Function.Name, args, LocalTools())
	if err != nil {
		return failTool(call.Function.Name, err.Error())
	}
	args = validated
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
	case "web_fetch":
		result := fetchURL(argString(args, "url"))
		if strings.HasPrefix(result, "tool error:") {
			return failTool(call.Function.Name, strings.TrimPrefix(result, "tool error: "))
		}
		return okTool(call.Function.Name, result, "URL fetched: "+argString(args, "url"), "")
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

func okTool(name, content, summary, path string) utils.LocalToolResult {
	log.Printf("tool=%s status=ok path=%q", name, path)
	return utils.LocalToolResult{Name: name, Content: content, OK: true, Path: path, Summary: summary}
}

func failTool(name, err string) utils.LocalToolResult {
	content := "tool error: " + err
	log.Printf("tool=%s status=error error=%q", name, err)
	return utils.LocalToolResult{Name: name, Content: content, OK: false, Summary: content}
}

func safeWorkspacePath(path string, writing bool) (string, error) {
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, "..") {
		return "", fmt.Errorf("unsafe path %q", path)
	}
	if writing && (filepath.Base(path) == ".env" || strings.Contains(strings.ToLower(path), "kimi-state")) {
		return "", fmt.Errorf("refusing to write sensitive file %q", path)
	}
	root := utils.WorkspaceRoot()
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

func validateToolArgs(name string, args map[string]interface{}, tools []utils.Tool) (map[string]interface{}, error) {
	var schema map[string]interface{}
	for _, t := range tools {
		if t.Function.Name == name {
			schema, _ = t.Function.Parameters.(map[string]interface{})
			break
		}
	}
	if schema == nil {
		return args, nil
	}

	required, _ := schema["required"].([]interface{})
	reqStrs := make([]string, len(required))
	for i, r := range required {
		reqStrs[i], _ = r.(string)
	}
	for _, req := range reqStrs {
		if _, ok := args[req]; !ok {
			return nil, fmt.Errorf("tool %s: missing required argument %q", name, req)
		}
	}

	props, _ := schema["properties"].(map[string]interface{})
	addProps, _ := schema["additionalProperties"].(bool)
	for key, val := range args {
		propSchema, hasProp := props[key]
		if !hasProp {
			if addProps {
				continue
			}
			return nil, fmt.Errorf("tool %s: unexpected argument %q", name, key)
		}
		ps, _ := propSchema.(map[string]interface{})
		if ps == nil {
			continue
		}
		expectedType, _ := ps["type"].(string)
		if expectedType == "" {
			continue
		}
		switch expectedType {
		case "string":
			if _, ok := val.(string); !ok {
				return nil, fmt.Errorf("tool %s: argument %q should be string, got %T", name, key, val)
			}
		case "integer":
			switch v := val.(type) {
			case float64:
				if v != float64(int64(v)) {
					return nil, fmt.Errorf("tool %s: argument %q should be integer, got float", name, key)
				}
				args[key] = int(v)
			default:
				return nil, fmt.Errorf("tool %s: argument %q should be integer, got %T", name, key, val)
			}
		}
	}
	return args, nil
}

func RunAutoToolLoop(input utils.ChatRequest, firstPrompt string) (utils.OpenAIResponse, error) {
	id := "chatcmpl-" + utils.RandomID()
	messages := append([]utils.Message(nil), input.Messages...)
	currentPrompt := firstPrompt
	maxSteps := utils.GetEnvInt("AUTO_TOOLS_MAX_STEPS", 6)
	var lastContent string
	for step := 0; step < maxSteps; step++ {
		resp, err := kimi.CallKimi(currentPrompt, input.User, utils.ShouldEnableKimiSearch(messages, true))
		if err != nil {
			return utils.OpenAIResponse{}, err
		}
		content, err := kimi.CollectKimiText(resp)
		resp.Close()
		if err != nil {
			return utils.OpenAIResponse{}, err
		}
		lastContent = content
		clean, calls := utils.ParseToolCalls(content)
		if len(calls) == 0 {
			if ShouldRetryWithFileTool(messages, content, step, maxSteps) {
				messages = append(messages, utils.Message{Role: "assistant", Content: content})
				messages = append(messages, utils.Message{Role: "user", Content: "You claimed or implied a file was available, but no local file tool was called. This is a real PC agent session. Call write_file now with the requested file path and full file contents. Respond only with the tool JSON."})
				currentPrompt = prompt.RenderPrompt(messages, input.Tools)
				continue
			}
			finish := "stop"
			return utils.OpenAIResponse{ID: id, Object: "chat.completion", Created: time.Now().Unix(), Model: input.Model, Choices: []utils.OpenAIChoice{{Index: 0, Message: utils.OpenAIMessage{Role: "assistant", Content: content}, FinishReason: &finish}}, Usage: utils.EstimateUsage(currentPrompt, content)}, nil
		}
		messages = append(messages, utils.Message{Role: "assistant", Content: clean, ToolCalls: calls})
		var results []utils.LocalToolResult
		for _, call := range calls {
			result := ExecuteLocalTool(call)
			results = append(results, result)
			messages = append(messages, utils.Message{Role: "tool", ToolCallID: call.ID, Content: result.Content})
		}
		if response, ok := FastToolResponse(input, id, currentPrompt, results); ok {
			return response, nil
		}
		currentPrompt = prompt.RenderPrompt(messages, input.Tools) + "\n\nContinue after the tool result. If finished, answer normally without tool JSON."
	}
	finish := "stop"
	return utils.OpenAIResponse{ID: id, Object: "chat.completion", Created: time.Now().Unix(), Model: input.Model, Choices: []utils.OpenAIChoice{{Index: 0, Message: utils.OpenAIMessage{Role: "assistant", Content: lastContent}, FinishReason: &finish}}, Usage: utils.EstimateUsage(currentPrompt, lastContent)}, nil
}

func FastToolResponse(input utils.ChatRequest, id, prompt string, results []utils.LocalToolResult) (utils.OpenAIResponse, bool) {
	if !strings.EqualFold(utils.GetEnv("AUTO_TOOLS_FAST_RETURN", "true"), "true") {
		return utils.OpenAIResponse{}, false
	}
	if len(results) == 0 {
		return utils.OpenAIResponse{}, false
	}
	var lines []string
	for _, result := range results {
		if !result.OK {
			return utils.OpenAIResponse{}, false
		}
		switch result.Name {
		case "write_file", "apply_patch", "run_command":
			lines = append(lines, result.Summary)
		default:
			return utils.OpenAIResponse{}, false
		}
	}
	content := strings.Join(lines, "\n")
	finish := "stop"
	return utils.OpenAIResponse{ID: id, Object: "chat.completion", Created: time.Now().Unix(), Model: input.Model, Choices: []utils.OpenAIChoice{{Index: 0, Message: utils.OpenAIMessage{Role: "assistant", Content: content}, FinishReason: &finish}}, Usage: utils.EstimateUsage(prompt, content)}, true
}

func HasLocalToolName(name string) bool {
	for _, t := range LocalTools() {
		if t.Function.Name == name {
			return true
		}
	}
	return false
}

func ShouldRetryWithFileTool(messages []utils.Message, content string, step, maxSteps int) bool {
	if step >= maxSteps-1 || !HasLocalToolName("write_file") {
		return false
	}
	lastUser := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lastUser = strings.ToLower(utils.ContentToText(messages[i].Content))
			break
		}
	}
	if !prompt.MentionsFileCreation(lastUser) {
		return false
	}
	lower := strings.ToLower(content)
	return strings.Contains(lower, "baixe aqui") || strings.Contains(lower, "download") || strings.Contains(lower, ".html") || strings.Contains(lower, "arquivo") || strings.Contains(lower, "salvo") || strings.Contains(lower, "criei")
}

func listWorkspaceFiles(pattern string) string {
	if pattern == "" {
		pattern = "*"
	}
	if filepath.IsAbs(pattern) || strings.Contains(pattern, "..") {
		return "tool error: unsafe pattern"
	}
	matches, err := filepath.Glob(filepath.Join(utils.WorkspaceRoot(), filepath.FromSlash(pattern)))
	if err != nil {
		return "tool error: " + err.Error()
	}
	var out []string
	for _, match := range matches {
		rel, _ := filepath.Rel(utils.WorkspaceRoot(), match)
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
		rel, _ := filepath.Rel(utils.WorkspaceRoot(), p)
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

func fetchURL(rawURL string) string {
	if rawURL == "" {
		return "tool error: url is required"
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return "tool error: invalid URL"
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "tool error: only http and https URLs are allowed"
	}
	client := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequest(http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "tool error: " + err.Error()
	}
	req.Header.Set("User-Agent", "kimi-ai-proxy/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return "tool error: " + err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Sprintf("tool error: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return "tool error: " + err.Error()
	}
	text := string(body)
	if len(text) > 20000 {
		text = text[:20000] + "\n... output truncated ..."
	}
	return text
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
	cmd.Dir = utils.WorkspaceRoot()
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
