package models

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// FormatToolResultForModel converts structured tool output to compact,
// TOON-style text for model feedback. Successful JSON outputs are encoded as
// TOON because that is where the large useful payload usually is. Failed outputs
// include TOON diagnostics and preserve the exact raw output so JSON/schema
// examples inside errors remain available for self-correction.
func FormatToolResultForModel(toolName, toolID, output string) string {
	fields := map[string]interface{}{
		"tool_feedback_format": "toon",
	}
	if strings.TrimSpace(toolName) != "" {
		fields["tool_name"] = toolName
	}
	if strings.TrimSpace(toolID) != "" {
		fields["tool_call_id"] = toolID
	}

	trimmed := strings.TrimSpace(output)
	var parsed interface{}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
		if isFailedParsedToolOutput(parsed) {
			fields["tool_status"] = "failed"
			copyUsefulErrorFields(fields, parsed)
			fields["raw_tool_output_json"] = trimmed
		} else {
			fields["tool_status"] = "succeeded"
			fields["tool_output"] = parsed
		}
		return encodeSimpleTOON(fields)
	}

	if isFailedToolOutput(output) {
		fields["tool_status"] = "failed"
		fields["error"] = output
		fields["raw_tool_output_text"] = output
		return encodeSimpleTOON(fields)
	}

	// Plain successful text is already compact and may be deliberately formatted
	// by the tool. Leave it unchanged unless it is structured JSON.
	return output
}

// FormatFunctionResponseForModel formats a stored function response for text
// based tool-message providers. It mirrors FormatToolResultForModel, but starts
// from the persisted response map instead of the raw tool output string.
func FormatFunctionResponseForModel(toolName, toolID string, response map[string]interface{}) string {
	responseBytes, _ := json.Marshal(response)
	return FormatToolResultForModel(toolName, toolID, string(responseBytes))
}

func isFailedToolOutput(output string) bool {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return false
	}

	var parsed interface{}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
		return isFailedParsedToolOutput(parsed)
	}

	lower := strings.ToLower(trimmed)
	return strings.Contains(lower, "error") ||
		strings.Contains(lower, "failed") ||
		strings.Contains(lower, "exception") ||
		strings.Contains(lower, "invalid") ||
		strings.Contains(lower, "not found") ||
		strings.Contains(lower, "unauthorized") ||
		strings.Contains(lower, "forbidden")
}

func isFailedParsedToolOutput(value interface{}) bool {
	switch v := value.(type) {
	case map[string]interface{}:
		if _, ok := v["error"]; ok {
			return true
		}
		if okValue, ok := v["ok"].(bool); ok && !okValue {
			return true
		}
		if status, ok := numberAsFloat(v["status"]); ok && status >= 400 {
			return true
		}
		if content, ok := v["content"]; ok {
			return isFailedParsedToolOutput(content)
		}
	case []interface{}:
		for _, item := range v {
			if isFailedParsedToolOutput(item) {
				return true
			}
		}
	case string:
		return isFailedToolOutput(v)
	}
	return false
}

func copyUsefulErrorFields(fields map[string]interface{}, parsed interface{}) {
	root, ok := parsed.(map[string]interface{})
	if !ok {
		fields["error"] = parsed
		return
	}

	if value, ok := root["status"]; ok {
		fields["http_status"] = value
	}
	if value, ok := root["ok"]; ok {
		fields["ok"] = value
	}
	if value, ok := root["error"]; ok {
		fields["error"] = value
	}
	if value, ok := root["message"]; ok {
		fields["message"] = value
	}
	if value, ok := root["partial_output"]; ok && value != "" {
		fields["partial_output"] = value
	}
	if value, ok := root["data"]; ok {
		fields["data"] = value
	}
	if _, ok := fields["error"]; !ok {
		fields["error"] = parsed
	}
}

func encodeSimpleTOON(fields map[string]interface{}) string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// Keep the most important diagnostic fields at the top.
	keys = prioritizeKeys(keys, []string{
		"tool_feedback_format",
		"tool_status",
		"tool_name",
		"tool_call_id",
		"http_status",
		"ok",
		"error",
		"message",
		"partial_output",
		"data",
		"tool_output",
		"raw_tool_output_json",
		"raw_tool_output_text",
	})

	var b strings.Builder
	for _, key := range keys {
		writeTOONKeyValue(&b, key, fields[key], 0)
	}
	return strings.TrimRight(b.String(), "\n")
}

func writeTOONKeyValue(b *strings.Builder, key string, value interface{}, indent int) {
	writeIndent(b, indent)
	if isTOONScalar(value) {
		b.WriteString(key)
		b.WriteString(": ")
		b.WriteString(formatTOONScalar(value))
		b.WriteByte('\n')
		return
	}

	b.WriteString(key)
	if array, ok := value.([]interface{}); ok {
		b.WriteString(fmt.Sprintf("[%d]", len(array)))
	}
	b.WriteString(":\n")
	writeTOONValue(b, value, indent+1)
}

func writeTOONValue(b *strings.Builder, value interface{}, indent int) {
	switch v := value.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			writeTOONKeyValue(b, key, v[key], indent)
		}
	case []interface{}:
		for _, item := range v {
			writeIndent(b, indent)
			if isTOONScalar(item) {
				b.WriteString("- ")
				b.WriteString(formatTOONScalar(item))
				b.WriteByte('\n')
				continue
			}
			b.WriteString("-\n")
			writeTOONValue(b, item, indent+1)
		}
	default:
		writeIndent(b, indent)
		b.WriteString(formatTOONScalar(v))
		b.WriteByte('\n')
	}
}

func writeIndent(b *strings.Builder, indent int) {
	for i := 0; i < indent; i++ {
		b.WriteString("  ")
	}
}

func isTOONScalar(value interface{}) bool {
	switch value.(type) {
	case nil, string, bool, float64, float32, int, int64, int32, uint, uint64, uint32, json.Number:
		return true
	default:
		return false
	}
}

func prioritizeKeys(keys []string, priority []string) []string {
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		seen[key] = true
	}

	ordered := make([]string, 0, len(keys))
	used := make(map[string]bool, len(keys))
	for _, key := range priority {
		if seen[key] {
			ordered = append(ordered, key)
			used[key] = true
		}
	}
	for _, key := range keys {
		if !used[key] {
			ordered = append(ordered, key)
		}
	}
	return ordered
}

func formatTOONScalar(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case string:
		encoded, _ := json.Marshal(v)
		return string(encoded)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64, float32, int, int64, int32, uint, uint64, uint32:
		return fmt.Sprint(v)
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			fallback, _ := json.Marshal(fmt.Sprint(v))
			return string(fallback)
		}
		return string(encoded)
	}
}

func numberAsFloat(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case json.Number:
		parsed, err := v.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}
