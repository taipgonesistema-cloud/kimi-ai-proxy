package utils

import (
	"encoding/json"
	"regexp"
	"strings"
)

func ParseToolCalls(text string) (string, []ToolCall) {
	for _, jsonText := range ToolJSONCandidates(text) {
		calls := ParseToolJSON(jsonText)
		if len(calls) == 0 {
			continue
		}
		clean := strings.TrimSpace(strings.Replace(text, jsonText, "", 1))
		return clean, calls
	}
	if calls, clean := ExtractXMLToolCalls(text); len(calls) > 0 {
		return clean, calls
	}
	return text, nil
}

func ExtractXMLToolCalls(text string) ([]ToolCall, string) {
	var calls []ToolCall
	remaining := text
	var textBuf strings.Builder
	for {
		startTag := "<tool_call>"
		endTag := "</tool_call>"
		startIdx := strings.Index(remaining, startTag)
		if startIdx == -1 {
			textBuf.WriteString(remaining)
			break
		}
		textBuf.WriteString(remaining[:startIdx])
		bodyStart := startIdx + len(startTag)
		endIdx := strings.Index(remaining[bodyStart:], endTag)
		if endIdx == -1 {
			textBuf.WriteString(remaining[startIdx:])
			break
		}
		jsonStr := strings.TrimSpace(remaining[bodyStart : bodyStart+endIdx])
		parsed := ParseToolJSON(jsonStr)
		if len(parsed) == 0 {
			var raw map[string]interface{}
			if json.Unmarshal([]byte(jsonStr), &raw) == nil {
				name, _ := raw["name"].(string)
				if name == "" {
					name, _ = raw["tool"].(string)
				}
				if name != "" {
					args := "{}"
					if a, ok := raw["arguments"]; ok {
						switch v := a.(type) {
						case string:
							args = v
						default:
							b, _ := json.Marshal(v)
							args = string(b)
						}
					} else {
						delete(raw, "name")
						delete(raw, "tool")
						b, _ := json.Marshal(raw)
						args = string(b)
					}
					parsed = append(parsed, ToolCall{
						ID:   "call_" + RandomID(),
						Type: "function",
						Function: ToolFunction{
							Name:      name,
							Arguments: args,
						},
					})
				}
			}
		}
		calls = append(calls, parsed...)
		remaining = remaining[bodyStart+endIdx+len(endTag):]
	}
	if len(calls) > 0 {
		return calls, strings.TrimSpace(textBuf.String())
	}
	return nil, text
}

func ToolJSONCandidates(text string) []string {
	trimmed := strings.TrimSpace(text)
	var candidates []string
	if trimmed != "" {
		candidates = append(candidates, StripMarkdownFence(trimmed))
	}
	for _, fenced := range ExtractFencedBlocks(text) {
		candidates = append(candidates, StripMarkdownFence(fenced))
	}
	candidates = append(candidates, ExtractJSONValues(text)...)
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

func StripMarkdownFence(text string) string {
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

func ExtractFencedBlocks(text string) []string {
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

func ExtractJSONValues(text string) []string {
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

func ParseToolJSON(jsonText string) []ToolCall {
	if !(strings.HasPrefix(jsonText, "{") || strings.HasPrefix(jsonText, "[")) {
		return nil
	}
	var raw interface{}
	if err := json.Unmarshal([]byte(jsonText), &raw); err != nil {
		return nil
	}
	return ParseToolValue(raw)
}

func ParseToolValue(raw interface{}) []ToolCall {
	switch v := raw.(type) {
	case []interface{}:
		var calls []ToolCall
		for _, item := range v {
			calls = append(calls, ParseToolValue(item)...)
		}
		for i := range calls {
			calls[i].Index = i
		}
		return calls
	case map[string]interface{}:
		if nested, ok := v["tool_calls"]; ok {
			return ParseToolValue(nested)
		}
		name, args := ParseToolObject(v)
		if name == "" {
			return nil
		}
		return []ToolCall{{Index: 0, ID: "call_" + RandomID(), Type: "function", Function: ToolFunction{Name: name, Arguments: args}}}
	default:
		return nil
	}
}

func ParseToolObject(v map[string]interface{}) (string, string) {
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
