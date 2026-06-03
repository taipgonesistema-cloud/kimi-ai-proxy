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
		{Type: "function", Function: utils.FunctionTool{Name: "list_files", Description: "List files under a workspace directory. Optional glob pattern supports ** recursion.", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"path": stringParam("Relative directory path, defaults to ."), "pattern": stringParam("Optional glob pattern, for example * or **/*.go")}, "required": []string{}}}},
		{Type: "function", Function: utils.FunctionTool{Name: "glob", Description: "Find files matching a glob pattern inside the workspace. Supports ** recursion.", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"pattern": stringParam("Glob pattern, for example **/*.go"), "path": stringParam("Relative directory path, defaults to .")}, "required": []string{"pattern"}}}},
		{Type: "function", Function: utils.FunctionTool{Name: "grep", Description: "Search file contents by regular expression", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"pattern": stringParam("Regular expression"), "path": stringParam("Relative directory or file path")}, "required": []string{"pattern"}}}},
		{Type: "function", Function: utils.FunctionTool{Name: "apply_patch", Description: "Replace exact text inside a file. Arguments: path, old, new", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"path": stringParam("Relative file path"), "old": stringParam("Exact text to replace"), "new": stringParam("Replacement text")}, "required": []string{"path", "old", "new"}}}},
		{Type: "function", Function: utils.FunctionTool{Name: "run_command", Description: "Run a non-interactive cmd.exe command in the workspace. Use only for build/test/install/status commands.", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"command": stringParam("Command to run"), "timeout_ms": map[string]interface{}{"type": "integer", "description": "Timeout in milliseconds, max 120000"}}, "required": []string{"command"}}}},
		{Type: "function", Function: utils.FunctionTool{Name: "clear_chats", Description: "Delete all Kimi chat sessions to free up concurrency slots. Call this when Kimi returns a concurrency limit error.", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "required": []string{}}}},
	}
}

func ExecuteLocalTool(call utils.ToolCall) utils.LocalToolResult {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return failTool(call.Function.Name, "invalid JSON arguments: "+err.Error())
	}
	args = normalizeArgumentAliases(call.Function.Name, args, LocalTools())
	validated, err := ValidateToolArgs(call.Function.Name, args, LocalTools())
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
		return okTool(call.Function.Name, listWorkspaceFiles(argString(args, "path"), argString(args, "pattern")), "listed files", "")
	case "glob":
		return okTool(call.Function.Name, listWorkspaceFiles(argString(args, "path"), argString(args, "pattern")), "matched files", "")
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
	case "clear_chats":
		cmd := exec.Command("node", "scripts/clear-kimi-chats.mjs")
		cmd.Dir = utils.WorkspaceRoot()
		output, err := cmd.CombinedOutput()
		if err != nil {
			return failTool(call.Function.Name, string(output)+": "+err.Error())
		}
		return okTool(call.Function.Name, string(output), "Chats limpos com sucesso", "")
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

func ValidateToolArgs(name string, args map[string]interface{}, tools []utils.Tool) (map[string]interface{}, error) {
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

	for _, req := range schemaStringList(schema["required"]) {
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
			case int:
				args[key] = v
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

func schemaStringList(value interface{}) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func NormalizeToolCalls(calls []utils.ToolCall, available []utils.Tool) ([]utils.ToolCall, []string) {
	if len(calls) == 0 {
		return nil, nil
	}
	exactNames := availableToolNames(available)
	aliases := availableToolAliases(available)
	var out []utils.ToolCall
	var errs []string
	for i, call := range calls {
		name := strings.TrimSpace(call.Function.Name)
		if name == "" {
			errs = append(errs, fmt.Sprintf("tool call %d has empty function.name", i))
			continue
		}
		lookup := strings.ToLower(name)
		if exact, ok := exactNames[lookup]; ok {
			name = exact
		} else if alias, ok := aliases[lookup]; ok {
			name = alias
		} else {
			errs = append(errs, fmt.Sprintf("tool %q is not available", name))
			continue
		}

		rawArgs := strings.TrimSpace(call.Function.Arguments)
		if rawArgs == "" {
			rawArgs = "{}"
		}
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
			errs = append(errs, fmt.Sprintf("tool %s has invalid JSON arguments: %s", name, err.Error()))
			continue
		}
		if args == nil {
			args = map[string]interface{}{}
		}
		args = normalizeArgumentAliases(name, args, available)
		validated, err := ValidateToolArgs(name, args, available)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		encoded, _ := json.Marshal(validated)
		if call.ID == "" {
			call.ID = "call_" + utils.RandomID()
		}
		call.Index = i
		call.Type = "function"
		call.Function.Name = name
		call.Function.Arguments = string(encoded)
		out = append(out, call)
	}
	if len(errs) > 0 {
		return nil, errs
	}
	return out, nil
}

func availableToolNames(available []utils.Tool) map[string]string {
	names := map[string]string{}
	for _, t := range available {
		if t.Type != "function" || t.Function.Name == "" {
			continue
		}
		names[strings.ToLower(t.Function.Name)] = t.Function.Name
	}
	return names
}

func availableToolAliases(available []utils.Tool) map[string]string {
	names := availableToolNames(available)
	aliases := map[string]string{}
	groups := [][]string{
		{"run_command", "bash", "shell", "cmd"},
		{"apply_patch", "edit", "replace_in_file"},
		{"read_file", "read"},
		{"write_file", "write"},
		{"list_files", "ls", "list", "dir"},
		{"glob", "find_files", "find"},
	}
	for _, group := range groups {
		canonical := ""
		for _, candidate := range group {
			if exact, ok := names[candidate]; ok {
				canonical = exact
				break
			}
		}
		if canonical == "" {
			continue
		}
		for _, alias := range group {
			aliases[alias] = canonical
		}
	}
	return aliases
}

func normalizeArgumentAliases(name string, args map[string]interface{}, available []utils.Tool) map[string]interface{} {
	props := toolProperties(name, available)
	if len(props) == 0 {
		return args
	}
	rename := func(from, to string) {
		if _, hasTargetProp := props[to]; !hasTargetProp {
			return
		}
		if _, alreadySet := args[to]; alreadySet {
			return
		}
		if value, ok := args[from]; ok {
			args[to] = value
			delete(args, from)
		}
	}
	rename("oldString", "old")
	rename("newString", "new")
	rename("old", "oldString")
	rename("new", "newString")
	rename("cmd", "command")
	rename("shell", "command")
	rename("directory", "path")
	rename("dir", "path")
	rename("folder", "path")
	rename("file", "path")
	rename("glob", "pattern")
	rename("query", "pattern")
	return args
}

func toolProperties(name string, available []utils.Tool) map[string]interface{} {
	for _, t := range available {
		if t.Function.Name != name {
			continue
		}
		schema, _ := t.Function.Parameters.(map[string]interface{})
		props, _ := schema["properties"].(map[string]interface{})
		return props
	}
	return nil
}

func ToolValidationRetryInstruction(errors []string, available []utils.Tool) string {
	var names []string
	for _, t := range available {
		if t.Type == "function" && t.Function.Name != "" {
			names = append(names, t.Function.Name)
		}
	}
	return "Your previous tool call was invalid: " + strings.Join(errors, "; ") + ". Use exactly one available tool name: " + strings.Join(names, ", ") + ". Respond only with valid JSON matching the selected tool schema."
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
		if len(calls) > 0 {
			var validationErrors []string
			calls, validationErrors = NormalizeToolCalls(calls, input.Tools)
			if len(validationErrors) > 0 {
				if step >= maxSteps-1 {
					finish := "stop"
					message := "Tool call failed validation: " + strings.Join(validationErrors, "; ")
					return utils.OpenAIResponse{ID: id, Object: "chat.completion", Created: time.Now().Unix(), Model: input.Model, Choices: []utils.OpenAIChoice{{Index: 0, Message: utils.OpenAIMessage{Role: "assistant", Content: message}, FinishReason: &finish}}, Usage: utils.EstimateUsage(currentPrompt, message)}, nil
				}
				messages = append(messages, utils.Message{Role: "assistant", Content: content})
				messages = append(messages, utils.Message{Role: "user", Content: ToolValidationRetryInstruction(validationErrors, input.Tools)})
				currentPrompt = prompt.RenderPrompt(messages, input.Tools)
				continue
			}
		}
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

func listWorkspaceFiles(relPath, pattern string) string {
	if relPath == "" {
		relPath = "."
	}
	if pattern == "" {
		pattern = "*"
	}
	if filepath.IsAbs(pattern) || strings.Contains(pattern, "..") {
		return "tool error: unsafe pattern"
	}
	rootPath, err := safeWorkspacePath(relPath, false)
	if err != nil {
		return "tool error: " + err.Error()
	}
	info, err := os.Stat(rootPath)
	if err != nil {
		return "tool error: " + err.Error()
	}
	if !info.IsDir() {
		rel, _ := filepath.Rel(utils.WorkspaceRoot(), rootPath)
		return filepath.ToSlash(rel)
	}
	re, err := globRegex(pattern)
	if err != nil {
		return "tool error: " + err.Error()
	}
	directOnly := pattern == "*"
	var out []string
	_ = filepath.WalkDir(rootPath, func(p string, d os.DirEntry, err error) error {
		if err != nil || p == rootPath || len(out) >= 200 {
			return nil
		}
		if d.IsDir() && shouldSkipDir(d.Name()) {
			return filepath.SkipDir
		}
		relToRoot, _ := filepath.Rel(rootPath, p)
		relToRoot = filepath.ToSlash(relToRoot)
		if directOnly && strings.Contains(relToRoot, "/") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		matchTarget := relToRoot
		if !strings.Contains(pattern, "/") && !strings.Contains(pattern, "**") {
			matchTarget = filepath.Base(p)
		}
		if !re.MatchString(filepath.ToSlash(matchTarget)) {
			return nil
		}
		relWorkspace, _ := filepath.Rel(utils.WorkspaceRoot(), p)
		item := filepath.ToSlash(relWorkspace)
		if d.IsDir() {
			item += "/"
		}
		out = append(out, item)
		return nil
	})
	if len(out) == 0 {
		return "no matches"
	}
	return strings.Join(out, "\n")
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", "node_modules", ".playwright", "storage", "build":
		return true
	default:
		return false
	}
}

func globRegex(pattern string) (*regexp.Regexp, error) {
	pattern = filepath.ToSlash(pattern)
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			b.WriteByte('\\')
			b.WriteByte(pattern[i])
		default:
			b.WriteByte(pattern[i])
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
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
