package sessions

import (
	"errors"
	"testing"
)

func TestClassifyToolResult(t *testing.T) {
	cases := []struct {
		name   string
		result string
		err    error
		want   bool
	}{
		{"go error", "", errors.New("boom"), true},
		{"plain text mentioning error", "Found 3 errors in the log file; all resolved.", nil, false},
		{"json success with error word in data", `{"ok":true,"data":{"note":"error handling docs"}}`, nil, false},
		{"json error string", `{"error":"permission denied"}`, nil, true},
		{"json error null", `{"error":null,"result":1}`, nil, false},
		{"json error empty", `{"error":""}`, nil, false},
		{"json error object", `{"error":{"code":"Forbidden","message":"Insufficient privileges"}}`, nil, true},
		{"ok false", `{"ok":false,"status":403}`, nil, true},
		{"success false", `{"success":false,"message":"nope"}`, nil, true},
		{"status 404", `{"status":404,"data":null}`, nil, true},
		{"status 200", `{"status":200,"ok":true}`, nil, false},
		{"status string", `{"status":"failed"}`, nil, false},
		{"json array", `[{"error":"x"}]`, nil, false},
	}
	for _, tc := range cases {
		got, msg := ClassifyToolResult(tc.result, tc.err)
		if got != tc.want {
			t.Errorf("%s: got %v (%q), want %v", tc.name, got, msg, tc.want)
		}
		if got && msg == "" {
			t.Errorf("%s: expected non-empty message", tc.name)
		}
	}
}

func TestHooksRecoverPanics(t *testing.T) {
	as := &AgentSession{
		ToolResultHook: func(ToolResultEvent) { panic("hook") },
		ErrorHook:      func(SessionErrorEvent) { panic("hook") },
	}
	as.fireToolResultHook(ToolResultEvent{})
	as.fireErrorHook("ws_error", "x", false)
}
