package sessions

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ToolResultEvent describes a finished tool call. It is delivered to
// AgentSession.ToolResultHook after every tool execution.
type ToolResultEvent struct {
	SessionID  string
	UserID     string
	ToolCallID string
	ToolName   string
	Args       map[string]interface{}
	Result     string
	Err        error
	// IsError is true when the tool failed, decided from structured signals
	// only (see ClassifyToolResult).
	IsError bool
	// ErrorMessage is the best short description of the failure (empty on success).
	ErrorMessage string
	Duration     time.Duration
}

// SessionErrorEvent describes an error the session reported to the client.
type SessionErrorEvent struct {
	SessionID string
	UserID    string
	// Kind is "provider_error" (model/stream failure) or "ws_error" (transport/session failure).
	Kind    string
	Message string
	Fatal   bool
}

// ClassifyToolResult decides whether a tool call failed using structured
// signals only, to avoid keyword false positives on successful output:
//   - a Go error was returned
//   - the result is a JSON object with a non-empty "error" field,
//     "ok": false, "success": false, or a numeric "status"/"status_code" >= 400
//
// It returns the classification and a short error message.
func ClassifyToolResult(result string, err error) (bool, string) {
	if err != nil {
		return true, err.Error()
	}
	trimmed := strings.TrimSpace(result)
	if !strings.HasPrefix(trimmed, "{") {
		return false, ""
	}
	var obj map[string]interface{}
	if json.Unmarshal([]byte(trimmed), &obj) != nil {
		return false, ""
	}
	if msg, ok := errorFieldMessage(obj["error"]); ok {
		return true, msg
	}
	if v, ok := obj["ok"].(bool); ok && !v {
		return true, fallbackErrorMessage(obj, "ok=false")
	}
	if v, ok := obj["success"].(bool); ok && !v {
		return true, fallbackErrorMessage(obj, "success=false")
	}
	for _, key := range []string{"status", "status_code", "statusCode"} {
		if n, ok := obj[key].(float64); ok && n >= 400 && n < 600 {
			return true, fallbackErrorMessage(obj, fmt.Sprintf("HTTP %d", int(n)))
		}
	}
	return false, ""
}

func errorFieldMessage(v interface{}) (string, bool) {
	switch e := v.(type) {
	case nil:
		return "", false
	case string:
		e = strings.TrimSpace(e)
		return e, e != ""
	case bool:
		return "error=true", e
	case map[string]interface{}:
		if len(e) == 0 {
			return "", false
		}
		if m, ok := e["message"].(string); ok && m != "" {
			if c, ok := e["code"].(string); ok && c != "" {
				return c + ": " + m, true
			}
			return m, true
		}
		b, _ := json.Marshal(e)
		return string(b), true
	default:
		b, _ := json.Marshal(e)
		return string(b), true
	}
}

func fallbackErrorMessage(obj map[string]interface{}, def string) string {
	for _, key := range []string{"message", "error_message", "detail", "statusText"} {
		if s, ok := obj[key].(string); ok && strings.TrimSpace(s) != "" {
			return def + ": " + strings.TrimSpace(s)
		}
	}
	if data, ok := obj["data"].(map[string]interface{}); ok {
		if msg, ok := errorFieldMessage(data["error"]); ok {
			return def + ": " + msg
		}
	}
	return def
}

// fireToolResultHook invokes the hook without ever letting it break the session.
func (as *AgentSession) fireToolResultHook(ev ToolResultEvent) {
	if as.ToolResultHook == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil && as.Logger != nil {
			as.Logger.Printf("ToolResultHook panicked: %v", r)
		}
	}()
	as.ToolResultHook(ev)
}

// fireErrorHook invokes the hook without ever letting it break the session.
func (as *AgentSession) fireErrorHook(kind, message string, fatal bool) {
	if as.ErrorHook == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil && as.Logger != nil {
			as.Logger.Printf("ErrorHook panicked: %v", r)
		}
	}()
	as.ErrorHook(SessionErrorEvent{
		SessionID: as.SessionID,
		UserID:    as.UserID,
		Kind:      kind,
		Message:   message,
		Fatal:     fatal,
	})
}
